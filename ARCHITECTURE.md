# Architecture

## Overview

Single Go binary running three long-running servers — DNS, HTTP, and an ACME manager — that share an in-memory registry with SQLite persistence. A periodic flush loop in `main` drains the registry to disk.

```
                         ┌──────────────────┐
                         │     main.go      │
                         │ config, signal,  │
                         │   flush loop     │
                         └──┬───┬───┬───┬───┘
                            │   │   │   │
               ┌────────────┘   │   │   └─────────────┐
               │                │   │                  │
               ▼                ▼   ▼                  ▼
        ┌─────────────┐ ┌────────────────┐     ┌─────────────┐
        │ DNS Server  │ │  HTTP Server   │     │ACME Manager │
        │ :53 UDP/TCP │ │  :80 + :443    │     │   (lego)    │
        └──────┬──────┘ └───────┬────────┘     └──────┬──────┘
               │                │                     │
               ▼                ▼                     ▼
        ┌───────────────────────────┐   ┌──────────────────┐
        │   Registry (in-memory)   │   │  Challenge Store  │
        │   slugs, events, pub/sub │   │  (sync.RWMutex)   │
        └────────────┬─────────────┘   └──────────────────┘
                     │
                     │ flush loop
                     ▼
              ┌─────────────┐
              │    Store    │
              │  (SQLite)   │
              └─────────────┘
```

## Components

### Registry (`internal/registry/`)

In-memory primary store. All reads and writes go through the registry — SQLite is only for persistence.

- **Slug CRUD**: `CreateSlug`, `GetSlug`, `DeleteSlug`, `ListSlugs` (filtered by owner)
- **Event recording**: `RecordDNS` and `RecordHTTP` append to a per-slug event slice
- **Ring buffer**: when a slug reaches `max_events_per_slug`, oldest events are evicted
- **Pub/sub**: `Subscribe` returns a channel that receives new events in real time, used by SSE streaming
- **Flush/load**: `FlushTo` persists dirty data to the store; `LoadFrom` restores state on startup
- **Cleanup**: removes expired slugs, closes subscriber channels

The registry tracks a `dirty` flag and `flushedCount` per slug to flush only new events incrementally.

### Store (`internal/store/`)

SQLite persistence layer behind the registry. Uses WAL mode for concurrent reads.

**Schema:**

```sql
CREATE TABLE slugs (
    id         TEXT PRIMARY KEY CHECK(length(id) <= 64),
    owner      TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    expires_at DATETIME NOT NULL
);

CREATE TABLE events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    slug       TEXT NOT NULL CHECK(length(slug) <= 128),
    type       TEXT NOT NULL,
    source_ip  TEXT NOT NULL,
    timestamp  DATETIME DEFAULT CURRENT_TIMESTAMP,
    data       JSON
);

CREATE INDEX idx_events_slug ON events(slug);
CREATE INDEX idx_events_timestamp ON events(timestamp);
```

The `data` JSON column stores type-specific details:
- DNS events: `{"query_type": "A", "query_name": "slug.domain.com", "client_subnet": "..."}`
- HTTP events: `{"scheme": "https", "method": "GET", "path": "/img.png", "query": "ref=x", "user_agent": "...", "headers": {...}}`

### DNS Server (`internal/dns/`)

Authoritative DNS server using `miekg/dns`. Listens on both UDP and TCP.

- **A/AAAA queries** for `*.domain.com` and bare `domain.com`: responds with configured external IP
- **NS/SOA queries** for bare domain: returns configured nameservers
- **TXT queries** for `_acme-challenge.domain.com`: reads from challenge store
- **NXDOMAIN** for everything else
- **Event recording**: logs DNS queries for known slugs to the registry (extracts slug from subdomain, supports nested subdomains like `prefix.slug.domain.com`)
- **EDNS Client Subnet**: extracts and records `EDNS0_SUBNET` when present
- **TTL=1** on all A/AAAA responses to prevent caching

Signals readiness via a channel after both UDP and TCP listeners bind — ACME waits for this before starting.

### HTTP Server (`internal/httpserver/`)

Standard `net/http` server with two listeners.

**Port 80** — tracking only:
- Subdomain requests (`slug.domain.com`): record event, serve pixel or OG page
- Bare domain: 404

**Port 443** — tracking + API + UI:
- Subdomain requests: same tracking behavior as port 80
- Bare domain routes (all require Basic Auth except `/`):
  - `GET /` — 404
  - `GET /api/slugs` — list slugs (filtered by authenticated user)
  - `POST /api/slugs` — create slug (owned by authenticated user)
  - `GET /api/slugs/{slug}` — slug detail with events (GeoIP-enriched)
  - `DELETE /api/slugs/{slug}` — delete slug (from registry and store)
  - `GET /api/slugs/{slug}/events` — SSE event stream (replays history, then live)
  - `DELETE /api/slugs/{slug}/events` — clear events
  - `GET /tripflare`, `/tripflare/style.css` — embedded web UI

**Key design:**
- `tls.Config.GetCertificate` callback for hot-reload of certificates
- Open Graph meta tags on non-pixel subdomain responses trigger messenger preview fetches
- Ownership enforcement: users can only access their own slugs
- Rate limiting via token bucket (key: `IP:slug`) before recording events
- UI is embedded via `go:embed` — no runtime filesystem reads

### ACME Manager (`internal/acme/`)

Wildcard certificate lifecycle using `go-acme/lego`.

- Requests `*.domain.com` + `domain.com` certificate via DNS-01 challenge
- Implements lego's `ChallengeProvider`: `Present()` writes TXT value to challenge store, `CleanUp()` removes it
- On startup: generates self-signed cert immediately, then loads or obtains ACME cert
- Background goroutine: checks daily, auto-renews when <30 days remaining
- Certificates stored on disk (`cert_path/staging/` or `cert_path/prod/`)
- When `acme.enabled: false`, runs with self-signed certificate only

### Challenge Store (`internal/acme/challenge.go`)

Shared store for ACME DNS-01 challenge tokens. Written by the ACME provider, read by the DNS server via the `ChallengeStore` interface (`GetTXT`). Supports multiple TXT values per FQDN (needed when both apex and wildcard share the same `_acme-challenge` record). Protected by `sync.RWMutex`.

### GeoIP (`internal/geoip/`)

Optional IP enrichment using MaxMind-format `.mmdb` databases.

- Looks for `*country*.mmdb` and `*asn*.mmdb` files in the configured directory
- Returns country code, flag emoji, ASN number, and organization name
- Graceful noop when databases are missing — returns empty `Info`
- Used by HTTP server to enrich events in API responses and SSE streams
- Per-request lookup cache avoids redundant database hits

### Rate Limiter (`internal/ratelimit/`)

Token bucket rate limiter, keyed by `IP:slug`.

- Configurable tiers (currently one: `max_hits_per_slug_per_minute` tokens per minute)
- `Allow(key)` checks all tiers and consumes one token from each
- `GC(maxAge)` removes entries not accessed within the given duration (called from flush loop)

### Pixel (`internal/pixel/`)

Static tracking responses.

- 1x1 transparent PNG (67 bytes) and GIF (43 bytes)
- `IsPixelPath` matches `.png`, `.jpg`, `.jpeg`, `.gif` extensions
- JPG/JPEG requests serve PNG bytes (browsers handle the mismatch for tracking)
- `WriteOGPage` renders minimal HTML with Open Graph meta tags for link previews

### Logging (`internal/logging/`)

- `PrettyHandler`: colorized text handler for `log/slog`
- `LegoAdapter`: bridges lego's `StdLogger` interface to `slog`

## Data Flow

### Tracking: `https://abc123.domain.com/img.png?ref=campaign1`

```
1. Client DNS resolver queries abc123.domain.com
   └─> DNS Server checks registry.SlugExists("abc123")
       └─> If exists: registry.RecordDNS("abc123", resolver_ip, "A", ...)
       └─> Responds with A record → configured external IP (TTL=1)

2. Client connects on :443, TLS handshake via wildcard cert

3. Client sends GET /img.png?ref=campaign1
   └─> HTTP Server extracts slug from Host header
       └─> Rate limiter checks Allow("client_ip:abc123")
           └─> If allowed: registry.RecordHTTP("abc123", client_ip, "https", "GET", "/img.png", "ref=campaign1", ...)
       └─> Responds with 1x1 transparent PNG

4. SSE subscribers receive events in real time via registry pub/sub
```

### Flush loop (runs every `flush_interval`)

```
1. registry.FlushTo(store) — persist dirty slugs and new events to SQLite
2. registry.Cleanup() — remove expired slugs from memory
3. store.DeleteExpired() — remove expired slugs/events from SQLite
4. ratelimit.GC(1h) — remove stale rate limiter entries
```

### ACME certificate issuance

```
1. ACME Manager requests wildcard cert from Let's Encrypt
2. Let's Encrypt returns DNS-01 challenge token
3. ACME provider writes token to Challenge Store
4. Let's Encrypt queries _acme-challenge.domain.com TXT
   └─> DNS Server reads from Challenge Store, responds with token
5. Let's Encrypt validates → issues cert
6. ACME Manager stores cert on disk, updates in-memory cert
7. HTTP Server picks up new cert via GetCertificate callback
```

## Startup Sequence

```
1. Load config (YAML + env overrides)
2. Open SQLite store, run migrations
3. Create registry (in-memory)
4. Load slugs and events from store into registry
5. Init GeoIP (noop if databases missing)
6. Init rate limiter
7. Create challenge store
8. Start DNS server (goroutine), wait for ready signal
9. Start ACME manager (goroutine) — load or obtain cert
10. Start HTTP server (goroutine) — port 80 + 443
11. Start flush loop (goroutine)
12. Block on SIGINT/SIGTERM
13. Final flush: registry → store
14. Shutdown: cancel context → all servers stop → close store
```

## Infrastructure Requirements

- **DNS delegation**: domain registrar must set NS records pointing to your server
- **Ports**: 53 (UDP+TCP), 80, 443 — must be open and not occupied
- **Privileges**: port 53 requires root or `setcap cap_net_bind_service=+ep`
- **Disk**: SQLite database + cert storage (minimal, <10MB for typical usage)
- **Memory**: all active slugs and events live in memory; ~1KB per event

## Keeping This Document Accurate

The code is the source of truth; this document tracks it. After an implementation
change, verify each of these and update the doc if it drifted:

- **Components vs. `internal/`** — `ls internal/` should match the Components
  section. A new package needs a new subsection here (and a node in the diagram if
  it is a server or a shared store).
- **Dependency direction** — the diagram assumes `store` is a leaf,
  `registry → store`, `dns → registry, acme`, and `httpserver` consumes the shared
  services. A new import edge (especially anything importing `httpserver`, `dns`,
  or `cmd`, or any cycle) means both this diagram and the dependency rules in
  `docs/coding-style.md` are stale. Check per package:
  `grep -rh "pilat/tripflare/internal" internal/<pkg>/*.go | grep -v _test | grep -oE "internal/[a-z]+" | sort -u`
- **SQLite schema** — the `CREATE TABLE` blocks must match `migrate()` in
  `internal/store/store.go`, and the `data` JSON shapes must match what `RecordDNS`
  / `RecordHTTP` marshal in `internal/registry/`.
- **HTTP routes** — the route list under *HTTP Server* must match the routing
  switch in `internal/httpserver/server.go` and `handlers.go`.
- **Config & limits** — fields named here must match `internal/config/config.go`
  and `config.yaml.example`.
- **Startup sequence** — the numbered boot steps must track the actual wiring and
  goroutine launches in `cmd/tripflare/main.go`.
- **Decisions** — if a change reverses something recorded in `docs/adr/`, don't
  edit the old ADR: add a new one that supersedes it.

Run `/pilat:arch-sync` to check automatically.
