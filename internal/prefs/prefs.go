// SPDX-License-Identifier: AGPL-3.0-only

// Package prefs defines the user preferences stored in cookies, their
// defaults, and the [Preferences] overrides from nitter.conf.
package prefs

import (
	"slices"
	"strings"

	"github.com/perennialtech/nitter/internal/config"
)

type Kind int

const (
	Checkbox Kind = iota
	Select
	Input
)

type Pref struct {
	Name  string
	Label string
	Kind  Kind
	// Checkbox
	DefaultState bool
	// Select
	DefaultOption string
	Options       []string
	// Input
	DefaultInput string
	Placeholder  string
	// Doc adds to Label in nitter.example.conf, where the label alone
	// doesn't say what the preference does.
	Doc string
}

func (p Pref) withDoc(doc string) Pref {
	p.Doc = doc
	return p
}

type Category struct {
	Name  string
	Prefs []Pref
}

func checkbox(name string, def bool, label string) Pref {
	return Pref{Name: name, Label: label, Kind: Checkbox, DefaultState: def}
}

func sel(name, def, label string, options ...string) Pref {
	return Pref{Name: name, Label: label, Kind: Select, DefaultOption: def, Options: options}
}

func input(name, def, label, placeholder string) Pref {
	return Pref{Name: name, Label: label, Kind: Input, DefaultInput: def, Placeholder: placeholder}
}

// Categories lists every preference in the order the settings page shows them.
// Theme options come from the stylesheets in public/css/themes, so the
// theme preference has none here.
var Categories = []Category{
	{"Display", []Pref{
		sel("theme", "Nitter", "Theme").withDoc("The name of a stylesheet in " +
			"css/themes under staticDir, as the settings page lists it, such as " +
			"Nitter, Twitter Dark or Catppuccin Mocha."),
		checkbox("infiniteScroll", false, "Infinite scrolling (experimental, requires JavaScript)"),
		checkbox("stickyProfile", true, "Make profile sidebar stick to top"),
		checkbox("stickyNav", true, "Keep navbar fixed to top"),
		checkbox("bidiSupport", false, "Support bidirectional text (makes clicking on tweets harder)"),
		checkbox("hideTweetStats", false, "Hide tweet stats (replies, retweets, likes)"),
		checkbox("hideTweetSource", false, "Hide tweet source (iPhone, Android, etc.)"),
		checkbox("abbreviateStats", false, "Abbreviate tweet stats (1.2M instead of 1,200,000)"),
		checkbox("hideBanner", false, "Hide profile banner"),
		checkbox("hidePins", false, "Hide pinned tweets"),
		checkbox("hideReplies", false, "Hide tweet replies"),
		checkbox("hideRelated", true, "Hide related tweets under replies"),
		checkbox("hideCommunityNotes", false, "Hide community notes"),
		checkbox("autoTranslate", false, "Auto translate tweets if available").withDoc(
			"Shows translations without clicking \"See translation\". Not offered with guestAuth."),
		checkbox("squareAvatars", false, "Square profile pictures"),
		checkbox("guestSearchFallback", true, "Use search for timelines as guest (turn off for shadowbanned accounts)").withDoc(
			"Only applies with guestAuth: profile tweets come from a search for the " +
				"user's tweets instead of their timeline."),
	}},
	{"Media", []Pref{
		checkbox("mp4Playback", true, "Enable mp4 video playback (only for gifs)"),
		checkbox("hlsPlayback", false, "Enable HLS video streaming (requires JavaScript)"),
		checkbox("proxyVideos", true, "Proxy video streaming through the server (might be slow)").withDoc(
			"When off, browsers load videos from Twitter's servers directly."),
		checkbox("muteVideos", false, "Mute videos by default"),
		checkbox("autoplayGifs", true, "Autoplay gifs"),
		checkbox("compactGallery", false, "Compact media gallery (no profile info or text)"),
		sel("gallerySize", "Medium", "Gallery column size", "Small", "Medium", "Large"),
		sel("mediaView", "Timeline", "Default media view", "Timeline", "Grid", "Gallery"),
	}},
	{"Link replacements (blank to disable)", []Pref{
		input("replaceTwitter", "", "Twitter -> Nitter", "Nitter hostname").withDoc(
			"Host name that links to twitter.com, x.com and t.co in tweets are " +
				"rewritten to, such as nitter.net. Empty leaves them as they are."),
		input("replaceYouTube", "", "YouTube -> Piped/Invidious", "Piped hostname").withDoc(
			"Host name of a Piped or Invidious instance that YouTube links are " +
				"rewritten to. Empty leaves them as they are."),
		input("replaceReddit", "", "Reddit -> Teddit/Libreddit", "Teddit hostname").withDoc(
			"Host name of a Teddit or Libreddit instance that Reddit links are " +
				"rewritten to. Empty leaves them as they are."),
	}},
}

// All returns every preference in display order.
func All() []Pref {
	var out []Pref
	for _, c := range Categories {
		out = append(out, c.Prefs...)
	}
	return out
}

type Prefs struct {
	Theme               string
	InfiniteScroll      bool
	StickyProfile       bool
	StickyNav           bool
	BidiSupport         bool
	HideTweetStats      bool
	HideTweetSource     bool
	AbbreviateStats     bool
	HideBanner          bool
	HidePins            bool
	HideReplies         bool
	HideRelated         bool
	HideCommunityNotes  bool
	AutoTranslate       bool
	SquareAvatars       bool
	GuestSearchFallback bool
	Mp4Playback         bool
	HlsPlayback         bool
	ProxyVideos         bool
	MuteVideos          bool
	AutoplayGifs        bool
	CompactGallery      bool
	GallerySize         string
	MediaView           string
	ReplaceTwitter      string
	ReplaceYouTube      string
	ReplaceReddit       string
}

// field returns a pointer to the field for name: *bool for checkboxes,
// *string otherwise.
func (p *Prefs) field(name string) any {
	switch name {
	case "theme":
		return &p.Theme
	case "infiniteScroll":
		return &p.InfiniteScroll
	case "stickyProfile":
		return &p.StickyProfile
	case "stickyNav":
		return &p.StickyNav
	case "bidiSupport":
		return &p.BidiSupport
	case "hideTweetStats":
		return &p.HideTweetStats
	case "hideTweetSource":
		return &p.HideTweetSource
	case "abbreviateStats":
		return &p.AbbreviateStats
	case "hideBanner":
		return &p.HideBanner
	case "hidePins":
		return &p.HidePins
	case "hideReplies":
		return &p.HideReplies
	case "hideRelated":
		return &p.HideRelated
	case "hideCommunityNotes":
		return &p.HideCommunityNotes
	case "autoTranslate":
		return &p.AutoTranslate
	case "squareAvatars":
		return &p.SquareAvatars
	case "guestSearchFallback":
		return &p.GuestSearchFallback
	case "mp4Playback":
		return &p.Mp4Playback
	case "hlsPlayback":
		return &p.HlsPlayback
	case "proxyVideos":
		return &p.ProxyVideos
	case "muteVideos":
		return &p.MuteVideos
	case "autoplayGifs":
		return &p.AutoplayGifs
	case "compactGallery":
		return &p.CompactGallery
	case "gallerySize":
		return &p.GallerySize
	case "mediaView":
		return &p.MediaView
	case "replaceTwitter":
		return &p.ReplaceTwitter
	case "replaceYouTube":
		return &p.ReplaceYouTube
	case "replaceReddit":
		return &p.ReplaceReddit
	}
	panic("prefs: unknown preference " + name)
}

// Bool returns a checkbox preference's value by name.
func (p *Prefs) Bool(name string) bool { return *p.field(name).(*bool) }

// String returns a select or input preference's value by name.
func (p *Prefs) String(name string) string { return *p.field(name).(*string) }

// Defaults are the preferences for a visitor with no cookies. Init applies
// the [Preferences] overrides from nitter.conf.
var Defaults = builtinDefaults()

func builtinDefaults() Prefs {
	var p Prefs
	for _, pref := range All() {
		switch f := p.field(pref.Name).(type) {
		case *bool:
			*f = pref.DefaultState
		case *string:
			if pref.Kind == Select {
				*f = pref.DefaultOption
			} else {
				*f = pref.DefaultInput
			}
		}
	}
	return p
}

// Init sets Defaults from the built-in values and the config overrides.
func Init(cfg *config.Config) error {
	Defaults = builtinDefaults()
	for _, pref := range All() {
		v := cfg.Preferences[pref.Name]
		if v == "" {
			continue
		}
		switch f := Defaults.field(pref.Name).(type) {
		case *bool:
			b, err := config.ParseBool(v)
			if err != nil {
				return err
			}
			*f = b
		case *string:
			*f = v
		}
	}
	return nil
}

func (p *Prefs) apply(values map[string]string) {
	for _, pref := range All() {
		v, ok := values[pref.Name]
		if !ok {
			continue
		}
		switch f := p.field(pref.Name).(type) {
		case *bool:
			*f = v == "on" || v == "true" || v == "1"
		case *string:
			if pref.Kind == Input || pref.Name == "theme" || slices.Contains(pref.Options, v) {
				*f = v
			}
		}
	}
}

// Parse builds a visitor's preferences from their cookies, overridden by
// any preference passed as a query parameter.
func Parse(cookies, params map[string]string) Prefs {
	p := Defaults
	p.apply(cookies)
	p.apply(params)
	if params["tr"] == "0" {
		p.AutoTranslate = false
	}
	return p
}

// Encode lists the preferences that differ from Defaults as name=value
// pairs, the format of the ?prefs= parameter.
func (p *Prefs) Encode() string {
	var pairs []string
	for _, pref := range All() {
		switch f := p.field(pref.Name).(type) {
		case *bool:
			if *f != *Defaults.field(pref.Name).(*bool) {
				if *f {
					pairs = append(pairs, pref.Name+"=on")
				} else {
					pairs = append(pairs, pref.Name+"=")
				}
			}
		case *string:
			if *f != *Defaults.field(pref.Name).(*string) {
				pairs = append(pairs, pref.Name+"="+*f)
			}
		}
	}
	return strings.Join(pairs, ",")
}

// IsDefault reports whether a submitted settings form value equals the
// default, in which case its cookie is removed rather than stored.
func IsDefault(pref Pref, val string) bool {
	switch pref.Kind {
	case Checkbox:
		return (val == "on") == *Defaults.field(pref.Name).(*bool)
	case Select:
		def := *Defaults.field(pref.Name).(*string)
		if pref.Name == "theme" {
			return val == def
		}
		return !slices.Contains(pref.Options, val) || val == def
	default:
		return val == *Defaults.field(pref.Name).(*string)
	}
}
