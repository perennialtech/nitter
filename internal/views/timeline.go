// SPDX-License-Identifier: AGPL-3.0-only

package views

import (
	"html/template"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/perennialtech/nitter/internal/formatters"
	"github.com/perennialtech/nitter/internal/prefs"
	"github.com/perennialtech/nitter/internal/query"
	"github.com/perennialtech/nitter/internal/types"
)

func timelineViewClass(q types.Query) string {
	if q.Kind != types.QueryMedia {
		return "timeline"
	}
	switch q.View {
	case "grid":
		return "timeline media-grid-view"
	case "gallery":
		return "timeline media-gallery-view"
	}
	return "timeline"
}

func getQuery(q types.Query) string {
	var result string
	if kindOf(q) != types.QueryPosts {
		result = query.URL(q)
	}
	if result != "" {
		result += "&"
	}
	return result
}

func getSearchMaxID(results types.Timeline) string {
	if results.Query.Kind != types.QueryTweets || len(results.Content) == 0 ||
		results.Query.Until == "" {
		return ""
	}
	lastThread := results.Content[len(results.Content)-1]
	if len(lastThread) == 0 || lastThread[len(lastThread)-1].ID == 0 {
		return ""
	}
	last := lastThread[len(lastThread)-1].ID
	// 2000000 is the minimum decrement to guarantee no result overlap
	maxID := last - 2_000_000
	if maxID <= 0 {
		maxID = last - 1
	}
	if maxID > 0 {
		return "maxid:" + strconv.FormatInt(maxID, 10)
	}
	return ""
}

// RenderToTop renders the scroll-to-top link; focus is its href ("#" in Nim).
func RenderToTop(focus string) template.HTML {
	return exec("toTop", focus)
}

// RenderNewer renders the "Load newest" link.
func RenderNewer(q types.Query, path, focus string) template.HTML {
	u := ""
	if s := query.URL(q); s != "" {
		u = "?" + s
	}
	if focus != "" {
		path = strings.ReplaceAll(path, "#m", focus)
	}
	return exec("newer", path+u)
}

// RenderMore renders the "Load more" link.
func RenderMore(q types.Query, cursor, focus, extra string) template.HTML {
	return exec("more", "?"+extra+getQuery(q)+"cursor="+encodeURLNoPlus(cursor)+focus)
}

func renderThread(thread types.Tweets, p *prefs.Prefs, path string, bigThumb bool) template.HTML {
	sorted := slices.Clone(thread)
	slices.SortStableFunc(sorted, func(a, b *types.Tweet) int {
		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		}
		return 0
	})
	var items []template.HTML
	for i, t := range sorted {
		// thread has a gap, display "more replies" link
		if i > 0 && t.ReplyID != sorted[i-1].ID {
			items = append(items, exec("moreRepliesThread", formatters.TweetLink(t, true)))
		}
		header := ""
		if t.Pinned || t.Retweet != nil {
			header = "with-header "
		}
		items = append(items, RenderTweet(t, p, path, TweetOpts{
			Class: header + "thread", Index: i, Last: i == len(thread)-1, BigThumb: bigThumb,
		}))
	}
	return exec("thread", items)
}

func renderUser(u types.User, p *prefs.Prefs) template.HTML {
	return exec("user", map[string]any{
		"U": u, "AvatarClass": avatarClass(p),
		"Bio": template.HTML(formatters.ReplaceURLs(u.Bio, p, "")),
	})
}

type resultsData struct {
	Newer, ToTop, More, Footer template.HTML
	Items                      []template.HTML
}

func timelineResults[T any](r types.Result[T], path string, render func(T) template.HTML) resultsData {
	var d resultsData
	if !r.Beginning {
		d.Newer = RenderNewer(r.Query, path, "")
	}
	switch {
	case len(r.Content) > 0:
		for _, it := range r.Content {
			d.Items = append(d.Items, render(it))
		}
		if r.Bottom != "" {
			d.More = RenderMore(r.Query, r.Bottom, "", "")
		}
		d.ToTop = RenderToTop("#")
	case r.Beginning:
		d.Footer = exec("noneFound", nil)
	default:
		d.Footer = exec("noMore", nil)
	}
	return d
}

// RenderTimelineUsers renders a list of users.
func RenderTimelineUsers(results types.UsersTimeline, p *prefs.Prefs, path string) template.HTML {
	return exec("timelineResults", timelineResults(results, path, func(u types.User) template.HTML {
		return renderUser(u, p)
	}))
}

// mentionUsername returns "user" for a well-formed "@user", and "" otherwise.
func mentionUsername(word string) string {
	if len(word) < 2 || word[0] != '@' {
		return ""
	}
	for _, c := range []byte(word[1:]) {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return ""
		}
	}
	return word[1:]
}

// mentionedUser returns the last @mention in strings like
// "65 followers including @user".
func mentionedUser(s string) string {
	words := strings.Split(s, " ")
	for i := len(words) - 1; i >= 0; i-- {
		if u := mentionUsername(words[i]); u != "" {
			return u
		}
	}
	return ""
}

type mentionWord struct{ Word, Username string }

func renderListCard(r types.ListSearchResult) template.HTML {
	d := map[string]any{
		"R":       r,
		"URL":     "/i/lists/" + r.List.ID,
		"Members": "· " + insertSep(r.List.Members) + " members",
	}
	if r.FollowersContext != "" {
		mentioned := mentionedUser(r.FollowersContext)
		var faces []map[string]string
		for i, f := range r.Facepiles {
			face := map[string]string{"Pic": f}
			if i == 0 && mentioned != "" {
				face["Href"] = "/" + mentioned
			}
			faces = append(faces, face)
		}
		var words []mentionWord
		for _, w := range strings.Split(r.FollowersContext, " ") {
			words = append(words, mentionWord{Word: w, Username: mentionUsername(w)})
		}
		d["Facepiles"] = faces
		d["Words"] = words
	} else {
		d["OwnerPic"] = formatters.UserPic(r.Owner.UserPic, "_mini")
	}
	return exec("listCard", d)
}

// RenderTimelineLists renders list search results.
func RenderTimelineLists(results types.Result[types.ListSearchResult], p *prefs.Prefs, path string) template.HTML {
	return exec("timelineResults", timelineResults(results, path, renderListCard))
}

func filterThreads(threads []types.Tweets, p *prefs.Prefs) []types.Tweets {
	var retweets []int64
	var result []types.Tweets
	for _, thread := range threads {
		if len(thread) == 1 {
			t := thread[0]
			var retweetID int64
			if t.Retweet != nil {
				retweetID = t.Retweet.ID
			}
			if slices.Contains(retweets, retweetID) || slices.Contains(retweets, t.ID) ||
				t.Pinned && p.HidePins {
				continue
			}
			if retweetID != 0 {
				retweets = append(retweets, retweetID)
			}
		}
		result = append(result, thread)
	}
	return result
}

// RenderTimelineTweets renders a timeline. pinned may be nil.
func RenderTimelineTweets(results types.Timeline, p *prefs.Prefs, path string, pinned *types.Tweet) template.HTML {
	d := map[string]any{"Class": timelineViewClass(results.Query)}
	if !results.Beginning {
		u, err := url.Parse(path)
		newerPath := path
		if err == nil {
			newerPath = u.EscapedPath()
		}
		d["Newer"] = RenderNewer(results.Query, newerPath, "")
	}
	if !p.HidePins && pinned != nil {
		d["Pinned"] = RenderTweet(pinned, p, path, TweetOpts{})
	}

	if len(results.Content) == 0 {
		if !results.Beginning {
			d["Footer"] = exec("noMore", nil)
		} else {
			d["Footer"] = exec("noneFound", nil)
		}
		return exec("timelineTweets", d)
	}

	filtered := filterThreads(results.Content, p)
	bigThumb := false
	if results.Query.View == "gallery" {
		bigThumb = p.GallerySize == "Large"
		gal := "gallery-masonry"
		if p.CompactGallery {
			gal = "gallery-masonry compact"
		}
		d["Gallery"] = gal
		d["ColSize"] = strings.ToLower(p.GallerySize)
	}
	var items []template.HTML
	for _, thread := range filtered {
		if len(thread) == 1 {
			items = append(items, RenderTweet(thread[0], p, path, TweetOpts{BigThumb: bigThumb}))
		} else {
			items = append(items, renderThread(thread, p, path, bigThumb))
		}
	}
	d["Items"] = items

	if cursor := getSearchMaxID(results); cursor != "" {
		d["More"] = RenderMore(results.Query, cursor, "", "")
	} else if results.Bottom != "" {
		d["More"] = RenderMore(results.Query, results.Bottom, "", "")
	}
	d["ToTop"] = RenderToTop("#")
	return exec("timelineTweets", d)
}
