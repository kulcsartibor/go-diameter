// Copyright 2013-2026 go-diameter authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package sy

import (
	"bytes"
	"testing"

	"github.com/fiorix/go-diameter/v4/staticodec"
)

// TestSLRRoundTrip builds a Spending-Limit-Request with the static codec,
// serializes it, parses it back, and checks the round trip is byte-identical
// and the fields decode correctly. No external fixtures: the codec's own
// AppendTo is the reference for its ParseFrom.
func TestSLRRoundTrip(t *testing.T) {
	var in SLR
	in.Hdr.CommandFlags = staticodec.FlagRequest
	in.Hdr.HopByHopID = 0x11112222
	in.Hdr.EndToEndID = 0x33334444
	in.SessionID = []byte("sy.example.com;1;1")
	in.AuthApplicationID, in.HasAuthApplicationID = 16777302, true
	in.OriginHost = []byte("pcrf.example.com")
	in.OriginRealm = []byte("example.com")
	in.DestinationRealm = []byte("ocs.example.com")
	in.SLRequestType, in.HasSLRequestType = SLRequestTypeInitialRequest, true
	in.SubscriptionID = append(in.SubscriptionID, SubscriptionID{
		Type: SubscriptionIDTypeEndUserImsi, HasType: true,
		Data: []byte("262011234567890"),
	})
	in.PolicyCounterIdentifier = append(in.PolicyCounterIdentifier,
		[]byte("counter-A"), []byte("counter-B"))

	wire := in.AppendTo(nil)

	var out SLR
	if err := out.ParseFrom(wire); err != nil {
		t.Fatalf("ParseFrom: %v", err)
	}
	if got := out.AppendTo(nil); !bytes.Equal(got, wire) {
		t.Fatal("SLR round trip not byte-identical")
	}
	if out.Hdr.CommandCode != 8388635 || out.Hdr.ApplicationID != 16777302 {
		t.Errorf("header: code=%d app=%d", out.Hdr.CommandCode, out.Hdr.ApplicationID)
	}
	if out.SLRequestType != SLRequestTypeInitialRequest {
		t.Errorf("SL-Request-Type: got %d", out.SLRequestType)
	}
	if len(out.SubscriptionID) != 1 || !bytes.Equal(out.SubscriptionID[0].Data, []byte("262011234567890")) {
		t.Errorf("Subscription-Id not decoded: %+v", out.SubscriptionID)
	}
	if len(out.PolicyCounterIdentifier) != 2 ||
		!bytes.Equal(out.PolicyCounterIdentifier[0], []byte("counter-A")) ||
		!bytes.Equal(out.PolicyCounterIdentifier[1], []byte("counter-B")) {
		t.Errorf("Policy-Counter-Identifier not decoded: %v", out.PolicyCounterIdentifier)
	}
}

// TestSLANestedRoundTrip exercises the deeply nested SLA answer:
// Policy-Counter-Status-Report containing Pending-Policy-Counter-Information.
func TestSLANestedRoundTrip(t *testing.T) {
	var in SLA
	in.SessionID = []byte("sy.example.com;1;1")
	in.AuthApplicationID, in.HasAuthApplicationID = 16777302, true
	in.OriginHost = []byte("ocs.example.com")
	in.OriginRealm = []byte("example.com")
	in.ResultCode, in.HasResultCode = 2001, true
	in.PolicyCounterStatusReport = append(in.PolicyCounterStatusReport, PolicyCounterStatusReport{
		PolicyCounterIdentifier: []byte("counter-A"),
		PolicyCounterStatus:     []byte("active"),
		PendingPolicyCounterInformation: []PendingPolicyCounterInformation{{
			PolicyCounterStatus:               []byte("pending"),
			PendingPolicyCounterChangeTime:    0x60000000,
			HasPendingPolicyCounterChangeTime: true,
		}},
	})

	wire := in.AppendTo(nil)
	var out SLA
	if err := out.ParseFrom(wire); err != nil {
		t.Fatalf("ParseFrom: %v", err)
	}
	if !bytes.Equal(out.AppendTo(nil), wire) {
		t.Fatal("SLA nested round trip not byte-identical")
	}
	if len(out.PolicyCounterStatusReport) != 1 {
		t.Fatalf("PCSR count: %d", len(out.PolicyCounterStatusReport))
	}
	r := &out.PolicyCounterStatusReport[0]
	if !bytes.Equal(r.PolicyCounterIdentifier, []byte("counter-A")) {
		t.Errorf("PCSR identifier: %q", r.PolicyCounterIdentifier)
	}
	if len(r.PendingPolicyCounterInformation) != 1 ||
		r.PendingPolicyCounterInformation[0].PendingPolicyCounterChangeTime != 0x60000000 {
		t.Errorf("nested Pending-Policy-Counter-Information not decoded: %+v", r.PendingPolicyCounterInformation)
	}
}

// TestSyRejectsWrongCommand: SLR must reject an SNR-coded message.
func TestSyRejectsWrongCommand(t *testing.T) {
	var snr SNR
	snr.SessionID = []byte("s;1;1")
	snr.OriginHost = []byte("h")
	snr.OriginRealm = []byte("r")
	wire := snr.AppendTo(nil)

	var slr SLR
	if err := slr.ParseFrom(wire); err != staticodec.ErrUnexpectedCommand {
		t.Fatalf("expected ErrUnexpectedCommand, got %v", err)
	}
}

// TestSyTruncationNeverPanics feeds truncations of a valid SLR through
// ParseFrom; it must error, never panic.
func TestSyTruncationNeverPanics(t *testing.T) {
	var in SLR
	in.SessionID = []byte("s;1;1")
	in.OriginHost = []byte("h")
	in.OriginRealm = []byte("r")
	in.DestinationRealm = []byte("d")
	wire := in.AppendTo(nil)
	for l := 0; l < len(wire); l++ {
		if l >= 20 && l%4 != 0 {
			continue
		}
		var out SLR
		if err := out.ParseFrom(wire[:l]); err == nil {
			t.Fatalf("truncation to %d bytes: expected error", l)
		}
	}
}
