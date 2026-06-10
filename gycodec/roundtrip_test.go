// Copyright 2013-2026 go-diameter authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package gycodec

import (
	"bytes"
	"testing"
)

// TestRoundTrip: for every golden fixture, staticParse → staticSerialize
// must be byte-identical to the input (codec-design.md §5.1).
func TestRoundTrip(t *testing.T) {
	for _, name := range fixtureNames {
		t.Run(name, func(t *testing.T) {
			fix := loadFixture(t, name)
			m := newMessageFor(name)
			if err := m.ParseFrom(fix); err != nil {
				t.Fatalf("ParseFrom: %v", err)
			}
			out := m.AppendTo(nil)
			if !bytes.Equal(fix, out) {
				i := 0
				for i < len(fix) && i < len(out) && fix[i] == out[i] {
					i++
				}
				t.Fatalf("round trip not byte-identical: len in=%d out=%d, first diff at %d",
					len(fix), len(out), i)
			}
		})
	}
}

// TestRoundTripReuse: parsing different fixtures through ONE reused
// message struct must not leak state between parses (the pooled-struct
// usage pattern of the OCS handler).
func TestRoundTripReuse(t *testing.T) {
	var ccr CCR
	var cca CCA
	// Two passes over all fixtures through the same structs.
	for pass := 0; pass < 2; pass++ {
		for _, name := range fixtureNames {
			fix := loadFixture(t, name)
			var out []byte
			if isRequest(name) {
				if err := ccr.ParseFrom(fix); err != nil {
					t.Fatalf("pass %d %s: ParseFrom: %v", pass, name, err)
				}
				out = ccr.AppendTo(nil)
			} else {
				if err := cca.ParseFrom(fix); err != nil {
					t.Fatalf("pass %d %s: ParseFrom: %v", pass, name, err)
				}
				out = cca.AppendTo(nil)
			}
			if !bytes.Equal(fix, out) {
				t.Fatalf("pass %d %s: reused-struct round trip not byte-identical", pass, name)
			}
		}
	}
}
