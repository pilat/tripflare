# ADR-0001: Own the authoritative DNS layer, not just HTTP

- **Status:** accepted
- **Date:** 2026-03-07

## Context

A plain tracking pixel only fires when something makes the HTTP request. But the
DNS lookup happens *first*, *separately*, and through *different* infrastructure —
the target's resolver, not the target itself. Sandboxes, AV scanners, corporate
proxies, and DNS-only exfil channels routinely resolve a hostname without ever
fetching the URL. An HTTP-only tracker is blind to all of them.

Seeing both stages — and seeing that they often come from *different parties* (the
resolver vs. the actual client) — is the entire reason this tool exists.

## Decision

Tripflare is the **authoritative DNS server** for its own domain, not just an HTTP
endpoint. One binary:

- answers `*.domain.com` / apex A/AAAA queries (recording the resolver IP and EDNS
  client subnet, TTL=1 so resolvers don't cache), and
- serves the HTTPS request for the same slug (recording the client IP, headers,
  User-Agent, path, query).

Both stages are recorded against the same slug and correlated in the dashboard.

## Consequences

- Captures DNS-only interactions a pixel can never see; surfaces resolver and
  client as distinct parties.
- Owning the nameserver is what makes self-answered DNS-01 wildcard TLS possible
  (see [ADR-3](0003-self-signed-bootstrap-then-acme-wildcard.md)) — no external DNS
  provider needed.
- Costs real setup: NS delegation to the server, a static public IP, and ports 53
  (UDP+TCP), 80, 443 free. Port 53 is privileged (root or `setcap`).
- Running an authoritative nameserver is more operational surface than a lone web
  endpoint.

## Alternatives Considered

- **Third-party OOB service (Burp Collaborator, interactsh).** Rejected — the goal
  was self-hosted and private, with no third-party custody of capture data.
- **HTTP-only tracking pixel.** Rejected — blind to the DNS stage, which is the
  differentiator.

---
_Captured retroactively (2026-06-08) from the project's founding design; see
`README.md` "Why own the DNS layer too?"._
