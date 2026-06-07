package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/pilat/tripflare/internal/config"
	"github.com/pilat/tripflare/internal/geoip"
	"github.com/pilat/tripflare/internal/ratelimit"
	"github.com/pilat/tripflare/internal/registry"
	"github.com/pilat/tripflare/internal/store"
)

const (
	testUsername = "admin"
	testPassword = "test-password"
)

func testAuth(t *testing.T) []config.AuthEntry {
	t.Helper()

	h, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.MinCost)
	require.NoError(t, err, "generate bcrypt hash")

	return []config.AuthEntry{{Username: testUsername, PasswordHash: string(h)}}
}

func newTestServer(t *testing.T) *svc {
	t.Helper()
	return newTestServerWithStore(t, nil)
}

func newTestServerWithStore(t *testing.T, st store.Service) *svc {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.MinCost)
	require.NoError(t, err, "generate bcrypt hash")

	geo, err := geoip.New("")
	require.NoError(t, err, "geoip.New")

	return &svc{
		domain:      "trap.example.com",
		registry:    registry.New(500, time.Hour),
		store:       st,
		accessLimit: bigLimiter(),
		auth:        []config.AuthEntry{{Username: testUsername, PasswordHash: string(hash)}},
		geo:         geo,
	}
}

func createTestSlug(t *testing.T, handler http.Handler) string {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/slugs", nil)
	req.Host = "trap.example.com"
	req.RemoteAddr = "1.2.3.4:1234"
	req.SetBasicAuth(testUsername, testPassword)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "create slug: body: %s", w.Body.String())

	var resp slugResponse

	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err, "decode slug response")
	require.NotEmpty(t, resp.Slug, "empty slug ID in response")

	return resp.Slug
}

func bigLimiter() ratelimit.Limiter {
	return ratelimit.New([]ratelimit.TierConfig{
		{Capacity: 10000, Window: time.Second},
	})
}

func newMemoryStore(t *testing.T) store.Service {
	t.Helper()
	s, err := store.New(t.TempDir() + "/test.db")
	require.NoError(t, err, "New store")

	return s
}
