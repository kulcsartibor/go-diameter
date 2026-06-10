// Copyright 2013-2026 go-diameter authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// Command gyclient is the Harness B load generator (codec-design.md §3
// Phase 3). It drives session-shaped Gy traffic (CCR-I → CCR-U×N →
// CCR-T) in a closed loop and reports a latency histogram and achieved
// TPS, using either the static gycodec (raw fast path) or the stock
// dictionary codec — matched to whichever the server runs.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/avp"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
	"github.com/fiorix/go-diameter/v4/diam/dict"
	"github.com/fiorix/go-diameter/v4/diam/sm"
	"github.com/fiorix/go-diameter/v4/gycodec"
)

func main() {
	addr := flag.String("addr", "localhost:3868", "server address ip:port")
	codec := flag.String("codec", "static", "codec: static | dict (match the server)")
	host := flag.String("host", "gyclient", "Origin-Host")
	realm := flag.String("realm", "go-diameter", "Origin-Realm")
	concurrency := flag.Int("concurrency", 16, "number of closed-loop workers")
	updates := flag.Int("updates", 4, "CCR-U messages per session")
	mscc := flag.Int("mscc", 1, "MSCC count per request")
	duration := flag.Duration("duration", 60*time.Second, "steady-state run duration")
	warmup := flag.Duration("warmup", 5*time.Second, "warmup before measuring")
	flag.Parse()

	if *codec != "static" && *codec != "dict" {
		log.Fatalf("invalid -codec %q: want static or dict", *codec)
	}

	cfg := &sm.Settings{
		OriginHost:       datatype.DiameterIdentity(*host),
		OriginRealm:      datatype.DiameterIdentity(*realm),
		VendorID:         13,
		ProductName:      "gyclient",
		FirmwareRevision: 1,
	}
	mux := sm.New(cfg)
	cli := &sm.Client{
		Dict:           dict.Default,
		Handler:        mux,
		EnableWatchdog: false,
		AuthApplicationID: []*diam.AVP{
			diam.NewAVP(avp.AuthApplicationID, avp.Mbit, 0, datatype.Unsigned32(4)),
		},
	}

	p := &pending{}
	conn := connect(cli, mux, *codec, *addr, p)

	h := newHistogram()
	run := &runner{
		conn:    conn,
		codec:   *codec,
		updates: *updates,
		mscc:    *mscc,
		pending: p,
		host:    *host,
		realm:   *realm,
		hist:    h, // stable pointer; observations gated by recording flag
	}

	log.Printf("gyclient: codec=%s concurrency=%d updates/session=%d mscc=%d warmup=%s duration=%s (LOOPBACK if same host)",
		*codec, *concurrency, *updates, *mscc, *warmup, *duration)

	// Warmup: run sessions without recording.
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func(id int) { defer wg.Done(); run.loop(id, stop, nil) }(i)
	}
	time.Sleep(*warmup)

	// Measure: reset the session counter and start recording into the
	// (already-installed) histogram.
	var sessions int64
	atomic.StoreInt64(&run.sessions, 0)
	start := time.Now()
	run.recording.Store(true)
	time.Sleep(*duration)
	elapsed := time.Since(start)
	close(stop)
	wg.Wait()
	sessions = atomic.LoadInt64(&run.sessions)

	reqs := h.count()
	tps := float64(reqs) / elapsed.Seconds()
	p50 := h.quantile(0.50)
	p95 := h.quantile(0.95)
	p99 := h.quantile(0.99)

	fmt.Printf("\nRESULT codec=%s conc=%d mscc=%d sessions=%d requests=%d elapsed=%.1fs TPS=%.0f p50=%.0fµs p95=%.0fµs p99=%.0fµs\n",
		*codec, *concurrency, *mscc, sessions, reqs, elapsed.Seconds(), tps,
		float64(p50.Microseconds()), float64(p95.Microseconds()), float64(p99.Microseconds()))

	if reqs == 0 {
		os.Exit(1)
	}
}

// pending correlates answers to requests by Hop-by-Hop ID.
type pending struct {
	m   sync.Map // uint32 -> chan time.Time (close/send on answer)
	seq uint32
}

func (p *pending) next() uint32 { return atomic.AddUint32(&p.seq, 1) }

func (p *pending) register(hbh uint32) chan struct{} {
	ch := make(chan struct{}, 1)
	p.m.Store(hbh, ch)
	return ch
}

func (p *pending) complete(hbh uint32) {
	if v, ok := p.m.LoadAndDelete(hbh); ok {
		close(v.(chan struct{}))
	}
}

// connect dials the server and installs the answer path for the chosen
// codec, then returns the live connection.
func connect(cli *sm.Client, mux *sm.StateMachine, codec, addr string, p *pending) diam.Conn {
	var conn diam.Conn
	var err error
	switch codec {
	case "static":
		srv := &diam.Server{
			RawHandlers: map[diam.RawKey]diam.RawHandler{
				{AppID: 4, Code: 272}: func(c diam.Conn, hdr *diam.Header, msg []byte) {
					p.complete(hdr.HopByHopID)
				},
			},
		}
		conn, err = cli.DialServer(srv, addr, 5*time.Second)
	case "dict":
		mux.HandleIdx(
			diam.CommandIndex{AppID: 4, Code: 272, Request: false},
			diam.HandlerFunc(func(c diam.Conn, m *diam.Message) {
				p.complete(m.Header.HopByHopID)
			}),
		)
		conn, err = cli.Dial(addr)
	}
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	return conn
}

type runner struct {
	conn      diam.Conn
	codec     string
	updates   int
	mscc      int
	host      string
	realm     string
	pending   *pending
	recording atomicBool
	hist      *histogram
	sessions  int64

	mu sync.Mutex // serializes Write on the shared conn
}

// loop runs closed-loop sessions until stop is closed.
func (r *runner) loop(worker int, stop <-chan struct{}, _ *struct{}) {
	num := 0
	for {
		select {
		case <-stop:
			return
		default:
		}
		sessionID := fmt.Sprintf("%s;%d;%d", r.host, worker, num)
		num++
		// CCR-I
		r.request(sessionID, 1, 0)
		// CCR-U × N
		for u := 1; u <= r.updates; u++ {
			select {
			case <-stop:
				return
			default:
			}
			r.request(sessionID, 2, uint32(u))
		}
		// CCR-T
		r.request(sessionID, 3, uint32(r.updates+1))
		if r.recording.Load() {
			atomic.AddInt64(&r.sessions, 1)
		}
	}
}

// request sends one CCR and waits for its CCA, recording latency when in
// the measurement window.
func (r *runner) request(sessionID string, reqType, reqNum uint32) {
	hbh := r.pending.next()
	ch := r.pending.register(hbh)
	var wire []byte
	if r.codec == "static" {
		wire = r.buildStatic(sessionID, reqType, reqNum, hbh)
	} else {
		wire = r.buildDict(sessionID, reqType, reqNum, hbh)
	}

	t0 := time.Now()
	r.mu.Lock()
	_, err := r.conn.Write(wire)
	r.mu.Unlock()
	if err != nil {
		log.Printf("write: %v", err)
		return
	}
	select {
	case <-ch:
		if r.recording.Load() {
			r.hist.observe(time.Since(t0))
		}
	case <-time.After(5 * time.Second):
		r.pending.complete(hbh) // clean up
		log.Printf("timeout waiting for CCA hbh=%d", hbh)
	}
}

func (r *runner) buildStatic(sessionID string, reqType, reqNum, hbh uint32) []byte {
	var ccr gycodec.CCR
	ccr.Hdr.CommandFlags = diam.RequestFlag
	ccr.Hdr.HopByHopID = hbh
	ccr.Hdr.EndToEndID = hbh
	ccr.SessionID = []byte(sessionID)
	ccr.OriginHost = []byte(r.host)
	ccr.OriginRealm = []byte(r.realm)
	ccr.DestinationRealm = []byte("go-diameter")
	ccr.AuthApplicationID = 4
	ccr.HasAuthApplicationID = true
	ccr.ServiceContextID = []byte("32251@3gpp.org")
	ccr.CCRequestType = int32(reqType)
	ccr.HasCCRequestType = true
	ccr.CCRequestNumber = reqNum
	ccr.HasCCRequestNumber = true
	for i := 0; i < r.mscc; i++ {
		ccr.MSCC = append(ccr.MSCC, gycodec.MSCC{
			RatingGroup: uint32(10 + i), HasRatingGroup: true,
			ServiceIdentifier: uint32(100 + i), HasServiceIdentifier: true,
			HasRequested: true,
		})
	}
	return ccr.AppendTo(nil)
}

func (r *runner) buildDict(sessionID string, reqType, reqNum, hbh uint32) []byte {
	m := diam.NewMessage(diam.CreditControl, diam.RequestFlag,
		diam.CHARGING_CONTROL_APP_ID, hbh, hbh, dict.Default)
	m.NewAVP(avp.SessionID, avp.Mbit, 0, datatype.UTF8String(sessionID))
	m.NewAVP(avp.OriginHost, avp.Mbit, 0, datatype.DiameterIdentity(r.host))
	m.NewAVP(avp.OriginRealm, avp.Mbit, 0, datatype.DiameterIdentity(r.realm))
	m.NewAVP(avp.DestinationRealm, avp.Mbit, 0, datatype.DiameterIdentity("go-diameter"))
	m.NewAVP(avp.AuthApplicationID, avp.Mbit, 0, datatype.Unsigned32(4))
	m.NewAVP(avp.ServiceContextID, avp.Mbit, 0, datatype.UTF8String("32251@3gpp.org"))
	m.NewAVP(avp.CCRequestType, avp.Mbit, 0, datatype.Enumerated(int32(reqType)))
	m.NewAVP(avp.CCRequestNumber, avp.Mbit, 0, datatype.Unsigned32(reqNum))
	for i := 0; i < r.mscc; i++ {
		m.NewAVP(avp.MultipleServicesCreditControl, avp.Mbit, 0, &diam.GroupedAVP{
			AVP: []*diam.AVP{
				diam.NewAVP(avp.RatingGroup, avp.Mbit, 0, datatype.Unsigned32(uint32(10+i))),
				diam.NewAVP(avp.ServiceIdentifier, avp.Mbit, 0, datatype.Unsigned32(uint32(100+i))),
				diam.NewAVP(avp.RequestedServiceUnit, avp.Mbit, 0, &diam.GroupedAVP{}),
			},
		})
	}
	b, err := m.Serialize()
	if err != nil {
		log.Fatalf("dict serialize: %v", err)
	}
	return b
}

// --- atomic bool ---

type atomicBool struct{ v int32 }

func (a *atomicBool) Store(b bool) {
	if b {
		atomic.StoreInt32(&a.v, 1)
	} else {
		atomic.StoreInt32(&a.v, 0)
	}
}
func (a *atomicBool) Load() bool { return atomic.LoadInt32(&a.v) == 1 }

// --- latency histogram (stdlib only, log-spaced buckets 1µs..~16s) ---

type histogram struct {
	mu      sync.Mutex
	buckets []int64 // index = bucket, value = count
	total   int64
}

const histBuckets = 48 // 2^48 µs ≈ 3 years; bucket i covers [2^i, 2^(i+1)) µs

func newHistogram() *histogram {
	return &histogram{buckets: make([]int64, histBuckets)}
}

func (h *histogram) observe(d time.Duration) {
	us := d.Microseconds()
	if us < 1 {
		us = 1
	}
	b := 0
	for v := us; v > 1 && b < histBuckets-1; v >>= 1 {
		b++
	}
	h.mu.Lock()
	h.buckets[b]++
	h.total++
	h.mu.Unlock()
}

func (h *histogram) count() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.total
}

// quantile returns the upper bound of the bucket holding the q-th
// percentile — a conservative (rounded-up) latency estimate.
func (h *histogram) quantile(q float64) time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.total == 0 {
		return 0
	}
	target := int64(q * float64(h.total))
	var cum int64
	for b := 0; b < len(h.buckets); b++ {
		cum += h.buckets[b]
		if cum >= target {
			return time.Duration(int64(1)<<uint(b+1)) * time.Microsecond
		}
	}
	return time.Duration(int64(1)<<histBuckets) * time.Microsecond
}

var _ = sort.Ints // reserved for future exact-quantile mode
