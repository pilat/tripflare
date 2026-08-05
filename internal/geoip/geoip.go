package geoip

import (
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oschwald/maxminddb-golang/v2"
)

type Info struct {
	CountryCode string `json:"country_code,omitempty"`
	Flag        string `json:"country_flag,omitempty"`
	ASN         uint   `json:"asn,omitempty"`
	Org         string `json:"org,omitempty"`
}

type Service interface {
	Lookup(ip string) Info
	Close() error
}

type svc struct {
	country *maxminddb.Reader
	asn     *maxminddb.Reader
}

type noop struct{}

var (
	_ Service = (*svc)(nil)
	_ Service = (*noop)(nil)
)

// New opens geoip databases from dir. Accepts any mmdb provider (DB-IP Lite,
// MaxMind GeoLite2) — a database is any *.mmdb file whose name contains
// "country" or "asn", case-insensitively, and the most recently modified match
// wins. Returns noop when dir is empty or no databases are found.
func New(dir string) (Service, error) {
	if dir == "" {
		return &noop{}, nil
	}

	countryPath, err := findDB(dir, "country")
	if err != nil {
		slog.Warn("geoip disabled, no country database found", "dir", dir, "error", err)

		return &noop{}, nil
	}

	asnPath, err := findDB(dir, "asn")
	if err != nil {
		slog.Warn("geoip disabled, no asn database found", "dir", dir, "error", err)

		return &noop{}, nil
	}

	country, err := maxminddb.Open(countryPath)
	if err != nil {
		return nil, fmt.Errorf("open country db %s: %w", countryPath, err)
	}

	asn, err := maxminddb.Open(asnPath)
	if err != nil {
		_ = country.Close()
		return nil, fmt.Errorf("open asn db %s: %w", asnPath, err)
	}

	slog.Info("geoip databases loaded", "country", countryPath, "asn", asnPath)

	return &svc{country: country, asn: asn}, nil
}

func (s *svc) Lookup(ip string) Info {
	parsed, err := netip.ParseAddr(ip)
	if err != nil {
		return Info{}
	}

	// Unmap ::ffff:a.b.c.d — an IPv4-only database rejects it as an IPv6 lookup.
	parsed = parsed.Unmap()

	var info Info

	var countryRecord struct {
		Country struct {
			ISOCode string `maxminddb:"iso_code"`
		} `maxminddb:"country"`
	}
	if err := s.country.Lookup(parsed).Decode(&countryRecord); err == nil {
		info.CountryCode = countryRecord.Country.ISOCode
		info.Flag = countryFlag(info.CountryCode)
	}

	var asnRecord struct {
		ASN uint   `maxminddb:"autonomous_system_number"`
		Org string `maxminddb:"autonomous_system_organization"`
	}
	if err := s.asn.Lookup(parsed).Decode(&asnRecord); err == nil {
		info.ASN = asnRecord.ASN
		info.Org = asnRecord.Org
	}

	return info
}

func (s *svc) Close() error {
	return errors.Join(s.country.Close(), s.asn.Close())
}

func (n *noop) Lookup(string) Info { return Info{} }
func (n *noop) Close() error       { return nil }

// findDB returns the most recently modified .mmdb file in dir whose name
// contains keyword, compared case-insensitively so a provider's own casing
// (GeoLite2-Country.mmdb) matches too.
func findDB(dir, keyword string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read geoip dir %s: %w", dir, err)
	}

	var (
		newest    string
		newestMod time.Time
	)

	for _, entry := range entries {
		if !isDatabase(entry.Name(), keyword) {
			continue
		}

		// Stat, not entry.Info: a symlinked database is only as fresh as its target.
		info, err := os.Stat(filepath.Join(dir, entry.Name()))
		if err != nil || info.IsDir() {
			continue
		}

		if newest != "" && !supersedes(entry.Name(), info.ModTime(), newest, newestMod) {
			continue
		}

		newest, newestMod = entry.Name(), info.ModTime()
	}

	if newest == "" {
		return "", fmt.Errorf("no %q .mmdb file found in %s", keyword, dir)
	}

	return filepath.Join(dir, newest), nil
}

// isDatabase reports whether filename is an .mmdb file containing keyword.
func isDatabase(filename, keyword string) bool {
	name := strings.ToLower(filename)

	return strings.HasSuffix(name, ".mmdb") && strings.Contains(name, strings.ToLower(keyword))
}

// supersedes reports whether a candidate should replace the current pick.
// Name breaks mtime ties, so a directory always resolves the same way.
func supersedes(name string, mod time.Time, curName string, curMod time.Time) bool {
	if mod.Equal(curMod) {
		return name > curName
	}

	return mod.After(curMod)
}

// countryFlag converts a 2-letter ISO country code to a flag emoji.
// Each letter is mapped to a regional indicator symbol (U+1F1E6..U+1F1FF).
func countryFlag(code string) string {
	if len(code) != 2 {
		return ""
	}

	return string(rune(code[0])-'A'+0x1F1E6) + string(rune(code[1])-'A'+0x1F1E6)
}
