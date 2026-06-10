// Copyright 2013-2026 go-diameter authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package gycodec

import (
	"bytes"
	"testing"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/dict"
)

// FuzzParseFrom fuzzes the static parser seeded with the golden corpus
// (codec-design.md §5.2). Invariants:
//   - ParseFrom never panics, never reads out of bounds;
//   - a successful parse must AppendTo without panicking, and the output
//     must re-parse successfully;
//   - differential: where the dictionary codec accepts the input, the
//     static codec must not crash. It may legitimately be stricter;
//     strictness differences are logged, not failed.
func FuzzParseFrom(f *testing.F) {
	for _, name := range fixtureNames {
		f.Add(loadFixture(f, name))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var ccr CCR
		errStatic := ccr.ParseFrom(data)
		if errStatic == nil {
			out := ccr.AppendTo(nil)
			var again CCR
			if err := again.ParseFrom(out); err != nil {
				t.Fatalf("re-parse of serialized output failed: %v", err)
			}
		}
		// CCA path shares the walker but has different fields.
		var cca CCA
		if cca.ParseFrom(data) == nil {
			_ = cca.AppendTo(nil)
		}
		// Differential strictness check against the dictionary codec.
		// The stock codec is known to PANIC on some malformed inputs
		// (a pre-existing remote-DoS in diam.decodeAVPs, unrelated to
		// gycodec — see NOTES.md §6); recover so a stock-codec crash
		// cannot mask or fail a static-codec finding. The invariant we
		// assert here is solely about the static codec.
		dictAccepts := dictParseNoPanic(data)
		if dictAccepts && errStatic != nil {
			t.Logf("strictness difference: dict accepts, static rejects with %v", errStatic)
		}
	})
}

// dictParseNoPanic runs the dictionary codec, treating a panic as
// rejection. Returns true only if the dict codec accepted the input
// without error and without panicking.
func dictParseNoPanic(data []byte) (accepted bool) {
	defer func() {
		if recover() != nil {
			accepted = false
		}
	}()
	_, err := diam.ReadMessage(bytes.NewReader(data), dict.Default)
	return err == nil
}
