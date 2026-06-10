// Copyright 2013-2026 go-diameter authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

//go:build gycodec_poison

package diam

// FORK: poison mode for the raw fast path. Overwrites a pooled read
// buffer with 0xDE the instant the RawHandler returns, so any retained
// sub-slice fails loudly in CI instead of corrupting silently in
// production (codec-design.md §2.5).
func poisonRaw(b []byte) {
	for i := range b {
		b[i] = 0xDE
	}
}
