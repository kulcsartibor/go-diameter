// Copyright 2013-2026 go-diameter authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package gycodec

import (
	"encoding/binary"
	"errors"
)

// AVP flag bits (RFC 6733 §4.1).
const (
	FlagVendor    = 0x80 // V-bit: 4-byte Vendor-ID present in AVP header
	FlagMandatory = 0x40 // M-bit
	FlagProtected = 0x20 // P-bit
)

// Pre-allocated sentinel errors; the parse path never calls fmt.Errorf.
var (
	// ErrTruncated reports a buffer shorter than a declared length.
	ErrTruncated = errors.New("gycodec: truncated message")
	// ErrBadAVPLength reports an AVP whose declared length is impossible
	// (shorter than its own header or extending past the buffer).
	ErrBadAVPLength = errors.New("gycodec: bad AVP length")
	// ErrBadVersion reports a Diameter version other than 1.
	ErrBadVersion = errors.New("gycodec: unsupported Diameter version")
	// ErrLengthMismatch reports a header Message-Length that disagrees
	// with the buffer length.
	ErrLengthMismatch = errors.New("gycodec: message length disagrees with buffer")
	// ErrBadAVPData reports AVP data whose size is invalid for its
	// declared type (e.g. a 3-byte Unsigned32).
	ErrBadAVPData = errors.New("gycodec: bad AVP data size for type")
	// ErrUnexpectedCommand reports a message whose command code does not
	// match the type it is being parsed into (CCR/CCA expect 272).
	ErrUnexpectedCommand = errors.New("gycodec: unexpected command code")
)

// RawAVP preserves an AVP the static codec does not model, so unknown
// AVPs survive a parse→serialize round trip (codec-design.md §2.4).
//
// OWNERSHIP: Data aliases the read buffer and is valid only until the
// handler returns; copy it out to retain.
type RawAVP struct {
	Code     uint32
	VendorID uint32 // 0 when the V-bit is unset
	Flags    byte
	Data     []byte
}

// clone returns a deep copy of the RawAVP with its own backing array.
func (r RawAVP) clone() RawAVP {
	c := r
	c.Data = append([]byte(nil), r.Data...)
	return c
}

// appendTo re-serializes the raw AVP, preserving its original flags.
func (r RawAVP) appendTo(dst []byte) []byte {
	dst = appendAVPHeader(dst, r.Code, r.Flags, r.VendorID, len(r.Data))
	dst = append(dst, r.Data...)
	return appendPad4(dst, len(r.Data))
}

// walkAVP decodes the AVP header at buf[off:] and returns the AVP's code,
// flags, vendor ID (0 if no V-bit), its data sub-slice (padding excluded),
// and the offset of the next AVP (padding included). It never panics on
// malformed input; every structural violation returns an error:
//   - fewer than 8 bytes remaining
//   - declared length smaller than the AVP's own header
//   - declared length extending past the buffer
//   - V-bit set but length too small for the Vendor-ID field
func walkAVP(buf []byte, off int) (code uint32, flags byte, vendorID uint32, data []byte, next int, err error) {
	if len(buf)-off < 8 {
		return 0, 0, 0, nil, 0, ErrTruncated
	}
	b := buf[off:]
	code = binary.BigEndian.Uint32(b[0:4])
	flags = b[4]
	length := int(uint24(b[5:8]))
	hdrLen := 8
	if flags&FlagVendor != 0 {
		hdrLen = 12
	}
	if length < hdrLen || length > len(buf)-off {
		return 0, 0, 0, nil, 0, ErrBadAVPLength
	}
	if hdrLen == 12 {
		vendorID = binary.BigEndian.Uint32(b[8:12])
	}
	data = b[hdrLen:length:length]
	next = off + pad4(length)
	if next > len(buf) {
		// Padding may not extend past the buffer; the final AVP of a
		// message is allowed to end unpadded only if the message ends
		// exactly at its declared length.
		if off+length == len(buf) {
			next = len(buf)
		} else {
			return 0, 0, 0, nil, 0, ErrBadAVPLength
		}
	}
	return code, flags, vendorID, data, next, nil
}

// pad4 rounds n up to the next multiple of 4.
func pad4(n int) int {
	return (n + 3) &^ 3
}

// uint24 decodes a 24-bit big-endian unsigned integer.
func uint24(b []byte) uint32 {
	return uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2])
}

// putUint24 encodes a 24-bit big-endian unsigned integer in place.
func putUint24(b []byte, n uint32) {
	b[0] = byte(n >> 16)
	b[1] = byte(n >> 8)
	b[2] = byte(n)
}

// be32 decodes a big-endian uint32 from exactly 4 bytes.
func be32(b []byte) uint32 { return binary.BigEndian.Uint32(b) }

// be64 decodes a big-endian uint64 from exactly 8 bytes.
func be64(b []byte) uint64 { return binary.BigEndian.Uint64(b) }

// appendAVPHeader appends an 8- or 12-byte AVP header. dataLen is the
// unpadded data size; the length field is header+data, padding excluded.
func appendAVPHeader(dst []byte, code uint32, flags byte, vendorID uint32, dataLen int) []byte {
	hdrLen := 8
	if flags&FlagVendor != 0 {
		hdrLen = 12
	}
	var h [12]byte
	binary.BigEndian.PutUint32(h[0:4], code)
	h[4] = flags
	putUint24(h[5:8], uint32(hdrLen+dataLen))
	if hdrLen == 12 {
		binary.BigEndian.PutUint32(h[8:12], vendorID)
	}
	return append(dst, h[:hdrLen]...)
}

// appendPad4 appends the zero padding for an AVP whose unpadded data
// size is dataLen.
func appendPad4(dst []byte, dataLen int) []byte {
	switch dataLen & 3 {
	case 1:
		return append(dst, 0, 0, 0)
	case 2:
		return append(dst, 0, 0)
	case 3:
		return append(dst, 0)
	}
	return dst
}

// appendAVPUint32 appends a complete 4-byte-data AVP (Unsigned32, Time).
func appendAVPUint32(dst []byte, code uint32, flags byte, vendorID uint32, v uint32) []byte {
	dst = appendAVPHeader(dst, code, flags, vendorID, 4)
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	return append(dst, b[:]...)
}

// appendAVPUint64 appends a complete 8-byte-data AVP (Unsigned64).
func appendAVPUint64(dst []byte, code uint32, flags byte, vendorID uint32, v uint64) []byte {
	dst = appendAVPHeader(dst, code, flags, vendorID, 8)
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	return append(dst, b[:]...)
}

// appendAVPInt32 appends a complete 4-byte-data AVP (Enumerated, Integer32).
func appendAVPInt32(dst []byte, code uint32, flags byte, vendorID uint32, v int32) []byte {
	return appendAVPUint32(dst, code, flags, vendorID, uint32(v))
}

// appendAVPBytes appends a complete variable-length AVP (OctetString,
// UTF8String, DiameterIdentity, Address) with padding.
func appendAVPBytes(dst []byte, code uint32, flags byte, vendorID uint32, v []byte) []byte {
	dst = appendAVPHeader(dst, code, flags, vendorID, len(v))
	dst = append(dst, v...)
	return appendPad4(dst, len(v))
}

// openGroupedAVP appends a grouped-AVP header with a zero length field
// and returns the offset of that header for closeGroupedAVP to backpatch.
// Children are appended between the two calls (codec-design.md §2.6,
// reserve-and-backpatch).
func openGroupedAVP(dst []byte, code uint32, flags byte, vendorID uint32) (out []byte, hdrOff int) {
	hdrOff = len(dst)
	return appendAVPHeader(dst, code, flags, vendorID, 0), hdrOff
}

// closeGroupedAVP backpatches the length of the grouped AVP opened at
// hdrOff. Grouped data is a sequence of padded AVPs, so no trailing
// padding is required.
func closeGroupedAVP(dst []byte, hdrOff int) []byte {
	putUint24(dst[hdrOff+5:hdrOff+8], uint32(len(dst)-hdrOff))
	return dst
}
