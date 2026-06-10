// Copyright 2013-2026 go-diameter authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package diam

// FORK: raw-codec fast path. Routes selected (Application-Id, Command-Code)
// messages to a RawHandler that receives the undecoded message bytes,
// bypassing the dictionary codec entirely. Everything else takes the
// unmodified stock path. See NOTES.md and codec-design.md §2.1.

import (
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"time"
)

// Errors returned by the raw fast path on a malformed fixed header. They
// close the connection, exactly as a stock readHeader error would.
var (
	errRawBadVersion   = errors.New("diam: raw fast path: unsupported Diameter version")
	errRawBadMsgLength = errors.New("diam: raw fast path: message length out of range")
)

// RawKey selects messages for a RawHandler by the two header fields that
// identify a message family. It is read from the fixed 20-byte header
// alone; the dictionary is never consulted.
type RawKey struct {
	AppID uint32
	Code  uint32
}

// RawHandler handles one complete raw Diameter message.
//
// hdr is decoded from the fixed 20-byte header. msg is the FULL message
// including that header. Both requests and answers whose (AppID, Code)
// match are delivered; dispatch on hdr.CommandFlags&RequestFlag.
//
// Handlers run synchronously on the connection's read goroutine, so the
// next message on the connection is not read until the handler returns.
//
// OWNERSHIP: msg aliases a pooled read buffer and is valid ONLY until the
// handler returns. Copy out anything that must outlive the call. Reply by
// writing pre-serialized bytes with c.Write.
type RawHandler func(c Conn, hdr *Header, msg []byte)

// maxRawMessageLength caps a raw message so a hostile or corrupt length
// field cannot drive an unbounded allocation. Diameter messages are far
// smaller in practice; 1 MiB is generous.
const maxRawMessageLength = 1 << 20

// rawBufferPool recycles read buffers for the raw fast path. Buffers
// larger than the default capacity are not returned to the pool.
var rawBufferPool = sync.Pool{
	New: func() interface{} { return make([]byte, 0, MessageBufferLength) },
}

func getRawBuffer(n int) []byte {
	b := rawBufferPool.Get().([]byte)
	if cap(b) < n {
		return make([]byte, n)
	}
	return b[:n]
}

func putRawBuffer(b []byte) {
	if cap(b) < MessageBufferLength {
		return
	}
	rawBufferPool.Put(b[:0]) //nolint:staticcheck // pooling a slice is intentional
}

// tryServeRaw peeks the fixed header on the buffered reader and, if a
// RawHandler is registered for the message's (AppID, Code), reads the
// whole message into a pooled buffer and invokes the handler.
//
// It returns handled=true when a raw handler consumed a message.
// handled=false means nothing was consumed and the caller must fall
// through to the stock decode path: this happens when there are no raw
// handlers, the connection is multi-stream (SCTP, which bypasses the
// buffered reader and cannot be peeked), or no handler matches the key.
// A non-nil err is a connection-level read/protocol failure and is
// reported exactly like a stock readMessage error.
func (c *conn) tryServeRaw() (handled bool, err error) {
	if len(c.server.RawHandlers) == 0 {
		return false, nil
	}
	// SCTP/MultistreamConn does its own buffering and is not peekable;
	// raw fast path is TCP-only (see NOTES.md §3 deviation 2).
	if _, isMulti := c.rwc.(MultistreamConn); isMulti {
		return false, nil
	}
	if c.server.ReadTimeout > 0 {
		c.rwc.SetReadDeadline(time.Now().Add(c.server.ReadTimeout))
	}

	hdrBytes, err := c.buf.Reader.Peek(HeaderLength)
	if err != nil {
		// Not enough bytes for a header yet, or the connection closed.
		// Surface as a read error so serve() reports/exits as it would
		// for the stock path (EOF is filtered there).
		return false, err
	}

	if hdrBytes[0] != 1 { // version
		return false, errRawBadVersion
	}
	msgLen := uint32(hdrBytes[1])<<16 | uint32(hdrBytes[2])<<8 | uint32(hdrBytes[3])
	if msgLen < HeaderLength || msgLen > maxRawMessageLength {
		return false, errRawBadMsgLength
	}

	key := RawKey{
		AppID: binary.BigEndian.Uint32(hdrBytes[8:12]),
		Code:  uint32(hdrBytes[5])<<16 | uint32(hdrBytes[6])<<8 | uint32(hdrBytes[7]),
	}
	h, ok := c.server.RawHandlers[key]
	if !ok {
		// Header inspected but not consumed; the stock path re-reads it.
		return false, nil
	}

	buf := getRawBuffer(int(msgLen))
	defer putRawBuffer(buf)
	if _, err := io.ReadFull(c.buf.Reader, buf); err != nil {
		return false, err
	}

	hdr := Header{
		Version:       buf[0],
		MessageLength: msgLen,
		CommandFlags:  buf[4],
		CommandCode:   key.Code,
		ApplicationID: key.AppID,
		HopByHopID:    binary.BigEndian.Uint32(buf[12:16]),
		EndToEndID:    binary.BigEndian.Uint32(buf[16:20]),
	}
	h(c.writer, &hdr, buf)
	poisonRaw(buf) // 0xDE-fills under -tags gycodec_poison; no-op otherwise
	return true, nil
}
