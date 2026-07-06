// Copyright 2013-2026 go-diameter authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package bench

import (
	"testing"

	"github.com/fiorix/go-diameter/v4/staticodec/gy"
)

// Static-codec counterparts of the dictionary baseline, exercising the
// intended usage pattern: one reused message struct (the OCS handler
// pools them) and a reused output buffer for serialization.

func benchmarkStaticParseCCR(b *testing.B, name string) {
	fix := loadFixture(b, name)
	var m gy.CCR
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := m.ParseFrom(fix); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkStaticParseCCA(b *testing.B, name string) {
	fix := loadFixture(b, name)
	var m gy.CCA
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := m.ParseFrom(fix); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkStaticSerializeCCR(b *testing.B, name string) {
	fix := loadFixture(b, name)
	var m gy.CCR
	if err := m.ParseFrom(fix); err != nil {
		b.Fatal(err)
	}
	dst := make([]byte, 0, len(fix))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dst = m.AppendTo(dst[:0])
	}
}

func benchmarkStaticSerializeCCA(b *testing.B, name string) {
	fix := loadFixture(b, name)
	var m gy.CCA
	if err := m.ParseFrom(fix); err != nil {
		b.Fatal(err)
	}
	dst := make([]byte, 0, len(fix))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dst = m.AppendTo(dst[:0])
	}
}

func BenchmarkStaticParse1MSCC(b *testing.B)      { benchmarkStaticParseCCR(b, "ccr_u_1mscc") }
func BenchmarkStaticParse3MSCC(b *testing.B)      { benchmarkStaticParseCCR(b, "ccr_u_3mscc") }
func BenchmarkStaticParse5MSCC(b *testing.B)      { benchmarkStaticParseCCR(b, "ccr_u_5mscc") }
func BenchmarkStaticParseTriggerTTC(b *testing.B) { benchmarkStaticParseCCR(b, "ccr_u_trigger_ttc") }
func BenchmarkStaticParseCCRInitial(b *testing.B) { benchmarkStaticParseCCR(b, "ccr_i_1mscc_rsu") }
func BenchmarkStaticParseCCA(b *testing.B)        { benchmarkStaticParseCCA(b, "cca_u_mscc_resultcodes") }

func BenchmarkStaticSerialize1MSCC(b *testing.B) { benchmarkStaticSerializeCCR(b, "ccr_u_1mscc") }
func BenchmarkStaticSerialize3MSCC(b *testing.B) { benchmarkStaticSerializeCCR(b, "ccr_u_3mscc") }
func BenchmarkStaticSerialize5MSCC(b *testing.B) { benchmarkStaticSerializeCCR(b, "ccr_u_5mscc") }
func BenchmarkStaticSerializeTriggerTTC(b *testing.B) {
	benchmarkStaticSerializeCCR(b, "ccr_u_trigger_ttc")
}
func BenchmarkStaticSerializeCCA(b *testing.B) { benchmarkStaticSerializeCCA(b, "cca_u_mscc_resultcodes") }
