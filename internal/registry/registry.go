package registry

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"sort"
	"sync"
	"time"

	"github.com/pilat/tripflare/internal/store"
)

type Slug struct {
	ID        string    `json:"slug"`
	Owner     string    `json:"owner"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Event struct {
	Type      string          `json:"type"`
	SourceIP  string          `json:"source_ip"`
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

type Service interface {
	CreateSlug(owner string) (Slug, error)
	GetSlug(slug string) (*Slug, bool)
	DeleteSlug(slug string)
	SlugExists(slug string) bool
	SlugCount() int
	EventCount(slug string) int
	ListSlugs(owner string) []Slug

	RecordDNS(slug, sourceIP, queryType, queryName, clientSubnet string) bool
	RecordHTTP(slug, sourceIP, scheme, method, path, query, userAgent string, headers map[string]string) bool
	GetEvents(slug string) []Event
	ClearEvents(slug string) bool
	Subscribe(slug string) (<-chan Event, func())

	FlushTo(ctx context.Context, st store.Service) error
	LoadFrom(ctx context.Context, st store.Service) error
	Cleanup() int
}

type subscriber struct {
	ch chan Event
}

type slugEntry struct {
	slug               Slug
	events             []Event
	subscribers        []*subscriber
	dirty              bool
	flushedCount       int
	evictedDuringFlush int
}

type svc struct {
	mu               sync.RWMutex
	slugs            map[string]*slugEntry
	maxEventsPerSlug int
	slugTTL          time.Duration
}

var _ Service = (*svc)(nil)

func New(maxEventsPerSlug int, slugTTL time.Duration) Service {
	return &svc{
		slugs:            make(map[string]*slugEntry),
		maxEventsPerSlug: maxEventsPerSlug,
		slugTTL:          slugTTL,
	}
}

func (s *svc) CreateSlug(owner string) (Slug, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id, err := generateSlugID()
	if err != nil {
		return Slug{}, fmt.Errorf("generate slug id: %w", err)
	}

	// Collision check (astronomically unlikely but cheap)
	for s.slugs[id] != nil {
		id, err = generateSlugID()
		if err != nil {
			return Slug{}, fmt.Errorf("generate slug id: %w", err)
		}
	}

	now := time.Now()
	slug := Slug{
		ID:        id,
		Owner:     owner,
		CreatedAt: now,
		ExpiresAt: now.Add(s.slugTTL),
	}

	s.slugs[id] = &slugEntry{slug: slug, dirty: true}
	slog.Info("slug created", "slug", id, "owner", owner, "expires_at", slug.ExpiresAt)

	return slug, nil
}

func (s *svc) GetSlug(slug string) (*Slug, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	e, ok := s.slugs[slug]
	if !ok {
		return nil, false
	}

	t := e.slug

	return &t, true
}

func (s *svc) DeleteSlug(slug string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if e, ok := s.slugs[slug]; ok {
		for _, sub := range e.subscribers {
			close(sub.ch)
		}
	}

	delete(s.slugs, slug)
}

func (s *svc) SlugExists(slug string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, ok := s.slugs[slug]

	return ok
}

func (s *svc) SlugCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.slugs)
}

func (s *svc) EventCount(slug string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	e, ok := s.slugs[slug]
	if !ok {
		return 0
	}

	return len(e.events)
}

func (s *svc) ListSlugs(owner string) []Slug {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []Slug

	for _, e := range s.slugs {
		if e.slug.Owner == owner {
			result = append(result, e.slug)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})

	return result
}

func (s *svc) RecordDNS(slug, sourceIP, queryType, queryName, clientSubnet string) bool {
	data := map[string]string{
		"query_type": queryType,
		"query_name": queryName,
	}
	if clientSubnet != "" {
		data["client_subnet"] = clientSubnet
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		slog.Error("failed to marshal dns data", "error", err)
		return false
	}

	return s.recordEvent(slug, "dns", sourceIP, jsonData)
}

func (s *svc) RecordHTTP(
	slug, sourceIP, scheme, method, path, query, userAgent string,
	headers map[string]string,
) bool {
	data, err := json.Marshal(map[string]any{
		"scheme":     scheme,
		"method":     method,
		"path":       path,
		"query":      query,
		"user_agent": userAgent,
		"headers":    headers,
	})
	if err != nil {
		slog.Error("failed to marshal http data", "error", err)
		return false
	}

	return s.recordEvent(slug, "http", sourceIP, data)
}

func (s *svc) GetEvents(slug string) []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()

	e, ok := s.slugs[slug]
	if !ok {
		return nil
	}

	result := make([]Event, len(e.events))
	copy(result, e.events)

	return result
}

func (s *svc) ClearEvents(slug string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.slugs[slug]
	if !ok {
		return false
	}

	e.events = e.events[:0]
	e.flushedCount = 0
	e.dirty = true

	return true
}

func (s *svc) Cleanup() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	removed := 0

	for id, e := range s.slugs {
		if e.slug.ExpiresAt.Before(now) {
			for _, sub := range e.subscribers {
				close(sub.ch)
			}

			delete(s.slugs, id)

			removed++
		}
	}

	if removed > 0 {
		slog.Info("cleanup expired slugs", "removed", removed, "remaining", len(s.slugs))
	}

	return removed
}

func (s *svc) recordEvent(slug, eventType, sourceIP string, data json.RawMessage) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.slugs[slug]
	if !ok {
		return false
	}

	if len(e.events) >= s.maxEventsPerSlug {
		if e.flushedCount > 0 {
			e.flushedCount--
		}

		e.evictedDuringFlush++
		newEvents := make([]Event, len(e.events)-1, s.maxEventsPerSlug)
		copy(newEvents, e.events[1:])
		e.events = newEvents
	}

	ev := Event{
		Type:      eventType,
		SourceIP:  sourceIP,
		Timestamp: time.Now(),
		Data:      data,
	}
	e.events = append(e.events, ev)
	e.dirty = true

	for _, sub := range e.subscribers {
		select {
		case sub.ch <- ev:
		default:
		}
	}

	return true
}

func (s *svc) Subscribe(slug string) (<-chan Event, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.slugs[slug]
	if !ok {
		ch := make(chan Event)
		close(ch)

		return ch, func() {}
	}

	sub := &subscriber{
		ch: make(chan Event, 64),
	}
	e.subscribers = append(e.subscribers, sub)

	unsub := func() {
		s.mu.Lock()
		defer s.mu.Unlock()

		e, ok := s.slugs[slug]
		if !ok {
			return
		}

		for i, ss := range e.subscribers {
			if ss == sub {
				e.subscribers = append(e.subscribers[:i], e.subscribers[i+1:]...)

				close(sub.ch)

				return
			}
		}
	}

	return sub.ch, unsub
}

func generateSlugID() (string, error) {
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}

	n := new(big.Int).SetBytes(b)

	id := n.Text(36)
	if len(id) > 8 {
		id = id[:8]
	}

	for len(id) < 8 {
		id = "0" + id
	}

	return id, nil
}
