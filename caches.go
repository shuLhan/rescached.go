// Copyright 2018, Shulhan <ms@kilabit.info>. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rescached

import (
	"fmt"
	"log"
	"sync"

	"github.com/shuLhan/share/lib/dns"
)

//
// caches represent a mapping between domain-name and cached responses.
//
type caches struct {
	n int
	v sync.Map
}

var _caches *caches

//
// newCaches create, initialize, and return new caches.
//
func newCaches() *caches {
	return &caches{}
}

//
// get cached response based on request name and type.
//
func (c *caches) get(req *request) *response {
	v, ok := c.v.Load(string(req.msg.Question.Name))
	if !ok {
		return nil
	}
	cres := v.(*cacheResponses)
	if cres == nil || cres.v == nil {
		return nil
	}
	return cres.get(req)
}

//
// put response to cache only if it's contains an answer and TTL is greater
// than zero (0).
//
func (c *caches) put(res *response) {
	if res.msg.Header.ANCount == 0 || len(res.msg.Answer) == 0 {
		log.Printf("! Empty answers on %s\n", res.msg)
		return
	}
	for x := 0; x < len(res.msg.Answer); x++ {
		if res.msg.Answer[x].TTL == 0 {
			return
		}
	}

	if DebugLevel >= 1 {
		fmt.Printf("+ caching: %s\n", res.msg.Answer[0])
	}

	qname := string(res.msg.Question.Name)
	v, ok := c.v.Load(qname)
	if !ok {
		cres := newCacheResponses(res)
		c.v.Store(qname, cres)
		c.n++
		return
	}

	cres := v.(*cacheResponses)
	cres.upsert(res)
}

//
// LoadHostsFile parse hosts formatted file as put it into caches.
//
func LoadHostsFile(path string) {
	if DebugLevel >= 1 {
		if len(path) == 0 {
			log.Println("= Loading system hosts file")
		} else {
			log.Printf("= Loading hosts file '%s'", path)
		}
	}

	msgs, err := dns.HostsLoad(path)
	if err != nil {
		return
	}

	for _, msg := range msgs {
		res := &response{
			// Flag to indicated that this response is from local
			// hosts file.
			receivedAt: 0,
			msg:        msg,
		}
		_caches.put(res)
	}

	if DebugLevel >= 1 {
		log.Printf("== %d loaded\n", len(msgs))
	}
}
