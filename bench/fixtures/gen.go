// Copyright 2013-2026 go-diameter authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

//go:build ignore

// Command gen builds the golden Gy message fixtures with the dictionary
// codec, per codec-design.md Phase 1. The fixtures double as equivalence
// references for the static codec (Phase 2).
//
// Usage (from the repo root):
//
//	go run bench/fixtures/gen.go
//
// Output is deterministic: explicit HopByHop/EndToEnd IDs (NewMessage
// randomizes them when zero) and fixed timestamps/identifiers. Rerunning
// must be byte-stable; CI may diff the output.
package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/avp"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
	"github.com/fiorix/go-diameter/v4/diam/dict"
)

const (
	vendor3GPP = 10415

	// Deterministic header IDs; NewMessage randomizes when given 0.
	hopByHopBase = 0x10000001
	endToEndBase = 0x20000001
)

// Fixed identities and timestamp shared by all fixtures.
var (
	sessionID   = datatype.UTF8String("gy.test.example.com;1;1")
	originHost  = datatype.DiameterIdentity("gy-client.test.example.com")
	originRealm = datatype.DiameterIdentity("test.example.com")
	destRealm   = datatype.DiameterIdentity("ocs.example.com")
	destHost    = datatype.DiameterIdentity("ocs1.ocs.example.com")
	serviceCtx  = datatype.UTF8String("32251@3gpp.org")
	imsi        = datatype.UTF8String("262011234567890")
	eventTime   = datatype.Time(time.Unix(1748736000, 0).UTC()) // 2025-06-01T00:00:00Z
)

func main() {
	out := flag.String("out", "bench/fixtures", "output directory")
	flag.Parse()

	fixtures := []struct {
		name  string
		build func(seq uint32) *diam.Message
	}{
		{"ccr_i_1mscc_rsu", buildCCRInitial},
		{"ccr_u_1mscc", func(s uint32) *diam.Message { return buildCCRUpdate(s, 1) }},
		{"ccr_u_3mscc", func(s uint32) *diam.Message { return buildCCRUpdate(s, 3) }},
		{"ccr_u_5mscc", func(s uint32) *diam.Message { return buildCCRUpdate(s, 5) }},
		{"ccr_u_trigger_ttc", buildCCRUpdateTriggerTTC},
		{"ccr_t_final_usu", buildCCRTerminate},
		{"cca_i_gsu_validity", buildCCAInitial},
		{"cca_u_mscc_resultcodes", buildCCAUpdateResultCodes},
		{"cca_fui_terminate", buildCCAFinalUnit},
		{"ccr_unknown_vendor_avps", buildCCRUnknownAVPs},
	}

	for i, f := range fixtures {
		m := f.build(uint32(i))
		b, err := m.Serialize()
		if err != nil {
			log.Fatalf("%s: serialize: %v", f.name, err)
		}
		bin := filepath.Join(*out, f.name+".bin")
		txt := filepath.Join(*out, f.name+".txt")
		if err := os.WriteFile(bin, b, 0644); err != nil {
			log.Fatalf("%s: %v", bin, err)
		}
		if err := os.WriteFile(txt, []byte(hex.Dump(b)), 0644); err != nil {
			log.Fatalf("%s: %v", txt, err)
		}
		fmt.Printf("%-28s %4d bytes\n", f.name, len(b))
	}
}

// newCCR creates a CCR skeleton with the AVPs common to every request.
func newCCR(seq, reqType, reqNum uint32) *diam.Message {
	m := diam.NewMessage(diam.CreditControl, diam.RequestFlag,
		diam.CHARGING_CONTROL_APP_ID, hopByHopBase+seq, endToEndBase+seq, dict.Default)
	m.NewAVP(avp.SessionID, avp.Mbit, 0, sessionID)
	m.NewAVP(avp.OriginHost, avp.Mbit, 0, originHost)
	m.NewAVP(avp.OriginRealm, avp.Mbit, 0, originRealm)
	m.NewAVP(avp.DestinationRealm, avp.Mbit, 0, destRealm)
	m.NewAVP(avp.AuthApplicationID, avp.Mbit, 0, datatype.Unsigned32(4))
	m.NewAVP(avp.ServiceContextID, avp.Mbit, 0, serviceCtx)
	m.NewAVP(avp.CCRequestType, avp.Mbit, 0, datatype.Enumerated(reqType))
	m.NewAVP(avp.CCRequestNumber, avp.Mbit, 0, datatype.Unsigned32(reqNum))
	return m
}

// newCCA creates a CCA skeleton.
func newCCA(seq, reqType, reqNum uint32) *diam.Message {
	m := diam.NewMessage(diam.CreditControl, 0,
		diam.CHARGING_CONTROL_APP_ID, hopByHopBase+seq, endToEndBase+seq, dict.Default)
	m.NewAVP(avp.SessionID, avp.Mbit, 0, sessionID)
	m.NewAVP(avp.ResultCode, avp.Mbit, 0, datatype.Unsigned32(2001))
	m.NewAVP(avp.OriginHost, avp.Mbit, 0, datatype.DiameterIdentity("ocs1.ocs.example.com"))
	m.NewAVP(avp.OriginRealm, avp.Mbit, 0, datatype.DiameterIdentity("ocs.example.com"))
	m.NewAVP(avp.AuthApplicationID, avp.Mbit, 0, datatype.Unsigned32(4))
	m.NewAVP(avp.CCRequestType, avp.Mbit, 0, datatype.Enumerated(reqType))
	m.NewAVP(avp.CCRequestNumber, avp.Mbit, 0, datatype.Unsigned32(reqNum))
	return m
}

func subscriptionID() *diam.AVP {
	return diam.NewAVP(avp.SubscriptionID, avp.Mbit, 0, &diam.GroupedAVP{
		AVP: []*diam.AVP{
			diam.NewAVP(avp.SubscriptionIDType, avp.Mbit, 0, datatype.Enumerated(1)), // END_USER_IMSI
			diam.NewAVP(avp.SubscriptionIDData, avp.Mbit, 0, imsi),
		},
	})
}

// usu returns a Used-Service-Unit with the unit type rotated by index:
// octets (total+input+output), time, service-specific units.
func usu(i int) *diam.AVP {
	var units []*diam.AVP
	switch i % 3 {
	case 0:
		units = []*diam.AVP{
			diam.NewAVP(avp.CCTotalOctets, avp.Mbit, 0, datatype.Unsigned64(5_000_000+uint64(i)*100_000)),
			diam.NewAVP(avp.CCInputOctets, avp.Mbit, 0, datatype.Unsigned64(2_000_000+uint64(i)*50_000)),
			diam.NewAVP(avp.CCOutputOctets, avp.Mbit, 0, datatype.Unsigned64(3_000_000+uint64(i)*50_000)),
		}
	case 1:
		units = []*diam.AVP{
			diam.NewAVP(avp.CCTime, avp.Mbit, 0, datatype.Unsigned32(1800+uint32(i)*60)),
		}
	default:
		units = []*diam.AVP{
			diam.NewAVP(avp.CCServiceSpecificUnits, avp.Mbit, 0, datatype.Unsigned64(42+uint64(i))),
		}
	}
	return diam.NewAVP(avp.UsedServiceUnit, avp.Mbit, 0, &diam.GroupedAVP{AVP: units})
}

// mscc returns one Multiple-Services-Credit-Control for a CCR-U:
// Rating-Group, Service-Identifier, empty RSU (quota request), USU.
func mscc(i int) *diam.AVP {
	return diam.NewAVP(avp.MultipleServicesCreditControl, avp.Mbit, 0, &diam.GroupedAVP{
		AVP: []*diam.AVP{
			diam.NewAVP(avp.RatingGroup, avp.Mbit, 0, datatype.Unsigned32(uint32(10+i))),
			diam.NewAVP(avp.ServiceIdentifier, avp.Mbit, 0, datatype.Unsigned32(uint32(100+i))),
			diam.NewAVP(avp.RequestedServiceUnit, avp.Mbit, 0, &diam.GroupedAVP{}),
			usu(i),
		},
	})
}

// buildCCRInitial: session start, Subscription-Id, Service-Information
// with PS-Information, one MSCC with an RSU.
func buildCCRInitial(seq uint32) *diam.Message {
	m := newCCR(seq, 1, 0) // INITIAL_REQUEST
	addAVP(m, subscriptionID())
	addAVP(m, diam.NewAVP(avp.OriginStateID, avp.Mbit, 0, datatype.Unsigned32(1)))
	addAVP(m, diam.NewAVP(avp.EventTimestamp, avp.Mbit, 0, eventTime))
	addAVP(m, diam.NewAVP(avp.MultipleServicesCreditControl, avp.Mbit, 0, &diam.GroupedAVP{
		AVP: []*diam.AVP{
			diam.NewAVP(avp.RatingGroup, avp.Mbit, 0, datatype.Unsigned32(10)),
			diam.NewAVP(avp.ServiceIdentifier, avp.Mbit, 0, datatype.Unsigned32(100)),
			diam.NewAVP(avp.RequestedServiceUnit, avp.Mbit, 0, &diam.GroupedAVP{}),
		},
	}))
	addAVP(m, diam.NewAVP(avp.ServiceInformation, avp.Mbit|avp.Vbit, vendor3GPP, &diam.GroupedAVP{
		AVP: []*diam.AVP{
			diam.NewAVP(avp.PSInformation, avp.Mbit|avp.Vbit, vendor3GPP, &diam.GroupedAVP{
				AVP: []*diam.AVP{
					diam.NewAVP(avp.TGPPChargingID, avp.Vbit, vendor3GPP, datatype.OctetString("\x00\x00\x30\x39")),
					diam.NewAVP(avp.TGPPPDPType, avp.Vbit, vendor3GPP, datatype.Enumerated(0)), // IPv4
					diam.NewAVP(avp.CalledStationID, avp.Mbit, 0, datatype.UTF8String("internet.apn")),
					diam.NewAVP(avp.TGPPRATType, avp.Vbit, vendor3GPP, datatype.OctetString("\x06")), // EUTRAN
				},
			}),
		},
	}))
	return m
}

func buildCCRUpdate(seq uint32, nMSCC int) *diam.Message {
	m := newCCR(seq, 2, 1) // UPDATE_REQUEST
	addAVP(m, subscriptionID())
	addAVP(m, diam.NewAVP(avp.EventTimestamp, avp.Mbit, 0, eventTime))
	for i := 0; i < nMSCC; i++ {
		addAVP(m, mscc(i))
	}
	return m
}

// buildCCRUpdateTriggerTTC: the nasty nested case — MSCC with Trigger
// (Trigger-Type), Tariff-Time-Change, and two USUs with Tariff-Change-Usage
// reporting usage before and after the tariff switch.
func buildCCRUpdateTriggerTTC(seq uint32) *diam.Message {
	m := newCCR(seq, 2, 2) // UPDATE_REQUEST
	addAVP(m, subscriptionID())
	addAVP(m, diam.NewAVP(avp.MultipleServicesCreditControl, avp.Mbit, 0, &diam.GroupedAVP{
		AVP: []*diam.AVP{
			diam.NewAVP(avp.RatingGroup, avp.Mbit, 0, datatype.Unsigned32(10)),
			diam.NewAVP(avp.ServiceIdentifier, avp.Mbit, 0, datatype.Unsigned32(100)),
			diam.NewAVP(avp.RequestedServiceUnit, avp.Mbit, 0, &diam.GroupedAVP{}),
			diam.NewAVP(avp.UsedServiceUnit, avp.Mbit, 0, &diam.GroupedAVP{
				AVP: []*diam.AVP{
					diam.NewAVP(avp.TariffChangeUsage, avp.Mbit, 0, datatype.Enumerated(0)), // UNIT_BEFORE_TARIFF_CHANGE
					diam.NewAVP(avp.CCTotalOctets, avp.Mbit, 0, datatype.Unsigned64(3_000_000)),
				},
			}),
			diam.NewAVP(avp.UsedServiceUnit, avp.Mbit, 0, &diam.GroupedAVP{
				AVP: []*diam.AVP{
					diam.NewAVP(avp.TariffChangeUsage, avp.Mbit, 0, datatype.Enumerated(1)), // UNIT_AFTER_TARIFF_CHANGE
					diam.NewAVP(avp.CCTotalOctets, avp.Mbit, 0, datatype.Unsigned64(1_500_000)),
				},
			}),
			diam.NewAVP(avp.Trigger, avp.Mbit|avp.Vbit, vendor3GPP, &diam.GroupedAVP{
				AVP: []*diam.AVP{
					diam.NewAVP(avp.TriggerType, avp.Mbit|avp.Vbit, vendor3GPP, datatype.Enumerated(5)), // CHANGE_IN_TARIFF_TIME
					diam.NewAVP(avp.TriggerType, avp.Mbit|avp.Vbit, vendor3GPP, datatype.Enumerated(2)), // CHANGEINQOS
				},
			}),
			diam.NewAVP(avp.TariffTimeChange, avp.Mbit, 0, eventTime),
		},
	}))
	return m
}

func buildCCRTerminate(seq uint32) *diam.Message {
	m := newCCR(seq, 3, 3) // TERMINATION_REQUEST
	addAVP(m, subscriptionID())
	addAVP(m, diam.NewAVP(avp.TerminationCause, avp.Mbit, 0, datatype.Enumerated(1))) // DIAMETER_LOGOUT
	addAVP(m, diam.NewAVP(avp.EventTimestamp, avp.Mbit, 0, eventTime))
	addAVP(m, diam.NewAVP(avp.MultipleServicesCreditControl, avp.Mbit, 0, &diam.GroupedAVP{
		AVP: []*diam.AVP{
			diam.NewAVP(avp.RatingGroup, avp.Mbit, 0, datatype.Unsigned32(10)),
			diam.NewAVP(avp.ServiceIdentifier, avp.Mbit, 0, datatype.Unsigned32(100)),
			usu(0), // final usage report
		},
	}))
	return m
}

func buildCCAInitial(seq uint32) *diam.Message {
	m := newCCA(seq, 1, 0)
	addAVP(m, diam.NewAVP(avp.MultipleServicesCreditControl, avp.Mbit, 0, &diam.GroupedAVP{
		AVP: []*diam.AVP{
			diam.NewAVP(avp.RatingGroup, avp.Mbit, 0, datatype.Unsigned32(10)),
			diam.NewAVP(avp.ServiceIdentifier, avp.Mbit, 0, datatype.Unsigned32(100)),
			diam.NewAVP(avp.GrantedServiceUnit, avp.Mbit, 0, &diam.GroupedAVP{
				AVP: []*diam.AVP{
					diam.NewAVP(avp.CCTotalOctets, avp.Mbit, 0, datatype.Unsigned64(10_000_000)),
				},
			}),
			diam.NewAVP(avp.ValidityTime, avp.Mbit, 0, datatype.Unsigned32(3600)),
			diam.NewAVP(avp.ResultCode, avp.Mbit, 0, datatype.Unsigned32(2001)),
		},
	}))
	addAVP(m, diam.NewAVP(avp.CCSessionFailover, avp.Mbit, 0, datatype.Enumerated(1)))            // FAILOVER_SUPPORTED
	addAVP(m, diam.NewAVP(avp.CreditControlFailureHandling, avp.Mbit, 0, datatype.Enumerated(0))) // TERMINATE
	return m
}

// buildCCAUpdateResultCodes: 3 MSCCs with per-MSCC Result-Codes 2001,
// 2001, 4012 (DIAMETER_CREDIT_LIMIT_REACHED).
func buildCCAUpdateResultCodes(seq uint32) *diam.Message {
	m := newCCA(seq, 2, 1)
	codes := []uint32{2001, 2001, 4012}
	for i, rc := range codes {
		children := []*diam.AVP{
			diam.NewAVP(avp.RatingGroup, avp.Mbit, 0, datatype.Unsigned32(uint32(10+i))),
			diam.NewAVP(avp.ServiceIdentifier, avp.Mbit, 0, datatype.Unsigned32(uint32(100+i))),
		}
		if rc == 2001 {
			children = append(children,
				diam.NewAVP(avp.GrantedServiceUnit, avp.Mbit, 0, &diam.GroupedAVP{
					AVP: []*diam.AVP{
						diam.NewAVP(avp.CCTotalOctets, avp.Mbit, 0, datatype.Unsigned64(10_000_000)),
					},
				}),
				diam.NewAVP(avp.ValidityTime, avp.Mbit, 0, datatype.Unsigned32(3600)),
			)
		}
		children = append(children, diam.NewAVP(avp.ResultCode, avp.Mbit, 0, datatype.Unsigned32(rc)))
		addAVP(m, diam.NewAVP(avp.MultipleServicesCreditControl, avp.Mbit, 0, &diam.GroupedAVP{AVP: children}))
	}
	return m
}

func buildCCAFinalUnit(seq uint32) *diam.Message {
	m := newCCA(seq, 2, 4)
	addAVP(m, diam.NewAVP(avp.MultipleServicesCreditControl, avp.Mbit, 0, &diam.GroupedAVP{
		AVP: []*diam.AVP{
			diam.NewAVP(avp.RatingGroup, avp.Mbit, 0, datatype.Unsigned32(10)),
			diam.NewAVP(avp.ServiceIdentifier, avp.Mbit, 0, datatype.Unsigned32(100)),
			diam.NewAVP(avp.GrantedServiceUnit, avp.Mbit, 0, &diam.GroupedAVP{
				AVP: []*diam.AVP{
					diam.NewAVP(avp.CCTotalOctets, avp.Mbit, 0, datatype.Unsigned64(500_000)),
				},
			}),
			diam.NewAVP(avp.FinalUnitIndication, avp.Mbit, 0, &diam.GroupedAVP{
				AVP: []*diam.AVP{
					diam.NewAVP(avp.FinalUnitAction, avp.Mbit, 0, datatype.Enumerated(0)), // TERMINATE
				},
			}),
			diam.NewAVP(avp.ResultCode, avp.Mbit, 0, datatype.Unsigned32(2001)),
		},
	}))
	return m
}

// buildCCRUnknownAVPs: a CCR-U carrying two AVPs unknown to every shipped
// dictionary (vendor 99999) — one with the M-bit set, one without —
// for forward-compat and M-bit surfacing tests (codec-design.md §2.4).
func buildCCRUnknownAVPs(seq uint32) *diam.Message {
	m := buildCCRUpdate(seq, 1)
	addAVP(m, diam.NewAVP(999001, avp.Mbit|avp.Vbit, 99999, datatype.OctetString("\xde\xad\xbe\xef\x01")))
	addAVP(m, diam.NewAVP(999002, avp.Vbit, 99999, datatype.OctetString("\xca\xfe")))
	return m
}

// addAVP appends a pre-built AVP keeping Header.MessageLength consistent,
// mirroring what Message.NewAVP does internally.
func addAVP(m *diam.Message, a *diam.AVP) {
	m.AVP = append(m.AVP, a)
	m.Header.MessageLength += uint32(a.Len())
}
