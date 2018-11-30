// Copyright 2018, Shulhan <ms@kilabit.info>. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package rescached implement DNS forwarder with cache.
package rescached

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"

	libbytes "github.com/shuLhan/share/lib/bytes"
	"github.com/shuLhan/share/lib/debug"
	"github.com/shuLhan/share/lib/dns"
	libio "github.com/shuLhan/share/lib/io"
	libnet "github.com/shuLhan/share/lib/net"
)

const (
	_maxQueue     = 512
	_maxForwarder = 4
)

// List of error messages.
var (
	ErrNetworkType = errors.New("Invalid network type")
)

// Server implement caching DNS server.
type Server struct {
	dnsServer  *dns.Server
	nsParents  []*net.UDPAddr
	reqQueue   chan *dns.Request
	fwQueue    chan *dns.Request
	fwDoHQueue chan *dns.Request
	fwStop     chan bool
	cw         *cacheWorker
	opts       *Options
}

//
// New create and initialize new rescached server.
//
func New(opts *Options) (*Server, error) {
	srv := &Server{
		dnsServer:  new(dns.Server),
		reqQueue:   make(chan *dns.Request, _maxQueue),
		fwQueue:    make(chan *dns.Request, _maxQueue),
		fwDoHQueue: make(chan *dns.Request, _maxQueue),
		fwStop:     make(chan bool),
		cw:         newCacheWorker(opts.CachePruneDelay, opts.CacheThreshold),
		opts:       opts,
	}

	if len(srv.opts.FileResolvConf) == 0 {
		srv.nsParents = srv.opts.NSParents
	} else {
		err := srv.loadResolvConf()
		if err != nil {
			log.Printf("! loadResolvConf: %s\n", err)
			srv.nsParents = srv.opts.NSParents
		} else {
			fmt.Printf("= Name servers fallback: %v\n", srv.opts.NSParents)
		}
	}

	srv.dnsServer.Handler = srv

	srv.LoadHostsFile("")

	return srv, nil
}

//
// LoadHostsFile parse hosts formatted file and put it into caches.
//
func (srv *Server) LoadHostsFile(path string) {
	if len(path) == 0 {
		fmt.Println("= Loading system hosts file")
	} else {
		fmt.Printf("= Loading hosts file '%s'\n", path)
	}

	msgs, err := dns.HostsLoad(path)
	if err != nil {
		return
	}

	srv.populateCaches(msgs)
}

//
// LostMasterFile parse master file and put the result into caches.
//
func (srv *Server) LoadMasterFile(path string) {
	fmt.Printf("= Loading master file '%s'\n", path)

	msgs, err := dns.MasterLoad(path, "", 0)
	if err != nil {
		return
	}

	srv.populateCaches(msgs)
}

func (srv *Server) loadResolvConf() error {
	rc, err := libnet.NewResolvConf(srv.opts.FileResolvConf)
	if err != nil {
		return err
	}

	nsAddrs, err := dns.ParseNameServers(rc.NameServers)
	if err != nil {
		return err
	}

	if len(nsAddrs) > 0 {
		srv.nsParents = nsAddrs
	} else {
		srv.nsParents = srv.opts.NSParents
	}

	return nil
}

func (srv *Server) populateCaches(msgs []*dns.Message) {
	n := 0
	for x := 0; x < len(msgs); x++ {
		ok := srv.cw.add(msgs[x], true)
		if ok {
			n++
		}
		msgs[x] = nil
	}

	fmt.Printf("== %d record cached\n", n)
}

//
// ServeDNS handle DNS request from server.
//
func (srv *Server) ServeDNS(req *dns.Request) {
	srv.reqQueue <- req
}

//
// Start the server, waiting for DNS query from clients, read it and response
// it.
//
func (srv *Server) Start() error {
	fmt.Printf("= Listening on '%s:%d'\n", srv.opts.ListenAddress,
		srv.opts.ListenPort)

	err := srv.runForwarders()
	if err != nil {
		return err
	}

	if len(srv.opts.DoHCert) > 0 && len(srv.opts.DoHCertKey) > 0 {
		fmt.Printf("= DoH listening on '%s:%d'\n",
			srv.opts.ListenAddress, srv.opts.DoHPort)

		err = srv.runDoHForwarders()
		if err != nil {
			return err
		}
	}

	if len(srv.opts.FileResolvConf) > 0 {
		go srv.watchResolvConf()
	}

	go srv.cw.start()
	go srv.processRequestQueue()

	serverOptions := &dns.ServerOptions{
		IPAddress:        srv.opts.ListenAddress,
		UDPPort:          srv.opts.ListenPort,
		TCPPort:          srv.opts.ListenPort,
		DoHPort:          srv.opts.DoHPort,
		DoHCert:          srv.opts.DoHCert,
		DoHCertKey:       srv.opts.DoHCertKey,
		DoHAllowInsecure: srv.opts.DoHAllowInsecure,
	}

	err = srv.dnsServer.ListenAndServe(serverOptions)

	return err
}

func (srv *Server) runForwarders() (err error) {
	max := _maxForwarder

	fmt.Printf("= Name servers: %v\n", srv.nsParents)

	if len(srv.nsParents) > max {
		max = len(srv.nsParents)
	}

	for x := 0; x < max; x++ {
		var (
			cl    dns.Client
			raddr *net.UDPAddr
		)

		nsIdx := x % len(srv.nsParents)
		raddr = srv.nsParents[nsIdx]

		if srv.opts.ConnType == dns.ConnTypeUDP {
			cl, err = dns.NewUDPClient(raddr.String())
			if err != nil {
				log.Fatal("processForwardQueue: NewUDPClient:", err)
				return
			}
		}

		go srv.processForwardQueue(cl, raddr)
	}
	return
}

func (srv *Server) runDoHForwarders() error {
	fmt.Printf("= DoH name servers: %v\n", srv.opts.DoHParents)

	for x := 0; x < len(srv.opts.DoHParents); x++ {
		cl, err := dns.NewDoHClient(srv.opts.DoHParents[x], srv.opts.DoHAllowInsecure)
		if err != nil {
			log.Fatal("processForwardQueue: NewDoHClient:", err)
			return err
		}

		go srv.processDoHForwardQueue(cl)
	}

	return nil
}

func (srv *Server) stopForwarders() {
	srv.fwStop <- true
}

func (srv *Server) processRequestQueue() {
	var err error

	for req := range srv.reqQueue {
		if debug.Value >= 1 {
			fmt.Printf("< request: %4d %10c %s\n", req.Kind, '-', req.Message.Question)
		}

		// Check if request query name exist in cache.
		libbytes.ToLower(&req.Message.Question.Name)
		qname := string(req.Message.Question.Name)
		_, res := srv.cw.caches.get(qname, req.Message.Question.Type, req.Message.Question.Class)
		if res == nil {
			// Check and/or push if the same request already
			// forwarded before.
			dup := srv.cw.cachesRequest.push(qname, req)
			if dup {
				continue
			}

			if req.Kind == dns.ConnTypeDoH {
				srv.fwDoHQueue <- req
			} else {
				srv.fwQueue <- req
			}
			continue
		}

		if res.checkExpiration() {
			// Check and/or push if the same request already
			// forwarded before.
			dup := srv.cw.cachesRequest.push(qname, req)
			if dup {
				continue
			}

			if req.Kind == dns.ConnTypeDoH {
				srv.fwDoHQueue <- req
			} else {
				srv.fwQueue <- req
			}
			continue
		}

		res.message.SetID(req.Message.Header.ID)

		switch req.Kind {
		case dns.ConnTypeUDP:
			if req.Sender != nil {
				_, err = req.Sender.Send(res.message, req.UDPAddr)
				if err != nil {
					log.Println("! processRequestQueue: Sender.Send:", err)
				}
			}
			dns.FreeRequest(req)

		case dns.ConnTypeTCP:
			if req.Sender != nil {
				_, err = req.Sender.Send(res.message, nil)
				if err != nil {
					log.Println("! processRequestQueue: Sender.Send:", err)
				}
			}
			dns.FreeRequest(req)

		case dns.ConnTypeDoH:
			if req.ResponseWriter != nil {
				_, err = req.ResponseWriter.Write(res.message.Packet)
				if err != nil {
					log.Println("! processRequestQueue: ResponseWriter.Write:", err)
				}
				req.ResponseWriter.(http.Flusher).Flush()
				req.ChanResponded <- true
			}

		default:
			dns.FreeRequest(req)
		}

		// Ignore update on local caches
		if res.receivedAt == 0 {
			if debug.Value >= 1 {
				fmt.Printf("= local  : %s\n", res.message.Question)
			}
			continue
		}

		srv.cw.updateQueue <- res
	}
}

func (srv *Server) processForwardQueue(cl dns.Client, raddr net.Addr) {
	for {
		select {
		case req := <-srv.fwQueue:
			var (
				err error
				res *dns.Message
			)

			switch srv.opts.ConnType {
			case dns.ConnTypeUDP:
				res, err = cl.Query(req.Message, raddr)

			case dns.ConnTypeTCP:
				cl, err = dns.NewTCPClient(raddr.String())
				if err != nil {
					dns.FreeRequest(req)
					continue
				}

				res, err = cl.Query(req.Message, nil)

				cl.Close()
			}
			if err != nil {
				srv.freeRequests(req)
				continue
			}

			srv.processForwardResponse(req, res)

		case <-srv.fwStop:
			return
		}
	}
}

func (srv *Server) processDoHForwardQueue(cl *dns.DoHClient) {
	for req := range srv.fwDoHQueue {
		res, err := cl.Query(req.Message, nil)
		if err != nil {
			srv.freeRequests(req)
			continue
		}

		srv.processForwardResponse(req, res)
	}
}

func (srv *Server) processForwardResponse(req *dns.Request, res *dns.Message) {
	var ok bool

	if bytes.Equal(req.Message.Question.Name, res.Question.Name) {
		if req.Message.Question.Type == res.Question.Type {
			ok = true
		}
	}
	if !ok {
		if res != nil {
			freeMessage(res)
		}
		srv.freeRequests(req)
		return
	}

	qname := string(req.Message.Question.Name)
	reqs := srv.cw.cachesRequest.pops(qname, req.Message.Question.Type, req.Message.Question.Class)

	for x := 0; x < len(reqs); x++ {
		res.SetID(reqs[x].Message.Header.ID)

		switch reqs[x].Kind {
		case dns.ConnTypeUDP:
			if reqs[x].Sender != nil {
				_, err := reqs[x].Sender.Send(res, reqs[x].UDPAddr)
				if err != nil {
					log.Println("! processForwardQueue: Send:", err)
				}
			}
			dns.FreeRequest(reqs[x])

		case dns.ConnTypeTCP:
			if reqs[x].Sender != nil {
				_, err := reqs[x].Sender.Send(res, nil)
				if err != nil {
					log.Println("! processForwardQueue: Send:", err)
				}
			}
			dns.FreeRequest(reqs[x])

		case dns.ConnTypeDoH:
			if reqs[x].ResponseWriter != nil {
				_, err := req.ResponseWriter.Write(res.Packet)
				if err != nil {
					log.Println("! processRequestQueue: ResponseWriter.Write:", err)
				}
				req.ResponseWriter.(http.Flusher).Flush()
				reqs[x].ChanResponded <- true
			}

		default:
			dns.FreeRequest(reqs[x])
		}

		reqs[x] = nil
	}

	srv.cw.addQueue <- res
}

//
// freeRequests clear all failed request from forward queue.
//
func (srv *Server) freeRequests(req *dns.Request) {
	qname := string(req.Message.Question.Name)
	reqs := srv.cw.cachesRequest.pops(qname, req.Message.Question.Type, req.Message.Question.Class)

	log.Printf("! freeReq: %4d %10c %s\n", len(reqs), '-', req.Message.Question)

	for x := 0; x < len(reqs); x++ {
		dns.FreeRequest(reqs[x])
		reqs[x] = nil
	}
}

func (srv *Server) watchResolvConf() {
	watcher, err := libio.NewWatcher(srv.opts.FileResolvConf, 0)
	if err != nil {
		log.Fatal("! watchResolvConf: ", err)
	}

	for fi := range watcher.C {
		if fi == nil {
			if srv.nsParents[0] == srv.opts.NSParents[0] {
				continue
			}

			log.Printf("= ResolvConf: file '%s' deleted\n",
				srv.opts.FileResolvConf)

			srv.nsParents = srv.opts.NSParents
		} else {
			err := srv.loadResolvConf()
			if err != nil {
				log.Printf("! loadResolvConf: %s\n", err)
				srv.nsParents = srv.opts.NSParents
			}
		}

		srv.stopForwarders()
		err = srv.runForwarders()
		if err != nil {
			log.Printf("! watchResolvConf: %s\n", err)
		}
	}
}
