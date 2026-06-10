// Copyright 2013-2026 go-diameter authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// Package gycodec is a generated static codec for the Diameter Gy/DCCA
// message set (Application-Id 4, command 272: CCR/CCA). It replaces the
// dictionary-driven codec on the hot path with direct offset arithmetic —
// no maps, no reflection, no interface boxing — targeting near-zero
// allocations per message. See codec-design.md.
//
// # Buffer ownership — the rule
//
// A parsed CCR/CCA and EVERY sub-slice within it ([]byte fields, RawAVP
// data) alias the read buffer and are valid ONLY until the handler
// callback returns. The read buffer is pooled and reused immediately
// after. Any data needed beyond the callback MUST be copied out via
// Clone() before return. Violations corrupt silently under load; the
// gycodec_poison build tag exists to make them fail loudly in CI.
//
// Generated types come from cmd/gycodegen (go:generate, output
// committed); the dictionary XML under diam/dict/testdata is the source
// of truth for AVP codes, vendor IDs and types.
package gycodec
