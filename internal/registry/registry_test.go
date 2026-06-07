package registry

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/pilat/tripflare/internal/store"
)

func TestCreateSlug(t *testing.T) {
	r := New(100, time.Hour)

	tok, err := r.CreateSlug("testuser")
	if err != nil {
		t.Fatalf("CreateSlug: %v", err)
	}

	if len(tok.ID) != 8 {
		t.Errorf("slug ID length = %d, want 8", len(tok.ID))
	}

	for _, c := range tok.ID {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			t.Errorf("slug ID contains invalid char %q", c)
		}
	}

	if tok.ExpiresAt.Before(tok.CreatedAt) {
		t.Error("expires_at before created_at")
	}

	if r.SlugCount() != 1 {
		t.Errorf("slug count = %d, want 1", r.SlugCount())
	}
}

func TestSlugExists(t *testing.T) {
	r := New(100, time.Hour)

	tok, err := r.CreateSlug("testuser")
	require.NoError(t, err)

	if !r.SlugExists(tok.ID) {
		t.Error("slug should exist")
	}

	if r.SlugExists("nonexistent") {
		t.Error("nonexistent slug should not exist")
	}
}

func TestGetSlug(t *testing.T) {
	r := New(100, time.Hour)

	tok, err := r.CreateSlug("testuser")
	require.NoError(t, err)

	got, ok := r.GetSlug(tok.ID)
	if !ok {
		t.Fatal("slug not found")
	}

	if got.ID != tok.ID {
		t.Errorf("got ID = %q, want %q", got.ID, tok.ID)
	}

	_, ok = r.GetSlug("missing")
	if ok {
		t.Error("should not find missing slug")
	}
}

func TestDeleteSlug(t *testing.T) {
	r := New(100, time.Hour)

	tok, err := r.CreateSlug("testuser")
	require.NoError(t, err)
	r.DeleteSlug(tok.ID)

	if r.SlugExists(tok.ID) {
		t.Error("slug should be deleted")
	}

	if r.SlugCount() != 0 {
		t.Errorf("count = %d, want 0", r.SlugCount())
	}
}

func TestRecordDNS(t *testing.T) {
	r := New(100, time.Hour)

	tok, err := r.CreateSlug("testuser")
	require.NoError(t, err)

	if ok := r.RecordDNS(tok.ID, "5.5.5.5", "A", "test.example.com", ""); !ok {
		t.Error("RecordDNS should return true")
	}

	events := r.GetEvents(tok.ID)
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}

	if events[0].Type != "dns" {
		t.Errorf("type = %q, want dns", events[0].Type)
	}

	// Test with client subnet
	r.RecordDNS(tok.ID, "5.5.5.5", "A", "test2.example.com", "203.0.113.0/24")

	events = r.GetEvents(tok.ID)
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
}

func TestRecordHTTP(t *testing.T) {
	r := New(100, time.Hour)

	tok, err := r.CreateSlug("testuser")
	require.NoError(t, err)

	ok := r.RecordHTTP(tok.ID, "5.5.5.5", "https", "GET", "/test", "q=1", "curl", map[string]string{"Accept": "*/*"})
	if !ok {
		t.Error("RecordHTTP should return true")
	}

	events := r.GetEvents(tok.ID)
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}

	if events[0].Type != "http" {
		t.Errorf("type = %q, want http", events[0].Type)
	}
}

func TestRecordUnregisteredSlug(t *testing.T) {
	r := New(100, time.Hour)

	if r.RecordDNS("nonexistent", "1.1.1.1", "A", "test.com", "") {
		t.Error("should return false for unregistered slug")
	}
}

func TestEventCapEvictsOldest(t *testing.T) {
	r := New(3, time.Hour)

	tok, err := r.CreateSlug("testuser")
	require.NoError(t, err)

	for i := range 5 {
		ip := fmt.Sprintf("10.0.0.%d", i)
		if !r.RecordDNS(tok.ID, ip, "A", "test.com", "") {
			t.Fatalf("event %d should succeed", i)
		}
	}

	events := r.GetEvents(tok.ID)
	if len(events) != 3 {
		t.Fatalf("events = %d, want 3", len(events))
	}
	// Should keep the last 3 (10.0.0.2, 10.0.0.3, 10.0.0.4)
	for i, want := range []string{"10.0.0.2", "10.0.0.3", "10.0.0.4"} {
		if events[i].SourceIP != want {
			t.Errorf("events[%d].source_ip = %q, want %q", i, events[i].SourceIP, want)
		}
	}
}

func TestCleanup(t *testing.T) {
	r := New(100, 1*time.Millisecond)

	if _, err := r.CreateSlug("testuser"); err != nil {
		t.Fatal(err)
	}

	if _, err := r.CreateSlug("testuser"); err != nil {
		t.Fatal(err)
	}

	time.Sleep(5 * time.Millisecond)

	removed := r.Cleanup()
	if removed != 2 {
		t.Errorf("removed = %d, want 2", removed)
	}

	if r.SlugCount() != 0 {
		t.Errorf("remaining = %d, want 0", r.SlugCount())
	}
}

func TestFlushAndLoad(t *testing.T) {
	st := newMemStore(t)
	defer st.Close()

	r := New(100, time.Hour)
	tok, err := r.CreateSlug("testuser")
	require.NoError(t, err)
	r.RecordDNS(tok.ID, "5.5.5.5", "A", "test.com", "")
	r.RecordHTTP(tok.ID, "6.6.6.6", "https", "GET", "/", "", "curl", nil)

	ctx := context.Background()
	if err := r.FlushTo(ctx, st); err != nil {
		t.Fatalf("FlushTo: %v", err)
	}

	// Load into new registry
	r2 := New(100, time.Hour)
	if err := r2.LoadFrom(ctx, st); err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}

	if r2.SlugCount() != 1 {
		t.Fatalf("loaded slugs = %d, want 1", r2.SlugCount())
	}

	if !r2.SlugExists(tok.ID) {
		t.Error("loaded slug should exist")
	}

	events := r2.GetEvents(tok.ID)
	if len(events) != 2 {
		t.Errorf("loaded events = %d, want 2", len(events))
	}
}

func TestFlushIdempotent(t *testing.T) {
	st := newMemStore(t)
	defer st.Close()

	r := New(100, time.Hour)
	tok, err := r.CreateSlug("testuser")
	require.NoError(t, err)
	r.RecordDNS(tok.ID, "5.5.5.5", "A", "test.com", "")

	ctx := context.Background()
	if err := r.FlushTo(ctx, st); err != nil {
		t.Fatalf("first flush: %v", err)
	}

	// Add more events, flush again
	r.RecordDNS(tok.ID, "6.6.6.6", "AAAA", "test.com", "")

	if err := r.FlushTo(ctx, st); err != nil {
		t.Fatalf("second flush: %v", err)
	}

	// Load and verify no duplicates
	events, err := st.LoadEvents(ctx, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}

	if len(events) != 2 {
		t.Errorf("total events = %d, want 2", len(events))
	}
}

func TestConcurrentRecording(t *testing.T) {
	r := New(1000, time.Hour)
	tok, err := r.CreateSlug("testuser")
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)

		go func(n int) {
			defer wg.Done()

			r.RecordDNS(tok.ID, "1.1.1.1", "A", "test.com", "")
		}(i)
	}

	wg.Wait()

	events := r.GetEvents(tok.ID)
	if len(events) != 100 {
		t.Errorf("events = %d, want 100", len(events))
	}
}

func TestSubscribeReceivesEvents(t *testing.T) {
	r := New(100, time.Hour)
	tok, err := r.CreateSlug("testuser")
	require.NoError(t, err)

	ch, unsub := r.Subscribe(tok.ID)
	defer unsub()

	r.RecordDNS(tok.ID, "8.8.8.8", "A", tok.ID+".example.com", "")

	select {
	case ev := <-ch:
		if ev.Type != "dns" {
			t.Errorf("type = %q, want dns", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestUnsubscribeStopsEvents(t *testing.T) {
	r := New(100, time.Hour)
	tok, err := r.CreateSlug("testuser")
	require.NoError(t, err)

	ch, unsub := r.Subscribe(tok.ID)
	unsub()

	r.RecordDNS(tok.ID, "8.8.8.8", "A", tok.ID+".example.com", "")

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("should not receive events after unsubscribe")
		}
	case <-time.After(50 * time.Millisecond):
		// channel closed, drain returned false — good
	}
}

func TestSubscribeClosedOnCleanup(t *testing.T) {
	r := New(100, 1*time.Millisecond)
	tok, err := r.CreateSlug("testuser")
	require.NoError(t, err)

	ch, _ := r.Subscribe(tok.ID)

	time.Sleep(5 * time.Millisecond)
	r.Cleanup()

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("channel should be closed after cleanup")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out — channel not closed")
	}
}

func TestSubscribeNonexistentSlug(t *testing.T) {
	r := New(100, time.Hour)

	ch, unsub := r.Subscribe("nonexistent")
	defer unsub()

	_, ok := <-ch
	if ok {
		t.Error("channel for nonexistent slug should be closed")
	}
}

func TestListSlugs(t *testing.T) {
	r := New(100, time.Hour)

	if _, err := r.CreateSlug("alice"); err != nil {
		t.Fatal(err)
	}

	time.Sleep(time.Millisecond)

	if _, err := r.CreateSlug("alice"); err != nil {
		t.Fatal(err)
	}

	if _, err := r.CreateSlug("bob"); err != nil {
		t.Fatal(err)
	}

	aliceSlugs := r.ListSlugs("alice")
	if len(aliceSlugs) != 2 {
		t.Fatalf("alice slugs = %d, want 2", len(aliceSlugs))
	}
	// Should be sorted by CreatedAt desc
	if aliceSlugs[0].CreatedAt.Before(aliceSlugs[1].CreatedAt) {
		t.Error("alice slugs not sorted by created_at desc")
	}

	bobSlugs := r.ListSlugs("bob")
	if len(bobSlugs) != 1 {
		t.Fatalf("bob slugs = %d, want 1", len(bobSlugs))
	}

	nobodySlugs := r.ListSlugs("nobody")
	if len(nobodySlugs) != 0 {
		t.Errorf("nobody slugs = %d, want 0", len(nobodySlugs))
	}
}

func newMemStore(t *testing.T) store.Service {
	t.Helper()
	dbPath := t.TempDir() + "/test.db"

	s, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("New store: %v", err)
	}

	return s
}
