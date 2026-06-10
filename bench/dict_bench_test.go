// Copyright 2013-2026 go-diameter authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// Package bench holds the Phase 1 baseline benchmarks of the dictionary
// codec over the golden Gy fixtures (codec-design.md §3 Phase 1). The
// static-codec counterparts land in Phase 2. Results are recorded in
// BENCHMARKS.md; profiles under bench/baseline/.
package bench

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/dict"
)

// loadFixture reads a golden fixture from bench/fixtures.
func loadFixture(tb testing.TB, name string) []byte {
	tb.Helper()
	b, err := os.ReadFile(filepath.Join("fixtures", name+".bin"))
	if err != nil {
		tb.Fatalf("fixture %s: %v (run: go run bench/fixtures/gen.go)", name, err)
	}
	return b
}

// TestFixturesParseable guards the corpus: every fixture must decode
// cleanly with the dictionary codec and re-serialize byte-identically.
func TestFixturesParseable(t *testing.T) {
	names := []string{
		"ccr_i_1mscc_rsu",
		"ccr_u_1mscc",
		"ccr_u_3mscc",
		"ccr_u_5mscc",
		"ccr_u_trigger_ttc",
		"ccr_t_final_usu",
		"cca_i_gsu_validity",
		"cca_u_mscc_resultcodes",
		"cca_fui_terminate",
		"ccr_unknown_vendor_avps",
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			fix := loadFixture(t, name)
			m, err := diam.ReadMessage(bytes.NewReader(fix), dict.Default)
			if err != nil {
				t.Fatalf("ReadMessage: %v", err)
			}
			out, err := m.Serialize()
			if err != nil {
				t.Fatalf("Serialize: %v", err)
			}
			if !bytes.Equal(fix, out) {
				t.Fatal("dict codec round-trip not byte-identical")
			}
		})
	}
}

func benchmarkDictParse(b *testing.B, name string) {
	fix := loadFixture(b, name)
	r := bytes.NewReader(fix)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := diam.ReadMessage(r, dict.Default); err != nil {
			b.Fatal(err)
		}
		r.Seek(0, 0)
	}
}

func benchmarkDictSerialize(b *testing.B, name string) {
	fix := loadFixture(b, name)
	m, err := diam.ReadMessage(bytes.NewReader(fix), dict.Default)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := m.Serialize(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDictParse1MSCC(b *testing.B)      { benchmarkDictParse(b, "ccr_u_1mscc") }
func BenchmarkDictParse3MSCC(b *testing.B)      { benchmarkDictParse(b, "ccr_u_3mscc") }
func BenchmarkDictParse5MSCC(b *testing.B)      { benchmarkDictParse(b, "ccr_u_5mscc") }
func BenchmarkDictParseTriggerTTC(b *testing.B) { benchmarkDictParse(b, "ccr_u_trigger_ttc") }
func BenchmarkDictParseCCRInitial(b *testing.B) { benchmarkDictParse(b, "ccr_i_1mscc_rsu") }
func BenchmarkDictParseCCA(b *testing.B)        { benchmarkDictParse(b, "cca_u_mscc_resultcodes") }

func BenchmarkDictSerialize1MSCC(b *testing.B)      { benchmarkDictSerialize(b, "ccr_u_1mscc") }
func BenchmarkDictSerialize3MSCC(b *testing.B)      { benchmarkDictSerialize(b, "ccr_u_3mscc") }
func BenchmarkDictSerialize5MSCC(b *testing.B)      { benchmarkDictSerialize(b, "ccr_u_5mscc") }
func BenchmarkDictSerializeTriggerTTC(b *testing.B) { benchmarkDictSerialize(b, "ccr_u_trigger_ttc") }
func BenchmarkDictSerializeCCA(b *testing.B)        { benchmarkDictSerialize(b, "cca_u_mscc_resultcodes") }
