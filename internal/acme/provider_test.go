package acme

import (
	"context"
	"testing"

	"github.com/go-acme/lego/v5/challenge/dns01"
)

func TestDNSProviderPresentCleanUp(t *testing.T) {
	const (
		domain  = "example.com"
		keyAuth = "test-key-authorization"
	)

	ctx := context.Background()
	cs := NewChallengeStore()
	provider := &dnsProvider{challenge: cs}

	info := dns01.GetChallengeInfo(ctx, domain, keyAuth)

	if err := provider.Present(ctx, domain, "token", keyAuth); err != nil {
		t.Fatalf("Present: %v", err)
	}

	vals := cs.GetTXT(info.FQDN)
	if len(vals) != 1 || vals[0] != info.Value {
		t.Fatalf("after Present: GetTXT(%q) = %v, want [%q]", info.FQDN, vals, info.Value)
	}

	if err := provider.CleanUp(ctx, domain, "token", keyAuth); err != nil {
		t.Fatalf("CleanUp: %v", err)
	}

	if vals := cs.GetTXT(info.FQDN); len(vals) != 0 {
		t.Errorf("after CleanUp: GetTXT(%q) = %v, want empty", info.FQDN, vals)
	}
}
