// SPDX-License-Identifier: AGPL-3.0-only

package parser

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"

	"github.com/perennialtech/nitter/internal/types"
	"github.com/perennialtech/nitter/internal/utils"
)

// The helpers below mirror packedjson's accessors: a missing key, or a key
// looked up on something that isn't an object, yields null, and typed
// getters return their default unless the value has exactly that JSON type.

// get follows a dot-separated key path (Nim's js{"a", "b"}). None of the keys
// used by the parser contain gjson path syntax, so they need no escaping.
func get(r gjson.Result, path string) gjson.Result {
	for {
		if !r.IsObject() {
			return gjson.Result{}
		}
		key, rest, more := strings.Cut(path, ".")
		r = r.Get(key)
		if !more {
			return r
		}
		path = rest
	}
}

// idx returns array element i, or null (Nim's js{i}).
func idx(r gjson.Result, i int) gjson.Result {
	if !r.IsArray() {
		return gjson.Result{}
	}
	return r.Get(strconv.Itoa(i))
}

func isNull(r gjson.Result) bool  { return r.Type == gjson.Null }
func notNull(r gjson.Result) bool { return r.Type != gjson.Null }

// sel returns the first non-null value, or the last one (Nim's select).
func sel(rs ...gjson.Result) gjson.Result {
	for _, r := range rs[:len(rs)-1] {
		if notNull(r) {
			return r
		}
	}
	return rs[len(rs)-1]
}

// has reports whether an object has key, even with a null value (Nim's in).
func has(r gjson.Result, key string) bool {
	return r.IsObject() && r.Get(key).Exists()
}

// arr returns the elements of an array, and nothing for any other value.
func arr(r gjson.Result) []gjson.Result {
	if !r.IsArray() {
		return nil
	}
	return r.Array()
}

// length counts array elements or object keys (packedjson's len).
func length(r gjson.Result) int {
	if !r.IsArray() && !r.IsObject() {
		return 0
	}
	n := 0
	r.ForEach(func(_, _ gjson.Result) bool { n++; return true })
	return n
}

func str(r gjson.Result) string { return strOr(r, "") }

func strOr(r gjson.Result, def string) string {
	if r.Type == gjson.String {
		return r.Str
	}
	return def
}

func getInt(r gjson.Result) int { return intOr(r, 0) }

// intOr returns integer JSON numbers; floats count as a different kind.
func intOr(r gjson.Result, def int) int {
	if r.Type != gjson.Number || strings.ContainsAny(r.Raw, ".eE") {
		return def
	}
	n, err := strconv.ParseInt(r.Raw, 10, 64)
	if err != nil {
		return def
	}
	return int(n)
}

func getBool(r gjson.Result) bool { return boolOr(r, false) }

func boolOr(r gjson.Result, def bool) bool {
	switch r.Type {
	case gjson.True:
		return true
	case gjson.False:
		return false
	}
	return def
}

func typeName(r gjson.Result) string {
	return strOr(get(r, "__typename"), str(get(r, "type")))
}

func entryID(e gjson.Result) string {
	return strOr(get(e, "entryId"), str(get(e, "entry_id")))
}

func getTweetResult(js gjson.Result, root string) gjson.Result {
	r := get(js, root)
	return sel(
		get(r, "content.tweet_results.result"),
		get(r, "itemContent.tweet_results.result"),
		get(r, "content.tweetResult.result"),
	)
}

// nimParseInt behaves like Nim's strutils.parseBiggestInt: an optional sign,
// digits with ignored underscores after them, and nothing else.
func nimParseInt(s string) (int64, bool) {
	i := 0
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	if i >= len(s) || s[i] < '0' || s[i] > '9' {
		return 0, false
	}
	// Accumulate negatively like Nim so math.MinInt64 parses.
	var b int64
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		c := int64(s[i] - '0')
		if b < (math.MinInt64+c)/10 {
			return 0, false
		}
		b = b*10 - c
		i++
		for i < len(s) && s[i] == '_' {
			i++
		}
	}
	if i != len(s) {
		return 0, false
	}
	if !neg {
		if b == math.MinInt64 {
			return 0, false
		}
		b = -b
	}
	return b, true
}

// ParseID returns the number after the last '-' of an entry ID such as
// "tweet-123", or the whole string's value, and 0 if it isn't a number.
func ParseID(id string) int64 {
	if start := strings.LastIndexByte(id, '-'); start >= 0 {
		id = id[start+1:]
	}
	n, _ := nimParseInt(id)
	return n
}

// GetID reads an ID given as a JSON string or integer.
func GetID(js gjson.Result) int64 {
	switch js.Type {
	case gjson.String:
		return ParseID(js.Str)
	case gjson.Number:
		return int64(intOr(js, 0))
	}
	return 0
}

// parseTimeLayout parses s only if it has the expected length, like Nim's
// parseTime template. Nim raises on a malformed value of the right length;
// that is returned as an unset time here.
func parseTimeLayout(s, layout string, n int) time.Time {
	if len(s) != n {
		return time.Time{}
	}
	t, err := time.Parse(layout, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

const (
	isoLayout     = "2006-01-02T15:04:05Z"
	twitterLayout = "Mon Jan 02 15:04:05 +0000 2006"
)

func parseIsoDate(s string) time.Time     { return parseTimeLayout(s, isoLayout, 20) }
func parseTwitterDate(s string) time.Time { return parseTimeLayout(s, twitterLayout, 30) }

func getDateTime(js gjson.Result) time.Time { return parseIsoDate(str(js)) }

// GetTime parses Twitter's "Wed Dec 19 20:20:32 +0000 2007" dates.
func GetTime(js gjson.Result) time.Time { return parseTwitterDate(str(js)) }

func fromUnixMs(ms int64) time.Time { return time.Unix(ms/1000, 0).UTC() }

func getTimeFromMs(js gjson.Result) time.Time {
	ms := intOr(js, 0)
	if ms == 0 {
		return time.Time{}
	}
	return fromUnixMs(int64(ms))
}

func getTimeFromMsStr(js gjson.Result) time.Time {
	ms, ok := nimParseInt(strOr(js, "0"))
	if !ok || ms == 0 {
		return time.Time{}
	}
	return fromUnixMs(ms)
}

func getStrVal(js gjson.Result, def string) string {
	return strOr(get(js, "string_value"), def)
}

// imageStr strips the scheme and then the pbs.twimg.com host.
func imageStr(s string) string {
	s = strings.TrimPrefix(s, utils.HTTPS)
	return strings.TrimPrefix(s, utils.Twimg)
}

// expImageURL is the experimental parsers' getImageUrl, which strips the
// host before the scheme and so only ever removes the scheme from a full
// https://pbs.twimg.com/ URL.
func expImageURL(s string) string {
	s = strings.TrimPrefix(s, utils.Twimg)
	return strings.TrimPrefix(s, utils.HTTPS)
}

func getImageStr(js gjson.Result) string { return imageStr(str(js)) }

func getImageVal(js gjson.Result) string { return getImageStr(get(js, "image_value.url")) }

func getExpandedURL(js gjson.Result, fallback string) string {
	return strOr(get(js, "expanded_url"), strOr(get(js, "url"), fallback))
}

func getCardURL(js gjson.Result, kind types.CardKind) string {
	result := getStrVal(get(js, "website_url"), "")
	if kind == types.CardPromoVideoConvo {
		result = getStrVal(get(js, "thank_you_url"), result)
	}
	if strings.HasPrefix(result, "card://") {
		result = ""
	}
	return result
}

func getCardDomain(js gjson.Result, kind types.CardKind) string {
	result := getStrVal(get(js, "vanity_url"), str(get(js, "domain")))
	if kind == types.CardPromoVideoConvo {
		result = getStrVal(get(js, "thank_you_vanity_url"), result)
	}
	return result
}

func getCardTitle(js gjson.Result, kind types.CardKind) string {
	result := getStrVal(get(js, "title"), "")
	switch kind {
	case types.CardPromoVideoConvo:
		result = getStrVal(get(js, "thank_you_text"), result)
	case types.CardLiveEvent:
		result = getStrVal(get(js, "event_category"), "")
	case types.CardVideoDirectMessage, types.CardImageDirectMessage:
		result = getStrVal(get(js, "cta1"), "")
	}
	return result
}

func getBanner(js gjson.Result) string {
	if url := getImageStr(get(js, "profile_banner_url")); url != "" {
		return url + "/1500x500"
	}
	if color := str(get(js, "profile_link_color")); color != "" {
		return "#" + color
	}
	// use primary color from profile picture color histogram
	if p := get(js, "profile_image_extensions.mediaColor.r.ok.palette"); notNull(p) && length(p) > 0 {
		pal := get(idx(p, 0), "rgb")
		return "#" + nimToHex(getInt(get(pal, "red"))) +
			nimToHex(getInt(get(pal, "green"))) + nimToHex(getInt(get(pal, "blue")))
	}
	return ""
}

// nimToHex is Nim's toHex(x, 2): the low byte as two upper-case digits.
func nimToHex(x int) string {
	const digits = "0123456789ABCDEF"
	return string([]byte{digits[(x>>4)&0xF], digits[x&0xF]})
}

func getTombstone(js gjson.Result) string {
	return strings.TrimSuffix(str(get(js, "text")), " Learn more")
}

// GetMp4Resolution parses the height out of a URL like
// https://video.twimg.com/ext_tw_video/<id>/pu/vid/720x1280/<random>.mp4,
// returning 0 when it can't (e.g. m3u8/non-mp4 video).
func GetMp4Resolution(url string) int {
	const vidSep = "/vid/"
	vidIdx := strings.Index(url, vidSep) + len(vidSep)
	resIdx := 0
	if vidIdx <= len(url) {
		if i := strings.IndexByte(url[vidIdx:], 'x'); i >= 0 {
			resIdx = vidIdx + i + 1
		}
	}
	end := strings.IndexByte(url[resIdx:], '/')
	if end < 0 {
		// Nim raises a range defect here; there's no resolution either way.
		return 0
	}
	n, ok := nimParseInt(url[resIdx : resIdx+end])
	if !ok {
		return 0
	}
	return int(n)
}

// nimNormalize is Nim's nimIdentNormalize, which parseEnum compares with:
// the first character as is, then the rest lower-cased without underscores.
func nimNormalize(s string) string {
	if s == "" {
		return s
	}
	b := []byte{s[0]}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c == '_' {
			continue
		}
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b = append(b, c)
	}
	return string(b)
}

func parseEnum[T ~string](s string, values []T, def T) T {
	n := nimNormalize(s)
	for _, v := range values {
		if nimNormalize(string(v)) == n {
			return v
		}
	}
	return def
}

var verifiedTypes = []types.VerifiedType{
	types.VerifiedNone, types.VerifiedBlue, types.VerifiedBusiness, types.VerifiedGovernment,
}

var cardKinds = []types.CardKind{
	types.CardAmplify, types.CardApp, types.CardAppPlayer, types.CardPlayer,
	types.CardSummary, types.CardSummaryLarge, types.CardPromoWebsite,
	types.CardPromoVideo, types.CardPromoVideoConvo, types.CardPromoImageConvo,
	types.CardPromoImageApp, types.CardStoreLink, types.CardLiveEvent,
	types.CardBroadcast, types.CardPeriscope, types.CardUnified, types.CardMoment,
	types.CardMessageMe, types.CardVideoDirectMessage, types.CardImageDirectMessage,
	types.CardAudiospace, types.CardNewsletterPublication, types.CardJobDetails,
	types.CardHidden, types.CardUnknown,
}

var videoTypes = []types.VideoType{types.VideoM3u8, types.VideoMp4, types.VideoVmap}

// parseVideoType maps a content type to its VideoType. Nim raises on unknown
// types; the raw value is kept instead.
func parseVideoType(s string) types.VideoType {
	return parseEnum(s, videoTypes, types.VideoType(s))
}

// nimURI holds the parts of Nim's lenient uri.parseUri the parser relies on.
// Go's net/url rejects some URLs Nim accepts and decodes the path, so the
// relevant subset is reimplemented.
type nimURI struct {
	scheme, hostname, path string
}

func parseNimURI(uri string) nimURI {
	var u nimURI
	i := 0
	doubleSlash := len(uri) > 1 && uri[0] == '/' && uri[1] == '/'
	if i < len(uri) && uri[i] == '/' && !doubleSlash {
		u.parsePath(uri, i)
		return u
	}
	for i < len(uri) && isSchemeChar(uri[i]) {
		i++
	}
	u.scheme = uri[:i]
	if (i >= len(uri) || uri[i] != ':') && !doubleSlash {
		u.scheme = ""
		u.parsePath(uri, 0)
		return u
	}
	if !doubleSlash {
		i++
	}
	if i+1 < len(uri) && uri[i] == '/' && uri[i+1] == '/' {
		i += 2
		end := i
		for end < len(uri) && uri[end] != '/' && uri[end] != '?' && uri[end] != '#' {
			end++
		}
		u.hostname = authorityHost(uri[i:end])
		i = end
	}
	u.parsePath(uri, i)
	return u
}

func (u *nimURI) parsePath(uri string, i int) {
	end := i
	for end < len(uri) && uri[end] != '?' && uri[end] != '#' {
		end++
	}
	u.path = uri[i:end]
	if strings.EqualFold(u.scheme, "mailto") {
		u.hostname = authorityHost(u.path)
		u.path = ""
	}
}

func isSchemeChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		c == '+' || c == '-' || c == '.'
}

// authorityHost follows Nim's parseAuthority to find the hostname.
func authorityHost(authority string) string {
	var host []byte
	inPort, inIPv6 := false, false
	for i := 0; i < len(authority); i++ {
		switch c := authority[i]; c {
		case '@':
			host = host[:0]
			inPort = false
		case ':':
			if inIPv6 {
				host = append(host, c)
			} else {
				inPort = true
			}
		case '[':
			inIPv6 = true
		case ']':
			inIPv6 = false
		default:
			if !inPort {
				host = append(host, c)
			}
		}
	}
	return string(host)
}

// nimSort is Nim's algorithm.sort, a bottom-up merge sort. It's reproduced
// because the entity comparator (start of one slice against the end of the
// other) is inconsistent for overlapping slices, so the resulting order
// depends on the algorithm.
func nimSort[T any](a []T, cmp func(x, y T) int) {
	n := len(a)
	b := make([]T, n/2)
	for s := 1; s < n; s *= 2 {
		for m := n - 1 - s; m >= 0; m -= s * 2 {
			nimMerge(a, b, max(m-s+1, 0), m, m+s, cmp)
		}
	}
}

func nimMerge[T any](a, b []T, lo, m, hi int, cmp func(x, y T) int) {
	if cmp(a[m], a[m+1]) <= 0 {
		return
	}
	copy(b, a[lo:m+1])
	i, j, k := 0, m+1, lo
	for k < j && j <= hi {
		if cmp(b[i], a[j]) <= 0 {
			a[k] = b[i]
			i++
		} else {
			a[k] = a[j]
			j++
		}
		k++
	}
	for k < j {
		a[k] = b[i]
		k++
		i++
	}
}
