package acme

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"testing"
	"time"
)

func TestGenerateSelfSigned(t *testing.T) {
	cert, err := generateSelfSigned("example.com")
	if err != nil {
		t.Fatalf("generateSelfSigned: %v", err)
	}

	if cert.Leaf == nil {
		t.Fatal("Leaf not populated")
	}

	leaf := cert.Leaf

	wantNames := map[string]bool{"*.example.com": false, "example.com": false}
	for _, name := range leaf.DNSNames {
		if _, ok := wantNames[name]; ok {
			wantNames[name] = true
		}
	}

	for name, found := range wantNames {
		if !found {
			t.Errorf("missing SAN: %s", name)
		}
	}

	if leaf.Issuer.CommonName != leaf.Subject.CommonName {
		t.Errorf("not self-signed: issuer=%q subject=%q", leaf.Issuer.CommonName, leaf.Subject.CommonName)
	}

	if time.Until(leaf.NotAfter) < 9*365*24*time.Hour {
		t.Errorf("cert expires too soon: %v", leaf.NotAfter)
	}

	if leaf.PublicKeyAlgorithm != x509.ECDSA {
		t.Errorf("key algorithm = %v, want ECDSA", leaf.PublicKeyAlgorithm)
	}
}

func TestGenerateSelfSignedTLSHandshake(t *testing.T) {
	cert, err := generateSelfSigned("example.com")
	if err != nil {
		t.Fatalf("generateSelfSigned: %v", err)
	}

	serverCfg := &tls.Config{
		Certificates: []tls.Certificate{*cert},
	}

	clientCAs := x509.NewCertPool()
	clientCAs.AddCert(cert.Leaf)
	clientCfg := &tls.Config{
		RootCAs:    clientCAs,
		ServerName: "foo.example.com",
	}

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	errCh := make(chan error, 1)

	go func() {
		tlsServer := tls.Server(server, serverCfg)
		errCh <- tlsServer.Handshake()
	}()

	tlsClient := tls.Client(client, clientCfg)
	if err := tlsClient.Handshake(); err != nil {
		t.Fatalf("client handshake: %v", err)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("server handshake: %v", err)
	}
}
