// Copyright 2013-2026 go-diameter authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// Command gyserver is the Harness B server (codec-design.md §3 Phase 3):
// a go-diameter server that answers Gy CCR with a canned CCA, using
// either the static gycodec (raw fast path) or the stock dictionary
// codec, selectable at startup so the two can be compared under
// identical traffic.
//
// It carries NO business logic: it echoes Session-Id and the
// request type/number, grants a fixed quota, and answers Result-Code
// 2001 at the message and per-MSCC level.
package main

import (
	"flag"
	"log"
	"net/http"
	"runtime"
	"sync"
	"time"

	_ "net/http/pprof"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/avp"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
	"github.com/fiorix/go-diameter/v4/diam/dict"
	"github.com/fiorix/go-diameter/v4/diam/sm"
	"github.com/fiorix/go-diameter/v4/gycodec"
)

const grantedTotalOctets = 1_000_000

func main() {
	addr := flag.String("addr", ":3868", "address ip:port to listen on")
	codec := flag.String("codec", "static", "codec for app-id 4 CCR: static | dict")
	host := flag.String("host", "gyserver", "Origin-Host")
	realm := flag.String("realm", "go-diameter", "Origin-Realm")
	pprofAddr := flag.String("pprof", "", "ip:port for net/http/pprof (empty disables)")
	memInterval := flag.Duration("memstats", 0, "interval for runtime.MemStats logging (0 disables)")
	concurrent := flag.Int("concurrent", 0, "MaxConcurrentHandlers (0 = sequential)")
	flag.Parse()

	if *codec != "static" && *codec != "dict" {
		log.Fatalf("invalid -codec %q: want static or dict", *codec)
	}

	settings := &sm.Settings{
		OriginHost:       datatype.DiameterIdentity(*host),
		OriginRealm:      datatype.DiameterIdentity(*realm),
		VendorID:         13,
		ProductName:      "gyserver",
		FirmwareRevision: 1,
	}
	mux := sm.New(settings)
	go func() {
		for err := range mux.ErrorReports() {
			log.Printf("error report: %v", err)
		}
	}()

	srv := &diam.Server{
		Addr:                  *addr,
		Handler:               mux,
		Dict:                  dict.Default,
		MaxConcurrentHandlers: *concurrent,
	}

	switch *codec {
	case "static":
		srv.RawHandlers = map[diam.RawKey]diam.RawHandler{
			{AppID: 4, Code: 272}: staticCCRHandler(),
		}
		log.Printf("gyserver: static gycodec on app-id 4/272, listening on %s", *addr)
	case "dict":
		mux.HandleIdx(
			diam.CommandIndex{AppID: 4, Code: 272, Request: true},
			diam.HandlerFunc(dictCCRHandler),
		)
		log.Printf("gyserver: dictionary codec on app-id 4/272, listening on %s", *addr)
	}

	if *pprofAddr != "" {
		go func() { log.Println("pprof:", http.ListenAndServe(*pprofAddr, nil)) }()
	}
	if *memInterval > 0 {
		go logMemStats(*memInterval)
	}

	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

// staticCCRHandler returns a RawHandler that parses CCR with gycodec and
// replies with a CCA built by gycodec, reusing pooled structs and write
// buffers so the steady-state hot path allocates nothing.
func staticCCRHandler() diam.RawHandler {
	ccrPool := sync.Pool{New: func() interface{} { return new(gycodec.CCR) }}
	ccaPool := sync.Pool{New: func() interface{} { return new(gycodec.CCA) }}
	bufPool := sync.Pool{New: func() interface{} { b := make([]byte, 0, 1024); return &b }}

	return func(c diam.Conn, hdr *diam.Header, msg []byte) {
		ccr := ccrPool.Get().(*gycodec.CCR)
		cca := ccaPool.Get().(*gycodec.CCA)
		bufp := bufPool.Get().(*[]byte)
		defer func() {
			ccrPool.Put(ccr)
			ccaPool.Put(cca)
			bufPool.Put(bufp)
		}()

		if err := ccr.ParseFrom(msg); err != nil {
			log.Printf("static: ParseFrom: %v", err)
			return
		}

		cca.Reset()
		// Echo the request's correlation fields.
		cca.Hdr.CommandFlags = 0 // answer
		cca.Hdr.HopByHopID = hdr.HopByHopID
		cca.Hdr.EndToEndID = hdr.EndToEndID
		cca.SessionID = ccr.SessionID
		cca.ResultCode = 2001
		cca.HasResultCode = true
		cca.OriginHost = []byte("gyserver")
		cca.OriginRealm = []byte("go-diameter")
		cca.AuthApplicationID = 4
		cca.HasAuthApplicationID = true
		cca.CCRequestType = ccr.CCRequestType
		cca.HasCCRequestType = ccr.HasCCRequestType
		cca.CCRequestNumber = ccr.CCRequestNumber
		cca.HasCCRequestNumber = ccr.HasCCRequestNumber

		// Grant a fixed quota per requested MSCC.
		for i := range ccr.MSCC {
			in := &ccr.MSCC[i]
			out := gycodec.MSCC{
				RatingGroup:       in.RatingGroup,
				HasRatingGroup:    in.HasRatingGroup,
				ServiceIdentifier: in.ServiceIdentifier, HasServiceIdentifier: in.HasServiceIdentifier,
				HasGranted: true,
				Granted: gycodec.GrantedServiceUnit{
					CCTotalOctets:    grantedTotalOctets,
					HasCCTotalOctets: true,
				},
				ResultCode: 2001, HasResultCode: true,
			}
			cca.MSCC = append(cca.MSCC, out)
		}

		out := cca.AppendTo((*bufp)[:0])
		*bufp = out
		if _, err := c.Write(out); err != nil {
			log.Printf("static: write: %v", err)
		}
	}
}

// dictCCRHandler answers a CCR using the dictionary codec, mirroring the
// static handler's canned response so the two are comparable.
func dictCCRHandler(c diam.Conn, m *diam.Message) {
	sessionID, err := m.FindAVP(avp.SessionID, 0)
	if err != nil {
		log.Printf("dict: missing Session-Id: %v", err)
		return
	}
	var reqType, reqNum *diam.AVP
	reqType, _ = m.FindAVP(avp.CCRequestType, 0)
	reqNum, _ = m.FindAVP(avp.CCRequestNumber, 0)

	a := m.Answer(diam.Success)
	a.AddAVP(sessionID)
	a.NewAVP(avp.OriginHost, avp.Mbit, 0, datatype.DiameterIdentity("gyserver"))
	a.NewAVP(avp.OriginRealm, avp.Mbit, 0, datatype.DiameterIdentity("go-diameter"))
	a.NewAVP(avp.AuthApplicationID, avp.Mbit, 0, datatype.Unsigned32(4))
	if reqType != nil {
		a.AddAVP(reqType)
	}
	if reqNum != nil {
		a.AddAVP(reqNum)
	}
	for _, mscc := range collectMSCC(m) {
		rg, _ := findChild(mscc, avp.RatingGroup)
		si, _ := findChild(mscc, avp.ServiceIdentifier)
		children := []*diam.AVP{}
		if rg != nil {
			children = append(children, rg)
		}
		if si != nil {
			children = append(children, si)
		}
		children = append(children,
			diam.NewAVP(avp.GrantedServiceUnit, avp.Mbit, 0, &diam.GroupedAVP{
				AVP: []*diam.AVP{
					diam.NewAVP(avp.CCTotalOctets, avp.Mbit, 0, datatype.Unsigned64(grantedTotalOctets)),
				},
			}),
			diam.NewAVP(avp.ResultCode, avp.Mbit, 0, datatype.Unsigned32(2001)),
		)
		a.NewAVP(avp.MultipleServicesCreditControl, avp.Mbit, 0, &diam.GroupedAVP{AVP: children})
	}
	if _, err := a.WriteTo(c); err != nil {
		log.Printf("dict: write: %v", err)
	}
}

func collectMSCC(m *diam.Message) [][]*diam.AVP {
	var out [][]*diam.AVP
	for _, a := range m.AVP {
		if a.Code == avp.MultipleServicesCreditControl {
			if g, ok := a.Data.(*diam.GroupedAVP); ok {
				out = append(out, g.AVP)
			}
		}
	}
	return out
}

func findChild(avps []*diam.AVP, code uint32) (*diam.AVP, bool) {
	for _, a := range avps {
		if a.Code == code {
			return a, true
		}
	}
	return nil, false
}

func logMemStats(d time.Duration) {
	var prev runtime.MemStats
	runtime.ReadMemStats(&prev)
	for range time.Tick(d) {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		log.Printf("memstats: HeapAlloc=%dKiB Mallocs/s=%.0f NumGC=%d PauseTotal=%dms",
			ms.HeapAlloc/1024,
			float64(ms.Mallocs-prev.Mallocs)/d.Seconds(),
			ms.NumGC,
			ms.PauseTotalNs/1e6,
		)
		prev = ms
	}
}
