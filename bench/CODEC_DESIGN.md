# DESIGN: Static DCCA (Gy) Codec for go-diameter — Profiling & Implementation

**Status:** Draft — for Claude Code session use
**Scope:** Codec layer only. NOT the OCS application. NOT Sy. NOT rating logic.
**Owner:** Tibor
**Base:** Fork of `fiorix/go-diameter` (already carries ~30% parse/serialize improvement on nested grouped AVPs, e.g. DCCA MSCC)

---

## 1. Context and Goals

The OCS front-end will speak Diameter Gy (DCCA, Application-Id 4, with 3GPP
vendor AVPs) at high sustained CCR rates. The stock go-diameter codec is
dictionary-driven: runtime dictionary lookups per AVP, reflection-based
struct marshaling, and `interface{}` boxing. This generates per-message
garbage and indirection whose ceiling cannot be tuned away — the existing
30% fork improvement is near that ceiling.

**Goal:** Replace the codec for Application-Id 4 with a *generated static
codec* — direct offset arithmetic, no maps, no reflection, near-zero
allocations on the hot path — while keeping go-diameter's transport and
base-protocol layer (CER/CEA, DWR/DWA, DPR/DPA, watchdog, connection
supervision) untouched.

**Primary success metric:** allocations per message, not nanoseconds.
At target load (~100k TPS/site eventually), GC pressure from per-message
garbage is the limiting factor, not parse CPU.

### Goals
1. Establish a rigorous, reproducible baseline of the current (forked)
   dictionary codec: CPU profile, alloc profile, allocs/op, p50/p99.
2. Implement a static parser/serializer for the Gy message set
   (CCR/CCA, plus base messages remain on the stock codec).
3. Prove byte-exact round-trip equivalence against the dictionary codec.
4. Provide two profiling harnesses:
   - **Harness A (ser/deser):** pure in-memory encode/decode benchmarks.
   - **Harness B (client/server):** real sockets, real go-diameter peers,
     session-shaped Gy traffic (CCR-I → CCR-U×N → CCR-T).

### Non-Goals (do not implement)
- OCS business logic, quota management, rating, CDR generation.
- Sy / Ro-specific message flows beyond what DCCA shares (Ro reuses the
  same CCR/CCA structure; the generator output covers it for free, but no
  Ro-specific testing in this phase).
- A general-purpose replacement for go-diameter's dictionary system.
- TLS/DTLS, SCTP multi-homing tuning, DRA routing logic.

---

## 2. Architecture

### 2.1 Layering — what stays, what's replaced

```
+--------------------------------------------------------+
| Application handlers (test harness only in this phase) |
+--------------------------------------------------------+
| NEW: static DCCA codec (generated)        app-id 4     |
|      - gycodec package: structs + Parse/Append         |
|      - lazy top-level scan, eager MSCC decode          |
+--------------------------------------------------------+
| KEEP: go-diameter base protocol & peer state machine   |
|      CER/CEA, DWR/DWA, DPR/DPA, retransmit, timers     |
| KEEP: stock dictionary codec for base app (app-id 0)   |
+--------------------------------------------------------+
| KEEP: go-diameter transport (TCP; SCTP if available)   |
+--------------------------------------------------------+
```

Integration point: go-diameter delivers a raw message buffer per Diameter
message. The mux/handler registration is extended (or bypassed via a raw
handler) so that messages with Application-Id 4 and Command-Code 272 (CC)
are routed to the static codec instead of `diam.ReadMessage`'s dictionary
path. Everything else continues through the stock path.

> Implementation note for Claude Code: inspect how the fork currently
> surfaces raw buffers. If `diam.Message` is always fully decoded before
> handlers run, add a raw-bytes interception point at the connection read
> loop keyed on (AppID, CmdCode) from the fixed 20-byte header — the
> header alone is enough to route without touching the dictionary.

### 2.2 Generated codec — design

**Generator input:** the dictionary XML files already shipped with
go-diameter (base, credit-control, 3GPP/TGPP), filtered to a curated AVP
allowlist for Gy. Dictionary stays the source of truth; the generator is
run at build time (`go:generate`), output is committed.

**Generator output (package `gycodec`):**
- One Go struct per message: `CCR`, `CCA`.
- One struct per grouped AVP actually needed: `MSCC`
  (Multiple-Services-Credit-Control), `RequestedServiceUnit`,
  `GrantedServiceUnit`, `UsedServiceUnit`, `SubscriptionId`,
  `ServiceInformation` / `PSInformation`, `FinalUnitIndication`, etc.
- `ParseFrom(buf []byte) error` and `AppendTo(dst []byte) []byte`
  methods — direct offset arithmetic, length-prefix walking, no maps,
  no reflection, no `interface{}`.
- A generic `RawAVP { Code, VendorID uint32; Flags byte; Data []byte }`
  fallback list per struct for **unknown AVPs** (see 2.4).

**AVP allowlist (initial — extend as test vectors demand):**
Session-Id, Origin-Host, Origin-Realm, Destination-Host, Destination-Realm,
Auth-Application-Id, Service-Context-Id, CC-Request-Type, CC-Request-Number,
Subscription-Id (+Type/Data), Event-Timestamp, Multiple-Services-Credit-Control,
Requested/Granted/Used-Service-Unit (CC-Time, CC-Total-Octets,
CC-Input-Octets, CC-Output-Octets, CC-Service-Specific-Units),
Rating-Group, Service-Identifier, Validity-Time, Result-Code (top and
per-MSCC), Final-Unit-Indication (+Final-Unit-Action),
Trigger/Trigger-Type, Tariff-Time-Change, Service-Information,
PS-Information (3GPP-Charging-Id, 3GPP-PDP-Type, SGSN/GGSN-Address,
Called-Station-Id, 3GPP-RAT-Type, User-Equipment-Info), Origin-State-Id,
Termination-Cause, CC-Session-Failover, Credit-Control-Failure-Handling.

### 2.3 Parsing strategy — lazy top level, eager MSCC

- **Top-level scan:** single pass over the message body recording offsets
  of AVPs of interest into fixed struct fields. AVPs not needed by the
  handler remain as offset/length references into the original buffer.
- **Eager decode** only for: Session-Id, CC-Request-Type, CC-Request-Number,
  Subscription-Id, and each MSCC (the hot fields for an OCS).
- **String fields:** stored as `[]byte` sub-slices of the read buffer by
  default (zero-copy). Provide `Clone()` for copy-out.

### 2.4 Unknown AVPs and M-bit semantics (RFC 6733) — not optional

- Unrecognized AVPs are skipped structurally but **preserved** in the
  `RawAVP` fallback list so a serialized round-trip is byte-identical
  (modulo ordering rules — see equivalence testing, §5).
- M-bit handling: parser records `UnsupportedMandatory []RawAVP`
  separately. The *codec* does not answer-with-error itself (that is
  application policy), but it must surface enough information for a
  handler to build `DIAMETER_AVP_UNSUPPORTED (5001)` with Failed-AVP.
- P-bit/V-bit: V-bit drives 4-byte Vendor-ID presence in header parsing;
  get this right for 3GPP (Vendor-Id 10415) AVPs.
- Padding: AVP data is padded to 4-byte boundaries; length field excludes
  padding. Off-by-one here is the classic Diameter codec bug — fuzz it.

### 2.5 Buffer ownership — the rule, stated brutally

This is the highest-risk design decision. Zero-copy + `sync.Pool` +
retained slices = silent corruption under load.

**Rule:** A parsed `CCR`/`CCA` and every sub-slice within it is valid
ONLY until the handler callback returns. The read buffer is pooled and
reused immediately after. Any data needed beyond the callback MUST be
copied out via `Clone()` (deep copy into heap) before return.

- Enforce in tests: a build-tag-guarded "poison mode" that overwrites
  returned buffers with `0xDE` immediately after handler return, so any
  retained-slice bug fails loudly in CI instead of corrupting silently
  in production.
- Document the rule on every generated type's doc comment (generator
  emits it).

### 2.6 Serialization strategy

- `AppendTo(dst []byte) []byte` append-style API (like `strconv.Append*`)
  so callers control buffer reuse; pair with a `sync.Pool` of write
  buffers in the harness.
- Lengths: reserve-and-backpatch for message and grouped-AVP length
  fields (single pass, no pre-computation pass needed unless profiling
  says otherwise).
- AVP emission order: fixed, deterministic, matching the order commonly
  produced by the dictionary codec where feasible (simplifies byte-exact
  equivalence tests).

---

## 3. Phase Plan

Execute phases in order; each phase has an exit criterion. Do not start
Phase 2 before Phase 1's baseline numbers are committed to the repo.

### Phase 0 — Repo orientation (Claude Code, first session step)
- Map the fork: where the read loop lives, where dictionary decode is
  invoked, what the existing 30% optimization changed (diff against
  upstream `fiorix/go-diameter` master).
- Identify the raw-buffer interception point (see §2.1 note).
- **Exit:** short `NOTES.md` describing the decode path call graph.

### Phase 1 — Baseline profiling (ser/deser, Harness A)
- Build a corpus of realistic Gy messages as golden binary fixtures:
  - CCR-I (session start, 1 MSCC with RSU)
  - CCR-U with 1, 3, and 5 MSCCs (USU + RSU each, mixed unit types)
  - CCR-U with Trigger + Tariff-Time-Change (the nasty nested case)
  - CCR-T with final USU
  - CCA variants with GSU, Validity-Time, FUI/Final-Unit-Action, MSCC-level
    Result-Codes (2001 and 4012)
  - One message carrying deliberately unknown vendor AVPs (forward-compat)
  Generate fixtures *with the existing dictionary codec* so they double as
  equivalence references. Store as `.bin` + hex-dump `.txt` pairs.
- Benchmarks (`testing.B`, `b.ReportAllocs()`):
  - `BenchmarkDictParse{1,3,5}MSCC`, `BenchmarkDictSerialize...`
  - Capture: ns/op, B/op, allocs/op; `pprof` CPU and alloc profiles
    committed under `bench/baseline/`.
- **Exit:** baseline table in `BENCHMARKS.md`. Expect the profile to show
  whether remaining time is grouped-AVP recursion (→ generator pays off
  big) or already syscall-bound (→ temper expectations, batching matters
  more).

### Phase 2 — Generator + static codec
- Write `cmd/gycodegen`: parses dictionary XML, applies allowlist,
  emits `gycodec/*.gen.go`. Keep the generator boring and readable —
  it is a build tool, not a hot path.
- Implement parse (lazy top level, eager MSCC) and serialize
  (append-style, backpatched lengths).
- Unknown-AVP fallback + M-bit surfacing per §2.4.
- **Exit:** all golden fixtures round-trip; equivalence suite (§5) green;
  Harness A re-run shows allocs/op at or near zero for parse of pooled
  messages. Target: ≥5× improvement in allocs/op vs baseline on the
  3-MSCC CCR-U; treat anything less as a design smell to investigate,
  not a failure to hide.

### Phase 3 — Client/server profiling harness (Harness B)
- Two binaries under `cmd/`:
  - `gyserver`: go-diameter server, static codec mounted for app-id 4,
    answers CCR with canned CCA (echo Session-Id, grant fixed GSU,
    per-MSCC 2001). No business logic.
  - `gyclient`: load generator driving session-shaped traffic:
    configurable sessions, CCR-U interval, MSCC count per request,
    target TPS; closed-loop (wait for CCA) with concurrency knob.
- Metrics: client-side latency histogram (HDR or simple p50/p95/p99
  capture), server-side `runtime.ReadMemStats` snapshots, GC pause log
  (`GODEBUG=gctrace=1` runs), allocs/op via pprof during steady state.
- Comparison runs: dictionary codec vs static codec, same traffic shape,
  same hardware, ≥60 s steady state after warmup, 3 repetitions.
- **Exit:** comparison table in `BENCHMARKS.md` (TPS ceiling at fixed
  latency budget, p99 at fixed TPS, GC pause behavior).

### Phase 4 (optional, later) — wire into OCS skeleton
Out of scope for this document. Listed only so the codec API is designed
with a real consumer in mind (handler gets parsed CCR, returns CCA builder).

---

## 4. Benchmark Methodology (applies to all phases)

- Pin Go version in `go.mod` toolchain directive; record GOMAXPROCS,
  CPU model, kernel, and governor in every benchmark report.
- Harness A: `go test -bench -benchmem -count=10`, compare with
  `benchstat`; never eyeball single runs.
- Harness B: run client and server on separate machines if possible
  (or at minimum separate, pinned core sets via taskset) so the load
  generator doesn't steal cycles from the system under test.
- Report **allocs/op first**, ns/op second, p99 third. A result that
  improves ns/op but not allocs/op is not a win for this project.
- Keep loopback runs labeled as loopback; do not present loopback TPS
  as a deployment claim. (Any externally communicated performance figure
  needs a real-network run and human review before it goes into a
  position paper or customer material.)

---

## 5. Testing & Correctness

1. **Round-trip equivalence vs dictionary codec.** For every golden
   fixture: `staticParse → staticSerialize` must be byte-identical to
   input; `dictParse → dictSerialize` output must be parseable by the
   static codec and vice versa (cross-parse equality on field values,
   since AVP ordering may differ between codecs — compare canonical
   field sets, not bytes, for the cross direction).
2. **Fuzzing.** `go test -fuzz` on `ParseFrom` seeded with the golden
   corpus. Invariants: no panics, no out-of-bounds, declared lengths
   never exceed buffer, padding handled, parser rejects messages whose
   header length disagrees with buffer length. Differential fuzzing:
   where the dictionary codec accepts an input, the static codec must
   not crash (it may legitimately differ on strictness — log and review
   differences rather than auto-failing).
3. **Poison-mode tests** for buffer ownership (§2.5).
4. **M-bit conformance table tests**: known-mandatory unknown AVP →
   surfaced in `UnsupportedMandatory`; non-mandatory unknown → preserved
   in `RawAVP` list, round-trips intact.
5. **Truncation/garbage tests**: every fixture truncated at every 4-byte
   boundary must return an error, never panic (cheap table test, huge
   payoff — this is the parser's security surface; a malformed-message
   panic is a remote DoS on the OCS front door).

---

## 6. Trade-offs and Decisions

| Decision | Chosen | Rejected alternative | Why |
|---|---|---|---|
| Codec scope | Generated static codec for app-id 4 only | Rewrite all of go-diameter's codec | Gy is the hot path; base-protocol traffic is negligible; smaller blast radius |
| Source of truth | Dictionary XML → generator | Hand-written parser | Hand-written speed with maintainability; Sy later = re-run generator |
| String handling | Zero-copy sub-slices + explicit Clone | Always copy | Allocs/op is the success metric; copy-out stays available where needed |
| Unknown AVPs | Preserve as RawAVP, surface M-bit | Strict schema rejection | Interop reality: DRAs and P-GWs send AVPs you didn't plan for |
| Integration | Raw-buffer interception keyed on header | Replace go-diameter mux wholesale | Keeps hardened peer state machine and the existing fork investment |
| Lengths | Backpatch on serialize | Two-pass size computation | One pass, simpler; revisit only if profiling objects |

**Known risks**
- *Buffer lifetime bugs* — mitigated by poison mode + brutal documented rule.
- *go-diameter internals may not expose a clean raw hook* — Phase 0 exists
  to find out; worst case, a thin patched read loop in the fork.
- *Equivalence ambiguity from AVP ordering* — handled by canonical-field
  comparison for cross-codec tests.
- *Generator scope creep* — allowlist is curated; resist generating the
  whole 3GPP dictionary.
- *License/IP hygiene* — fork of permissively licensed code embedded in a
  commercial product: confirm the exact LICENSE terms and attribution
  requirements, and route through OSS-compliance sign-off. (Not legal
  advice; needs human review.)

**Revisit as it grows:** if Phase 1 shows the decode path is already
syscall/read dominated, deprioritize Phase 2 micro-optimizations and add a
read-batching experiment (single read → multiple Diameter messages per
syscall) to Phase 3 instead.

---

## 7. Repository Layout (target)

```
/cmd
  /gycodegen      # generator: dictionary XML -> gycodec/*.gen.go
  /gyserver       # Harness B server
  /gyclient       # Harness B load generator
/gycodec          # generated static codec + hand-written runtime helpers
  avp_raw.go      # RawAVP, header walking, padding, M-bit helpers
  *.gen.go        # generated structs + ParseFrom/AppendTo
/bench
  /fixtures       # golden .bin + .txt hex dumps
  /baseline       # Phase 1 pprof profiles + benchstat output
BENCHMARKS.md
NOTES.md          # Phase 0 decode-path call graph
DESIGN.md         # this file
```

---

## 8. Instructions for the Claude Code Session

1. Start with Phase 0. Do not write codec code before the baseline exists.
2. Every benchmark claim goes into `BENCHMARKS.md` with the exact command,
   environment, and `benchstat` output — reproducibility over speed.
3. When touching the fork's read loop, keep changes behind a minimal diff
   against upstream; annotate with `// FORK:` comments.
4. Never weaken the truncation/fuzz error handling to make a benchmark
   faster. Parser robustness is non-negotiable — this code will face the
   public side of a charging system.
5. If a design assumption in this document conflicts with what the code
   actually does (e.g., no clean raw hook), stop and record the conflict
   in NOTES.md with options, rather than silently working around it.
