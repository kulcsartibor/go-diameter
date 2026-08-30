# go-diameter findings to push upstream

**Fork-local tracking document — not intended for inclusion in upstream PRs.**

Bugs and design gaps in the Go implementation, discovered while building the
Rust port. The Rust code now lives in its own repository
(`../rs-diameter`, split out with history); the paths referenced below for
Rust evidence are relative to THAT repo. Each entry: what, where, evidence,
and the fix to reimplement on a topic branch off `main` as a self-contained
upstream PR.

## Status of the rebase onto upstream (2026-08-22)

`main` has been rebased onto `upstream/main`, which had advanced by six
commits. Two of them interact with the findings below and **change the scope
of what still needs upstreaming** — read this before opening any PR:

- **`ad16c7e` "fix: AVP decoding panic on dictionary/wire type mismatch
  (#249)"** fixes the SAME PANIC CLASS as finding #2, in different places:
  `diam/avp.go` (`DecodeFromBytes` falls back to `datatype.Unknown` on a
  type/length mismatch so `AVP.Len()` reports the wire size) and
  `diam/group.go` (`DecodeGroupedFromBytes` breaks out when a fatal decode
  leaves `avp.Data == nil`). Our fix (#2) covers a *different* code path —
  the top-level `decodeAVPs` loop in `diam/message.go` plus a nil guard in
  `AVP.Len()` — so the two are complementary defense-in-depth, and both
  coexist cleanly (verified: our regression table, seed corpus and a 30 s
  `FuzzReadMessage` run all pass on the rebased tree). **Re-scope the PR
  description as "remaining gaps after #249" rather than presenting it as
  the first fix for this class.**
- **`bd06a62` "dict: resolve unknown vendor AVPs to Unknown, not a
  same-code base AVP (#255)"** touches dictionary resolution. Verified it
  does NOT change the generated static codecs (`go generate ./staticodec`
  is byte-identical after the rebase), but keep it in mind for finding #5.

Legend: **[confirmed bug]** = wrong behavior with a reproducer;
**[design gap]** = works but is a latent hazard the Rust design corrects.

---

## 1. [confirmed bug] DWA never carries Origin-State-Id

- **Where:** `diam/sm/dwr.go:40` (in `handleDWR`).
- **What:** The Origin-State-Id AVP is appended to the *received request*
  `m.NewAVP(...)` instead of the *answer* `a.NewAVP(...)`. Lines 36–37 above
  correctly use `a`; line 40 uses `m`. So a configured Origin-State-Id is
  never emitted on the DWA wire message despite the code's clear intent.
- **Evidence:** Found during Rust peer interop; the Rust `build_dwa`
  deliberately does not reproduce it (see
  `../rs-diameter/crates/diameter-peer/src/base.rs`, note near `build_dwa`).
- **Fix:** change `m.NewAVP(avp.OriginStateID, ...)` → `a.NewAVP(...)`.
  Add a `diam/sm` test asserting the DWA wire bytes contain Origin-State-Id
  when configured. One-line change + regression test.
- **Status:** FIXED on this fork in commit `c565f1c` ("sm: send
  Origin-State-Id on DWA, not back into the request"), with
  `TestHandleDWR_OriginStateID` (verified failing before the fix).
  Ready to upstream as-is. Note upstream's `1f4309b` edited this same
  function (adding the OnDWA hook) without spotting the bug, so it is
  still present on `upstream/main`.

## 2. [confirmed bug — already FIXED on this fork] decodeAVPs remote-DoS panic

- **Where:** `diam/message.go` `decodeAVPs` / `diam/avp.go` `(*AVP).Len`.
- **What:** On a `DecodeError`, `decodeAVPs` appended the partially-decoded
  AVP and called `a.Len()` on it → nil-deref panic from the network
  (remote DoS on the charging front door). A zero/garbage AVP length could
  also fail to advance the loop.
- **Status:** ALREADY FIXED on this fork in commit `eb709b3` (was `4ca0d6c` before the upstream rebase)
  ("diam: fix remote-DoS panic on malformed AVPs in decodeAVPs"), with a
  regression table test and `FuzzReadMessage` (`diam/message_fuzz_test.go`).
- **Upstream action:** this commit is the upstream PR — cherry-pick `eb709b3`
  onto master. Listed here so it isn't forgotten in the upstream batch.

## 3. [design gap] Unbounded grouped-AVP recursion on decode

- **Where:** `diam/group.go:32` `DecodeGroupedFromBytes` (recurses via
  `DecodeAVP` with no depth limit); reached from `decodeAVPs`.
- **What:** Deeply/maliciously nested Grouped AVPs recurse without bound —
  a crafted message can drive the decoder to a stack-exhaustion crash
  (remote DoS class, same front door as #2).
- **Evidence:** The Rust dynamic decoder caps nesting
  (`MAX_GROUP_DEPTH = 8`, `CodecError::GroupDepthExceeded`) with a
  recursion-bomb test (`../rs-diameter/crates/diameter-dictionary/tests/malformed.rs`).
- **Fix:** thread a depth counter through `DecodeGroupedFromBytes` /
  `decodeAVPs` (or the `dict`-aware decode path), return a decode error past
  a sane cap (8–16). Add a recursion-bomb test. Security-relevant.

## 4. [design gap] No raw-bytes handler hook; decode welded to transport

- **Where:** `diam/server.go` read loop → `dispatch` → `ServeMux` always
  hands handlers a fully dictionary-decoded `*diam.Message`.
- **What:** There is no way to receive an undecoded frame and route on the
  fixed 20-byte header before the dictionary codec runs. On the Rust-static
  branch we had to fork-patch the read loop (`diam/rawhook.go`) to bypass it.
- **Rust correction:** routing on `(app_id, code)` from the header, static
  raw-byte hooks first-class (../rs-diameter/PLAN.md F1).
- **Upstream shape (optional):** add an opt-in `RawHandler`/`RawKey`
  registration on `Server`/`ServeMux` that fires from the read loop before
  decode, leaving the stock path unchanged for everything else. The
  `diam/rawhook.go` patch on branch `feat/rust-static-codec` is a working
  reference implementation to clean up and upstream. Larger change; propose
  as a design discussion, not a drive-by.

## 5. [design gap] Global mutable dictionary + per-decode threading

- **Where:** `diam/dict` — `dict.Default`, `init()` loading, `sync.Mutex`
  in `Parser`; the dictionary is threaded through every decode call.
- **What:** Global mutable state; a lock on a structure that is effectively
  read-only after startup; awkward to run multiple independent dictionaries.
- **Rust correction:** parse once into an immutable snapshot shared as
  `Arc<Dictionary>`, no globals, no locks on the read path (../rs-diameter/PLAN.md F6).
- **Upstream shape:** hard to change without breaking the public API;
  probably a v5 item. Record as a known wart, not an easy PR.

## 6. [design gap] Fixed 1 KiB read-buffer pool

- **Where:** `diam/message.go:25` `MessageBufferLength = 1 << 10`; pooled
  reader buffers of this fixed size.
- **What:** Messages larger than 1 KiB allocate a fresh buffer per message
  (measured: shows up in the alloc profile for 5-MSCC CCR-U).
- **Rust correction:** per-connection buffer growing to the observed
  high-water mark, capped at max-message-size (../rs-diameter/PLAN.md F7).
- **Fix (small):** grow-and-reuse the pooled buffer to a per-connection
  high-water mark instead of a global constant; keep an upper bound.

## 7. [dictionary quirk, not a code bug] MSCC declared max="1"

- **Where:** `diam/dict/testdata/credit_control.xml:251`
  (`Multiple-Services-Credit-Control` rule `max="1"` on CCR/CCA).
- **What:** RFC 4006 allows multiple MSCCs; the shipped dictionary says
  `max="1"`. Harmless in practice because go-diameter does not enforce
  command rules on decode/encode, but it is technically non-conformant and
  could confuse anyone who *does* enforce rules.
- **Status:** FIXED on this fork in commit `5a23043`. The bound was
  removed from both copies — `diam/dict/testdata/credit_control.xml` and
  the embedded duplicate in `diam/dict/default.go` (no generator keeps
  those in sync). Verified inert: `Rule.Max` is only read by
  printCommand/printAVP for the dictionary dump, never enforced on
  decode/encode. Guarded by `TestCreditControlMSCCUnbounded`.

---

## 8. [design gap] `Message.Answer` gives no way to satisfy RFC 6733 §6.2

- **Where:** `diam/message.go:531` (`func (m *Message) Answer(resultCode uint32) *Message`).
- **What:** §6.2, "Diameter Answer Processing", lists what a node that
  locally processes a request MUST put in the answer. Two of those items
  are copied from the request:

  > If the Session-Id is present in the request, it MUST be included in the
  > answer.

  > Any Proxy-Info AVPs in the request MUST be added to the answer message,
  > in the same order they were present in the request.

  `Answer` clones the header and appends Result-Code. It copies neither,
  and the library ships no helper that does. Grepping the whole tree for
  `Proxy-Info` finds only dictionary XML — there is no code path anywhere
  in go-diameter that reads or writes AVP 284.

  This is **not a bug in `Answer`**: its doc comment promises "an answer
  for the current Message with optinal ResultCode AVP" and that is exactly
  what it delivers. It is a gap in what the library makes *possible to get
  right by default* — every consumer must independently know §6.2 and
  reimplement it, and the evidence is that they do not.

- **Evidence that it is a footgun, not a theoretical one:** three
  independent codebases built on this library got it wrong the same way.
  Our Rust DRA (rs-dra) before `02453cc`, and both Go consumers today —
  `diameter/diam-session-helper` (four locally built answers) and
  `ng-apps/ocs-stack` (seven, across gy-server and sy-server). Upstream's
  own `diam/sm/client_test.go:283` does `m.Answer(diam.UnableToComply).WriteTo(c)`
  with nothing added, which is the shape everyone copies.

  The Session-Id half is usually caught, because an answer without one is
  immediately malformed and fails in the first integration test. Both Go
  repos carry a comment saying exactly that. **Proxy-Info is not caught,
  because it only breaks a third party.** A proxy adds Proxy-Info (§6.7.2)
  to park local state it needs to recover its context when the answer
  returns; the Proxy-State inside is opaque to everyone else, and the
  message *is* the recovery channel. Drop it and that agent is stranded,
  silently, and only in a deployment that has such an agent in the path —
  so it never shows up in anyone's own test suite.

- **Rust evidence:** `crates/diameter/src/router.rs`, `answer_echo` and
  `build_answer` (rs-diameter `02453cc`). One walk of the request collects
  the Session-Id and every Proxy-Info in order; the AVPs are copied
  verbatim including flags, since this is relaying another node's data
  rather than re-encoding our own; vendor 0 only, because a vendor-tagged
  284 is a different AVP. Tests in
  `crates/diameter/tests/fallback_and_answers.rs` pin order, Session-Id
  staying first (§8.8), the vendor-tagged case, and truncation at every
  offset.

- **Proposed fix, non-breaking:** leave `Answer` alone — code depends on it
  being a bare header clone — and add a sibling that does §6.2:

  ```go
  // AnswerConformant creates an answer carrying what RFC 6733 §6.2
  // requires be copied from the request: the Session-Id, if present, as
  // the first AVP (§8.8), and every Proxy-Info in request order.
  func (m *Message) AnswerConformant(resultCode uint32) *Message
  ```

  Then fix `Answer`'s doc comment to say what it does *not* do, and point
  at the new one. Even the doc change alone would be worth the PR: the
  current text reads as though it produces a usable answer.

  Two details the implementation must get right, both learned the hard way:
  copy the Proxy-Info groups **verbatim** rather than decoding and
  re-encoding them (the Proxy-State inside is opaque and must survive byte
  for byte), and bound the copy against the 3-byte message length — a
  request sitting just under the limit can push the answer over it, and an
  answer that cannot be encoded is worse than one missing an AVP.

  Note the upstream typo while in there: "optinal".

- **Status:** open. Fixed in Rust (`02453cc`); notes filed for both Go
  consumers at `docs/PROXY_INFO_ECHO.md` in each repository.

---

## Not bugs — deliberate Rust divergences (do NOT "fix" in Go)

Recorded so a future reader doesn't mistake them for Go defects:

- **Sequential dispatch default** (`diam/server.go`, `MaxConcurrentHandlers`
  defaults to 0 = sequential): a valid conservative default, not a bug. Rust
  defaults to bounded-concurrent because HbH correlation makes ordering
  unnecessary (../rs-diameter/PLAN.md F4). Go's is a design choice; leave it.
- **One write syscall per message**: fine for Go's model; the Rust writer
  coalesces (../rs-diameter/PLAN.md F5). Not a Go defect.
- **DWA omits Origin-State-Id in the plan's field list**: that omission in
  the Rust plan is *because of* bug #1 above (the Go DWA never carries it in
  practice), so the Rust output matches observed Go behavior. Once #1 is
  fixed upstream, revisit whether Rust should also emit it on DWA.

---

## Upstream push order (after Rust completion)

1. `eb709b3` (already committed on main) — the decodeAVPs DoS fix. Cleanest first PR.
2. `c565f1c` (already committed on main) — #1 DWA Origin-State-Id, one-liner + test.
3. #3 grouped-AVP recursion cap — small, security-relevant.
4. #6 read-buffer high-water mark — small perf/robustness.
5. `5a23043` (already committed on main) — #7 dictionary max="1".
6. #8 §6.2 answer conformance — split it: the doc-comment correction on
   `Answer` is a trivial PR that stands alone and is worth landing first;
   the `AnswerConformant` helper is an API addition and will want
   discussion.
7. #4 raw handler hook — larger, propose as design discussion (reference
   `diam/rawhook.go` on `feat/rust-static-codec`).
8. #5 immutable dictionary — v5/API-break territory; discuss, don't rush.
