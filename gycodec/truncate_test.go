// Copyright 2013-2026 go-diameter authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package gycodec

import "testing"

// TestTruncation: every fixture truncated at every 4-byte boundary (and
// every length 0..19) must return an error and never panic
// (codec-design.md §5.5 — the parser's security surface; a
// malformed-message panic is a remote DoS on the OCS front door).
func TestTruncation(t *testing.T) {
	for _, name := range fixtureNames {
		fix := loadFixture(t, name)
		t.Run(name, func(t *testing.T) {
			for l := 0; l < len(fix); l++ {
				if l >= 20 && l%4 != 0 {
					continue // 4-byte boundaries past the header, all of 0..19
				}
				m := newMessageFor(name)
				if err := m.ParseFrom(fix[:l]); err == nil {
					t.Fatalf("truncated to %d bytes: expected error, got nil", l)
				}
			}
		})
	}
}

// TestTruncationPatchedLength repeats the truncation sweep with the
// header Message-Length field patched to match the truncated buffer, so
// the AVP walker itself is exercised instead of the cheap length check.
// A truncation landing exactly on a top-level AVP boundary may parse
// successfully (it is a structurally valid shorter message); the
// invariant is no panic, and any successful parse must re-serialize
// without panicking.
func TestTruncationPatchedLength(t *testing.T) {
	for _, name := range fixtureNames {
		fix := loadFixture(t, name)
		t.Run(name, func(t *testing.T) {
			for l := 20; l < len(fix); l += 4 {
				buf := append([]byte(nil), fix[:l]...)
				putUint24(buf[1:4], uint32(l))
				m := newMessageFor(name)
				if err := m.ParseFrom(buf); err == nil {
					_ = m.AppendTo(nil)
				}
			}
		})
	}
}

// TestGarbageHeader exercises the header validations directly.
func TestGarbageHeader(t *testing.T) {
	var m CCR
	cases := []struct {
		name string
		buf  []byte
		want error
	}{
		{"empty", nil, ErrTruncated},
		{"short", make([]byte, 19), ErrTruncated},
		{"bad version", append([]byte{2}, make([]byte, 19)...), ErrBadVersion},
		{"zero length", append([]byte{1}, make([]byte, 19)...), ErrLengthMismatch},
		{"wrong command", func() []byte {
			b := make([]byte, 20)
			b[0] = 1
			putUint24(b[1:4], 20)
			putUint24(b[5:8], 257) // CER, not CC
			return b
		}(), ErrUnexpectedCommand},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := m.ParseFrom(c.buf); err != c.want {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}
}
