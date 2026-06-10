// Copyright 2013-2026 go-diameter authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// Command gycodegen generates the static Gy codec (package gycodec) from
// the dictionary XML files shipped with go-diameter, filtered through the
// curated allowlist in model.go (codec-design.md §2.2).
//
// The dictionary stays the source of truth for AVP codes, vendor IDs,
// types, flags and enum values; the allowlist defines which AVPs are
// modeled as struct fields, their Go names, and the fixed emission order.
//
// Usage (via go:generate in gycodec/gen.go):
//
//	gycodegen -dict diam/dict/testdata -out gycodec
//
// This is a build tool, not a hot path: boring and readable on purpose.
package main

import (
	"encoding/xml"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/fiorix/go-diameter/v4/diam/dict"
)

// dictFiles are the XML sources consulted, in resolution priority order
// (first definition of an AVP name wins).
var dictFiles = []string{
	"credit_control.xml",
	"tgpp_ro_rf.xml",
	"base.xml",
	"network_access_server.xml", // Called-Station-Id
}

func main() {
	dictDir := flag.String("dict", "diam/dict/testdata", "directory with dictionary XML files")
	outDir := flag.String("out", "gycodec", "output directory for generated files")
	flag.Parse()

	avps, err := loadDictionaries(*dictDir)
	if err != nil {
		log.Fatal(err)
	}

	if err := resolveAll(avps); err != nil {
		log.Fatal(err)
	}

	files := map[string][]byte{
		"grouped.gen.go": emitGroups(),
		"ccr.gen.go":     emitMessage(&messages[0]),
		"cca.gen.go":     emitMessage(&messages[1]),
		"enums.gen.go":   emitEnums(avps),
	}
	for name, src := range files {
		path := filepath.Join(*outDir, name)
		if err := os.WriteFile(path, src, 0644); err != nil {
			log.Fatal(err)
		}
		fmt.Println("wrote", path)
	}
}

// loadDictionaries parses the XML files into a name → *dict.AVP map.
func loadDictionaries(dir string) (map[string]*dict.AVP, error) {
	avps := make(map[string]*dict.AVP)
	for _, name := range dictFiles {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		var f dict.File
		if err := xml.Unmarshal(b, &f); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		for _, app := range f.App {
			for _, a := range app.AVP {
				if _, ok := avps[a.Name]; !ok {
					avps[a.Name] = a
				}
			}
		}
	}
	return avps, nil
}

// resolveAll fills code/vendorID/flags/kind on every allowlisted field
// from the dictionary, validating the allowlist against the XML.
func resolveAll(avps map[string]*dict.AVP) error {
	resolve := func(s *structDef) error {
		for i := range s.fields {
			f := &s.fields[i]
			a, ok := avps[f.avpName]
			if !ok {
				return fmt.Errorf("%s.%s: AVP %q not found in dictionaries", s.name, f.goName, f.avpName)
			}
			f.code = a.Code
			f.vendorID = a.VendorID
			f.flags = emissionFlags(a)
			k, err := kindOf(a.Data.TypeName)
			if err != nil {
				return fmt.Errorf("%s.%s (%s): %w", s.name, f.goName, f.avpName, err)
			}
			f.kind = k
			if (k == kindGroup) != (f.group != "") {
				return fmt.Errorf("%s.%s (%s): grouped/group-name mismatch (dict type %q)",
					s.name, f.goName, f.avpName, a.Data.TypeName)
			}
		}
		return nil
	}
	for i := range groups {
		if err := resolve(&groups[i]); err != nil {
			return err
		}
	}
	for i := range messages {
		if err := resolve(&messages[i].structDef); err != nil {
			return err
		}
	}
	return nil
}

// emissionFlags derives the fixed serialization flags from the
// dictionary: M-bit when "must" contains M, V-bit when a vendor id is
// declared. The parser accepts any flags; these apply on AppendTo.
func emissionFlags(a *dict.AVP) byte {
	var fl byte
	if strings.Contains(a.Must, "M") {
		fl |= 0x40
	}
	if a.VendorID != 0 {
		fl |= 0x80
	}
	return fl
}

// kindOf maps a dictionary type name to a wire kind.
func kindOf(typeName string) (kind, error) {
	switch typeName {
	case "Unsigned32":
		return kindU32, nil
	case "Unsigned64":
		return kindU64, nil
	case "Enumerated", "Integer32":
		return kindEnum, nil
	case "Time":
		return kindTime, nil
	case "UTF8String", "OctetString", "DiameterIdentity", "DiameterURI", "Address", "IPAddress":
		return kindBytes, nil
	case "Grouped":
		return kindGroup, nil
	}
	return 0, fmt.Errorf("unsupported dictionary type %q", typeName)
}
