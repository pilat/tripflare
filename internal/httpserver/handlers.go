package httpserver

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/pilat/tripflare/internal/geoip"
	"github.com/pilat/tripflare/internal/registry"
)

type slugResponse struct {
	Slug      string `json:"slug"`
	Domain    string `json:"domain"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
}

type slugDetailResponse struct {
	slugResponse
	Events []json.RawMessage `json:"events"`
}

type slugListItem struct {
	slugResponse
	EventCount int `json:"event_count"`
}

type enrichedEvent struct {
	registry.Event
	CountryCode string `json:"country_code,omitempty"`
	CountryFlag string `json:"country_flag,omitempty"`
	ASN         uint   `json:"asn,omitempty"`
	Org         string `json:"org,omitempty"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func (s *svc) handleRoot(w http.ResponseWriter, r *http.Request) {
	http.NotFound(w, r)
}

func (s *svc) handleCreateSlug(w http.ResponseWriter, r *http.Request) {
	owner := usernameFromContext(r.Context())

	slug, err := s.registry.CreateSlug(owner)
	if err != nil {
		slog.Error("failed to create slug", "error", err)
		writeError(w, http.StatusServiceUnavailable, "service unavailable")

		return
	}

	resp := slugResponse{
		Slug:      slug.ID,
		Domain:    s.domain,
		CreatedAt: slug.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		ExpiresAt: slug.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *svc) handleListSlugs(w http.ResponseWriter, r *http.Request) {
	owner := usernameFromContext(r.Context())
	slugs := s.registry.ListSlugs(owner)

	items := make([]slugListItem, len(slugs))
	for i, sl := range slugs {
		items[i] = slugListItem{
			slugResponse: slugResponse{
				Slug:      sl.ID,
				Domain:    s.domain,
				CreatedAt: sl.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
				ExpiresAt: sl.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
			},
			EventCount: s.registry.EventCount(sl.ID),
		}
	}

	writeJSON(w, http.StatusOK, items)
}

func (s *svc) requireSlugOwner(w http.ResponseWriter, r *http.Request, slugID string) (*registry.Slug, bool) {
	owner := usernameFromContext(r.Context())

	slug, ok := s.registry.GetSlug(slugID)
	if !ok || slug.Owner != owner {
		writeError(w, http.StatusNotFound, "not found")
		return nil, false
	}

	return slug, true
}

func (s *svc) handleGetSlug(w http.ResponseWriter, r *http.Request, slugID string) {
	slug, ok := s.requireSlugOwner(w, r, slugID)
	if !ok {
		return
	}

	events := s.registry.GetEvents(slugID)
	cache := make(map[string]geoip.Info)

	rawEvents := make([]json.RawMessage, 0, len(events))
	for _, ev := range events {
		b, _ := json.Marshal(s.enrichEvent(ev, cache))
		rawEvents = append(rawEvents, b)
	}

	resp := slugDetailResponse{
		slugResponse: slugResponse{
			Slug:      slug.ID,
			Domain:    s.domain,
			CreatedAt: slug.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			ExpiresAt: slug.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
		},
		Events: rawEvents,
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *svc) enrichEvent(ev registry.Event, cache map[string]geoip.Info) enrichedEvent {
	info, ok := cache[ev.SourceIP]
	if !ok {
		info = s.geo.Lookup(ev.SourceIP)
		cache[ev.SourceIP] = info
	}

	return enrichedEvent{
		Event:       ev,
		CountryCode: info.CountryCode,
		CountryFlag: info.Flag,
		ASN:         info.ASN,
		Org:         info.Org,
	}
}

func (s *svc) handleDeleteSlug(w http.ResponseWriter, r *http.Request, slugID string) {
	if _, ok := s.requireSlugOwner(w, r, slugID); !ok {
		return
	}

	s.registry.DeleteSlug(slugID)

	if s.store != nil {
		if err := s.store.DeleteSlug(r.Context(), slugID); err != nil {
			slog.Error("failed to delete slug from store", "slug", slugID, "error", err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *svc) handleClearEvents(w http.ResponseWriter, r *http.Request, slugID string) {
	if _, ok := s.requireSlugOwner(w, r, slugID); !ok {
		return
	}

	s.registry.ClearEvents(slugID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

func (s *svc) handleUI(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case pathUI, pathUI + "/":
		data, err := uiFS.ReadFile("ui/index.html")
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	case pathUI + "/style.css":
		data, err := uiFS.ReadFile("ui/style.css")
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		_, _ = w.Write(data)
	default:
		http.NotFound(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}
