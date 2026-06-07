# ADR-0005: Basic Auth with owner-scoped slugs

- **Status:** accepted
- **Date:** 2026-03-08
- **Supersedes:** an earlier api_key + bearer-token scheme (never recorded as its own ADR)

## Context

Tripflare moved from a public-SaaS framing to a private, self-hosted tool. It needs
to gate the dashboard and API behind credentials without standing up session
infrastructure — while the **tracking endpoints** (`slug.domain.com`) stay open,
since external targets have to reach them. An earlier design used an `api_key` plus
per-request bearer tokens; that layer added moving parts without real security
value, because the slug ID is already random and secret.

## Decision

- **HTTP Basic Auth** (bcrypt password hashes in `config.yaml`) on all API and UI
  routes. The tracking hot path stays unauthenticated.
- Every slug carries an **`owner`** (the authenticated username). `GET /api/slugs`
  returns only the caller's slugs, and every slug-scoped route enforces ownership
  server-side.
- A slug that isn't yours returns **404, not 403** — existence is not leaked, so
  slugs can't be enumerated.
- Multiple users are supported and isolated by owner.

## Consequences

- Stateless auth: no sessions, no token store — bcrypt over the always-on TLS.
- One instance can serve several operators with isolated views, with no change to
  the core registry schema.
- 404-on-not-yours blocks slug enumeration.
- Credentials ride on every request (fine over TLS).
- No per-slug revocation independent of deletion, and no granular roles.

## Alternatives Considered

- **api_key + bearer tokens (the superseded scheme).** Rejected — extra ceremony
  with no added protection; the slug ID already gates access to its own data.
- **Session cookies / OAuth / JWT.** Rejected — session management and
  identity-provider overhead are unjustified for a self-hosted single binary.

---
_Captured retroactively (2026-06-08) from `ai/tasks/plan-2026-03-08-auth-and-simplify.md`,
`ai/tasks/plan-2026-03-08-owner-slugs-dashboard.md`, and
`ai/tasks/plan-2026-03-09-slug-delete-and-ownership.md`._
