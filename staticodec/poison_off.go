// Copyright 2013-2026 go-diameter authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

//go:build !staticodec_poison

package staticodec

// PoisonEnabled reports whether poison mode is compiled in.
const PoisonEnabled = false

// Poison is a no-op unless built with -tags staticodec_poison.
func Poison(b []byte) {}
