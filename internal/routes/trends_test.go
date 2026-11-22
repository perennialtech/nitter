// SPDX-License-Identifier: AGPL-3.0-only

package routes

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tidwall/gjson"

	"github.com/perennialtech/nitter/internal/api"
	"github.com/perennialtech/nitter/internal/auth"
	"github.com/perennialtech/nitter/internal/cache"
	"github.com/perennialtech/nitter/internal/config"
)

func TestTrendsPages(t *testing.T) {
	var firstPages, details atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := gjson.Parse(r.URL.Query().Get("variables"))
		switch {
		case strings.HasSuffix(r.URL.Path, "/useStoryTopicQuery"):
			if v.Get("rest_id").String() != "For You" || v.Get("limit").Int() != 25 ||
				r.URL.Query().Get("features") != "{}" || !strings.Contains(r.Header.Get("Cookie"), "auth_token=fixture") {
				t.Error("story index request is missing its variables, features, or session")
			}
			switch cursor := v.Get("cursor").String(); cursor {
			case "":
				if firstPages.Add(1) == 1 {
					io.WriteString(w, `{"errors":[{"code":34}]}`)
					return
				}
				io.WriteString(w, `{"data":{"story_topic":{"stories":{"items":[
					{"trend_results":{"result":{"rest_id":"123","core":{"name":"Story <A>","category":"Zoo","created_at_ms":1740000000000},"post_count":30}}},
					{"trend_results":{"result":{"rest_id":"456","core":{"name":"Story B","category":"Arts","created_at_ms":1740010000000},"post_count":"1,234"}}},
					{"trend_results":{"result":{"rest_id":"789","core":{"name":"Story C","category":"Sports","created_at_ms":1740020000000},"post_count":"5"}}}
				],"slice_info":{"next_cursor":"news+/="}}}}}`)
			case "news+/=":
				io.WriteString(w, `{"data":{"story_topic":{"stories":{"items":[
					{"trend_results":{"result":{"rest_id":"321","core":{"name":"Next story"}}}}
				],"next_cursor":"news+/="}}}}`)
			default:
				t.Errorf("story list cursor = %q", cursor)
				w.WriteHeader(http.StatusBadRequest)
			}
		case strings.HasSuffix(r.URL.Path, "/AiTrendByRestId"):
			if v.Get("trendId").String() != "123" {
				io.WriteString(w, `{"data":{"ai_trend_by_rest_id":null}}`)
				return
			}
			details.Add(1)
			io.WriteString(w, `{"data":{"ai_trend_by_rest_id":{"result":{"page":{
				"article":{"title":"Example story","article_text":{"text":"A <summary>"}},
				"post_timelines":[{"label":"Top","post_timeline":{"id":"top"}},{"label":"Latest","post_timeline":{"id":"latest+/="}}]
			}}}}}`)
		case strings.HasSuffix(r.URL.Path, "/GenericTimelineById"):
			cursor, timeline := v.Get("cursor").String(), v.Get("timelineId").String()
			switch {
			case (timeline == "top" || timeline == "latest+/=") && cursor == "":
				io.WriteString(w, `{"data":{"timeline":{"timeline":{"instructions":[{"type":"TimelineAddEntries","entries":[
					{"entryId":"tweet-1","content":{"itemContent":{"tweet_results":{"result":{"rest_id":"1","details":{"full_text":"Story post","display_text_range":[0,10]}}}}}},
					{"entryId":"cursor-bottom-1","content":{"value":"posts+/="}}
				]}]}}}}`)
			case timeline == "latest+/=" && cursor == "posts+/=":
				io.WriteString(w, `{"data":{"timeline":{"timeline":{"instructions":[{"type":"TimelineReplaceEntry",
					"entry_id_to_replace":"cursor-bottom-1","entry":{"content":{"value":"posts+/="}}
				}]}}}}`)
			case timeline == "VGltZWxpbmU6CgCaAAAAAAAAA+cA":
				io.WriteString(w, `{"data":{"timeline":{"timeline":{"instructions":[]}}}}`)
			default:
				t.Errorf("post timeline = %q, cursor = %q", timeline, cursor)
				w.WriteHeader(http.StatusBadRequest)
			}
		default:
			t.Errorf("unexpected upstream request %s", r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(upstream.Close)

	cfg := &config.Config{APIProxy: upstream.URL, DisableTid: true, MaxRetries: 1, CacheMaxMB: 1, TrendsCacheMinutes: 15}
	path := filepath.Join(t.TempDir(), "sessions.jsonl")
	if err := os.WriteFile(path, []byte(`{"kind":"cookie","id":"100","auth_token":"fixture","ct0":"fixture"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := auth.Init(cfg, path); err != nil {
		t.Fatal(err)
	}
	if err := api.Init(cfg, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { api.Init(&config.Config{}, "") })
	cache.Init(cfg)
	s := &Server{cfg: cfg}
	s.generalRoutes()
	s.trendsRoutes()
	s.unsupportedRoutes()

	get := func(path string, status int, contains ...string) string {
		t.Helper()
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		body := w.Body.String()
		if w.Code != status {
			t.Fatalf("%s: status = %d, want %d", path, w.Code, status)
		}
		for _, text := range contains {
			if !strings.Contains(body, text) {
				t.Errorf("%s: missing %q", path, text)
			}
		}
		return body
	}

	for _, path := range []string{"/i/trends", "/i/trends?tab=news", "/i/trending", "/i/trending/123", "/i/trending/123/new"} {
		get(path, http.StatusNotFound)
	}
	if body := get("/", http.StatusOK); strings.Contains(body, "nav-trends") {
		t.Fatal("trends link shown while disabled")
	}
	cfg.EnableTrendsPages = true
	if body := get("/", http.StatusOK, `class="nav-trends" href="/i/trends"`); strings.Contains(body, "trends-panel") {
		t.Fatal("enabling trends pages enabled the homepage panel")
	}
	cfg.GuestAuth = true
	for _, path := range []string{"/i/trends?tab=news", "/i/trending", "/i/trending/123", "/i/trending/123/new"} {
		get(path, http.StatusNotFound)
	}
	cfg.GuestAuth = false
	get("/i/trending", http.StatusInternalServerError)
	for _, test := range []struct{ path, first, second string }{
		{"/i/trending", "Story &lt;A&gt;", "Story B"},
		{"/i/trends?tab=news&sort=recent", "Story C", "Story B"},
		{"/i/trends?tab=news&sort=count", "Story B", "Story &lt;A&gt;"},
		{"/i/trends?tab=news&sort=category", "Story B", "Story C"},
		{"/i/trends?tab=news&sort=unknown", "Story &lt;A&gt;", "Story B"},
	} {
		body := get(test.path, http.StatusOK, test.first, test.second, "1,234 posts")
		if strings.Index(body, test.first) >= strings.Index(body, test.second) {
			t.Errorf("%s: %q should sort before %q", test.path, test.first, test.second)
		}
	}
	get("/i/trends?tab=news&sort=count", http.StatusOK, `href="?cursor=news%2B%2F%3D&amp;sort=count&amp;tab=news"`)
	if body := get("/i/trends?cursor=news%2B%2F%3D&sort=count&tab=news", http.StatusOK, "Next story",
		`href="?sort=count&amp;tab=news">Load newest`); strings.Contains(body, "Load more") {
		t.Fatal("repeated news cursor yielded another page link")
	}
	if got := firstPages.Load(); got != 2 {
		t.Fatalf("first news page fetched %d times, want one failed fetch and one cached success", got)
	}

	get("/i/trending/123?timeline=invalid", http.StatusOK, "Example story", "A &lt;summary&gt;", "Story post")
	get("/i/trending/123?timeline=latest%2B%2F%3D", http.StatusOK, `href="?timeline=latest%2B%2F%3D&amp;cursor=posts%2B%2F%3D"`)
	get("/i/trending/123?timeline=latest%2B%2F%3D&cursor=posts%2B%2F%3D", http.StatusOK,
		`href="/i/trending/123?timeline=latest%2B%2F%3D">Load newest`, "No more items")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/i/trending/123/new", nil))
	if target := w.Header().Get("Location"); w.Code != http.StatusSeeOther || target != "/i/trending/123?timeline=latest%2B%2F%3D" {
		t.Errorf("latest redirect: %d to %q", w.Code, target)
	}
	if got := details.Load(); got != 1 {
		t.Fatalf("story detail fetched %d times, want 1", got)
	}
	get("/i/trending/999", http.StatusNotFound, "Story not found")
	get("/i/trending/not-a-number", http.StatusNotFound)

	for _, path := range []string{"/i/trends?tab=news", "/i/trending"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Header.Add("Accept-Language", "en;q=0.5")
		r.Header.Add("Accept-Language", "de-DE")
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "1.234 posts") || w.Header().Get("Vary") != "Accept-Language" {
			t.Errorf("%s: expected localized count and Vary: Accept-Language, got %d and Vary: %q",
				path, w.Code, w.Header().Get("Vary"))
		}
	}
}
