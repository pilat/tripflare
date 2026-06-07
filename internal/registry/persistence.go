package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/pilat/tripflare/internal/store"
)

type flushSnapshot struct {
	slug        store.SlugRow
	events      []store.EventRow
	entry       *slugEntry
	snapshotLen int
}

// FlushTo persists dirty slugs and their new events to the store.
// Snapshots data under lock, writes to SQLite outside lock, then updates bookkeeping.
func (s *svc) FlushTo(ctx context.Context, st store.Service) error {
	snapshots := s.collectDirtySnapshots()

	for _, snap := range snapshots {
		if err := st.InsertSlug(ctx, snap.slug); err != nil {
			return fmt.Errorf("flush slug %s: %w", snap.slug.ID, err)
		}

		if len(snap.events) > 0 {
			if err := st.InsertEvents(ctx, snap.events); err != nil {
				return fmt.Errorf("flush events for %s: %w", snap.slug.ID, err)
			}
		}

		s.mu.Lock()
		snap.entry.flushedCount = max(snap.snapshotLen-snap.entry.evictedDuringFlush, 0)
		snap.entry.dirty = snap.entry.flushedCount < len(snap.entry.events)
		s.mu.Unlock()
	}

	return nil
}

// LoadFrom restores slugs and events from the store on startup.
func (s *svc) LoadFrom(ctx context.Context, st store.Service) error {
	now := time.Now()

	slugs, err := st.LoadSlugs(ctx, now)
	if err != nil {
		return fmt.Errorf("load slugs: %w", err)
	}

	eventsBySlug, totalEvents, err := s.loadAndGroupEvents(ctx, st, slugs, now)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, t := range slugs {
		evts := eventsBySlug[t.ID]
		s.slugs[t.ID] = &slugEntry{
			slug: Slug{
				ID:        t.ID,
				Owner:     t.Owner,
				CreatedAt: t.CreatedAt,
				ExpiresAt: t.ExpiresAt,
			},
			events:       evts,
			flushedCount: len(evts),
		}
	}

	slog.Info("registry loaded from store", "slugs", len(slugs), "events", totalEvents)

	return nil
}

func (s *svc) collectDirtySnapshots() []flushSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	var snapshots []flushSnapshot

	for _, e := range s.slugs {
		if !e.dirty {
			continue
		}

		e.evictedDuringFlush = 0

		snap := flushSnapshot{
			slug: store.SlugRow{
				ID:        e.slug.ID,
				Owner:     e.slug.Owner,
				CreatedAt: e.slug.CreatedAt,
				ExpiresAt: e.slug.ExpiresAt,
			},
			entry:       e,
			snapshotLen: len(e.events),
		}

		// Copy unflushed events into a new slice (snapshot)
		if e.flushedCount < len(e.events) {
			newEvents := e.events[e.flushedCount:]

			rows := make([]store.EventRow, len(newEvents))
			for i, ev := range newEvents {
				rows[i] = store.EventRow{
					Slug:      e.slug.ID,
					Type:      ev.Type,
					SourceIP:  ev.SourceIP,
					Timestamp: ev.Timestamp,
					Data:      string(ev.Data),
				}
			}

			snap.events = rows
		}

		snapshots = append(snapshots, snap)
	}

	return snapshots
}

func (s *svc) loadAndGroupEvents(
	ctx context.Context, st store.Service, slugs []store.SlugRow, now time.Time,
) (map[string][]Event, int, error) {
	earliest := now
	for _, t := range slugs {
		if t.CreatedAt.Before(earliest) {
			earliest = t.CreatedAt
		}
	}

	events, err := st.LoadEvents(ctx, earliest.Add(-time.Second))
	if err != nil {
		return nil, 0, fmt.Errorf("load events: %w", err)
	}

	result := make(map[string][]Event)
	for _, e := range events {
		result[e.Slug] = append(result[e.Slug], Event{
			Type:      e.Type,
			SourceIP:  e.SourceIP,
			Timestamp: e.Timestamp,
			Data:      json.RawMessage(e.Data),
		})
	}

	return result, len(events), nil
}
