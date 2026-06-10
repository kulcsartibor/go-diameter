// Copyright 2013-2026 go-diameter authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package gycodec

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/avp"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
	"github.com/fiorix/go-diameter/v4/diam/dict"
)

// TestCrossCodecEquivalence implements codec-design.md §5.1: the
// dictionary codec's serialization of each fixture must parse under the
// static codec with the same canonical field values, and the static
// codec's serialization must be parseable by the dictionary codec.
// Canonical field sets are compared (not bytes) because AVP ordering may
// legitimately differ between codecs.
func TestCrossCodecEquivalence(t *testing.T) {
	for _, name := range fixtureNames {
		t.Run(name, func(t *testing.T) {
			fix := loadFixture(t, name)

			// Dictionary parse → dictionary serialize.
			dm, err := diam.ReadMessage(bytes.NewReader(fix), dict.Default)
			if err != nil {
				t.Fatalf("dict ReadMessage: %v", err)
			}
			dictOut, err := dm.Serialize()
			if err != nil {
				t.Fatalf("dict Serialize: %v", err)
			}

			// Static parse of the original and of the dict output must
			// agree on canonical fields.
			m1 := newMessageFor(name)
			if err := m1.ParseFrom(fix); err != nil {
				t.Fatalf("static ParseFrom(fixture): %v", err)
			}
			m2 := newMessageFor(name)
			if err := m2.ParseFrom(dictOut); err != nil {
				t.Fatalf("static ParseFrom(dict output): %v", err)
			}
			c1, c2 := canonicalStatic(m1), canonicalStatic(m2)
			if c1 != c2 {
				t.Fatalf("static canonical mismatch:\n fixture: %s\n dictout: %s", c1, c2)
			}

			// Canonical fields must also match what the dictionary codec
			// decoded from the same bytes.
			cd := canonicalDict(t, dm)
			if c1 != cd {
				t.Fatalf("cross-codec canonical mismatch:\n static: %s\n dict:   %s", c1, cd)
			}

			// Static serialization must be parseable by the dict codec.
			staticOut := m1.AppendTo(nil)
			if _, err := diam.ReadMessage(bytes.NewReader(staticOut), dict.Default); err != nil {
				t.Fatalf("dict cannot parse static output: %v", err)
			}
		})
	}
}

// canonicalStatic renders the hot OCS-relevant fields of a static
// CCR/CCA into a comparable string.
func canonicalStatic(m gyMessage) string {
	var b bytes.Buffer
	switch v := m.(type) {
	case *CCR:
		fmt.Fprintf(&b, "sid=%q rt=%d/%v rn=%d/%v subs=%d", v.SessionID,
			v.CCRequestType, v.HasCCRequestType, v.CCRequestNumber, v.HasCCRequestNumber,
			len(v.SubscriptionID))
		for i := range v.SubscriptionID {
			s := &v.SubscriptionID[i]
			fmt.Fprintf(&b, " sub[%d]=%d:%q", i, s.Type, s.Data)
		}
		writeMSCCs(&b, v.MSCC)
		fmt.Fprintf(&b, " other=%d um=%d", len(v.Other), len(v.UnsupportedMandatory))
	case *CCA:
		fmt.Fprintf(&b, "sid=%q rc=%d/%v rt=%d/%v rn=%d/%v", v.SessionID,
			v.ResultCode, v.HasResultCode,
			v.CCRequestType, v.HasCCRequestType, v.CCRequestNumber, v.HasCCRequestNumber)
		writeMSCCs(&b, v.MSCC)
		fmt.Fprintf(&b, " other=%d um=%d", len(v.Other), len(v.UnsupportedMandatory))
	}
	return b.String()
}

func writeMSCCs(b *bytes.Buffer, mscc []MSCC) {
	fmt.Fprintf(b, " mscc=%d", len(mscc))
	for i := range mscc {
		c := &mscc[i]
		fmt.Fprintf(b, " m[%d]={rg=%d/%v si=%d/%v rc=%d/%v vt=%d/%v ttc=%d/%v",
			i, c.RatingGroup, c.HasRatingGroup, c.ServiceIdentifier, c.HasServiceIdentifier,
			c.ResultCode, c.HasResultCode, c.ValidityTime, c.HasValidityTime,
			c.TariffTimeChange, c.HasTariffTimeChange)
		if c.HasGranted {
			fmt.Fprintf(b, " gsu={t=%d/%v to=%d/%v}",
				c.Granted.CCTime, c.Granted.HasCCTime,
				c.Granted.CCTotalOctets, c.Granted.HasCCTotalOctets)
		}
		fmt.Fprintf(b, " usu=%d", len(c.Used))
		for j := range c.Used {
			u := &c.Used[j]
			fmt.Fprintf(b, " u[%d]={tcu=%d/%v t=%d/%v to=%d/%v ssu=%d/%v}",
				j, u.TariffChangeUsage, u.HasTariffChangeUsage,
				u.CCTime, u.HasCCTime, u.CCTotalOctets, u.HasCCTotalOctets,
				u.CCServiceSpecificUnits, u.HasCCServiceSpecificUnits)
		}
		if c.HasFinalUnitIndication {
			fmt.Fprintf(b, " fui=%d/%v", c.FinalUnitIndication.FinalUnitAction,
				c.FinalUnitIndication.HasFinalUnitAction)
		}
		if c.HasTrigger {
			fmt.Fprintf(b, " trig=%v", c.Trigger.TriggerType)
		}
		fmt.Fprintf(b, "}")
	}
}

// canonicalDict renders the same canonical fields from a
// dictionary-codec message.
func canonicalDict(t *testing.T, m *diam.Message) string {
	t.Helper()
	var b bytes.Buffer
	req := m.Header.CommandFlags&diam.RequestFlag != 0
	if req {
		fmt.Fprintf(&b, "sid=%q rt=%d/%v rn=%d/%v",
			dictBytes(m.AVP, avp.SessionID),
			dictInt32(m.AVP, avp.CCRequestType), dictHas(m.AVP, avp.CCRequestType),
			dictU32(m.AVP, avp.CCRequestNumber), dictHas(m.AVP, avp.CCRequestNumber))
		subs := dictGroups(m.AVP, avp.SubscriptionID)
		fmt.Fprintf(&b, " subs=%d", len(subs))
		for i, s := range subs {
			fmt.Fprintf(&b, " sub[%d]=%d:%q", i,
				dictInt32(s, avp.SubscriptionIDType), dictBytes(s, avp.SubscriptionIDData))
		}
	} else {
		fmt.Fprintf(&b, "sid=%q rc=%d/%v rt=%d/%v rn=%d/%v",
			dictBytes(m.AVP, avp.SessionID),
			dictU32(m.AVP, avp.ResultCode), dictHas(m.AVP, avp.ResultCode),
			dictInt32(m.AVP, avp.CCRequestType), dictHas(m.AVP, avp.CCRequestType),
			dictU32(m.AVP, avp.CCRequestNumber), dictHas(m.AVP, avp.CCRequestNumber))
	}
	msccs := dictGroups(m.AVP, avp.MultipleServicesCreditControl)
	fmt.Fprintf(&b, " mscc=%d", len(msccs))
	for i, c := range msccs {
		fmt.Fprintf(&b, " m[%d]={rg=%d/%v si=%d/%v rc=%d/%v vt=%d/%v ttc=%d/%v",
			i, dictU32(c, avp.RatingGroup), dictHas(c, avp.RatingGroup),
			dictU32(c, avp.ServiceIdentifier), dictHas(c, avp.ServiceIdentifier),
			dictU32(c, avp.ResultCode), dictHas(c, avp.ResultCode),
			dictU32(c, avp.ValidityTime), dictHas(c, avp.ValidityTime),
			dictTime(c, avp.TariffTimeChange), dictHas(c, avp.TariffTimeChange))
		if gsu := dictGroups(c, avp.GrantedServiceUnit); len(gsu) > 0 {
			fmt.Fprintf(&b, " gsu={t=%d/%v to=%d/%v}",
				dictU32(gsu[0], avp.CCTime), dictHas(gsu[0], avp.CCTime),
				dictU64(gsu[0], avp.CCTotalOctets), dictHas(gsu[0], avp.CCTotalOctets))
		}
		usus := dictGroups(c, avp.UsedServiceUnit)
		fmt.Fprintf(&b, " usu=%d", len(usus))
		for j, u := range usus {
			fmt.Fprintf(&b, " u[%d]={tcu=%d/%v t=%d/%v to=%d/%v ssu=%d/%v}",
				j, dictInt32(u, avp.TariffChangeUsage), dictHas(u, avp.TariffChangeUsage),
				dictU32(u, avp.CCTime), dictHas(u, avp.CCTime),
				dictU64(u, avp.CCTotalOctets), dictHas(u, avp.CCTotalOctets),
				dictU64(u, avp.CCServiceSpecificUnits), dictHas(u, avp.CCServiceSpecificUnits))
		}
		if fui := dictGroups(c, avp.FinalUnitIndication); len(fui) > 0 {
			fmt.Fprintf(&b, " fui=%d/%v",
				dictInt32(fui[0], avp.FinalUnitAction), dictHas(fui[0], avp.FinalUnitAction))
		}
		if trig := dictGroups(c, avp.Trigger); len(trig) > 0 {
			var tt []int32
			for _, a := range trig[0] {
				if a.Code == avp.TriggerType {
					tt = append(tt, int32(a.Data.(datatype.Enumerated)))
				}
			}
			fmt.Fprintf(&b, " trig=%v", tt)
		}
		fmt.Fprintf(&b, "}")
	}
	// Unknown AVPs: vendor 99999 codes 999001/999002 in the fixture.
	other, um := 0, 0
	for _, a := range m.AVP {
		if a.Code >= 999000 {
			other++
			if a.Flags&avp.Mbit != 0 {
				um++
			}
		}
	}
	fmt.Fprintf(&b, " other=%d um=%d", other, um)
	return b.String()
}

// --- dictionary-message field extraction helpers ---

func dictFind(avps []*diam.AVP, code uint32) *diam.AVP {
	for _, a := range avps {
		if a.Code == code {
			return a
		}
	}
	return nil
}

func dictHas(avps []*diam.AVP, code uint32) bool { return dictFind(avps, code) != nil }

func dictBytes(avps []*diam.AVP, code uint32) string {
	if a := dictFind(avps, code); a != nil {
		switch d := a.Data.(type) {
		case datatype.UTF8String:
			return string(d)
		case datatype.OctetString:
			return string(d)
		case datatype.DiameterIdentity:
			return string(d)
		}
	}
	return ""
}

func dictU32(avps []*diam.AVP, code uint32) uint32 {
	if a := dictFind(avps, code); a != nil {
		if d, ok := a.Data.(datatype.Unsigned32); ok {
			return uint32(d)
		}
	}
	return 0
}

func dictU64(avps []*diam.AVP, code uint32) uint64 {
	if a := dictFind(avps, code); a != nil {
		if d, ok := a.Data.(datatype.Unsigned64); ok {
			return uint64(d)
		}
	}
	return 0
}

func dictInt32(avps []*diam.AVP, code uint32) int32 {
	if a := dictFind(avps, code); a != nil {
		switch d := a.Data.(type) {
		case datatype.Enumerated:
			return int32(d)
		case datatype.Integer32:
			return int32(d)
		}
	}
	return 0
}

// dictTime returns the raw NTP seconds of a Time AVP, matching the
// static codec's uint32 representation.
func dictTime(avps []*diam.AVP, code uint32) uint32 {
	if a := dictFind(avps, code); a != nil {
		if d, ok := a.Data.(datatype.Time); ok {
			b, _ := datatype.Time(d).Serialize(), error(nil)
			if len(b) == 4 {
				return be32(b)
			}
		}
	}
	return 0
}

func dictGroups(avps []*diam.AVP, code uint32) [][]*diam.AVP {
	var out [][]*diam.AVP
	for _, a := range avps {
		if a.Code == code {
			if g, ok := a.Data.(*diam.GroupedAVP); ok {
				out = append(out, g.AVP)
			}
		}
	}
	return out
}
