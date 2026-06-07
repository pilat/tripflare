# Coding Style

Code and architecture conventions for tripflare. Examples are drawn from this
codebase — keep them honest as the code evolves. Machine-checkable rules live in
`.golangci.yml`; this file covers the conventions a linter can't enforce.

The doc has two layers: **code-level** rules (naming, errors, logging, file
order, testing) and **architecture-level** rules (package taxonomy, interface
design, dependency direction, lifecycle ownership). Both are normative.

---

## Project Structure

One package = one responsibility. No `package main` outside of `cmd/`.

Packages come in two kinds:

- **Service packages** — `store`, `registry`, `dns`, `httpserver`, `acme`,
  `geoip`, `ratelimit`. Stateful; they export an interface + a private
  implementation.
- **Plain packages** — `config`, `pixel`, `logging`. Data structs and pure
  functions. No interface — don't invent one just to have one.

## Dependency Direction (architecture-level)

Imports point one way: toward leaves. The graph is acyclic and stays that way.

```text
cmd/tripflare (composition root) ──► imports everything

httpserver ──► config, geoip, pixel, ratelimit, registry, store
dns        ──► registry, acme
registry   ──► store
store      ──► (none — pure leaf)
config, acme, geoip, ratelimit, pixel, logging ──► (none — leaves)
```

Invariants — a change that breaks one of these is an architectural change, not a
refactor:

1. **`store` imports no other internal package.** Persistence is a leaf; the
   registry depends on the store, never the reverse.
2. **Nothing imports `httpserver`, `dns`, or `cmd`.** These are the outer edge —
   servers and the composition root. They consume; they are not consumed.
3. **Only `cmd/tripflare` may import everything.** Wiring lives in `main`, not in
   the packages.
4. **No import cycles.** If two packages need each other, a type or interface is
   in the wrong place.

When in doubt, verify per package:

```sh
grep -rh "pilat/tripflare/internal" internal/<pkg>/*.go \
  | grep -v _test | grep -oE "internal/[a-z]+" | sort -u
```

## Interface-First Design (service packages)

A stateful service package exports an interface; the implementation is private:

```go
// store.go
type Service interface {
    InsertSlug(ctx context.Context, slug SlugRow) error
    LoadEvents(ctx context.Context, since time.Time) ([]EventRow, error)
    Close() error
}

type svc struct {
    db *sql.DB
}

var _ Service = (*svc)(nil)

func New(dbPath string) (Service, error) {
    db, err := sql.Open("sqlite", dbPath)
    if err != nil {
        return nil, fmt.Errorf("open sqlite %s: %w", dbPath, err)
    }
    return &svc{db: db}, nil
}
```

Required:

- `var _ Interface = (*impl)(nil)` compile-time check, right after the impl type.
- `New(...)` returns the interface, not `*svc`. (One deliberate exception:
  `logging.NewLegoAdapter` returns a concrete type — it satisfies an external
  library's `log.StdLogger`, not one of ours.)
- The implementation struct is private.

No DI framework. Dependencies are passed into `New(...)` by `cmd/tripflare/main.go`
as plain arguments — that's the entire wiring strategy.

## Naming Conventions

| What | Pattern | Real example |
|------|---------|--------------|
| Interface | named for its role | `Service`, `Limiter`, `ChallengeStore` |
| Primary implementation | `svc` | `type svc struct` |
| Alternative impl / helper | purpose-named | `noop` (geoip), `dnsProvider` (acme), `prettyHandler` (logging) |
| Constructor | `New` / `New<Thing>` | `New()`, `NewChallengeStore()` |
| Test files | `*_test.go` next to code | `store_test.go` |

`svc` is the default for the single main implementation in a package. When a
package legitimately has more than one (geoip's real `svc` plus its `noop`
fallback), name each for what it is.

## Error Handling

Wrap errors with context at the boundary:

```go
func New(dbPath string) (Service, error) {
    db, err := sql.Open("sqlite", dbPath)
    if err != nil {
        return nil, fmt.Errorf("open sqlite %s: %w", dbPath, err)
    }
    if err := migrate(db); err != nil {
        db.Close()
        return nil, fmt.Errorf("migrate: %w", err)
    }
    return &svc{db: db}, nil
}
```

Rules:

- Wrap with `fmt.Errorf("context: %w", err)` and add actionable context (slug,
  path, addr).
- Passing through an error you already wrapped one level down is fine — don't
  double-wrap the same context into noise.
- Comments in English.

### No Nested Error Handling

Keep error handling flat — never nest `if err != nil { if errors.Is(...) }`.
Check the specific sentinel first, then the general case:

```go
// flat: specific sentinel first, then the general error
if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
    errCh <- fmt.Errorf("http serve: %w", err)
}
```

When a function branches over IO, extract each branch into its own flat method
returning `(*T, error)` and dispatch between them, rather than nesting the error
checks inside one function.

## Error Propagation

**Propagate by default.** Log-and-continue is allowed only in these cases:

- **Cleanup / shutdown** where no one can act on the error — `slog.Error`/`Warn`:
  ```go
  if err := httpServer.Shutdown(shutdownCtx); err != nil {
      slog.Error("http shutdown error", "error", err)
  }
  ```
- **Post-commit side effect** — the primary work already succeeded and the
  response is already on the wire (e.g. writing the tracking pixel after headers
  are sent).
- **Optional enrichment** with a sane fallback — e.g. a JSON marshal or GeoIP
  lookup failing while recording an event:
  ```go
  slog.Error("failed to marshal dns data", "error", err)
  return false
  ```
- **Retry loops** — a renewal/flush tick that will run again:
  ```go
  if err := s.loadOrObtain(); err != nil {
      slog.Error("initial cert obtainment failed, will retry", "error", err)
  }
  ```

**Never log-and-continue on a primary write.** If persisting a captured DNS/HTTP
event fails on the request's hot path, propagate it — silent drops turn bugs into
lost captures.

## Logging

Use `log/slog`. The default logger is configured once in `cmd/tripflare/main.go`
(`slog.SetDefault`); everything else calls the package-level `slog.*` directly.

```go
slog.Info("slug created", "slug", id, "owner", owner, "expires_at", slug.ExpiresAt)
```

Rules:

- Structured key-value pairs, never string interpolation into the message.
- Levels: `Debug` for details, `Info` for milestones, `Warn`/`Error` for problems.
- This service captures attacker-supplied headers and payloads — don't dump full
  captured bodies or secrets at `Info`.

## Lifecycle & Goroutines

Tripflare is a long-running daemon — DNS, HTTPS, and the ACME renewal loop all own
background work. The root context comes from `signal.NotifyContext` in
`cmd/tripflare/main.go` and flows down into every server.

**Blocking serve idiom.** A long-running component exposes a blocking entry point
that takes the root context, serves until it is cancelled, then drains:

```go
func (s *svc) ListenAndServe(ctx context.Context, ready chan<- struct{}) error {
    // bind, signal ready, serve; on <-ctx.Done() drain with a bounded
    // context.WithTimeout(context.Background(), ...) shutdown.
}
```

Capturing the parameter `ctx` here is correct — it IS the long-lived signal
context. The ACME manager uses the same shape with `Run(ctx)`.

**`go` statements are scoped.** A `go func()` appears only where it owns a clear
cancellation path: inside a serve method (`dns`, `httpserver` spin up their UDP/TCP
and HTTP/HTTPS listeners this way) or a background loop launched from `main`. No
ad-hoc `go func()` buried in business logic.

**One mutex strategy per struct.** A struct with shared state uses ONE of: a
single `sync.RWMutex` (`registry`, `acme`), a single `sync.Mutex` (`ratelimit`),
or a channel actor. Don't mix styles on one struct.

## Size Guidance

No hard per-line limits on functions or files. A long function is not inherently
bad — a **deeply nested** one is. If a function is long but flat (sequential
steps, early returns, no arrow problem), leave it. Split by responsibility, not by
line count.

What the linters actually guard (see `.golangci.yml`):

| Linter | Guards |
|--------|--------|
| `gocyclo` / `cyclop` | branch / cyclomatic complexity |
| `nestif` | nested-`if` depth |

`funlen` is intentionally **not** enabled: raw line count is the wrong signal.
Keep functions flat and the complexity linters stay quiet on their own.

## Code Organization in File

Strict declaration order in every `.go` file (top to bottom):

```text
const      → All constants
type       → All type declarations (interfaces, structs, aliases)
func New() → Constructor(s)
Func()     → Exported functions/methods
func()     → Unexported functions/methods
```

Rules:

1. Compile-time interface checks go right after the implementation type:
   `var _ Interface = (*impl)(nil)`.
2. Constructor is named `New` and returns the interface type.
3. Exported functions come before unexported — never interleave.
4. Keep declarations grouped and predictable.

```go
package ratelimit

import (
    "sync"
    "time"
)

type TierConfig struct {
    Capacity int
    Window   time.Duration
}

type Limiter interface {
    Allow(key string) bool
    GC(maxAge time.Duration)
}

type svc struct {
    mu    sync.Mutex
    tiers []TierConfig
    keys  map[string]*entry
}

var _ Limiter = (*svc)(nil)

func New(tiers []TierConfig) Limiter {
    return &svc{tiers: tiers, keys: make(map[string]*entry)}
}

func (s *svc) Allow(key string) bool { /* ... */ }

func (s *svc) refill(b *bucket, tier TierConfig) { /* ... */ }
```

## Database

`modernc.org/sqlite` (pure Go, no CGO) via `database/sql`.

Rules:

- Use the `database/sql` standard interface; `?` placeholders for parameters.
- WAL journal mode is enabled at open.
- Schema migrations are a list of `CREATE TABLE IF NOT EXISTS ...` statements
  applied on startup in `migrate(db)` — plain SQL strings, run in order.

```go
_, err := s.db.ExecContext(ctx,
    `INSERT OR REPLACE INTO slugs (id, owner, created_at, expires_at) VALUES (?, ?, ?, ?)`,
    slug.ID, slug.Owner, slug.CreatedAt.UTC(), slug.ExpiresAt.UTC(),
)
```

## JSON Serialization

Struct tags are `snake_case` (enforced by `tagliatelle`):

```go
type Slug struct {
    ID        string    `json:"slug"`
    Owner     string    `json:"owner"`
    CreatedAt time.Time `json:"created_at"`
    ExpiresAt time.Time `json:"expires_at"`
}

type Event struct {
    Type      string          `json:"type"`
    SourceIP  string          `json:"source_ip"`
    Timestamp time.Time       `json:"timestamp"`
    Data      json.RawMessage `json:"data"`
}
```

## Testing

Tests live in `*_test.go` next to the code.

- **Table-driven where the test is parametric** (one logic path, many
  input→output pairs): `geoip`, `ratelimit`, `pixel`, the `dns`/`httpserver` slug
  parsers. Use `t.Run(name, ...)` subtests.
- **Plain test functions are fine** for setup-heavy flows where each case has its
  own arrange/act/assert (most of `config`, `acme`, `registry`).

```go
func TestCountryFlag(t *testing.T) {
    tests := []struct {
        code string
        want string
    }{
        {"US", "\U0001F1FA\U0001F1F8"},
        {"DE", "\U0001F1E9\U0001F1EA"},
    }
    for _, tt := range tests {
        t.Run(tt.code, func(t *testing.T) {
            if got := countryFlag(tt.code); got != tt.want {
                t.Errorf("countryFlag(%q) = %q, want %q", tt.code, got, tt.want)
            }
        })
    }
}
```

Libraries and doubles:

- Standard `testing` with `if got != want { t.Errorf(...) }` is the norm.
- `github.com/stretchr/testify/require` is available for fatal assertions in
  setup-heavy tests — reach for it when a failed precondition should stop the test.
- Test doubles are small hand-written structs implementing the interface (e.g.
  `mockChallenges` in `dns/server_test.go`). No mock-generation framework.

## Quick Checklist

Before committing, verify:

- [ ] `go build ./...` passes
- [ ] `go test ./...` passes
- [ ] `golangci-lint run` is clean (or minimally justified)
- [ ] No `package main` outside `cmd/`
- [ ] `var _ Interface = (*impl)(nil)` for every service implementation
- [ ] Errors wrapped with context
- [ ] Functions flat, not deeply nested
- [ ] No new import edge that breaks the dependency direction above

## Core Principles

### Fail Fast
- Don't check for nil if the value can never be nil here.
- Trust your invariants — no defensive checks for impossible states.
- Surface a wrong state loudly rather than corrupt data silently.

### No Arrow Problem (avoid deep nesting)

```go
// BAD: arrow / ladder anti-pattern
func process(items []Item) error {
    if len(items) > 0 {
        for _, item := range items {
            if item.Valid {
                if item.Type == "special" {
                    // deeply nested logic
                }
            }
        }
    }
    return nil
}

// GOOD: early returns, flat structure
func process(items []Item) error {
    if len(items) == 0 {
        return nil
    }
    for _, item := range items {
        if !item.Valid {
            continue
        }
        if item.Type != "special" {
            continue
        }
        // logic at the same indentation level
    }
    return nil
}
```

### YAGNI over DRY
- Don't extract helpers or abstractions to reduce repetition unless the
  abstraction removes actual **complexity** (not just lines).
- Three identical 3–5 line `if` blocks beat one helper that adds an indirection
  layer. Leave them inline.
- Extract only when the duplication hides a bug-prone invariant, or the shared
  logic will genuinely evolve as one unit.

### Single-Use Helpers Are the Same Smell
- Don't extract a private method called from exactly one place unless it genuinely
  simplifies the caller's control flow (a retry loop, a transaction boundary,
  branch logic the caller shouldn't see).
- A sequential list of store/service calls doesn't qualify — it reads fine inline
  and the extraction only adds a jump.

### Minimal Exports
- Never export what doesn't need to be exported.
- Start private; export only when an external package needs it.
- Internal types, helpers, constants stay lowercase.

### Comments in English Only
- All code comments MUST be in English.
- Non-English is allowed only in user-facing strings and `*.md` docs.
- Commit messages in English.

## Documentation Style

What we avoid:

- Package-level doc blocks at the top of files (`// Package foo does...`).
- Trivial comments that restate the code (`// increments counter`).
- A comment on every single function.

What we want:

- Godoc on **exported** APIs whose behavior isn't obvious from the name.
- Comments that explain **why**, not **what**.
- No comment when the code speaks for itself.

```go
// BAD: trivial, restates the name
// New creates a new service
func New() Service { ... }

// GOOD: explains a non-obvious decision
// Validate the certificate before writing it to disk to avoid boot loops.
func (s *svc) loadOrObtain() error { ... }
```

## Anti-Patterns

Don't:

```go
// BAD: exported struct instead of an interface (for a service)
type Server struct { ... }
func NewServer() *Server { ... }

// BAD: import alias without a genuine conflict
import reg "github.com/pilat/tripflare/internal/registry"

// BAD: no error context
return err

// BAD: log.Printf instead of slog
log.Printf("serving %s", addr)

// BAD: global mutable state
var globalStore *svc

// BAD: defensive nil check for an impossible case
func (s *svc) handle(req *Request) {
    if req == nil { // req is never nil here
        return
    }
}

// BAD: arrow problem
if x {
    if y {
        if z {
            doSomething()
        }
    }
}

// BAD: exporting an internal helper
func HelperNoOneOutsideNeeds() { ... }
```

Do:

```go
// GOOD: interface + private impl
type Service interface { /* ... */ }
var _ Service = (*svc)(nil)
func New() Service { return &svc{} }

// GOOD: error context
return fmt.Errorf("insert slug %s: %w", slug.ID, err)

// GOOD: structured logging
slog.Info("slug created", "slug", id, "owner", owner)

// GOOD: flat structure with early returns
if !valid {
    return errInvalid
}
return process(data)

// GOOD: private by default
type svc struct { ... }
func newEntry() *entry { ... }
```
