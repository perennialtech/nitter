// SPDX-License-Identifier: AGPL-3.0-only
package routes

import (
	"context"
	"errors"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/perennialtech/nitter/internal/config"
	"github.com/perennialtech/nitter/internal/formatters"
	"github.com/perennialtech/nitter/internal/prefs"
	"github.com/perennialtech/nitter/internal/types"
	"github.com/perennialtech/nitter/internal/views"
)

const (
	instancesURL = "https://github.com/zedeus/nitter/wiki/Instances"
	issuesURL    = "https://github.com/perennialtech/nitter/issues"
)

// New builds the handler for every nitter route. Routes are tried in
// order, so a route whose conditions don't hold passes to the next.
func New(cfg *config.Config) (http.Handler, error) {
	if err := views.Init(cfg.StaticDir); err != nil {
		return nil, err
	}
	themes, err := findThemes(cfg.StaticDir)
	if err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, themes: themes}
	s.generalRoutes()
	s.articleRoutes()
	s.rssRoutes()
	s.statusRoutes()
	s.searchRoutes()
	s.timelineRoutes()
	s.mediaRoutes()
	s.listRoutes()
	s.communityRoutes()
	s.preferenceRoutes()
	s.resolverRoutes()
	s.embedRoutes()
	s.broadcastRoutes()
	s.spaceRoutes()
	s.debugRoutes()
	s.unsupportedRoutes()
	return s, nil
}

// findThemes lists the stylesheets in css/themes as theme names, such as
// "Catppuccin Mocha" for catppuccin_mocha.css.
func findThemes(staticDir string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(staticDir, "css", "themes"))
	if err != nil {
		return nil, err
	}
	var themes []string
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		themes = append(themes, formatters.Titleize(strings.ReplaceAll(name, "_", " ")))
	}
	sort.Strings(themes)
	return themes, nil
}

func (c *ctx) html(status int, body string) error {
	c.w.Header().Set("Content-Type", "text/html;charset=utf-8")
	c.w.WriteHeader(status)
	io.WriteString(c.w, body)
	return nil
}

func (c *ctx) text(status int, contentType, body string) error {
	c.w.Header().Set("Content-Type", contentType)
	c.w.WriteHeader(status)
	io.WriteString(c.w, body)
	return nil
}

func (c *ctx) status(code int) error {
	c.w.WriteHeader(code)
	return nil
}

// page renders body inside the main layout.
func (c *ctx) page(body template.HTML, o views.PageOpts) error {
	return c.html(http.StatusOK, views.RenderMain(body, c.r, c.s.cfg, c.prefs(), o))
}

// fail renders an error page. msg is HTML.
func (c *ctx) fail(status int, msg string) error {
	c.s.showError(c.w, c.r, status, msg)
	return nil
}

func (s *Server) showError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	body := views.RenderMain(views.RenderError(msg), r, s.cfg, s.prefs(r), views.PageOpts{TitleText: "Error"})
	w.Header().Set("Content-Type", "text/html;charset=utf-8")
	w.WriteHeader(status)
	io.WriteString(w, body)
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.showError(w, r, http.StatusNotFound, "Page not found")
}

func link(text, href string) string {
	return `<a href="` + href + `">` + text + `</a>`
}

// handleError maps an error from a route to an error page.
func (s *Server) handleError(c *ctx, err error) {
	switch {
	case errors.Is(err, context.Canceled) && c.r.Context().Err() != nil:
		// The client went away; there's nobody to answer.
	case errors.Is(err, types.ErrRateLimited):
		c.fail(http.StatusTooManyRequests, "Instance has been rate limited.<br>Use "+
			link("another instance", instancesURL)+" or try again later.")
	case errors.Is(err, types.ErrNoSessions):
		c.fail(http.StatusTooManyRequests, "Instance has no auth tokens, or is fully rate limited.<br>Use "+
			link("another instance", instancesURL)+" or try again later.")
	case errors.Is(err, types.ErrBadClient):
		log.Printf("%s: %v", c.r.URL.Path, err)
		c.fail(http.StatusInternalServerError, "Network error occurred, please try again.")
	default:
		log.Printf("%s: %v", c.r.URL.Path, err)
		c.fail(http.StatusInternalServerError, "An error occurred, please "+
			link("open a GitHub issue", issuesURL)+" with the URL you tried to visit.")
	}
}

func (s *Server) generalRoutes() {
	s.get("/", func(c *ctx) error {
		search := views.RenderSearch()
		body := search
		if cfg := s.cfg; cfg.EnableTrends {
			tab1, tab2, err := s.trends(c.r.Context())
			if err != nil {
				return err
			}
			body = views.RenderHomeBody(cfg.TrendsTab1Name, tab1, cfg.TrendsTab2Name, tab2, search)
		}
		return c.page(body, views.PageOpts{})
	})
	s.get("/about", func(c *ctx) error {
		return c.page(views.RenderAbout(), views.PageOpts{})
	})
	s.get("/explore", func(c *ctx) error { return c.redirect("/about") })
	s.get("/help", func(c *ctx) error { return c.redirect("/about") })
	s.get("/i/redirect", func(c *ctx) error {
		u, err := url.QueryUnescape(c.param("url"))
		if err != nil || u == "" {
			return c.status(http.StatusNotFound)
		}
		return c.redirect(formatters.ReplaceURLs(u, c.prefs(), ""))
	})
}

func (s *Server) preferenceRoutes() {
	s.get("/settings", func(c *ctx) error {
		p := c.prefs()
		prefsURL := s.cfg.URLPrefix() + "/?prefs=" + p.Encode()
		return c.page(views.RenderPreferences(p, c.refPath(), s.themes, prefsURL),
			views.PageOpts{TitleText: "Preferences"})
	})
	s.get("/settings/@i?", func(c *ctx) error { return c.redirect("/settings") })
	s.post("/saveprefs", func(c *ctx) error {
		for _, pref := range prefs.All() {
			val := c.param(pref.Name)
			if prefs.IsDefault(pref, val) {
				c.savePref(pref.Name, "", true)
			} else {
				c.savePref(pref.Name, val, false)
			}
		}
		return c.redirect(c.refPath())
	})
	s.post("/resetprefs", func(c *ctx) error {
		for _, pref := range prefs.All() {
			c.savePref(pref.Name, "", true)
		}
		return c.redirect("/settings?referer=" + url.QueryEscape(c.refPath()))
	})
	s.post("/enablehls", func(c *ctx) error {
		c.savePref("hlsPlayback", "on", false)
		return c.redirect(c.refPath())
	})
	s.post("/enablemp4", func(c *ctx) error {
		c.savePref("mp4Playback", "on", false)
		return c.redirect(c.refPath())
	})
}

func (s *Server) unsupportedRoutes() {
	feature := func(c *ctx) error { return c.page(views.RenderFeature(), views.PageOpts{}) }
	s.get("/about/feature", feature)
	s.get("/login/?@i?", feature)
	s.get("/@name/lists/?", feature)
	s.get("/intent/?@i?", func(c *ctx) error {
		if i := c.param("i"); i == "user" || i == "follow" {
			return errPass
		}
		return feature(c)
	})
	s.get("/i/@i?/?@j?", func(c *ctx) error {
		if i := c.param("i"); i == "status" || i == "lists" || i == "user" {
			return errPass
		}
		return feature(c)
	})
}
