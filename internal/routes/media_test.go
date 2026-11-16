// SPDX-License-Identifier: AGPL-3.0-only

package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDirectBroadcastManifest(t *testing.T) {
	const upstream = "https://video.twimg.com/broadcast/live.m3u8?token=abc"
	r := httptest.NewRequest(http.MethodGet, "/i/broadcasts/1/stream?directMedia=on", nil)
	w := httptest.NewRecorder()
	c := ctx{s: &Server{}, w: w, r: r}
	if err := c.streamManifest(upstream); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != upstream {
		t.Errorf("direct manifest status = %d, location = %q, want redirect to %s", w.Code, w.Header().Get("Location"), upstream)
	}
}
