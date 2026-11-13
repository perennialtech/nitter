// SPDX-License-Identifier: AGPL-3.0-only
package utils

import "testing"

// Regression tests for zedeus/nitter#1411: SSRF via the /video proxy with
// the default HMAC key and no host validation.

func TestHmacMatchesKnownSignatures(t *testing.T) {
	Init("secretkey", false)
	// Computed independently (Python hmac-sha256, uppercase hex, first 13
	// chars) and observed live in the issue report.
	for in, want := range map[string]string{
		"http://172.17.0.1:19999/secret_data.m3u8": "BBD19ACC6C012",
		"http://172.17.0.1:19999/secret_data.mp4":  "0780F00DDF3E7",
	} {
		if got := Hmac(in); got != want {
			t.Errorf("Hmac(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsTwitterURL(t *testing.T) {
	for _, u := range []string{
		"http://172.17.0.1:19999/secret_data.m3u8",
		"http://169.254.169.254/latest/meta-data/x.m3u8",
		"http://localhost/x.mp4",
		"http://[::1]/x.mp4",
		"http://video.twimg.com@169.254.169.254/x.mp4",
		"http://video.twimg.com.evil.com/x.mp4",
		"http://evilvideo.twimg.com.attacker/x.mp4",
		"gopher://video.twimg.com/x.mp4",
		"file:///etc/passwd",
		"ftp://video.twimg.com/x.mp4",
	} {
		if IsTwitterURL(u) {
			t.Errorf("IsTwitterURL(%q) = true, want false", u)
		}
	}
	for _, u := range []string{
		"https://video.twimg.com/ext_tw_video/1/pu/pl/x.m3u8",
		"https://video.twimg.com/amplify_video/1/vid/x.mp4",
		"https://prod-fastly-us-east-1.video.pscp.tv/x.m3u8",
	} {
		if !IsTwitterURL(u) {
			t.Errorf("IsTwitterURL(%q) = false, want true", u)
		}
	}
}

func TestPastedTwitterLink(t *testing.T) {
	for q, want := range map[string]string{
		"https://x.com/jack/status/20":                "/jack/status/20",
		"  https://twitter.com/jack/status/20?s=46  ": "/jack/status/20?s=46",
		"x.com/jack": "/jack",
		"mobile.twitter.com/jack/status/20/photo/1": "/jack/status/20/photo/1",
		"https://x.com/i/lists/123":                 "/i/lists/123",
		"https://x.com/search?q=nitter":             "/search?q=nitter",
		"x.com":                                     "",
		"https://x.com/":                            "",
		"https://x.com/?s=1":                        "",
		"jack":                                      "",
		"@jack":                                     "",
		"from:jack x.com/jack":                      "",
		"https://x.com/jack/status/20 and more":     "",
		"https://example.com/jack/status/20":        "",
		"https://x.com.evil.com/jack":               "",
		"https://x.com//evil.com":                   "",
		"ftp://x.com/jack":                          "",
	} {
		if got := PastedTwitterLink(q); got != want {
			t.Errorf("PastedTwitterLink(%q) = %q, want %q", q, got, want)
		}
	}
}
