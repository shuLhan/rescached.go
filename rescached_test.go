// Copyright 2019, Shulhan <ms@kilabit.info>. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rescached

import (
	"log"
	"os"
	"testing"

	"github.com/shuLhan/share/lib/debug"
	"github.com/shuLhan/share/lib/dns"
)

func TestMain(m *testing.M) {
	// Make debug counted on coverage
	debug.Value = 2

	// Add response for testing non-expired message, so we can check if
	// response.message.SubTTL work as expected.
	msg := dns.NewMessage()
	msg.Packet = []byte{
		// Header
		0x8c, 0xdb, 0x81, 0x80,
		0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		// Question
		0x07, 0x6b, 0x69, 0x6c, 0x61, 0x62, 0x69, 0x74,
		0x04, 0x69, 0x6e, 0x66, 0x6f, 0x00,
		0x00, 0x01, 0x00, 0x01,
		// Answer
		0xc0, 0x0c, 0x00, 0x01, 0x00, 0x01,
		0x00, 0x00, 0x01, 0x68,
		0x00, 0x04,
		0x67, 0xc8, 0x04, 0xa2,
		// OPT
		0x00, 0x00, 0x29, 0x05, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}
	err := msg.Unpack()
	if err != nil {
		log.Fatal(err)
	}

	res := newResponse(msg)
	_testResponses = append(_testResponses, res)

	os.Exit(m.Run())
}
