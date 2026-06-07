package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestInsertAndLoadSlugs(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	slugs := []SlugRow{
		{ID: "tok-a", Owner: "1.1.1.1", CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
		{ID: "tok-b", Owner: "2.2.2.2", CreatedAt: now, ExpiresAt: now.Add(2 * time.Hour)},
		{ID: "tok-expired", Owner: "3.3.3.3", CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour)},
	}

	for _, tok := range slugs {
		if err := s.InsertSlug(ctx, tok); err != nil {
			t.Fatalf("InsertSlug(%s): %v", tok.ID, err)
		}
	}

	got, err := s.LoadSlugs(ctx, now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("LoadSlugs: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d slugs, want 2", len(got))
	}

	ids := map[string]bool{}
	for _, tok := range got {
		ids[tok.ID] = true
	}

	if !ids["tok-a"] || !ids["tok-b"] {
		t.Errorf("expected tok-a and tok-b, got %v", ids)
	}

	if ids["tok-expired"] {
		t.Error("expired slug should not be loaded")
	}
}

func TestInsertAndLoadEvents(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	events := []EventRow{
		{Slug: "t1", Type: "dns", SourceIP: "1.1.1.1", Timestamp: now, Data: `{"q":"A"}`},
		{Slug: "t1", Type: "http", SourceIP: "2.2.2.2", Timestamp: now.Add(time.Second), Data: `{"m":"GET"}`},
		{Slug: "t2", Type: "dns", SourceIP: "3.3.3.3", Timestamp: now.Add(2 * time.Second), Data: `{"q":"AAAA"}`},
	}

	if err := s.InsertEvents(ctx, events); err != nil {
		t.Fatalf("InsertEvents: %v", err)
	}

	got, err := s.LoadEvents(ctx, now.Add(-time.Second))
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("got %d events, want 3", len(got))
	}

	if got[0].Slug != "t1" || got[0].Type != "dns" {
		t.Errorf("event[0] = %+v, want slug=t1 type=dns", got[0])
	}

	if got[1].Type != "http" {
		t.Errorf("event[1].Type = %q, want http", got[1].Type)
	}
}

func TestInsertEventsEmpty(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()

	if err := s.InsertEvents(context.Background(), nil); err != nil {
		t.Fatalf("InsertEvents(nil): %v", err)
	}
}

func TestDeleteExpired(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	alive := SlugRow{ID: "alive", Owner: "1.1.1.1", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	expired := SlugRow{
		ID:        "expired",
		Owner:     "2.2.2.2",
		CreatedAt: now.Add(-2 * time.Hour),
		ExpiresAt: now.Add(-time.Hour),
	}

	for _, tok := range []SlugRow{alive, expired} {
		if err := s.InsertSlug(ctx, tok); err != nil {
			t.Fatalf("InsertSlug(%s): %v", tok.ID, err)
		}
	}

	events := []EventRow{
		{Slug: "alive", Type: "dns", SourceIP: "1.1.1.1", Timestamp: now, Data: "{}"},
		{Slug: "expired", Type: "dns", SourceIP: "2.2.2.2", Timestamp: now.Add(-90 * time.Minute), Data: "{}"},
		{Slug: "expired", Type: "http", SourceIP: "3.3.3.3", Timestamp: now.Add(-80 * time.Minute), Data: "{}"},
	}
	if err := s.InsertEvents(ctx, events); err != nil {
		t.Fatalf("InsertEvents: %v", err)
	}

	n, err := s.DeleteExpired(ctx, now)
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}

	if n != 1 {
		t.Errorf("deleted %d slugs, want 1", n)
	}

	slugs, err := s.LoadSlugs(ctx, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("LoadSlugs: %v", err)
	}

	if len(slugs) != 1 || slugs[0].ID != "alive" {
		t.Errorf("remaining slugs = %v, want [alive]", slugs)
	}

	allEvents, err := s.LoadEvents(ctx, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("LoadEvents: %v", err)
	}

	if len(allEvents) != 1 {
		t.Errorf("remaining events = %d, want 1", len(allEvents))
	}

	if allEvents[0].Slug != "alive" {
		t.Errorf("remaining event slug = %q, want alive", allEvents[0].Slug)
	}
}

func TestDeleteSlug(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	if err := s.InsertSlug(
		ctx,
		SlugRow{ID: "del-me", Owner: "1.1.1.1", CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
	); err != nil {
		t.Fatalf("InsertSlug: %v", err)
	}

	if err := s.DeleteSlug(ctx, "del-me"); err != nil {
		t.Fatalf("DeleteSlug: %v", err)
	}

	slugs, err := s.LoadSlugs(ctx, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("LoadSlugs: %v", err)
	}

	if len(slugs) != 0 {
		t.Errorf("got %d slugs after delete, want 0", len(slugs))
	}
}

func TestInsertSlugReplace(t *testing.T) {
	s := newTestStore(t)
	defer s.Close()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	tok := SlugRow{ID: "replace-me", Owner: "1.1.1.1", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := s.InsertSlug(ctx, tok); err != nil {
		t.Fatalf("InsertSlug: %v", err)
	}

	tok.Owner = "9.9.9.9"
	if err := s.InsertSlug(ctx, tok); err != nil {
		t.Fatalf("InsertSlug (replace): %v", err)
	}

	slugs, err := s.LoadSlugs(ctx, now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("LoadSlugs: %v", err)
	}

	if len(slugs) != 1 {
		t.Fatalf("got %d slugs, want 1", len(slugs))
	}

	if slugs[0].Owner != "9.9.9.9" {
		t.Errorf("owner = %q, want 9.9.9.9", slugs[0].Owner)
	}
}

func TestMigrationIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := migrate(db); err != nil {
		t.Fatalf("first migrate: %v", err)
	}

	if err := migrate(db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func newTestStore(t *testing.T) Service {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return s
}
