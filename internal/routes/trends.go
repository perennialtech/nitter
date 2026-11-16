// SPDX-License-Identifier: AGPL-3.0-only

package routes

import (
	"cmp"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/perennialtech/nitter/internal/api"
	"github.com/perennialtech/nitter/internal/cache"
	"github.com/perennialtech/nitter/internal/types"
	"github.com/perennialtech/nitter/internal/views"
)

type storySortMode struct{ key, label string }

var storySortModes = []storySortMode{
	{"relevant", "Relevant"},
	{"recent", "Recent"},
	{"count", "Post count"},
	{"category", "Category"},
}

func sortStories(stories []types.Story, mode string) {
	switch mode {
	case "recent":
		slices.SortStableFunc(stories, func(a, b types.Story) int {
			return cmp.Compare(b.CreatedMs, a.CreatedMs)
		})
	case "count":
		slices.SortStableFunc(stories, func(a, b types.Story) int {
			return cmp.Compare(b.PostCount, a.PostCount)
		})
	case "category":
		slices.SortStableFunc(stories, func(a, b types.Story) int {
			return strings.Compare(strings.ToLower(a.Category), strings.ToLower(b.Category))
		})
	}
}

func (s *Server) trendsRoutes() {
	cfg := s.cfg
	guard := func(needsAccount bool, h handler) handler {
		return func(c *ctx) error {
			if !cfg.EnableTrendsPages || (needsAccount && cfg.GuestAuth) {
				return c.fail(http.StatusNotFound, "Page not found")
			}
			return h(c)
		}
	}

	ttl := time.Duration(cfg.TrendsCacheMinutes) * time.Minute
	s.get("/i/trends/?", guard(false, func(c *ctx) error {
		ctx := c.r.Context()
		tab := c.param("tab")
		if tab == "news" && cfg.GuestAuth {
			return c.fail(http.StatusNotFound, "Page not found")
		}
		tabs := []views.TrendsTab{
			{Label: cfg.TrendsTab1Name, URL: "/i/trends", Active: tab != "2" && tab != "news"},
			{Label: cfg.TrendsTab2Name, URL: "/i/trends?tab=2", Active: tab == "2"},
		}
		if !cfg.GuestAuth {
			tabs = append(tabs, views.TrendsTab{Label: "News", URL: "/i/trends?tab=news", Active: tab == "news"})
		}
		data := views.TrendsPage{Title: "Trends", Tabs: tabs}
		if tab == "news" {
			cursor := c.cursor()
			page, err := cache.Stories(ctx, cursor, ttl)
			if err != nil {
				return err
			}
			requested := strings.ToLower(c.param("sort"))
			index := slices.IndexFunc(storySortModes, func(option storySortMode) bool { return option.key == requested })
			mode := storySortModes[max(0, index)].key
			sortStories(page.Stories, mode)
			data.Stories = &page
			data.Cursor = cursor
			for _, option := range storySortModes {
				params := url.Values{"tab": {"news"}}
				if option.key != "relevant" {
					params.Set("sort", option.key)
				}
				data.Sort = append(data.Sort, views.TrendsTab{
					Label: option.label, URL: "?" + params.Encode(), Active: mode == option.key,
				})
				if mode == option.key {
					data.Params = params
				}
			}
		} else {
			woeid := cfg.TrendsTab1Woeid
			if tab == "2" {
				woeid = cfg.TrendsTab2Woeid
			}
			trends, err := api.Trends(ctx, woeid, cfg.TrendsMaxItems)
			if err != nil {
				return err
			}
			data.Trends = trends
		}
		return c.page(views.RenderTrendsPage(data), views.PageOpts{TitleText: "Trends"})
	}))

	s.get("/i/trending/?", guard(true, func(c *ctx) error {
		cursor := c.cursor()
		page, err := cache.Stories(c.r.Context(), cursor, ttl)
		if err != nil {
			return err
		}
		data := views.TrendsPage{Title: "Trending", Stories: &page, Cursor: cursor}
		return c.page(views.RenderTrendsPage(data), views.PageOpts{TitleText: "Trending"})
	}))

	story := func(c *ctx) error {
		id := c.param("id")
		if !isTweetID(id) {
			return c.fail(http.StatusNotFound, "Page not found")
		}
		ctx := c.r.Context()
		detail, err := cache.StoryDetail(ctx, id, ttl)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			// X can omit story metadata while still serving the post timeline.
			detail = types.StoryDetail{}
		}
		if len(detail.Timelines) == 0 {
			snowflake, _ := strconv.ParseUint(id, 10, 64)
			detail.Timelines = api.DefaultStoryTimelines(snowflake)
		}

		path := "/i/trending/" + id
		active := 0
		if strings.HasSuffix(strings.TrimSuffix(c.r.URL.Path, "/"), "/new") {
			timeline := detail.Timelines[len(detail.Timelines)-1].ID
			return c.redirect(path + "?" + url.Values{"timeline": {timeline}}.Encode())
		}
		wanted := strings.ReplaceAll(c.param("timeline"), " ", "+")
		for i, tab := range detail.Timelines {
			if tab.ID == wanted {
				active = i
				break
			}
		}
		posts, err := api.GraphStoryPosts(ctx, detail.Timelines[active].ID, c.cursor())
		if err != nil {
			return err
		}
		if detail.Title == "" && posts.Beginning && len(posts.Content) == 0 {
			return c.fail(http.StatusNotFound, "Story not found")
		}
		if c.param("scroll") == "true" && len(posts.Content) == 0 {
			return c.status(http.StatusNoContent)
		}
		title := detail.Title
		if title == "" {
			title = "Trending"
		}
		return c.page(views.RenderStory(detail, active, posts, c.prefs(), path),
			views.PageOpts{TitleText: title, Desc: detail.Summary})
	}
	s.get("/i/trending/@id/?", guard(true, story))
	s.get("/i/trending/@id/new/?", guard(true, story))
}
