package httpserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pilat/tripflare/internal/geoip"
	"github.com/pilat/tripflare/internal/registry"
)

const heartbeatInterval = 15 * time.Second

func (s *svc) streamSSE(w http.ResponseWriter, r *http.Request, slug string, history []registry.Event) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	ch, unsub := s.registry.Subscribe(slug)
	defer unsub()

	cache := make(map[string]geoip.Info)
	for _, ev := range history {
		writeSSEEvent(w, s.enrichEvent(ev, cache))
	}

	flusher.Flush()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				_, _ = fmt.Fprintln(w, "event: close")
				_, _ = fmt.Fprintln(w, "data: slug expired")
				_, _ = fmt.Fprintln(w)

				flusher.Flush()

				return
			}

			writeSSEEvent(w, s.enrichEvent(ev, cache))
			flusher.Flush()
		case <-ticker.C:
			_, _ = fmt.Fprintln(w, ": heartbeat")
			_, _ = fmt.Fprintln(w)

			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func writeSSEEvent(w http.ResponseWriter, ev enrichedEvent) {
	data, _ := json.Marshal(ev)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
}
