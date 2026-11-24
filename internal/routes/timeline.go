// SPDX-License-Identifier: AGPL-3.0-only
package routes

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/perennialtech/nitter/internal/api"
	"github.com/perennialtech/nitter/internal/cache"
	"github.com/perennialtech/nitter/internal/config"
	"github.com/perennialtech/nitter/internal/flags"
	"github.com/perennialtech/nitter/internal/formatters"
	"github.com/perennialtech/nitter/internal/prefs"
	"github.com/perennialtech/nitter/internal/query"
	"github.com/perennialtech/nitter/internal/types"
	"github.com/perennialtech/nitter/internal/views"
)

func tabRssEnabled(cfg *config.Config, tab string) bool {
	switch tab {
	case "":
		return cfg.EnableRSSUserTweets
	case "with_replies":
		return cfg.EnableRSSUserReplies
	case "media":
		return cfg.EnableRSSUserMedia
	case "articles":
		return cfg.EnableRSSUserArticles
	case "reposts":
		return cfg.EnableRSSUserReposts
	case "search":
		return cfg.EnableRSSSearch
	}
	return false
}

func (c *ctx) mediaView(p *prefs.Prefs) string {
	if v := c.query.Get("view"); slices.Contains(query.MediaViews, v) {
		return v
	}
	return strings.ToLower(p.MediaView)
}

func (c *ctx) timelineQuery(tab, name, mediaView string) types.Query {
	switch tab {
	case "with_replies":
		return query.Replies(name)
	case "articles":
		return query.Articles(name)
	case "reposts":
		return query.Reposts(name)
	case "media":
		return query.Media(name, mediaView)
	case "search":
		q := query.Init(c.query, name)
		if q.Kind == types.QueryMedia && mediaView != "" {
			q.View = mediaView
		}
		return q
	}
	return types.Query{Kind: types.QueryPosts, FromUser: []string{name}}
}

// group runs functions concurrently and returns the first error.
type group struct {
	wg  sync.WaitGroup
	mu  sync.Mutex
	err error
}

func (g *group) go_(f func() error) {
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		if err := f(); err != nil {
			g.mu.Lock()
			if g.err == nil {
				g.err = err
			}
			g.mu.Unlock()
		}
	}()
}

func (g *group) wait() error {
	g.wg.Wait()
	return g.err
}

func searchTweets(ctx context.Context, q types.Query, after string) (types.Timeline, error) {
	names := append([]string(nil), q.FromUser...)
	for _, name := range query.ParseFromUsers(q.Text, cache.MaxMultiUsers) {
		if len(names) >= cache.MaxMultiUsers {
			break
		}
		if !slices.ContainsFunc(names, func(existing string) bool { return strings.EqualFold(existing, name) }) {
			names = append(names, name)
		}
	}

	if rest, ok := strings.CutPrefix(after, cache.MultiUserCursorPrefix); ok && len(names) > 0 {
		tl, err := cache.MultiUserTimeline(ctx, q, names, cache.TimelineKindFor(q), rest)
		if err != nil {
			return types.Timeline{}, err
		}
		tl.Query.Text = q.Text
		if tl.Bottom != "" {
			tl.Bottom = cache.MultiUserCursorPrefix + tl.Bottom
		}
		return tl, nil
	}

	tl, err := cache.TweetSearch(ctx, q, after)
	if err != nil && (errors.Is(err, types.ErrNoSessions) || errors.Is(err, types.ErrRateLimited)) && len(names) > 0 {
		fb, ferr := cache.MultiUserTimeline(ctx, q, names, cache.TimelineKindFor(q), "")
		if ferr == nil {
			fb.Query.Text = q.Text
			if fb.Bottom != "" {
				fb.Bottom = cache.MultiUserCursorPrefix + fb.Bottom
			}
			return fb, nil
		}
	}
	return tl, err
}

func fetchProfile(ctx context.Context, after string, q types.Query, p *prefs.Prefs,
	skipRail, rss, emptyTweets bool) (types.Profile, error) {
	name := q.FromUser[0]
	userID, err := cache.UserID(ctx, name)
	if err != nil {
		return types.Profile{}, err
	}
	switch userID {
	case "":
		return types.Profile{User: types.User{Username: name}}, nil
	case "suspended":
		return types.Profile{User: types.User{Username: name, Suspended: true}}, nil
	}

	// Cursors from before a deployment can still be in visitors' pages.
	if (q.Kind == types.QueryPosts || q.Kind == types.QueryReplies) &&
		strings.HasPrefix(after, "scroll") {
		after = ""
	}

	user, err := cache.User(ctx, name, true)
	if err != nil {
		return types.Profile{}, err
	}
	info, err := cache.AccountInfo(ctx, name, false)
	if err != nil {
		return types.Profile{}, err
	}
	if user.Protected || user.Suspended {
		return types.Profile{
			User:        user,
			Tweets:      types.Timeline{Query: q, Beginning: after == ""},
			AccountInfo: info,
		}, nil
	}

	var (
		g       group
		profile types.Profile
		rail    types.PhotoRail
	)
	if !skipRail && q.Kind != types.QueryMedia {
		g.go_(func() (err error) { rail, err = cache.PhotoRail(ctx, userID); return })
	}
	useGuestSearch := flags.GuestAuth && !rss
	search := func(q types.Query) (types.Profile, error) {
		tl, err := searchTweets(ctx, q, after)
		return types.Profile{Tweets: tl}, err
	}
	switch q.Kind {
	case types.QueryPosts:
		if useGuestSearch && p.GuestSearchFallback {
			profile, err = search(q)
		} else {
			profile, err = cache.UserTweets(ctx, userID, types.TimelineTweets, after, rss)
		}
	case types.QueryReplies:
		if useGuestSearch {
			profile, err = search(q)
		} else {
			profile, err = cache.UserTweets(ctx, userID, types.TimelineReplies, after, rss)
		}
	case types.QueryMedia:
		if !q.ProfileMedia || query.MediaLinkTerm(q) != "" {
			profile, err = search(q)
		} else {
			profile, err = cache.UserMediaTimeline(ctx, userID, name, q, after)
		}
	case types.QueryArticles:
		if flags.GuestAuth {
			profile, err = search(types.Query{Kind: types.QueryPosts, FromUser: q.FromUser,
				Text: "url:x.com/i/article/"})
		} else {
			profile, err = cache.UserTweets(ctx, userID, types.TimelineArticles, after, false)
		}
	case types.QueryReposts:
		profile, err = cache.UserTweets(ctx, userID, types.TimelineReposts, after, false)
	default:
		if emptyTweets {
			profile = types.Profile{Tweets: types.Timeline{Beginning: after == "", Query: q}}
		} else {
			profile, err = search(q)
		}
	}
	if gerr := g.wait(); err == nil && !profile.User.Protected {
		err = gerr
	}
	if err != nil {
		return types.Profile{}, err
	}
	user.Protected = profile.User.Protected
	profile.User = user
	if !user.Protected {
		profile.PhotoRail = rail
	}
	profile.AccountInfo = info
	profile.Tweets.Query = q
	return profile, nil
}

// showTimeline renders a profile, or a combined timeline for several
// users. It returns "" when the user doesn't exist.
func (c *ctx) showTimeline(q types.Query, rss, after string, emptyTweets bool) (string, error) {
	p := c.prefs()
	if len(q.FromUser) != 1 {
		var (
			tl  types.Timeline
			err error
		)
		if emptyTweets {
			tl = types.Timeline{Beginning: after == "", Query: q}
		} else {
			tl, err = searchTweets(c.r.Context(), q, after)
			if err != nil {
				return "", err
			}
		}
		html := views.RenderTweetSearch(tl, p, c.fullPath(), nil)
		return views.RenderMain(html, c.r, c.s.cfg, p, views.PageOpts{TitleText: "Multi", RSS: rss}), nil
	}

	profile, err := fetchProfile(c.r.Context(), after, q, p, false, false, emptyTweets)
	if err != nil {
		return "", err
	}
	u := profile.User
	if u.Suspended {
		return views.RenderMain(views.RenderError(formatters.Suspended(u.Username)), c.r, c.s.cfg, p,
			views.PageOpts{TitleText: "Error"}), nil
	}
	if u.ID == "" {
		return "", nil
	}
	html := views.RenderProfile(&profile, p, c.fullPath())
	return views.RenderMain(html, c.r, c.s.cfg, p, views.PageOpts{
		TitleText: formatters.UserPageTitle(u),
		Desc:      formatters.UserPageDesc(u),
		RSS:       rss,
		Images:    []string{formatters.UserPic(u.UserPic, "_400x400")},
	}), nil
}

func (c *ctx) respUserID() error {
	id := c.param("user_id")
	if id == "" {
		return errPass
	}
	username, err := cache.Username(c.r.Context(), id)
	if err != nil {
		return err
	}
	if username == "" {
		return c.fail(http.StatusNotFound, "User not found")
	}
	return c.redirect("/" + username)
}

func (s *Server) timelineRoutes() {
	s.get("/i/user/@user_id", func(c *ctx) error { return c.respUserID() })
	s.get("/intent/user", func(c *ctx) error { return c.respUserID() })
	s.get("/intent/follow/?", func(c *ctx) error {
		username := c.query.Get("screen_name")
		if username == "" {
			return c.fail(http.StatusBadRequest, "Missing screen_name parameter")
		}
		return c.redirect("/" + username)
	})

	s.get("/@name/about/?", func(c *ctx) error {
		name := c.param("name")
		if !allIn(name, isUsernameChar) {
			return errPass
		}
		info, err := cache.AccountInfo(c.r.Context(), name, true)
		if err != nil {
			return err
		}
		if info.Suspended {
			return c.fail(http.StatusOK, formatters.Suspended(name))
		}
		if info.Username == "" {
			return c.fail(http.StatusNotFound, `User "`+name+`" not found`)
		}
		return c.page(views.RenderAboutAccount(info, c.prefs()), views.PageOpts{TitleText: "About @" + info.Username})
	})

	s.get("/@name/@kind/?", func(c *ctx) error {
		name, kind := c.param("name"), c.param("kind")
		if kind != "followers" && kind != "following" || !allIn(name, isUsernameChar) {
			return errPass
		}
		ctx := c.r.Context()
		userID, err := cache.UserID(ctx, name)
		if err != nil {
			return err
		}
		if userID == "" {
			return c.fail(http.StatusNotFound, `User "`+name+`" not found`)
		}
		if userID == "suspended" {
			return c.fail(http.StatusOK, formatters.Suspended(name))
		}
		isFollowers := kind == "followers"
		user, err := cache.User(ctx, name, true)
		if err != nil {
			return err
		}
		var results types.Result[types.User]
		if isFollowers {
			results, err = api.GraphFollowers(ctx, userID, c.cursor())
		} else {
			results, err = api.GraphFollowing(ctx, userID, c.cursor())
		}
		if err != nil {
			return err
		}
		if user.Protected {
			return c.page(views.RenderProtected(user.Username),
				views.PageOpts{TitleText: "Protected account @" + name})
		}
		tab, title := "Following", "People followed by @"+name
		if isFollowers {
			tab, title = "Followers", "People following @"+name
		}
		html := views.RenderUserList(user, results, c.prefs(), c.r.URL.EscapedPath(), tab)
		return c.page(html, views.PageOpts{TitleText: title,
			Images: []string{formatters.UserPic(user.UserPic, "_400x400")}})
	})

	s.get("/@name/?@tab?/?", func(c *ctx) error {
		name, tab := c.param("name"), c.param("tab")
		if strings.Contains(name, ".") ||
			slices.Contains([]string{"pic", "gif", "video", "search", "settings", "login", "intent", "i"}, name) ||
			!allIn(name, func(b byte) bool { return isUsernameChar(b) || b == ',' }) ||
			!slices.Contains([]string{"with_replies", "media", "search", "articles", "reposts", ""}, tab) ||
			// articles and reposts can't be approximated by search, so
			// multi-user timelines don't support them
			(tab == "articles" || tab == "reposts") && strings.Contains(name, ",") {
			return errPass
		}
		p := c.prefs()
		after := c.cursor()
		users := names(name)
		q := c.timelineQuery(tab, name, c.mediaView(p))
		if len(users) != 1 {
			q.FromUser = users
		}

		// used by infinite scroll
		if c.query.Get("scroll") != "" {
			if len(q.FromUser) != 1 {
				tl, err := searchTweets(c.r.Context(), q, after)
				if err != nil {
					return err
				}
				if len(tl.Content) == 0 {
					if tl.Bottom == "" {
						return c.status(http.StatusNoContent)
					}
					return c.html(http.StatusOK, string(views.RenderMore(tl.Query, tl.Bottom, "", "")))
				}
				tl.Beginning = true
				return c.html(http.StatusOK, string(views.RenderTweetSearch(tl, p, c.fullPath(), nil)))
			}
			profile, err := fetchProfile(c.r.Context(), after, q, p, true, false, false)
			if err != nil {
				return err
			}
			if len(profile.Tweets.Content) == 0 {
				if profile.Tweets.Bottom == "" {
					return c.status(http.StatusNoContent)
				}
				return c.html(http.StatusOK, string(views.RenderMore(profile.Tweets.Query, profile.Tweets.Bottom, "", "")))
			}
			profile.Tweets.Beginning = true
			return c.html(http.StatusOK, string(views.RenderTimelineTweets(profile.Tweets, p, c.fullPath(), nil)))
		}

		var rss string
		switch {
		case !tabRssEnabled(s.cfg, tab):
		case tab == "":
			rss = "/" + name + "/rss"
		case tab == "search":
			rss = "/" + name + "/search/rss?" + query.URL(q)
		default:
			rss = "/" + name + "/" + tab + "/rss"
		}

		emptyTweets := tab == "search" && after == "" && !query.HasSearchInput(q)
		html, err := c.showTimeline(q, rss, after, emptyTweets)
		if err != nil {
			return err
		}
		if html == "" {
			return c.fail(http.StatusNotFound, `User "`+name+`" not found`)
		}
		return c.html(http.StatusOK, html)
	})
}
