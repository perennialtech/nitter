// SPDX-License-Identifier: AGPL-3.0-only
package formatters

import (
	"testing"

	"github.com/perennialtech/nitter/internal/prefs"
)

func TestStripHTML(t *testing.T) {
	cases := []struct {
		in      string
		shorten bool
		want    string
	}{
		{"", false, ""},
		{"Nim", false, "Nim"},
		{"<p>Hello <strong>Nim</strong>!</p>", false, "Hello Nim!"},
		{"Nim &amp; C &lt;3 &#x2764;", false, "Nim & C <3 ❤"},
		{`<a href="https://example.com/path">short</a>`, false, "https://example.com/path"},
		{`<p><a href="https://example.com/path">short</a></p>`, false, "https://example.com/path"},
		{`<a href="https://example.com/abcdefghijklmnopqrstuvwxyz">short</a>`, true, "example.com/abcdefghijklmnop…"},
		{`<a href="/nim_lang">@nim_lang</a>`, false, "@nim_lang"},
		{`<a href="https://example.com"></a>`, false, ""},
	}
	for _, c := range cases {
		if got := StripHTML(c.in, c.shorten); got != c.want {
			t.Errorf("StripHTML(%q, %v) = %q, want %q", c.in, c.shorten, got, c.want)
		}
	}
}

func TestReplaceURLs(t *testing.T) {
	p := prefs.Prefs{ReplaceTwitter: "nitter.example", ReplaceReddit: "teddit.example", ReplaceYouTube: "piped.example"}
	cases := []struct{ in, want string }{
		{"see https://twitter.com/jack", "see https://nitter.example/jack"},
		{"https://x.com/jack at start", "https://nitter.example/jack at start"},
		{"bare www.twitter.com/jack", "bare nitter.example/jack"},
		// a host that doesn't follow whitespace or a leading https:// is left alone
		{"notx.com/jack", "notx.com/jack"},
		{`<a href="https://x.com/jack">x.com/jack</a>`, `<a href="https://nitter.example/jack">nitter.example/jack</a>`},
		{"https://t.co/abc", "https://nitter.example/t.co/abc"},
		{"https://www.reddit.com/r/nim", "https://teddit.example/r/nim"},
		{"https://redd.it/abc", "https://teddit.example/comments/abc"},
		// v.redd.it and subreddit-looking hosts after '.' or 'b' aren't replaced
		{"https://v.redd.it/abc", "https://v.redd.it/abc"},
		{"https://youtu.be/abc", "https://piped.example/abc"},
	}
	for _, c := range cases {
		if got := ReplaceURLs(c.in, &p, ""); got != c.want {
			t.Errorf("ReplaceURLs(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestProxifyVideo(t *testing.T) {
	manifest := "#EXTM3U\n#EXT-X-MAP:URI=\"/init.mp4\"\n/seg1.ts\nseg2.ts\n#EXT-X-ENDLIST"
	got := ProxifyVideo(manifest, false, "https://video.twimg.com/a/b/pl.m3u8")
	want := "#EXTM3U\n#EXT-X-MAP:URI=\"https://video.twimg.com/init.mp4\"\nhttps://video.twimg.com/seg1.ts\n" +
		"https://video.twimg.com/a/b/seg2.ts\n#EXT-X-ENDLIST"
	if got != want {
		t.Errorf("ProxifyVideo:\n got %q\nwant %q", got, want)
	}
}

func TestDuration(t *testing.T) {
	for ms, want := range map[int]string{0: "0:00", 61500: "1:02", 3_723_000: "1:02:03"} {
		if got := Duration(ms); got != want {
			t.Errorf("Duration(%d) = %q, want %q", ms, got, want)
		}
	}
}
