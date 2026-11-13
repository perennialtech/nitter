// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/perennialtech/nitter/internal/auth"
	"github.com/perennialtech/nitter/internal/config"
	"github.com/perennialtech/nitter/internal/prefs"
	"github.com/perennialtech/nitter/internal/types"
)

var testReq = gqlReq("abc123/TestOperation", `{"a":1}`, `{"t":true}`)

func gzipped(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	w.Write([]byte(s))
	w.Close()
	return b.Bytes()
}

type debugSession struct {
	Kind    string
	Pending int
	APIs    map[string]struct{ Remaining, Reset int64 } `json:"apis"`
}

func debugSessions(t *testing.T) map[string]debugSession {
	t.Helper()
	var d map[string]debugSession
	if err := json.Unmarshal(auth.DebugJSON(), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func sessionTotal(t *testing.T) int {
	t.Helper()
	var h struct{ Sessions struct{ Total int } }
	if err := json.Unmarshal(auth.HealthJSON(), &h); err != nil {
		t.Fatal(err)
	}
	return h.Sessions.Total
}

func TestFetchSendsCookieRequestAndRecordsRateLimit(t *testing.T) {
	addCookieSessions(t, 1)
	reset := time.Now().Add(time.Hour).Unix()
	var got *http.Request
	fakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		got = r
		w.Header().Set("x-rate-limit-remaining", "40")
		w.Header().Set("x-rate-limit-reset", strconv.FormatInt(reset, 10))
		w.Header().Set("x-rate-limit-limit", "50")
		w.Header().Set("Content-Encoding", "gzip")
		w.Write(gzipped(t, `{"data":{"ok":true}}`))
	})

	js, err := fetch(context.Background(), testReq)
	if err != nil || !js.Get("data.ok").Bool() {
		t.Fatalf("got %s, %v", js.Raw, err)
	}
	if got.Host != "x.com" || got.URL.Path != "/i/api/graphql/abc123/TestOperation" {
		t.Fatalf("requested %s%s", got.Host, got.URL.Path)
	}
	wantQuery := "variables=" + url.QueryEscape(`{"a":1}`) + "&features=" + url.QueryEscape(gqlFeatures) +
		"&fieldToggles=" + url.QueryEscape(`{"t":true}`)
	if got.URL.RawQuery != wantQuery {
		t.Fatalf("query %s\nwant  %s", got.URL.RawQuery, wantQuery)
	}
	h := got.Header
	if h.Get("Authorization") != bearerToken2 || h.Get("X-Twitter-Auth-Type") != "OAuth2Session" ||
		!strings.HasPrefix(h.Get("Cookie"), "auth_token=auth") || h.Get("X-Csrf-Token") == "" ||
		h.Get("X-Client-Transaction-Id") != "" {
		t.Fatalf("headers %v", h)
	}

	found := false
	for _, s := range debugSessions(t) {
		if s.Pending != 0 {
			t.Fatal("sessions must be released")
		}
		if rl, ok := s.APIs["abc123/TestOperation"]; ok && rl.Remaining == 40 && rl.Reset == reset {
			found = true
		}
	}
	if !found {
		t.Fatal("the rate limit headers must be recorded")
	}
}

func TestFetchFailures(t *testing.T) {
	addCookieSessions(t, 3)
	oldTimeout := apiTimeout
	apiTimeout = 200 * time.Millisecond
	t.Cleanup(func() { apiTimeout = oldTimeout })

	for _, tc := range []struct {
		name   string
		status int
		body   string
		delay  time.Duration
		want   error
		calls  int32
	}{
		{"rate limit code", 200, `{"errors":[{"code":88,"message":"Rate limit exceeded"}]}`, 0, types.ErrRateLimited, 1},
		{"skipped code", 200, `{"errors":[{"code":144}],"data":{}}`, 0, nil, 1},
		{"503", 503, "", 0, types.ErrBadClient, 2},
		{"400", 400, `{"data":{}}`, 0, types.ErrInternal, 1},
		{"transient 404", 404, "", 0, types.ErrRateLimited, 1},
		{"cloudflare", 403, "<!DOCTYPE html><title>Attention | Cloudflare</title>", 0, types.ErrRateLimited, 1},
		{"429 text", 429, "429 Too Many Requests", 0, types.ErrRateLimited, 1},
		{"timeout", 200, "{}", 400 * time.Millisecond, types.ErrRateLimited, 1},
		{"not JSON", 200, "hello", 0, nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			fakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				select {
				case <-time.After(tc.delay):
				case <-r.Context().Done():
					return
				}
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			})
			_, err := fetch(context.Background(), testReq)
			if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if n := calls.Load(); n != tc.calls {
				t.Fatalf("%d upstream requests, want %d", n, tc.calls)
			}
		})
	}
}

func TestExpiredTokenInvalidatesSession(t *testing.T) {
	addCookieSessions(t, 2)
	fakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"errors":[{"code":89,"message":"Invalid or expired token."}]}`))
	})
	before := sessionTotal(t)
	if _, err := fetchRaw(context.Background(), testReq); !errors.Is(err, types.ErrRateLimited) {
		t.Fatalf("got %v", err)
	}
	if after := sessionTotal(t); after != before-1 {
		t.Fatalf("sessions %d -> %d; the expired session must be dropped", before, after)
	}
	for _, s := range debugSessions(t) {
		if s.Pending != 0 {
			t.Fatal("sessions must be released")
		}
	}
}

func TestRetriesUseAnotherAttempt(t *testing.T) {
	addCookieSessions(t, 2)
	oldRetries := maxRetries
	maxRetries = 2
	t.Cleanup(func() { maxRetries = oldRetries })
	var calls atomic.Int32
	fakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(404)
			return
		}
		w.Write([]byte(`{"data":{}}`))
	})
	if _, err := fetch(context.Background(), testReq); err != nil {
		t.Fatalf("the second attempt must succeed: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("%d upstream requests", calls.Load())
	}
}

func TestFxTwitterFirstThenSessions(t *testing.T) {
	addCookieSessions(t, 1)
	var fx, sessions atomic.Int32
	fxStatus := 200
	fakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Host {
		case "api.fxtwitter.com":
			fx.Add(1)
			if r.URL.Path != "/2/profile/jack" || r.Header.Get("User-Agent") != "Nitter" {
				t.Errorf("fx request %s %v", r.URL, r.Header)
			}
			w.WriteHeader(fxStatus)
			w.Write([]byte(`{"code":` + strconv.Itoa(fxStatus) + `,"user":{"id":"12","screen_name":"jack"}}`))
		case "x.com":
			sessions.Add(1)
			if !strings.HasSuffix(r.URL.Path, "/UserByScreenName") {
				t.Errorf("session request %s", r.URL)
			}
			w.Write([]byte(`{"data":{"user":{"result":{"rest_id":"12"}}}}`))
		default:
			t.Errorf("unexpected host %s", r.Host)
		}
	})

	u, err := GraphUser(context.Background(), "jack")
	if err != nil || u.ID != "12" || u.Username != "jack" || fx.Load() != 1 || sessions.Load() != 0 {
		t.Fatalf("FxTwitter must answer: %+v, %v, fx %d, sessions %d", u, err, fx.Load(), sessions.Load())
	}
	fxStatus = 500
	if _, err := GraphUser(context.Background(), "jack"); err != nil || sessions.Load() != 1 {
		t.Fatalf("a failed FxTwitter call must fall back to the sessions: %v, sessions %d", err, sessions.Load())
	}
	if !strings.Contains(formatAPIStats(), "fx profile error=1") {
		t.Fatalf("stats: %s", formatAPIStats())
	}
}

func TestFxRateLimit(t *testing.T) {
	var calls atomic.Int32
	fakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(429)
	})
	for range 2 {
		if _, err := fxGet(context.Background(), "search", "search", nil, false); !fxFailed(err) {
			t.Fatalf("got %v", err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("a 429 must stop requests until the next minute, got %d", calls.Load())
	}
	fxRate.Lock()
	fxRate.minute = 0
	fxRate.Unlock()
}

// Independent HMAC-SHA1 signature of a GET with one query parameter.
func TestOAuthHeader(t *testing.T) {
	rawURL := "https://api.x.com/graphql/abc/Op?variables=%7B%22a%22%3A1%7D"
	h := oauthHeader(rawURL, "123-token", "tokensecret", 1700000000)

	params := "oauth_consumer_key=" + consumerKey + "&oauth_nonce=0&oauth_signature_method=HMAC-SHA1" +
		"&oauth_timestamp=1700000000&oauth_token=123-token&oauth_version=1.0" +
		"&variables=%7B%22a%22%3A1%7D"
	base := "GET&" + url.QueryEscape("https://api.x.com/graphql/abc/Op") + "&" +
		strings.ReplaceAll(url.QueryEscape(params), "+", "%20")
	mac := hmac.New(sha1.New, []byte(consumerSecret+"&tokensecret"))
	mac.Write([]byte(base))
	sig := url.QueryEscape(base64.StdEncoding.EncodeToString(mac.Sum(nil)))

	want := `OAuth oauth_consumer_key="` + consumerKey + `", oauth_signature_method="HMAC-SHA1", ` +
		`oauth_timestamp="1700000000", oauth_nonce="0", oauth_signature="` + sig + `", ` +
		`oauth_token="123-token", oauth_version="1.0"`
	if h != want {
		t.Fatalf("got  %s\nwant %s", h, want)
	}
}

func TestOAuthRequestTarget(t *testing.T) {
	tg := userURL("jack").target(auth.OAuth)
	if !strings.HasPrefix(tg.url, "https://api.x.com/graphql/"+graphUserV2+"?variables=") || tg.path != "/graphql/"+graphUserV2 {
		t.Fatalf("target %+v", tg)
	}
	tg = restReq(restLiveStream + "key").target(auth.Cookie)
	if tg.url != "https://x.com/i/api/1.1/live_video_stream/status/key" {
		t.Fatalf("REST target %+v", tg)
	}
}

func TestGuestHeadersUseRuntimeBearerToken(t *testing.T) {
	saveSettings(t)
	if err := Init(&config.Config{GuestAuth: true}, " "); err == nil {
		t.Fatal("guest mode requires a bearer token")
	}
	if err := Init(&config.Config{GuestAuth: true, APIProxy: "proxy.local/", APITimeoutMs: 10}, "guest-bearer-fixture"); err != nil {
		t.Fatal(err)
	}
	if apiProxy != "http://proxy.local/" || apiTimeout != time.Second {
		t.Fatalf("apiProxy %q, apiTimeout %v", apiProxy, apiTimeout)
	}

	s := auth.NewGuestSession("guest-fixture")
	h, err := genHeaders(context.Background(), s, target{url: "https://x.com/i/api/graphql/test", path: "/i/api/graphql/test"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if h.Get("Authorization") != "Bearer guest-bearer-fixture" || h.Get("X-Guest-Token") != "guest-fixture" {
		t.Fatalf("headers %v", h)
	}
}

func TestTransactionID(t *testing.T) {
	key := []byte{1, 2, 3, 4, 5}
	pair := tidPair{animationKey: "anim", verification: base64.StdEncoding.EncodeToString(key)}
	tid, err := tidFromPair(pair, "/i/api/graphql/x/Op", 0x01020304, 0x5a)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(tid, "=") {
		t.Fatal("padding must be stripped")
	}
	raw, err := base64.RawStdEncoding.DecodeString(tid)
	if err != nil || raw[0] != 0x5a {
		t.Fatalf("%x, %v", raw, err)
	}
	for i := 1; i < len(raw); i++ {
		raw[i] ^= 0x5a
	}
	plain := raw[1:]
	if !bytes.Equal(plain[:5], key) || !bytes.Equal(plain[5:9], []byte{4, 3, 2, 1}) ||
		len(plain) != 5+4+16+1 || plain[len(plain)-1] != 3 {
		t.Fatalf("decoded %x", plain)
	}
}

func TestCookieRequestsGetTransactionIDs(t *testing.T) {
	addCookieSessions(t, 1)
	disableTid = false
	t.Cleanup(func() { disableTid = true })
	tidPairs.Lock()
	tidPairs.pairs = nil
	tidPairs.Unlock()

	var pairRequests atomic.Int32
	var tid string
	fakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "raw.githubusercontent.com" {
			pairRequests.Add(1)
			w.Write([]byte(`[{"animationKey":"anim","verification":"AQIDBAU="}]`))
			return
		}
		tid = r.Header.Get("X-Client-Transaction-Id")
		if r.Header.Get("Authorization") != bearerToken {
			t.Errorf("authorization %q", r.Header.Get("Authorization"))
		}
		w.Write([]byte(`{}`))
	})
	for range 2 {
		if _, err := fetch(context.Background(), testReq); err != nil {
			t.Fatal(err)
		}
	}
	if tid == "" || pairRequests.Load() != 1 {
		t.Fatalf("tid %q, pair requests %d", tid, pairRequests.Load())
	}
}

func TestGrok429CoolsDownOnlyTranslation(t *testing.T) {
	addCookieSessions(t, 2)
	grok := apiReq{cookie: apiURL{endpoint: "2/grok/translation.json"}}
	s, err := auth.GetCookieSession(grok.auth())
	if err != nil {
		t.Fatal(err)
	}
	h := http.Header{}
	h.Set("retry-after", "120")
	recordTranslationRateLimit(s, grok, http.StatusTooManyRequests, h)
	auth.Release(s)

	for range 10 {
		next, err := auth.GetCookieSession(grok.auth())
		if err != nil {
			t.Fatal(err)
		}
		auth.Release(next)
		if next == s {
			t.Fatal("a Grok 429 must cool the session down for translations")
		}
	}
	for _, d := range debugSessions(t) {
		for api, rl := range d.APIs {
			if api != "2/grok/translation.json" && rl.Remaining == 0 {
				t.Fatalf("only the translation endpoint may cool down, got %s", api)
			}
		}
	}
}

func TestPostGrokTranslation(t *testing.T) {
	addCookieSessions(t, 1)
	var body string
	fakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		var b bytes.Buffer
		b.ReadFrom(r.Body)
		body = b.String()
		if r.Method != http.MethodPost || r.Host != "api.x.com" || r.URL.Path != "/2/grok/translation.json" ||
			r.Header.Get("Authorization") != bearerToken || r.Header.Get("Content-Type") != "text/plain;charset=UTF-8" {
			t.Errorf("request %s %s %v", r.Method, r.URL, r.Header)
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Write(gzipped(t, "data: {}"))
	})
	got, err := PostGrokTranslation(context.Background(), "123", "en", "POST")
	if err != nil || got != "data: {}" {
		t.Fatalf("got %q, %v", got, err)
	}
	if body != `{"content_type":"POST","id":"123","dst_lang":"en"}` {
		t.Fatalf("body %s", body)
	}
}

func TestResolveDoesNotFollowRedirects(t *testing.T) {
	var requests atomic.Int32
	fakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodHead {
			t.Errorf("method %s", r.Method)
		}
		if r.URL.Path == "/abc" {
			w.Header().Set("Location", "https://example.com/target")
			w.WriteHeader(http.StatusMovedPermanently)
		}
	})
	p := prefs.Parse(nil, nil)
	got, err := Resolve(context.Background(), "https://t.co/abc", &p)
	if err != nil || got != "https://example.com/target" || requests.Load() != 1 {
		t.Fatalf("got %q, %v after %d requests", got, err, requests.Load())
	}
	if got, err := Resolve(context.Background(), "https://t.co/none", &p); err != nil || got != "" {
		t.Fatalf("no redirect: %q, %v", got, err)
	}
}

func TestDialErrorsAreNotRetried(t *testing.T) {
	addCookieSessions(t, 1)
	useClient(t, newHTTPClient(nil))
	saveSettings(t)
	apiProxy = "http://127.0.0.1:1/"
	_, err := fetch(context.Background(), testReq)
	if err == nil || errors.Is(err, types.ErrRateLimited) || !isDialError(err) {
		t.Fatalf("a refused connection must be returned as it is, got %v", err)
	}
}
