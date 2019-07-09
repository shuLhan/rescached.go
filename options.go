// Copyright 2018, Shulhan <ms@kilabit.info>. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rescached

import (
	"fmt"
	"time"

	"github.com/shuLhan/share/lib/debug"
	"github.com/shuLhan/share/lib/dns"
	libnet "github.com/shuLhan/share/lib/net"
	libstrings "github.com/shuLhan/share/lib/strings"
)

//
// Options for running rescached.
//
type Options struct {
	dns.ServerOptions
	Timeout        time.Duration
	FilePID        string
	FileResolvConf string
	DirHosts       string
	DirMaster      string
}

//
// NewOptions create and initialize options with default values.
//
func NewOptions() *Options {
	return &Options{
		ServerOptions: dns.ServerOptions{
			IPAddress: "127.0.0.1",
		},

		Timeout: 6 * time.Second,
		FilePID: "rescached.pid",
	}
}

//
// init check and initialize the Options instance with default values.
//
func (opts *Options) init() {
	if len(opts.IPAddress) == 0 {
		opts.IPAddress = "127.0.0.1"
	}
	if opts.Timeout <= 0 || opts.Timeout > (6*time.Second) {
		opts.Timeout = 6 * time.Second
	}
	if len(opts.FilePID) == 0 {
		opts.FilePID = "rescached.pid"
	}
	if len(opts.FileResolvConf) > 0 {
		_, _ = opts.loadResolvConf()
	}
}

func (opts *Options) loadResolvConf() (ok bool, err error) {
	rc, err := libnet.NewResolvConf(opts.FileResolvConf)
	if err != nil {
		return false, err
	}

	if debug.Value > 0 {
		fmt.Printf("rescached: loadResolvConf: %+v\n", rc)
	}

	if len(rc.NameServers) == 0 {
		return false, nil
	}

	for x := 0; x < len(rc.NameServers); x++ {
		rc.NameServers[x] = "udp://" + rc.NameServers[x]
	}

	if libstrings.IsEqual(opts.NameServers, rc.NameServers) {
		return false, nil
	}

	if len(opts.NameServers) == 0 {
		opts.NameServers = rc.NameServers
	} else {
		opts.FallbackNS = rc.NameServers
	}

	return true, nil
}
