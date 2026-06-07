package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	legoLog "github.com/go-acme/lego/v4/log"

	"github.com/pilat/tripflare/internal/acme"
	"github.com/pilat/tripflare/internal/config"
	"github.com/pilat/tripflare/internal/dns"
	"github.com/pilat/tripflare/internal/geoip"
	"github.com/pilat/tripflare/internal/httpserver"
	"github.com/pilat/tripflare/internal/logging"
	"github.com/pilat/tripflare/internal/ratelimit"
	"github.com/pilat/tripflare/internal/registry"
	"github.com/pilat/tripflare/internal/store"
)

// version, commit, and date are set at build time via -ldflags (see .goreleaser.yml).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var _ legoLog.StdLogger = (*logging.LegoAdapter)(nil)

const rateLimitGCMaxAge = 1 * time.Hour

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	showVersion := flag.Bool("version", false, "print version and exit")

	flag.Parse()

	if *showVersion {
		fmt.Printf("tripflare %s (commit %s, built %s)\n", version, commit, date)
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	var logHandler slog.Handler

	switch cfg.LogFormat {
	case "text":
		logHandler = logging.NewPrettyHandler(os.Stdout, slog.LevelDebug)
	default:
		logHandler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
	}

	slog.SetDefault(slog.New(logHandler))

	legoLog.Logger = logging.NewLegoAdapter()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, cfg, cancel); err != nil {
		slog.Error("fatal", "error", err)
		cancel()
		os.Exit(1) //nolint:gocritic // intentional exit after fatal error
	}
}

func run(ctx context.Context, cfg *config.Config, cancel context.CancelFunc) error {
	slog.Info("config loaded", "domain", cfg.Domain, "external_ip", cfg.ExternalIP)

	eventStore, reg, geo, accessLimit, err := initServices(ctx, cfg)
	if err != nil {
		return err
	}
	defer eventStore.Close()
	defer geo.Close()

	if err := startServers(ctx, cfg, reg, eventStore, accessLimit, geo, cancel); err != nil {
		return err
	}

	go flushLoop(ctx, reg, eventStore, cfg.Limits.FlushInterval.Duration(), accessLimit)

	<-ctx.Done()
	slog.Info("shutting down")

	if err := reg.FlushTo(context.Background(), eventStore); err != nil {
		slog.Error("final flush failed", "error", err)
	}

	return nil
}

func initServices(
	ctx context.Context,
	cfg *config.Config,
) (store.Service, registry.Service, geoip.Service, ratelimit.Limiter, error) {
	eventStore, err := store.New(cfg.SQLitePath)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("create store: %w", err)
	}

	reg := registry.New(
		cfg.Limits.MaxEventsPerSlug,
		cfg.Limits.SlugTTL.Duration(),
	)

	if err := reg.LoadFrom(ctx, eventStore); err != nil {
		_ = eventStore.Close()
		return nil, nil, nil, nil, fmt.Errorf("load registry: %w", err)
	}

	geo, err := geoip.New(cfg.GeoIPPath)
	if err != nil {
		slog.Warn("geoip disabled, falling back to noop", "error", err)

		geo, err = geoip.New("")
		if err != nil {
			_ = eventStore.Close()
			return nil, nil, nil, nil, fmt.Errorf("create noop geoip: %w", err)
		}
	}

	accessLimit := ratelimit.New([]ratelimit.TierConfig{
		{Capacity: cfg.Limits.MaxHitsPerSlugPerMinute, Window: time.Minute},
	})

	return eventStore, reg, geo, accessLimit, nil
}

func startServers(
	ctx context.Context,
	cfg *config.Config,
	reg registry.Service,
	eventStore store.Service,
	accessLimit ratelimit.Limiter,
	geo geoip.Service,
	cancel context.CancelFunc,
) error {
	challengeStore := acme.NewChallengeStore()

	dnsServer, err := dns.New(cfg.Domain, cfg.ExternalIP, cfg.Listen.DNS, cfg.Nameservers, reg, challengeStore)
	if err != nil {
		return fmt.Errorf("create dns server: %w", err)
	}

	dnsReady := make(chan struct{})

	go func() {
		if err := dnsServer.ListenAndServe(ctx, dnsReady); err != nil {
			slog.Error("dns server failed", "error", err)
			cancel()
		}
	}()

	select {
	case <-dnsReady:
		slog.Info("dns server ready")
	case <-ctx.Done():
		return nil
	}

	acmeManager, err := acme.New(
		cfg.Domain,
		cfg.ACME.Email,
		cfg.CertPath,
		cfg.ACME.Staging,
		cfg.ACME.Enabled,
		challengeStore,
	)
	if err != nil {
		return fmt.Errorf("create acme manager: %w", err)
	}

	go func() {
		if err := acmeManager.Run(ctx); err != nil {
			slog.Error("acme manager failed", "error", err)
			cancel()
		}
	}()

	httpServer := httpserver.New(
		cfg.Domain, cfg.Listen.HTTP, cfg.Listen.HTTPS,
		reg, eventStore, acmeManager.GetCertificate,
		accessLimit,
		cfg.Auth,
		geo,
	)

	go func() {
		if err := httpServer.ListenAndServe(ctx); err != nil {
			slog.Error("http server failed", "error", err)
			cancel()
		}
	}()

	slog.Info("tripflare started",
		"domain", cfg.Domain,
		"dns", cfg.Listen.DNS,
		"http", cfg.Listen.HTTP,
		"https", cfg.Listen.HTTPS,
	)

	return nil
}

func flushLoop(
	ctx context.Context,
	reg registry.Service,
	st store.Service,
	interval time.Duration,
	accessLimit ratelimit.Limiter,
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := reg.FlushTo(ctx, st); err != nil {
				slog.Error("flush failed", "error", err)
			}

			reg.Cleanup()

			if _, err := st.DeleteExpired(ctx, time.Now()); err != nil {
				slog.Error("delete expired failed", "error", err)
			}

			accessLimit.GC(rateLimitGCMaxAge)
		case <-ctx.Done():
			return
		}
	}
}
