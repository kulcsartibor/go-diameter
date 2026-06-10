// Copyright 2013-2026 go-diameter authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

//go:build gycodec_poison

package gycodec

import (
	"bytes"
	"testing"
)

// TestPoisonClone verifies the §2.5 ownership contract mechanically:
// after the read buffer is poisoned (simulating pool reuse), a Clone()
// taken before poisoning is unaffected, while a retained sub-slice
// reads 0xDE — the loud failure mode poison mode exists to provide.
func TestPoisonClone(t *testing.T) {
	fix := loadFixture(t, "ccr_u_3mscc")
	buf := append([]byte(nil), fix...)

	var m CCR
	if err := m.ParseFrom(buf); err != nil {
		t.Fatalf("ParseFrom: %v", err)
	}

	clone := m.Clone()
	retained := m.SessionID // deliberate ownership violation

	Poison(buf) // simulate the pool recycling the read buffer

	// The clone owns its memory: still equal to a fresh parse.
	var fresh CCR
	if err := fresh.ParseFrom(fix); err != nil {
		t.Fatalf("fresh ParseFrom: %v", err)
	}
	if !bytes.Equal(clone.SessionID, fresh.SessionID) {
		t.Errorf("clone SessionID corrupted by poisoning: %q", clone.SessionID)
	}
	if len(clone.MSCC) != len(fresh.MSCC) {
		t.Fatalf("clone MSCC count: %d vs %d", len(clone.MSCC), len(fresh.MSCC))
	}
	for i := range clone.MSCC {
		if clone.MSCC[i].RatingGroup != fresh.MSCC[i].RatingGroup {
			t.Errorf("clone MSCC[%d].RatingGroup corrupted", i)
		}
	}

	// The retained slice now reads poison — documenting the failure mode.
	for _, b := range retained {
		if b != 0xDE {
			t.Fatalf("retained slice expected to read poison 0xDE, got %#x", b)
		}
	}
}

// TestPoisonRoundTripAfterClone makes sure Clone() output serializes
// identically to the original message even after the source buffer dies.
func TestPoisonRoundTripAfterClone(t *testing.T) {
	for _, name := range fixtureNames {
		fix := loadFixture(t, name)
		buf := append([]byte(nil), fix...)
		m := newMessageFor(name)
		if err := m.ParseFrom(buf); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var out []byte
		switch v := m.(type) {
		case *CCR:
			c := v.Clone()
			Poison(buf)
			out = c.AppendTo(nil)
		case *CCA:
			c := v.Clone()
			Poison(buf)
			out = c.AppendTo(nil)
		}
		if !bytes.Equal(fix, out) {
			t.Errorf("%s: clone round trip not byte-identical after poison", name)
		}
	}
}
