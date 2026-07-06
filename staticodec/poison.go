// Copyright 2013-2026 go-diameter authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

//go:build staticodec_poison

package staticodec

// PoisonEnabled reports whether poison mode is compiled in.
const PoisonEnabled = true

// Poison overwrites b with 0xDE so any retained sub-slice of a recycled
// read buffer fails loudly instead of corrupting silently
// (codec-design.md §2.5). Call it on a buffer immediately after the
// handler that received it returns.
func Poison(b []byte) {
	for i := range b {
		b[i] = 0xDE
	}
}
