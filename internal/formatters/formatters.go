// SPDX-License-Identifier: AGPL-3.0-only

// Package formatters renders dates, durations, links and rewritten text
// for the views, RSS and parser.
package formatters

import (
	"fmt"
	"html"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/perennialtech/nitter/internal/config"
	"github.com/perennialtech/nitter/internal/prefs"
	"github.com/perennialtech/nitter/internal/query"
	"github.com/perennialtech/nitter/internal/types"
	"github.com/perennialtech/nitter/internal/utils"
)

const (
	cards = "cards.twitter.com/cards"
	tco   = "https://t.co"
)

var (
	// The Nim regexes these replace used lookbehind, which RE2 lacks: a host
	// is rewritten when it follows whitespace, or follows "https://" at the
	// start of the text or after whitespace.
	twHTTPSRegex = regexp.MustCompile(`(^|\s)https://(www\.|mobile\.)?twitter\.com`)
	twBareRegex  = regexp.MustCompile(`(\s)(www\.|mobile\.)?twitter\.com`)
	twLinkRegex  = regexp.MustCompile(`<a href="https://twitter.com([^"]+)">twitter\.com(\S+)</a>`)
	xHTTPSRegex  = regexp.MustCompile(`(^|\s)https://(www\.|mobile\.)?x\.com`)
	xBareRegex   = regexp.MustCompile(`(\s)(www\.|mobile\.)?x\.com`)
	xLinkRegex   = regexp.MustCompile(`<a href="https://x.com([^"]+)">x\.com(\S+)</a>`)

	ytRegex = regexp.MustCompile(`(?i)([A-z.]+\.)?youtu(be\.com|\.be)`)

	// Matches must not follow '.' or 'b'; replaceNotAfter checks that.
	// v.redd.it videos and i.redd.it images aren't replaced: frontends
	// can't serve them uniformly.
	rdRegex      = regexp.MustCompile(`((www|np|new|amp|old)\.)?reddit.com`)
	rdShortRegex = regexp.MustCompile(`redd\.it/`)

	wwwRegex          = regexp.MustCompile(`https?://(www[0-9]?\.)?`)
	m3u8Regex         = regexp.MustCompile(`url="(.+.m3u8)"`)
	userPicRegex      = regexp.MustCompile(`_(normal|bigger|mini|200x200|400x400)(\.[A-z]+)$`)
	extRegex          = regexp.MustCompile(`(\.[A-z]+)$`)
	birdwatchURLRegex = regexp.MustCompile(`https?://[^\s<>"]+`)
	tcoHrefRegex      = regexp.MustCompile(`"https?://t\.co/(\w+)"`)
)

// Shorten truncates text to at most length bytes, adding an ellipsis.
func Shorten(text string, length int) string {
	if len(text) <= length {
		return text
	}
	cut := length
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "…"
}

func ShortLink(text string) string {
	return Shorten(wwwRegex.ReplaceAllString(text, ""), 28)
}

// StripHTML returns the text of an HTML fragment, with each link's text
// replaced by its URL (shortened if shorten is set).
func StripHTML(text string, shorten bool) string {
	nodes, err := xhtml.ParseFragment(strings.NewReader(text), &xhtml.Node{
		Type: xhtml.ElementNode, Data: "div", DataAtom: atom.Div,
	})
	if err != nil {
		return text
	}
	var b strings.Builder
	var walk func(n *xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode && n.Data == "a" {
			href := attr(n, "href")
			if strings.Contains(href, "http") && n.FirstChild != nil {
				if shorten {
					b.WriteString(ShortLink(href))
				} else {
					b.WriteString(href)
				}
				for c := n.FirstChild.NextSibling; c != nil; c = c.NextSibling {
					walk(c)
				}
				return
			}
		}
		if n.Type == xhtml.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for _, n := range nodes {
		walk(n)
	}
	return b.String()
}

func attr(n *xhtml.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// SanitizeXML drops characters that aren't allowed in XML 1.0.
func SanitizeXML(text string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == 0x09 || r == 0x0A || r == 0x0D,
			r >= 0x20 && r <= 0xD7FF,
			r >= 0xE000 && r <= 0xFFFD,
			r >= 0x10000 && r <= 0x10FFFF:
			return r
		}
		return -1
	}, text)
}

// replaceNotAfter replaces matches of re that aren't preceded by any byte
// in notAfter.
func replaceNotAfter(s string, re *regexp.Regexp, notAfter, repl string) string {
	var b strings.Builder
	last, pos := 0, 0
	for pos <= len(s) {
		loc := re.FindStringIndex(s[pos:])
		if loc == nil {
			break
		}
		start, end := pos+loc[0], pos+loc[1]
		if start > 0 && strings.IndexByte(notAfter, s[start-1]) >= 0 {
			pos = start + 1
			continue
		}
		b.WriteString(s[last:start])
		b.WriteString(repl)
		last, pos = end, end
		if end == start {
			pos++
		}
	}
	b.WriteString(s[last:])
	return b.String()
}

// ReplaceURLs rewrites links to the visitor's chosen alternative frontends.
// absolute, if set, is prefixed to root-relative hrefs.
func ReplaceURLs(body string, p *prefs.Prefs, absolute string) string {
	result := body

	if p.ReplaceYouTube != "" && strings.Contains(result, "youtu") {
		host := strings.Trim(p.ReplaceYouTube, "/")
		result = ytRegex.ReplaceAllLiteralString(result, host)
	}

	if p.ReplaceTwitter != "" {
		host := strings.Trim(p.ReplaceTwitter, "/")
		hostRepl := strings.ReplaceAll(host, "$", "$$")
		if strings.Contains(result, tco) {
			result = strings.ReplaceAll(result, tco, utils.HTTPS+host+"/t.co")
		}
		if strings.Contains(result, "x.com") {
			result = xHTTPSRegex.ReplaceAllString(result, "${1}https://"+hostRepl)
			result = xBareRegex.ReplaceAllString(result, "${1}"+hostRepl)
			result = xLinkRegex.ReplaceAllString(result,
				`<a href="https://`+hostRepl+`$1">`+hostRepl+`$2</a>`)
		}
		if strings.Contains(result, "twitter.com") {
			result = strings.ReplaceAll(result, cards, host+"/cards")
			result = twHTTPSRegex.ReplaceAllString(result, "${1}https://"+hostRepl)
			result = twBareRegex.ReplaceAllString(result, "${1}"+hostRepl)
			result = twLinkRegex.ReplaceAllString(result,
				`<a href="https://`+hostRepl+`$1">`+hostRepl+`$2</a>`)
		}
	}

	if p.ReplaceReddit != "" && (strings.Contains(result, "reddit.com") || strings.Contains(result, "redd.it")) {
		host := strings.Trim(p.ReplaceReddit, "/")
		result = replaceNotAfter(result, rdShortRegex, ".b", host+"/comments/")
		result = replaceNotAfter(result, rdRegex, ".b", host)
		if strings.Contains(result, host) && strings.Contains(result, "/gallery/") {
			result = strings.ReplaceAll(result, "/gallery/", "/comments/")
		}
	}

	if absolute != "" && strings.Contains(result, "href") {
		result = strings.ReplaceAll(result, `href="/`, `href="`+absolute+"/")
	}
	return result
}

// LinkifyBirdwatchURLs escapes a community note's plain text and turns its
// URLs into links, localizing Twitter and t.co links.
func LinkifyBirdwatchURLs(text string) string {
	var b strings.Builder
	pos := 0
	for pos < len(text) {
		loc := birdwatchURLRegex.FindStringIndex(text[pos:])
		if loc == nil {
			break
		}
		first, last := pos+loc[0], pos+loc[1]
		for last > first+1 && strings.IndexByte(".,;:!?)", text[last-1]) >= 0 {
			last--
		}
		u := text[first:last]
		b.WriteString(html.EscapeString(text[pos:first]))
		b.WriteString(`<a href="` + html.EscapeString(utils.LocalizeTwitterLink(u)) + `">` +
			html.EscapeString(u) + "</a>")
		pos = last
	}
	if pos < len(text) {
		b.WriteString(html.EscapeString(text[pos:]))
	}
	return tcoHrefRegex.ReplaceAllString(b.String(), `"/t.co/$1"`)
}

func M3u8URL(content string) string {
	if m := m3u8Regex.FindStringSubmatch(content); m != nil {
		return m[1]
	}
	return ""
}

// ProxifyVideo rewrites the segment and playlist URLs in an HLS manifest to
// absolute URLs, signed for the video proxy if proxy is set. Relative URLs
// resolve against manifestURL.
func ProxifyVideo(manifest string, proxy bool, manifestURL string) string {
	baseURL, basePath := "https://video.twimg.com", ""
	if manifestURL != "" {
		u, err := url.Parse(manifestURL)
		if err == nil {
			baseURL = u.Scheme + "://" + u.Hostname()
		}
		if idx := strings.LastIndexByte(manifestURL, '/'); idx > 8 {
			basePath = manifestURL[:idx+1]
		} else {
			basePath = ""
		}
	}

	var pairs []string
	for _, line := range strings.Split(manifest, "\n") {
		line = strings.TrimSuffix(line, "\r")
		u := line
		switch {
		case strings.HasPrefix(line, "#EXT-X-MAP:URI"):
			if len(line) >= 17 {
				u = line[16 : len(line)-1]
			}
		case strings.HasPrefix(line, "#EXT-X-MEDIA") && strings.Contains(line, "URI="):
			start := strings.Index(line, "URI=") + 5
			if end := strings.IndexByte(line[min(start, len(line)):], '"'); end >= 0 {
				u = line[start : start+end]
			}
		}
		var resolved string
		switch {
		case strings.HasPrefix(u, "/"):
			resolved = baseURL + u
		case basePath != "" && u != "" && !strings.HasPrefix(u, "#") &&
			!strings.HasPrefix(u, "http") && strings.Contains(u, "."):
			resolved = basePath + u
		}
		if resolved != "" {
			if proxy {
				resolved = utils.VidURL(resolved)
			}
			pairs = append(pairs, u, resolved)
		}
	}
	if len(pairs) == 0 {
		return manifest
	}
	return strings.NewReplacer(pairs...).Replace(manifest)
}

// UserPic returns the full-size profile picture URL with style (such as
// "_400x400" or "_mini") inserted before the extension.
func UserPic(userPic, style string) string {
	s := userPicRegex.ReplaceAllString(userPic, "$2")
	return extRegex.ReplaceAllString(s, strings.ReplaceAll(style, "$", "$$")+"$1")
}

func VideoEmbed(cfg *config.Config, id int64) string {
	return fmt.Sprintf("%s/i/videos/tweet/%d", cfg.URLPrefix(), id)
}

func UserPageTitle(u types.User) string {
	return fmt.Sprintf("%s (@%s)", u.Fullname, u.Username)
}

func TweetPageTitle(t *types.Tweet) string {
	return UserPageTitle(t.User) + ": \"" + StripHTML(t.Text, false) + "\""
}

func UserPageDesc(u types.User) string {
	if u.Bio != "" {
		return StripHTML(u.Bio, false)
	}
	return "The latest tweets from " + u.Fullname
}

func JoinDate(u types.User) string {
	if u.JoinDate.IsZero() {
		return ""
	}
	return "Joined " + u.JoinDate.Format("January 2006")
}

func JoinDateFull(u types.User) string {
	if u.JoinDate.IsZero() {
		return ""
	}
	return u.JoinDate.Format("3:04 PM - 2 Jan 2006")
}

func Time(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("Jan 2, 2006 · 3:04 PM") + " UTC"
}

func Rfc822Time(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("Mon, 02 Jan 2006 15:04:05") + " GMT"
}

func ShortTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	now := time.Now().UTC()
	since := now.Sub(t)
	switch {
	case now.Year() != t.Year():
		return t.Format("2 Jan 2006")
	case since >= 24*time.Hour:
		return t.Format("Jan 2")
	case since >= time.Hour:
		return strconv.Itoa(int(since.Hours())) + "h"
	case since >= time.Minute:
		return strconv.Itoa(int(since.Minutes())) + "m"
	case since > time.Second:
		return strconv.Itoa(int(since.Seconds())) + "s"
	}
	return "now"
}

func Duration(ms int) string {
	sec := int(math.Round(float64(ms) / 1000))
	mins := sec / 60
	hour := mins / 60
	if hour > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hour, mins%60, sec%60)
	}
	return fmt.Sprintf("%d:%02d", mins%60, sec%60)
}

// Link returns the path of a tweet. focus adds the #m anchor that scrolls
// to the main tweet.
func Link(id int64, username string, focus bool) string {
	if username == "" {
		username = "i"
	}
	s := fmt.Sprintf("/%s/status/%d", username, id)
	if focus {
		s += "#m"
	}
	return s
}

func TweetLink(t *types.Tweet, focus bool) string {
	if t.ID == 0 {
		return ""
	}
	return Link(t.ID, t.User.Username, focus)
}

// TwitterLink returns the x.com URL of the page at path, used for the
// "Open in Twitter" link.
func TwitterLink(path string, params url.Values) string {
	username := params.Get("name")
	q := query.Init(params, username)
	if strings.Contains(username, ",") {
		q.FromUser = strings.Split(username, ",")
		path = "/search"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if !strings.Contains(path, "/search") && len(q.FromUser) < 2 {
		return "https://x.com" + path
	}
	f := "live"
	if q.Kind == types.QueryUsers {
		f = "user"
	}
	result := "https://x.com" + path + "?f=" + f + "&q=" + utils.EncodeURL(query.Param(q, "")) +
		"&src=typed_query"
	if username != "" {
		result = strings.ReplaceAll(result, "/"+username, "")
	}
	return result
}

// Location splits a "name:placeID" location into its display name and a
// search URL for the place.
func Location(location string) (string, string) {
	if strings.Contains(location, "://") {
		return location, ""
	}
	name, place, ok := strings.Cut(location, ":")
	if !ok {
		return name, ""
	}
	place, _, _ = strings.Cut(place, ":")
	return name, "/search?f=tweets&q=place:" + place
}

func Suspended(username string) string {
	return "User \"" + username + "\" has been suspended"
}

// Titleize uppercases ASCII letters at the start of each word.
func Titleize(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' && (i == 0 || s[i-1] == ' ' || s[i-1] == '(') {
			b[i] = c - 'a' + 'A'
		}
	}
	return string(b)
}
