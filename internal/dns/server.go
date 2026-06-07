package dns

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"

	"github.com/miekg/dns"

	"github.com/pilat/tripflare/internal/acme"
	"github.com/pilat/tripflare/internal/registry"
)

type Service interface {
	ListenAndServe(ctx context.Context, ready chan<- struct{}) error
}

type svc struct {
	domain      string
	externalIP  net.IP
	listenAddr  string
	registry    registry.Service
	challenges  acme.ChallengeStore
	nameservers []string
}

var _ Service = (*svc)(nil)

func New(
	domain, externalIP, listenAddr string,
	nameservers []string,
	reg registry.Service,
	challenges acme.ChallengeStore,
) (Service, error) {
	ip := net.ParseIP(externalIP)
	if ip == nil {
		return nil, fmt.Errorf("invalid external IP: %q", externalIP)
	}

	fqdnNS := make([]string, len(nameservers))
	for i, ns := range nameservers {
		fqdnNS[i] = dns.Fqdn(ns)
	}

	return &svc{
		domain:      dns.Fqdn(domain),
		externalIP:  ip,
		listenAddr:  listenAddr,
		registry:    reg,
		challenges:  challenges,
		nameservers: fqdnNS,
	}, nil
}

func (s *svc) ListenAndServe(ctx context.Context, ready chan<- struct{}) error {
	mux := dns.NewServeMux()
	mux.HandleFunc(".", s.handleQuery)

	serversReady := make(chan struct{}, 2)
	notifyStarted := func() { serversReady <- struct{}{} }

	udpServer := &dns.Server{Addr: s.listenAddr, Net: "udp", Handler: mux, NotifyStartedFunc: notifyStarted}
	tcpServer := &dns.Server{Addr: s.listenAddr, Net: "tcp", Handler: mux, NotifyStartedFunc: notifyStarted}

	var wg sync.WaitGroup

	errCh := make(chan error, 2)

	wg.Add(2)

	go func() {
		defer wg.Done()

		slog.Info("dns server starting", "addr", s.listenAddr, "proto", "udp")

		if err := udpServer.ListenAndServe(); err != nil {
			errCh <- fmt.Errorf("dns udp: %w", err)
		}
	}()
	go func() {
		defer wg.Done()

		slog.Info("dns server starting", "addr", s.listenAddr, "proto", "tcp")

		if err := tcpServer.ListenAndServe(); err != nil {
			errCh <- fmt.Errorf("dns tcp: %w", err)
		}
	}()

	for range 2 {
		select {
		case <-serversReady:
		case err := <-errCh:
			return err
		}
	}

	close(ready)

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("dns server shutting down")

		if err := udpServer.Shutdown(); err != nil {
			slog.Error("dns udp shutdown error", "error", err)
		}

		if err := tcpServer.Shutdown(); err != nil {
			slog.Error("dns tcp shutdown error", "error", err)
		}

		wg.Wait()

		return nil
	}
}

func (s *svc) handleQuery(w dns.ResponseWriter, r *dns.Msg) {
	msg := new(dns.Msg)
	msg.SetReply(r)
	msg.Authoritative = true

	answered := false

	for _, q := range r.Question {
		qname := strings.ToLower(q.Name)

		switch {
		case q.Qtype == dns.TypeTXT && s.isACMEChallenge(qname):
			s.handleACME(msg, qname)

			answered = true
		case qname == s.domain:
			s.handleSubdomain(msg, q)

			answered = true
		case s.isSubdomain(qname):
			s.handleSubdomain(msg, q)
			s.recordEvent(qname, w.RemoteAddr(), q.Qtype, r)

			answered = true
		}
	}

	if !answered {
		msg.Rcode = dns.RcodeNameError
	}

	if err := w.WriteMsg(msg); err != nil {
		slog.Error("failed to write dns response", "error", err)
	}
}

func (s *svc) handleACME(msg *dns.Msg, qname string) {
	values := s.challenges.GetTXT(qname)
	if len(values) == 0 {
		msg.Rcode = dns.RcodeNameError
		return
	}

	for _, txt := range values {
		msg.Answer = append(msg.Answer, &dns.TXT{
			Hdr: dns.RR_Header{Name: qname, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 1},
			Txt: []string{txt},
		})
	}
}

func (s *svc) handleSubdomain(msg *dns.Msg, q dns.Question) {
	switch q.Qtype {
	case dns.TypeA:
		s.answerA(msg, q)
	case dns.TypeAAAA:
		s.answerAAAA(msg, q)
	case dns.TypeNS:
		s.answerNS(msg, q)
	case dns.TypeSOA:
		s.answerSOA(msg, q)
	}
}

func (s *svc) answerA(msg *dns.Msg, q dns.Question) {
	if ip4 := s.externalIP.To4(); ip4 != nil {
		msg.Answer = append(msg.Answer, &dns.A{
			Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 1},
			A:   ip4,
		})
	}
}

func (s *svc) answerAAAA(msg *dns.Msg, q dns.Question) {
	if ip6 := s.externalIP.To16(); ip6 != nil && s.externalIP.To4() == nil {
		msg.Answer = append(msg.Answer, &dns.AAAA{
			Hdr:  dns.RR_Header{Name: q.Name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 1},
			AAAA: ip6,
		})
	}
}

func (s *svc) answerNS(msg *dns.Msg, q dns.Question) {
	if q.Name != s.domain {
		return
	}

	for _, ns := range s.nameservers {
		msg.Answer = append(msg.Answer, &dns.NS{
			Hdr: dns.RR_Header{Name: s.domain, Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: 3600},
			Ns:  ns,
		})
	}
}

func (s *svc) answerSOA(msg *dns.Msg, q dns.Question) {
	if q.Name == s.domain && len(s.nameservers) > 0 {
		msg.Answer = append(msg.Answer, s.soaRecord())
	}
}

func (s *svc) soaRecord() *dns.SOA {
	return &dns.SOA{
		Hdr:     dns.RR_Header{Name: s.domain, Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: 3600},
		Ns:      s.nameservers[0],
		Mbox:    "hostmaster." + s.domain,
		Serial:  1,
		Refresh: 3600,
		Retry:   600,
		Expire:  604800,
		Minttl:  60,
	}
}

func (s *svc) recordEvent(qname string, remoteAddr net.Addr, qtype uint16, r *dns.Msg) {
	slug := s.extractSlug(qname)
	if slug == "" {
		return
	}

	if !s.registry.SlugExists(slug) {
		return
	}

	host, _, _ := net.SplitHostPort(remoteAddr.String())
	clientSubnet := extractClientSubnet(r)
	s.registry.RecordDNS(slug, host, dns.TypeToString[qtype], qname, clientSubnet)
}

func extractClientSubnet(r *dns.Msg) string {
	opt := r.IsEdns0()
	if opt == nil {
		return ""
	}

	for _, o := range opt.Option {
		if subnet, ok := o.(*dns.EDNS0_SUBNET); ok {
			return fmt.Sprintf("%s/%d", subnet.Address.String(), subnet.SourceNetmask)
		}
	}

	return ""
}

func (s *svc) isACMEChallenge(qname string) bool {
	return strings.HasPrefix(qname, "_acme-challenge.") && strings.HasSuffix(qname, s.domain)
}

func (s *svc) isSubdomain(qname string) bool {
	return strings.HasSuffix(qname, "."+s.domain) && qname != s.domain
}

func (s *svc) extractSlug(qname string) string {
	suffix := "." + s.domain
	if !strings.HasSuffix(qname, suffix) {
		return ""
	}

	sub := strings.TrimSuffix(qname, suffix)
	// For nested subdomains like "prefix.slug.domain", extract "slug"
	if i := strings.LastIndex(sub, "."); i >= 0 {
		return sub[i+1:]
	}

	return sub
}
