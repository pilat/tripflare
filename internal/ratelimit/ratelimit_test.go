package ratelimit

import (
	"sync"
	"testing"
	"time"
)

func TestAllow(t *testing.T) {
	tests := []struct {
		name  string
		tiers []TierConfig
		calls int
		want  []bool
	}{
		{
			name: "single tier exhaustion",
			tiers: []TierConfig{
				{Capacity: 2, Window: time.Hour},
			},
			calls: 3,
			want:  []bool{true, true, false},
		},
		{
			name: "multi tier uses strictest",
			tiers: []TierConfig{
				{Capacity: 5, Window: time.Hour},
				{Capacity: 1, Window: time.Hour},
			},
			calls: 2,
			want:  []bool{true, false},
		},
		{
			name: "separate keys are independent",
			tiers: []TierConfig{
				{Capacity: 1, Window: time.Hour},
			},
			calls: 1,
			want:  []bool{true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lim := New(tt.tiers)
			for i, wantOK := range tt.want {
				got := lim.Allow("key")
				if got != wantOK {
					t.Errorf("call %d: Allow() = %v, want %v", i, got, wantOK)
				}
			}
		})
	}
}

func TestAllow_SeparateKeys(t *testing.T) {
	lim := New([]TierConfig{{Capacity: 1, Window: time.Hour}})

	if !lim.Allow("a") {
		t.Fatal("Allow(a) = false, want true")
	}

	if !lim.Allow("b") {
		t.Fatal("Allow(b) = false, want true (different key)")
	}

	if lim.Allow("a") {
		t.Fatal("Allow(a) second call = true, want false")
	}
}

func TestAllow_Refill(t *testing.T) {
	window := 50 * time.Millisecond
	lim := New([]TierConfig{{Capacity: 1, Window: window}})

	if !lim.Allow("k") {
		t.Fatal("first Allow = false")
	}

	if lim.Allow("k") {
		t.Fatal("second Allow = true before refill")
	}

	time.Sleep(window + 10*time.Millisecond)

	if !lim.Allow("k") {
		t.Fatal("Allow after refill = false")
	}
}

func TestGC(t *testing.T) {
	lim := New([]TierConfig{{Capacity: 10, Window: time.Hour}})

	lim.Allow("fresh")
	lim.Allow("stale")

	// Manually age the stale entry
	s := lim.(*svc)
	s.mu.Lock()
	s.keys["stale"].lastAccess = time.Now().Add(-2 * time.Hour)
	s.mu.Unlock()

	lim.GC(time.Hour)

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.keys["fresh"]; !ok {
		t.Error("GC removed fresh entry")
	}

	if _, ok := s.keys["stale"]; ok {
		t.Error("GC did not remove stale entry")
	}
}

func TestAllow_Concurrent(t *testing.T) {
	lim := New([]TierConfig{{Capacity: 100, Window: time.Hour}})

	var wg sync.WaitGroup

	allowed := make(chan bool, 200)

	for range 200 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			allowed <- lim.Allow("shared")
		}()
	}

	wg.Wait()
	close(allowed)

	trueCount := 0

	for v := range allowed {
		if v {
			trueCount++
		}
	}

	if trueCount != 100 {
		t.Errorf("concurrent Allow: got %d true, want 100", trueCount)
	}
}
