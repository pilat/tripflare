package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

type duration time.Duration

type AuthEntry struct {
	Username     string `yaml:"username"`
	PasswordHash string `yaml:"password_hash"`
}

type LimitsConfig struct {
	MaxEventsPerSlug        int      `yaml:"max_events_per_slug"`
	SlugTTL                 duration `yaml:"slug_ttl"`
	FlushInterval           duration `yaml:"flush_interval"`
	MaxHitsPerSlugPerMinute int      `yaml:"max_hits_per_slug_per_minute"`
}

type ListenConfig struct {
	DNS   string `yaml:"dns"`
	HTTP  string `yaml:"http"`
	HTTPS string `yaml:"https"`
}

type ACMEConfig struct {
	Enabled bool   `yaml:"enabled"`
	Email   string `yaml:"email"`
	Staging bool   `yaml:"staging"`
}

type Config struct {
	Domain      string       `yaml:"domain"`
	ExternalIP  string       `yaml:"external_ip"`
	Nameservers []string     `yaml:"nameservers"`
	LogFormat   string       `yaml:"log_format"`
	Listen      ListenConfig `yaml:"listen"`
	SQLitePath  string       `yaml:"sqlite_path"`
	CertPath    string       `yaml:"cert_path"`
	ACME        ACMEConfig   `yaml:"acme"`
	Auth        []AuthEntry  `yaml:"auth"`
	Limits      LimitsConfig `yaml:"limits"`
	GeoIPPath   string       `yaml:"geoip_path"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // config path from CLI flag
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	cfg := &Config{
		Listen: ListenConfig{
			DNS:   ":53",
			HTTP:  ":80",
			HTTPS: ":443",
		},
		SQLitePath: "./tripflare.db",
		CertPath:   "./certs",
		ACME: ACMEConfig{
			Staging: true,
		},
		Limits: defaultLimits(),
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	applyEnvOverrides(cfg)

	if err := validate(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Duration returns the underlying time.Duration value.
func (d duration) Duration() time.Duration {
	return time.Duration(d)
}

func (d *duration) UnmarshalYAML(value *yaml.Node) error {
	parsed, err := time.ParseDuration(value.Value)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", value.Value, err)
	}

	*d = duration(parsed)

	return nil
}

func defaultLimits() LimitsConfig {
	return LimitsConfig{
		MaxEventsPerSlug:        500,
		SlugTTL:                 duration(12 * time.Hour),
		FlushInterval:           duration(30 * time.Second),
		MaxHitsPerSlugPerMinute: 60,
	}
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("TRIPFLARE_DOMAIN"); v != "" {
		cfg.Domain = v
	}

	if v := os.Getenv("TRIPFLARE_EXTERNAL_IP"); v != "" {
		cfg.ExternalIP = v
	}

	if v := os.Getenv("TRIPFLARE_SQLITE_PATH"); v != "" {
		cfg.SQLitePath = v
	}

	if v := os.Getenv("TRIPFLARE_CERT_PATH"); v != "" {
		cfg.CertPath = v
	}

	if v := os.Getenv("TRIPFLARE_ACME_EMAIL"); v != "" {
		cfg.ACME.Email = v
	}

	if v := os.Getenv("TRIPFLARE_ACME_ENABLED"); v == "true" || v == "1" {
		cfg.ACME.Enabled = true
	}

	if v := os.Getenv("TRIPFLARE_GEOIP_PATH"); v != "" {
		cfg.GeoIPPath = v
	}
}

func validate(cfg *Config) error {
	if cfg.Domain == "" {
		return errors.New("config: domain is required")
	}

	if cfg.ExternalIP == "" {
		return errors.New("config: external_ip is required")
	}

	if cfg.ACME.Enabled && cfg.ACME.Email == "" {
		return errors.New("config: acme.email is required when acme.enabled is true")
	}

	if len(cfg.Auth) == 0 {
		return errors.New("config: auth requires at least one entry")
	}

	for i, entry := range cfg.Auth {
		if entry.Username == "" {
			return fmt.Errorf("config: auth[%d].username is required", i)
		}

		if entry.PasswordHash == "" {
			return fmt.Errorf("config: auth[%d].password_hash is required", i)
		}

		if _, err := bcrypt.Cost([]byte(entry.PasswordHash)); err != nil {
			return fmt.Errorf("config: auth[%d].password_hash is not a valid bcrypt hash", i)
		}
	}

	return nil
}
