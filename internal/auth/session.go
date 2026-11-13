// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

type Kind int

const (
	OAuth Kind = iota
	Cookie
	Guest
)

func (k Kind) String() string {
	switch k {
	case OAuth:
		return "oauth"
	case Cookie:
		return "cookie"
	default:
		return "guest"
	}
}

// RateLimit is the last rate limit Twitter reported for one endpoint.
type RateLimit struct {
	Limit     int
	Remaining int
	Reset     int64
}

// Session is an account (OAuth or cookie) or guest-token session. The
// credential fields are set once at creation and safe to read anywhere; the
// unexported bookkeeping fields are shared by concurrent requests and guarded
// by mu.
type Session struct {
	Kind     Kind
	ID       int64
	Username string

	OAuthToken  string
	OAuthSecret string

	AuthToken string
	Ct0       string

	GuestToken string

	pending   int
	limited   bool
	limitedAt int64
	apis      map[string]RateLimit
	// epoch seconds when this guest token was activated
	guestCreated int64
	// requests made with this guest token since activation
	guestReqs int
}

// NewGuestSession wraps a freshly activated guest token.
func NewGuestSession(token string) *Session {
	return &Session{Kind: Guest, GuestToken: token, guestCreated: time.Now().Unix()}
}

// Req names the endpoint a request uses for each session kind, which is what
// rate limits are tracked by.
type Req struct {
	OAuth  string
	Cookie string
}

// Endpoint is the endpoint req uses with s.
func (r Req) Endpoint(s *Session) string {
	if s.Kind == OAuth {
		return r.OAuth
	}
	return r.Cookie
}

// String identifies s in logs without its credentials.
func (s *Session) String() string {
	if s == nil {
		return "<null>"
	}
	if s.Kind == Guest {
		return "guest"
	}
	var name string
	switch {
	case s.ID > 0 && s.Username != "":
		name = strconv.FormatInt(s.ID, 10) + " (" + s.Username + ")"
	case s.Username != "":
		name = s.Username
	case s.ID > 0:
		name = strconv.FormatInt(s.ID, 10)
	default:
		name = "<unknown>"
	}
	return s.Kind.String() + " " + name
}

// field reads a sessions.jsonl key, which may be spelled in camelCase or
// snake_case.
func field(js gjson.Result, camel, snake string) string {
	if v := js.Get(camel); v.Exists() {
		return v.String()
	}
	return js.Get(snake).String()
}

// ParseSession parses one line of sessions.jsonl.
func ParseSession(line string) (*Session, error) {
	if !gjson.Valid(line) {
		return nil, fmt.Errorf("invalid session JSON: %q", line)
	}
	js := gjson.Parse(line)
	kind := js.Get("kind").String()
	if kind == "" {
		kind = "oauth"
	}
	username := field(js, "username", "username")

	switch kind {
	case "oauth":
		token := field(js, "oauthToken", "oauth_token")
		idStr, _, ok := strings.Cut(token, "-")
		if !ok {
			return nil, fmt.Errorf("oauth token has no user ID prefix")
		}
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("oauth token user ID: %w", err)
		}
		return &Session{
			Kind:        OAuth,
			ID:          id,
			Username:    username,
			OAuthToken:  token,
			OAuthSecret: field(js, "oauthTokenSecret", "oauth_token_secret"),
		}, nil
	case "cookie":
		var id int64
		if idStr := field(js, "id", "id"); idStr != "" {
			var err error
			if id, err = strconv.ParseInt(idStr, 10, 64); err != nil {
				return nil, fmt.Errorf("cookie session id: %w", err)
			}
		}
		return &Session{
			Kind:      Cookie,
			ID:        id,
			Username:  username,
			AuthToken: field(js, "authToken", "auth_token"),
			Ct0:       field(js, "ct0", "ct0"),
		}, nil
	}
	return nil, fmt.Errorf("unknown session kind: %s", kind)
}
