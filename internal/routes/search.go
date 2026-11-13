// SPDX-License-Identifier: AGPL-3.0-only
package routes

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/perennialtech/nitter/internal/api"
	"github.com/perennialtech/nitter/internal/query"
	"github.com/perennialtech/nitter/internal/types"
	"github.com/perennialtech/nitter/internal/utils"
	"github.com/perennialtech/nitter/internal/views"
)

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
		case types.QueryUsers:
			if strings.Contains(q, ",") {
				return c.redirect("/" + q)
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
