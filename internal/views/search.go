// SPDX-License-Identifier: AGPL-3.0-only

package views

import (
	"html/template"
	"slices"
	"strings"
	"unicode"

	"github.com/perennialtech/nitter/internal/flags"
	"github.com/perennialtech/nitter/internal/prefs"
	"github.com/perennialtech/nitter/internal/query"
	"github.com/perennialtech/nitter/internal/types"
)

var toggles = []struct{ Key, Label string }{
	{"nativeretweets", "Retweets"},
	{"media", "Media"},
	{"videos", "Videos"},
	{"news", "News"},
	{"native_video", "Native videos"},
	{"replies", "Replies"},
	{"links", "Links"},
	{"images", "Images"},
	{"quote", "Quotes"},
	{"spaces", "Spaces"},
}

// RenderSearch renders the home page search bar.
func RenderSearch() template.HTML {
	return exec("search", nil)
}

type tab struct{ Class, Href, Text string }

// RenderProfileTabs renders the tabs above a profile timeline.
func RenderProfileTabs(q types.Query, username string) template.HTML {
	link := "/" + username
	tabs := []tab{{tabClass(q, types.QueryPosts), link, "Tweets"}}
	if flags.GuestAuth {
		tabs = append(tabs, tab{tabClass(q, types.QueryReplies) + " wide", link + "/with_replies", "Tweets & Replies"})
	} else {
		tabs = append(tabs, tab{tabClass(q, types.QueryReplies), link + "/with_replies", "Replies"})
		tabs = append(tabs, tab{tabClass(q, types.QueryReposts), link + "/reposts", "Retweets"})
	}
	tabs = append(tabs, tab{tabClass(q, types.QueryMedia), link + "/media", "Media"})
	if len(q.FromUser) == 1 {
		tabs = append(tabs, tab{tabClass(q, types.QueryArticles), link + "/articles", "Articles"})
	}
	tabs = append(tabs, tab{tabClass(q, types.QueryTweets), link + "/search", "Search"})
	return exec("tabs", map[string]any{"Class": "tab", "Tabs": tabs})
}

func mediaViewURL(q types.Query, view string) string {
	q.View = view
	return "?" + query.URL(q)
}

// RenderMediaViewTabs renders the timeline/grid/gallery switch.
func RenderMediaViewTabs(q types.Query) template.HTML {
	current := q.View
	if current == "" {
		current = "timeline"
	}
	var tabs []tab
	for _, v := range []struct{ View, Text string }{
		{"timeline", "Timeline"}, {"grid", "Grid"}, {"gallery", "Gallery"},
	} {
		tabs = append(tabs, tab{activeTab(current == v.View), mediaViewURL(q, v.View), v.Text})
	}
	return exec("tabs", map[string]any{"Class": "tab media-view-tabs", "Tabs": tabs})
}

// RenderSearchTabs renders the Top/Latest/Media/Users/Lists tabs.
func RenderSearchTabs(q types.Query) template.HTML {
	link := func(kind types.QueryKind, view string) string {
		t := q
		t.Kind = kind
		t.View = view
		return "?" + query.URL(t)
	}
	var tabs []tab
	if !flags.GuestAuth {
		tabs = append(tabs, tab{tabClass(q, types.QueryTop), link(types.QueryTop, ""), "Top"})
	}
	tabs = append(tabs,
		tab{tabClass(q, types.QueryTweets), link(types.QueryTweets, ""), "Latest"},
		// the media view mode only applies to the Media tab
		tab{tabClass(q, types.QueryMedia), link(types.QueryMedia, q.View), "Media"},
		tab{tabClass(q, types.QueryUsers), link(types.QueryUsers, ""), "Users"},
		tab{tabClass(q, types.QueryLists), link(types.QueryLists, ""), "Lists"},
	)
	return exec("tabs", map[string]any{"Class": "tab", "Tabs": tabs})
}

func isPanelOpen(q types.Query) bool {
	return len(q.FromUser) == 0 && (len(q.Filters) > 0 || len(q.Excludes) > 0 ||
		q.MinLikes != "" || q.Until != "" || q.Since != "")
}

type toggleData struct {
	Name, Label string
	State       bool
}

// RenderSearchPanel renders the search form with its filter panel.
func RenderSearchPanel(q types.Query) template.HTML {
	user := strings.Join(q.FromUser, ",")
	action := "/search"
	if user != "" {
		action = "/" + user + "/search"
	}
	var sections []map[string]any
	for _, f := range []string{"filter", "exclude"} {
		var ts []toggleData
		for _, t := range toggles {
			state := slices.Contains(q.Excludes, t.Key)
			if f == "filter" {
				state = slices.Contains(q.Filters, t.Key)
			}
			ts = append(ts, toggleData{f[:1] + "-" + t.Key, t.Label, state})
		}
		sections = append(sections, map[string]any{"Title": capitalize(f), "Toggles": ts})
	}
	return exec("searchPanel", map[string]any{
		"Action": action, "Kind": string(kindOf(q)), "Q": q, "Open": isPanelOpen(q),
		"Sections": sections,
	})
}

// RenderTweetSearch renders a tweet search or profile timeline with its
// tabs and search panel. pinned may be nil.
func RenderTweetSearch(results types.Timeline, p *prefs.Prefs, path string, pinned *types.Tweet) template.HTML {
	q := results.Query
	containerClass := "timeline-container"
	if len(q.FromUser) == 0 && q.Kind == types.QueryMedia && q.View == "gallery" {
		containerClass = "timeline-container media-only"
	}
	var parts []template.HTML
	if len(q.FromUser) > 1 {
		parts = append(parts, exec("timelineHeader", strings.Join(q.FromUser, " | ")))
	}
	if len(q.FromUser) > 0 {
		if q.Kind != types.QueryMedia || q.View != "gallery" {
			parts = append(parts, RenderProfileTabs(q, strings.Join(q.FromUser, ",")))
		}
		if q.Kind == types.QueryMedia && len(q.FromUser) == 1 {
			parts = append(parts, RenderMediaViewTabs(q))
		}
	}
	if len(q.FromUser) == 0 || q.Kind == types.QueryTweets {
		parts = append(parts, exec("timelineHeader", RenderSearchPanel(q)))
	}
	if len(q.FromUser) == 0 {
		parts = append(parts, RenderSearchTabs(q))
		if q.Kind == types.QueryMedia {
			parts = append(parts, RenderMediaViewTabs(q))
		}
	}
	parts = append(parts, RenderTimelineTweets(results, p, path, pinned))
	return exec("container", map[string]any{"Class": containerClass, "Parts": parts})
}

func renderSearchForm(kind, placeholder, value string) template.HTML {
	return exec("searchForm", map[string]string{"Kind": kind, "Placeholder": placeholder, "Value": value})
}

// RenderUserSearch renders user search results.
func RenderUserSearch(results types.UsersTimeline, p *prefs.Prefs) template.HTML {
	return exec("container", map[string]any{"Class": "timeline-container", "Parts": []template.HTML{
		exec("timelineHeader", renderSearchForm("users", "Enter username...", results.Query.Text)),
		RenderSearchTabs(results.Query),
		RenderTimelineUsers(results, p, ""),
	}})
}

// RenderListSearch renders list search results.
func RenderListSearch(results types.Result[types.ListSearchResult], p *prefs.Prefs, path string) template.HTML {
	return exec("container", map[string]any{"Class": "timeline-container", "Parts": []template.HTML{
		exec("timelineHeader", renderSearchForm("lists", "Enter search...", results.Query.Text)),
		RenderSearchTabs(results.Query),
		RenderTimelineLists(results, p, path),
	}})
}

func renderReactorsTabs(name, id, active string) template.HTML {
	base := "/" + name + "/status/" + id
	return exec("tabs", map[string]any{"Class": "tab", "Tabs": []tab{
		{activeTab(active == "quotes"), base + "/quotes", "Quoted Tweets"},
		{activeTab(active == "retweets"), base + "/retweets", "Retweets"},
	}})
}

// RenderRetweeters renders the users who retweeted a tweet.
func RenderRetweeters(results types.UsersTimeline, p *prefs.Prefs, name, id string) template.HTML {
	return exec("container", map[string]any{"Class": "timeline-container", "Parts": []template.HTML{
		renderReactorsTabs(name, id, "retweets"),
		RenderTimelineUsers(results, p, ""),
	}})
}

// RenderRetweetersUnavailable renders the retweeters tab in guest mode.
func RenderRetweetersUnavailable(name, id string) template.HTML {
	return exec("container", map[string]any{"Class": "timeline-container", "Parts": []template.HTML{
		renderReactorsTabs(name, id, "retweets"),
		exec("timelineNone", "Retweets aren't available in guest mode."),
	}})
}

// RenderQuotes renders the tweets quoting a tweet.
func RenderQuotes(results types.Timeline, p *prefs.Prefs, path, name, id string) template.HTML {
	return exec("container", map[string]any{"Class": "timeline-container", "Parts": []template.HTML{
		renderReactorsTabs(name, id, "quotes"),
		RenderTimelineTweets(results, p, path, nil),
	}})
}

// capitalize is Nim's unicode.capitalize: the first rune in upper case.
func capitalize(s string) string {
	for i, r := range s {
		return string(unicode.ToUpper(r)) + s[i+len(string(r)):]
	}
	return s
}
