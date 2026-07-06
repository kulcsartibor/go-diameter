// Copyright 2013-2026 go-diameter authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package staticodec

// Regenerate every application's static codec from the dictionary XML.
// One invocation writes all app sub-packages (staticodec/gy, staticodec/sy,
// …). Output is committed; `go generate ./staticodec && git diff --exit-code
// staticodec` is the CI determinism check.
//
//go:generate go run github.com/fiorix/go-diameter/v4/cmd/staticodegen -dict ../diam/dict/testdata -out .
