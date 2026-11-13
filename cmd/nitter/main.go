// SPDX-License-Identifier: AGPL-3.0-only

// Command nitter serves the Nitter front-end. It reads nitter.conf (or
// $NITTER_CONF_FILE) and sessions.jsonl (or $NITTER_SESSIONS_FILE).
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/perennialtech/nitter/internal/api"
	"github.com/perennialtech/nitter/internal/auth"
	"github.com/perennialtech/nitter/internal/cache"
	"github.com/perennialtech/nitter/internal/config"
	"github.com/perennialtech/nitter/internal/flags"
	"github.com/perennialtech/nitter/internal/prefs"
	"github.com/perennialtech/nitter/internal/routes"
	"github.com/perennialtech/nitter/internal/utils"
)

func main() {
	log.SetFlags(0)
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func run() error {
	cfg, err := config.Load(getenv("NITTER_CONF_FILE", "./nitter.conf"))
	if err != nil {
		return err
	}
	if err := prefs.Init(cfg); err != nil {
		return fmt.Errorf("[Preferences]: %w", err)
	}

	flags.GuestAuth = cfg.GuestAuth
	flags.ShowRetweetTime = cfg.ShowRetweetTime
	flags.EnableTranslation = cfg.EnableTranslation
	if cfg.OnDemandTranslation && cfg.EnableTranslation {
		switch {
		case cfg.GuestAuth:
			log.Print("[translation] onDemandTranslation requires cookie sessions")
		case !cfg.HasHmacKey():
			log.Print("[translation] onDemandTranslation requires a unique hmacKey")
		default:
			flags.OnDemandTranslation = true
		}
	}
	flags.OnDemandTranslationLang = cfg.OnDemandTranslationLang

	utils.Init(cfg.HmacKey, cfg.Base64Media)
	if !cfg.HasHmacKey() {
		log.Print("WARNING: insecure default 'hmacKey' in nitter.conf; " +
			"set a unique random value to stop media URL signatures being forgeable.")
	}

	if err := auth.Init(cfg, getenv("NITTER_SESSIONS_FILE", "./sessions.jsonl")); err != nil {
		return err
	}
	if err := api.Init(cfg, os.Getenv("NITTER_GUEST_BEARER_TOKEN")); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cache.Init(ctx, cfg); err != nil {
		return fmt.Errorf("failed to connect to Redis at %s:%d: %w", cfg.RedisHost, cfg.RedisPort, err)
	}
	log.Printf("Connected to Redis at %s:%d", cfg.RedisHost, cfg.RedisPort)
	api.StartStatsLogger(ctx)

	handler, err := routes.New(cfg)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              net.JoinHostPort(cfg.Address, strconv.Itoa(cfg.Port)),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	log.Printf("Starting Nitter at %s", cfg.URLPrefix())

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}
