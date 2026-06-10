# Harness B — gyserver / gyclient

Client/server profiling harness for the static Gy codec
(codec-design.md §3 Phase 3). Both binaries can run the **static**
`gycodec` (raw fast path) or the **stock dictionary** codec, selected at
startup, so the two are comparable under identical session-shaped
traffic.

No business logic: the server echoes Session-Id and the request
type/number, grants a fixed quota (CC-Total-Octets = 1,000,000), and
answers Result-Code 2001 at the message and per-MSCC level.

## Run

```sh
# server — static codec, with pprof and memstats logging
go run ./cmd/gyserver -addr :3868 -codec static -pprof :6060 -memstats 10s

# client — drive 64 closed-loop workers, 3 MSCC/request, 60s after 5s warmup
go run ./cmd/gyclient -addr localhost:3868 -codec static \
    -concurrency 64 -mscc 3 -updates 4 -warmup 5s -duration 60s
```

Swap `-codec dict` on **both** to measure the dictionary codec under the
same traffic. For GC behavior, start the server under
`GODEBUG=gctrace=1`.

## gyserver flags

| flag | default | meaning |
|---|---|---|
| `-addr` | `:3868` | listen address |
| `-codec` | `static` | `static` (raw fast path) or `dict` |
| `-pprof` | – | `ip:port` for net/http/pprof (empty disables) |
| `-memstats` | `0` | interval for `runtime.MemStats` logging |
| `-concurrent` | `0` | `MaxConcurrentHandlers` (0 = sequential) |

## gyclient flags

| flag | default | meaning |
|---|---|---|
| `-addr` | `localhost:3868` | server address |
| `-codec` | `static` | must match the server |
| `-concurrency` | `16` | closed-loop workers |
| `-updates` | `4` | CCR-U per session (CCR-I → CCR-U×N → CCR-T) |
| `-mscc` | `1` | MSCC count per request |
| `-warmup` | `5s` | unmeasured warmup |
| `-duration` | `60s` | measured steady state |

Output is one `RESULT` line per run with achieved TPS and p50/p95/p99,
ready to paste into BENCHMARKS.md.

## Caveats

- **Loopback ≠ deployment.** Running client and server on one host makes
  them contend for CPU, so the load generator steals cycles from the
  system under test (codec-design.md §4). Loopback TPS is a codec-vs-codec
  signal, **not** a capacity claim. Any externally communicated figure
  needs a real-network, separate-host run **and human review**.
- The raw fast path is **TCP only** (SCTP connections use the dictionary
  path) and fires **before** the CER/CEA handshake gate — fine for this
  harness, but a production OCS must gate on peer state in the handler.
  See NOTES.md §3.
