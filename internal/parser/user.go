// SPDX-License-Identifier: AGPL-3.0-only

package parser

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/tidwall/gjson"

	"github.com/perennialtech/nitter/internal/formatters"
	"github.com/perennialtech/nitter/internal/types"
)

// This file ports the experimental parsers, which decoded responses into
// typed objects with jsony. jsony matches a JSON key to a field by the
// field's name or its snake_case form, after any renameHook, and the last
// matching key wins; jfield reproduces that lookup.

func snakeCase(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			b.WriteByte('_')
			c += 'a' - 'A'
		}
		b.WriteByte(c)
	}
	return b.String()
}

// jfield returns the value jsony would decode into the field called name;
// renamed lists JSON keys a renameHook maps onto it.
func jfield(r gjson.Result, name string, renamed ...string) gjson.Result {
	if !r.IsObject() {
		return gjson.Result{}
	}
	snake := snakeCase(name)
	var out gjson.Result
	r.ForEach(func(k, v gjson.Result) bool {
		if key := k.String(); key == name || key == snake || slices.Contains(renamed, key) {
			out = v
		}
		return true
	})
	return out
}

// jpath follows nested fields with jfield.
func jpath(r gjson.Result, names ...string) gjson.Result {
	for _, n := range names {
		r = jfield(r, n)
	}
	return r
}

// expVerifiedType is graphuser's enumHook: unknown values are None.
func expVerifiedType(r gjson.Result) types.VerifiedType {
	return parseEnum(str(r), verifiedTypes, types.VerifiedNone)
}

// expHashtag is a match of the experimental bio hashtag pattern
// (*U)(^|[^\w-_.?])([#＃$])([\w_]*+)(?!</a>|">|#).
type expHashtag struct {
	prefix, hash, name string
	end                int
}

// matchExpHashtag tries the pattern at byte offset i. RE2 has neither the
// possessive quantifier nor the lookahead, so it's matched by hand: the name
// takes every word character, and the match fails (without backtracking into
// the name) if "</a>", `">` or "#" follows.
func matchExpHashtag(s string, i int) (expHashtag, bool) {
	try := func(at int) (expHashtag, bool) {
		if at >= len(s) {
			return expHashtag{}, false
		}
		r, size := utf8.DecodeRuneInString(s[at:])
		if r != '#' && r != '＃' && r != '$' {
			return expHashtag{}, false
		}
		j := at + size
		k := j
		for k < len(s) && isWordByte(s[k]) {
			k++
		}
		if rest := s[k:]; strings.HasPrefix(rest, "</a>") || strings.HasPrefix(rest, `">`) ||
			strings.HasPrefix(rest, "#") {
			return expHashtag{}, false
		}
		return expHashtag{prefix: s[i:at], hash: s[at:j], name: s[j:k], end: k}, true
	}

	// The first alternative, ^, only applies at the start of the text.
	if i == 0 {
		if m, ok := try(0); ok {
			return m, true
		}
	}
	if i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !(r < utf8.RuneSelf && isWordByte(byte(r))) && r != '-' && r != '.' && r != '?' {
			return try(i + size)
		}
	}
	return expHashtag{}, false
}

// \w in PCRE without UCP is ASCII only, as in Go.
func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

func expLinkHashtags(s string) string {
	var b strings.Builder
	last := 0
	for i := 0; i < len(s); {
		if m, ok := matchExpHashtag(s, i); ok {
			b.WriteString(s[last:i])
			b.WriteString(m.prefix + `<a href="/search?f=tweets&q=%23` + m.name + `">` + m.hash + m.name + "</a>")
			last, i = m.end, m.end
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	b.WriteString(s[last:])
	return b.String()
}

// runeRange is runes[lo:hi], empty where Nim's slice would be empty or fail.
func runeRange(runes []rune, lo, hi int) string {
	lo, hi = max(lo, 0), min(hi, len(runes))
	if lo >= hi {
		return ""
	}
	return string(runes[lo:hi])
}

// replacedWithExp is the experimental slices.replacedWith. Unlike
// parserutils' version it neither validates ranges nor escapes the URL, and
// the experimental parsers only ever give it URL replacements.
func replacedWithExp(runes []rune, repls []replaceSlice, start, end int) string {
	lowerBound := func(i int) int {
		if i > 0 {
			return repls[i-1].b + 1
		}
		return start
	}

	var sb strings.Builder
	for i, rep := range repls {
		sb.WriteString(runeRange(runes, lowerBound(i), rep.a))
		if rep.kind == rkURL {
			sb.WriteString(`<a href="` + rep.url + `">` + rep.display + "</a>")
		}
	}

	if restA, restB := lowerBound(len(repls)), end-1; restA <= restB {
		sb.WriteString(runeRange(runes, restA, restB+1))
	}
	return sb.String()
}

// expandUserEntitiesExp is the experimental user parser's expandUserEntities.
func expandUserEntitiesExp(user *types.User, ent gjson.Result) {
	orig := []rune(user.Bio)

	if urls := arr(jpath(ent, "url", "urls")); len(urls) > 0 {
		user.Website = str(jfield(urls[0], "expandedUrl"))
	}

	var repls []replaceSlice
	for _, u := range arr(jpath(ent, "description", "urls")) {
		link := str(jfield(u, "expandedUrl"))
		ind := jfield(u, "indices")
		repls = append(repls, replaceSlice{kind: rkURL, url: link, display: formatters.ShortLink(link),
			a: getInt(idx(ind, 0)), b: getInt(idx(ind, 1)) - 1})
	}

	repls = deduplicate(repls)
	sortSlices(repls)

	bio := replacedWithExp(orig, repls, 0, len(orig))
	user.Bio = expLinkHashtags(unRegex.ReplaceAllString(bio, unReplace))
}

func expBanner(raw gjson.Result) string {
	if url := str(jfield(raw, "profileBannerUrl")); url != "" {
		return url + "/1500x500"
	}

	if color := str(jfield(raw, "profileLinkColor")); color != "" {
		return "#" + color
	}

	if ext := jfield(raw, "profileImageExtensions"); notNull(ext) {
		if palette := arr(jpath(ext, "mediaColor", "r", "ok", "palette")); len(palette) > 0 {
			color := jfield(palette[0], "rgb")
			return fmt.Sprintf("#%02x%02x%02x", getInt(jfield(color, "red")),
				getInt(jfield(color, "green")), getInt(jfield(color, "blue")))
		}
	}
	return ""
}

// toUser decodes a legacy user object (the experimental RawUser).
func toUser(raw gjson.Result) types.User {
	result := types.User{
		ID:           str(jfield(raw, "idStr")),
		Username:     str(jfield(raw, "screenName")),
		Fullname:     str(jfield(raw, "name")),
		Location:     str(jfield(raw, "location")),
		Bio:          str(jfield(raw, "description")),
		Following:    getInt(jfield(raw, "friendsCount")),
		Followers:    getInt(jfield(raw, "followersCount")),
		Tweets:       getInt(jfield(raw, "statusesCount")),
		Likes:        getInt(jfield(raw, "favouritesCount")),
		Media:        getInt(jfield(raw, "mediaCount")),
		VerifiedType: expVerifiedType(jfield(raw, "verifiedType")),
		Protected:    getBool(jfield(raw, "protected")),
		Banner:       expBanner(raw),
		UserPic:      strings.ReplaceAll(expImageURL(str(jfield(raw, "profileImageUrlHttps"))), "_normal", ""),
	}

	if createdAt := str(jfield(raw, "createdAt")); createdAt != "" {
		result.JoinDate = parseTwitterDate(createdAt)
	}

	if pinned := arr(jfield(raw, "pinnedTweetIdsStr")); len(pinned) > 0 {
		result.PinnedTweet, _ = nimParseInt(str(pinned[0]))
	}

	expandUserEntitiesExp(&result, jfield(raw, "entities"))
	return result
}

// parseUserResult decodes a GraphQL user result (the experimental
// graphql.parseUserResult).
func parseUserResult(userResult gjson.Result) types.User {
	result := toUser(jfield(userResult, "legacy"))

	if result.VerifiedType == types.VerifiedNone && getBool(jfield(userResult, "isBlueVerified")) {
		result.VerifiedType = types.VerifiedBlue
	}

	core := jfield(userResult, "core")
	if result.Username == "" && str(jfield(core, "screenName")) != "" {
		// Modern UserByRestId/UserByScreenName shape: `legacy` is empty and the data
		// lives in typed sub-objects (core/relationship_counts/tweet_counts/etc.).
		result.ID = str(jfield(userResult, "restId"))
		result.Username = str(jfield(core, "screenName"))
		result.Fullname = str(jfield(core, "name"))
		result.UserPic = strings.ReplaceAll(str(jpath(userResult, "avatar", "imageUrl")), "_normal", "")

		if banner := str(jpath(userResult, "banner", "imageUrl")); banner != "" {
			result.Banner = banner + "/1500x500"
		}

		if privacy := jfield(userResult, "privacy"); notNull(privacy) {
			result.Protected = getBool(jfield(privacy, "protected"))
		}

		if location := jfield(userResult, "location"); notNull(location) {
			result.Location = str(jfield(location, "location"))
		}

		if createdAt := str(jfield(core, "createdAt")); createdAt != "" {
			result.JoinDate = parseTwitterDate(createdAt)
		}

		if v := jfield(userResult, "verification"); notNull(v) {
			if vt := expVerifiedType(jfield(v, "verifiedType")); vt != types.VerifiedNone {
				result.VerifiedType = vt
			}
		}

		if rc := jfield(userResult, "relationshipCounts"); notNull(rc) {
			result.Followers = getInt(jfield(rc, "followers"))
			result.Following = getInt(jfield(rc, "following"))
		}

		if tc := jfield(userResult, "tweetCounts"); notNull(tc) {
			result.Tweets = getInt(jfield(tc, "tweets"))
			result.Media = getInt(jfield(tc, "mediaTweets"))
		}

		if ac := jfield(userResult, "actionCounts"); notNull(ac) {
			result.Likes = getInt(jfield(ac, "favoritesCount"))
		}

		if bio := jfield(userResult, "profileBio"); notNull(bio) {
			result.Bio = str(jfield(bio, "description"))
			expandUserEntitiesExp(&result, jfield(bio, "entities"))
		}
	}

	// "Parody", "Commentary" or "Fan"; "None" (or missing) when not flagged
	if label := str(jfield(userResult, "parodyCommentaryFanLabel")); label != "" && strings.ToLower(label) != "none" {
		result.ParodyLabel = label
	}
	return result
}

// ParseGraphUser parses a UserByScreenName/UserByRestId response.
func ParseGraphUser(js gjson.Result) types.User {
	if !js.IsObject() {
		return emptyUser()
	}

	data := jfield(js, "data")
	var userResult gjson.Result
	if ur := jfield(data, "userResult"); notNull(ur) {
		userResult = jfield(ur, "result")
	} else if u := jfield(data, "user"); notNull(u) {
		userResult = jfield(u, "result")
	}

	if str(jfield(userResult, "unavailableReason")) == "Suspended" ||
		str(jfield(userResult, "reason")) == "Suspended" {
		u := emptyUser()
		u.Suspended = true
		return u
	}

	return parseUserResult(userResult)
}

// parseUserTimeline reads the users and bottom cursor of a members or
// followers timeline.
func parseUserTimeline(result *types.Result[types.User], instructions gjson.Result) {
	for _, instruction := range arr(instructions) {
		if str(jfield(instruction, "kind", "type")) != "TimelineAddEntries" {
			continue
		}
		for _, entry := range arr(jfield(instruction, "entries")) {
			content := jfield(entry, "content")
			entryType := jfield(content, "entryType")
			// A missing discriminator decodes as the first variant.
			switch {
			case isNull(entryType) || str(entryType) == "TimelineTimelineItem":
				userResult := jpath(content, "itemContent", "userResults", "result")
				if str(jfield(userResult, "restId")) != "" {
					result.Content = append(result.Content, parseUserResult(userResult))
				}
			case str(entryType) == "TimelineTimelineCursor":
				if str(jfield(content, "cursorType")) == "Bottom" {
					result.Bottom = str(jfield(content, "value"))
				}
			}
		}
	}
}

func ParseGraphListMembers(js gjson.Result, cursor string) types.Result[types.User] {
	result := types.Result[types.User]{
		Beginning: cursor == "",
		Query:     types.Query{Kind: types.QueryUserList},
	}
	parseUserTimeline(&result, jpath(js, "data", "list", "membersTimeline", "timeline", "instructions"))
	return result
}

func ParseGraphFollowers(js gjson.Result, cursor string, kind types.QueryKind) types.Result[types.User] {
	result := types.Result[types.User]{
		Beginning: cursor == "",
		Query:     types.Query{Kind: kind},
	}
	if !js.IsObject() {
		return result
	}
	parseUserTimeline(&result, jpath(js, "data", "user", "result", "timeline", "timeline", "instructions"))
	return result
}
