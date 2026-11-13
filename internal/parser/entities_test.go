// SPDX-License-Identifier: AGPL-3.0-only

package parser

import (
	"math/rand"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tidwall/gjson"

	"github.com/perennialtech/nitter/internal/types"
)

func pj(s string) gjson.Result { return gjson.Parse(s) }

// Ported from tests/test_upstream_features.nim, "upstream link rendering".

func TestTranslatedURLsEscapeHTMLAttributes(t *testing.T) {
	entities := pj(`{"urls":[{"indices":[4,8],"expanded_url":"https://example.org/\" onmouseover=\"alert(1)"}]}`)
	html := expandTranslation("see link", entities)
	if !strings.Contains(html, `href="https://example.org/&quot; onmouseover=&quot;alert(1)"`) {
		t.Errorf("href not escaped: %s", html)
	}
	if strings.Contains(html, `" onmouseover="`) {
		t.Errorf("attribute breakout: %s", html)
	}
}

func TestTranslatedNotesLeaveNonWebURLsAsText(t *testing.T) {
	entities := pj(`[{"from_index":4,"to_index":8,"ref":{"url":"javascript:alert(1)"}}]`)
	if got := expandNoteTranslation("see link", entities); got != "see link" {
		t.Errorf("got %q", got)
	}
}

func TestInvalidNoteRangesPreserveEscapedText(t *testing.T) {
	entities := pj(`[{"fromIndex":4,"toIndex":6,"ref":{"url":"https://example.org"}}]`)
	if got := expandBirdwatchEntities("<b>", entities); got != "&lt;b&gt;" {
		t.Errorf("got %q", got)
	}
	if got := expandBirdwatchEntities("<b>", pj("null")); got != "&lt;b&gt;" {
		t.Errorf("null entities: got %q", got)
	}
}

func TestNoteEntityOffsetsSupportBothSpellings(t *testing.T) {
	const want = `<a href="/test">link</a>`
	for _, entities := range []string{
		`[{"fromIndex":0,"toIndex":4,"ref":{"url":"https://x.com/test"}}]`,
		`[{"from_index":0,"to_index":4,"ref":{"url":"https://x.com/test"}}]`,
	} {
		if got := expandBirdwatchEntities("link", pj(entities)); got != want {
			t.Errorf("%s: got %q", entities, got)
		}
	}
}

func TestBirdwatchEntitiesEscapeTextAroundLinks(t *testing.T) {
	entities := pj(`[{"fromIndex":3,"toIndex":7,"ref":{"url":"https://t.co/abc"}}]`)
	want := `a&amp; <a href="/t.co/abc">link</a> &lt;b&gt;`
	if got := expandBirdwatchEntities("a& link <b>", entities); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Ported from tests/test_upstream_features.nim, "translated links".

func TestGrokTranslationEscapesURLs(t *testing.T) {
	link := "https://t.co/example"
	expanded := `https://example.com/?x=" onmouseover="alert(1)&y=2`
	want := `<a href="https://example.com/?x=&quot; onmouseover=&quot;alert(1)&amp;y=2">` +
		`https://example.com/?x=&quot; onmouseover=&quot;alert(1)&amp;y=2</a>`
	if got := ExpandGrokTranslation(link, []GrokLink{{URL: link, Expanded: expanded}}); got != want {
		t.Errorf("got %q", got)
	}
}

func TestGrokTranslationUnsafeSchemesStayText(t *testing.T) {
	link := "https://t.co/example"
	if got := ExpandGrokTranslation(link, []GrokLink{{URL: link, Expanded: "javascript:alert(1)"}}); got != link {
		t.Errorf("got %q", got)
	}
}

func TestGrokTranslationLinksEveryOccurrence(t *testing.T) {
	source := "https://example.com/?a=1&b=2"
	target := "https://example.org/"
	anchor := `<a href="` + target + `">` + target + "</a>"
	got := ExpandGrokTranslation("猫 "+source+" and "+source, []GrokLink{{URL: source, Expanded: target}})
	if want := "猫 " + anchor + " and " + anchor; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGrokTranslationPrefersLongerOverlappingURL(t *testing.T) {
	text := "see https://a.com/x/y now"
	got := ExpandGrokTranslation(text, []GrokLink{
		{URL: "https://a.com/x", Expanded: "https://short.example/"},
		{URL: "https://a.com/x/y", Expanded: "https://long.example/"},
	})
	want := `see <a href="https://long.example/">https://long.example/</a> now`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Ported from tests/test_fxtwitter.nim: the parserutils helpers FxTwitter
// parsing relies on.

const fxRawDescription = `{"text": "Missions at https://t.co/abc and @NASAWebb #space",
  "facets": [{"type": "url", "indices": [12, 28], "original": "https://t.co/abc",
              "replacement": "https://www.nasa.gov/missions", "display": "nasa.gov/missions"}]}`

func TestFxBioLinksMentionsAndHashtags(t *testing.T) {
	var u types.User
	ExpandFxBio(&u, pj(fxRawDescription))
	want := `Missions at <a href="https://www.nasa.gov/missions">nasa.gov/missions</a> and ` +
		`<a href="/NASAWebb">@NASAWebb</a> <a href="/search?f=tweets&q=%23space">#space</a>`
	if u.Bio != want {
		t.Errorf("got %q\nwant %q", u.Bio, want)
	}
}

func TestFxTextFacetsExpandAndTrailingMediaLinksAreCut(t *testing.T) {
	tweet := newTweet()
	ExpandFxText(tweet, pj(`{"text": "Tethys &amp; Dione via @NASAWebb #Saturn https://t.co/link https://t.co/media",
        "display_text_range": [0, 63],
        "facets": [
          {"type": "mention", "indices": [23, 32], "original": "NASAWebb", "id": "1"},
          {"type": "hashtag", "indices": [33, 40], "original": "Saturn"},
          {"type": "url", "indices": [41, 63], "original": "https://t.co/link",
           "replacement": "https://science.nasa.gov/saturn/", "display": "science.nasa.gov/saturn/"},
          {"type": "media", "indices": [64, 87], "original": "https://t.co/media"}]}`), "", false, false)
	for _, want := range []string{
		`href="/search?f=tweets&q=%23Saturn">#Saturn</a>`,
		`href="https://science.nasa.gov/saturn/"`,
	} {
		if !strings.Contains(tweet.Text, want) {
			t.Errorf("%q missing from %q", want, tweet.Text)
		}
	}
	if !strings.HasPrefix(tweet.Text, `Tethys &amp; Dione via <a href="/NASAWebb" title="">@NASAWebb</a>`) {
		t.Errorf("got %q", tweet.Text)
	}
	if strings.Contains(tweet.Text, "t.co/media") {
		t.Errorf("media link kept: %q", tweet.Text)
	}
}

func TestFxTextLeadingReplyMentionsBecomeReplyList(t *testing.T) {
	tweet := newTweet()
	tweet.ReplyID = 2121073525142507992
	ExpandFxText(tweet, pj(`{"text": "@_itsforkb @other take care", "display_text_range": [18, 27],
        "facets": [{"type": "mention", "indices": [0, 10], "original": "_itsforkb"},
                   {"type": "mention", "indices": [11, 17], "original": "other"}]}`), "_itsforkb", false, false)
	if !slices.Equal(tweet.Reply, []string{"_itsforkb", "other"}) {
		t.Errorf("reply = %v", tweet.Reply)
	}
	if tweet.Text != "take care" {
		t.Errorf("text = %q", tweet.Text)
	}
}

func TestFxTextQuotesHideTrailingLink(t *testing.T) {
	tweet := newTweet()
	ExpandFxText(tweet, pj(`{"text": "look https://t.co/q", "display_text_range": [0, 19],
        "facets": [{"type": "url", "indices": [5, 19], "original": "https://t.co/q",
                    "replacement": "https://x.com/MCRmyPH/status/2120863244055384470"}]}`), "", true, false)
	if tweet.Text != "look" {
		t.Errorf("text = %q", tweet.Text)
	}
}

func TestFxTextUpdatesCardURL(t *testing.T) {
	tweet := newTweet()
	tweet.Card = &types.Card{URL: "https://t.co/c"}
	ExpandFxText(tweet, pj(`{"text": "a https://t.co/c", "display_text_range": [0, 16],
        "facets": [{"type": "url", "indices": [2, 16], "original": "https://t.co/c",
                    "replacement": "https://example.com/"}]}`), "", false, false)
	if tweet.Card.URL != "https://example.com/" {
		t.Errorf("card url = %q", tweet.Card.URL)
	}
}

func TestFxNote(t *testing.T) {
	note := pj(`{"text": "a<b> see https://t.co/n", "facets": [{"type": "url", "indices": [9, 23],
        "original": "https://t.co/n", "replacement": "https://x.com/i/status/1"}]}`)
	want := `a&lt;b&gt; see <a href="/i/status/1">https://t.co/n</a>`
	if got := ExpandFxNote(note); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := ExpandFxNote(pj(`{"text": "plain <b>"}`)); got != "plain &lt;b&gt;" {
		t.Errorf("no facets: got %q", got)
	}
}

func TestGetTimeParsesTwitterDates(t *testing.T) {
	got := GetTime(pj(`"Wed Dec 19 20:20:32 +0000 2007"`))
	want := time.Date(2007, 12, 19, 20, 20, 32, 0, time.UTC)
	if !got.Equal(want) || got.Location() != time.UTC {
		t.Errorf("got %v", got)
	}
	for _, bad := range []string{`"Wed Dec 19 20:20:32 +0000 07"`, `123`, `null`} {
		if got := GetTime(pj(bad)); !got.IsZero() {
			t.Errorf("%s: got %v", bad, got)
		}
	}
	if got := getDateTime(pj(`"2030-01-02T03:04:05Z"`)); !got.Equal(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("iso: got %v", got)
	}
}

func TestGetMp4Resolution(t *testing.T) {
	for url, want := range map[string]int{
		"https://video.twimg.com/ext_tw_video/1/pu/vid/720x1280/a.mp4":          1280,
		"https://video.twimg.com/amplify_video/1/vid/avc1/320x400/d.mp4?tag=29": 400,
		"https://video.twimg.com/tweet_video/abc.mp4":                           0,
		"https://video.twimg.com/a/b/xyz.mp4":                                   0,
		"":                                                                      0,
	} {
		if got := GetMp4Resolution(url); got != want {
			t.Errorf("%s: got %d, want %d", url, got, want)
		}
	}
}

func TestIDs(t *testing.T) {
	for in, want := range map[string]int64{
		"conversationthread-123-tweet-456": 456,
		"tweet-1_000":                      1000,
		"789":                              789,
		"abc":                              0,
		"tweet-":                           0,
	} {
		if got := ParseID(in); got != want {
			t.Errorf("ParseID(%q) = %d, want %d", in, got, want)
		}
	}
	if got := GetID(pj(`"2120974110335312297"`)); got != 2120974110335312297 {
		t.Errorf("string id: %d", got)
	}
	if got := GetID(pj(`42`)); got != 42 {
		t.Errorf("int id: %d", got)
	}
	if got := GetID(pj(`4.2`)); got != 0 {
		t.Errorf("float id: %d", got)
	}
}

func TestNimParseInt(t *testing.T) {
	ok := map[string]int64{"0": 0, "+5": 5, "-5": -5, "1_000": 1000,
		"9223372036854775807": 9223372036854775807, "-9223372036854775808": -9223372036854775808}
	for in, want := range ok {
		if got, valid := nimParseInt(in); !valid || got != want {
			t.Errorf("%q: got %d %v", in, got, valid)
		}
	}
	for _, in := range []string{"", "-", "_1", "12a", " 1", "9223372036854775808"} {
		if _, valid := nimParseInt(in); valid {
			t.Errorf("%q parsed", in)
		}
	}
}

// Entity expansion with code point offsets: an astral emoji is one index.
func TestTextEntitiesUseCodePointOffsets(t *testing.T) {
	tweet := newTweet()
	entities := pj(`{"user_mentions":[{"screen_name":"bob","name":"Bob <3","indices":[2,6]}],
		"hashtags":[{"indices":[10,14]}],
		"urls":[{"url":"https://t.co/z","expanded_url":"https://example.com/a?b=1&c=2","indices":[15,29]}]}`)
	text := "😀 @bob hi #tag https://t.co/z"
	expandTextEntities(tweet, entities, text, 0, 29, "", false, false)
	want := `😀 <a href="/bob" title="Bob &lt;3">@bob</a> hi ` +
		`<a href="/search?f=tweets&q=%23tag">#tag</a> ` +
		`<a href="https://example.com/a?b=1&amp;c=2">example.com/a?b=1&amp;c=2</a>`
	if tweet.Text != want {
		t.Errorf("got  %q\nwant %q", tweet.Text, want)
	}
}

func TestTextEntitiesDisplayRangeAndReplies(t *testing.T) {
	tweet := newTweet()
	tweet.ReplyID = 1
	js := pj(`{"display_text_range":[11,23],"in_reply_to_screen_name":"alice",
		"entities":{"user_mentions":[
			{"screen_name":"alice","name":"A","indices":[0,6]},
			{"screen_name":"carol","name":"C","indices":[7,10]},
			{"screen_name":"alice","name":"A","indices":[17,23]}]}}`)
	tweet.Text = "@alice @ca hello @alice"
	expandTweetEntities(tweet, js, false)
	if !slices.Equal(tweet.Reply, []string{"alice", "carol"}) {
		t.Errorf("reply = %v", tweet.Reply)
	}
	if want := `hello <a href="/alice" title="A">@alice</a>`; tweet.Text != want {
		t.Errorf("got %q", tweet.Text)
	}
}

func TestTextEntitiesHideTrailingTwitterLinks(t *testing.T) {
	tweet := newTweet()
	entities := pj(`{"urls":[{"url":"https://t.co/q","expanded_url":"https://twitter.com/a/status/1","indices":[5,19]}],
		"media":[{"url":"https://t.co/m","expanded_url":"https://twitter.com/a/status/1/photo/1","indices":[20,34]}]}`)
	// display_text_range ends before the media link, as in real tweets.
	expandTextEntities(tweet, entities, "look https://t.co/q https://t.co/m", 0, 19, "", true, false)
	if tweet.Text != "look" {
		t.Errorf("got %q", tweet.Text)
	}
}

func TestNoteTweetEntitiesEscapeAngleBrackets(t *testing.T) {
	tweet := newTweet()
	expandNoteTweetEntities(tweet, pj(`{"text":"a <b> #x","entity_set":{"hashtags":[{"indices":[6,8]}]}}`))
	if want := `a &lt;b&gt; <a href="/search?f=tweets&q=%23x">#x</a>`; tweet.Text != want {
		t.Errorf("got %q", tweet.Text)
	}
}

func TestReplacedWithSkipsInvalidRanges(t *testing.T) {
	runes := []rune("abc")
	repls := []replaceSlice{{kind: rkURL, a: 1, b: 5, url: "u", display: "d"}, {kind: rkRemove, a: 2, b: 2}}
	if got := replacedWith(runes, repls, 0, 3); got != "ab" {
		t.Errorf("got %q", got)
	}
	if got := replacedWith(runes, nil, -4, 10); got != "abc" {
		t.Errorf("clamped: got %q", got)
	}
}

func TestDeduplicateMovesLastIntoGap(t *testing.T) {
	s := []replaceSlice{{a: 1}, {a: 2}, {a: 1}, {a: 3}, {a: 2}}
	got := deduplicate(s)
	var starts []int
	for _, r := range got {
		starts = append(starts, r.a)
	}
	if !slices.Equal(starts, []int{1, 2, 3}) {
		t.Errorf("got %v", starts)
	}
}

func TestNimSortMatchesStableSort(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for n := 0; n < 40; n++ {
		type item struct{ key, pos int }
		a := make([]item, n)
		for i := range a {
			a[i] = item{rng.Intn(5), i}
		}
		b := slices.Clone(a)
		nimSort(a, func(x, y item) int { return cmpInt(x.key, y.key) })
		sort.SliceStable(b, func(i, j int) bool { return b[i].key < b[j].key })
		if !slices.Equal(a, b) {
			t.Fatalf("n=%d: got %v, want %v", n, a, b)
		}
	}
}

// Regex rewrites: Nim's PCRE patterns ported to RE2 or hand-written matchers.

func TestUserAndHashtagRegexes(t *testing.T) {
	cases := map[string]string{
		"hi @abc and x@y":    `hi <a href="/abc">@abc</a> and x@y`,
		"[@abc":              "[@abc", // [ falls inside the A-z range
		"@abcdefghijklmnopq": `<a href="/abcdefghijklmno">@abcdefghijklmno</a>pq`,
		"#tag é#tag2 a#no /#no": `<a href="/search?f=tweets&q=%23tag">#tag</a> ` +
			`é<a href="/search?f=tweets&q=%23tag2">#tag2</a> a#no /#no`,
		"＃full $CASH": `<a href="/search?f=tweets&q=%23full">＃full</a> ` +
			`<a href="/search?f=tweets&q=%23CASH">$CASH</a>`,
	}
	for in, want := range cases {
		if got := linkUsersAndHashtags(in); got != want {
			t.Errorf("%q:\n got %q\nwant %q", in, got, want)
		}
	}
}

func TestExperimentalHashtagMatcher(t *testing.T) {
	link := func(prefix, hash, name string) string {
		return prefix + `<a href="/search?f=tweets&q=%23` + name + `">` + hash + name + "</a>"
	}
	linked := `<a href="/search?f=tweets&q=%23tag">#tag</a>`
	cases := map[string]string{
		"#go":        link("", "#", "go"),
		"see #go!":   "see " + link("", "#", "go") + "!",
		"#ab#":       "#ab#",                   // possessive name, then # follows: no match
		"##a":        "#" + link("", "#", "a"), // falls back to # as the prefix
		"x #":        "x " + link("", "#", ""),
		"/#x":        "/" + link("", "#", "x"), // / isn't excluded here
		"a#x .#y":    "a#x .#y",
		linked:       linked,
		`#x">`:       `#x">`,
		"＃全":         link("", "＃", "") + "全",
		"é$TSLA end": "é" + link("", "$", "TSLA") + " end",
	}
	for in, want := range cases {
		if got := expLinkHashtags(in); got != want {
			t.Errorf("%q:\n got %q\nwant %q", in, got, want)
		}
	}
}

func TestNimHash(t *testing.T) {
	for in, want := range map[string]uint32{
		"":      314159265, // murmur3("") is 0, which Nim's tables replace
		"hello": 0x248bfa47,
		"The quick brown fox jumps over the lazy dog": 0x2e4ff723,
	} {
		if got := nimHash(in); got != want {
			t.Errorf("%q: got %#x, want %#x", in, got, want)
		}
	}
}

func TestNimTableValuesKeepFirstSlotForDuplicates(t *testing.T) {
	vals := nimTableValues(pj(`{"a":1,"b":2,"a":3}`))
	var got []string
	for _, v := range vals {
		got = append(got, v.Raw)
	}
	slices.Sort(got)
	if !slices.Equal(got, []string{"2", "3"}) {
		t.Errorf("got %v", got)
	}
	many := `{`
	for i := range 40 {
		if i > 0 {
			many += ","
		}
		many += `"k` + itoa(int64(i)) + `":` + itoa(int64(i))
	}
	if n := len(nimTableValues(pj(many + "}"))); n != 40 {
		t.Errorf("after growth: %d values", n)
	}
}

func TestNimURI(t *testing.T) {
	for in, want := range map[string]nimURI{
		"https://user:pw@x.com:443/a/video/1?q#f": {"https", "x.com", "/a/video/1"},
		"//host/p":            {"", "host", "/p"},
		"/rel/path":           {"", "", "/rel/path"},
		"javascript:alert(1)": {"javascript", "", "alert(1)"},
		"http://[::1]:80/":    {"http", "::1", "/"},
	} {
		if got := parseNimURI(in); got != want {
			t.Errorf("%q: got %+v, want %+v", in, got, want)
		}
	}
}

func TestParseEnumIsStyleInsensitive(t *testing.T) {
	if got := parseVerifiedType("BLUE", types.VerifiedNone); got != types.VerifiedBlue {
		t.Errorf("BLUE: %q", got)
	}
	if got := parseVerifiedType("blue", types.VerifiedBusiness); got != types.VerifiedBusiness {
		t.Errorf("blue: %q", got)
	}
	if got := parseEnum("summary_LARGE_image", cardKinds, types.CardUnknown); got != types.CardSummaryLarge {
		t.Errorf("card: %q", got)
	}
}

func TestDurationLead(t *testing.T) {
	for d, want := range map[time.Duration]string{
		26*time.Hour + time.Second: "1 day",
		53 * time.Hour:             "2 days",
		15 * 24 * time.Hour:        "2 weeks",
		time.Hour + time.Minute:    "1 hour",
		90 * time.Second:           "1 minute",
	} {
		if got := durationLead(d); got != want {
			t.Errorf("%v: got %q, want %q", d, got, want)
		}
	}
}
