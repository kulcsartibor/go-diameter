// Copyright 2013-2026 go-diameter authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// Command gyspike is the Go counterpart of the Rust rust/ingress-spike: a
// host-CPU Diameter Gy throughput harness that measures the software model
// (real TCP + framing + the Go gycodec static parse & serialize + a
// session-map op + a stub rater) over loopback. It exists to give a fair
// Go-on-Linux baseline against the Rust blocking-socket spike, using the
// SAME traffic shape (batched-window pipelining), the SAME golden fixtures,
// and idiomatic Go networking (goroutine per connection over the runtime
// netpoller).
//
// This mirrors ingress-spike deliberately; see rust/ingress-spike for the
// design notes and caveats (loopback, shared cores, not a capacity claim).
//
// Usage:
//
//	gyspike --role both --conns 16 --window 128 --secs 4 --warmup 1 --fixtures /fixtures
package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"math/bits"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fiorix/go-diameter/v4/staticodec"
	"github.com/fiorix/go-diameter/v4/staticodec/gy"
)

const sessionsPerConn = 4096

var (
	role        = flag.String("role", "both", "server | client | both")
	addr        = flag.String("addr", "127.0.0.1:3868", "listen/connect address")
	conns       = flag.Int("conns", 16, "client connections")
	window      = flag.Int("window", 128, "pipelining depth per connection")
	secs        = flag.Int("secs", 4, "measurement seconds")
	warmup      = flag.Int("warmup", 1, "warmup seconds")
	fixturesDir = flag.String("fixtures", "bench/fixtures", "directory with .bin fixtures")
)

var (
	ccrTemplate []byte // ccr_u_3mscc.bin
	ccaTemplate []byte // cca_u_mscc_resultcodes.bin
)

func main() {
	flag.Parse()
	ccrTemplate = mustReadFixture("ccr_u_3mscc")
	ccaTemplate = mustReadFixture("cca_u_mscc_resultcodes")

	switch *role {
	case "server":
		runServer()
	case "client":
		runClient(*addr)
	case "both":
		ln := mustListen("127.0.0.1:0")
		fmt.Printf("gyspike: server on %s (loopback), %d conns, window %d, %ds measure after %ds warmup\n",
			ln.Addr(), *conns, *window, *secs, *warmup)
		go acceptLoop(ln)
		runClient(ln.Addr().String())
	default:
		log.Fatalf("unknown --role %q (server|client|both)", *role)
	}
}

func mustReadFixture(name string) []byte {
	b, err := os.ReadFile(filepath.Join(*fixturesDir, name+".bin"))
	if err != nil {
		log.Fatalf("fixture %s: %v", name, err)
	}
	return b
}

func mustListen(a string) net.Listener {
	ln, err := net.Listen("tcp", a)
	if err != nil {
		log.Fatal(err)
	}
	return ln
}

// ---- server -------------------------------------------------------------

func runServer() {
	ln := mustListen(*addr)
	fmt.Printf("gyspike server listening on %s\n", *addr)
	acceptLoop(ln)
}

func acceptLoop(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go serveConn(c)
	}
}

// serveConn reads CCRs, parses each with a reused gy.CCR, does a
// session-map op, and writes a CCA built from the reused template with the
// request's correlation ids echoed. One write per response, matching the
// Rust blocking baseline.
func serveConn(c net.Conn) {
	defer c.Close()
	if tc, ok := c.(*net.TCPConn); ok {
		tc.SetNoDelay(true)
	}
	r := bufio.NewReaderSize(c, 64*1024)

	var ccr gy.CCR
	var cca gy.CCA
	if err := cca.ParseFrom(ccaTemplate); err != nil {
		log.Printf("server: CCA template parse: %v", err)
		return
	}
	sessions := make(map[uint32]uint64, sessionsPerConn)
	inbuf := make([]byte, 0, 2048)
	outbuf := make([]byte, 0, 2048)

	for {
		msg, err := readMessage(r, &inbuf)
		if err != nil {
			return
		}
		if err := ccr.ParseFrom(msg); err != nil {
			continue // malformed: skip, never panic
		}
		hbh, e2e := ccr.Hdr.HopByHopID, ccr.Hdr.EndToEndID
		key := hbh % sessionsPerConn
		sessions[key]++

		cca.Hdr.HopByHopID = hbh
		cca.Hdr.EndToEndID = e2e
		outbuf = cca.AppendTo(outbuf[:0])
		if _, err := c.Write(outbuf); err != nil {
			return
		}
	}
}

// ---- client -------------------------------------------------------------

func runClient(target string) {
	start := time.Now()
	warmupEnd := start.Add(time.Duration(*warmup) * time.Second)
	measureEnd := warmupEnd.Add(time.Duration(*secs) * time.Second)

	var wg sync.WaitGroup
	results := make([]connResult, *conns)
	for i := 0; i < *conns; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			results[id] = clientConn(uint32(id), target, warmupEnd, measureEnd)
		}(i)
	}
	wg.Wait()

	var total uint64
	hist := newHistogram()
	measureStart := warmupEnd
	for _, r := range results {
		total += r.count
		hist.merge(&r.hist)
		if r.measureStart.Before(measureStart) {
			measureStart = r.measureStart
		}
	}
	elapsed := measureEnd.Sub(measureStart).Seconds()
	tps := float64(total) / elapsed
	fmt.Printf("\n=== gyspike (loopback, Go netpoller, gycodec) ===\n"+
		"conns=%d window=%d measured=%.2fs\n"+
		"transactions=%d  TPS=%.0f\n"+
		"latency (per-request, batch-approx): p50=%s p95=%s p99=%s p999=%s\n",
		*conns, *window, elapsed, total, tps,
		fmtNS(hist.percentile(0.50)), fmtNS(hist.percentile(0.95)),
		fmtNS(hist.percentile(0.99)), fmtNS(hist.percentile(0.999)))
}

type connResult struct {
	count        uint64
	hist         histogram
	measureStart time.Time
}

// clientConn batches `window` CCRs (unique hop-by-hop each), writes them in
// one write, then reads `window` CCAs. Mirrors the Rust client exactly.
func clientConn(id uint32, target string, warmupEnd, measureEnd time.Time) connResult {
	c, err := net.Dial("tcp", target)
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if tc, ok := c.(*net.TCPConn); ok {
		tc.SetNoDelay(true)
	}
	r := bufio.NewReaderSize(c, 64*1024)

	res := connResult{hist: newHistogram(), measureStart: warmupEnd}
	counting := false
	hbh := id << 24 // disjoint hop-by-hop ranges per connection
	batch := make([]byte, 0, *window*len(ccrTemplate))
	scratch := make([]byte, 0, 2048)

	for {
		now := time.Now()
		if now.After(measureEnd) {
			break
		}
		if !counting && !now.Before(warmupEnd) {
			counting = true
			res.measureStart = time.Now()
		}

		batch = batch[:0]
		for i := 0; i < *window; i++ {
			base := len(batch)
			batch = append(batch, ccrTemplate...)
			binary.BigEndian.PutUint32(batch[base+12:base+16], hbh)
			binary.BigEndian.PutUint32(batch[base+16:base+20], hbh)
			hbh++
		}

		t0 := time.Now()
		if _, err := c.Write(batch); err != nil {
			break
		}
		ok := true
		for i := 0; i < *window; i++ {
			if _, err := readMessage(r, &scratch); err != nil {
				ok = false
				break
			}
		}
		if !ok {
			break
		}
		if counting {
			perNS := uint64(time.Since(t0).Nanoseconds()) / uint64(*window)
			if perNS == 0 {
				perNS = 1
			}
			for i := 0; i < *window; i++ {
				res.hist.record(perNS)
			}
			res.count += uint64(*window)
		}
	}
	return res
}

// ---- framing ------------------------------------------------------------

// readMessage reads one complete Diameter message into *buf and returns the
// slice. The 3-byte length field at bytes[1:4] gives the total size.
func readMessage(r *bufio.Reader, buf *[]byte) ([]byte, error) {
	b := (*buf)[:0]
	if cap(b) < staticodec.HeaderLength {
		b = make([]byte, staticodec.HeaderLength)
	} else {
		b = b[:staticodec.HeaderLength]
	}
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, err
	}
	length := int(b[1])<<16 | int(b[2])<<8 | int(b[3])
	if length < staticodec.HeaderLength {
		return nil, fmt.Errorf("bad message length %d", length)
	}
	if cap(b) < length {
		nb := make([]byte, length)
		copy(nb, b)
		b = nb
	} else {
		b = b[:length]
	}
	if _, err := io.ReadFull(r, b[staticodec.HeaderLength:]); err != nil {
		return nil, err
	}
	*buf = b
	return b, nil
}

// ---- histogram (dependency-free, log2-bucketed) -------------------------

type histogram struct {
	buckets [64]uint64
	total   uint64
}

func newHistogram() histogram { return histogram{} }

func (h *histogram) record(nanos uint64) {
	if nanos == 0 {
		nanos = 1
	}
	idx := 63 - bits.LeadingZeros64(nanos)
	h.buckets[idx]++
	h.total++
}

func (h *histogram) merge(o *histogram) {
	for i := range h.buckets {
		h.buckets[i] += o.buckets[i]
	}
	h.total += o.total
}

func (h *histogram) percentile(p float64) uint64 {
	if h.total == 0 {
		return 0
	}
	target := uint64(float64(h.total) * p)
	var cum uint64
	for i, c := range h.buckets {
		cum += c
		if cum >= target {
			return uint64(1) << uint(i)
		}
	}
	return uint64(1) << 63
}

func fmtNS(ns uint64) string {
	switch {
	case ns >= 1_000_000:
		return fmt.Sprintf("%.1fms", float64(ns)/1e6)
	case ns >= 1_000:
		return fmt.Sprintf("%.1fµs", float64(ns)/1e3)
	default:
		return fmt.Sprintf("%dns", ns)
	}
}
