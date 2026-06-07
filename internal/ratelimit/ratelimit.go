package ratelimit

import (
	"sync"
	"time"
)

type TierConfig struct {
	Capacity int
	Window   time.Duration
}

type Limiter interface {
	Allow(key string) bool
	GC(maxAge time.Duration)
}

type bucket struct {
	tokens     float64
	lastRefill time.Time
}

type entry struct {
	buckets    []bucket
	lastAccess time.Time
}

type svc struct {
	mu    sync.Mutex
	tiers []TierConfig
	keys  map[string]*entry
}

var _ Limiter = (*svc)(nil)

func New(tiers []TierConfig) Limiter {
	return &svc{
		tiers: tiers,
		keys:  make(map[string]*entry),
	}
}

func (s *svc) Allow(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	e := s.getOrCreate(key, now)
	e.lastAccess = now

	for i := range e.buckets {
		s.refill(&e.buckets[i], i, now)
	}

	for i := range e.buckets {
		if e.buckets[i].tokens < 1 {
			return false
		}
	}

	for i := range e.buckets {
		e.buckets[i].tokens--
	}

	return true
}

func (s *svc) GC(maxAge time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	for key, e := range s.keys {
		if e.lastAccess.Before(cutoff) {
			delete(s.keys, key)
		}
	}
}

func (s *svc) getOrCreate(key string, now time.Time) *entry {
	e, ok := s.keys[key]
	if ok {
		return e
	}

	buckets := make([]bucket, len(s.tiers))
	for i, tier := range s.tiers {
		buckets[i] = bucket{
			tokens:     float64(tier.Capacity),
			lastRefill: now,
		}
	}

	e = &entry{
		buckets:    buckets,
		lastAccess: now,
	}
	s.keys[key] = e

	return e
}

func (s *svc) refill(b *bucket, tierIdx int, now time.Time) {
	tier := s.tiers[tierIdx]

	elapsed := now.Sub(b.lastRefill)
	if elapsed <= 0 {
		return
	}

	rate := float64(tier.Capacity) / float64(tier.Window)

	b.tokens += float64(elapsed) * rate
	if b.tokens > float64(tier.Capacity) {
		b.tokens = float64(tier.Capacity)
	}

	b.lastRefill = now
}
