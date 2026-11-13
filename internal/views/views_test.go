// SPDX-License-Identifier: AGPL-3.0-only

package views

import (
	"encoding/xml"
	"html/template"
	"io"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"github.com/perennialtech/nitter/internal/config"
	"github.com/perennialtech/nitter/internal/flags"
	"github.com/perennialtech/nitter/internal/prefs"
	"github.com/perennialtech/nitter/internal/types"
	"github.com/perennialtech/nitter/internal/utils"
)

func TestMain(m *testing.M) {
	if err := Init("../../public"); err != nil {
		panic(err)
	}
	m.Run()
}

// --- a minimal CSS selector matcher for the selectors in tests/base.py ---

type simpleSel struct {
	tag     string
	classes []string
	attrs   [][3]string // name, op ("=" or "*="), value
}

var selPart = regexp.MustCompile(`^([a-z0-9]*)((?:\.[\w-]+)*)((?:\[[\w-]+\*?="[^"]*"\])*)$`)
var selAttr = regexp.MustCompile(`\[([\w-]+)(\*?=)"([^"]*)"\]`)

func parseSel(s string) []simpleSel {
	var parts []string
	depth, start := 0, 0
	for i := 0; i <= len(s); i++ {
		switch {
		case i < len(s) && s[i] == '[':
			depth++
		case i < len(s) && s[i] == ']':
			depth--
		case (i == len(s) || s[i] == ' ') && depth == 0:
			if i > start {
				parts = append(parts, s[start:i])
			}
			start = i + 1
		}
	}
	var out []simpleSel
	for _, part := range parts {
		m := selPart.FindStringSubmatch(part)
		if m == nil {
			panic("unsupported selector " + part)
		}
		sel := simpleSel{tag: m[1]}
		for _, c := range strings.Split(m[2], ".")[1:] {
			sel.classes = append(sel.classes, c)
		}
		for _, a := range selAttr.FindAllStringSubmatch(m[3], -1) {
			sel.attrs = append(sel.attrs, [3]string{a[1], a[2], a[3]})
		}
		out = append(out, sel)
	}
	return out
}

func attrOf(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

func (s simpleSel) match(n *html.Node) bool {
	if n.Type != html.ElementNode || s.tag != "" && n.Data != s.tag {
		return false
	}
	cls, _ := attrOf(n, "class")
	have := strings.Fields(cls)
	for _, c := range s.classes {
		found := false
		for _, h := range have {
			found = found || h == c
		}
		if !found {
			return false
		}
	}
	for _, a := range s.attrs {
		v, ok := attrOf(n, a[0])
		if !ok || a[1] == "=" && v != a[2] || a[1] == "*=" && !strings.Contains(v, a[2]) {
			return false
		}
	}
	return true
}

func matches(n *html.Node, sels []simpleSel) bool {
	last := len(sels) - 1
	if !sels[last].match(n) {
		return false
	}
	i := last - 1
	for p := n.Parent; p != nil && i >= 0; p = p.Parent {
		if sels[i].match(p) {
			i--
		}
	}
	return i < 0
}

func queryAll(root *html.Node, selector string) []*html.Node {
	sels := parseSel(selector)
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if matches(n, sels) {
			out = append(out, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return out
}

func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func parse(t *testing.T, s template.HTML) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(string(s)))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func requireSel(t *testing.T, doc *html.Node, selectors ...string) {
	t.Helper()
	for _, s := range selectors {
		if len(queryAll(doc, s)) == 0 {
			t.Errorf("no element matches %q", s)
		}
	}
}

// --- sample data ---

var epoch = time.Unix(0, 0).UTC()

func nimUser() types.User {
	return types.User{Username: "nim_lang", Fullname: "Nim", JoinDate: epoch}
}

func richTweet() *types.Tweet {
	return &types.Tweet{
		ID: 1234567890, Available: true, Time: epoch,
		User:  types.User{Username: "jack", Fullname: "jack", VerifiedType: types.VerifiedBlue},
		Text:  `just setting up my <a href="https://x.com/i">twttr</a>`,
		Reply: []string{"biz", "ev"},
		Stats: types.TweetStats{Replies: 1, Retweets: 1200, Likes: 3, Source: "Twitter for iPhone"},
		Media: []types.Media{
			{Kind: types.PhotoMedia, Photo: types.Photo{URL: "https://pbs.twimg.com/media/a.jpg", AltText: "alt"}},
			{Kind: types.PhotoMedia, Photo: types.Photo{URL: "https://pbs.twimg.com/media/b.jpg"}},
			{Kind: types.VideoMedia, Video: types.Video{
				Available: true, PlaybackType: types.VideoMp4, DurationMs: 61000,
				Thumb: "https://pbs.twimg.com/thumb.jpg",
				Variants: []types.VideoVariant{
					{ContentType: types.VideoMp4, URL: "https://video.twimg.com/low.mp4", Bitrate: 1, Resolution: 360},
					{ContentType: types.VideoMp4, URL: "https://video.twimg.com/high.mp4", Bitrate: 9, Resolution: 720},
				},
			}},
		},
		Card: &types.Card{
			Kind: types.CardSummaryLarge, URL: "https://example.com/a_(b)", Title: "Card title",
			Text: "Card text", Dest: "example.com", Image: "https://pbs.twimg.com/card_img/c.jpg",
		},
		Poll: &types.Poll{Options: []string{"Yes", "No"}, Values: []int{3, 1}, Votes: 4, Leader: 0, Status: "Final results"},
		Quote: &types.Tweet{
			ID: 20, Available: true, Time: epoch, Text: "quoted text",
			User:  types.User{Username: "quoted", Fullname: "Quoted"},
			Media: []types.Media{{Kind: types.GifMedia, Gif: types.Gif{URL: "https://video.twimg.com/g.mp4", Thumb: "https://pbs.twimg.com/g.jpg"}}},
		},
	}
}

// --- ports of tests/test_timeline.nim ---

func TestPinnedTweetRendering(t *testing.T) {
	tweets := types.Timeline{Beginning: true}
	pinned := &types.Tweet{
		ID: 1, Text: "Pinned regression tweet", Pinned: true, Time: epoch, User: nimUser(),
	}

	t.Run("an absent pinned tweet is not rendered", func(t *testing.T) {
		if strings.Contains(string(RenderTimelineTweets(tweets, &prefs.Prefs{}, "/nim_lang", nil)), "Pinned regression tweet") {
			t.Fail()
		}
	})
	t.Run("a pinned tweet is rendered", func(t *testing.T) {
		if !strings.Contains(string(RenderTimelineTweets(tweets, &prefs.Prefs{}, "/nim_lang", pinned)), "Pinned regression tweet") {
			t.Fail()
		}
	})
	t.Run("hidden pinned tweets are not rendered", func(t *testing.T) {
		if strings.Contains(string(RenderTimelineTweets(tweets, &prefs.Prefs{HidePins: true}, "/nim_lang", pinned)), "Pinned regression tweet") {
			t.Fail()
		}
	})
	t.Run("search rendering forwards the pinned tweet", func(t *testing.T) {
		if !strings.Contains(string(RenderTweetSearch(tweets, &prefs.Prefs{}, "/nim_lang", pinned)), "Pinned regression tweet") {
			t.Fail()
		}
	})
	t.Run("profile rendering unwraps the pinned tweet", func(t *testing.T) {
		profile := &types.Profile{User: pinned.User, Tweets: tweets, Pinned: pinned}
		if !strings.Contains(string(RenderProfile(profile, &prefs.Prefs{}, "/nim_lang")), "Pinned regression tweet") {
			t.Fail()
		}
	})
}

// --- port of the views part of tests/test_upstream_features.nim ---

func TestTranslateOnDemandForm(t *testing.T) {
	utils.Init("translation-test-key", false)
	defer utils.Init("secretkey", false)
	note := utils.TranslationSignature("123", true)
	post := utils.TranslationSignature("123", false)
	form := string(RenderTranslateOnDemandForm("/i/birdwatch/n/123/translate", "123", true, ""))
	if note == post || note == utils.TranslationSignature("124", true) {
		t.Error("signatures must differ by content type and id")
	}
	for _, want := range []string{
		`method="post"`, `action="/i/birdwatch/n/123/translate"`, `name="sig" value="` + note + `"`,
	} {
		if !strings.Contains(form, want) {
			t.Errorf("form lacks %s:\n%s", want, form)
		}
	}
}

// --- render tests against the selectors in tests/base.py ---

func TestTweetSelectors(t *testing.T) {
	p := prefs.Defaults
	tw := richTweet()
	doc := parse(t, RenderTweet(tw, &p, "/jack/status/1234567890", TweetOpts{}))
	requireSel(t, doc,
		".tweet-header .fullname", ".tweet-header .username", ".tweet-header .tweet-date",
		".tweet-content.media-body", ".replying-to", ".verified-icon",
		".card a", ".card .card-title", ".card .card-description", ".card .card-destination", ".card .card-image",
		".card.large",
		".quote .quote-link", ".quote .fullname-and-username .fullname", ".quote .fullname-and-username .username",
		".quote .quote-text", ".quote .quote-media-container", ".quote .media-gif",
		".poll-meter", ".poll-info", ".poll-meter.leader",
		".attachments", ".gallery-row", ".still-image", ".gallery-row.mixed-row",
		`a.tweet-link[href="/jack/status/1234567890#m"]`,
		`a[href="/jack/status/1234567890/retweets"]`,
		`.video-download[href*="/video/"]`,
	)
	if got := textOf(queryAll(doc, ".tweet-header .username")[0]); got != "@jack" {
		t.Errorf("username = %q", got)
	}
	if got := textOf(queryAll(doc, ".poll-info")[0]); got != "4 votes • Final results" {
		t.Errorf("poll info = %q", got)
	}
	if got := strings.TrimSpace(textOf(queryAll(doc, ".poll-choice-value")[0])); got != "75%" {
		t.Errorf("poll value = %q", got)
	}
	// verbatim tweet text keeps its markup
	if len(queryAll(doc, `.tweet-content a[href="https://x.com/i"]`)) != 1 {
		t.Error("tweet text link missing")
	}
	// Karax emitted external URLs as-is: no #ZgotmplZ
	if s := string(RenderTweet(tw, &p, "", TweetOpts{})); strings.Contains(s, "ZgotmplZ") {
		t.Error("html/template filtered a URL")
	}
	// the source drops the "Twitter for " prefix
	if got := textOf(queryAll(doc, ".tweet-stat.source")[0]); got != "iPhone" {
		t.Errorf("source = %q", got)
	}
	// single video renders as .gallery-video
	tw.Media = tw.Media[2:]
	requireSel(t, parse(t, RenderTweet(tw, &p, "", TweetOpts{})), ".attachments.card .gallery-video video source")
}

func TestRetweetAndUnavailable(t *testing.T) {
	p := prefs.Defaults
	rt := &types.Tweet{
		ID: 9, Available: true, User: types.User{Username: "rter", Fullname: "Retweeter"},
		Retweet: &types.Tweet{ID: 8, Available: true, User: types.User{Username: "orig", Fullname: "Orig"}, Text: "hi"},
	}
	doc := parse(t, RenderTweet(rt, &p, "", TweetOpts{}))
	requireSel(t, doc, ".retweet-header", `div[data-username="orig"]`)
	if !strings.Contains(textOf(queryAll(doc, ".retweet-header")[0]), "Retweeter retweeted") {
		t.Error("retweet header text")
	}

	gone := &types.Tweet{ID: 7, User: types.User{Username: "x"}, Tombstone: "This Tweet is from a suspended account."}
	doc = parse(t, RenderTweet(gone, &p, "", TweetOpts{Last: true}))
	requireSel(t, doc, `div[class="thread-last unavailable timeline-item"] a.unavailable-box`)

	q := &types.Tweet{ID: 6, Available: true, User: types.User{Username: "y"}, Quote: &types.Tweet{ID: 5}}
	requireSel(t, parse(t, RenderTweet(q, &p, "", TweetOpts{})), ".quote.unavailable")
}

func TestProfileSelectors(t *testing.T) {
	p := prefs.Defaults
	u := types.User{
		Username: "mobile_test", Fullname: "Test", Bio: "bio <b>x</b>", Location: "Earth:abc",
		Website: "https://example.com", Banner: "https://pbs.twimg.com/profile_banners/1/2",
		UserPic: "https://pbs.twimg.com/profile_images/1/a_normal.jpg", Protected: true,
		VerifiedType: types.VerifiedBusiness, JoinDate: epoch, Media: 1234, Tweets: 1500,
	}
	profile := &types.Profile{
		User:      u,
		PhotoRail: types.PhotoRail{{URL: "https://pbs.twimg.com/media/x.jpg", TweetID: "5"}},
		Tweets:    types.Timeline{Beginning: true},
	}
	doc := parse(t, RenderProfile(profile, &p, "/mobile_test"))
	requireSel(t, doc,
		".profile-card-fullname", ".profile-card-username", ".icon-lock", ".verified-icon.business",
		".profile-banner", ".profile-bio", ".profile-location", ".profile-website", ".profile-joindate",
		".photo-rail-header", ".photo-rail-grid", ".timeline-protected",
		`.profile-joindate a[href="/mobile_test/about"]`,
		`.profile-location a[href="/search?f=tweets&q=place:abc"]`,
	)
	if got := textOf(queryAll(doc, ".profile-statlist .posts .profile-stat-num")[0]); got != "1,500" {
		t.Errorf("tweet count = %q", got)
	}

	profile.User.Protected = false
	profile.Tweets.Query = types.Query{Kind: types.QueryMedia}
	doc = parse(t, RenderProfile(profile, &p, "/mobile_test/media"))
	requireSel(t, doc, ".media-view-tabs", `.media-view-tabs a[href*="view=grid"]`,
		".media-view-tabs .tab-item.active a", ".tab .tab-item.active a", ".timeline-none")
}

func TestConversationSelectors(t *testing.T) {
	p := prefs.Defaults
	tw := func(id int64, text string) *types.Tweet {
		return &types.Tweet{ID: id, Available: true, Text: text, Time: epoch,
			User: types.User{Username: "u", Fullname: "U"}}
	}
	conv := types.Conversation{
		Tweet:  tw(2, "main"),
		Before: types.Chain{Content: types.Tweets{tw(1, "before")}},
		After:  types.Chain{Content: types.Tweets{tw(3, "after")}, HasMore: true},
		Replies: types.Result[types.Chain]{
			Beginning: true, Bottom: "cursor value",
			Content: []types.Chain{{Content: types.Tweets{tw(4, "reply")}}},
		},
	}
	conv.Tweet.Stats.Replies = 10
	doc := parse(t, RenderConversation(conv, &p, "/u/status/2", types.RankingRecency))
	requireSel(t, doc, ".main-tweet", ".before-tweet", ".after-tweet", ".replies", ".reply",
		".conversation .timeline-item", ".tweet-content", ".reply-sort", ".reply-sort-option.active",
		`.reply-sort-option[href="?sort=Recency#r"]`,
		`.replies .show-more a[href="?sort=Recency&cursor=cursor%20value#r"]`,
		`div[class="show-more"]`, ".after-tweet .more-replies",
		".main-tweet .tweet-published",
	)
	if len(queryAll(doc, ".main-tweet a.tweet-link")) != 0 {
		t.Error("main tweet must not have a tweet-link")
	}
}

func TestTimelineSelectors(t *testing.T) {
	p := prefs.Defaults
	tl := types.Timeline{
		Query:  types.Query{Kind: types.QueryTweets, Text: "nasa"},
		Bottom: "abc",
		Content: []types.Tweets{
			{{ID: 1, Available: true, User: types.User{Username: "a"}}},
			{{ID: 3, ReplyID: 2, Available: true, User: types.User{Username: "a"}},
				{ID: 5, ReplyID: 9, Available: true, User: types.User{Username: "a"}}},
		},
	}
	doc := parse(t, RenderTweetSearch(tl, &p, "/search?f=tweets&q=nasa", nil))
	requireSel(t, doc, `div[class="timeline-item show-more"]`, `div[class="show-more"]`,
		".thread-line .more-replies-thread", ".thread-last",
		`.tab-item a[href="?f=top&q=nasa"]`, `.tab-item a[href="?f=media&q=nasa"]`,
		`.tab-item a[href="?f=tweets&q=nasa"]`, `.tab-item a[href="?f=users&q=nasa"]`,
		`.tab-item a[href="?f=lists&q=nasa"]`, `.search-field input[name="f"][value="tweets"]`,
		`.show-more a[href="?f=tweets&q=nasa&cursor=abc"]`, ".tab .tab-item.active a",
	)

	tl.Content = nil
	requireSel(t, parse(t, RenderTimelineTweets(tl, &p, "/search", nil)), ".timeline-end")
	tl.Beginning = true
	requireSel(t, parse(t, RenderTimelineTweets(tl, &p, "/search", nil)), ".timeline-none")

	tl.Query = types.Query{Kind: types.QueryMedia, View: "gallery"}
	tl.Content = []types.Tweets{{{ID: 1, Available: true}}}
	requireSel(t, parse(t, RenderTweetSearch(tl, &p, "/search", nil)),
		".timeline.media-gallery-view .gallery-masonry", ".timeline-container.media-only")
}

func TestPages(t *testing.T) {
	cfg, err := config.FromFile(config.File{})
	if err != nil {
		t.Fatal(err)
	}
	p := prefs.Defaults
	p.Theme = "Catppuccin Frappe"
	r := httptest.NewRequest("GET", "/jack/status/1?x=1&name=ignored", nil)
	r.SetPathValue("name", "jack")

	page := RenderMain(RenderSearch(), r, cfg, &p, PageOpts{TitleText: "T", RSS: "/jack/rss", Images: []string{"https://pbs.twimg.com/a_400x400.jpg"}})
	doc := parse(t, template.HTML(page))
	requireSel(t, doc, `link[href="/css/themes/catppuccin_frappe.css"]`, ".search-bar input",
		`.icon-cog[href="/settings?referer=%2Fjack%2Fstatus%2F1%3Fx%3D1%23m"]`,
		`.icon-rss[href="/jack/rss"]`, `meta[content="summary"]`, `body.fixed-nav`, ".container .panel-container")
	if !strings.HasPrefix(page, "<!DOCTYPE html>") || !strings.Contains(page, "<title>T | Nitter</title>") {
		t.Error("main page shell")
	}

	about := string(RenderAbout())
	if !strings.Contains(about, "https://github.com/perennialtech/nitter/commit/") ||
		!strings.Contains(about, "<h1>About</h1>") {
		t.Errorf("about page:\n%s", about)
	}

	settings := string(RenderPreferences(&p, "/", []string{"Catppuccin Frappe", "Nitter"}, "https://nitter.net/?prefs=x"))
	for _, want := range []string{`<option value="Catppuccin Frappe" selected="">`, `name="hlsPlayback"`, `action="/resetprefs"`} {
		if !strings.Contains(settings, want) {
			t.Errorf("settings lack %s", want)
		}
	}
	flags.GuestAuth = true
	if !strings.Contains(string(RenderPreferences(&p, "/", nil, "")), "guestSearchFallback") {
		t.Error("guest mode shows guestSearchFallback")
	}
	flags.GuestAuth = false

	tw := richTweet()
	for name, s := range map[string]string{
		"tweet embed": RenderTweetEmbed(tw, "/jack/status/1", &p, cfg, r),
		"video embed": RenderVideoEmbed(tw, cfg, r),
		"error embed": RenderErrorEmbed("Tweet not found", &p, cfg, r, "1", ""),
	} {
		if !strings.Contains(s, "window.parent.postMessage") || strings.Contains(s, "ZgotmplZ") {
			t.Errorf("%s:\n%s", name, s)
		}
	}
	requireSel(t, parse(t, template.HTML(RenderVideoEmbed(tw, cfg, r))), ".embed-video .gallery-video",
		`meta[property="og:video:url"]`)
}

func TestOtherViews(t *testing.T) {
	p := prefs.Defaults
	p.HlsPlayback = true
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for name, h := range map[string]template.HTML{
		"space": RenderSpace(types.AudioSpace{
			Title: "INTEL WILL MOON NEXT WEEK", M3u8URL: "https://x/a.m3u8", StartTime: start, EndTime: start.Add(time.Hour),
			Creator: types.User{Username: "h"}, Admins: []types.SpaceParticipant{{Username: "h"}},
		}, &p, "/i/spaces/1"),
		"broadcast": RenderBroadcast(types.Broadcast{State: "RUNNING", M3u8URL: "https://x/a.m3u8", Title: "b"}, &p, "/"),
		"about account": RenderAboutAccount(types.AccountInfo{
			Username: "jack", JoinDate: start, VerifiedType: types.VerifiedBlue, OverrideVerifiedYear: -5,
			UsernameChanges: 2,
		}),
		"community": RenderCommunity(RenderCommunityAbout(types.Community{
			ID: "1", Name: "C", JoinPolicy: "Open", CreatedAt: start, Rules: []types.CommunityRule{{Name: "r"}},
		}, []types.User{{Username: "m"}}), RenderCommunityTabs(types.QueryUserList, types.Community{ID: "1", Hashtags: []string{"#go"}}), types.Community{ID: "1"}),
		"list": RenderList(RenderTimelineUsers(types.UsersTimeline{Beginning: true}, &p, ""),
			types.Query{Kind: types.QueryUserList}, types.List{ID: "1", Name: "L", Username: "o"}),
		"notes": RenderBirdwatchNotes(richTweet(), types.BirdwatchNotes{Notes: []types.BirdwatchNote{
			{ID: "9", Text: "see https://x.com/a", Helpful: true, CreatedAt: start},
		}}, &p, "/i/birdwatch/t/1", false),
		"history":  RenderBirdwatchHistory(types.BirdwatchHistory{Alias: "a", Bottom: "c d"}),
		"trends":   RenderHomeBody("US", []types.Trend{{Name: "#go", SearchQuery: "#go lang", TweetVolume: -1}}, "World", nil, RenderSearch()),
		"lists":    RenderListSearch(types.Result[types.ListSearchResult]{Content: []types.ListSearchResult{{List: types.List{ID: "1", Name: "L", Members: 1200}, FollowersContext: "65 followers including @jack", Facepiles: []string{"https://pbs.twimg.com/f.jpg"}}}}, &p, "/search"),
		"users":    RenderUserList(nimUser(), types.UsersTimeline{Beginning: true, Content: []types.User{nimUser()}, Bottom: "x"}, &p, "/nim_lang/followers", "Followers"),
		"edits":    RenderEditHistory(types.EditHistory{Latest: richTweet(), History: types.Tweets{richTweet()}}, &p, "/"),
		"error":    RenderError("<b>boom</b>"),
		"feature":  RenderFeature(),
		"retweets": RenderRetweetersUnavailable("jack", "1"),
	} {
		s := string(h)
		if strings.Contains(s, "ZgotmplZ") || strings.Contains(s, "<no value>") || s == "" {
			t.Errorf("%s:\n%s", name, s)
		}
	}
	if s := string(RenderSpace(types.AudioSpace{Admins: []types.SpaceParticipant{{Username: "h"}}, Creator: types.User{Username: "h"}}, &p, "")); !strings.Contains(s, `class="host-badge">Host<`) {
		t.Errorf("space host badge:\n%s", s)
	}
	if s := string(RenderHomeBody("US", []types.Trend{{Name: "#go", SearchQuery: "#go lang", TweetVolume: 3}}, "W", nil, "")); !strings.Contains(s, `href="/search?q=%23go&#43;lang"`) &&
		!strings.Contains(s, `href="/search?q=%23go+lang"`) {
		t.Errorf("trend link:\n%s", s)
	}
	if s := string(RenderListSearch(types.Result[types.ListSearchResult]{Content: []types.ListSearchResult{{List: types.List{ID: "1"}, FollowersContext: "65 followers including @jack", Facepiles: []string{"f"}}}}, &p, "")); !strings.Contains(s, `<a class="facepile-link" href="/jack">`) {
		t.Errorf("facepile link:\n%s", s)
	}
}

func TestArticle(t *testing.T) {
	p := prefs.Defaults
	a := &types.Article{
		Title: "Title", CoverImage: "https://pbs.twimg.com/cover.jpg", User: nimUser(), Time: epoch,
		Paragraphs: []types.ArticleParagraph{
			{Kind: "unstyled", Text: "Hi @nim_lang, see link & bold",
				InlineStyles: []types.ArticleStyle{{Offset: 25, Length: 4, Style: "Bold"}},
				EntityRanges: []types.ArticleEntityRange{{Offset: 18, Length: 4, Key: 1}}},
			{Kind: "unordered-list-item", Text: "one"},
			{Kind: "unordered-list-item", Text: "two"},
			{Kind: "atomic", EntityRanges: []types.ArticleEntityRange{{Key: 2}}},
			{Kind: "blockquote", Text: "quote — me", InlineStyles: []types.ArticleStyle{{Offset: 6, Length: 4, Style: "Bold"}}},
		},
		Entities: map[int]types.ArticleEntity{
			1: {Kind: "LINK", URL: "https://x.com/nim_lang/status/1"},
			2: {Kind: "DIVIDER"},
		},
	}
	s := string(RenderArticle(a, nil, "/i/article/1", &p, "1"))
	for _, want := range []string{
		`<a href="/nim_lang">@nim_lang</a>`, `<a href="/nim_lang/status/1">link</a>`, ` &amp; <strong>bold</strong>`,
		`<ul><li>one</li><li>two</li></ul>`, `<hr class="article-divider">`,
		`<strong class="blockquote-attribution">— me</strong>`, `class="article-cover"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("article lacks %s:\n%s", want, s)
		}
	}
}

func TestRSS(t *testing.T) {
	cfg, err := config.FromFile(config.File{})
	if err != nil {
		t.Fatal(err)
	}
	p := prefs.Defaults
	tw := richTweet()
	tw.User.ID = "1"
	tw.Note = "note"
	profile := types.Profile{
		User:   types.User{ID: "1", Username: "jack", Fullname: "Jack & <Co>"},
		Tweets: types.Timeline{Content: []types.Tweets{{tw}, {tw}}},
	}
	feeds := map[string]string{
		"timeline": RenderTimelineRss(profile, cfg, &p, false),
		"list":     RenderListRss(profile.Tweets.Content, types.List{ID: "1", Name: "L & M", Username: "o"}, cfg, &p),
		"notes":    RenderCommunityNotesRss(profile.Tweets.Content, "new", cfg, &p),
		"search":   RenderSearchRss(profile.Tweets.Content, `"q" & r`, "", cfg, &p),
		"open":     GenerateOpenSearchXML("Nitter", "nitter.net", "https://nitter.net/search?q="),
	}
	for name, feed := range feeds {
		dec := xml.NewDecoder(strings.NewReader(feed))
		for {
			_, err := dec.Token()
			if err == io.EOF {
				break
			}
			if err != nil && name != "list" { // Nim leaves the list name unescaped in <description>
				t.Errorf("%s feed is not XML: %v\n%s", name, err, feed)
				break
			} else if err != nil {
				break
			}
		}
	}
	feed := feeds["timeline"]
	if n := strings.Count(feed, "<item>"); n != 1 {
		t.Errorf("duplicate links must collapse to one item, got %d", n)
	}
	for _, want := range []string{
		"<title>Jack &amp; &lt;Co&gt; / @jack</title>",
		"<title>R to @biz: just setting up my https://x.com/i</title>",
		"<guid>https://nitter.net/jack/status/1234567890#m</guid>",
		`<img src="https://nitter.net/pic/https%3A%2F%2Fpbs.twimg.com%2Fmedia%2Fa.jpg" style="max-width:250px;" />`,
		"75% — Yes<br>", "<p><b>Community note:</b> note</p>", "<b>Quoted (@quoted)</b>",
		"<description>Twitter feed for: @jack. Generated by https://nitter.net\n</description>",
	} {
		if !strings.Contains(feed, want) {
			t.Errorf("timeline feed lacks %q:\n%s", want, feed)
		}
	}
	if !strings.Contains(feeds["search"], "<title>Search results for \"&quot;q&quot; &amp; r\"</title>") {
		t.Errorf("search title:\n%s", feeds["search"])
	}

	tw.ID = guidCutoff + 1
	if !strings.Contains(RenderListRss([]types.Tweets{{tw}}, types.List{}, cfg, &p), `<guid isPermaLink="false">2000000000000000001</guid>`) {
		t.Error("new tweets use the ID as GUID")
	}

	// the pinned tweet goes before the first older tweet, without
	// touching the profile
	older := &types.Tweet{ID: 1, User: types.User{ID: "1", Username: "jack"}}
	pinned := &types.Tweet{ID: 5, Pinned: true, Text: "pinned", User: older.User}
	profile = types.Profile{User: older.User, Pinned: pinned, Tweets: types.Timeline{Content: []types.Tweets{{older}}}}
	feed = RenderTimelineRss(profile, cfg, &p, false)
	if i, j := strings.Index(feed, "Pinned: pinned"), strings.Index(feed, "status/1#m"); i < 0 || j < i {
		t.Errorf("pinned order:\n%s", feed)
	}
	if len(profile.Tweets.Content[0]) != 1 {
		t.Error("profile timeline modified")
	}

	if got := RenderOembedIframe("https://nitter.net/jack/status/1/embed", 300); !strings.HasPrefix(got, `<iframe src="https://nitter.net/jack/status/1/embed" style="width:100%;max-width:300px;`) {
		t.Errorf("oembed: %s", got)
	}
}

func TestHelpers(t *testing.T) {
	for in, want := range map[int]string{0: "0", 999: "999", 1000: "1,000", 1234567: "1,234,567", -1234: "-1,234"} {
		if got := insertSep(in); got != want {
			t.Errorf("insertSep(%d) = %q", in, got)
		}
	}
	for _, c := range []struct {
		perc  float64
		width int
		want  string
	}{{5, 3, " 5"}, {50, 3, "50"}, {100, 3, "100"}, {0, 3, " 0"}, {33.4, 0, "33"}} {
		if got := pollPercent(c.perc, c.width); got != c.want {
			t.Errorf("pollPercent(%v, %d) = %q", c.perc, c.width, got)
		}
	}
	if got := withTrParam("/a?tr=1&x=2#m", "0"); got != "/a?x=2&tr=0#m" {
		t.Errorf("withTrParam = %q", got)
	}
	if got := encodeURLNoPlus("a b+c:"); got != "a%20b%2Bc%3A" {
		t.Errorf("encodeURLNoPlus = %q", got)
	}
	if got := formatStat(1234567, true); got != "1.2M" {
		t.Errorf("formatStat = %q", got)
	}
	if got := xmlEscape(`<a href="x">'&`); got != "&lt;a href=&quot;x&quot;&gt;&apos;&amp;" {
		t.Errorf("xmlEscape = %q", got)
	}
}
