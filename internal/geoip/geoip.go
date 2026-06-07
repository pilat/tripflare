package geoip

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"

	"github.com/oschwald/maxminddb-golang"
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

// New opens geoip databases from dir. Accepts any mmdb provider
// (DB-IP Lite, MaxMind GeoLite2) — files are matched by keyword
// (*country*.mmdb, *asn*.mmdb). Returns noop when dir is empty
// or no databases are found.
func New(dir string) (Service, error) {
	if dir == "" {
		return &noop{}, nil
	}

	countryPath, err := findDB(dir, "*country*")
	if err != nil {
		return &noop{}, nil //nolint:nilerr // geoip is optional; missing DBs fall back to noop
	}

	asnPath, err := findDB(dir, "*asn*")
	if err != nil {
		return &noop{}, nil //nolint:nilerr // geoip is optional; missing DBs fall back to noop
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

	return &svc{country: country, asn: asn}, nil
}

func (s *svc) Lookup(ip string) Info {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return Info{}
	}

	var info Info

	var countryRecord struct {
		Country struct {
			ISOCode string `maxminddb:"iso_code"`
		} `maxminddb:"country"`
	}
	if err := s.country.Lookup(parsed, &countryRecord); err == nil {
		info.CountryCode = countryRecord.Country.ISOCode
		info.Flag = countryFlag(info.CountryCode)
	}

	var asnRecord struct {
		ASN uint   `maxminddb:"autonomous_system_number"`
		Org string `maxminddb:"autonomous_system_organization"`
	}
	if err := s.asn.Lookup(parsed, &asnRecord); err == nil {
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

// findDB locates a single .mmdb file matching pattern in dir.
func findDB(dir, pattern string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, pattern+".mmdb"))
	if err != nil {
		return "", fmt.Errorf("glob %s in %s: %w", pattern, dir, err)
	}

	if len(matches) == 0 {
		return "", fmt.Errorf("no %s.mmdb file found in %s", pattern, dir)
	}

	return matches[0], nil
}

// countryFlag converts a 2-letter ISO country code to a flag emoji.
// Each letter is mapped to a regional indicator symbol (U+1F1E6..U+1F1FF).
func countryFlag(code string) string {
	if len(code) != 2 {
		return ""
	}

	return string(rune(code[0])-'A'+0x1F1E6) + string(rune(code[1])-'A'+0x1F1E6)
}
