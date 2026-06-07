# Deploying Tripflare

A production install on a Linux VPS with **systemd**. Verified on **Ubuntu 24.04 LTS**
and applicable to Debian and other systemd-based distros. For a quick local run
instead, see the [Quick start](../README.md#quick-start).

Tripflare ships as a single **static binary** (pure Go, `CGO_ENABLED=0` — no CGO, no
libc dependency), so there is nothing to install on the server but the binary itself.
No Go toolchain required: grab a prebuilt release.

This guide assumes the install lives in `/opt/tripflare`. Adjust to taste.

## Prerequisites

- A VPS with a **static public IP** and ports **53 (UDP+TCP)**, **80**, **443** free.
- A registered domain whose **NS records** you can point at the VPS — see
  [DNS delegation](../README.md#dns-delegation).
- SSH access as a user that can write to the install dir and manage systemd
  (examples use `root`).

## 1. Install the binary on the server

Download the latest release straight onto the server and unpack it:

```bash
ssh root@YOUR_SERVER_IP bash -s <<'EOF'
mkdir -p /opt/tripflare/certs /opt/tripflare/geoip
cd /opt/tripflare
TAG=$(curl -fsSL https://api.github.com/repos/pilat/tripflare/releases/latest | grep -oP '"tag_name": "\K[^"]+')
curl -fsSL "https://github.com/pilat/tripflare/releases/download/${TAG}/tripflare_${TAG#v}_Linux_x86_64.tar.gz" | tar -xz tripflare
./tripflare -version
EOF
```

For an ARM server, swap `_Linux_x86_64` for `_Linux_arm64`. Every release also ships a
`checksums.txt` if you want to verify the archive before unpacking.

> Prefer to build it yourself? `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o
> tripflare ./cmd/tripflare/` and `rsync` the binary to `/opt/tripflare/` instead.

## 2. Upload your config

```bash
rsync -avz config.yaml root@YOUR_SERVER_IP:/opt/tripflare/
```

Your `config.yaml` needs `domain`, `external_ip`, `nameservers`, and at least one
`auth` entry; enable ACME for real certificates. See the
[configuration reference](../README.md#configuration-reference) and
[TLS / ACME](../README.md#tls--acme).

## 3. Free port 53 (systemd-resolved)

On Ubuntu and Debian, `systemd-resolved` runs a **DNS stub listener on
`127.0.0.53:53`**, which prevents Tripflare from binding port 53. Disable just the
stub — host name resolution keeps working:

```bash
ssh root@YOUR_SERVER_IP bash -s <<'EOF'
mkdir -p /etc/systemd/resolved.conf.d
printf '[Resolve]\nDNSStubListener=no\n' > /etc/systemd/resolved.conf.d/no-stub.conf
ln -sf /run/systemd/resolve/resolv.conf /etc/resolv.conf
systemctl restart systemd-resolved
EOF
```

If your distro doesn't use `systemd-resolved`, skip this. Confirm nothing else holds
`:53`:

```bash
ssh root@YOUR_SERVER_IP "ss -tulnp | grep ':53'"
```

## 4. (Optional) GeoIP databases

GeoIP enrichment (country, flag, ASN, org) is **optional** — Tripflare runs fine
without it and silently skips enrichment when the databases are absent. To enable
it, drop MaxMind-format `.mmdb` files into the `geoip` dir. Free DB-IP Lite,
refreshed monthly:

```bash
ssh root@YOUR_SERVER_IP bash -s <<'EOF'
MONTH="$(date +%Y-%m)"
DIR="/opt/tripflare/geoip"
for DB in dbip-country-lite dbip-asn-lite; do
    FILE="${DB}-${MONTH}.mmdb"
    [ -f "${DIR}/${FILE}" ] && { echo "${FILE} exists, skipping"; continue; }
    curl -fSL "https://download.db-ip.com/free/${FILE}.gz" | gunzip > "${DIR}/${FILE}"
done
EOF
```

Files are matched by keyword (`*country*.mmdb`, `*asn*.mmdb`), so MaxMind GeoLite2
equivalents work too. Re-run monthly (e.g. from cron) to keep them current.

## 5. systemd service

This unit runs Tripflare as a daemon, restarts it on failure, and grants
`CAP_NET_BIND_SERVICE` so it can bind privileged ports **without** `setcap` or
running fully privileged:

```bash
ssh root@YOUR_SERVER_IP bash -s <<'EOF'
cat > /etc/systemd/system/tripflare.service <<'UNIT'
[Unit]
Description=Tripflare DNS+HTTPS tracking service
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=/opt/tripflare
ExecStart=/opt/tripflare/tripflare -config /opt/tripflare/config.yaml
Restart=on-failure
RestartSec=5

# Plenty of file descriptors for concurrent SSE connections
LimitNOFILE=65535

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/opt/tripflare

# Bind privileged ports 53/80/443 without full root privileges
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable --now tripflare
systemctl status tripflare --no-pager
EOF
```

## 6. Verify

```bash
# DNS: your server answers authoritatively
dig NS your-domain.com
dig A test.your-domain.com @YOUR_SERVER_IP    # should return your external_ip

# Service health and live logs
ssh root@YOUR_SERVER_IP "systemctl is-active tripflare"
ssh root@YOUR_SERVER_IP "journalctl -u tripflare -f"
```

Then open `https://your-domain.com/tripflare`, log in with your config credentials,
and create a tracker.

## Updating

Pull the latest release on the server and restart — config, SQLite data, and
certificates persist under `/opt/tripflare`:

```bash
ssh root@YOUR_SERVER_IP bash -s <<'EOF'
cd /opt/tripflare
TAG=$(curl -fsSL https://api.github.com/repos/pilat/tripflare/releases/latest | grep -oP '"tag_name": "\K[^"]+')
curl -fsSL "https://github.com/pilat/tripflare/releases/download/${TAG}/tripflare_${TAG#v}_Linux_x86_64.tar.gz" | tar -xz tripflare
systemctl restart tripflare
./tripflare -version
EOF
```
