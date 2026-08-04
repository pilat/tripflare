package geoip

import (
	"testing"
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
