// Copyright 2013-2026 go-diameter authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

//go:build !gycodec_poison

package gycodec

// PoisonEnabled reports whether poison mode is compiled in.
const PoisonEnabled = false

// Poison is a no-op unless built with -tags gycodec_poison.
func Poison(b []byte) {}
