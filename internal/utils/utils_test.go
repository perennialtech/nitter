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
