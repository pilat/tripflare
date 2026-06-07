# ADR-0003: Self-signed bootstrap, then ACME wildcard via self-answered DNS-01

- **Status:** accepted
- **Date:** 2026-03-07

## Context

Tracking links must be clean HTTPS on `*.domain.com` with no certificate warnings,
which means a **wildcard** cert from Let's Encrypt — and wildcards require the
**DNS-01** challenge. But ACME issuance takes time (DNS propagation, validation),
and during that window the HTTPS listener would have no certificate and every
handshake would fail. Tripflare is already the authoritative DNS server for the
domain ([ADR-1](0001-own-the-authoritative-dns-layer.md)).

## Decision

- On startup, generate a **self-signed wildcard cert immediately** and serve with
  it, so `:443` is functional from the first second.
- In the background (when `acme.enabled`), obtain or load the real wildcard cert
  and **hot-swap** it through the `tls.Config.GetCertificate` callback — no restart.
- **Answer the DNS-01 challenge ourselves:** the ACME provider writes the TXT
  token into a shared challenge store; the DNS server serves it for
  `_acme-challenge.domain.com`. No external DNS provider or API credentials.
- Renewal runs daily, renewing when under 30 days remain. With
  `acme.enabled: false`, the self-signed cert is permanent (local testing).

## Consequences

- No "waiting for certificate" startup window and no readiness gate on ACME.
- No external DNS provider — only possible because we own the nameserver (ADR-1).
- Certificate rotation with zero downtime via `GetCertificate`.
- A brief self-signed window on first boot (browser warning until the real cert
  lands).
- The challenge store is shared mutable state between the ACME and DNS subsystems
  (guarded by `sync.RWMutex`; it must hold multiple TXT values, since apex and
  wildcard share one `_acme-challenge` record).

## Alternatives Considered

- **Block startup until ACME succeeds.** Rejected — the service is down for the
  full issuance time on every cold start, and fails hard if ACME is unreachable.
- **HTTP-01 challenge.** Rejected — cannot issue wildcard certificates.
- **External DNS provider for DNS-01.** Rejected — we already *are* the
  authoritative nameserver; adding a provider and API keys would be pointless.

---
_Captured retroactively (2026-06-08) from `ai/tasks/2026-03-07-self-signed-fallback.md`._
