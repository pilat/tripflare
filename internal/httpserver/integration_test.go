package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/pilat/tripflare/internal/geoip"
	"github.com/pilat/tripflare/internal/registry"
)

func TestIntegration_SlugLifecycle(t *testing.T) {
	s := newTestServer(t)
	handler := s.mainHandler()

	slugID := createTestSlug(t, handler)

	// Simulate tripflare request (no auth needed)
	req := httptest.NewRequest(http.MethodGet, "/pixel.png", nil)
	req.Host = slugID + ".trap.example.com"
	req.RemoteAddr = "10.0.0.1:5555"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("tripflare status = %d", w.Code)
	}

	// Check events via JSON API (auth required)
	req = httptest.NewRequest(http.MethodGet, "/api/slugs/"+slugID, nil)
	req.Host = "trap.example.com"
	req.SetBasicAuth(testUsername, testPassword)

	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("get slug status = %d", w.Code)
	}

	var resp slugDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Events) != 1 {
		t.Errorf("events = %d, want 1", len(resp.Events))
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("content-type = %q, want application/json", ct)
	}
}

func TestIntegration_EventCapEnforced(t *testing.T) {
	geo, err := geoip.New("")
	require.NoError(t, err, "geoip.New")

	s := &svc{
		domain:      "trap.example.com",
		registry:    registry.New(3, time.Hour), // only 3 events allowed
		accessLimit: bigLimiter(),
		auth:        testAuth(t),
		geo:         geo,
	}
	handler := s.mainHandler()
	slugID := createTestSlug(t, handler)

	for i := range 5 {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/pixel.png?seq=%d", i), nil)
		req.Host = slugID + ".trap.example.com"
		req.RemoteAddr = fmt.Sprintf("10.0.0.%d:5555", i)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	events := s.registry.GetEvents(slugID)
	if len(events) != 3 {
		t.Fatalf("events = %d, want 3 (ring buffer cap)", len(events))
	}
	// Should keep the last 3 events (from 10.0.0.2, 10.0.0.3, 10.0.0.4)
	for i, want := range []string{"10.0.0.2", "10.0.0.3", "10.0.0.4"} {
		if events[i].SourceIP != want {
			t.Errorf("events[%d].source_ip = %q, want %q", i, events[i].SourceIP, want)
		}
	}
}

func TestIntegration_UnregisteredSlugNoRecording(t *testing.T) {
	s := newTestServer(t)
	handler := s.mainHandler()

	req := httptest.NewRequest(http.MethodGet, "/pixel.png", nil)
	req.Host = "fake-slug.trap.example.com"
	req.RemoteAddr = "10.0.0.1:5555"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	if w.Header().Get("Content-Type") != "image/png" {
		t.Errorf("content-type = %q", w.Header().Get("Content-Type"))
	}
}

func TestIntegration_FlushAndRestore(t *testing.T) {
	st := newMemoryStore(t)
	defer st.Close()

	reg := registry.New(500, time.Hour)

	tok, err := reg.CreateSlug("testuser")
	if err != nil {
		t.Fatal(err)
	}

	reg.RecordHTTP(tok.ID, "5.5.5.5", "https", "GET", "/test", "", "curl", nil)
	reg.RecordDNS(tok.ID, "8.8.8.8", "A", tok.ID+".example.com", "")

	ctx := context.Background()
	if err := reg.FlushTo(ctx, st); err != nil {
		t.Fatalf("FlushTo: %v", err)
	}

	reg2 := registry.New(500, time.Hour)
	if err := reg2.LoadFrom(ctx, st); err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}

	if !reg2.SlugExists(tok.ID) {
		t.Error("slug should exist after restore")
	}

	events := reg2.GetEvents(tok.ID)
	if len(events) != 2 {
		t.Errorf("events after restore = %d, want 2", len(events))
	}
}

func TestIntegration_ExpiredSlugCleanup(t *testing.T) {
	reg := registry.New(500, 1*time.Millisecond)
	tok, err := reg.CreateSlug("testuser")
	require.NoError(t, err, "CreateSlug")
	reg.RecordDNS(tok.ID, "5.5.5.5", "A", "test.com", "")

	time.Sleep(5 * time.Millisecond)

	removed := reg.Cleanup()
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}

	if reg.SlugExists(tok.ID) {
		t.Error("expired slug should be cleaned up")
	}
}
