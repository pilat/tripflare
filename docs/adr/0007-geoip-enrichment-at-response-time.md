# ADR-0007: GeoIP enrichment at response time

- **Status:** accepted
- **Date:** 2026-03-08

## Context

Events show raw IPs; the dashboard is far more useful with country, flag, ASN, and
organization beside each address. That enrichment could be computed once and stored
on the event, or looked up on demand when the event is served.

## Decision

**Enrich on read.** When the API or SSE stream serves events, look up each IP
against MaxMind-format `.mmdb` files (matched by keyword — `*country*`, `*asn*`)
and attach the result to the response. Nothing geo is persisted: no schema column,
no migration. A per-request cache avoids repeat lookups for the same IP within one
response. Missing databases degrade to a silent noop — everything else still works.

## Consequences

- Zero storage cost and no migration; the events table is unchanged.
- Always current — re-running against updated `.mmdb` files reflects new mappings,
  with no stale geo baked into old rows.
- Fully optional: drop the databases in to enable, remove them to disable.
- A few microseconds of lookup per IP per response (negligible at ≤500 events per
  slug).
- Geo is not queryable or filterable in storage — acceptable, since events are
  viewed live rather than mined.

## Alternatives Considered

- **Persist geo fields on the event at capture time.** Rejected — needs a schema
  migration and goes stale when IP→org/geo mappings change, with no upside for a
  live-view tool.

---
_Captured retroactively (2026-06-08) from `ai/tasks/plan-2026-03-08-geoip-enrichment.md`._
