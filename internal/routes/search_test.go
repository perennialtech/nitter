// SPDX-License-Identifier: AGPL-3.0-only

package routes

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/perennialtech/nitter/internal/config"
)

func TestHashtagRedirect(t *testing.T) {
	s := &Server{cfg: &config.Config{StaticDir: t.TempDir()}}
	s.searchRoutes()
	for _, tag := range []string{"日本", "go", "a+b", "%E6%97%A5"} {
		t.Run(tag, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/hashtag/"+url.PathEscape(tag), nil)
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusSeeOther)
			}
			target, err := url.Parse(w.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			if target.Path != "/search" || target.Query().Get("f") != "tweets" || target.Query().Get("q") != "#"+tag {
				t.Errorf("redirect = %q, want search for %q", target, "#"+tag)
			}
		})
	}
}
