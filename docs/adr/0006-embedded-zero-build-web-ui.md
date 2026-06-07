# ADR-0006: Embedded zero-build web UI

- **Status:** accepted
- **Date:** 2026-03-08

## Context

The dashboard needs a genuinely interactive UI — create slugs, watch events stream
in live — but the project's deployment promise is "one `go build`, one binary,
`setcap`, run." A Node toolchain, bundler, or build step would break that promise
and add CI/deploy surface for what is a two-screen app.

## Decision

Ship a small SPA with **no build step**:

- **Preact + htm imported directly from the esm.sh CDN** as ES modules, with
  hand-written HTML/CSS — no transpilation, no bundler, no `node_modules`.
- **Embedded into the binary via `go:embed`** (`internal/httpserver/ui`) and served
  at `/tripflare`. No runtime filesystem reads for assets.
- The UI is just another client of the same JSON API as curl, and consumes the SSE
  stream for live events.

`go build` produces the entire application, UI included.

## Consequences

- Single self-contained binary; assets travel inside it; `go build` is the whole
  toolchain.
- UI and programmatic clients share one API surface — the dashboard is not special.
- Runtime dependency on the esm.sh CDN to load Preact/htm in the browser; the JS is
  not vendored locally today.
- No JSX build, HMR, or component ecosystem — deliberately scoped to a small app
  where vanilla htm is enough.

## Alternatives Considered

- **React/Vue/Next with a bundler.** Rejected — build step and toolchain weight
  unjustified for two screens.
- **Server-rendered HTML templates.** Rejected — wanted live SSE updates without
  full-page reloads.

---
_Captured retroactively (2026-06-08) from `ai/tasks/plan-2026-03-08-json-api-spa.md`._
