package geoip

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCountryFlag(t *testing.T) {
	tests := []struct {
		code string
		want string
	}{
		{"US", "\U0001F1FA\U0001F1F8"},
		{"DE", "\U0001F1E9\U0001F1EA"},
		{"JP", "\U0001F1EF\U0001F1F5"},
		{"", ""},
		{"A", ""},
		{"ABC", ""},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			got := countryFlag(tt.code)
			if got != tt.want {
				t.Errorf("countryFlag(%q) = %q, want %q", tt.code, got, tt.want)
			}
		})
	}
}

func TestNoopLookup(t *testing.T) {
	svc, err := New("")
	if err != nil {
		t.Fatalf("New empty: %v", err)
	}
	defer svc.Close()

	info := svc.Lookup("1.2.3.4")
	if info.CountryCode != "" || info.Flag != "" || info.ASN != 0 || info.Org != "" {
		t.Errorf("noop should return zero Info, got %+v", info)
	}
}

func TestLookup(t *testing.T) {
	svc, err := New("testdata")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer svc.Close()

	tests := []struct {
		name string
		ip   string
		want Info
	}{
		{
			name: "country and asn",
			ip:   "89.160.20.112",
			want: Info{CountryCode: "SE", Flag: "\U0001F1F8\U0001F1EA", ASN: 29518, Org: "Bredband2 AB"},
		},
		{
			name: "ipv4-mapped ipv6 resolves as ipv4",
			ip:   "::ffff:89.160.20.112",
			want: Info{CountryCode: "SE", Flag: "\U0001F1F8\U0001F1EA", ASN: 29518, Org: "Bredband2 AB"},
		},
		{
			name: "asn only",
			ip:   "1.128.0.0",
			want: Info{ASN: 1221, Org: "Telstra Pty Ltd"},
		},
		{
			name: "country only",
			ip:   "2.125.160.216",
			want: Info{CountryCode: "GB", Flag: "\U0001F1EC\U0001F1E7"},
		},
		{
			name: "absent from both databases",
			ip:   "127.0.0.1",
			want: Info{},
		},
		{
			name: "unparseable",
			ip:   "not-an-ip",
			want: Info{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := svc.Lookup(tt.ip)
			if got != tt.want {
				t.Errorf("Lookup(%q) = %+v, want %+v", tt.ip, got, tt.want)
			}
		})
	}
}

// fixtureDir copies the testdata databases into a fresh dir under the given
// names, stamping each with an mtime a month after the previous one (so index 0
// is oldest) — or all with the same mtime when sameMtime is set.
func fixtureDir(t *testing.T, sameMtime bool, names ...string) string {
	t.Helper()

	dir := t.TempDir()
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	for i, name := range names {
		if sameMtime {
			i = 0
		}

		src := "testdata/geolite2-country-test.mmdb"
		if strings.Contains(strings.ToLower(name), "asn") {
			src = "testdata/geolite2-asn-test.mmdb"
		}

		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("read fixture %s: %v", src, err)
		}

		dst := filepath.Join(dir, name)
		if err := os.WriteFile(dst, data, 0o600); err != nil {
			t.Fatalf("write %s: %v", dst, err)
		}

		mod := base.AddDate(0, i, 0)
		if err := os.Chtimes(dst, mod, mod); err != nil {
			t.Fatalf("chtimes %s: %v", dst, err)
		}
	}

	return dir
}

func TestFindDB(t *testing.T) {
	tests := []struct {
		name      string
		files     []string
		sameMtime bool
		keyword   string
		want      string
		wantErr   bool
	}{
		{
			name:    "newest mtime wins over lexicographically first",
			files:   []string{"dbip-country-lite-2026-03.mmdb", "dbip-country-lite-2026-06.mmdb"},
			keyword: "country",
			want:    "dbip-country-lite-2026-06.mmdb",
		},
		{
			name:    "newest wins even when its name sorts first",
			files:   []string{"zzz-country.mmdb", "aaa-country.mmdb"},
			keyword: "country",
			want:    "aaa-country.mmdb",
		},
		{
			name:    "provider casing is matched",
			files:   []string{"GeoLite2-Country.mmdb"},
			keyword: "country",
			want:    "GeoLite2-Country.mmdb",
		},
		{
			name:    "asn keyword does not match a country database",
			files:   []string{"dbip-country-lite-2026-06.mmdb"},
			keyword: "asn",
			wantErr: true,
		},
		{
			name:    "non-mmdb files are ignored",
			files:   []string{"country.mmdb.gz", "notes-country.txt"},
			keyword: "country",
			wantErr: true,
		},
		{
			name:      "equal mtimes fall back to the name",
			files:     []string{"b-country.mmdb", "c-country.mmdb", "a-country.mmdb"},
			sameMtime: true,
			keyword:   "country",
			want:      "c-country.mmdb",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := findDB(fixtureDir(t, tt.sameMtime, tt.files...), tt.keyword)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("findDB(%q) = %q, want error", tt.keyword, got)
				}

				return
			}

			if err != nil {
				t.Fatalf("findDB(%q): %v", tt.keyword, err)
			}

			if filepath.Base(got) != tt.want {
				t.Errorf("findDB(%q) = %q, want %q", tt.keyword, filepath.Base(got), tt.want)
			}
		})
	}
}

func TestFindDBJudgesSymlinkByItsTarget(t *testing.T) {
	dir := fixtureDir(t, false, "direct-country.mmdb")

	target := filepath.Join(fixtureDir(t, false, "target-country.mmdb"), "target-country.mmdb")
	stale := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	if err := os.Chtimes(target, stale, stale); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	// The link itself is created now, so judging by its own mtime would pick it.
	if err := os.Symlink(target, filepath.Join(dir, "linked-country.mmdb")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	got, err := findDB(dir, "country")
	if err != nil {
		t.Fatalf("findDB: %v", err)
	}

	if filepath.Base(got) != "direct-country.mmdb" {
		t.Errorf("findDB = %q, want direct-country.mmdb", filepath.Base(got))
	}
}

func TestNewUsesNewestDatabases(t *testing.T) {
	dir := fixtureDir(t, false,
		"dbip-country-lite-2026-03.mmdb",
		"dbip-asn-lite-2026-03.mmdb",
		"GeoLite2-Country.mmdb",
		"GeoLite2-ASN.mmdb",
	)

	// The fixtures are byte-identical, so a lookup cannot reveal which file New
	// opened — the startup log is the only observable that can.
	var logged bytes.Buffer

	restore := slog.Default()
	defer slog.SetDefault(restore)

	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))

	svc, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer svc.Close()

	for _, want := range []string{"GeoLite2-Country.mmdb", "GeoLite2-ASN.mmdb"} {
		if !strings.Contains(logged.String(), want) {
			t.Errorf("startup log does not name %s\nlog: %s", want, logged.String())
		}
	}

	if strings.Contains(logged.String(), "2026-03") {
		t.Errorf("startup log names a superseded database\nlog: %s", logged.String())
	}

	if got := svc.Lookup("89.160.20.112"); got.CountryCode != "SE" || got.ASN != 29518 {
		t.Errorf("Lookup = %+v, want SE/29518", got)
	}
}

func TestNewMissingDir(t *testing.T) {
	svc, err := New("/nonexistent/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer svc.Close()

	info := svc.Lookup("1.2.3.4")
	if info.CountryCode != "" {
		t.Error("missing dir should return noop, got data")
	}
}
