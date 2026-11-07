# ADR-0008: Operator-only build identity, release stamp first

- **Status:** accepted
- **Date:** 2026-11-07

## Context

The operator deploys two ways: goreleaser release artifacts, which stamp
`main.version/commit/date` via `-ldflags`, and a plain `go build` shipped between
releases. The dashboard has to answer "is my fix live?", so it needs the running
binary's version, commit and a dirty marker.

Two sources exist, and they disagree:

- **ldflags.** Present only on release builds. goreleaser's `{{.Version}}` drops the
  `v` (`0.1.3`).
- **Go's embedded build info** (`runtime/debug.ReadBuildInfo`). Present on every build
  from a git checkout: the module version (`v0.1.3`, a pseudo-version, or `+dirty`),
  `vcs.revision` and `vcs.time`.

Go counts *untracked* files as uncommitted. goreleaser writes `dist/` into the
checkout before building, so the real `v0.1.3` release binary's build info reports
`v0.1.3+dirty`. Neither source carries a build time: both hold the commit time.

Tripflare is also a tracker. Its slug subdomains, pixels and OG pages answer
untrusted targets, so anything that identifies its build there is a fingerprint.

## Decision

- **Resolution:** a pure resolver in `internal/buildinfo`. If the ldflags version is
  set (not `dev`, not empty), the ldflags trio is the identity, `v`-normalised.
  Otherwise Go build info is used as it is, including pseudo-versions and `+dirty`.
  `main` resolves once and injects the value. `.goreleaser.yml` is unchanged.
- **Date:** the date shown is labelled as the commit date, never "built".
- **Exposure:** the identity reaches clients only through the authenticated
  `GET /api/version`. The UI header and `-version` use the same value.
- **Invariant:** no unauthenticated response carries build identity. That covers slug
  subdomains, pixels, OG pages, 401s and the bare `/`, and it means no `Server` or
  version header.
- **Hygiene:** `.gitignore` covers the repo's untracked build outputs (`/dist/`,
  `/bin/`, `/coverage.html`), so `+dirty` means uncommitted changes.

## Consequences

- Both deploy paths show a real identity: no more `dev` on local builds, and releases
  read `v0.1.3`, matching the tags.
- Release identity doesn't depend on how clean CI's checkout is. The cost is that a
  release can never report dirtiness, which goreleaser's own clean-tree check makes
  acceptable.
- Two dirty deploys on the same base commit look identical. The fix is to commit
  first.
- Any future "about" or diagnostics output has to stay behind auth. Adding a version
  header or footer to the tracking surface reverses this ADR.
- A new untracked build output in the repo brings back false `+dirty` on local builds
  until it is git-ignored.

## Alternatives Considered

- **Build info only, ldflags dropped.** One source of truth. Rejected because it is
  fragile: any untracked file in CI before the build (today `dist/`, tomorrow a
  generated asset) silently marks every release dirty.
- **Moving the ldflags vars into the package and having `httpserver` read them.**
  This saves a constructor argument. Rejected: a server package would read link-time
  global state, tests couldn't inject an identity, and goreleaser's `-X` paths would
  change.
- **Injecting the identity into `index.html` at serve time.** This saves a request.
  Rejected: it turns the static embedded page into a template (against ADR-0006's
  "UI is just another API client"), and curl gets nothing.
- **A real build timestamp.** It would tell two dirty deploys apart. Rejected because
  it needs ldflags in an untracked, operator-local deploy script and a per-field merge
  rule that breaks "release stamp as a unit".

---
_Recorded from `ai/tasks/plan-2026-11-07-version-in-ui.md`._
