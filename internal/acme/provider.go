package acme

import (
	"context"
	"log/slog"

	legochallenge "github.com/go-acme/lego/v5/challenge"
	"github.com/go-acme/lego/v5/challenge/dns01"
)

type dnsProvider struct {
	challenge ChallengeStore
}

var _ legochallenge.Provider = (*dnsProvider)(nil)

func (p *dnsProvider) Present(ctx context.Context, domain, _ /* token */, keyAuth string) error {
	info := dns01.GetChallengeInfo(ctx, domain, keyAuth)
	slog.Info("presenting acme challenge", "fqdn", info.FQDN, "value", info.Value)
	p.challenge.Set(info.FQDN, info.Value)

	return nil
}

func (p *dnsProvider) CleanUp(ctx context.Context, domain, _ /* token */, keyAuth string) error {
	info := dns01.GetChallengeInfo(ctx, domain, keyAuth)
	slog.Info("cleaning up acme challenge", "fqdn", info.FQDN)
	p.challenge.Delete(info.FQDN, info.Value)

	return nil
}
