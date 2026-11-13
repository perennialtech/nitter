// SPDX-License-Identifier: AGPL-3.0-only

// Package routes serves nitter's pages, feeds, embeds and media proxy.
package routes

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/perennialtech/nitter/internal/config"
	"github.com/perennialtech/nitter/internal/prefs"
)

// maxBody limits POST bodies; the only forms are the settings pages.
const maxBody = 64 << 10

// errPass makes the router try the next matching route, like Jester's cond.
var errPass = errors.New("pass")

type handler func(c *ctx) error

type route struct {
	method  string
	pattern *regexp.Regexp
	handle  handler
}

// ctx is one request's state while it's routed.
type ctx struct {
	s      *Server
	w      http.ResponseWriter
	r      *http.Request
	params map[string]string
	query  url.Values
	p      *prefs.Prefs
}

// param returns a path parameter, falling back to the query string and
// then the form body, like Jester's @"name".
func (c *ctx) param(name string) string {
	if v, ok := c.params[name]; ok {
		return v
	}
	if c.query.Has(name) {
		return c.query.Get(name)
	}
	if c.r.Method == http.MethodPost {
		return c.r.PostFormValue(name)
	}
	return ""
}

func (c *ctx) prefs() *prefs.Prefs {
	if c.p == nil {
		c.p = c.s.prefs(c.r)
	}
	return c.p
}

// compilePattern turns a Jester route pattern into a regexp: "@name"
// captures a path segment and "?" makes the preceding element optional.
func compilePattern(p string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(p); i++ {
		switch c := p[i]; c {
		case '@':
			j := i + 1
			for j < len(p) && (isAlnum(p[j]) || p[j] == '_') {
				j++
			}
			b.WriteString("(?P<" + p[i+1:j] + ">[^/]+)")
			i = j - 1
		case '?':
			b.WriteByte('?')
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

type Server struct {
	cfg    *config.Config
	routes []route
	themes []string
}

func (s *Server) get(pattern string, h handler)  { s.add(http.MethodGet, pattern, h) }
func (s *Server) post(pattern string, h handler) { s.add(http.MethodPost, pattern, h) }

func (s *Server) add(method, pattern string, h handler) {
	s.routes = append(s.routes, route{method, compilePattern(pattern), h})
}

// getRe adds a GET route matched by a regular expression.
func (s *Server) getRe(re string, h handler) {
	s.routes = append(s.routes, route{http.MethodGet, regexp.MustCompile(re), h})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.EscapedPath()
	if p == "" || p[0] != '/' {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if s.serveStatic(w, r) {
		return
	}
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	}
	c := &ctx{s: s, w: w, r: r, query: r.URL.Query()}
	// Paths with a dot are files or proxied media, which never carry prefs.
	if !strings.Contains(p, ".") || p == "/embed/Tweet.html" {
		if c.applyURLPrefs() {
			return
		}
	}
	for _, rt := range s.routes {
		if rt.method != r.Method && !(rt.method == http.MethodGet && r.Method == http.MethodHead) {
			continue
		}
		m := rt.pattern.FindStringSubmatch(p)
		if m == nil {
			continue
		}
		c.params = map[string]string{}
		for i, name := range rt.pattern.SubexpNames() {
			if name != "" {
				c.params[name] = m[i]
				r.SetPathValue(name, m[i])
			}
		}
		err := rt.handle(c)
		if errors.Is(err, errPass) {
			continue
		}
		if err != nil {
			s.handleError(c, err)
		}
		return
	}
	s.notFound(w, r)
}

// serveStatic serves files from staticDir, which take precedence over routes.
func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	clean := path.Clean(r.URL.Path)
	if clean == "/" {
		return false
	}
	file := filepath.Join(s.cfg.StaticDir, filepath.FromSlash(clean))
	info, err := os.Stat(file)
	if err != nil || info.IsDir() {
		return false
	}
	http.ServeFile(w, r, file)
	return true
}

// savePref sets a preference cookie. An expiring cookie is only sent if
// the visitor has one to remove.
func (c *ctx) savePref(name, value string, expire bool) {
	if expire {
		if _, err := c.r.Cookie(name); err != nil {
			return
		}
	}
	days := 360
	if expire {
		days = -10
	}
	sameSite := http.SameSiteLaxMode
	if c.s.cfg.UseHTTPS {
		sameSite = http.SameSiteNoneMode
	}
	http.SetCookie(c.w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		Expires:  time.Now().AddDate(0, 0, days),
		HttpOnly: true,
		Secure:   c.s.cfg.UseHTTPS,
		SameSite: sameSite,
	})
}

// applyURLPrefs stores the preferences in a ?prefs= link as cookies and
// redirects to the URL without it. It reports whether it responded.
func (c *ctx) applyURLPrefs() bool {
	raw := c.query.Get("prefs")
	if raw == "" {
		return false
	}
	values := map[string]string{}
	for _, pair := range strings.Split(raw, ",") {
		k, v, _ := strings.Cut(pair, "=")
		if k != "" {
			values[k] = v
		}
	}
	for _, pref := range prefs.All() {
		if v, ok := values[pref.Name]; ok {
			c.savePref(pref.Name, v, false)
		} else {
			c.savePref(pref.Name, "", true)
		}
	}
	q := c.r.URL.Query()
	q.Del("prefs")
	target := c.r.URL.EscapedPath()
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	c.redirect(target)
	return true
}

func (c *ctx) redirect(target string) error {
	http.Redirect(c.w, c.r, target, http.StatusSeeOther)
	return nil
}

// prefs reads the visitor's preferences from cookies and query parameters.
func (s *Server) prefs(r *http.Request) *prefs.Prefs {
	cookies := map[string]string{}
	for _, ck := range r.Cookies() {
		cookies[ck.Name] = ck.Value
	}
	params := map[string]string{}
	for k, vs := range r.URL.Query() {
		params[k] = vs[0]
	}
	p := prefs.Parse(cookies, params)
	return &p
}

// cursor returns the pagination cursor, also accepted as max_position.
func (c *ctx) cursor() string {
	if v := c.param("cursor"); v != "" {
		return v
	}
	return c.param("max_position")
}

// fullPath is the request path with nitter's own parameters dropped.
func (c *ctx) fullPath() string {
	q := c.r.URL.Query()
	for k, vs := range q {
		if len(vs) == 0 || vs[0] == "" || isNitterParam(k) {
			q.Del(k)
		}
	}
	if len(q) == 0 {
		return c.r.URL.EscapedPath()
	}
	return c.r.URL.EscapedPath() + "?" + q.Encode()
}

func isNitterParam(k string) bool {
	switch k {
	case "name", "tab", "id", "list", "referer", "scroll", "prefs":
		return true
	}
	return false
}

func (c *ctx) refPath() string {
	if v := c.param("referer"); v != "" {
		return v
	}
	return "/"
}

func names(name string) []string {
	var out []string
	for _, n := range strings.Split(strings.Trim(name, "/"), ",") {
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

func allIn(s string, ok func(byte) bool) bool {
	for i := 0; i < len(s); i++ {
		if !ok(s[i]) {
			return false
		}
	}
	return true
}

func isUsernameChar(c byte) bool { return isAlnum(c) || c == '_' }
func isDigit(c byte) bool        { return c >= '0' && c <= '9' }

// isTweetID reports whether s is a valid tweet or note ID.
func isTweetID(s string) bool {
	return len(s) >= 1 && len(s) <= 19 && allIn(s, isDigit)
}
