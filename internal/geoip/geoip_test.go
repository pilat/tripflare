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
