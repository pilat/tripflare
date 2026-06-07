# ADR-0002: In-memory registry with SQLite persistence

- **Status:** accepted
- **Date:** 2026-03-07

## Context

Tripflare runs on a €1/month single-core VPS. The hot path — a DNS query or an
HTTP fetch hitting a live slug — must record an event with minimal latency and no
contention on disk I/O. Writing every captured event synchronously to SQLite would
put disk in the request critical section and bottleneck under a burst.

## Decision

The **in-memory registry is the primary store** for all reads and writes. SQLite
is persistence only.

- A background **flush loop** (`flush_interval`, default 30s) writes dirty slugs
  and new events to SQLite incrementally, tracked per slug with a `dirty` flag and
  a `flushedCount` so only new rows are written.
- On startup, non-expired slugs and events are **loaded from SQLite back into
  memory**.
- SQLite runs in **WAL mode** for concurrent reads.

## Consequences

- The hot path touches only memory behind a mutex — no disk in the request
  critical section.
- Incremental flush avoids rewriting unchanged data each tick.
- A crash loses up to one flush interval of events. Acceptable for a honeypot:
  recent captures matter, durability does not.
- All active slugs and events live in RAM (~1KB/event). This is bounded by slug
  TTL and the per-slug ring buffer — see
  [ADR-4](0004-bounded-resource-design.md).

## Alternatives Considered

- **Persistent-first (write each event straight to SQLite).** Rejected — disk I/O
  and lock contention on the hot path, too slow for bursts on a single core.
- **Append-only log.** Rejected — more complex crash recovery to buy a durability
  guarantee the use case doesn't need.

---
_Captured retroactively (2026-06-08) from `ai/tasks/2026-03-07-in-memory-architecture.md`._
