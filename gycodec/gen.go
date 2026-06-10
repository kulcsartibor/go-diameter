// Copyright 2013-2026 go-diameter authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package gycodec

// Regenerate the static codec from the dictionary XML. Output is
// committed; `go generate ./gycodec && git diff --exit-code gycodec`
// is the CI determinism check.
//
//go:generate go run github.com/fiorix/go-diameter/v4/cmd/gycodegen -dict ../diam/dict/testdata -out .
