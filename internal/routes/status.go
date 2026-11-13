// SPDX-License-Identifier: AGPL-3.0-only
package routes

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/perennialtech/nitter/internal/api"
	"github.com/perennialtech/nitter/internal/flags"
	"github.com/perennialtech/nitter/internal/formatters"
	"github.com/perennialtech/nitter/internal/types"
	"github.com/perennialtech/nitter/internal/utils"
	"github.com/perennialtech/nitter/internal/views"
)

// noteBackPath returns where to go after translating a note: back if it's
// a tweet or note page, and the note's own page otherwise.
func noteBackPath(back, noteID string) string {
	p := strings.Split(back, "/")
	if len(p) == 4 && p[0] == "" && p[1] == "i" && p[2] == "status" && isTweetID(p[3]) {
		return back
	}
	if len(p) == 5 && p[0] == "" && p[1] == "i" && p[2] == "birdwatch" &&
		(p[3] == "t" || p[3] == "n") && isTweetID(p[4]) {
		return back
	}
	return "/i/birdwatch/n/" + noteID
}

func rankingMode(s string) types.RankingMode {
	switch strings.ToLower(s) {
	case "recency":
		return types.RankingRecency
	case "likes":
		return types.RankingLikes
	}
	return types.RankingRelevance
}

func (s *Server) statusRoutes() {
	s.get("/@name/status/@id/?", func(c *ctx) error {
		if strings.Contains(c.param("name"), ".") {
			return errPass
		}
		id := c.param("id")
		if !isTweetID(id) {
			return c.fail(http.StatusNotFound, "Invalid tweet ID")
		}
		ctx, p := c.r.Context(), c.prefs()
		sort := rankingMode(c.param("sort"))

		// used by infinite scroll
		if c.query.Get("scroll") != "" {
			replies, err := api.Replies(ctx, id, c.cursor(), sort)
			if err != nil {
				return err
			}
			if len(replies.Content) == 0 {
				return c.status(http.StatusNoContent)
			}
			return c.html(http.StatusOK, string(views.RenderReplies(replies, p, c.fullPath(), nil, sort)))
		}

		conv, err := api.Tweet(ctx, id, c.cursor(), sort)
		if err != nil {
			return err
		}
		if conv == nil || conv.Tweet == nil || conv.Tweet.ID == 0 {
			msg := "Tweet not found"
			if conv != nil && conv.Tweet != nil && conv.Tweet.Tombstone != "" {
				msg = conv.Tweet.Tombstone
			}
			return c.fail(http.StatusNotFound, msg)
		}
		t := conv.Tweet
		switch tr := c.param("tr"); {
		case tr == "1" || p.AutoTranslate:
			api.TranslateTweet(ctx, t)
		case tr == "0":
			api.ApplyOnDemandCache(t)
		}

		var images []string
		for _, ph := range t.Photos() {
			images = append(images, ph.URL)
		}
		var video string
		switch {
		case len(t.Media) > 0 && t.Media[0].Kind == types.VideoMedia:
			images = []string{t.Media[0].Thumb()}
			video = formatters.VideoEmbed(s.cfg, t.ID)
		case len(t.Media) > 0 && t.Media[0].Kind == types.GifMedia:
			images = []string{t.Media[0].Thumb()}
			video = utils.PicURL(t.Media[0].Gif.URL)
		case t.Card != nil && t.Card.Image != "":
			images = []string{t.Card.Image}
		case t.Card != nil && t.Card.Video != nil:
			images = []string{t.Card.Video.Thumb}
		}

		tweetURL := s.cfg.URLPrefix() + "/" + t.User.Username + "/status/" + strconv.FormatInt(t.ID, 10)
		html := views.RenderConversation(*conv, p, c.fullPath()+"#m", sort)
		return c.page(html, views.PageOpts{
			TitleText: formatters.TweetPageTitle(t),
			Desc:      t.Text,
			OgTitle:   formatters.UserPageTitle(t.User),
			Images:    images,
			Video:     video,
			Oembed:    s.cfg.URLPrefix() + "/api/oembed?url=" + utils.EncodeURL(tweetURL),
		})
	})

	s.get("/@name/status/@id/history/?", func(c *ctx) error {
		if strings.Contains(c.param("name"), ".") {
			return errPass
		}
		id := c.param("id")
		if !isTweetID(id) {
			return c.fail(http.StatusNotFound, "Invalid tweet ID")
		}
		edits, err := api.GraphEditHistory(c.r.Context(), id)
		if err != nil {
			return err
		}
		if edits.Latest == nil || edits.Latest.ID == 0 {
			return c.fail(http.StatusNotFound, "Tweet history not found")
		}
		return c.page(views.RenderEditHistory(edits, c.prefs(), c.fullPath()), views.PageOpts{
			TitleText: "History for " + formatters.TweetPageTitle(edits.Latest),
			Desc:      edits.Latest.Text,
			OgTitle:   "Edit History for " + formatters.UserPageTitle(edits.Latest.User),
		})
	})

	s.post("/@name/status/@id/translate/?", func(c *ctx) error {
		if strings.Contains(c.param("name"), ".") || !flags.OnDemandTranslation {
			return errPass
		}
		id := c.param("id")
		if !isTweetID(id) {
			return c.fail(http.StatusNotFound, "Invalid tweet ID")
		}
		if c.param("sig") != utils.TranslationSignature(id, false) {
			return c.fail(http.StatusForbidden, "Invalid translation request")
		}
		ok, err := api.TranslateOnDemand(c.r.Context(), id, false)
		if err != nil || !ok {
			return c.fail(http.StatusBadGateway, "Translation request failed")
		}
		return c.redirect("/" + c.param("name") + "/status/" + id + "?tr=1#m")
	})

	s.get("/@name/status/@id/@tab/?", func(c *ctx) error {
		name, id, tab := c.param("name"), c.param("id"), c.param("tab")
		if strings.Contains(name, ".") || tab != "retweets" && tab != "quotes" {
			return errPass
		}
		if !isTweetID(id) {
			return c.fail(http.StatusNotFound, "Invalid tweet ID")
		}
		ctx, p := c.r.Context(), c.prefs()
		scroll := c.query.Get("scroll") != ""
		if tab == "retweets" {
			opts := views.PageOpts{TitleText: "Retweets", Desc: "Retweets of this post", OgTitle: "Retweets"}
			if flags.GuestAuth {
				if scroll {
					return c.status(http.StatusNoContent)
				}
				return c.page(views.RenderRetweetersUnavailable(name, id), opts)
			}
			results, err := api.GraphRetweeters(ctx, id, c.cursor())
			if err != nil {
				return err
			}
			if scroll {
				if len(results.Content) == 0 {
					return c.status(http.StatusNoContent)
				}
				return c.html(http.StatusOK, string(views.RenderTimelineUsers(results, p, "")))
			}
			return c.page(views.RenderRetweeters(results, p, name, id), opts)
		}
		q := types.Query{Kind: types.QueryPosts, Text: "-filter:retweets quoted_tweet_id:" + id}
		results, err := api.GraphTweetSearch(ctx, q, c.cursor())
		if err != nil {
			return err
		}
		if scroll {
			if len(results.Content) == 0 {
				return c.status(http.StatusNoContent)
			}
			return c.html(http.StatusOK, string(views.RenderTimelineTweets(results, p, c.fullPath(), nil)))
		}
		return c.page(views.RenderQuotes(results, p, c.fullPath(), name, id), views.PageOpts{
			TitleText: "Quoted Tweets", Desc: "Tweets quoting this post", OgTitle: "Quoted Tweets",
		})
	})

	s.get("/i/birdwatch/t/@id", func(c *ctx) error {
		id := c.param("id")
		if !isTweetID(id) {
			return c.fail(http.StatusNotFound, "Invalid tweet ID")
		}
		ctx, p := c.r.Context(), c.prefs()
		conv, err := api.Tweet(ctx, id, "", types.RankingRelevance)
		if err != nil {
			return err
		}
		var tweet *types.Tweet
		if conv != nil {
			tweet = conv.Tweet
		}
		translate := c.param("tr") == "1"
		notes, err := api.GraphBirdwatchNotes(ctx, id)
		if err != nil {
			return err
		}
		for i := range notes.Notes {
			api.ApplyOnDemandNoteCache(&notes.Notes[i])
		}
		if (translate || p.AutoTranslate) && tweet != nil {
			api.TranslateTweet(ctx, tweet)
		}
		return c.page(views.RenderBirdwatchNotes(tweet, notes, p, c.fullPath(), translate), views.PageOpts{
			TitleText: "Community Notes", Desc: "Community notes for this post", OgTitle: "Community Notes",
		})
	})

	communityNotesRss := func(path string) string {
		if s.cfg.EnableRSSCommunityNotes && !s.cfg.GuestAuth {
			return path
		}
		return ""
	}
	s.get("/i/communitynotes/?", func(c *ctx) error {
		tl, err := api.GraphBirdwatchGlobalTimeline(c.r.Context())
		if err != nil {
			return err
		}
		return c.page(views.RenderCommunityNotesTimeline(tl, c.prefs(), c.fullPath(), "helpful"), views.PageOpts{
			TitleText: "Community Notes - Rated Helpful",
			Desc:      "Posts with community notes rated helpful by contributors",
			OgTitle:   "Community Notes",
			RSS:       communityNotesRss("/i/communitynotes/rss"),
		})
	})
	s.get("/i/communitynotes/new/?", func(c *ctx) error {
		tl, err := api.GraphCommunityNotesNew(c.r.Context())
		if err != nil {
			return err
		}
		return c.page(views.RenderCommunityNotesTimeline(tl, c.prefs(), c.fullPath(), "new"), views.PageOpts{
			TitleText: "Community Notes - New",
			Desc:      "The newest community notes",
			OgTitle:   "Community Notes",
			RSS:       communityNotesRss("/i/communitynotes/new/rss"),
		})
	})

	s.get("/i/communitynotes/u/@alias", func(c *ctx) error {
		alias := c.param("alias")
		if alias == "" {
			return c.fail(http.StatusNotFound, "Invalid contributor alias")
		}
		history, err := api.GraphBirdwatchHistory(c.r.Context(), alias, c.cursor())
		if err != nil {
			return err
		}
		if c.query.Get("scroll") != "" {
			if len(history.Notes) == 0 {
				return c.status(http.StatusNoContent)
			}
			return c.html(http.StatusOK, string(views.RenderBirdwatchHistoryContent(history)))
		}
		return c.page(views.RenderBirdwatchHistory(history), views.PageOpts{
			TitleText: "Notes by " + alias, Desc: "Community notes history for " + alias,
			OgTitle: "Notes by " + alias,
		})
	})

	s.get("/i/birdwatch/n/@id", func(c *ctx) error {
		id := c.param("id")
		if !isTweetID(id) {
			return c.fail(http.StatusNotFound, "Invalid note ID")
		}
		ctx, p := c.r.Context(), c.prefs()
		single, err := api.GraphBirdwatchOneNote(ctx, id)
		if err != nil {
			return err
		}
		api.ApplyOnDemandNoteCache(&single.Note)
		tweet, err := api.TweetByRestID(ctx, strconv.FormatInt(single.TweetID, 10))
		if err != nil {
			return err
		}
		translate := c.param("tr") == "1"
		if (translate || p.AutoTranslate) && tweet != nil {
			api.TranslateTweet(ctx, tweet)
		}
		return c.page(views.RenderBirdwatchSingleNote(tweet, single.Note, p, c.fullPath(), translate),
			views.PageOpts{TitleText: "Community Note", Desc: "Community note", OgTitle: "Community Note"})
	})

	s.post("/i/birdwatch/n/@id/translate/?", func(c *ctx) error {
		if !flags.OnDemandTranslation {
			return errPass
		}
		id := c.param("id")
		if !isTweetID(id) {
			return c.fail(http.StatusNotFound, "Invalid note ID")
		}
		if c.param("sig") != utils.TranslationSignature(id, true) {
			return c.fail(http.StatusForbidden, "Invalid translation request")
		}
		ok, err := api.TranslateOnDemand(c.r.Context(), id, true)
		if err != nil || !ok {
			return c.fail(http.StatusBadGateway, "Translation request failed")
		}
		return c.redirect(noteBackPath(c.param("r"), id) + "?tr=1#m")
	})

	s.get("/@name/@s/@id/@m/?@i?", func(c *ctx) error {
		if s, m := c.param("s"), c.param("m"); s != "status" && s != "statuses" || m != "video" && m != "photo" {
			return errPass
		}
		return c.redirect("/" + c.param("name") + "/status/" + c.param("id"))
	})
	s.get("/@name/statuses/@id/?", func(c *ctx) error {
		return c.redirect("/" + c.param("name") + "/status/" + c.param("id"))
	})
	s.get("/i/web/status/@id", func(c *ctx) error { return c.redirect("/i/status/" + c.param("id")) })
	s.get("/@name/thread/@id/?", func(c *ctx) error {
		return c.redirect("/" + c.param("name") + "/status/" + c.param("id"))
	})
}
