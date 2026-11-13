// SPDX-License-Identifier: AGPL-3.0-only

// Command nitter serves the Nitter front-end. It reads the config file and
// sessions file named by config.EnvVars.
//
// "nitter healthcheck" asks the instance configured in nitter.conf for
// /.health and exits non-zero unless it answers 200 OK, for container
// healthchecks in images with no shell or HTTP client.
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
	cmd := run
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		cmd = healthcheck
	}
	if err := cmd(); err != nil {
		log.Fatal(err)
	}
}

// healthcheck checks the server on the configured port. /.health only
// reads the session pools, so it doesn't depend on upstream.
func healthcheck() error {
	cfg, err := config.Load(config.ConfFile.Value())
	if err != nil {
		return err
	}
	host := cfg.Address
	if ip := net.ParseIP(host); host == "" || ip != nil && ip.IsUnspecified() {
		host = "127.0.0.1"
	}
	resp, err := http.Get("http://" + net.JoinHostPort(host, strconv.Itoa(cfg.Port)) + "/.health")
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("/.health: %s", resp.Status)
	}
	return nil
}

func run() error {
	cfg, err := config.Load(config.ConfFile.Value())
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

	if err := auth.Init(cfg, config.SessionsFile.Value()); err != nil {
		return err
	}
	if err := api.Init(cfg, config.GuestBearerToken.Value()); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cache.Init(cfg)
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
