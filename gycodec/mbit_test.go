// Copyright 2013-2026 go-diameter authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package gycodec

import (
	"bytes"
	"testing"
)

// TestMbitSurfacing implements the codec-design.md §5.4 conformance
// table: an unknown AVP with the M-bit set is surfaced in
// UnsupportedMandatory (and preserved in Other); a non-mandatory unknown
// is preserved in Other only; both survive the round trip intact.
func TestMbitSurfacing(t *testing.T) {
	fix := loadFixture(t, "ccr_unknown_vendor_avps")
	var m CCR
	if err := m.ParseFrom(fix); err != nil {
		t.Fatalf("ParseFrom: %v", err)
	}

	if len(m.Other) != 2 {
		t.Fatalf("Other: got %d entries, want 2", len(m.Other))
	}
	if m.Other[0].Code != 999001 || m.Other[0].VendorID != 99999 {
		t.Errorf("Other[0]: got code %d vendor %d, want 999001/99999", m.Other[0].Code, m.Other[0].VendorID)
	}
	if m.Other[0].Flags&FlagMandatory == 0 {
		t.Error("Other[0]: M-bit lost")
	}
	if m.Other[1].Code != 999002 || m.Other[1].Flags&FlagMandatory != 0 {
		t.Errorf("Other[1]: got code %d flags %#x, want 999002 without M-bit", m.Other[1].Code, m.Other[1].Flags)
	}

	if len(m.UnsupportedMandatory) != 1 {
		t.Fatalf("UnsupportedMandatory: got %d entries, want 1", len(m.UnsupportedMandatory))
	}
	if m.UnsupportedMandatory[0].Code != 999001 {
		t.Errorf("UnsupportedMandatory[0]: got code %d, want 999001", m.UnsupportedMandatory[0].Code)
	}
	if !bytes.Equal(m.UnsupportedMandatory[0].Data, []byte("\xde\xad\xbe\xef\x01")) {
		t.Errorf("UnsupportedMandatory[0]: data %x", m.UnsupportedMandatory[0].Data)
	}
}

// TestMbitNested verifies unknown-AVP surfacing inside a grouped AVP:
// a synthetic MSCC carrying an unknown mandatory AVP.
func TestMbitNested(t *testing.T) {
	// Build the message with the static codec itself: MSCC with
	// Rating-Group plus a hand-rolled unknown AVP in Other.
	var in CCR
	in.Hdr.CommandFlags = FlagRequest
	in.Hdr.HopByHopID = 1
	in.Hdr.EndToEndID = 2
	in.SessionID = []byte("s;1")
	in.MSCC = append(in.MSCC, MSCC{
		RatingGroup:    7,
		HasRatingGroup: true,
		Other: []RawAVP{{
			Code: 888001, VendorID: 4242, Flags: FlagVendor | FlagMandatory,
			Data: []byte{1, 2, 3},
		}},
	})
	wire := in.AppendTo(nil)

	var out CCR
	if err := out.ParseFrom(wire); err != nil {
		t.Fatalf("ParseFrom: %v", err)
	}
	if len(out.MSCC) != 1 {
		t.Fatalf("MSCC count: %d", len(out.MSCC))
	}
	c := &out.MSCC[0]
	if len(c.Other) != 1 || len(c.UnsupportedMandatory) != 1 {
		t.Fatalf("nested unknown not surfaced: other=%d um=%d", len(c.Other), len(c.UnsupportedMandatory))
	}
	r := c.UnsupportedMandatory[0]
	if r.Code != 888001 || r.VendorID != 4242 || !bytes.Equal(r.Data, []byte{1, 2, 3}) {
		t.Errorf("nested raw AVP mangled: %+v", r)
	}
	// And the round trip of the synthetic message is byte-identical.
	if !bytes.Equal(wire, out.AppendTo(nil)) {
		t.Error("synthetic nested-unknown round trip not byte-identical")
	}
}
