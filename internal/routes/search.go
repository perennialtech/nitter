// SPDX-License-Identifier: AGPL-3.0-only
package routes

import (
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/perennialtech/nitter/internal/api"
	"github.com/perennialtech/nitter/internal/cache"
	"github.com/perennialtech/nitter/internal/query"
	"github.com/perennialtech/nitter/internal/types"
	"github.com/perennialtech/nitter/internal/utils"
	"github.com/perennialtech/nitter/internal/views"
)

func searchUsername(s string) bool {
	return len(s) > 0 && len(s) <= 15 && allIn(s, isUsernameChar)
}

func (s *Server) searchRoutes() {
	s.get("/search/?", func(c *ctx) error {
		q := c.param("q")
		if len(q) > 500 {
			return c.fail(http.StatusBadRequest, "Search input too long.")
		}
		if path := utils.PastedTwitterLink(q); path != "" {
			return c.redirect(path)
		}
		ctx, p := c.r.Context(), c.prefs()
		title := "Search"
		if q != "" {
			title += " (" + q + ")"
		}

		sq := query.Init(c.query, "")
		// x.com URL compat: f=user and f=list map to our kind names
		switch c.param("f") {
		case "":
			advanced := len(sq.Filters) != 0 || len(sq.Excludes) != 0 ||
				sq.Since != "" || sq.Until != "" || sq.MinLikes != "" || strings.ContainsAny(q, "#:")
			if !advanced {
				sq.Kind = types.QueryUsers
				if c.cursor() == "" {
					handle, mentioned := strings.CutPrefix(strings.TrimSpace(q), "@")
					if searchUsername(handle) {
						if mentioned {
							return c.redirect("/" + handle)
						}
						if id, err := cache.UserID(ctx, handle); err == nil && id != "" && id != "suspended" {
							return c.redirect("/" + handle)
						}
					}
				}
			}
		case "user":
			sq.Kind = types.QueryUsers
		case "list":
			sq.Kind = types.QueryLists
		}
		// media searches support view modes, defaulting like /user/media
		if sq.Kind == types.QueryMedia && sq.View != "timeline" && sq.View != "grid" && sq.View != "gallery" {
			sq.View = strings.ToLower(p.MediaView)
		}

		switch sq.Kind {
		case types.QueryCommunitySearch, types.QueryCommunities, types.QueryCommunitiesLatest:
			if s.cfg.GuestAuth {
				return c.fail(http.StatusNotFound, "Community search requires account sessions")
			}
			return c.communitySearch(sq, title)
		case types.QueryUsers:
			if handles := strings.Split(q, ","); len(handles) > 1 {
				if !slices.ContainsFunc(handles, func(handle string) bool { return !searchUsername(handle) }) {
					return c.redirect("/" + q)
				}
			}
			users, err := api.GraphUserSearch(ctx, sq, c.cursor())
			if errors.Is(err, types.ErrInternal) {
				users, err = types.Result[types.User]{Beginning: true, Query: sq}, nil
			}
			if err != nil {
				return err
			}
			return c.page(views.RenderUserSearch(users, p), views.PageOpts{TitleText: title})
		case types.QueryTweets, types.QueryTop, types.QueryMedia:
			tweets, err := api.GraphTweetSearch(ctx, sq, c.cursor())
			if err != nil {
				return err
			}
			var rss string
			if s.cfg.EnableRSSSearch {
				rss = "/search/rss?" + query.URL(sq)
			}
			return c.page(views.RenderTweetSearch(tweets, p, c.fullPath(), nil),
				views.PageOpts{TitleText: title, RSS: rss})
		case types.QueryLists:
			lists, err := api.GraphListSearch(ctx, sq, c.cursor())
			if err != nil {
				return err
			}
			return c.page(views.RenderListSearch(lists, p, c.fullPath()), views.PageOpts{TitleText: title})
		}
		return c.fail(http.StatusNotFound, "Invalid search")
	})

	s.get("/hashtag/@hash", func(c *ctx) error {
		tag, _ := url.PathUnescape(c.param("hash"))
		return c.redirect("/search?f=tweets&q=" + utils.EncodeURL("#"+tag))
	})

	s.get("/opensearch", func(c *ctx) error {
		u := s.cfg.URLPrefix() + "/search?f=tweets&q="
		return c.text(http.StatusOK, "application/opensearchdescription+xml",
			views.GenerateOpenSearchXML(s.cfg.Title, s.cfg.Hostname, u))
	})
}

func (c *ctx) communitySearch(q types.Query, title string) error {
	ctx, after, p := c.r.Context(), c.cursor(), c.prefs()
	path := c.r.URL.EscapedPath()
	var body template.HTML
	var empty bool
	if q.Kind == types.QueryCommunitySearch {
		results, err := api.GraphCommunitySearch(ctx, q, after)
		if err != nil {
			return err
		}
		body = views.RenderTimelineCommunities(results, p, path)
		empty = len(results.Content) == 0
	} else {
		results, err := api.GraphCommunityPostSearch(ctx, q, after)
		if err != nil {
			return err
		}
		body = views.RenderTimelineTweets(results, p, path, nil)
		empty = len(results.Content) == 0
	}
	if c.param("scroll") != "" {
		if empty {
			return c.status(http.StatusNoContent)
		}
		return c.html(http.StatusOK, string(body))
	}
	return c.page(views.RenderCommunitySearch(q, body), views.PageOpts{TitleText: title})
}
