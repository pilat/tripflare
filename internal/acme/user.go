package acme

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/go-acme/lego/v4/registration"
)

type acmeUser struct {
	Email        string                 `json:"email"`
	Registration *registration.Resource `json:"registration,omitempty"`
	key          crypto.PrivateKey
}

func (u *acmeUser) GetEmail() string                        { return u.Email }
func (u *acmeUser) GetRegistration() *registration.Resource { return u.Registration }
func (u *acmeUser) GetPrivateKey() crypto.PrivateKey        { return u.key }

func (s *svc) getOrCreateUser() (*acmeUser, error) {
	dir := s.certDir()
	keyFile := filepath.Join(dir, "account.key")
	userFile := filepath.Join(dir, "account.json")

	user, err := s.loadUser(keyFile, userFile)
	if err == nil {
		return user, nil
	}

	if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("load existing account key: %w", err)
	}

	return s.createUser(keyFile)
}

func (s *svc) loadUser(keyFile, userFile string) (*acmeUser, error) {
	keyData, err := os.ReadFile(keyFile) //nolint:gosec // path from config, not user input
	if err != nil {
		return nil, fmt.Errorf("read key file %s: %w", keyFile, err)
	}

	block, _ := pem.Decode(keyData)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM from %s", keyFile)
	}

	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse account key: %w", err)
	}

	user := &acmeUser{Email: s.email, key: key}

	userData, err := os.ReadFile(userFile) //nolint:gosec // path from config, not user input
	if err != nil {
		slog.Warn("account key found but no registration file", "error", err)
		return user, nil
	}

	if err := json.Unmarshal(userData, user); err != nil {
		slog.Warn("failed to parse account registration", "error", err)
	}

	return user, nil
}

func (s *svc) createUser(keyFile string) (*acmeUser, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal key: %w", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		return nil, fmt.Errorf("write account key: %w", err)
	}

	return &acmeUser{Email: s.email, key: key}, nil
}

func (s *svc) saveUser(user *acmeUser) {
	userFile := filepath.Join(s.certDir(), "account.json")

	data, err := json.Marshal(user)
	if err != nil {
		slog.Error("failed to marshal user registration", "error", err)
		return
	}

	if err := os.WriteFile(userFile, data, 0o600); err != nil {
		slog.Error("failed to save user registration", "error", err)
	}
}
