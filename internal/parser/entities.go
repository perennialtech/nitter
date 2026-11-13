// SPDX-License-Identifier: AGPL-3.0-only

package parser

import (
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/tidwall/gjson"

	"github.com/perennialtech/nitter/internal/formatters"
	"github.com/perennialtech/nitter/internal/types"
	"github.com/perennialtech/nitter/internal/utils"
)

const (
	unicodeOpen  = "￺"
	unicodeClose = "￻"
	unicodeAmp   = "￹"
	xmlOpen      = "&lt;"
	xmlClose     = "&gt;"
)

var (
	// Nim's re module runs PCRE in byte mode; for these patterns that gives
	// the same output as Go's rune-based matching on valid UTF-8.
	unRegex   = regexp.MustCompile(`(^|[^A-z0-9\-_./?])@([A-z0-9_]{1,15})`)
	unReplace = `${1}<a href="/${2}">@${2}</a>`

	htRegex   = regexp.MustCompile(`(^|[^\w\-_./?])([#$]|＃)([\w_]+)`)
	htReplace = `${1}<a href="/search?f=tweets&q=%23${3}">${2}${3}</a>`

	xmlEscaper = strings.NewReplacer(
		"<", "&lt;", ">", "&gt;", "&", "&amp;", `"`, "&quot;", "'", "&apos;")
	entityEscaper   = strings.NewReplacer("&", unicodeAmp, "<", unicodeOpen, ">", unicodeClose)
	entityUnescaper = strings.NewReplacer(unicodeAmp, "&amp;", unicodeOpen, xmlOpen, unicodeClose, xmlClose)
)

// xmlEscape is Nim's xmltree.escape.
func xmlEscape(s string) string { return xmlEscaper.Replace(s) }

type replaceKind int

const (
	rkRemove replaceKind = iota
	rkURL
	rkHashtag
	rkMention
)

// replaceSlice replaces the runes a..b (inclusive, like a Nim slice).
type replaceSlice struct {
	a, b         int
	kind         replaceKind
	url, display string
}

// cmpSlices is the Nim comparator, which compares x's start with y's end.
func cmpSlices(x, y replaceSlice) int { return cmpInt(x.a, y.b) }

func cmpInt(x, y int) int {
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}

// deduplicate drops slices starting where an earlier one does, moving the
// last element into the gap like Nim's seq.del.
func deduplicate(s []replaceSlice) []replaceSlice {
	for i := 0; i < len(s); i++ {
		for j := i + 1; j < len(s); {
			if s[i].a == s[j].a {
				s[j] = s[len(s)-1]
				s = s[:len(s)-1]
			} else {
				j++
			}
		}
	}
	return s
}

func sortSlices(s []replaceSlice) { nimSort(s, cmpSlices) }

// extractSlice reads an entity's [start, end) indices as an inclusive slice.
func extractSlice(js gjson.Result) (int, int) {
	ind := get(js, "indices")
	return getInt(idx(ind, 0)), getInt(idx(ind, 1)) - 1
}

func extractURLs(repls []replaceSlice, js gjson.Result, textLen int, hideTwitter, hideArticle bool) []replaceSlice {
	url := getExpandedURL(js, "")
	a, b := extractSlice(js)

	switch {
	case hideArticle && utils.IsTwitterURL(url) && strings.Contains(url, "/article/"):
		if a < textLen {
			repls = append(repls, replaceSlice{kind: rkRemove, a: a, b: b})
		}
	case hideTwitter && b+1 >= textLen && utils.IsTwitterURL(url):
		if a < textLen {
			repls = append(repls, replaceSlice{kind: rkRemove, a: a, b: b})
		}
	default:
		repls = append(repls, replaceSlice{kind: rkURL, url: url,
			display: formatters.ShortLink(url), a: a, b: b})
	}
	return repls
}

func hashtagSlice(js gjson.Result) replaceSlice {
	a, b := extractSlice(js)
	return replaceSlice{kind: rkHashtag, a: a, b: b}
}

// replacedWith renders runes[start:end] with the replacements applied as
// links, skipping replacements whose range doesn't fit the text.
func replacedWith(runes []rune, repls []replaceSlice, start, end int) string {
	return replacedWithText(runes, repls, start, end, func(s string) string { return s })
}

// isLinkable reports whether url is a path or an http(s) URL.
func isLinkable(url string) bool {
	if strings.HasPrefix(url, "/") {
		return true
	}
	scheme, _, ok := strings.Cut(url, ":")
	scheme = strings.ToLower(scheme)
	return ok && (scheme == "http" || scheme == "https")
}

// replacedWithText is replacedWith, passing the text outside the
// replacements through text.
func replacedWithText(runes []rune, repls []replaceSlice, start, end int, text func(string) string) string {
	runeLen := len(runes)
	safeStart := max(0, start)
	safeEnd := min(runeLen, end)

	var valid []replaceSlice
	for _, rep := range repls {
		if rep.a >= 0 && rep.b >= 0 && rep.b < runeLen && rep.a <= rep.b {
			valid = append(valid, rep)
		}
	}

	lowerBound := func(i int) int {
		if i > 0 {
			return min(valid[i-1].b+1, runeLen)
		}
		return safeStart
	}

	var sb strings.Builder
	sb.Grow(runeLen)
	for i, rep := range valid {
		if lower := lowerBound(i); lower < rep.a {
			sb.WriteString(text(string(runes[lower:rep.a])))
		}
		switch rep.kind {
		case rkHashtag:
			if rep.a+1 <= rep.b {
				name := string(runes[rep.a+1 : rep.b+1])
				symbol := string(runes[rep.a])
				sb.WriteString(`<a href="/search?f=tweets&q=%23` + xmlEscape(name) + `">` +
					symbol + name + "</a>")
			}
		case rkMention:
			sb.WriteString(`<a href="` + xmlEscape(rep.url) + `" title="` + xmlEscape(rep.display) +
				`">` + string(runes[rep.a:rep.b+1]) + "</a>")
		case rkURL:
			if isLinkable(rep.url) {
				sb.WriteString(`<a href="` + xmlEscape(rep.url) + `">` + xmlEscape(rep.display) + "</a>")
			} else {
				sb.WriteString(xmlEscape(rep.display))
			}
		}
	}

	restA, restB := lowerBound(len(valid)), safeEnd-1
	if restA >= 0 && restA <= restB && restB <= runeLen {
		sb.WriteString(text(string(runes[restA : restB+1])))
	}
	return sb.String()
}

func stripTrailing(s string) string { return strings.TrimRight(s, " \t\v\r\n\f") }

func linkUsersAndHashtags(s string) string {
	s = unRegex.ReplaceAllString(s, unReplace)
	return htRegex.ReplaceAllString(s, htReplace)
}

// expandBio renders a profile description, which X sends as plain text
// (unlike tweet text), with its URL entities as links and the mentions and
// hashtags in the text between them linked. xmlEscape's named entities,
// unlike html.EscapeString's "&#39;", give the hashtag pattern nothing to
// match.
func expandBio(bio string, repls []replaceSlice) string {
	runes := []rune(bio)
	repls = deduplicate(repls)
	sortSlices(repls)
	return replacedWithText(runes, repls, 0, len(runes), func(s string) string {
		return linkUsersAndHashtags(xmlEscape(s))
	})
}

func expandUserEntities(user *types.User, js gjson.Result) {
	ent := get(js, "entities")
	if urls := get(ent, "url.urls"); notNull(urls) {
		user.Website = getExpandedURL(idx(urls, 0), "")
	}

	var repls []replaceSlice
	for _, u := range arr(get(ent, "description.urls")) {
		repls = extractURLs(repls, u, utf8.RuneCountInString(user.Bio)-1, false, false)
	}
	user.Bio = expandBio(user.Bio, repls)
}

func indexOf(s []string, v string) int { return slices.Index(s, v) }

func removeAt(s []string, i int) []string { return append(s[:i], s[i+1:]...) }

// expandTextEntities links the entities of text; start and end are Nim's
// inclusive textSlice bounds, which callers fill from display_text_range.
func expandTextEntities(tweet *types.Tweet, entities gjson.Result, text string, start, end int,
	replyTo string, hasRedundantLink, hasArticle bool) {
	hasCard := tweet.Card != nil

	var repls []replaceSlice

	for _, u := range arr(get(entities, "urls")) {
		urlStr := str(get(u, "url"))
		if urlStr == "" || !strings.Contains(text, urlStr) {
			continue
		}

		repls = extractURLs(repls, u, end, hasRedundantLink, hasArticle)

		if hasCard && urlStr == tweet.Card.URL {
			tweet.Card.URL = getExpandedURL(u, "")
		}
	}

	for _, m := range arr(get(entities, "media")) {
		repls = extractURLs(repls, m, end, true, false)
	}

	for _, h := range arr(get(entities, "hashtags")) {
		repls = append(repls, hashtagSlice(h))
	}

	for _, s := range arr(get(entities, "symbols")) {
		repls = append(repls, hashtagSlice(s))
	}

	for _, mention := range arr(get(entities, "user_mentions")) {
		name := str(get(mention, "screen_name"))
		a, b := extractSlice(mention)
		i := indexOf(tweet.Reply, name)

		if a >= start {
			repls = append(repls, replaceSlice{kind: rkMention, a: a, b: b,
				url: "/" + name, display: str(get(mention, "name"))})
			if i > -1 && name != replyTo {
				tweet.Reply = removeAt(tweet.Reply, i)
			}
		} else if i == -1 && tweet.ReplyID != 0 {
			tweet.Reply = append(tweet.Reply, name)
		}
	}

	repls = deduplicate(repls)
	sortSlices(repls)

	tweet.Text = stripTrailing(replacedWith([]rune(text), repls, start, end))
}

func textRange(r gjson.Result) (int, int) {
	return getInt(idx(r, 0)), getInt(idx(r, 1))
}

func expandTweetEntities(tweet *types.Tweet, js gjson.Result, hasArticle bool) {
	entities := get(js, "entities")
	if isNull(entities) {
		return
	}
	start, end := textRange(get(js, "display_text_range"))
	hasQuote := getBool(get(js, "is_quote_status"))
	hasJobCard := tweet.Card != nil && tweet.Card.Kind == types.CardJobDetails

	replyTo := ""
	if tweet.ReplyID != 0 {
		if reply := get(js, "in_reply_to_screen_name"); notNull(reply) {
			replyTo = str(reply)
			tweet.Reply = append(tweet.Reply, replyTo)
		}
	}

	expandTextEntities(tweet, entities, tweet.Text, start, end, replyTo,
		hasQuote || hasJobCard, hasArticle)
}

func expandTextEntitiesV2(tweet *types.Tweet, js gjson.Result, text string, start, end int,
	hasRedundantLink, hasArticle bool) {
	hasCard := tweet.Card != nil

	var repls []replaceSlice

	for _, u := range arr(get(js, "url_entities")) {
		urlStr := str(get(u, "url"))
		if urlStr == "" || !strings.Contains(text, urlStr) {
			continue
		}

		repls = extractURLs(repls, u, end, hasRedundantLink, hasArticle)

		if hasCard && urlStr == tweet.Card.URL {
			tweet.Card.URL = getExpandedURL(u, "")
		}
	}

	for _, h := range arr(get(js, "details.hashtag_entities")) {
		repls = append(repls, hashtagSlice(h))
	}

	for _, c := range arr(get(js, "details.cashtag_entities")) {
		repls = append(repls, hashtagSlice(c))
	}

	for _, mention := range arr(get(js, "mention_entities")) {
		name := str(get(mention, "screen_name"))
		a, b := extractSlice(mention)
		i := indexOf(tweet.Reply, name)

		if a >= start {
			repls = append(repls, replaceSlice{kind: rkMention, a: a, b: b,
				url: "/" + name, display: str(get(mention, "name"))})
		} else if i == -1 && tweet.ReplyID != 0 {
			tweet.Reply = append(tweet.Reply, name)
		}
	}

	repls = deduplicate(repls)
	sortSlices(repls)

	tweet.Text = stripTrailing(replacedWith([]rune(text), repls, start, end))
}

func expandTweetEntitiesV2(tweet *types.Tweet, js gjson.Result, hasArticle bool) {
	start, end := textRange(get(js, "details.display_text_range"))
	hasQuote := has(js, "quoted_tweet_results")
	hasJobCard := tweet.Card != nil && tweet.Card.Kind == types.CardJobDetails
	hasAttribution := tweet.Attribution != nil

	expandTextEntitiesV2(tweet, js, tweet.Text, start, end,
		hasQuote || hasJobCard || hasAttribution, hasArticle)
}

var (
	noteEscaper   = strings.NewReplacer("<", unicodeOpen, ">", unicodeClose)
	noteUnescaper = strings.NewReplacer(unicodeOpen, xmlOpen, unicodeClose, xmlClose)
)

func expandNoteTweetEntities(tweet *types.Tweet, js gjson.Result) {
	entities := get(js, "entity_set")
	if isNull(entities) {
		return
	}
	text := noteEscaper.Replace(str(get(js, "text")))
	hasAttribution := tweet.Attribution != nil

	expandTextEntities(tweet, entities, text, 0, utf8.RuneCountInString(text), "",
		hasAttribution, false)

	tweet.Text = noteUnescaper.Replace(tweet.Text)
}

// localizeExternalLink returns "" for anything but an http(s) URL with a
// host, and otherwise points t.co and Twitter links at this instance.
func localizeExternalLink(url string) string {
	target := parseNimURI(url)
	scheme := strings.ToLower(target.scheme)
	if (scheme != "http" && scheme != "https") || target.hostname == "" {
		return ""
	}

	const tco = "https://t.co/"
	if strings.HasPrefix(url, tco) {
		return "/t.co/" + url[len(tco):]
	}
	return utils.LocalizeTwitterLink(url)
}

func escapeEntityText(text string) string   { return entityEscaper.Replace(text) }
func unescapeEntityText(html string) string { return entityUnescaper.Replace(html) }

func getIndex(js gjson.Result) int {
	index := -1
	if js.Type == gjson.String {
		n, ok := nimParseInt(js.Str)
		if !ok {
			return -1
		}
		index = int(n)
	} else {
		index = intOr(js, -1)
	}
	return max(index, -1)
}

// expandBirdwatchEntities links a community note's text using its entities,
// falling back to linkifying bare URLs.
func expandBirdwatchEntities(text string, entities gjson.Result) string {
	if !entities.IsArray() {
		return formatters.LinkifyBirdwatchURLs(text)
	}

	runes := []rune(escapeEntityText(text))
	var repls []replaceSlice

	for _, entity := range arr(entities) {
		fromIdx := getIndex(sel(get(entity, "fromIndex"), get(entity, "from_index")))
		toIdx := getIndex(sel(get(entity, "toIndex"), get(entity, "to_index")))
		url := localizeExternalLink(str(get(entity, "ref.url")))
		if url != "" && fromIdx >= 0 && toIdx > fromIdx && toIdx <= len(runes) {
			repls = append(repls, replaceSlice{kind: rkURL, a: fromIdx, b: toIdx - 1,
				url: url, display: string(runes[fromIdx:toIdx])})
		}
	}

	if len(repls) == 0 {
		return formatters.LinkifyBirdwatchURLs(text)
	}

	sortSlices(repls)
	return unescapeEntityText(replacedWith(runes, repls, 0, len(runes)))
}

func indexSlice(js gjson.Result) (int, int) {
	ind := get(js, "indices")
	return getIndex(idx(ind, 0)), getIndex(idx(ind, 1)) - 1
}

// expandTranslation links the entities of a Grok post translation.
func expandTranslation(text string, entities gjson.Result) string {
	runes := []rune(escapeEntityText(text))
	var repls []replaceSlice

	for _, u := range arr(get(entities, "urls")) {
		url := str(get(u, "expanded_url"))
		if href := localizeExternalLink(url); href != "" {
			a, b := indexSlice(u)
			repls = append(repls, replaceSlice{kind: rkURL, a: a, b: b, url: href,
				display: strOr(get(u, "display_url"), formatters.ShortLink(url))})
		}
	}

	for _, key := range []string{"hashtags", "symbols"} {
		for _, tag := range arr(get(entities, key)) {
			a, b := indexSlice(tag)
			repls = append(repls, replaceSlice{kind: rkHashtag, a: a, b: b})
		}
	}

	for _, mention := range arr(get(entities, "user_mentions")) {
		if name := str(get(mention, "screen_name")); name != "" {
			a, b := indexSlice(mention)
			repls = append(repls, replaceSlice{kind: rkMention, a: a, b: b,
				url: "/" + name, display: str(get(mention, "name"))})
		}
	}

	repls = deduplicate(repls)
	sortSlices(repls)
	return unescapeEntityText(stripTrailing(replacedWith(runes, repls, 0, len(runes))))
}

// expandNoteTranslation links the entities of a Grok community note translation.
func expandNoteTranslation(text string, entities gjson.Result) string {
	runes := []rune(escapeEntityText(text))
	var repls []replaceSlice

	for _, e := range arr(entities) {
		fromIdx := getIndex(get(e, "from_index"))
		toIdx := getIndex(get(e, "to_index"))
		r := get(e, "ref")
		url := localizeExternalLink(strOr(get(r, "expanded_url"), str(get(r, "url"))))
		if url != "" && fromIdx >= 0 && toIdx > fromIdx && fromIdx < len(runes) {
			repls = append(repls, replaceSlice{kind: rkURL, a: fromIdx, b: toIdx - 1, url: url,
				display: strOr(get(r, "display_url"), string(runes[fromIdx:min(toIdx, len(runes))]))})
		}
	}

	sortSlices(repls)
	return unescapeEntityText(replacedWith(runes, repls, 0, len(runes)))
}

// GrokLink is a link reported alongside an on-demand Grok translation.
type GrokLink struct {
	URL      string
	Expanded string
}

// ExpandGrokTranslation links every occurrence of each reported URL in an
// on-demand translation.
func ExpandGrokTranslation(text string, links []GrokLink) string {
	runes := []rune(escapeEntityText(text))
	var repls []replaceSlice
	seen := map[string]bool{}

	for _, link := range links {
		if link.URL == "" || seen[link.URL] {
			continue
		}
		target := link.Expanded
		if target == "" {
			target = link.URL
		}
		href := localizeExternalLink(target)
		if href == "" {
			continue
		}
		seen[link.URL] = true

		urlLen := utf8.RuneCountInString(link.URL)
		for offset := 0; ; {
			i := strings.Index(text[offset:], link.URL)
			if i < 0 {
				break
			}
			i += offset
			start := utf8.RuneCountInString(text[:i])
			repls = append(repls, replaceSlice{kind: rkURL, a: start, b: start + urlLen - 1,
				url: href, display: target})
			offset = i + len(link.URL)
		}
	}

	// An expanded source URL can be a prefix of another reported URL.
	nimSort(repls, func(a, b replaceSlice) int {
		if c := cmpInt(a.a, b.a); c != 0 {
			return c
		}
		return cmpInt(b.b, a.b)
	})
	var disjoint []replaceSlice
	for _, r := range repls {
		if len(disjoint) == 0 || r.a > disjoint[len(disjoint)-1].b {
			disjoint = append(disjoint, r)
		}
	}
	return unescapeEntityText(replacedWith(runes, disjoint, 0, len(runes)))
}

// ExtractGalleryPhoto picks a tweet's first media thumbnail, or its card
// image, for the profile photo rail.
func ExtractGalleryPhoto(t *types.Tweet) types.GalleryPhoto {
	url := ""
	if len(t.Media) > 0 {
		url = t.Media[0].Thumb()
	} else if t.Card != nil {
		url = t.Card.Image
	}
	return types.GalleryPhoto{URL: url, TweetID: itoa(t.ID)}
}

func facetSlice(f gjson.Result) (int, int) {
	ind := get(f, "indices")
	return getInt(idx(ind, 0)), getInt(idx(ind, 1)) - 1
}

// ExpandFxText expands FxTwitter's raw_text, which is X's full_text
// (HTML-escaped) with its entities as facets, indexed in code points like
// X's, so it expands the same way.
func ExpandFxText(tweet *types.Tweet, rawText gjson.Result, replyTo string, hideTwitter, hideArticle bool) {
	text := str(get(rawText, "text"))
	start, end := textRange(get(rawText, "display_text_range"))
	hasCard := tweet.Card != nil

	if replyTo != "" {
		tweet.Reply = append(tweet.Reply, replyTo)
	}

	var repls []replaceSlice
	for _, f := range arr(get(rawText, "facets")) {
		a, b := facetSlice(f)
		switch str(get(f, "type")) {
		case "url":
			original := str(get(f, "original"))
			url := strOr(get(f, "replacement"), original)
			switch {
			case hideArticle && utils.IsTwitterURL(url) && strings.Contains(url, "/article/"):
				if a < end {
					repls = append(repls, replaceSlice{kind: rkRemove, a: a, b: b})
				}
			case hideTwitter && b+1 >= end && utils.IsTwitterURL(url):
				if a < end {
					repls = append(repls, replaceSlice{kind: rkRemove, a: a, b: b})
				}
			default:
				repls = append(repls, replaceSlice{kind: rkURL, url: url,
					display: formatters.ShortLink(url), a: a, b: b})
			}
			if hasCard && original == tweet.Card.URL {
				tweet.Card.URL = url
			}
		case "hashtag", "cashtag", "symbol":
			repls = append(repls, replaceSlice{kind: rkHashtag, a: a, b: b})
		case "mention":
			name := str(get(f, "original"))
			i := indexOf(tweet.Reply, name)
			if a >= start {
				repls = append(repls, replaceSlice{kind: rkMention, a: a, b: b, url: "/" + name})
				if i > -1 && name != replyTo {
					tweet.Reply = removeAt(tweet.Reply, i)
				}
			} else if i == -1 && tweet.ReplyID != 0 {
				tweet.Reply = append(tweet.Reply, name)
			}
		}
	}

	repls = deduplicate(repls)
	sortSlices(repls)

	tweet.Text = stripTrailing(replacedWith([]rune(text), repls, start, end))
}

// ExpandFxBio expands FxTwitter's raw_description into user.Bio.
func ExpandFxBio(user *types.User, raw gjson.Result) {
	var repls []replaceSlice
	for _, f := range arr(get(raw, "facets")) {
		if str(get(f, "type")) == "url" {
			url := strOr(get(f, "replacement"), str(get(f, "original")))
			a, b := facetSlice(f)
			repls = append(repls, replaceSlice{kind: rkURL, url: url,
				display: formatters.ShortLink(url), a: a, b: b})
		}
	}
	user.Bio = expandBio(str(get(raw, "text")), repls)
}

// ExpandFxNote renders an FxTwitter community note as HTML.
func ExpandFxNote(note gjson.Result) string {
	text := str(get(note, "text"))
	runes := []rune(escapeEntityText(text))
	var repls []replaceSlice
	for _, f := range arr(get(note, "facets")) {
		a, b := facetSlice(f)
		if str(get(f, "type")) == "url" && a >= 0 && b < len(runes) {
			url := localizeExternalLink(strOr(get(f, "replacement"), str(get(f, "original"))))
			display := ""
			if a <= b {
				display = string(runes[a : b+1])
			}
			repls = append(repls, replaceSlice{kind: rkURL, a: a, b: b, url: url, display: display})
		}
	}

	if len(repls) == 0 {
		return formatters.LinkifyBirdwatchURLs(text)
	}

	sortSlices(repls)
	return unescapeEntityText(replacedWith(runes, repls, 0, len(runes)))
}
