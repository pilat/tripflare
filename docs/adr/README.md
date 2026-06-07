# Architecture Decision Records

This directory records the **why** behind tripflare's architecture — the
decisions that have real trade-offs and aren't obvious from reading the code.

## What an ADR is

An ADR captures one architecturally significant decision: the forces that led to
it, what was chosen, what it costs, and what was rejected. It is a historical
record, not living documentation — ARCHITECTURE.md describes how the system works
*today*; an ADR explains how it got that way.

## When to write one

Write an ADR when a change:

- Picks one approach over a viable alternative with a real trade-off
  (in-memory vs. persistent-first, self-signed vs. blocking on ACME).
- Establishes or changes an invariant (dependency direction, ownership model).
- Will make a future reader ask "why is it done this way?" and the answer isn't
  in the code.

Don't write one for routine feature work, bug fixes, or anything where the code
is self-explanatory.

## Filename convention

`NNNN-kebab-case-title.md` — a zero-padded sequential number plus a short title:

```
0001-own-the-authoritative-dns-layer.md
0002-in-memory-registry-with-sqlite-persistence.md
```

The number makes ADRs easy to reference ("see ADR-3") and sortable. This is the
standard convention from Michael Nygard / adr-tools.

## Lifecycle

ADRs are **immutable once accepted**. You don't edit a decision — you supersede
it by writing a new ADR that links back:

- A new ADR's status is `proposed`, then `accepted` once the decision is made.
- When a later decision overrides an earlier one, the new ADR gets
  `Supersedes: ADR-XXXX` and the old one is marked `superseded by ADR-YYYY` in its
  Status line. The old ADR stays in the repo — the record is the point.

## Writing one

Copy `TEMPLATE.md` to the next number, fill it in, keep it tight. The decisions
already in this folder were captured retroactively from the design notes in
`ai/tasks/` — they reference their originating plan where one exists.

## Index

| ADR | Title | Status |
|-----|-------|--------|
| [0001](0001-own-the-authoritative-dns-layer.md) | Own the authoritative DNS layer, not just HTTP | accepted |
| [0002](0002-in-memory-registry-with-sqlite-persistence.md) | In-memory registry with SQLite persistence | accepted |
| [0003](0003-self-signed-bootstrap-then-acme-wildcard.md) | Self-signed bootstrap, then ACME wildcard via self-answered DNS-01 | accepted |
| [0004](0004-bounded-resource-design.md) | Bounded-resource design (TTL, ring buffer, rate limit) | accepted |
| [0005](0005-basic-auth-with-owner-scoped-slugs.md) | Basic Auth with owner-scoped slugs | accepted |
| [0006](0006-embedded-zero-build-web-ui.md) | Embedded zero-build web UI | accepted |
| [0007](0007-geoip-enrichment-at-response-time.md) | GeoIP enrichment at response time | accepted |
