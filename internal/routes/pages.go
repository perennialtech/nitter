// SPDX-License-Identifier: AGPL-3.0-only
package routes

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/perennialtech/nitter/internal/api"
	"github.com/perennialtech/nitter/internal/auth"
	"github.com/perennialtech/nitter/internal/cache"
	"github.com/perennialtech/nitter/internal/formatters"
	"github.com/perennialtech/nitter/internal/types"
	"github.com/perennialtech/nitter/internal/utils"
	"github.com/perennialtech/nitter/internal/views"
)

func (s *Server) trends(ctx context.Context) (tab1, tab2 []types.Trend, err error) {
	var g group
	g.go_(func() (err error) { tab1, err = api.Trends(ctx, s.cfg.TrendsTab1Woeid, s.cfg.TrendsMaxItems); return })
	g.go_(func() (err error) { tab2, err = api.Trends(ctx, s.cfg.TrendsTab2Woeid, s.cfg.TrendsMaxItems); return })
	err = g.wait()
	return
}

func listTitle(l types.List) string { return "@" + l.Username + "/" + l.Name }

func (s *Server) listRoutes() {
	respList := func(c *ctx, l types.List, body template.HTML, q types.Query) error {
		if l.ID == "" || l.Name == "" {
			return c.fail(http.StatusNotFound, `List "`+c.param("id")+`" not found`)
		}
		var rss string
		if s.cfg.EnableRSSList {
			rss = "/i/lists/" + c.param("id") + "/rss"
		}
		return c.page(views.RenderList(body, q, l), views.PageOpts{TitleText: listTitle(l), RSS: rss, Banner: l.Banner})
	}

	s.get("/@name/lists/@slug/?", func(c *ctx) error {
		name, rawSlug := c.param("name"), c.param("slug")
		if strings.Contains(name, ".") || name == "i" || rawSlug == "memberships" {
			return errPass
		}
		slug, _ := url.PathUnescape(rawSlug)
		l, err := cache.List(c.r.Context(), name, slug, "")
		if err != nil {
			return err
		}
		if l.ID == "" {
			return c.fail(http.StatusNotFound, `List "`+rawSlug+`" not found`)
		}
		return c.redirect("/i/lists/" + l.ID)
	})

	s.get("/i/lists/@id/?", func(c *ctx) error {
		if strings.Contains(c.param("id"), ".") {
			return errPass
		}
		ctx := c.r.Context()
		l, err := cache.List(ctx, "", "", c.param("id"))
		if err != nil {
			return err
		}
		tl, err := api.GraphListTweets(ctx, l.ID, c.cursor())
		if err != nil {
			return err
		}
		return respList(c, l, views.RenderTimelineTweets(tl, c.prefs(), c.r.URL.EscapedPath(), nil), tl.Query)
	})

	s.get("/i/lists/@id/members", func(c *ctx) error {
		if strings.Contains(c.param("id"), ".") {
			return errPass
		}
		ctx := c.r.Context()
		l, err := cache.List(ctx, "", "", c.param("id"))
		if err != nil {
			return err
		}
		members, err := api.GraphListMembers(ctx, l, c.cursor())
		if err != nil {
			return err
		}
		return respList(c, l, views.RenderTimelineUsers(members, c.prefs(), c.r.URL.EscapedPath()), members.Query)
	})
}

func (s *Server) communityRoutes() {
	type pageFunc func(c *ctx, cm types.Community) (title string, nav, body template.HTML, err error)
	add := func(suffix string, f pageFunc) {
		s.get("/i/communities/@id"+suffix, func(c *ctx) error {
			if strings.Contains(c.param("id"), ".") {
				return errPass
			}
			cm, err := cache.Community(c.r.Context(), c.param("id"))
			if err != nil {
				return err
			}
			title, nav, body, err := f(c, cm)
			if err != nil {
				return err
			}
			if cm.ID == "" || cm.Name == "" {
				return c.fail(http.StatusNotFound, `Community "`+c.param("id")+`" not found`)
			}
			return c.page(views.RenderCommunity(body, nav, cm), views.PageOpts{TitleText: title, Banner: cm.Banner})
		})
	}
	tweets := func(c *ctx, tl types.Timeline, err error) (template.HTML, error) {
		if err != nil {
			return "", err
		}
		return views.RenderTimelineTweets(tl, c.prefs(), c.r.URL.EscapedPath(), nil), nil
	}

	add("/?", func(c *ctx, cm types.Community) (string, template.HTML, template.HTML, error) {
		tl, err := api.GraphCommunityTweets(c.r.Context(), cm.ID, "Relevance", c.cursor())
		body, err := tweets(c, tl, err)
		return cm.Name, views.RenderCommunityTabs(types.QueryPosts, cm), body, err
	})
	add("/latest", func(c *ctx, cm types.Community) (string, template.HTML, template.HTML, error) {
		tl, err := api.GraphCommunityTweets(c.r.Context(), cm.ID, "Recency", c.cursor())
		body, err := tweets(c, tl, err)
		return cm.Name + " - Latest", views.RenderCommunityTabs(types.QueryReplies, cm), body, err
	})
	add("/media", func(c *ctx, cm types.Community) (string, template.HTML, template.HTML, error) {
		tl, err := api.GraphCommunityMedia(c.r.Context(), cm.ID, c.cursor())
		body, err := tweets(c, tl, err)
		return cm.Name + " - Media", views.RenderCommunityTabs(types.QueryMedia, cm), body, err
	})
	add("/about", func(c *ctx, cm types.Community) (string, template.HTML, template.HTML, error) {
		mods, err := cache.CommunityModerators(c.r.Context(), cm.ID)
		return cm.Name + " - About", views.RenderCommunityTabs(types.QueryUserList, cm),
			views.RenderCommunityAbout(cm, mods), err
	})
	add("/members", func(c *ctx, cm types.Community) (string, template.HTML, template.HTML, error) {
		members, err := api.GraphCommunityMembers(c.r.Context(), cm.ID, c.cursor())
		return cm.Name + " - Members", views.RenderMemberTabs(cm, false),
			views.RenderTimelineUsers(members, c.prefs(), c.r.URL.EscapedPath()), err
	})
	add("/moderators", func(c *ctx, cm types.Community) (string, template.HTML, template.HTML, error) {
		mods, err := cache.CommunityModerators(c.r.Context(), cm.ID)
		return cm.Name + " - Moderators", views.RenderMemberTabs(cm, true),
			views.RenderTimelineUsers(types.Result[types.User]{Content: mods}, c.prefs(), c.r.URL.EscapedPath()), err
	})
	add("/hashtag/@tag", func(c *ctx, cm types.Community) (string, template.HTML, template.HTML, error) {
		tag, _ := url.PathUnescape(c.param("tag"))
		tl, err := api.GraphCommunityHashtags(c.r.Context(), cm.ID, tag, c.cursor())
		body, err := tweets(c, tl, err)
		return cm.Name + " - #" + tag, views.RenderHashtagHeader(cm, tag), body, err
	})
}

func (s *Server) resolverRoutes() {
	resolve := func(c *ctx, u, kind string) error {
		resolved, err := api.Resolve(c.r.Context(), u, c.prefs())
		if err != nil {
			return err
		}
		if resolved == "" {
			return c.fail(http.StatusOK, "Invalid "+kind+" link")
		}
		return c.redirect(utils.LocalizeTwitterLink(resolved))
	}
	s.get("/cards/@card/@id", func(c *ctx) error {
		return resolve(c, "https://cards.twitter.com/cards/"+c.param("card")+"/"+c.param("id"), "card")
	})
	s.get("/t.co/@url", func(c *ctx) error {
		return resolve(c, "https://t.co/"+c.param("url"), "t.co")
	})
}

func parseTweetPath(path string) (username, id string) {
	parts := strings.Split(path, "/")
	if len(parts) >= 3 && (parts[1] == "status" || parts[1] == "statuses") {
		tweetID, _, _ := strings.Cut(parts[2], "?")
		tweetID, _, _ = strings.Cut(tweetID, "#")
		if tweetID != "" && allIn(tweetID, isDigit) {
			return parts[0], tweetID
		}
	}
	return "", ""
}

// parseTweetURL extracts the username and tweet ID from a Twitter or
// nitter status URL. URLs on other hosts are accepted too, since the
// instance can be reached under names other than its hostname.
func (s *Server) parseTweetURL(u string) (username, id string) {
	path := strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	for _, prefix := range []string{"twitter.com/", "x.com/", "mobile.twitter.com/", "www.twitter.com/",
		"www.x.com/", s.cfg.Hostname + "/"} {
		if rest, ok := strings.CutPrefix(path, prefix); ok {
			return parseTweetPath(rest)
		}
	}
	if i := strings.IndexByte(path, '/'); i > 0 {
		return parseTweetPath(path[i+1:])
	}
	return "", ""
}

func (s *Server) embedRoutes() {
	s.get("/i/videos/tweet/@id", func(c *ctx) error {
		id := c.param("id")
		t, err := api.TweetByRestID(c.r.Context(), id)
		if err != nil {
			return err
		}
		p := c.prefs()
		if t == nil {
			return c.html(http.StatusOK, views.RenderErrorEmbed("Tweet not found", p, s.cfg, c.r, id, ""))
		}
		if !t.HasVideos() {
			return c.html(http.StatusOK, views.RenderErrorEmbed("No video in tweet", p, s.cfg, c.r, id, t.User.Username))
		}
		return c.html(http.StatusOK, views.RenderVideoEmbed(t, s.cfg, c.r))
	})

	s.get("/@user/status/@id/embed", func(c *ctx) error {
		id := c.param("id")
		t, err := api.TweetByRestID(c.r.Context(), id)
		if err != nil {
			return err
		}
		p := c.prefs()
		if t == nil {
			return c.html(http.StatusOK, views.RenderErrorEmbed("Tweet not found", p, s.cfg, c.r, id, c.param("user")))
		}
		return c.html(http.StatusOK, views.RenderTweetEmbed(t, c.fullPath(), p, s.cfg, c.r))
	})

	s.get("/embed/Tweet.html", func(c *ctx) error {
		if id := c.param("id"); id != "" {
			return c.redirect("/i/status/" + id + "/embed")
		}
		return c.status(http.StatusNotFound)
	})

	s.get("/api/oembed", func(c *ctx) error {
		c.w.Header().Set("Access-Control-Allow-Origin", "*")
		u, format := c.param("url"), c.param("format")
		if format != "" && format != "json" {
			return c.text(http.StatusNotImplemented, "text/plain", "Only JSON format is supported")
		}
		if u == "" {
			return c.text(http.StatusBadRequest, "text/plain", "Missing url parameter")
		}
		username, tweetID := s.parseTweetURL(u)
		if username == "" || tweetID == "" {
			return c.text(http.StatusBadRequest, "text/plain", "Invalid tweet URL")
		}
		t, err := api.TweetByRestID(c.r.Context(), tweetID)
		if err != nil {
			return err
		}
		if t == nil {
			return c.status(http.StatusNotFound)
		}
		maxwidth := 550
		if v := c.param("maxwidth"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				maxwidth = min(max(n, 220), 550)
			}
		}
		prefix := s.cfg.URLPrefix()
		embedURL := prefix + "/" + t.User.Username + "/status/" + tweetID + "/embed"
		res := map[string]any{
			"version":       "1.0",
			"type":          "rich",
			"provider_name": s.cfg.Title,
			"provider_url":  prefix,
			"title":         formatters.StripHTML(t.Text, false),
			"author_name":   t.User.Fullname,
			"author_url":    prefix + "/" + t.User.Username,
			"url":           embedURL,
			"width":         maxwidth,
			"height":        nil,
			"cache_age":     "3153600000",
			"html":          views.RenderOembedIframe(embedURL, maxwidth),
		}
		if len(t.Media) > 0 {
			res["thumbnail_url"] = prefix + utils.PicURL(t.Media[0].Thumb())
			res["thumbnail_width"] = maxwidth
			res["thumbnail_height"] = maxwidth
		}
		return c.json(res)
	})
}

func (c *ctx) json(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.text(http.StatusOK, "application/json", string(data))
}

func (s *Server) articleRoutes() {
	s.get("/i/article/@id", func(c *ctx) error {
		id := c.param("id")
		if !allIn(id, isDigit) {
			return errPass
		}
		ctx := c.r.Context()
		a, err := api.GraphArticle(ctx, id)
		if err != nil {
			return err
		}
		if a == nil {
			return c.fail(http.StatusNotFound, "Article not found")
		}
		var ids []string
		for _, e := range a.Entities {
			if e.Kind == "TWEET" {
				ids = append(ids, e.TweetID)
			}
		}
		tweets := map[int64]*types.Tweet{}
		if len(ids) > 0 {
			// Embedded tweets are optional; the article renders without them.
			if ts, err := api.GraphTweetResults(ctx, ids); err == nil {
				for _, t := range ts {
					tweets[t.ID] = t
				}
			}
		}
		p := c.prefs()
		return c.page(views.RenderArticle(a, tweets, c.fullPath(), p, id), views.PageOpts{
			TitleText:   a.Title,
			TwitterLink: "https://x.com/" + a.User.Username + "/article/" + id,
		})
	})
	redirect := func(c *ctx) error {
		if strings.Contains(c.param("name"), ".") || !allIn(c.param("id"), isDigit) {
			return errPass
		}
		return c.redirect("/i/article/" + c.param("id"))
	}
	s.get("/@name/article/@id/?", redirect)
	s.get("/@name/status/@id/article", redirect)
}

func isAlnumID(s string) bool { return s != "" && allIn(s, isAlnum) }

// streamManifest serves a broadcast or space's HLS playlist, rewritten so
// its segments go through the video proxy when the visitor wants that.
func (c *ctx) streamManifest(m3u8 string) error {
	if m3u8 == "" {
		return c.status(http.StatusNotFound)
	}
	manifest := safeFetch(c.r.Context(), m3u8)
	if manifest == "" {
		return c.status(http.StatusBadGateway)
	}
	return c.text(http.StatusOK, m3u8Mime, formatters.ProxifyVideo(manifest, c.prefs().ProxyVideos, m3u8))
}

func (s *Server) broadcastRoutes() {
	// A broadcast that fails to load is shown as not found.
	broadcast := func(c *ctx) types.Broadcast {
		bc, err := cache.Broadcast(c.r.Context(), c.param("id"))
		if err != nil {
			return types.Broadcast{}
		}
		return bc
	}
	s.get("/i/broadcasts/@id", func(c *ctx) error {
		if !isAlnumID(c.param("id")) {
			return errPass
		}
		bc := broadcast(c)
		if bc.ID == "" {
			return c.fail(http.StatusNotFound, "Broadcast not found")
		}
		return c.page(views.RenderBroadcast(bc, c.prefs(), c.r.URL.EscapedPath()),
			views.PageOpts{TitleText: bc.Title, OgTitle: bc.Title})
	})
	s.get("/i/broadcasts/@id/stream", func(c *ctx) error {
		if !isAlnumID(c.param("id")) {
			return errPass
		}
		return c.streamManifest(broadcast(c).M3u8URL)
	})
}

func (s *Server) spaceRoutes() {
	s.get("/i/spaces/@id", func(c *ctx) error {
		if !isAlnumID(c.param("id")) {
			return errPass
		}
		sp, err := cache.AudioSpace(c.r.Context(), c.param("id"))
		if err != nil {
			return err
		}
		if sp.ID == "" {
			return c.fail(http.StatusNotFound, "Space not found")
		}
		return c.page(views.RenderSpace(sp, c.prefs(), c.r.URL.EscapedPath()),
			views.PageOpts{TitleText: sp.Title, OgTitle: sp.Title})
	})
	s.get("/i/spaces/@id/stream", func(c *ctx) error {
		if !isAlnumID(c.param("id")) {
			return errPass
		}
		sp, err := cache.AudioSpace(c.r.Context(), c.param("id"))
		if err != nil {
			return err
		}
		return c.streamManifest(sp.M3u8URL)
	})
}

func (s *Server) debugRoutes() {
	s.get("/.health", func(c *ctx) error {
		return c.text(http.StatusOK, "application/json", string(auth.HealthJSON()))
	})
	s.get("/.sessions", func(c *ctx) error {
		if !s.cfg.EnableDebug {
			return errPass
		}
		return c.text(http.StatusOK, "application/json", string(auth.DebugJSON()))
	})
}
