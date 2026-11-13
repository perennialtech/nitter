// SPDX-License-Identifier: AGPL-3.0-only

// Package flags holds instance-wide behaviour derived from nitter.conf at
// startup. main sets these once before serving; everything else reads them.
package flags

var (
	// GuestAuth is set when the instance uses guest tokens instead of
	// account sessions, which changes which tabs and pages are available.
	GuestAuth bool
	// ShowRetweetTime shows the retweet timestamp next to "user retweeted".
	ShowRetweetTime bool
	// EnableTranslation shows "See translation" links and translated text.
	EnableTranslation bool
	// OnDemandTranslation offers Grok translation of tweets and notes that
	// Twitter didn't translate. It requires cookie sessions and a unique
	// hmacKey, which main checks before setting it.
	OnDemandTranslation bool
	// OnDemandTranslationLang is the target language for OnDemandTranslation.
	OnDemandTranslationLang string
)
