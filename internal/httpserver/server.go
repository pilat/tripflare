package httpserver

import (
	"context"
	"crypto/tls"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/pilat/tripflare/internal/config"
	"github.com/pilat/tripflare/internal/geoip"
	"github.com/pilat/tripflare/internal/pixel"
	"github.com/pilat/tripflare/internal/ratelimit"
	"github.com/pilat/tripflare/internal/registry"
	"github.com/pilat/tripflare/internal/store"
)

const ctxUsername contextKey = "username"

type contextKey string

type Service interface {
	ListenAndServe(ctx context.Context) error
}

type svc struct {
	domain         string
	httpAddr       string
	httpsAddr      string
	registry       registry.Service
	store          store.Service
	getCertificate func(*tls.ClientHelloInfo) (*tls.Certificate, error)
	accessLimit    ratelimit.Limiter
	auth           []config.AuthEntry
	geo            geoip.Service
}

var _ Service = (*svc)(nil)

//go:embed ui
var uiFS embed.FS

func New(
	domain, httpAddr, httpsAddr string,
	reg registry.Service,
	st store.Service,
	getCert func(*tls.ClientHelloInfo) (*tls.Certificate, error),
	accessLimit ratelimit.Limiter,
	auth []config.AuthEntry,
	geo geoip.Service,
) Service {
	return &svc{
		domain:         domain,
		httpAddr:       httpAddr,
		httpsAddr:      httpsAddr,
		registry:       reg,
		store:          st,
		getCertificate: getCert,
		accessLimit:    accessLimit,
		auth:           auth,
		geo:            geo,
	}
}

func (s *svc) ListenAndServe(ctx context.Context) error {
	httpServer := &http.Server{
		Addr:              s.httpAddr,
		Handler:           s.redirectHandler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	httpsServer := &http.Server{
		Addr:              s.httpsAddr,
		Handler:           s.mainHandler(),
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig: &tls.Config{
			MinVersion:     tls.VersionTLS12,
			GetCertificate: s.getCertificate,
		},
	}

	var wg sync.WaitGroup

	errCh := make(chan error, 2)

	wg.Add(2)

	go func() {
		defer wg.Done()

		slog.Info("http server starting", "addr", s.httpAddr)

		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http: %w", err)
		}
	}()
	go func() {
		defer wg.Done()

		slog.Info("https server starting", "addr", s.httpsAddr)

		ln, err := tls.Listen("tcp", s.httpsAddr, httpsServer.TLSConfig)
		if err != nil {
			errCh <- fmt.Errorf("https listen: %w", err)
			return
		}

		if err := httpsServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("https: %w", err)
		}
	}()

	select {
	case err := <-errCh:
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()

		_ = httpServer.Shutdown(shutdownCtx)
		_ = httpsServer.Shutdown(shutdownCtx)

		wg.Wait()

		return err
	case <-ctx.Done():
		slog.Info("http servers shutting down")

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("http shutdown error", "error", err)
		}

		if err := httpsServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("https shutdown error", "error", err)
		}

		wg.Wait()

		return nil
	}
}

func (s *svc) redirectHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slug := s.extractSlug(r.Host)
		if slug != "" {
			s.handleTripflare(w, r, slug)
			return
		}

		http.NotFound(w, r)
	})
}

func (s *svc) mainHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slug := s.extractSlug(r.Host)

		if slug == "" {
			s.handleBareDomain(w, r)
			return
		}

		s.handleTripflare(w, r, slug)
	})
}

func (s *svc) extractUsername(r *http.Request) string {
	username, password, ok := r.BasicAuth()
	if !ok {
		return ""
	}

	for _, entry := range s.auth {
		if entry.Username == username &&
			bcrypt.CompareHashAndPassword([]byte(entry.PasswordHash), []byte(password)) == nil {
			return username
		}
	}

	return ""
}

func (s *svc) requireAuth(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	username := s.extractUsername(r)
	if username == "" {
		w.Header().Set("WWW-Authenticate", `Basic realm="tripflare"`)
		writeError(w, http.StatusUnauthorized, "unauthorized")

		return r, false
	}

	ctx := context.WithValue(r.Context(), ctxUsername, username)

	return r.WithContext(ctx), true
}

func usernameFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxUsername).(string)
	return v
}

func (s *svc) handleBareDomain(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/":
		s.handleRoot(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/"):
		authed, ok := s.requireAuth(w, r)
		if !ok {
			return
		}

		s.routeAPI(w, authed)
	case r.URL.Path == "/tripflare" || strings.HasPrefix(r.URL.Path, "/tripflare/"):
		_, ok := s.requireAuth(w, r)
		if !ok {
			return
		}

		s.handleUI(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *svc) routeAPI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/slugs" {
		s.routeSlugsCollection(w, r)
		return
	}

	const prefix = "/api/slugs/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.NotFound(w, r)
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, prefix)

	slug, suffix, _ := strings.Cut(rest, "/")
	if slug == "" {
		http.NotFound(w, r)
		return
	}

	s.routeSlugResource(w, r, slug, suffix)
}

func (s *svc) routeSlugsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListSlugs(w, r)
	case http.MethodPost:
		s.handleCreateSlug(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *svc) routeSlugResource(w http.ResponseWriter, r *http.Request, slug, suffix string) {
	switch suffix {
	case "":
		if r.Method == http.MethodDelete {
			s.handleDeleteSlug(w, r, slug)
			return
		}

		s.handleGetSlug(w, r, slug)
	case "events":
		if r.Method == http.MethodDelete {
			s.handleClearEvents(w, r, slug)
			return
		}

		if _, ok := s.requireSlugOwner(w, r, slug); !ok {
			return
		}

		events := s.registry.GetEvents(slug)
		s.streamSSE(w, r, slug, events)
	default:
		http.NotFound(w, r)
	}
}

func (s *svc) handleTripflare(w http.ResponseWriter, r *http.Request, slug string) {
	if s.registry.SlugExists(slug) {
		clientIP := s.clientIP(r)

		key := clientIP + ":" + slug
		if s.accessLimit.Allow(key) {
			s.recordEvent(r, slug, clientIP)
		}
	}

	if pixel.IsPixelPath(r.URL.Path) {
		w.Header().Set("Content-Type", pixel.ContentType(r.URL.Path))
		w.Header().Set("Cache-Control", "no-store")

		if err := pixel.WritePixel(w, r.URL.Path); err != nil {
			slog.Error("failed to write pixel", "error", err)
		}

		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	if err := pixel.WriteOGPage(w, r.Host); err != nil {
		slog.Error("failed to write og page", "error", err)
	}
}

func (s *svc) recordEvent(r *http.Request, slug, clientIP string) {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}

	headers := map[string]string{
		"Accept":          r.Header.Get("Accept"),
		"Accept-Language": r.Header.Get("Accept-Language"),
		"Referer":         r.Header.Get("Referer"),
	}

	s.registry.RecordHTTP(slug, clientIP, scheme, r.Method, r.URL.Path, r.URL.RawQuery, r.UserAgent(), headers)
}

func (s *svc) extractSlug(host string) string {
	h, _, _ := strings.Cut(host, ":")
	if !strings.HasSuffix(h, "."+s.domain) {
		return ""
	}

	slug := strings.TrimSuffix(h, "."+s.domain)
	if strings.Contains(slug, ".") {
		return ""
	}

	return slug
}

func (s *svc) clientIP(r *http.Request) string {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if host == "" {
		host = r.RemoteAddr
	}

	return host
}
