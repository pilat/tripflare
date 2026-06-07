package dns

import (
	"net"
	"testing"

	"github.com/miekg/dns"
)

type mockChallenges struct {
	records map[string][]string
}

func (m *mockChallenges) Set(fqdn, value string)      { m.records[fqdn] = append(m.records[fqdn], value) }
func (m *mockChallenges) Delete(fqdn, value string)   {}
func (m *mockChallenges) GetTXT(fqdn string) []string { return m.records[fqdn] }

func TestNewValidatesIP(t *testing.T) {
	_, err := New("example.com", "not-an-ip", ":5353", nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for invalid IP")
	}

	s, err := New("example.com", "1.2.3.4", ":5353", nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	impl := s.(*svc)
	if !impl.externalIP.Equal(net.ParseIP("1.2.3.4")) {
		t.Errorf("externalIP = %v, want 1.2.3.4", impl.externalIP)
	}
}

func TestExtractSlug(t *testing.T) {
	s := &svc{domain: "trap.example.com."}

	tests := []struct {
		name  string
		qname string
		want  string
	}{
		{"simple slug", "abc123.trap.example.com.", "abc123"},
		{"nested subdomain extracts slug", "prefix.abc123.trap.example.com.", "abc123"},
		{"deeply nested", "a.b.abc123.trap.example.com.", "abc123"},
		{"bare domain", "trap.example.com.", ""},
		{"unrelated domain", "other.com.", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.extractSlug(tt.qname)
			if got != tt.want {
				t.Errorf("extractSlug(%q) = %q, want %q", tt.qname, got, tt.want)
			}
		})
	}
}

func TestExtractClientSubnet(t *testing.T) {
	t.Run("with ECS", func(t *testing.T) {
		r := new(dns.Msg)
		r.SetQuestion("test.example.com.", dns.TypeA)

		opt := &dns.OPT{
			Hdr: dns.RR_Header{Name: ".", Rrtype: dns.TypeOPT},
			Option: []dns.EDNS0{
				&dns.EDNS0_SUBNET{
					Code:          dns.EDNS0SUBNET,
					Family:        1,
					SourceNetmask: 24,
					Address:       net.ParseIP("203.0.113.0"),
				},
			},
		}
		r.Extra = append(r.Extra, opt)

		got := extractClientSubnet(r)
		if got != "203.0.113.0/24" {
			t.Errorf("extractClientSubnet = %q, want 203.0.113.0/24", got)
		}
	})

	t.Run("without ECS", func(t *testing.T) {
		r := new(dns.Msg)
		r.SetQuestion("test.example.com.", dns.TypeA)

		got := extractClientSubnet(r)
		if got != "" {
			t.Errorf("extractClientSubnet = %q, want empty", got)
		}
	})
}

func TestIsSubdomain(t *testing.T) {
	s := &svc{domain: "trap.example.com."}

	tests := []struct {
		qname string
		want  bool
	}{
		{"abc.trap.example.com.", true},
		{"trap.example.com.", false},
		{"other.com.", false},
	}

	for _, tt := range tests {
		t.Run(tt.qname, func(t *testing.T) {
			if got := s.isSubdomain(tt.qname); got != tt.want {
				t.Errorf("isSubdomain(%q) = %v, want %v", tt.qname, got, tt.want)
			}
		})
	}
}

func TestHandleACME(t *testing.T) {
	challenges := &mockChallenges{records: map[string][]string{
		"_acme-challenge.trap.example.com.": {"token-wildcard", "token-apex"},
	}}
	s := &svc{
		domain:     "trap.example.com.",
		externalIP: net.ParseIP("1.2.3.4"),
		challenges: challenges,
	}

	t.Run("multiple TXT records", func(t *testing.T) {
		msg := new(dns.Msg)
		msg.SetReply(&dns.Msg{})
		s.handleACME(msg, "_acme-challenge.trap.example.com.")

		if len(msg.Answer) != 2 {
			t.Fatalf("expected 2 answers, got %d", len(msg.Answer))
		}

		for i, rr := range msg.Answer {
			txt, ok := rr.(*dns.TXT)
			if !ok {
				t.Fatalf("answer[%d] is not TXT", i)
			}

			if len(txt.Txt) != 1 {
				t.Fatalf("answer[%d] has %d strings, want 1", i, len(txt.Txt))
			}
		}

		if msg.Answer[0].(*dns.TXT).Txt[0] != "token-wildcard" {
			t.Errorf("answer[0] = %q, want token-wildcard", msg.Answer[0].(*dns.TXT).Txt[0])
		}

		if msg.Answer[1].(*dns.TXT).Txt[0] != "token-apex" {
			t.Errorf("answer[1] = %q, want token-apex", msg.Answer[1].(*dns.TXT).Txt[0])
		}
	})

	t.Run("no challenge returns NXDOMAIN", func(t *testing.T) {
		msg := new(dns.Msg)
		msg.SetReply(&dns.Msg{})
		s.handleACME(msg, "_acme-challenge.other.example.com.")

		if msg.Rcode != dns.RcodeNameError {
			t.Errorf("rcode = %d, want NXDOMAIN (%d)", msg.Rcode, dns.RcodeNameError)
		}

		if len(msg.Answer) != 0 {
			t.Errorf("expected 0 answers, got %d", len(msg.Answer))
		}
	})
}

func TestIsACMEChallenge(t *testing.T) {
	s := &svc{domain: "trap.example.com."}

	tests := []struct {
		qname string
		want  bool
	}{
		{"_acme-challenge.trap.example.com.", true},
		{"abc.trap.example.com.", false},
		{"_acme-challenge.other.com.", false},
	}

	for _, tt := range tests {
		t.Run(tt.qname, func(t *testing.T) {
			if got := s.isACMEChallenge(tt.qname); got != tt.want {
				t.Errorf("isACMEChallenge(%q) = %v, want %v", tt.qname, got, tt.want)
			}
		})
	}
}
