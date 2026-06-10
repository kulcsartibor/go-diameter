# NOTES — Phase 0 repo orientation (static Gy codec)

Companion to `codec-design.md`. Records the decode-path call graph, the prior
fork optimization, and conflicts between the design doc's assumptions and the
actual code (per design doc §8.5). Line numbers refer to branch
`feat/gy-static-codec` forked at upstream `3e2be62`.

## 1. Decode path call graph (inbound message)

```
(c *conn) serve()                       diam/server.go:186
  └─ (c *conn) readMessage()            diam/server.go:171
       └─ ReadMessage(r, dict)          diam/message.go:68
            ├─ buf from readerBufferPool        message.go:42 (sync.Pool, 1024 B)
            ├─ (m *Message) readHeader()        message.go:93
            │    ├─ io.ReadFull 20 bytes
            │    ├─ DecodeHeader                diam/header.go:34
            │    └─ dict.FindCommand            ← unknown command ⇒ error
            └─ (m *Message) readBody()          message.go:121
                 ├─ io.ReadFull (MessageLength−20) bytes
                 └─ (m *Message) decodeAVPs()   message.go:156
                      └─ DecodeAVP per AVP      diam/avp.go:55
                           ├─ dict.FindAVPByCode        avp.go:95
                           ├─ DecodeGroupedFromBytes    (grouped, recursive)
                           └─ datatype.Decode           (scalar)
  └─ (c *conn) dispatch(m)              diam/server.go:235
       └─ ServeMux.ServeDIAM            diam/server.go:488
            └─ handler lookup by CommandIndex{AppID, Code, Request}
```

Handlers always receive a fully decoded `*diam.Message`. There is **no**
raw-bytes hook anywhere between socket read and handler dispatch.

- TCP connections: socket wrapped in `bufio.NewReader` (server.go:161),
  field `c.buf.Reader` — supports `Peek`.
- SCTP (`MultistreamConn`): bypasses `c.buf`, has its own buffering
  (server.go:175-178) — no `Peek`.
- Client side: all `diam.Dial*` variants construct an internal `*Server`
  and reuse `srv.newConn` + `go c.serve()` (client.go:85-89, 162-166,
  192-196), so a `Server`-level hook applies to client connections too.
  `(srv *Server).Dial(timeout)` exists at client.go:177.
- Write path: `response.Write([]byte)` (server.go:282) writes raw bytes
  directly to the (buffered) socket — pre-serialized answers can be sent
  as-is, no `*diam.Message` required.
- `sm.StateMachine` registers CER/CEA/DWR handlers for app-id 0 on its
  internal mux and allows `HandleIdx` for other app-ids, wrapped in
  `handshakeOK` (sm/sm.go:159-168, 206-216).

## 2. What the existing fork optimization changed (commit 454c08c)

"Optimise AVP decoding and add typed decoder registration" — the ~30%
improvement referenced by the design doc (measured: −34% allocations,
−26% latency on ReadMessage):

1. Typed `dict.FindAVPByCode(appid, code, vendorID uint32)` — removes
   `interface{}` boxing/type switch in the inner decode loop (dict/util.go).
2. `mergeInheritedAVPs()` at `Load()` time — parent-chain lookups
   (app 4 → 1 → 0) flattened into one map access (dict/parser.go).
3. Array-indexed decoder dispatch for built-in TypeIDs, replacing map
   lookup; `RegisterDecoder` API for overrides (datatype/decoder.go).
4. Pre-allocated sentinel errors on the AVP decode path (avp.go).
5. `putUint24` direct 3-byte write, zero-alloc header/AVP serialization
   (uintconv.go, header.go); `DecodeGroupedFromBytes` skips the
   intermediate `datatype.Grouped` copy (group.go).

This is the ceiling of dictionary-driven decoding: per-AVP dictionary
lookups, `datatype.Type` interface boxing, and `[]*AVP` allocation remain.
The static codec replaces this whole path for app-id 4; the `putUint24`
and sentinel-error patterns are mirrored in `gycodec`.

## 3. Design-doc conflicts / deviations (per §8.5)

1. **No raw hook exists.** The doc's §2.1 implementation note anticipated
   this ("worst case, a thin patched read loop in the fork"). Resolution:
   `Peek(HeaderLength)` on the buffered reader in `serve()` before
   `readMessage()`, keyed on (AppID, CmdCode) from the fixed header,
   behind a `Server.RawHandlers` registry (Phase 3). The dictionary is
   never consulted for hooked messages; all other traffic takes the
   unmodified stock path.
2. **Raw fast path is TCP-only.** `MultistreamConn` (SCTP) cannot `Peek`;
   SCTP connections fall through to the dictionary path. Limitation
   accepted for this phase.
3. **Raw handlers bypass CER/CEA handshake gating.** `handshakeOK`
   wrapping lives in the mux; the raw hook fires at the read loop.
   Acceptable for the profiling harness; a production OCS must gate in
   the handler (or the hook must consult peer state). Flagged in
   BENCHMARKS.md and the Phase 3 code comments.
4. **`credit_control.xml` declares MSCC `max="1"`** on CCR/CCA command
   rules — non-conformant with RFC 4006 (multiple MSCCs allowed). Harmless
   here: go-diameter does not enforce command rules on decode or encode,
   so multi-MSCC fixtures are constructible. Do not edit the XML.
5. **`readerBufferPool` buffers are 1024 B** (`MessageBufferLength`).
   Larger messages (likely the 5-MSCC CCR-U) force a fresh `make` per
   message in `readerBufferSlice` on the stock path — expect this in the
   baseline alloc profile.
6. **`diam.NewMessage` randomizes HopByHop/EndToEnd when 0**
   (message.go:180). Fixture generation must set explicit IDs for
   byte-stable output.
7. **Generator input:** `dict/skel.go` XML structs are exported and
   reusable, but `Data.Type` is `xml:"-"` — the generator maps the
   `TypeName` string to a wire kind itself, mirroring the Time epoch
   convention of `diam/datatype/time.go`.

## 4. Test baseline on this machine (darwin/arm64)

`go test ./...` is green except `TestServerClose/sctp`, which fails with
"SCTP is unsupported on darwin/arm64" — a pre-existing platform
limitation, not a regression. This is the reference state for the
Phase 3 stock-path regression gate.

## 5. Fork diff scope vs upstream

At branch point, `main` == upstream `fiorix/go-diameter` master
(`3e2be62`); the fork's prior optimizations (454c08c et al.) are already
merged upstream. New fork-only surface introduced by this work (Phase 3)
is annotated with `// FORK:` comments and kept to:
`diam/rawhook.go` (new), two small hunks in `diam/server.go`, one
additive method in `diam/sm/client.go`.

## 6. Pre-existing remote-DoS in the dictionary decoder (found by fuzzing)

Phase 2 fuzzing of the static parser (`gycodec.FuzzParseFrom`, whose
differential arm calls `diam.ReadMessage`) surfaced a **panic in the
stock dictionary codec** on malformed input — a remote DoS, since the
decoder faces untrusted peers. The static codec rejects the same input
cleanly; this is purely a stock-path bug.

Reproduction:

```go
in := []byte("0\x00\x01\x000\x00\x01\x020000...0000") // bad version/lengths
diam.ReadMessage(bytes.NewReader(in), dict.Default)     // panics
```

Crash path: `ReadMessage` → `readBody` (message.go:143) → `decodeAVPs`
(message.go:156) → nil deref in `(*AVP).Len()` (avp.go:156). Likely
cause: on a `DecodeError`, `decodeAVPs` (message.go:160-171) does not
return — it appends the partially-decoded `*AVP` and calls `a.Len()` to
advance, dereferencing a nil/incomplete `Data`.

This is **out of scope** for the static-codec work and is tracked as a
separate task. The `gycodec` fuzz harness recovers around the
differential `diam.ReadMessage` call so the stock bug cannot mask or
fail a static-codec finding (see `gycodec/fuzz_test.go`,
`dictParseNoPanic`). The fix belongs in `diam/`, not here.
