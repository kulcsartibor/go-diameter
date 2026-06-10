// Copyright 2013-2026 go-diameter authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package diam_test

import (
	"bytes"
	"sync"
	"testing"
	"time"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/avp"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
	"github.com/fiorix/go-diameter/v4/diam/diamtest"
	"github.com/fiorix/go-diameter/v4/diam/dict"
)

// rawTestKey is the Gy CCR family (Application-Id 4, command 272).
var rawTestKey = diam.RawKey{AppID: 4, Code: 272}

func buildCCR() *diam.Message {
	m := diam.NewMessage(diam.CreditControl, diam.RequestFlag,
		diam.CHARGING_CONTROL_APP_ID, 0xaabbccdd, 0x11223344, dict.Default)
	m.NewAVP(avp.SessionID, avp.Mbit, 0, datatype.UTF8String("raw.test;1;1"))
	m.NewAVP(avp.OriginHost, avp.Mbit, 0, datatype.DiameterIdentity("client"))
	m.NewAVP(avp.OriginRealm, avp.Mbit, 0, datatype.DiameterIdentity("test"))
	m.NewAVP(avp.CCRequestType, avp.Mbit, 0, datatype.Enumerated(1))
	m.NewAVP(avp.CCRequestNumber, avp.Mbit, 0, datatype.Unsigned32(0))
	return m
}

// TestRawHandlerBypassesDictionary verifies that a registered RawHandler
// receives the exact message bytes (header + body) and that the
// dictionary codec is never invoked for that message.
func TestRawHandlerBypassesDictionary(t *testing.T) {
	ccr := buildCCR()
	want, err := ccr.Serialize()
	if err != nil {
		t.Fatal(err)
	}

	var got []byte
	var gotHdr diam.Header
	done := make(chan struct{})

	srv := diamtest.NewUnstartedServer(diam.HandlerFunc(func(c diam.Conn, m *diam.Message) {
		t.Errorf("dictionary handler must not run for a hooked message; got command %d", m.Header.CommandCode)
	}), dict.Default)
	srv.Config.RawHandlers = map[diam.RawKey]diam.RawHandler{
		rawTestKey: func(c diam.Conn, hdr *diam.Header, msg []byte) {
			// Copy out: msg aliases a pooled buffer (ownership rule).
			got = append([]byte(nil), msg...)
			gotHdr = *hdr
			close(done)
		},
	}
	srv.Start()
	defer srv.Close()

	cli, err := diam.Dial(srv.Addr, nil, dict.Default)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cli.Write(want); err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("raw handler not invoked within 5s")
	}

	if !bytes.Equal(got, want) {
		t.Fatalf("raw handler got %d bytes, want %d (byte-identical)", len(got), len(want))
	}
	if gotHdr.ApplicationID != 4 || gotHdr.CommandCode != 272 {
		t.Errorf("header: app=%d code=%d, want 4/272", gotHdr.ApplicationID, gotHdr.CommandCode)
	}
	if gotHdr.HopByHopID != 0xaabbccdd || gotHdr.EndToEndID != 0x11223344 {
		t.Errorf("header ids: hbh=%#x e2e=%#x", gotHdr.HopByHopID, gotHdr.EndToEndID)
	}
	if gotHdr.CommandFlags&diam.RequestFlag == 0 {
		t.Error("request flag not set in header")
	}
}

// TestRawHandlerUnmatchedUsesDictionary verifies that messages whose
// (AppID, Code) is not hooked still flow through the stock dictionary
// mux, even while a RawHandler is registered for a different key.
func TestRawHandlerUnmatchedUsesDictionary(t *testing.T) {
	var mu sync.Mutex
	dictSeen := 0

	mux := diam.NewServeMux()
	mux.HandleFunc("CER", func(c diam.Conn, m *diam.Message) {
		mu.Lock()
		dictSeen++
		mu.Unlock()
	})

	srv := diamtest.NewUnstartedServer(mux, dict.Default)
	srv.Config.RawHandlers = map[diam.RawKey]diam.RawHandler{
		rawTestKey: func(c diam.Conn, hdr *diam.Header, msg []byte) {
			t.Error("raw handler must not run for an unmatched (app,code)")
		},
	}
	srv.Start()
	defer srv.Close()

	cli, err := diam.Dial(srv.Addr, nil, dict.Default)
	if err != nil {
		t.Fatal(err)
	}

	// Send a CER (app 0, command 257) — not the hooked key.
	cer := diam.NewMessage(diam.CapabilitiesExchange, diam.RequestFlag, 0, 1, 1, dict.Default)
	cer.NewAVP(avp.OriginHost, avp.Mbit, 0, datatype.DiameterIdentity("client"))
	cer.NewAVP(avp.OriginRealm, avp.Mbit, 0, datatype.DiameterIdentity("test"))
	b, err := cer.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cli.Write(b); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(5 * time.Second)
	for {
		mu.Lock()
		n := dictSeen
		mu.Unlock()
		if n > 0 {
			return
		}
		select {
		case <-deadline:
			t.Fatal("dictionary CER handler not invoked within 5s")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
