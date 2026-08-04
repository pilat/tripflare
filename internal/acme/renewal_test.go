package acme

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNeedsRenewal(t *testing.T) {
	withLeaf := func(mutate func(*x509.Certificate)) *tls.Certificate {
		cert, err := generateSelfSigned("example.com")
		if err != nil {
			t.Fatalf("generateSelfSigned: %v", err)
		}

		if mutate != nil {
			mutate(cert.Leaf)
		}

		return cert
	}

	tests := []struct {
		name string
		cert *tls.Certificate
		want bool
	}{
		{"nil cert", nil, true},
		{"nil leaf", &tls.Certificate{}, true},
		{"expiring within 30d", withLeaf(func(c *x509.Certificate) {
			c.NotAfter = time.Now().Add(10 * 24 * time.Hour)
		}), true},
		{"far future", withLeaf(nil), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &svc{cert: tt.cert}
			if got := s.needsRenewal(); got != tt.want {
				t.Errorf("needsRenewal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadOrObtainLoadsExistingCert(t *testing.T) {
	dir := t.TempDir()

	cert, err := generateSelfSigned("loaded.example")
	if err != nil {
		t.Fatalf("generateSelfSigned: %v", err)
	}

	// staging so a regression that falls through to obtain() cannot reach the production CA
	s := &svc{domain: "loaded.example", certPath: dir, staging: true}

	if err := os.MkdirAll(s.certDir(), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	writeCertKeyPEM(t, s.certDir(), cert)

	// A far-future cert loads without obtain — obtain would need the network.
	if err := s.loadOrObtain(context.Background()); err != nil {
		t.Fatalf("loadOrObtain: %v", err)
	}

	got, err := s.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("GetCertificate: %v", err)
	}

	if got.Leaf == nil {
		t.Fatal("loaded cert has no leaf")
	}

	found := false

	for _, name := range got.Leaf.DNSNames {
		if name == "*.loaded.example" {
			found = true
		}
	}

	if !found {
		t.Errorf("loaded cert SANs = %v, want to contain *.loaded.example", got.Leaf.DNSNames)
	}
}

func TestAccountSelfHeal(t *testing.T) {
	tests := []struct {
		name    string
		userDoc string // account.json contents; empty means no file written
		want    bool
	}{
		{
			name:    "v4 shape re-registers",
			userDoc: `{"email":"x","registration":{"body":{"status":"valid"},"uri":"https://acme.example/acct/1"}}`,
			want:    false,
		},
		{
			name:    "v5 shape is registered",
			userDoc: `{"email":"x","registration":{"status":"valid","accountURL":"https://acme.example/acct/1"}}`,
			want:    true,
		},
		{
			name:    "key only re-registers",
			userDoc: "",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			keyFile := filepath.Join(dir, "account.key")
			userFile := filepath.Join(dir, "account.json")

			writeAccountKey(t, keyFile)

			if tt.userDoc != "" {
				if err := os.WriteFile(userFile, []byte(tt.userDoc), 0o600); err != nil {
					t.Fatalf("write account.json: %v", err)
				}
			}

			s := &svc{email: "x"}

			user, err := s.loadUser(keyFile, userFile)
			if err != nil {
				t.Fatalf("loadUser: %v", err)
			}

			if got := user.registered(); got != tt.want {
				t.Errorf("registered() = %v, want %v", got, tt.want)
			}
		})
	}
}

func writeCertKeyPEM(t *testing.T, dir string, cert *tls.Certificate) {
	t.Helper()

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]})
	if err := os.WriteFile(filepath.Join(dir, "cert.pem"), certPEM, 0o600); err != nil {
		t.Fatalf("write cert.pem: %v", err)
	}

	key, ok := cert.PrivateKey.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatalf("private key type = %T, want *ecdsa.PrivateKey", cert.PrivateKey)
	}

	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: ecPrivateKeyPEMType, Bytes: keyBytes})
	if err := os.WriteFile(filepath.Join(dir, "key.pem"), keyPEM, 0o600); err != nil {
		t.Fatalf("write key.pem: %v", err)
	}
}

func writeAccountKey(t *testing.T, keyFile string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: ecPrivateKeyPEMType, Bytes: keyBytes})
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatalf("write account.key: %v", err)
	}
}
