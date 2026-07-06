// Copyright 2013-2026 go-diameter authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package staticodec

import "encoding/binary"

// HeaderLength is the fixed Diameter header size (RFC 6733 §3).
const HeaderLength = 20

// Command flag bits (RFC 6733 §3).
const (
	FlagRequest       = 0x80 // R-bit
	FlagProxiable     = 0x40 // P-bit
	FlagError         = 0x20 // E-bit
	FlagRetransmitted = 0x10 // T-bit
)

// Header is the fixed 20-byte Diameter message header. Value type; no
// pointers, no allocation.
type Header struct {
	Version       uint8
	MessageLength uint32
	CommandFlags  uint8
	CommandCode   uint32
	ApplicationID uint32
	HopByHopID    uint32
	EndToEndID    uint32
}

// ParseHeader decodes the fixed header from a complete message buffer
// and validates: version is 1, declared length covers at least the
// header, and the declared length equals the buffer length.
func ParseHeader(buf []byte) (Header, error) {
	var h Header
	if len(buf) < HeaderLength {
		return h, ErrTruncated
	}
	h.Version = buf[0]
	if h.Version != 1 {
		return h, ErrBadVersion
	}
	h.MessageLength = uint24(buf[1:4])
	if h.MessageLength < HeaderLength || int(h.MessageLength) != len(buf) {
		return h, ErrLengthMismatch
	}
	h.CommandFlags = buf[4]
	h.CommandCode = uint24(buf[5:8])
	h.ApplicationID = binary.BigEndian.Uint32(buf[8:12])
	h.HopByHopID = binary.BigEndian.Uint32(buf[12:16])
	h.EndToEndID = binary.BigEndian.Uint32(buf[16:20])
	return h, nil
}

// AppendHeader appends the 20-byte header with a zero length field; the
// caller backpatches the length once the body is appended.
func AppendHeader(dst []byte, h Header) []byte {
	var b [HeaderLength]byte
	b[0] = 1 // version
	// b[1:4] message length: backpatched by the caller
	b[4] = h.CommandFlags
	PutUint24(b[5:8], h.CommandCode)
	binary.BigEndian.PutUint32(b[8:12], h.ApplicationID)
	binary.BigEndian.PutUint32(b[12:16], h.HopByHopID)
	binary.BigEndian.PutUint32(b[16:20], h.EndToEndID)
	return append(dst, b[:]...)
}

// BackpatchMessageLength writes the final message length into the header
// that was appended at msgOff.
func BackpatchMessageLength(dst []byte, msgOff int) {
	PutUint24(dst[msgOff+1:msgOff+4], uint32(len(dst)-msgOff))
}
