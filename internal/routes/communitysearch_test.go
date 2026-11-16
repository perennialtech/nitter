// SPDX-License-Identifier: AGPL-3.0-only
package routes

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/perennialtech/nitter/internal/api"
	"github.com/perennialtech/nitter/internal/auth"
	"github.com/perennialtech/nitter/internal/cache"
	"github.com/perennialtech/nitter/internal/config"
	"github.com/tidwall/gjson"
)

func TestCommunitySearchRoutes(t *testing.T) {
	const text = "space & science"
	const cursor = "next+/="
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vars := gjson.Parse(r.URL.Query().Get("variables"))
		operation := filepath.Base(r.URL.Path)
		after := vars.Get("cursor").String()
		if !strings.Contains(r.Header.Get("Cookie"), "auth_token=fixture") ||
			(after != "" && after != cursor) {
			t.Errorf("%s sent the wrong session or cursor", operation)
		}
		switch operation {
		case "CommunitiesSearchQuery":
			if vars.Get("query").String() != text {
				t.Error("community discovery query was not encoded")
			}
			fmt.Fprint(w, `{"data":{"community_search":{"items":[
				{"__typename":"Community","rest_id":"42","name":"Science <club>","member_count":2000,
				 "default_banner_media":{"media_info":{"original_img_url":"https://pbs.twimg.com/banner/test.jpg"}}},
				{"__typename":"Community","rest_id":"42","name":"Duplicate"},
				{"__typename":"Community","rest_id":"invalid","name":"Invalid"}
				],"next_cursor":"next+/="}}}`)
		case "GlobalCommunitiesPostSearchTimeline", "GlobalCommunitiesLatestPostSearchTimeline":
			if vars.Get("rawQuery").String() != text {
				t.Error("community post query was not encoded")
			}
			field := "communities_posts_search_page"
			if operation == "GlobalCommunitiesLatestPostSearchTimeline" {
				field = "communities_latest_posts_search_page"
			}
			fmt.Fprintf(w, `{"data":{"search_by_raw_query":{"%s":{"timeline":{"instructions":[
				{"type":"TimelineAddEntries","entries":[
					{"entryId":"tweet-9","content":{"itemContent":{
						"tweet_results":{"result":{"rest_id":"9","details":{"full_text":"Space post","display_text_range":[0,10]}}},
						"socialContext":{"contextType":"Community","text":"Science club","landingUrl":{"url":"https://x.com/i/communities/42"}}
					}}},
					{"entryId":"cursor-bottom-1","content":{"value":"next+/="}}
				]}
			]}}}}}`, field)
		default:
			t.Errorf("unexpected upstream operation %q", operation)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(upstream.Close)

	cfg := &config.Config{APIProxy: upstream.URL, DisableTid: true, MaxRetries: 1, CacheMaxMB: 1}
	sessions := filepath.Join(t.TempDir(), "sessions.jsonl")
	if err := os.WriteFile(sessions, []byte(`{"kind":"cookie","id":"101","auth_token":"fixture","ct0":"fixture"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := auth.Init(cfg, sessions); err != nil {
		t.Fatal(err)
	}
	if err := api.Init(cfg, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { api.Init(&config.Config{}, "") })
	cache.Init(cfg)
	s := &Server{cfg: cfg}
	s.searchRoutes()
	get := func(path string, status int, contains ...string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != status {
			t.Fatalf("%s: HTTP %d, want %d", path, w.Code, status)
		}
		for _, value := range contains {
			if !strings.Contains(w.Body.String(), value) {
				t.Errorf("%s: missing %q", path, value)
			}
		}
		return w
	}

	q := "&q=" + url.QueryEscape(text)
	w := get("/search?f=communitySearch"+q, http.StatusOK, "Science &lt;club&gt;", "2,000 members",
		"/i/communities/42", "f=communitiesLatest", "cursor=next%2B%2F%3D")
	if strings.Contains(w.Body.String(), "Duplicate") || strings.Contains(w.Body.String(), "/i/communities/invalid") {
		t.Error("discovery rendered duplicate or invalid results")
	}
	w = get("/search?f=communitySearch"+q+"&cursor="+url.QueryEscape(cursor)+"&scroll=true",
		http.StatusOK, "Science &lt;club&gt;")
	if strings.Contains(w.Body.String(), "Load more") || strings.Contains(w.Body.String(), "<html>") {
		t.Error("repeated discovery cursor did not end paginated results")
	}
	for _, mode := range []string{"communities", "communitiesLatest"} {
		get("/search?f="+mode+q, http.StatusOK, "Space post", "Science club",
			"f="+mode+"&amp;q=space&#43;%26&#43;science&amp;cursor=next%2B%2F%3D")
		get("/search?f="+mode+q+"&cursor="+url.QueryEscape(cursor)+"&scroll=true", http.StatusNoContent)
	}
	w = get("/search?q=%40NASA", http.StatusSeeOther)
	if location := w.Header().Get("Location"); location != "/NASA" {
		t.Errorf("mention redirected to %q, want /NASA", location)
	}
	get("/search", http.StatusOK, `value="users"`, `class="nav-search"`)
	cfg.GuestAuth = true
	get("/search?f=communitySearch"+q, http.StatusNotFound)
}
