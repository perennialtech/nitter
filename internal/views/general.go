// SPDX-License-Identifier: AGPL-3.0-only

package views

import (
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/perennialtech/nitter/internal/config"
	"github.com/perennialtech/nitter/internal/formatters"
	"github.com/perennialtech/nitter/internal/prefs"
	"github.com/perennialtech/nitter/internal/utils"
)

// PageOpts are renderMain's (and renderHead's) optional parameters.
type PageOpts struct {
	TitleText string
	Desc      string
	OgTitle   string
	RSS       string
	Video     string
	Images    []string
	Banner    string
	// TwitterLink is the "Open in X" link; RenderMain derives it from the
	// request when empty. RenderHead emits it as the rel=alternate link
	// (Nim's alternate parameter).
	TwitterLink string
	Oembed      string
}

func toTheme(theme string) string {
	return strings.ReplaceAll(strings.ToLower(theme), " ", "_")
}

// requestParams merges the query string with the {name} path value, the
// subset of jester's request.params the views read.
func requestParams(r *http.Request) url.Values {
	params := url.Values{}
	for k, v := range r.URL.Query() {
		params[k] = v
	}
	if name := r.PathValue("name"); name != "" {
		params.Set("name", name)
	}
	return params
}

type navbarData struct {
	Title, RSS, Canonical, SettingsURL string
	ShowNotes                          bool
	LP                                 template.HTML
}

func renderNavbar(cfg *config.Config, r *http.Request, rss, canonical string) template.HTML {
	params := requestParams(r)
	path := params.Get("referer")
	if path == "" {
		path = r.URL.Path
		if q := utils.FilterParams(params).Encode(); q != "" {
			path += "?" + q
		}
		if strings.Contains(path, "/status/") {
			path += "#m"
		}
	}
	return exec("navbar", navbarData{
		Title:       cfg.Title,
		RSS:         rss,
		Canonical:   canonical,
		SettingsURL: "/settings?referer=" + utils.EncodeURL(path),
		ShowNotes:   cfg.ShowCommunityNotesNav && !cfg.GuestAuth,
		LP:          template.HTML(lpSVG),
	})
}

type headImage struct{ Preload, Image string }

type headData struct {
	Theme, SiteTitle, OpensearchURL, Alternate, RSS, Oembed, OembedTitle string
	Title, OgType, OgTitle, Desc, BannerURL, TwitterCard, Video          string
	HLS, InfiniteScroll                                                  bool
	Images                                                               []headImage
}

// RenderHead renders the <head> element.
func RenderHead(p *prefs.Prefs, cfg *config.Config, r *http.Request, o PageOpts) template.HTML {
	d := headData{
		SiteTitle:      cfg.Title,
		OpensearchURL:  cfg.URLPrefix() + "/opensearch",
		Alternate:      o.TwitterLink,
		RSS:            o.RSS,
		Oembed:         o.Oembed,
		OembedTitle:    "oEmbed",
		HLS:            p.HlsPlayback,
		InfiniteScroll: p.InfiniteScroll,
		Title:          cfg.Title,
		OgTitle:        o.TitleText,
		Desc:           formatters.StripHTML(o.Desc, false),
		TwitterCard:    "summary_large_image",
		Video:          o.Video,
	}
	if theme := toTheme(p.Theme); theme != "" {
		d.Theme = "/css/themes/" + theme + ".css"
	}
	switch {
	case o.Video != "":
		d.OgType = "video"
	case o.RSS != "":
		d.OgType = "object"
	case len(o.Images) > 0:
		d.OgType = "photo"
	default:
		d.OgType = "article"
	}
	if o.RSS != "" {
		d.TwitterCard = "summary"
	}
	if o.TitleText != "" {
		d.OembedTitle = o.TitleText
		d.Title = o.TitleText + " | " + cfg.Title
	}
	if o.OgTitle != "" {
		d.OgTitle = o.OgTitle
	}
	if o.Banner != "" && !strings.HasPrefix(o.Banner, "#") {
		d.BannerURL = utils.PicURL(o.Banner)
	}
	for _, u := range o.Images {
		if u == "" {
			continue
		}
		img := headImage{Image: cfg.URLPrefix() + utils.PicURL(u)}
		if strings.Contains(u, "400x400") {
			img.Preload = utils.PicURL(u)
		} else {
			img.Preload = smallPic(u)
		}
		d.Images = append(d.Images, img)
	}
	return exec("head", d)
}

// RenderMain renders a full page around body.
func RenderMain(body template.HTML, r *http.Request, cfg *config.Config, p *prefs.Prefs, o PageOpts) string {
	if p == nil {
		p = &prefs.Defaults
	}
	if o.TwitterLink == "" {
		o.TwitterLink = formatters.TwitterLink(r.URL.Path, requestParams(r))
	}
	bodyClass := ""
	if p.StickyNav {
		bodyClass = "fixed-nav"
	}
	return string(exec("main", map[string]any{
		"Head":      RenderHead(p, cfg, r, o),
		"BodyClass": bodyClass,
		"Navbar":    renderNavbar(cfg, r, o.RSS, o.TwitterLink),
		"Body":      body,
	}))
}

// RenderError renders an error panel. msg is HTML.
func RenderError(msg string) template.HTML {
	return exec("error", template.HTML(msg))
}
