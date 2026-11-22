// SPDX-License-Identifier: AGPL-3.0-only

package query

import (
	"slices"
	"strconv"
	"strings"

	"github.com/perennialtech/nitter/internal/types"
	"github.com/perennialtech/nitter/internal/utils"
)

func addUnique(list *[]string, value string) {
	if value != "" && !slices.Contains(*list, value) {
		*list = append(*list, value)
	}
}

func isUsernameChar(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_'
}

func isWordChar(b byte) bool {
	return isUsernameChar(b)
}

func isISODate(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	for i := range s {
		if i == 4 || i == 7 {
			continue
		}
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func searchTokens(text string) []string {
	var tokens []string
	var cur strings.Builder
	quoted := false
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c == '"' {
			quoted = !quoted
			cur.WriteByte(c)
		} else if !quoted && (c == ' ' || c == '\t' || c == '\n' || c == '\r') {
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		} else {
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

func cleanTerm(token string) string {
	return strings.Trim(token, `"`)
}

func containsTerm(haystack, term string) bool {
	if term == "" {
		return true
	}
	startsWord := isWordChar(term[0])
	endsWord := isWordChar(term[len(term)-1])
	for start := 0; start < len(haystack); {
		idx := strings.Index(haystack[start:], term)
		if idx < 0 {
			return false
		}
		pos := start + idx
		var before, after byte
		if pos > 0 {
			before = haystack[pos-1]
		}
		if pos+len(term) < len(haystack) {
			after = haystack[pos+len(term)]
		}
		if (!startsWord || !isWordChar(before)) && (!endsWord || !isWordChar(after)) {
			return true
		}
		start = pos + 1
	}
	return false
}

// HasSearchInput reports whether q carries any user-supplied search terms or filters.
func HasSearchInput(q types.Query) bool {
	return q.Text != "" || len(q.Filters) > 0 || len(q.Excludes) > 0 ||
		len(q.Includes) > 0 || q.Since != "" || q.Until != "" || q.MinLikes != ""
}

// ParseFromUsers extracts valid from:username operators from text, up to limit.
func ParseFromUsers(text string, limit int) []string {
	var users []string
	seen := map[string]struct{}{}
	for _, token := range searchTokens(text) {
		low := strings.ToLower(token)
		if !strings.HasPrefix(low, "from:") {
			continue
		}
		name := strings.TrimPrefix(token[5:], "@")
		if len(name) == 0 || len(name) > 15 {
			continue
		}
		valid := true
		for i := range name {
			if !isUsernameChar(name[i]) {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		users = append(users, name)
		if limit > 0 && len(users) >= limit {
			break
		}
	}
	return users
}

// ApplySearchOperators extracts supported inline operators from text into q
// and returns the remaining keyword query text.
func ApplySearchOperators(q *types.Query, text string) string {
	var words []string
	for _, token := range searchTokens(text) {
		low := strings.ToLower(token)
		switch {
		case strings.HasPrefix(low, "from:"):
			continue
		case strings.HasPrefix(low, "-filter:"):
			val := low[8:]
			if slices.Contains(ValidFilters, val) {
				addUnique(&q.Excludes, val)
			}
		case strings.HasPrefix(low, "filter:"):
			val := low[7:]
			if slices.Contains(ValidFilters, val) {
				addUnique(&q.Filters, val)
			}
		case strings.HasPrefix(low, "-include:"):
			val := low[9:]
			addUnique(&q.Excludes, val)
		case strings.HasPrefix(low, "include:"):
			val := low[8:]
			addUnique(&q.Includes, val)
		case strings.HasPrefix(low, "since:"):
			val := low[6:]
			if isISODate(val) {
				q.Since = val
			}
		case strings.HasPrefix(low, "until:"):
			val := low[6:]
			if isISODate(val) {
				q.Until = val
			}
		case strings.HasPrefix(low, "min_faves:"):
			val := utils.ValidateNumber(low[10:])
			if val != "" {
				q.MinLikes = val
			}
		default:
			words = append(words, token)
		}
	}
	return strings.Join(words, " ")
}

func effectiveMediaTweet(t *types.Tweet) *types.Tweet {
	if len(t.Media) == 0 && t.Card == nil && t.Retweet != nil {
		return t.Retweet
	}
	return t
}

func hasPhoto(t *types.Tweet) bool {
	src := effectiveMediaTweet(t)
	return src.HasPhotos() || (src.Card != nil && src.Card.Image != "")
}

func hasVideo(t *types.Tweet) bool {
	return effectiveMediaTweet(t).HasVideos()
}

func hasGif(t *types.Tweet) bool {
	return effectiveMediaTweet(t).HasGifs()
}

func hasMedia(t *types.Tweet) bool {
	src := effectiveMediaTweet(t)
	return len(src.Media) > 0 || (src.Card != nil && (src.Card.Image != "" || src.Card.Video != nil))
}

func tweetSearchText(t *types.Tweet, depth int) string {
	if t == nil || depth > 2 {
		return ""
	}
	var b strings.Builder
	b.WriteString(t.Text)
	if t.Card != nil {
		b.WriteByte(' ')
		b.WriteString(t.Card.Title)
		b.WriteByte(' ')
		b.WriteString(t.Card.Text)
		b.WriteByte(' ')
		b.WriteString(t.Card.Dest)
		b.WriteByte(' ')
		b.WriteString(t.Card.URL)
	}
	if t.Poll != nil {
		for _, opt := range t.Poll.Options {
			b.WriteByte(' ')
			b.WriteString(opt)
		}
	}
	for _, m := range t.Media {
		switch m.Kind {
		case types.PhotoMedia:
			b.WriteByte(' ')
			b.WriteString(m.Photo.AltText)
		case types.VideoMedia:
			b.WriteByte(' ')
			b.WriteString(m.Video.Title)
			b.WriteByte(' ')
			b.WriteString(m.Video.Description)
		case types.GifMedia:
			b.WriteByte(' ')
			b.WriteString(m.Gif.AltText)
		}
	}
	if t.Retweet != nil {
		b.WriteByte(' ')
		b.WriteString(tweetSearchText(t.Retweet, depth+1))
	}
	if t.Quote != nil {
		b.WriteByte(' ')
		b.WriteString(tweetSearchText(t.Quote, depth+1))
	}
	return b.String()
}

func matchesFilter(t *types.Tweet, filter string) bool {
	switch filter {
	case "media", "twimg":
		return hasMedia(t)
	case "images":
		return hasPhoto(t)
	case "videos", "consumer_video":
		return hasVideo(t) || hasGif(t)
	case "native_video":
		return hasVideo(t)
	case "spaces":
		src := effectiveMediaTweet(t)
		text := strings.ToLower(tweetSearchText(t, 0))
		return (src.Card != nil && src.Card.Kind == types.CardAudiospace) ||
			strings.Contains(text, "twitter.com/i/spaces/") ||
			strings.Contains(text, "/i/spaces/")
	case "links":
		text := strings.ToLower(tweetSearchText(t, 0))
		return t.Card != nil || strings.Contains(text, "http://") || strings.Contains(text, "https://")
	case "news":
		return t.Card != nil && t.Card.URL != ""
	case "quote":
		return t.Quote != nil
	case "mentions":
		return t.Retweet == nil && (strings.Contains(t.Text, "@") || len(t.MediaTags) > 0)
	case "replies":
		return t.ReplyID > 0
	case "retweets", "nativeretweets":
		return t.Retweet != nil
	default:
		return false
	}
}

func matchesToken(haystack, token string) bool {
	if token == "" {
		return true
	}
	if urlPart, ok := strings.CutPrefix(token, "url:"); ok {
		return urlPart == "" || strings.Contains(haystack, urlPart)
	}
	return containsTerm(haystack, token)
}

func matchesSearchText(t *types.Tweet, text string) bool {
	if strings.TrimSpace(text) == "" {
		return true
	}
	haystack := strings.ToLower(tweetSearchText(t, 0))
	groups := [][]string{{}}
	var excluded []string
	for _, token := range searchTokens(text) {
		low := strings.ToLower(token)
		if low == "or" {
			groups = append(groups, nil)
		} else if len(token) > 1 && token[0] == '-' {
			excluded = append(excluded, strings.ToLower(cleanTerm(token[1:])))
		} else {
			groups[len(groups)-1] = append(groups[len(groups)-1], strings.ToLower(cleanTerm(token)))
		}
	}

	hasPositive := false
	matched := false
	for _, group := range groups {
		if len(group) == 0 {
			continue
		}
		hasPositive = true
		allMatch := true
		for _, term := range group {
			if !matchesToken(haystack, term) {
				allMatch = false
				break
			}
		}
		if allMatch {
			matched = true
			break
		}
	}
	if hasPositive && !matched {
		return false
	}

	for _, term := range excluded {
		if term != "" && matchesToken(haystack, term) {
			return false
		}
	}
	return true
}

// MatchesQuery reports whether tweet satisfies q's local search filters, date range,
// minimum likes, and keyword/operator text.
func MatchesQuery(tweet *types.Tweet, q types.Query) bool {
	if tweet == nil {
		return false
	}
	if len(q.Since) == 10 && !tweet.Time.IsZero() && tweet.Time.UTC().Format("2006-01-02") < q.Since {
		return false
	}
	if len(q.Until) == 10 && !tweet.Time.IsZero() && tweet.Time.UTC().Format("2006-01-02") > q.Until {
		return false
	}
	if q.MinLikes != "" {
		if minLikes, err := strconv.Atoi(q.MinLikes); err == nil && tweet.Stats.Likes < minLikes {
			return false
		}
	}

	if term := MediaLinkTerm(q); term != "" {
		haystack := strings.ToLower(tweetSearchText(tweet, 0))
		if !matchesToken(haystack, strings.ToLower(term)) {
			return false
		}
	}

	if !matchesSearchText(tweet, q.Text) {
		return false
	}

	if len(q.Filters) > 0 {
		if q.Sep == "OR" {
			ok := false
			for _, f := range q.Filters {
				if matchesFilter(tweet, f) {
					ok = true
					break
				}
			}
			if !ok {
				return false
			}
		} else {
			for _, f := range q.Filters {
				if !matchesFilter(tweet, f) {
					return false
				}
			}
		}
	}

	for _, f := range q.Excludes {
		if matchesFilter(tweet, f) {
			return false
		}
	}

	for _, f := range q.Includes {
		if !matchesFilter(tweet, f) {
			return false
		}
	}

	return true
}
