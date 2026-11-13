// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"

	"github.com/perennialtech/nitter/internal/auth"
	"github.com/perennialtech/nitter/internal/config"
	"github.com/perennialtech/nitter/internal/flags"
	"github.com/perennialtech/nitter/internal/types"
)

const (
	rlRemaining             = "x-rate-limit-remaining"
	rlReset                 = "x-rate-limit-reset"
	rlLimit                 = "x-rate-limit-limit"
	npCache                 = "x-np-cache"
	translationRetrySeconds = 60

	guestActivateURL       = "https://api.x.com/1.1/guest/activate.json"
	grokTranslationURL     = "https://api.x.com/2/grok/translation.json"
	grokTranslationTimeout = 45 * time.Second

	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36"
	secChUa   = `"Google Chrome";v="142", "Chromium";v="142", "Not A(Brand";v="24"`
)

var errorsToSkip = []types.ErrorCode{
	types.ErrNull, types.ErrDoesntExist, types.ErrTweetNotFound,
	types.ErrTimeout, types.ErrUnauthorized, types.ErrBadRequest,
}

// Settings from nitter.conf, set once by Init before serving.
var (
	disableTid         bool
	apiProxy           string
	maxRetries         = 1
	retryDelay         time.Duration
	apiTimeout         = 15 * time.Second
	guestAuthorization string
)

// Init applies the API client settings in cfg. In guest mode
// (cfg.GuestAuth, which must match flags.GuestAuth), guestBearerToken is
// the bearer token guest requests authenticate with.
func Init(cfg *config.Config, guestBearerToken string) error {
	proxy, err := parseProxy(cfg.Proxy, cfg.ProxyAuth)
	if err != nil {
		return err
	}
	httpClient = newHTTPClient(proxy)
	apiProxy = ""
	if cfg.APIProxy != "" {
		apiProxy = strings.Trim(cfg.APIProxy, "/") + "/"
		if !strings.Contains(apiProxy, "http") {
			apiProxy = "http://" + apiProxy
		}
	}
	disableTid = cfg.DisableTid
	maxRetries = cfg.MaxRetries
	retryDelay = time.Duration(cfg.RetryDelayMs) * time.Millisecond
	apiTimeout = time.Duration(max(cfg.APITimeoutMs, 1000)) * time.Millisecond
	trendsCacheTTL = time.Duration(cfg.TrendsCacheMinutes) * time.Minute

	guestAuthorization = ""
	if cfg.GuestAuth {
		token := strings.TrimSpace(guestBearerToken)
		if token == "" {
			return errors.New("guestAuth requires NITTER_GUEST_BEARER_TOKEN")
		}
		guestAuthorization = "Bearer " + token
	}
	return nil
}

func isCloudflareHTML(body string) bool {
	// Cloudflare returns HTML error pages instead of JSON
	if len(body) < 14 || body[0] != '<' {
		return false
	}
	return strings.ToLower(body[:14]) == "<!doctype html" && strings.Contains(body, "Cloudflare")
}

// cfTitle extracts the <title> of a Cloudflare page for the logs.
func cfTitle(body string) string {
	start := strings.Index(body, "<title>")
	if start < 0 {
		return "unknown"
	}
	content := body[start+len("<title>"):]
	stop := strings.Index(content, "</title>")
	if stop < 0 {
		return "unknown"
	}
	return strings.Join(strings.Fields(content[:stop]), " ")
}

type param struct{ key, value string }

type apiURL struct {
	endpoint string
	params   []param
	skipTid  bool
}

// apiReq is one request, as it is made with each kind of session.
type apiReq struct {
	oauth  apiURL
	cookie apiURL
}

func (r apiReq) auth() auth.Req {
	return auth.Req{OAuth: r.oauth.endpoint, Cookie: r.cookie.endpoint}
}

func (r apiReq) forKind(kind auth.Kind) apiURL {
	if kind == auth.OAuth {
		return r.oauth
	}
	return r.cookie
}

// apiName names a request in the call stats: GraphQL endpoints are
// "<query id>/<operation>", counted by operation.
func (r apiReq) apiName() string {
	e := r.cookie.endpoint
	return e[strings.LastIndexByte(e, '/')+1:]
}

// encodeQuery encodes params in order, like Nim's encodeQuery: "=" is left
// out for empty values.
func encodeQuery(params []param) string {
	var b strings.Builder
	for i, p := range params {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(url.QueryEscape(p.key))
		if p.value != "" {
			b.WriteByte('=')
			b.WriteString(url.QueryEscape(p.value))
		}
	}
	return b.String()
}

// target is a request URL and its path.
type target struct {
	url  string
	path string
}

func (r apiReq) target(kind auth.Kind) target {
	u := r.forKind(kind)
	base, basePath := "https://x.com", "/i/api/"
	if kind == auth.OAuth {
		base, basePath = "https://api.x.com", "/"
	}
	prefix := "graphql/"
	if strings.HasPrefix(u.endpoint, "1.1/") || strings.HasPrefix(u.endpoint, "2/") {
		prefix = ""
	}
	path := basePath + prefix + u.endpoint
	full := base + path
	if q := encodeQuery(u.params); q != "" {
		full += "?" + q
	}
	return target{url: full, path: path}
}

func isRestV2(path string) bool {
	return strings.Contains(path, "/2/timeline/")
}

// percentEncode is the OAuth 1.0 encoding: everything but unreserved
// characters, with uppercase hex.
func percentEncode(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9',
			c == '-', c == '.', c == '_', c == '~':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0xf])
		}
	}
	return b.String()
}

// oauthHeader signs a GET of rawURL with HMAC-SHA1, as the Nim oauth 0.11
// package does: the URL's query is added to the signed parameters as it is,
// without decoding or re-encoding it.
func oauthHeader(rawURL, token, tokenSecret string, timestamp int64) string {
	encoded := strings.ReplaceAll(strings.ReplaceAll(rawURL, ",", "%2C"), "+", "%20")
	ts := strconv.FormatInt(timestamp, 10)
	params := [][2]string{
		{"oauth_consumer_key", consumerKey},
		{"oauth_nonce", "0"},
		{"oauth_signature_method", "HMAC-SHA1"},
		{"oauth_timestamp", ts},
		{"oauth_version", "1.0"},
		{"oauth_token", token},
	}
	for i, p := range params {
		params[i] = [2]string{percentEncode(p[0]), percentEncode(p[1])}
	}

	base, query, _ := strings.Cut(encoded, "?")
	if u, err := url.Parse(base); err == nil {
		base = u.Scheme + "://" + u.Hostname()
		if port := u.Port(); port != "" {
			base += ":" + port
		}
		base += u.EscapedPath()
	}
	for _, pair := range strings.Split(query, "&") {
		if k, v, ok := strings.Cut(pair, "="); ok {
			params = append(params, [2]string{k, v})
		}
	}
	slices.SortFunc(params, func(a, b [2]string) int {
		if c := strings.Compare(a[0], b[0]); c != 0 {
			return c
		}
		return strings.Compare(a[1], b[1])
	})
	joined := make([]string, len(params))
	for i, p := range params {
		joined[i] = p[0] + "=" + p[1]
	}

	baseString := "GET&" + percentEncode(base) + "&" + percentEncode(strings.Join(joined, "&"))
	mac := hmac.New(sha1.New, []byte(percentEncode(consumerSecret)+"&"+percentEncode(tokenSecret)))
	mac.Write([]byte(baseString))
	signature := percentEncode(base64.StdEncoding.EncodeToString(mac.Sum(nil)))

	h := `OAuth oauth_consumer_key="` + consumerKey + `", oauth_signature_method="HMAC-SHA1", oauth_timestamp="` +
		ts + `", oauth_nonce="0", oauth_signature="` + signature + `"`
	if token != "" {
		h += `, oauth_token="` + token + `"`
	}
	return h + `, oauth_version="1.0"`
}

func setBrowserHeaders(h http.Header) {
	h.Set("referer", "https://x.com/")
	h.Set("sec-ch-ua", secChUa)
	h.Set("sec-ch-ua-mobile", "?0")
	h.Set("sec-ch-ua-platform", "Windows")
	h.Set("sec-fetch-dest", "empty")
	h.Set("sec-fetch-mode", "cors")
	h.Set("sec-fetch-site", "same-origin")
}

func genHeaders(ctx context.Context, s *auth.Session, t target, skipTid bool) (http.Header, error) {
	h := http.Header{}
	h.Set("accept", "*/*")
	h.Set("accept-encoding", "gzip")
	h.Set("accept-language", "en-US,en;q=0.9")
	h.Set("content-type", "application/json")
	h.Set("origin", "https://x.com")
	h.Set("user-agent", userAgent)
	h.Set("x-twitter-active-user", "yes")
	h.Set("x-twitter-client-language", "en")
	h.Set("priority", "u=1, i")

	switch s.Kind {
	case auth.OAuth:
		timestamp := time.Now().Add(500 * time.Millisecond).Unix()
		h.Set("authorization", oauthHeader(t.url, s.OAuthToken, s.OAuthSecret, timestamp))
	case auth.Guest:
		h.Set("x-guest-token", s.GuestToken)
		setBrowserHeaders(h)
		h.Set("authorization", guestAuthorization)
		if !(disableTid || skipTid || strings.Contains(t.path, "/1.1/") || isRestV2(t.path)) {
			tid, err := genTid(ctx, t.path)
			if err != nil {
				return nil, err
			}
			h.Set("x-client-transaction-id", tid)
		}
		auth.RegisterGuestRequest(s)
	case auth.Cookie:
		h.Set("x-twitter-auth-type", "OAuth2Session")
		h.Set("x-csrf-token", s.Ct0)
		h.Set("cookie", "auth_token="+s.AuthToken+"; ct0="+s.Ct0)
		setBrowserHeaders(h)
		switch {
		case isRestV2(t.path):
			h.Set("authorization", bearerToken)
		case disableTid || skipTid || strings.Contains(t.path, "/1.1/"):
			h.Set("authorization", bearerToken2)
		default:
			h.Set("authorization", bearerToken)
			tid, err := genTid(ctx, t.path)
			if err != nil {
				return nil, err
			}
			h.Set("x-client-transaction-id", tid)
		}
	}
	return h, nil
}

// activateGuestToken activates a guest token, to be added to the guest pool.
func activateGuestToken(ctx context.Context) (*auth.Session, error) {
	h := http.Header{}
	h.Set("authorization", guestAuthorization)
	h.Set("content-type", "application/x-www-form-urlencoded")
	h.Set("accept", "*/*")
	h.Set("accept-language", "en-US,en;q=0.9")
	h.Set("origin", "https://x.com")
	h.Set("user-agent", userAgent)
	setBrowserHeaders(h)
	h.Set("x-twitter-active-user", "yes")
	h.Set("x-twitter-client-language", "en")

	resp, err := doRequest(ctx, http.MethodPost, guestActivateURL, h, "", apiTimeout)
	if err != nil {
		return nil, err
	}
	if resp.status != http.StatusOK {
		log.Printf("[sessions] guest activation failed, status: %d", resp.status)
		return nil, types.ErrRateLimited
	}
	if !gjson.Valid(resp.body) {
		return nil, errors.New("guest activation returned invalid JSON")
	}
	token := gjson.Get(resp.body, "guest_token")
	if !token.Exists() {
		log.Printf("[sessions] guest activation returned no token")
		return nil, types.ErrRateLimited
	}
	if getStr(token) == "" {
		log.Printf("[sessions] guest activation returned an empty token")
		return nil, types.ErrRateLimited
	}
	log.Printf("[sessions] activated new guest token")
	return auth.NewGuestSession(strings.Clone(token.Str)), nil
}

func getAndValidateSession(ctx context.Context, req apiReq) (*auth.Session, error) {
	if flags.GuestAuth {
		s, err := auth.GetReadyGuestSession(ctx, req.auth(), activateGuestToken)
		if err != nil {
			return nil, err
		}
		if s.GuestToken == "" {
			log.Printf("[sessions] Empty guest token, session: %s", s)
			auth.Release(s)
			return nil, types.ErrRateLimited
		}
		return s, nil
	}

	s, err := auth.GetSession(req.auth())
	if err != nil {
		return nil, err
	}
	switch {
	case s.Kind == auth.OAuth && s.OAuthToken == "":
		log.Printf("[sessions] Empty oauth token, session: %s", s)
	case s.Kind == auth.Cookie && (s.AuthToken == "" || s.Ct0 == ""):
		log.Printf("[sessions] Empty cookie credentials, session: %s", s)
	default:
		return s, nil
	}
	auth.Release(s)
	return nil, types.ErrRateLimited
}

func hasHeader(h http.Header, key string) bool {
	return len(h.Values(key)) > 0
}

func recordRateLimit(s *auth.Session, req apiReq, h http.Header) error {
	if cache := h.Get(npCache); cache == "HIT" || cache == "STALE" || !hasHeader(h, rlRemaining) {
		return nil
	}
	remaining, err := strconv.Atoi(h.Get(rlRemaining))
	if err != nil {
		return err
	}
	reset, err := strconv.ParseInt(h.Get(rlReset), 10, 64)
	if err != nil {
		return err
	}
	limit, err := strconv.Atoi(h.Get(rlLimit))
	if err != nil {
		return err
	}
	auth.SetRateLimit(s, req.auth(), remaining, reset, limit)
	return nil
}

// recordTranslationRateLimit records Grok's rate limit. A 429 cools down only
// the translation endpoint of s, until the reported reset or for retry-after
// seconds.
func recordTranslationRateLimit(s *auth.Session, req apiReq, status int, h http.Header) {
	if status != http.StatusTooManyRequests {
		_ = recordRateLimit(s, req, h)
		return
	}
	headerInt := func(name string, fallback int64) int64 {
		n, err := strconv.ParseInt(h.Get(name), 10, 64)
		if err != nil {
			return fallback
		}
		return n
	}
	now := time.Now().Unix()
	reset := headerInt(rlReset, 0)
	retryAt := now + max(1, headerInt("retry-after", translationRetrySeconds))
	if reset > now {
		retryAt = reset
	}
	auth.SetRateLimit(s, req.auth(), 0, retryAt, int(headerInt(rlLimit, 0)))
}

// errInvalidSession makes fetchOnce drop the session from the pool and fail
// with types.ErrRateLimited, so the request is retried with another.
var errInvalidSession = errors.New("invalid session")

// errorCodes returns the codes in an {"errors": [...]} response.
func errorCodes(body string) ([]types.ErrorCode, error) {
	if !gjson.Valid(body) {
		return nil, errors.New("invalid errors JSON")
	}
	var codes []types.ErrorCode
	for _, e := range items(gjson.Get(body, "errors")) {
		codes = append(codes, types.ErrorCode(e.Get("code").Int()))
	}
	return codes, nil
}

func containsAny(codes []types.ErrorCode, set ...types.ErrorCode) bool {
	for _, c := range codes {
		if slices.Contains(set, c) {
			return true
		}
	}
	return false
}

// isDialError reports a failure to reach the server at all, such as a
// refused connection, which retrying with another session can't fix. Like
// any other timeout, a connect timeout is retried.
func isDialError(err error) bool {
	var op *net.OpError
	return errors.As(err, &op) && (op.Op == "dial" || op.Op == "proxyconnect") && !errors.Is(err, errTimeout)
}

// get sends a GET, retrying once on a 503, which upstream treats as a bad
// client connection.
func get(ctx context.Context, url string, h http.Header) (*response, error) {
	var resp *response
	var err error
	for range 2 {
		resp, err = doRequest(ctx, http.MethodGet, url, h, "", apiTimeout)
		if err == nil && resp.status == http.StatusServiceUnavailable {
			err = fmt.Errorf("%w: 503 from %s", types.ErrBadClient, url)
			continue
		}
		break
	}
	return resp, err
}

// fetchOnce makes one request for req with s, which it releases, and
// passes the body to process. Failures that another session might not hit
// are returned as types.ErrRateLimited.
func fetchOnce(ctx context.Context, req apiReq, s *auth.Session,
	process func(body string) (string, error)) (body string, err error) {
	t := req.target(s.Kind)
	defer func() {
		switch {
		case err == nil, ctx.Err() != nil:
		case errors.Is(err, errInvalidSession):
			auth.Invalidate(s)
			s = nil
			err = types.ErrRateLimited
		case errors.Is(err, types.ErrInternal), errors.Is(err, types.ErrBadClient), isDialError(err):
		case !errors.Is(err, types.ErrRateLimited):
			log.Printf("error: %v, session: %s, url: %s", err, s, t.url)
			err = types.ErrRateLimited
		}
		if s != nil {
			auth.Release(s)
		}
	}()

	h, err := genHeaders(ctx, s, t, req.forKind(s.Kind).skipTid)
	if err != nil {
		return "", err
	}
	target := t.url
	if apiProxy != "" && !strings.Contains(t.path, "/1.1/") {
		target = strings.ReplaceAll(target, "https://", apiProxy)
	}
	resp, err := get(ctx, target, h)
	if err != nil {
		return "", err
	}
	body = resp.body
	if resp.status == http.StatusNotFound && body == "" {
		log.Printf("[sessions] transient 404 (empty body), retrying: %s, session: %s", t.path, s)
		return "", types.ErrRateLimited
	}

	if err := recordRateLimit(s, req, resp.header); err != nil {
		return "", fmt.Errorf("rate limit headers: %w", err)
	}

	if body != "" {
		switch {
		case isCloudflareHTML(body):
			log.Printf("[cloudflare] %d (%s), API: %s, session: %s", resp.status, cfTitle(body), t.path, s)
			return "", types.ErrRateLimited
		case strings.HasPrefix(body, `{"errors`):
			codes, err := errorCodes(body)
			if err != nil {
				return "", err
			}
			if !containsAny(codes, errorsToSkip...) {
				log.Printf("Fetch error, API: %s, errors: %v, session: %s", t.path, codes, s)
				switch {
				case containsAny(codes, types.ErrExpiredToken, types.ErrBadToken, types.ErrLocked):
					return "", errInvalidSession
				case containsAny(codes, types.ErrRateLimitedCode):
					// rate limit hit, resets after 24 hours
					auth.SetLimited(s, req.auth())
					if s.Kind == auth.Guest {
						return "", errInvalidSession
					}
					return "", types.ErrRateLimited
				}
			}
		case strings.HasPrefix(body, "429 Too Many Requests"):
			log.Printf("[sessions] 429 error, API: %s, session: %s", t.path, s)
			return "", types.ErrRateLimited
		}
	}

	if body, err = process(body); err != nil {
		return "", err
	}

	if resp.status == http.StatusBadRequest {
		log.Printf("ERROR 400, %s: %s, session: %s", t.path, body, s)
		return "", fmt.Errorf("%w: %s", types.ErrInternal, t.url)
	}
	return body, nil
}

// retry makes up to maxRetries attempts at req, each with a session from
// the pool, while they fail with types.ErrRateLimited.
func retry(ctx context.Context, req apiReq, process func(body string) (string, error)) (string, error) {
	name := req.apiName()
	for i := range maxRetries {
		s, err := getAndValidateSession(ctx, req)
		var body string
		if err == nil {
			body, err = fetchOnce(ctx, req, s, process)
			if err == nil {
				recordCall("sessions", name, "ok")
				return body, nil
			}
		}
		if errors.Is(err, types.ErrNoSessions) {
			recordCall("sessions", name, "limited")
			return "", err
		}
		if !errors.Is(err, types.ErrRateLimited) {
			recordCall("sessions", name, "error")
			return "", err
		}

		if s == nil {
			log.Printf("[sessions] Rate limited, retrying %s request (%d/%d)...", req.cookie.endpoint, i, maxRetries)
		} else {
			log.Printf("[sessions] Rate limited, retrying %s request (%d/%d)..., session: %s",
				req.auth().Endpoint(s), i, maxRetries, s)
		}
		if retryDelay > 0 {
			select {
			case <-time.After(retryDelay):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
	}
	recordCall("sessions", name, "limited")
	return "", types.ErrRateLimited
}

// getError reads the code of the first error when the response is an
// array of errors.
func getError(js gjson.Result) types.ErrorCode {
	arr := items(js)
	if len(arr) == 0 {
		return types.ErrNull
	}
	return types.ErrorCode(getInt(arr[0].Get("code")))
}

func isJSONStart(body string) bool {
	return strings.HasPrefix(body, "{") || strings.HasPrefix(body, "[")
}

// fetch requests req and parses the response. A response that isn't JSON
// parses as null.
func fetch(ctx context.Context, req apiReq) (gjson.Result, error) {
	body, err := retry(ctx, req, func(body string) (string, error) {
		if !isJSONStart(body) {
			log.Printf("non-JSON response: %s --- API: %s", body, req.cookie.endpoint)
			return "", nil
		}
		if !gjson.Valid(body) {
			return "", errors.New("invalid JSON response")
		}
		if code := getError(gjson.Parse(body)); !slices.Contains(errorsToSkip, code) {
			log.Printf("Fetch error, API: %s, error: %d", req.cookie.endpoint, code)
			if slices.Contains([]types.ErrorCode{types.ErrExpiredToken, types.ErrBadToken, types.ErrLocked}, code) {
				return "", errInvalidSession
			}
		}
		return body, nil
	})
	if err != nil {
		return gjson.Result{}, err
	}
	return gjson.Parse(body), nil
}

// fetchRaw requests req and returns the body, or "" if it isn't JSON.
func fetchRaw(ctx context.Context, req apiReq) (string, error) {
	return retry(ctx, req, func(body string) (string, error) {
		if !isJSONStart(body) {
			log.Printf("non-JSON response: %s --- API: %s", body, req.cookie.endpoint)
			return "", nil
		}
		return body, nil
	})
}

// fetchRawJSON is fetchRaw for callers that parse the result.
func fetchRawJSON(ctx context.Context, req apiReq) (gjson.Result, error) {
	body, err := fetchRaw(ctx, req)
	if err != nil {
		return gjson.Result{}, err
	}
	return gjson.Parse(body), nil
}

// PostGrokTranslation asks Grok to translate the post or community note id
// into dstLang. contentType is "POST" or "COMMUNITY_NOTE". It returns ""
// when Grok answers with an error status.
func PostGrokTranslation(ctx context.Context, id, dstLang, contentType string) (string, error) {
	if flags.GuestAuth {
		return "", fmt.Errorf("%w: translation requires cookie sessions", types.ErrInternal)
	}
	const endpoint = "2/grok/translation.json"
	req := apiReq{
		cookie: apiURL{endpoint: endpoint, skipTid: true},
		oauth:  apiURL{endpoint: endpoint, skipTid: true},
	}
	s, err := auth.GetCookieSession(req.auth())
	if err != nil {
		return "", err
	}
	defer auth.Release(s)

	h, err := genHeaders(ctx, s, target{url: grokTranslationURL, path: "/2/grok/translation.json"}, true)
	if err != nil {
		return "", err
	}
	h.Set("authorization", bearerToken)
	h.Set("content-type", "text/plain;charset=UTF-8")
	body := obj{{"content_type", contentType}, {"id", id}, {"dst_lang", dstLang}}.String()

	var resp *response
	for range 2 {
		resp, err = doRequest(ctx, http.MethodPost, grokTranslationURL, h, body, grokTranslationTimeout)
		if err == nil && resp.status == http.StatusServiceUnavailable {
			err = fmt.Errorf("%w: 503 from %s", types.ErrBadClient, grokTranslationURL)
			continue
		}
		break
	}
	if err != nil {
		return "", err
	}

	recordTranslationRateLimit(s, req, resp.status, resp.header)
	if resp.status != http.StatusOK {
		log.Printf("[translation] grok returned %d for post %s, session: %s", resp.status, id, s)
		return "", nil
	}
	return resp.body, nil
}
