package acme

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge/dns01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
)

const renewBeforeDays = 30

type Service interface {
	GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error)
	Run(ctx context.Context) error
}

type svc struct {
	domain    string
	email     string
	staging   bool
	enabled   bool
	certPath  string
	challenge ChallengeStore

	mu   sync.RWMutex
	cert *tls.Certificate
}

var _ Service = (*svc)(nil)

func New(domain, email, certPath string, staging, enabled bool, challenge ChallengeStore) (Service, error) {
	cert, err := generateSelfSigned(domain)
	if err != nil {
		return nil, fmt.Errorf("generate self-signed cert: %w", err)
	}

	slog.Info("generated self-signed certificate", "domain", domain)

	return &svc{
		domain:    domain,
		email:     email,
		staging:   staging,
		enabled:   enabled,
		certPath:  certPath,
		challenge: challenge,
		cert:      cert,
	}, nil
}

func (s *svc) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.cert == nil {
		return nil, errors.New("no certificate available")
	}

	return s.cert, nil
}

func (s *svc) Run(ctx context.Context) error {
	if !s.enabled {
		slog.Info("ACME disabled, using self-signed certificate")
		return nil
	}

	if err := os.MkdirAll(s.certDir(), 0o700); err != nil {
		return fmt.Errorf("create cert dir: %w", err)
	}

	if err := s.loadOrObtain(); err != nil {
		slog.Error("initial cert obtainment failed, will retry", "error", err)
	}

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if s.needsRenewal() {
				slog.Info("certificate needs renewal, renewing")

				if err := s.obtain(); err != nil {
					slog.Error("failed to renew certificate", "error", err)
				}
			}
		}
	}
}

func (s *svc) certDir() string {
	if s.staging {
		return filepath.Join(s.certPath, "staging")
	}

	return filepath.Join(s.certPath, "prod")
}

func (s *svc) certFiles() (certFile, keyFile string) {
	dir := s.certDir()

	return filepath.Join(dir, "cert.pem"),
		filepath.Join(dir, "key.pem")
}

func (s *svc) loadOrObtain() error {
	certFile, keyFile := s.certFiles()

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		slog.Info("no existing certificate, requesting new one", "cert_file", certFile, "error", err)
		return s.obtain()
	}

	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		slog.Warn("failed to parse existing certificate, requesting new one", "error", err)
		return s.obtain()
	}

	if time.Until(leaf.NotAfter) <= renewBeforeDays*24*time.Hour {
		slog.Info("existing certificate expiring soon, requesting new one", "expires", leaf.NotAfter)
		return s.obtain()
	}

	cert.Leaf = leaf
	slog.Info(
		"loaded existing certificate",
		"cert_file",
		certFile,
		"issuer",
		leaf.Issuer.CommonName,
		"expires",
		leaf.NotAfter,
	)

	s.mu.Lock()
	s.cert = &cert
	s.mu.Unlock()

	return nil
}

func (s *svc) obtain() error {
	user, err := s.getOrCreateUser()
	if err != nil {
		return fmt.Errorf("acme user: %w", err)
	}

	client, err := s.setupACMEClient(user)
	if err != nil {
		return err
	}

	if err := s.registerIfNeeded(client, user); err != nil {
		return err
	}

	return s.requestCertificate(client)
}

func (s *svc) setupACMEClient(user *acmeUser) (*lego.Client, error) {
	cfg := lego.NewConfig(user)
	if s.staging {
		cfg.CADirURL = lego.LEDirectoryStaging
	} else {
		cfg.CADirURL = lego.LEDirectoryProduction
	}

	cfg.Certificate.KeyType = certcrypto.RSA2048

	client, err := lego.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("create acme client: %w", err)
	}

	provider := &dnsProvider{challenge: s.challenge}
	if err := client.Challenge.SetDNS01Provider(
		provider,
		dns01.AddRecursiveNameservers([]string{"1.1.1.1:53", "8.8.8.8:53"}),
	); err != nil {
		return nil, fmt.Errorf("set dns provider: %w", err)
	}

	return client, nil
}

func (s *svc) registerIfNeeded(client *lego.Client, user *acmeUser) error {
	if user.Registration != nil {
		return nil
	}

	reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		return fmt.Errorf("register: %w", err)
	}

	user.Registration = reg
	s.saveUser(user)

	return nil
}

func (s *svc) requestCertificate(client *lego.Client) error {
	wildcard := "*." + s.domain
	request := certificate.ObtainRequest{
		Domains: []string{s.domain, wildcard},
		Bundle:  true,
	}

	certificates, err := client.Certificate.Obtain(request)
	if err != nil {
		return fmt.Errorf("obtain certificate for %s: %w", wildcard, err)
	}

	// Validate before writing to disk to avoid boot loops
	tlsCert, err := tls.X509KeyPair(certificates.Certificate, certificates.PrivateKey)
	if err != nil {
		return fmt.Errorf("parse obtained cert: %w", err)
	}

	leaf, err := x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		return fmt.Errorf("parse cert leaf: %w", err)
	}

	tlsCert.Leaf = leaf

	certFile, keyFile := s.certFiles()

	if err := os.WriteFile(certFile, certificates.Certificate, 0o600); err != nil {
		return fmt.Errorf("write cert: %w", err)
	}

	if err := os.WriteFile(keyFile, certificates.PrivateKey, 0o600); err != nil {
		return fmt.Errorf("write key: %w", err)
	}

	s.mu.Lock()
	s.cert = &tlsCert
	s.mu.Unlock()

	slog.Info(
		"obtained new certificate",
		"domain",
		wildcard,
		"cert_file",
		certFile,
		"issuer",
		leaf.Issuer.CommonName,
		"expires",
		leaf.NotAfter,
	)

	return nil
}

func (s *svc) needsRenewal() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.cert == nil || s.cert.Leaf == nil {
		return true
	}

	return time.Until(s.cert.Leaf.NotAfter) < renewBeforeDays*24*time.Hour
}
