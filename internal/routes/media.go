// SPDX-License-Identifier: AGPL-3.0-only
package routes

import (
	"context"
	"encoding/base64"
	"errors"
	"hash/fnv"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/perennialtech/nitter/internal/formatters"
	"github.com/perennialtech/nitter/internal/utils"
)

const (
	m3u8Mime = "application/vnd.apple.mpegurl"
	maxAge   = "max-age=604800"
	// A stream that delivers nothing for this long is abandoned.
	mediaIdleTimeout = 5 * time.Second
	mediaAttempts    = 3
)

// mediaClient never follows redirects: proxyMedia only follows them to
// Twitter hosts, so a redirect can't reach an internal address (#1411).
var mediaClient = &http.Client{
	Transport: &http.Transport{
		ResponseHeaderTimeout: mediaIdleTimeout,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   32,
		ForceAttemptHTTP2:     true,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// safeFetch returns the body at u, or "" on any failure.
func safeFetch(ctx context.Context, u string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return ""
	}
	res, err := mediaClient.Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return ""
	}
	return string(body)
}

func etag(u string) string {
	h := fnv.New64a()
	h.Write([]byte(u))
	return `"` + strconv.FormatUint(h.Sum64(), 16) + `"`
}

// idleReader cancels the request once no data has arrived for
// mediaIdleTimeout, so a stalled upstream can't hold the connection.
type idleReader struct {
	r     io.Reader
	timer *time.Timer
}

func (ir idleReader) Read(p []byte) (int, error) {
	n, err := ir.r.Read(p)
	if n > 0 {
		ir.timer.Reset(mediaIdleTimeout)
	}
	return n, err
}

// proxyMedia streams u to w. The upstream request uses the client's
// context, so a disconnect cancels it. Range requests are passed through
// so browsers can seek in videos.
func proxyMedia(w http.ResponseWriter, r *http.Request, u string) {
	tag := etag(u)
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	fetchURL := u
	for attempt := 1; attempt <= mediaAttempts; attempt++ {
		ctx, cancel := context.WithCancel(r.Context())
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
		if err != nil {
			cancel()
			http.NotFound(w, r)
			return
		}
		if rng := r.Header.Get("Range"); rng != "" {
			req.Header.Set("Range", rng)
		}
		res, err := mediaClient.Do(req)
		if err != nil {
			cancel()
			if r.Context().Err() != nil {
				return
			}
			if attempt < mediaAttempts {
				log.Printf("[media] Retry %d/%d, error: %v, url: %s", attempt, mediaAttempts-1, err, fetchURL)
				continue
			}
			log.Printf("[media] Proxying failed, error: %v, url: %s", err, fetchURL)
			if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
				w.WriteHeader(http.StatusGatewayTimeout)
			} else {
				http.NotFound(w, r)
			}
			return
		}

		switch {
		case res.StatusCode == http.StatusOK || res.StatusCode == http.StatusPartialContent:
			stream(w, res, tag, cancel)
			cancel()
			return
		case res.StatusCode == http.StatusNotFound:
			res.Body.Close()
			cancel()
			http.NotFound(w, r)
			return
		case res.StatusCode >= 300 && res.StatusCode < 400 && res.Header.Get("Location") != "":
			res.Body.Close()
			cancel()
			loc, err := res.Request.URL.Parse(res.Header.Get("Location"))
			if err != nil || !utils.IsTwitterURL(loc.String()) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			fetchURL = loc.String()
			continue
		}
		res.Body.Close()
		cancel()
		if attempt < mediaAttempts {
			log.Printf("[media] Retry %d/%d, status: %s, url: %s", attempt, mediaAttempts-1, res.Status, fetchURL)
			continue
		}
		log.Printf("[media] Proxying failed, status: %s, url: %s", res.Status, fetchURL)
		http.NotFound(w, r)
		return
	}
	http.NotFound(w, r)
}

func isTimeout(err error) bool {
	var t interface{ Timeout() bool }
	return errors.As(err, &t) && t.Timeout()
}

func stream(w http.ResponseWriter, res *http.Response, tag string, cancel context.CancelFunc) {
	defer res.Body.Close()
	h := w.Header()
	for _, k := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges"} {
		if v := res.Header.Get(k); v != "" {
			h.Set(k, v)
		}
	}
	h.Set("Cache-Control", maxAge)
	h.Set("ETag", tag)
	w.WriteHeader(res.StatusCode)

	timer := time.AfterFunc(mediaIdleTimeout, func() {
		log.Printf("[media] Streaming stalled, url: %s", res.Request.URL)
		cancel()
	})
	defer timer.Stop()
	// Errors here are disconnects or stalls; the response is already started.
	_, _ = io.Copy(w, idleReader{res.Body, timer})
}

// decodeMediaPath decodes the URL part of a /pic or /video path, which is
// base64 after an "enc/" prefix and percent-encoded otherwise.
func decodeMediaPath(rest string) (string, bool) {
	if enc, ok := strings.CutPrefix(rest, "enc/"); ok {
		b, err := base64.URLEncoding.DecodeString(enc)
		if err != nil {
			// Accept unpadded input too.
			b, err = base64.RawURLEncoding.DecodeString(strings.TrimRight(enc, "="))
			if err != nil {
				return "", false
			}
		}
		return string(b), true
	}
	s, err := url.QueryUnescape(rest)
	if err != nil {
		return "", false
	}
	return s, true
}

func normalizeImgURL(u string) string {
	if strings.HasPrefix(u, "http") {
		return u
	}
	if !strings.Contains(u, "twimg.com") {
		u = utils.Twimg + u
	}
	return utils.HTTPS + u
}

func (s *Server) mediaRoutes() {
	s.get("/pic/?", func(c *ctx) error { return c.status(http.StatusNotFound) })
	s.getRe(`^/pic/.+`, func(c *ctx) error { s.handlePic(c.w, c.r); return nil })
	s.getRe(`^/video/.+/.+$`, func(c *ctx) error { s.handleVideo(c.w, c.r); return nil })
}

// handlePic serves /pic/<url> and /pic/orig/<url>; either can be
// /enc/<base64>.
func (s *Server) handlePic(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.EscapedPath(), "/pic/")
	orig := false
	if after, ok := strings.CutPrefix(rest, "orig/"); ok {
		rest, orig = after, true
	}
	u, ok := decodeMediaPath(rest)
	if !ok || u == "" || strings.Contains(u, "/amplify_video/") {
		s.notFound(w, r)
		return
	}
	u = normalizeImgURL(u)
	if orig {
		u += "?name=orig"
	}
	if !utils.IsTwitterURL(u) {
		s.notFound(w, r)
		return
	}
	proxyMedia(w, r, u)
}

// handleVideo serves /video/<sig>/<url> and /video/enc/<sig>/<base64>.
// Segments are streamed; playlists are rewritten so their segments go
// through this proxy too.
func (s *Server) handleVideo(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.EscapedPath(), "/video/")
	enc := false
	if after, ok := strings.CutPrefix(rest, "enc/"); ok {
		rest, enc = after, true
	}
	sig, encoded, ok := strings.Cut(rest, "/")
	if !ok || encoded == "" {
		s.notFound(w, r)
		return
	}
	if enc {
		encoded = "enc/" + encoded
	}
	u, ok := decodeMediaPath(encoded)
	if !ok || !utils.IsTwitterURL(u) {
		s.notFound(w, r)
		return
	}
	if utils.Hmac(u) != sig {
		s.showError(w, r, http.StatusForbidden, "Failed to verify signature")
		return
	}

	for _, ext := range []string{".mp4", ".ts", ".m4s", ".aac"} {
		if strings.Contains(u, ext) {
			proxyMedia(w, r, u)
			return
		}
	}

	var content string
	if strings.Contains(u, ".vmap") {
		if formatters.M3u8URL(safeFetch(r.Context(), u)) == "" {
			s.notFound(w, r)
			return
		}
		content = safeFetch(r.Context(), u)
	}
	if strings.Contains(u, ".m3u8") {
		p := s.prefs(r)
		content = formatters.ProxifyVideo(safeFetch(r.Context(), u), p.ProxyVideos, u)
	}
	w.Header().Set("Content-Type", m3u8Mime)
	io.WriteString(w, content)
}
