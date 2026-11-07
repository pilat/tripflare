package httpserver

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/pilat/tripflare/internal/buildinfo"
	"github.com/pilat/tripflare/internal/config"
)

// Distinctive values so a "does not contain" assertion can't pass by accident.
var leakCheckBuild = buildinfo.Info{
	Version: "v9.8.7-leakcheck",
	Commit:  "deadbeefcafebabe0123456789abcdeffedcba98",
	Date:    "2026-11-07T21:44:34Z",
}

func TestExtractSlug(t *testing.T) {
	s := &svc{domain: "trap.example.com"}

	tests := []struct {
		name string
		host string
		want string
	}{
		{"with slug", "abc123.trap.example.com", "abc123"},
		{"with port", "abc123.trap.example.com:443", "abc123"},
		{"bare domain", "trap.example.com", ""},
		{"nested subdomain", "a.b.trap.example.com", ""},
		{"unrelated", "other.com", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.extractSlug(tt.host); got != tt.want {
				t.Errorf("extractSlug(%q) = %q, want %q", tt.host, got, tt.want)
			}
		})
	}
}

func TestBareDomainRouting(t *testing.T) {
	s := newTestServer(t)

	tests := []struct {
		name   string
		method string
		path   string
		auth   bool
		status int
	}{
		{"root not found", http.MethodGet, "/", false, http.StatusNotFound},
		{"tripflare requires auth", http.MethodGet, "/tripflare", false, http.StatusUnauthorized},
		{"tripflare with auth", http.MethodGet, "/tripflare", true, http.StatusOK},
		{"api slugs requires auth", http.MethodGet, "/api/slugs", false, http.StatusUnauthorized},
		{"api slugs with auth lists slugs", http.MethodGet, "/api/slugs", true, http.StatusOK},
		{"not found", http.MethodGet, "/unknown", false, http.StatusNotFound},
		{"terms removed", http.MethodGet, "/terms", false, http.StatusNotFound},
		{"impressum removed", http.MethodGet, "/impressum", false, http.StatusNotFound},
		{"privacy removed", http.MethodGet, "/privacy", false, http.StatusNotFound},
		{"old ui path removed", http.MethodGet, "/ui", false, http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Host = "trap.example.com"

			req.RemoteAddr = "1.2.3.4:1234"
			if tt.auth {
				req.SetBasicAuth(testUsername, testPassword)
			}

			w := httptest.NewRecorder()
			s.mainHandler().ServeHTTP(w, req)

			if w.Code != tt.status {
				t.Errorf("status = %d, want %d", w.Code, tt.status)
			}
		})
	}
}

func TestAuthReturns401WithWWWAuthenticate(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/slugs", nil)
	req.Host = "trap.example.com"
	w := httptest.NewRecorder()
	s.mainHandler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}

	if got := w.Header().Get("WWW-Authenticate"); got != `Basic realm="tripflare"` {
		t.Errorf("WWW-Authenticate = %q", got)
	}
}

func TestAuthWrongPassword(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/slugs", nil)
	req.Host = "trap.example.com"
	req.RemoteAddr = "1.2.3.4:1234"
	req.SetBasicAuth(testUsername, "wrong-password")

	w := httptest.NewRecorder()
	s.mainHandler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestAuthWrongUsername(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/slugs", nil)
	req.Host = "trap.example.com"
	req.RemoteAddr = "1.2.3.4:1234"
	req.SetBasicAuth("wrong-user", testPassword)

	w := httptest.NewRecorder()
	s.mainHandler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestAuthMultipleUsers(t *testing.T) {
	h1, _ := bcrypt.GenerateFromPassword([]byte("pass-alice"), bcrypt.MinCost)
	h2, _ := bcrypt.GenerateFromPassword([]byte("pass-bob"), bcrypt.MinCost)
	s := newTestServer(t)
	s.auth = []config.AuthEntry{
		{Username: "alice", PasswordHash: string(h1)},
		{Username: "bob", PasswordHash: string(h2)},
	}
	handler := s.mainHandler()

	// Alice with correct password
	req := httptest.NewRequest(http.MethodPost, "/api/slugs", nil)
	req.Host = "trap.example.com"
	req.RemoteAddr = "1.2.3.4:1234"
	req.SetBasicAuth("alice", "pass-alice")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("alice: status = %d, want 201", w.Code)
	}

	// Bob with correct password
	req = httptest.NewRequest(http.MethodPost, "/api/slugs", nil)
	req.Host = "trap.example.com"
	req.RemoteAddr = "1.2.3.4:1234"
	req.SetBasicAuth("bob", "pass-bob")

	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("bob: status = %d, want 201", w.Code)
	}

	// Alice with Bob's password
	req = httptest.NewRequest(http.MethodPost, "/api/slugs", nil)
	req.Host = "trap.example.com"
	req.RemoteAddr = "1.2.3.4:1234"
	req.SetBasicAuth("alice", "pass-bob")

	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("alice with bob's password: status = %d, want 401", w.Code)
	}
}

func TestCreateSlugAPI(t *testing.T) {
	s := newTestServer(t)
	handler := s.mainHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/slugs", nil)
	req.Host = "trap.example.com"
	req.RemoteAddr = "1.2.3.4:1234"
	req.SetBasicAuth(testUsername, testPassword)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body: %s", w.Code, w.Body.String())
	}

	var resp slugResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Slug == "" {
		t.Error("empty slug in response")
	}

	if resp.Domain != "trap.example.com" {
		t.Errorf("domain = %q, want trap.example.com", resp.Domain)
	}

	if resp.CreatedAt == "" || resp.ExpiresAt == "" {
		t.Error("missing timestamps")
	}

	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("content-type = %q", ct)
	}
}

func TestGetSlugAPIEmpty(t *testing.T) {
	s := newTestServer(t)
	handler := s.mainHandler()

	tok, err := s.registry.CreateSlug(testUsername)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/slugs/"+tok.ID, nil)
	req.Host = "trap.example.com"
	req.SetBasicAuth(testUsername, testPassword)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	var resp slugDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Events) != 0 {
		t.Errorf("events = %d, want 0", len(resp.Events))
	}
}

func TestGetSlugAPINotFound(t *testing.T) {
	s := newTestServer(t)
	handler := s.mainHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/slugs/nonexistent", nil)
	req.Host = "trap.example.com"
	req.SetBasicAuth(testUsername, testPassword)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}

	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("content-type = %q", ct)
	}
}

func TestSlugSSEStream(t *testing.T) {
	s := newTestServer(t)
	handler := s.mainHandler()

	tok, err := s.registry.CreateSlug(testUsername)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/slugs/"+tok.ID+"/events", nil)
	req.Host = "trap.example.com"
	req.SetBasicAuth(testUsername, testPassword)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("content-type = %q, want text/event-stream", ct)
	}
}

func TestSSENotFoundForUnknownSlug(t *testing.T) {
	s := newTestServer(t)
	handler := s.mainHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/slugs/nonexistent/events", nil)
	req.Host = "trap.example.com"
	req.SetBasicAuth(testUsername, testPassword)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestTripflareUIServed(t *testing.T) {
	s := newTestServer(t)
	handler := s.mainHandler()

	req := httptest.NewRequest(http.MethodGet, "/tripflare", nil)
	req.Host = "trap.example.com"
	req.SetBasicAuth(testUsername, testPassword)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("content-type = %q, want text/html", ct)
	}

	if !strings.Contains(w.Body.String(), "Tripflare") {
		t.Error("UI page missing expected content")
	}
}

func TestTripflareStyleCSS(t *testing.T) {
	s := newTestServer(t)
	handler := s.mainHandler()

	req := httptest.NewRequest(http.MethodGet, "/tripflare/style.css", nil)
	req.Host = "trap.example.com"
	req.SetBasicAuth(testUsername, testPassword)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/css") {
		t.Errorf("content-type = %q, want text/css", ct)
	}
}

func TestTripflareRecordsForRegisteredSlug(t *testing.T) {
	s := newTestServer(t)
	handler := s.mainHandler()

	tok, err := s.registry.CreateSlug(testUsername)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/pixel.png", nil)
	req.Host = tok.ID + ".trap.example.com"
	req.RemoteAddr = "5.5.5.5:9999"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	if w.Header().Get("Content-Type") != "image/png" {
		t.Errorf("content-type = %q", w.Header().Get("Content-Type"))
	}

	events := s.registry.GetEvents(tok.ID)
	if len(events) != 1 {
		t.Errorf("events = %d, want 1", len(events))
	}
}

func TestAPISubpathsRequireAuth(t *testing.T) {
	s := newTestServer(t)
	handler := s.mainHandler()

	tok, err := s.registry.CreateSlug(testUsername)
	if err != nil {
		t.Fatal(err)
	}

	paths := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/slugs/" + tok.ID},
		{http.MethodGet, "/api/slugs/" + tok.ID + "/events"},
		{http.MethodDelete, "/api/slugs/" + tok.ID + "/events"},
	}

	for _, tt := range paths {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Host = "trap.example.com"
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", w.Code)
			}
		})
	}
}

func TestClearEventsAPI(t *testing.T) {
	s := newTestServer(t)
	handler := s.mainHandler()

	tok, err := s.registry.CreateSlug(testUsername)
	if err != nil {
		t.Fatal(err)
	}

	s.registry.RecordDNS(tok.ID, "5.5.5.5", "A", "test.com", "")

	req := httptest.NewRequest(http.MethodDelete, "/api/slugs/"+tok.ID+"/events", nil)
	req.Host = "trap.example.com"
	req.SetBasicAuth(testUsername, testPassword)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	events := s.registry.GetEvents(tok.ID)
	if len(events) != 0 {
		t.Errorf("events = %d, want 0 after clear", len(events))
	}
}

func TestDeleteSlugAPI(t *testing.T) {
	s := newTestServerWithStore(t, newMemoryStore(t))
	handler := s.mainHandler()

	slugID := createTestSlug(t, handler)

	// Delete the slug
	req := httptest.NewRequest(http.MethodDelete, "/api/slugs/"+slugID, nil)
	req.Host = "trap.example.com"
	req.SetBasicAuth(testUsername, testPassword)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}

	// Subsequent GET should return 404
	req = httptest.NewRequest(http.MethodGet, "/api/slugs/"+slugID, nil)
	req.Host = "trap.example.com"
	req.SetBasicAuth(testUsername, testPassword)

	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("after delete: status = %d, want 404", w.Code)
	}
}

func TestDeleteSlugOtherUserReturns404(t *testing.T) {
	s := newTestServer(t)
	handler := s.mainHandler()

	// Create slug owned by someone else
	tok, err := s.registry.CreateSlug("otheruser")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/slugs/"+tok.ID, nil)
	req.Host = "trap.example.com"
	req.SetBasicAuth(testUsername, testPassword)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}

	// Slug should still exist
	if !s.registry.SlugExists(tok.ID) {
		t.Error("slug should still exist after failed delete")
	}
}

func TestOwnershipCheckReturns404ForOtherUser(t *testing.T) {
	s := newTestServer(t)
	handler := s.mainHandler()

	// Create slug owned by "otheruser" (not testUsername)
	tok, err := s.registry.CreateSlug("otheruser")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"GET slug", http.MethodGet, "/api/slugs/" + tok.ID},
		{"DELETE events", http.MethodDelete, "/api/slugs/" + tok.ID + "/events"},
		{"SSE events", http.MethodGet, "/api/slugs/" + tok.ID + "/events"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Host = "trap.example.com"
			req.SetBasicAuth(testUsername, testPassword)

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404", w.Code)
			}
		})
	}
}

func TestTripflareDoesNotRequireAuth(t *testing.T) {
	s := newTestServer(t)
	handler := s.mainHandler()

	tok, err := s.registry.CreateSlug(testUsername)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/pixel.png", nil)
	req.Host = tok.ID + ".trap.example.com"
	req.RemoteAddr = "5.5.5.5:9999"
	// No auth header — tracker endpoints must work without auth
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestVersionAPI(t *testing.T) {
	tests := []struct {
		name  string
		build buildinfo.Info
		want  map[string]string
	}{
		{
			name:  "full identity",
			build: leakCheckBuild,
			want: map[string]string{
				"version": leakCheckBuild.Version,
				"commit":  leakCheckBuild.Commit,
				"date":    leakCheckBuild.Date,
			},
		},
		{
			name:  "commit and date omitted when empty",
			build: buildinfo.Info{Version: "dev"},
			want:  map[string]string{"version": "dev"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t)
			s.build = tt.build

			req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
			req.Host = "trap.example.com"
			req.SetBasicAuth(testUsername, testPassword)

			w := httptest.NewRecorder()
			s.mainHandler().ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
			}

			if ct := w.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
				t.Errorf("content-type = %q", ct)
			}

			var body map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v", err)
			}

			if !maps.Equal(body, tt.want) {
				t.Errorf("body = %v, want %v", body, tt.want)
			}
		})
	}
}

func TestVersionAPIMethodNotAllowed(t *testing.T) {
	s := newTestServer(t)
	s.build = leakCheckBuild

	req := httptest.NewRequest(http.MethodPost, "/api/version", nil)
	req.Host = "trap.example.com"
	req.SetBasicAuth(testUsername, testPassword)

	w := httptest.NewRecorder()
	s.mainHandler().ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestVersionAPIUnauthenticatedLeaksNothing(t *testing.T) {
	s := newTestServer(t)
	s.build = leakCheckBuild

	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	req.Host = "trap.example.com"

	w := httptest.NewRecorder()
	s.mainHandler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}

	if w.Header().Get("WWW-Authenticate") == "" {
		t.Error("missing WWW-Authenticate")
	}

	assertNoBuildIdentity(t, w)
}

func TestBareDomainNotFoundLeaksNothing(t *testing.T) {
	s := newTestServer(t)
	s.build = leakCheckBuild

	tests := []struct {
		name    string
		handler http.Handler
		path    string
	}{
		{":443 root", s.mainHandler(), "/"},
		{":80 api version", s.redirectHandler(), "/api/version"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			req.Host = "trap.example.com"
			req.SetBasicAuth(testUsername, testPassword)

			w := httptest.NewRecorder()
			tt.handler.ServeHTTP(w, req)

			if w.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404", w.Code)
			}

			assertNoBuildIdentity(t, w)
		})
	}
}

func TestVersionPathOnSlugSubdomainLeaksNothing(t *testing.T) {
	s := newTestServer(t)
	s.build = leakCheckBuild

	tok, err := s.registry.CreateSlug(testUsername)
	if err != nil {
		t.Fatal(err)
	}

	handlers := []struct {
		name    string
		handler http.Handler
	}{
		{":443", s.mainHandler()},
		{":80", s.redirectHandler()},
	}
	slugs := []struct {
		name string
		slug string
	}{
		{"existing slug", tok.ID},
		{"unknown slug", "nosuchslug"},
	}

	for _, h := range handlers {
		for _, sl := range slugs {
			t.Run(h.name+" "+sl.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
				req.Host = sl.slug + ".trap.example.com"
				req.RemoteAddr = "5.5.5.5:9999"
				req.SetBasicAuth(testUsername, testPassword)

				w := httptest.NewRecorder()
				h.handler.ServeHTTP(w, req)

				if w.Code != http.StatusOK {
					t.Errorf("status = %d, want 200", w.Code)
				}

				if !strings.Contains(w.Body.String(), "og:title") {
					t.Errorf("not served by the tracking handler, body: %s", w.Body.String())
				}

				assertNoBuildIdentity(t, w)
			})
		}
	}
}

func assertNoBuildIdentity(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()

	for _, needle := range []string{leakCheckBuild.Version, leakCheckBuild.Commit[:7], leakCheckBuild.Date} {
		if strings.Contains(w.Body.String(), needle) {
			t.Errorf("body contains %q", needle)
		}

		for name, values := range w.Header() {
			for _, v := range values {
				if strings.Contains(v, needle) {
					t.Errorf("header %s contains %q", name, needle)
				}
			}
		}
	}
}
