// SPDX-License-Identifier: AGPL-3.0-only

package api

// Client for the public FxTwitter API (https://docs.fxembed.com/api/), which
// serves X data without Nitter's own account sessions.

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/gjson"

	"github.com/perennialtech/nitter/internal/parser"
	"github.com/perennialtech/nitter/internal/types"
	"github.com/perennialtech/nitter/internal/utils"
)

const (
	fxBase = "https://api.fxtwitter.com/2/"
	// https://docs.fxembed.com/api/introduction/#rate-limiting
	fxRequestsPerMinute = 1000
)

// fxError is a failed FxTwitter call; callers fall back to the sessions.
type fxError struct{ msg string }

func (e *fxError) Error() string { return e.msg }

var fxRate struct {
	sync.Mutex
	minute int64
	reqs   int
}

func newFxError(label, outcome, msg string) error {
	recordCall("fx", label, outcome)
	return &fxError{"FxTwitter " + label + ": " + msg}
}

// fxFailed reports whether err is an FxTwitter failure the caller should
// answer by falling back to the account sessions, and logs it.
func fxFailed(err error) bool {
	var fx *fxError
	if !errors.As(err, &fx) {
		return false
	}
	log.Printf("[fx] %s, falling back to sessions", fx.msg)
	return true
}

// fxReserve counts a request against FxTwitter's per-minute rate limit.
func fxReserve() bool {
	fxRate.Lock()
	defer fxRate.Unlock()
	if minute := time.Now().Unix() / 60; minute != fxRate.minute {
		fxRate.minute = minute
		fxRate.reqs = 0
	}
	if fxRate.reqs >= fxRequestsPerMinute {
		return false
	}
	fxRate.reqs++
	recordFxMinute(fxRate.reqs)
	return true
}

// fxGet requests an FxTwitter endpoint. label names it in the call stats. It
// fails with an *fxError for anything but a 200, or a 404 when allowNotFound
// is set.
func fxGet(ctx context.Context, label, path string, params []param,
	allowNotFound bool) (gjson.Result, error) {
	if !fxReserve() {
		return gjson.Result{}, newFxError(label, "limited", "over the per-minute rate limit")
	}

	target := fxBase + path
	if len(params) > 0 {
		target += "?" + encodeQuery(params)
	}
	h := http.Header{}
	h.Set("accept", "application/json")
	h.Set("user-agent", "Nitter")
	resp, err := doRequest(ctx, http.MethodGet, target, h, "", apiTimeout)
	if err != nil {
		if ctx.Err() != nil {
			return gjson.Result{}, ctx.Err()
		}
		return gjson.Result{}, newFxError(label, "error", err.Error())
	}

	if resp.status == http.StatusTooManyRequests {
		// Stop sending requests until the next minute window.
		fxRate.Lock()
		fxRate.reqs = fxRequestsPerMinute
		fxRate.Unlock()
		return gjson.Result{}, newFxError(label, "limited", "429")
	}

	if !gjson.Valid(resp.body) {
		return gjson.Result{}, newFxError(label, "error", strconv.Itoa(resp.status)+", body is not JSON")
	}
	js := gjson.Parse(resp.body)

	// The body's code mirrors the HTTP status and is set even on a 200.
	switch getIntOr(js.Get("code"), resp.status) {
	case 200:
		recordCall("fx", label, "ok")
		return js, nil
	case 404:
		if !allowNotFound {
			return gjson.Result{}, newFxError(label, "notfound", getStr(js.Get("message")))
		}
		recordCall("fx", label, "notfound")
		return js, nil
	}
	return gjson.Result{}, newFxError(label, "error",
		strconv.Itoa(resp.status)+", "+getStr(js.Get("message")))
}

// fxImage drops the scheme and host Nitter doesn't store for pbs.twimg.com.
func fxImage(u string) string {
	u = strings.TrimPrefix(u, utils.HTTPS)
	return strings.TrimPrefix(u, utils.Twimg)
}

func fxAvatar(u string) string {
	return strings.NewReplacer("_normal", "", "_200x200", "").Replace(fxImage(u))
}

func parseFxUser(js gjson.Result) types.User {
	if !js.IsObject() {
		return types.User{}
	}
	u := types.User{
		ID:        getStr(js.Get("id")),
		Username:  getStr(js.Get("screen_name")),
		Fullname:  getStr(js.Get("name")),
		Location:  getStr(js.Get("location")),
		Website:   getStr(js.Get("website.url")),
		UserPic:   fxAvatar(getStr(js.Get("avatar_url"))),
		Following: getInt(js.Get("following")),
		Followers: getInt(js.Get("followers")),
		Tweets:    getInt(js.Get("statuses")),
		Likes:     getInt(js.Get("likes")),
		Media:     getInt(js.Get("media_count")),
		Protected: getBool(js.Get("protected")),
		JoinDate:  parser.GetTime(js.Get("joined")),
	}
	if banner := fxImage(getStr(js.Get("banner_url"))); banner != "" {
		u.Banner = banner + "/1500x500"
	}
	if getBool(js.Get("verification.verified")) {
		switch getStr(js.Get("verification.type")) {
		case "government":
			u.VerifiedType = types.VerifiedGovernment
		case "organization", "business":
			u.VerifiedType = types.VerifiedBusiness
		default:
			u.VerifiedType = types.VerifiedBlue
		}
	}
	parser.ExpandFxBio(&u, js.Get("raw_description"))
	return u
}

func parseFxVideo(js gjson.Result) types.Video {
	v := types.Video{
		Available:  true,
		Thumb:      fxImage(getStr(js.Get("thumbnail_url"))),
		Title:      getStr(js.Get("altText")),
		DurationMs: int(getFloat(js.Get("duration")) * 1000),
	}
	for _, f := range items(js.Get("formats")) {
		u := getStr(f.Get("url"))
		if getStr(f.Get("container")) == "m3u8" {
			v.Variants = append(v.Variants, types.VideoVariant{ContentType: types.VideoM3u8, URL: u})
		} else {
			v.Variants = append(v.Variants, types.VideoVariant{
				ContentType: types.VideoMp4,
				URL:         u,
				Bitrate:     getInt(f.Get("bitrate")),
				Resolution:  parser.GetMp4Resolution(u),
			})
		}
	}
	return v
}

func parseFxMedia(js gjson.Result, t *types.Tweet) {
	for _, m := range items(js.Get("all")) {
		switch getStr(m.Get("type")) {
		case "photo":
			u, _, _ := strings.Cut(getStr(m.Get("url")), "?")
			t.Media = append(t.Media, types.Media{Kind: types.PhotoMedia, Photo: types.Photo{
				URL:     fxImage(u),
				AltText: getStr(m.Get("altText")),
			}})
		case "video":
			t.Media = append(t.Media, types.Media{Kind: types.VideoMedia, Video: parseFxVideo(m)})
		case "gif":
			t.Media = append(t.Media, types.Media{Kind: types.GifMedia, Gif: types.Gif{
				URL:     fxImage(getStr(m.Get("url"))),
				Thumb:   fxImage(getStr(m.Get("thumbnail_url"))),
				AltText: getStr(m.Get("altText")),
			}})
		}
	}
}

// nimDuration renders d the way Nim's $Duration does, e.g.
// "1 week, 2 days, and 3 hours".
func nimDuration(d time.Duration) string {
	units := []struct {
		name string
		size time.Duration
	}{
		{"week", 7 * 24 * time.Hour}, {"day", 24 * time.Hour}, {"hour", time.Hour},
		{"minute", time.Minute}, {"second", time.Second}, {"millisecond", time.Millisecond},
		{"microsecond", time.Microsecond}, {"nanosecond", time.Nanosecond},
	}
	var parts []string
	for _, u := range units {
		n := d / u.size
		d -= n * u.size
		if n == 0 {
			continue
		}
		name := u.name
		if n != 1 && n != -1 {
			name += "s"
		}
		parts = append(parts, strconv.FormatInt(int64(n), 10)+" "+name)
	}
	switch len(parts) {
	case 0:
		return "0 nanoseconds"
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + ", and " + parts[len(parts)-1]
}

func parseFxPoll(js gjson.Result) *types.Poll {
	p := &types.Poll{}
	for _, c := range items(js.Get("choices")) {
		p.Options = append(p.Options, getStr(c.Get("label")))
		p.Values = append(p.Values, getInt(c.Get("count")))
	}
	p.Votes = getInt(js.Get("total_votes"))
	if len(p.Values) > 0 {
		p.Leader = slices.Index(p.Values, slices.Max(p.Values))
	}

	p.Status = "Final results"
	ends, err := time.Parse("2006-01-02T15:04:05.000Z", getStr(js.Get("ends_at")))
	if now := time.Now(); err == nil && ends.After(now) {
		timeLeft := nimDuration(ends.Sub(now))
		p.Status = timeLeft[:max(0, strings.Index(timeLeft, ","))]
		if p.Status == "" {
			p.Status = timeLeft
		}
	}
	return p
}

func parseFxCard(js gjson.Result, embedCard string) *types.Card {
	kind := types.CardSummaryLarge
	if embedCard == "summary" {
		kind = types.CardSummary
	}
	return &types.Card{
		Kind:  kind,
		URL:   getStr(js.Get("url")),
		Dest:  getStr(js.Get("domain")),
		Title: getStr(js.Get("title")),
		Text:  getStr(js.Get("description")),
		Image: fxImage(getStr(js.Get("image.url"))),
	}
}

func parseFxTweet(js gjson.Result) *types.Tweet {
	if !js.IsObject() {
		return &types.Tweet{}
	}
	if getStr(js.Get("type")) == "tombstone" {
		return &types.Tweet{Text: getStr(js.Get("message"))}
	}

	t := &types.Tweet{
		ID:        parser.GetID(js.Get("id")),
		ReplyID:   parser.GetID(js.Get("replying_to.status")),
		User:      parseFxUser(js.Get("author")),
		Time:      time.Unix(js.Get("created_timestamp").Int(), 0).UTC(),
		Available: true,
		Stats: types.TweetStats{
			Replies:  getInt(js.Get("replies")),
			Retweets: getInt(js.Get("reposts")),
			Quotes:   getInt(js.Get("quotes")),
			Likes:    getInt(js.Get("likes")),
			Views:    getInt(js.Get("views")),
			Source:   getStr(js.Get("source")),
		},
	}

	if quote := js.Get("quote"); notNull(quote) {
		t.Quote = parseFxTweet(quote)
	}
	if poll := js.Get("poll"); notNull(poll) {
		t.Poll = parseFxPoll(poll)
	}
	if card := js.Get("card"); notNull(card) {
		t.Card = parseFxCard(card, getStr(js.Get("embed_card")))
	}
	if media := js.Get("media"); notNull(media) {
		parseFxMedia(media, t)
	}
	if note := js.Get("community_note"); notNull(note) {
		t.Note = parser.ExpandFxNote(note)
	}

	article := js.Get("article")
	hasArticle := getStr(article.Get("title")) != ""
	if hasArticle {
		t.ArticlePreview = &types.ArticlePreview{
			Title:       getStr(article.Get("title")),
			PreviewText: getStr(article.Get("preview_text")),
			CoverImage:  fxImage(getStr(article.Get("cover_media.media_info.original_img_url"))),
			TweetID:     t.ID,
		}
	}

	parser.ExpandFxText(t, js.Get("raw_text"), getStr(js.Get("replying_to.screen_name")),
		t.Quote != nil, hasArticle)

	// Reposts arrive as the original post, naming the reposter.
	if reposter := js.Get("reposted_by"); notNull(reposter) {
		return &types.Tweet{
			ID:        t.ID,
			Time:      t.Time,
			Available: true,
			User: types.User{
				ID:       getStr(reposter.Get("id")),
				Username: getStr(reposter.Get("screen_name")),
				Fullname: getStr(reposter.Get("name")),
				UserPic:  fxAvatar(getStr(reposter.Get("avatar_url"))),
			},
			Retweet: t,
		}
	}
	return t
}

func parseFxTimeline(js gjson.Result, after string) types.Timeline {
	tl := types.Timeline{Beginning: after == "", Bottom: getStr(js.Get("cursor.bottom"))}
	for _, status := range items(js.Get("results")) {
		if t := parseFxTweet(status); t.ID != 0 {
			tl.Content = append(tl.Content, types.Tweets{t})
		}
	}
	return tl
}

func parseFxUsers(js gjson.Result, after string, kind types.QueryKind) types.Result[types.User] {
	res := types.Result[types.User]{
		Beginning: after == "",
		Query:     types.Query{Kind: kind},
		Bottom:    getStr(js.Get("cursor.bottom")),
	}
	for _, u := range items(js.Get("results")) {
		if parsed := parseFxUser(u); parsed.ID != "" {
			res.Content = append(res.Content, parsed)
		}
	}
	return res
}

func pageParams(count int, after string) []param {
	ps := []param{{"count", strconv.Itoa(count)}}
	if after != "" {
		ps = append(ps, param{"cursor", after})
	}
	return ps
}

// fxUser fetches a profile. handle is a username, or "id:<rest_id>".
func fxUser(ctx context.Context, handle string) (types.User, error) {
	js, err := fxGet(ctx, "profile", "profile/"+url.QueryEscape(handle), nil, true)
	if err != nil {
		return types.User{}, err
	}
	if js.Get("code").Int() == 404 {
		if getStr(js.Get("reason")) == "suspended" {
			return types.User{Suspended: true}, nil
		}
		return types.User{}, nil
	}
	return parseFxUser(js.Get("user")), nil
}

// fxUserTimeline fetches a profile tab, "statuses" or "media".
func fxUserTimeline(ctx context.Context, userID, tab string, count int, after string,
	params ...param) (types.Timeline, error) {
	js, err := fxGet(ctx, "profile/"+tab, "profile/id:"+userID+"/"+tab,
		append(pageParams(count, after), params...), false)
	if err != nil {
		return types.Timeline{}, err
	}
	return parseFxTimeline(js, after), nil
}

// fxSearch searches posts. feed is "latest", "top" or "media".
func fxSearch(ctx context.Context, query, feed string, count int, after string) (types.Timeline, error) {
	js, err := fxGet(ctx, "search", "search",
		append([]param{{"q", query}, {"feed", feed}}, pageParams(count, after)...), false)
	if err != nil {
		return types.Timeline{}, err
	}
	return parseFxTimeline(js, after), nil
}

func fxUserSearch(ctx context.Context, query string, count int, after string) (types.Result[types.User], error) {
	js, err := fxGet(ctx, "search/users", "search/users",
		append([]param{{"q", query}}, pageParams(count, after)...), false)
	if err != nil {
		return types.Result[types.User]{}, err
	}
	return parseFxUsers(js, after, types.QueryUsers), nil
}

// fxConnections lists followers or following; kind is QueryFollowers or
// QueryFollowing.
func fxConnections(ctx context.Context, userID string, kind types.QueryKind, count int,
	after string) (types.Result[types.User], error) {
	tab := string(kind)
	js, err := fxGet(ctx, "profile/"+tab, "profile/id:"+userID+"/"+tab, pageParams(count, after), false)
	if err != nil {
		return types.Result[types.User]{}, err
	}
	return parseFxUsers(js, after, kind), nil
}

func fxReposters(ctx context.Context, id string, count int, after string) (types.UsersTimeline, error) {
	js, err := fxGet(ctx, "status/reposts", "status/"+id+"/reposts", pageParams(count, after), false)
	if err != nil {
		return types.UsersTimeline{}, err
	}
	return parseFxUsers(js, after, types.QueryUsers), nil
}
