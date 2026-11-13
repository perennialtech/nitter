// SPDX-License-Identifier: AGPL-3.0-only

package prefs

import (
	"slices"
	"testing"
)

func TestParseLinkReplacements(t *testing.T) {
	valid := []string{
		"", "nitter.example", "Piped.Example:8443", "my-instance.example.",
		"localhost:8080", "127.0.0.1:8080", "[::1]:8080", "日本.example",
	}
	invalid := []string{
		`"><img src=x onerror=alert(1)>`, `example.com" onmouseover="alert(1)`,
		"https://example.com", "//example.com", "example.com/path", "example.com/",
		"example.com?x=y", "example.com#fragment", "user@example.com",
		"example.com&x=y", "example.com'", "example.com%22", "example.com\\path",
		"example.com\n", " example.com", "example.com:bad", "example.com:",
		".example.com", "example..com", "-example.com", "example-.com",
	}
	for _, name := range []string{"replaceTwitter", "replaceYouTube", "replaceReddit"} {
		t.Run(name, func(t *testing.T) {
			for _, source := range []string{"cookie", "query"} {
				for _, value := range append(valid, invalid...) {
					values := map[string]string{name: value}
					var p Prefs
					if source == "cookie" {
						p = Parse(values, nil)
					} else {
						p = Parse(nil, values)
					}
					want := Defaults.String(name)
					if slices.Contains(valid, value) {
						want = value
					}
					if got := p.String(name); got != want {
						t.Errorf("%s %q = %q, want %q", source, value, got, want)
					}
				}
			}
			cookies := map[string]string{name: "saved.example:8443"}
			p := Parse(cookies, map[string]string{name: invalid[0]})
			if got := p.String(name); got != cookies[name] {
				t.Errorf("invalid query overwrote saved hostname: %q", got)
			}
			p = Parse(cookies, map[string]string{name: ""})
			if got := p.String(name); got != "" {
				t.Errorf("blank query did not disable replacement: %q", got)
			}
		})
	}
}
