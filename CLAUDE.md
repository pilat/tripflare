# Tripflare

All-in-one DNS + HTTPS tracking service in Go. Single-user, self-hosted — designed for personal security research use. Generates unique links like `{slug}.domain.com/img.png?payload=x`, captures DNS resolutions, HTTP requests, headers, and query params. Manages its own wildcard Let's Encrypt certificate.

The project is open source; the deployed instance is private (bcrypt auth required). Single-user by design — the operator is the only user, so there is no third-party data collection or consent flow.

## Quick Reference

- **Language:** Go 1.26+
- **Module:** `github.com/pilat/tripflare`
- **Entry point:** `cmd/tripflare/main.go`
- **Config:** `config.yaml` (env vars override)
- **Build:** `go build ./cmd/tripflare/`
- **Test:** `go test ./...`
- **Lint:** `golangci-lint run`

## Key Dependencies

| Module | Purpose |
|--------|---------|
| `github.com/miekg/dns` | DNS server |
| `github.com/go-acme/lego/v4` | ACME client (DNS-01 wildcard certs) |
| `modernc.org/sqlite` | SQLite storage (pure Go, no CGO) |

## Code Style

Follow `docs/coding-style.md` in this repo. Key points:
- Interface-first design: export interfaces, not structs
- `var _ Interface = (*impl)(nil)` compile-time checks
- `log/slog` for structured logging
- No DI frameworks — plain constructor injection
- Declaration order: `const` -> `type` -> `New()` -> exported -> unexported
- Prefer flat functions over short ones — complexity is capped by linters (`gocyclo`/`cyclop`/`nestif`), not raw line count

## Architecture & Documentation

Read in this order to come up to speed:

1. `README.md` — what tripflare is and how to run it
2. `ARCHITECTURE.md` — components, data flow, startup sequence, schema
3. `docs/coding-style.md` — code- and architecture-level conventions
4. `docs/adr/` — the *why* behind the big decisions (start at `docs/adr/README.md`)

After any implementation change, keep these docs in sync with the code: run
`/pilat:arch-sync` to check for drift, and add an ADR (`docs/adr/TEMPLATE.md`) when
you make a decision with a real trade-off. ARCHITECTURE.md's "Keeping This Document
Accurate" section lists exactly what to verify.

## Deployment

General setup is in `README.md`. Operator-specific hosting and DNS details are kept out of version control in `CLAUDE.local.md`.

## Responsible Use

Single-user security research tooling — for authorized penetration testing, CTF competitions, and security education. Not intended for unauthorized access, data exfiltration, C2 infrastructure, phishing, or surveillance. Rate limits protect the constrained VPS resources.
