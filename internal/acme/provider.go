package acme

import (
	"log/slog"

	"github.com/go-acme/lego/v4/challenge/dns01"
)

type dnsProvider struct {
	challenge ChallengeStore
}

func (p *dnsProvider) Present(domain, _ /* token */, keyAuth string) error {
	info := dns01.GetChallengeInfo(domain, keyAuth)
	slog.Info("presenting acme challenge", "fqdn", info.FQDN, "value", info.Value)
	p.challenge.Set(info.FQDN, info.Value)

	return nil
}

func (p *dnsProvider) CleanUp(domain, _ /* token */, keyAuth string) error {
	info := dns01.GetChallengeInfo(domain, keyAuth)
	slog.Info("cleaning up acme challenge", "fqdn", info.FQDN)
	p.challenge.Delete(info.FQDN, info.Value)

	return nil
}
