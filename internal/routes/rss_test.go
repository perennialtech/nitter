// SPDX-License-Identifier: AGPL-3.0-only

package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/perennialtech/nitter/internal/cache"
	"github.com/perennialtech/nitter/internal/config"
	"github.com/perennialtech/nitter/internal/types"
)

func TestCommunityNotesRssCacheVariants(t *testing.T) {
	cfg := &config.Config{
		CacheMaxMB: 1, RssCacheTime: 1, EnableRSSCommunityNotes: true, EnableTranslation: true,
	}
	cache.Init(cfg)
	s := &Server{cfg: cfg}
	s.rssRoutes()

	for _, tt := range []struct{ tab, path string }{
		{"helpful", "/i/communitynotes/rss"},
		{"new", "/i/communitynotes/new/rss"},
	} {
		t.Run(tt.tab, func(t *testing.T) {
			base := "communitynotes:" + tt.tab
			for _, suffix := range []string{"", ":tr"} {
				_, err := cache.Rss(context.Background(), base+suffix, func() (types.Rss, error) {
					return types.Rss{Feed: base + suffix, Cursor: "123"}, nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}

			for _, request := range []struct {
				query, suffix string
				enabled       bool
			}{
				{"", "", true},
				{"?tr=1", ":tr", true},
				{"?tr=0", "", true},
				{"?tr=1", "", false},
			} {
				cfg.EnableTranslation = request.enabled
				w := httptest.NewRecorder()
				s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tt.path+request.query, nil))
				if want := base + request.suffix; w.Code != http.StatusOK || w.Body.String() != want {
					t.Errorf("%s (translation %t): status = %d, body = %q, want 200 and %q",
						tt.path+request.query, request.enabled, w.Code, w.Body.String(), want)
				}
			}
		})
	}
}

func TestRssMediaPreferenceCacheVariants(t *testing.T) {
	cfg := &config.Config{CacheMaxMB: 1, RssCacheTime: 1, EnableRSSUserTweets: true}
	cache.Init(cfg)
	s := &Server{cfg: cfg}
	s.rssRoutes()
	for _, key := range []string{"twitter:jack", "direct:twitter:jack"} {
		_, err := cache.Rss(context.Background(), key, func() (types.Rss, error) {
			return types.Rss{Feed: key, Cursor: "123"}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, tt := range []struct {
		query, cookie, want string
	}{
		{"", "on", "direct:twitter:jack"},
		{"", "", "twitter:jack"},
		{"?directMedia=on", "", "direct:twitter:jack"},
		{"?directMedia=false", "on", "twitter:jack"},
	} {
		r := httptest.NewRequest(http.MethodGet, "/jack/rss"+tt.query, nil)
		if tt.cookie != "" {
			r.AddCookie(&http.Cookie{Name: "directMedia", Value: tt.cookie})
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != http.StatusOK || w.Body.String() != tt.want {
			t.Errorf("RSS with query %q and cookie %q: status = %d, body = %q, want 200 and %q",
				tt.query, tt.cookie, w.Code, w.Body.String(), tt.want)
		}
		if got := w.Header().Get("Vary"); got != "Cookie" {
			t.Errorf("RSS Vary = %q, want Cookie", got)
		}
	}
}
