// Copyright 2013-2026 go-diameter authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

//go:build !gycodec_poison

package diam

// FORK: poisonRaw is a no-op unless built with -tags gycodec_poison.
func poisonRaw(b []byte) {}
