# ADR-0004: Bounded-resource design (TTL, ring buffer, rate limit)

- **Status:** accepted
- **Date:** 2026-03-08

## Context

The service runs on a constrained VPS, keeps all live data in RAM
([ADR-2](0002-in-memory-registry-with-sqlite-persistence.md)), and is security
tooling that logs IP addresses. A noisy or hostile visitor — or just unbounded
accumulation — would exhaust memory. Indefinite retention of personal data is both
an operational liability and a privacy concern.

## Decision

Three structural bounds, all on by default:

1. **Slug TTL** — every slug and its events auto-expire (`slug_ttl`); the flush
   loop purges expired data from both memory and SQLite.
2. **Per-slug ring buffer** — at `max_events_per_slug` (default 500) the *oldest*
   event is evicted and the new one appended. **Newest always wins** — the most
   recent ping matters more than the first.
3. **Per-`IP:slug` rate limit** — a token-bucket tier
   (`max_hits_per_slug_per_minute`) gates event recording *before* it reaches the
   registry; stale buckets are garbage-collected from the flush loop.

## Consequences

- Memory stays bounded regardless of traffic; the box stays healthy.
- Short retention shrinks the personal-data footprint — it strengthens the
  legitimate-interest basis and limits exposure.
- Structurally unsuitable for bulk harvesting or persistent abuse: no export, and
  data evaporates on its own.
- History is lossy *by design* — a flooded slug shows only its most recent N
  events, and everything ages out at TTL.
- The rate limiter is a per-instance in-memory map, consistent with the
  single-binary model (no distributed limiting).

> Note: the limiter is built to support multiple tiers (`[]TierConfig`), but only
> the per-minute tier is wired in `cmd/tripflare/main.go` today. An earlier
> multi-window design (minute/hour/day) was simplified to one.

## Alternatives Considered

- **Hard cap that rejects new events at the limit.** Rejected — it drops the
  freshest, most relevant captures.
- **Unbounded retention with manual cleanup.** Rejected — memory risk plus a
  standing privacy liability.

---
_Captured retroactively (2026-06-08) from `ai/tasks/plan-2026-03-08-limits-and-ringbuffer.md`._
