# BENCHMARKS — static Gy codec project

All numbers below are **loopback/in-memory development measurements** on a
laptop. They support codec design decisions only; no figure here is a
deployment or capacity claim. Any externally communicated performance figure
requires a real-network run on representative hardware **and human review**
(codec-design.md §4).

Reporting order per the design doc: **allocs/op first**, B/op second, ns/op
third. A change that improves ns/op but not allocs/op is not a win for this
project.

---

## Phase 1 — Dictionary-codec baseline

### Environment

| | |
|---|---|
| Date | 2026-06-10 |
| Commit | branch `feat/gy-static-codec`, fixtures as of this commit |
| Go | go1.26.2 darwin/arm64 |
| CPU | Apple M1 Max (10 cores), GOMAXPROCS=10 |
| OS | macOS, Darwin kernel 25.5.0 |
| CPU governor | n/a (macOS; no pinning — laptop measurement, see caveat above) |

### Commands

```
go run bench/fixtures/gen.go                      # regenerate fixtures (byte-stable)
go test ./bench -bench . -benchmem -count=10 > bench/baseline/bench-raw.txt
benchstat bench/baseline/bench-raw.txt > bench/baseline/benchstat.txt
go test ./bench -bench . -benchmem -count=1 \
  -cpuprofile bench/baseline/cpu.pprof -memprofile bench/baseline/mem.pprof
```

### Results (benchstat over 10 runs)

| Benchmark | allocs/op | B/op | sec/op |
|---|---:|---:|---:|
| DictParse1MSCC | 51 | 1793 | 2.12 µs ±16% |
| DictParse3MSCC | 78 | 2649 | 3.18 µs ±12% |
| DictParse5MSCC | 112 | 3674 | 4.31 µs ±7% |
| DictParseTriggerTTC | 61 | 2153 | 2.56 µs ±3% |
| DictParseCCRInitial | 58 | 2009 | 2.29 µs ±4% |
| DictParseCCA | 60 | 2105 | 2.33 µs ±8% |
| DictSerialize1MSCC | 20 | 720 | 0.97 µs ±1% |
| DictSerialize3MSCC | 30 | 1032 | 1.52 µs ±4% |
| DictSerialize5MSCC | 42 | 1464 | 2.30 µs ±10% |
| DictSerializeTriggerTTC | 25 | 904 | 1.33 µs ±0% |
| DictSerializeCCA | 26 | 768 | 1.07 µs ±1% |

allocs/op and B/op are exact (±0% across all 10 runs). Raw output:
`bench/baseline/bench-raw.txt`; profiles: `bench/baseline/{cpu,mem}.pprof`.

### Profile analysis (the §6 "revisit" check)

The alloc profile is dominated by the codec, not by I/O — these are
in-memory benchmarks, and within them allocation concentrates exactly where
codec-design.md predicted:

| alloc_space | flat% | cum% |
|---|---:|---:|
| `diam.DecodeAVP` | 34.8% | 52.2% |
| `diam.(*Message).Serialize` | 18.0% | 38.7% |
| `diam.(*GroupedAVP).Serialize` | 12.8% | 15.8% |
| `diam.DecodeGroupedFromBytes` | 10.5% | 31.6% |
| `diam.(*Message).readBody` | 5.9% | 58.2% (cum incl. children) |

i.e. per-AVP allocation in grouped-AVP recursion (`*AVP` structs,
`datatype.Type` boxing, `[]*AVP` growth) — **the generator approach pays
off; proceed with Phase 2 as planned.** No read-batching pivot needed at
this stage.

Notes:
- `mallocgc` accounts for ~14% of CPU samples in the profile run — GC
  pressure is visible even at benchmark scale.
- Correction to an expectation recorded in NOTES.md §3(5): the 5-MSCC CCR-U
  fixture is 628 bytes and **fits** the 1024 B `readerBufferPool` buffer;
  no oversized-buffer allocation occurs for this corpus. The remaining
  allocations are per-AVP, not per-message-buffer.
- Phase 2 exit gate derived from this table: static parse of `ccr_u_3mscc`
  must come in at **≤ 15 allocs/op** (≥5×); the design target is ~0
  steady-state.

---

## Phase 2 — Static codec vs dictionary baseline

Same environment as Phase 1 (Apple M1 Max, go1.26.2, loopback/in-memory).
Static benchmarks reuse one message struct and one output buffer across
iterations — the OCS handler's intended pooled usage. benchstat over 10
runs; raw: `bench/baseline/phase2-compare-raw.txt`,
summary: `bench/baseline/phase2-benchstat.txt`.

### allocs/op — the success metric

| Benchmark | dict | static | reduction |
|---|---:|---:|---:|
| Parse 1 MSCC | 51 | **0** | ∞ |
| Parse 3 MSCC | 78 | **0** | ∞ |
| Parse 5 MSCC | 112 | **0** | ∞ |
| Parse Trigger/TTC | 61 | **0** | ∞ |
| Parse CCR-Initial | 58 | **0** | ∞ |
| Parse CCA | 60 | **0** | ∞ |
| Serialize 1 MSCC | 20 | **0** | ∞ |
| Serialize 3 MSCC | 30 | **0** | ∞ |
| Serialize 5 MSCC | 42 | **0** | ∞ |

The static codec allocates **nothing** on the steady-state hot path:
parse writes into a reused struct (sub-slices alias the input buffer),
serialize appends into a reused buffer. The Phase 2 exit gate (≤15
allocs/op, i.e. ≥5× on the 3-MSCC CCR-U) is met with margin — the design
target of ~0 is achieved outright.

### B/op and ns/op (secondary)

| Benchmark | dict B/op | static B/op | dict ns/op | static ns/op | speedup |
|---|---:|---:|---:|---:|---:|
| Parse 1 MSCC | 1793 | 0 | 2007 | 119 | 17× |
| Parse 3 MSCC | 2649 | 0 | 2998 | 188 | 16× |
| Parse 5 MSCC | 3674 | 0 | 4158 | 275 | 15× |
| Parse Trigger/TTC | 2153 | 0 | 2432 | 137 | 18× |
| Serialize 3 MSCC | 1032 | 0 | 1499 | 166 | 9× |

Caveat (restated): loopback/in-memory laptop measurements. The ns/op
speedups are real for the codec in isolation but say nothing about
end-to-end TPS under syscall/network load — that is Phase 3's job, and
no figure here is a deployment claim.

## Phase 3 — Harness B client/server comparison

`gyserver` + `gyclient` (cmd/gyserver/README.md) over **loopback on one
host** — client and server contend for the same cores, so this is a
codec-vs-codec signal, **not** a capacity claim (see the caveat at the
top of this file and codec-design.md §4). Same traffic for both codecs:
64 closed-loop workers, 3 MSCC/request, sessions of CCR-I → CCR-U×4 →
CCR-T, 60 s steady state after 5 s warmup, **3 repetitions**.

Environment: Apple M1 Max (10 cores), go1.26.2, macOS. Raw:
`bench/harness/phase3-results.txt`; GC traces:
`bench/harness/phase3-{static,dict}-gctrace.txt`.

### Throughput and latency (3 reps each)

| codec | TPS (mean of 3) | p50 | p95 | p99 |
|---|---:|---:|---:|---:|
| static (gycodec) | ~103,000 | 1.0 ms | 1.0 ms | 2.0 ms |
| dictionary | ~81,800 | 1.0 ms | 2.0 ms | 2.0 ms |

Per-rep TPS — static: 101196 / 103744 / 104195; dict: 81655 / 81578 /
82095. Static is **~26% higher throughput** at one notch better p95.
The gap is modest here precisely because loopback is scheduler/syscall
bound and the load generator steals cycles from the server; the codec's
own cost (the in-memory benchmarks above) differs by ~16×.

### GC pressure — the metric that matters at scale

Server side, during the 60 s steady state (`GODEBUG=gctrace=1` +
`runtime.MemStats`):

| | static | dictionary | ratio |
|---|---:|---:|---:|
| Mallocs/sec | ~0.42 M | ~9.85 M | **23× fewer** |
| GC cycles / run | 227 | 8,795 | **39× fewer** |
| Total GC pause / run | ~11 ms | ~501 ms | **45× less** |

This is the project's thesis confirmed end-to-end: near-zero
per-message allocation collapses GC pressure by ~20–45× under sustained
load. At the target ~100k TPS/site, GC pause — not parse CPU — is the
limiting factor, and that is exactly what the static codec removes. (The
static server still allocates ~0.42 M/s from the reply `c.Write` path
and pool bookkeeping, not from parsing; reducible further but already
far below the dictionary path.)

### Exit criteria

All Phase 3 exit criteria met: both codecs mounted under identical
traffic, comparison table above, GC behavior characterized. The raw fast
path is verified to bypass the dictionary for the hooked key while
leaving all other traffic (CER/DWR/…) on the stock mux
(`diam/rawhook_test.go`); the full `diam` suite still passes except the
pre-existing `TestServerClose/sctp` platform case on darwin/arm64.
