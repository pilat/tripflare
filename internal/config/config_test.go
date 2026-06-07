package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func testHash(t *testing.T) string {
	t.Helper()

	h, err := bcrypt.GenerateFromPassword([]byte("test-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("generate bcrypt hash: %v", err)
	}

	return string(h)
}

func minimalYAML(t *testing.T) string {
	t.Helper()

	return `
domain: "test.example.com"
external_ip: "10.0.0.1"
auth:
  - username: "admin"
    password_hash: "` + testHash(t) + `"
`
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()

	f := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return f
}

func TestLoad(t *testing.T) {
	hash := testHash(t)
	content := `
domain: "test.example.com"
external_ip: "10.0.0.1"
listen:
  dns: ":5353"
  http: ":8080"
  https: ":8443"
sqlite_path: "/tmp/test.db"
cert_path: "/tmp/certs"
acme:
  email: "test@example.com"
  staging: true
auth:
  - username: "admin"
    password_hash: "` + hash + `"
`

	cfg, err := Load(writeConfig(t, content))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Domain != "test.example.com" {
		t.Errorf("domain = %q", cfg.Domain)
	}

	if cfg.ExternalIP != "10.0.0.1" {
		t.Errorf("external_ip = %q", cfg.ExternalIP)
	}

	if cfg.Listen.DNS != ":5353" {
		t.Errorf("listen.dns = %q", cfg.Listen.DNS)
	}

	if cfg.ACME.Email != "test@example.com" {
		t.Errorf("acme.email = %q", cfg.ACME.Email)
	}

	if len(cfg.Auth) != 1 || cfg.Auth[0].Username != "admin" || cfg.Auth[0].PasswordHash != hash {
		t.Errorf("auth = %+v", cfg.Auth)
	}
}

func TestLoadMissingDomain(t *testing.T) {
	hash := testHash(t)
	content := `
external_ip: "1.2.3.4"
auth:
  - username: "admin"
    password_hash: "` + hash + `"
`

	_, err := Load(writeConfig(t, content))
	if err == nil {
		t.Fatal("expected error for missing domain")
	}
}

func TestLoadMissingExternalIP(t *testing.T) {
	hash := testHash(t)
	content := `
domain: "example.com"
auth:
  - username: "admin"
    password_hash: "` + hash + `"
`

	_, err := Load(writeConfig(t, content))
	if err == nil {
		t.Fatal("expected error for missing external_ip")
	}
}

func TestLoadMissingAuth(t *testing.T) {
	content := `
domain: "example.com"
external_ip: "1.2.3.4"
`

	_, err := Load(writeConfig(t, content))
	if err == nil {
		t.Fatal("expected error for missing auth")
	}
}

func TestLoadInvalidAuthHash(t *testing.T) {
	content := `
domain: "example.com"
external_ip: "1.2.3.4"
auth:
  - username: "admin"
    password_hash: "not-a-bcrypt-hash"
`

	_, err := Load(writeConfig(t, content))
	if err == nil {
		t.Fatal("expected error for invalid bcrypt hash")
	}
}

func TestLoadAuthMissingUsername(t *testing.T) {
	hash := testHash(t)
	content := `
domain: "example.com"
external_ip: "1.2.3.4"
auth:
  - password_hash: "` + hash + `"
`

	_, err := Load(writeConfig(t, content))
	if err == nil {
		t.Fatal("expected error for missing username")
	}
}

func TestLoadMultipleAuthEntries(t *testing.T) {
	h1 := testHash(t)
	h2, _ := bcrypt.GenerateFromPassword([]byte("other"), bcrypt.MinCost)
	content := `
domain: "example.com"
external_ip: "1.2.3.4"
auth:
  - username: "alice"
    password_hash: "` + h1 + `"
  - username: "bob"
    password_hash: "` + string(h2) + `"
`

	cfg, err := Load(writeConfig(t, content))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(cfg.Auth) != 2 {
		t.Errorf("auth entries = %d, want 2", len(cfg.Auth))
	}

	if cfg.Auth[0].Username != "alice" {
		t.Errorf("auth[0].username = %q, want alice", cfg.Auth[0].Username)
	}

	if cfg.Auth[1].Username != "bob" {
		t.Errorf("auth[1].username = %q, want bob", cfg.Auth[1].Username)
	}
}

func TestEnvOverrides(t *testing.T) {
	t.Setenv("TRIPFLARE_DOMAIN", "override.com")

	cfg, err := Load(writeConfig(t, minimalYAML(t)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Domain != "override.com" {
		t.Errorf("domain = %q, want override.com", cfg.Domain)
	}
}

func TestEnvOverrideRescuesEmptyRequired(t *testing.T) {
	hash := testHash(t)
	content := `
external_ip: "1.2.3.4"
auth:
  - username: "admin"
    password_hash: "` + hash + `"
`

	t.Setenv("TRIPFLARE_DOMAIN", "from-env.com")

	cfg, err := Load(writeConfig(t, content))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Domain != "from-env.com" {
		t.Errorf("domain = %q, want from-env.com", cfg.Domain)
	}
}

func TestLoadDefaultLimits(t *testing.T) {
	cfg, err := Load(writeConfig(t, minimalYAML(t)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Limits.MaxEventsPerSlug != 500 {
		t.Errorf("max_events_per_slug = %d, want 500", cfg.Limits.MaxEventsPerSlug)
	}

	if cfg.Limits.SlugTTL.Duration() != 12*time.Hour {
		t.Errorf("slug_ttl = %v, want 12h", cfg.Limits.SlugTTL.Duration())
	}

	if cfg.Limits.FlushInterval.Duration() != 30*time.Second {
		t.Errorf("flush_interval = %v, want 30s", cfg.Limits.FlushInterval.Duration())
	}

	if cfg.Limits.MaxHitsPerSlugPerMinute != 60 {
		t.Errorf("max_hits_per_slug_per_minute = %d, want 60", cfg.Limits.MaxHitsPerSlugPerMinute)
	}
}

func TestACMEEnabledWithoutEmail(t *testing.T) {
	hash := testHash(t)
	content := `
domain: "example.com"
external_ip: "1.2.3.4"
acme:
  enabled: true
auth:
  - username: "admin"
    password_hash: "` + hash + `"
`

	_, err := Load(writeConfig(t, content))
	if err == nil {
		t.Fatal("expected error for acme.enabled without email")
	}
}

func TestACMEEnabledWithEmail(t *testing.T) {
	hash := testHash(t)
	content := `
domain: "example.com"
external_ip: "1.2.3.4"
acme:
  enabled: true
  email: "admin@example.com"
auth:
  - username: "admin"
    password_hash: "` + hash + `"
`

	cfg, err := Load(writeConfig(t, content))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !cfg.ACME.Enabled {
		t.Error("acme.enabled = false, want true")
	}
}

func TestACMEDisabledWithoutEmail(t *testing.T) {
	hash := testHash(t)
	content := `
domain: "example.com"
external_ip: "1.2.3.4"
acme:
  enabled: false
auth:
  - username: "admin"
    password_hash: "` + hash + `"
`

	_, err := Load(writeConfig(t, content))
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
}

func TestACMEEnabledEnvOverride(t *testing.T) {
	t.Setenv("TRIPFLARE_ACME_ENABLED", "true")
	t.Setenv("TRIPFLARE_ACME_EMAIL", "test@example.com")

	cfg, err := Load(writeConfig(t, minimalYAML(t)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !cfg.ACME.Enabled {
		t.Error("acme.enabled = false after TRIPFLARE_ACME_ENABLED=true")
	}
}

func TestACMEDisabledByDefault(t *testing.T) {
	cfg, err := Load(writeConfig(t, minimalYAML(t)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.ACME.Enabled {
		t.Error("acme.enabled should default to false")
	}
}

func TestLoadWithLimits(t *testing.T) {
	hash := testHash(t)
	content := `
domain: "example.com"
external_ip: "1.2.3.4"
auth:
  - username: "admin"
    password_hash: "` + hash + `"
limits:
  max_events_per_slug: 50
  slug_ttl: "6h"
  flush_interval: "10s"
  max_hits_per_slug_per_minute: 120
`

	cfg, err := Load(writeConfig(t, content))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Limits.MaxEventsPerSlug != 50 {
		t.Errorf("max_events_per_slug = %d, want 50", cfg.Limits.MaxEventsPerSlug)
	}

	if cfg.Limits.SlugTTL.Duration() != 6*time.Hour {
		t.Errorf("slug_ttl = %v, want 6h", cfg.Limits.SlugTTL.Duration())
	}

	if cfg.Limits.FlushInterval.Duration() != 10*time.Second {
		t.Errorf("flush_interval = %v, want 10s", cfg.Limits.FlushInterval.Duration())
	}

	if cfg.Limits.MaxHitsPerSlugPerMinute != 120 {
		t.Errorf("max_hits_per_slug_per_minute = %d, want 120", cfg.Limits.MaxHitsPerSlugPerMinute)
	}
}
