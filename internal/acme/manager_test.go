package acme

import (
	"context"
	"crypto/tls"
	"testing"
	"time"
)

func TestNewReturnsCertImmediately(t *testing.T) {
	cs := NewChallengeStore()

	mgr, err := New("example.com", "", "", false, false, cs)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cert, err := mgr.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("GetCertificate: %v", err)
	}

	if cert == nil {
		t.Fatal("cert is nil")
	}

	if cert.Leaf == nil {
		t.Fatal("cert.Leaf is nil")
	}
}

func TestRunDisabledReturnsImmediately(t *testing.T) {
	cs := NewChallengeStore()

	mgr, err := New("example.com", "", "", false, false, cs)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := mgr.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
}
