// SPDX-License-Identifier: AGPL-3.0-only
package routes

import (
	"hash/fnv"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/perennialtech/nitter/internal/api"
	"github.com/perennialtech/nitter/internal/cache"
	"github.com/perennialtech/nitter/internal/formatters"
	"github.com/perennialtech/nitter/internal/query"
	"github.com/perennialtech/nitter/internal/types"
	"github.com/perennialtech/nitter/internal/views"
)

func rssKey(page, name, cursor string) string {
	key := page + ":" + name
	if cursor != "" {
		key += ":" + cursor
	}
	return key
}

func hashString(s string) string {
	h := fnv.New64a()
	h.Write([]byte(s))
	return strconv.FormatUint(h.Sum64(), 16)
}

func (c *ctx) timelineRss(q types.Query) (types.Rss, error) {
	name := c.param("name")
	after := c.cursor()
	users := names(name)
	p := c.prefs()

	var profile types.Profile
	if len(users) == 1 {
		var err error
		if profile, err = fetchProfile(c.r.Context(), after, q, p, true, true); err != nil {
			return types.Rss{}, err
		}
	} else {
		q.FromUser = users
		tl, err := api.GraphTweetSearch(c.r.Context(), q, after)
		if err != nil {
			return types.Rss{}, err
		}
		profile.Tweets = tl
		profile.User = types.User{
			Username: name,
			Fullname: strings.Join(users, " | "),
			UserPic:  "https://abs.twimg.com/sticky/default_profile_images/default_profile.png",
		}
	}
	if profile.User.Suspended {
		return types.Rss{Feed: profile.User.Username, Cursor: "suspended"}, nil
	}
	if profile.User.Fullname == "" {
		return types.Rss{}, nil
	}
	feed := views.RenderTimelineRss(profile, c.s.cfg, p, len(users) > 1)
	return types.Rss{Feed: feed, Cursor: profile.Tweets.Bottom}, nil
}

func (c *ctx) communityNotesRss(tab string) (types.Rss, error) {
	var tl types.Timeline
	var err error
	if tab == "new" {
		tl, err = api.GraphCommunityNotesNew(c.r.Context())
	} else {
		tl, err = api.GraphBirdwatchGlobalTimeline(c.r.Context())
	}
	if err != nil {
		return types.Rss{}, err
	}
	// These timelines have no cursor and the cache treats an empty one as a
	// miss, so the oldest tweet ID stands in for Min-Id.
	cursor := tl.Bottom
	if len(cursor) < 3 && len(tl.Content) > 0 {
		if last := tl.Content[len(tl.Content)-1]; len(last) > 0 {
			cursor = strconv.FormatInt(last[len(last)-1].ID, 10)
		}
	}
	return types.Rss{Feed: views.RenderCommunityNotesRss(tl.Content, tab, c.s.cfg, c.prefs()), Cursor: cursor}, nil
}

func (c *ctx) respRss(rss types.Rss, page string) error {
	if rss.Cursor == "" {
		info := " "
		switch page {
		case "User":
			info = ` "` + c.param("name") + `" `
		case "List":
			info = ` "` + c.param("id") + `" `
		}
		return c.fail(http.StatusNotFound, page+info+"not found")
	}
	if rss.Cursor == "suspended" {
		return c.fail(http.StatusNotFound, formatters.Suspended(c.param("name")))
	}
	c.w.Header().Set("Min-Id", rss.Cursor)
	return c.text(http.StatusOK, "application/rss+xml; charset=utf-8", rss.Feed)
}

// cachedRss serves the feed cached at key, or builds, caches and serves it.
func (c *ctx) cachedRss(key, page string, build func() (types.Rss, error)) error {
	rss, err := cache.Rss(c.r.Context(), key, build)
	if err != nil {
		return err
	}
	return c.respRss(rss, page)
}

func (s *Server) rssRoutes() {
	disabled := func(c *ctx) error { return c.fail(http.StatusForbidden, "RSS feed is disabled") }

	s.get("/search/rss", func(c *ctx) error {
		if !s.cfg.EnableRSSSearch {
			return disabled(c)
		}
		if len(c.param("q")) > 200 {
			return c.fail(http.StatusBadRequest, "Search input too long.")
		}
		q := query.Init(c.query, "")
		if q.Kind != types.QueryTweets && q.Kind != types.QueryTop && q.Kind != types.QueryMedia {
			return c.fail(http.StatusBadRequest, "Only Tweet searches are allowed for RSS feeds.")
		}
		cursor := c.cursor()
		key := rssKey("search", hashString(query.URL(q)), cursor)
		return c.cachedRss(key, "Search", func() (types.Rss, error) {
			tweets, err := api.GraphTweetSearch(c.r.Context(), q, cursor)
			if err != nil {
				return types.Rss{}, err
			}
			feed := views.RenderSearchRss(tweets.Content, q.Text, query.URL(q), s.cfg, c.prefs())
			return types.Rss{Feed: feed, Cursor: tweets.Bottom}, nil
		})
	})

	for _, tab := range []string{"helpful", "new"} {
		path := "/i/communitynotes/rss"
		if tab == "new" {
			path = "/i/communitynotes/new/rss"
		}
		s.get(path, func(c *ctx) error {
			if !s.cfg.EnableRSSCommunityNotes || s.cfg.GuestAuth {
				return disabled(c)
			}
			return c.cachedRss(rssKey("communitynotes", tab, ""), "Community Notes", func() (types.Rss, error) {
				return c.communityNotesRss(tab)
			})
		})
	}

	s.get("/@name/rss", func(c *ctx) error {
		name := c.param("name")
		if strings.Contains(name, ".") {
			return errPass
		}
		if !s.cfg.EnableRSSUserTweets {
			return disabled(c)
		}
		return c.cachedRss(rssKey("twitter", name, c.cursor()), "User", func() (types.Rss, error) {
			return c.timelineRss(types.Query{Kind: types.QueryPosts, FromUser: []string{name}})
		})
	})

	s.get("/@name/@tab/rss", func(c *ctx) error {
		name, tab := c.param("name"), c.param("tab")
		if strings.Contains(name, ".") ||
			!slices.Contains([]string{"with_replies", "media", "search", "articles", "reposts"}, tab) ||
			(tab == "articles" || tab == "reposts") && strings.Contains(name, ",") {
			return errPass
		}
		if !tabRssEnabled(s.cfg, tab) {
			return disabled(c)
		}
		q := c.timelineQuery(tab, name, c.prefs())
		searchKey := ""
		if tab == "search" {
			searchKey = ":" + hashString(query.URL(q))
		}
		return c.cachedRss(rssKey(tab, name+searchKey, c.cursor()), "User", func() (types.Rss, error) {
			return c.timelineRss(q)
		})
	})

	s.get("/@name/lists/@slug/rss", func(c *ctx) error {
		if c.param("name") == "i" {
			return errPass
		}
		if !s.cfg.EnableRSSList {
			return disabled(c)
		}
		slug, _ := url.PathUnescape(c.param("slug"))
		list, err := cache.List(c.r.Context(), c.param("name"), slug, "")
		if err != nil {
			return err
		}
		if list.ID == "" {
			return c.fail(http.StatusNotFound, `List "`+c.param("slug")+`" not found`)
		}
		target := "/i/lists/" + list.ID + "/rss"
		if cursor := c.cursor(); cursor != "" {
			target += "?cursor=" + url.PathEscape(cursor)
		}
		return c.redirect(target)
	})

	s.get("/i/lists/@id/rss", func(c *ctx) error {
		if !s.cfg.EnableRSSList {
			return disabled(c)
		}
		id, cursor := c.param("id"), c.cursor()
		return c.cachedRss(rssKey("lists", id, cursor), "List", func() (types.Rss, error) {
			list, err := cache.List(c.r.Context(), "", "", id)
			if err != nil {
				return types.Rss{}, err
			}
			tl, err := api.GraphListTweets(c.r.Context(), list.ID, cursor)
			if err != nil {
				return types.Rss{}, err
			}
			return types.Rss{Feed: views.RenderListRss(tl.Content, list, s.cfg, c.prefs()), Cursor: tl.Bottom}, nil
		})
	})
}
