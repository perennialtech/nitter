// SPDX-License-Identifier: AGPL-3.0-only

package views

import (
	"html/template"
	"strings"

	"github.com/perennialtech/nitter/internal/types"
	"github.com/perennialtech/nitter/internal/utils"
)

// RenderListTabs renders a list's Tweets/Members tabs.
func RenderListTabs(q types.Query, path string) template.HTML {
	return exec("tabs", map[string]any{"Class": "tab", "Tabs": []tab{
		{tabClass(q, types.QueryPosts), path, "Tweets"},
		{tabClass(q, types.QueryUserList), path + "/members", "Members"},
	}})
}

// RenderList renders a list page around body.
func RenderList(body template.HTML, q types.Query, l types.List) template.HTML {
	return exec("list", map[string]any{
		"L":         l,
		"BannerURL": utils.PicURL(l.Banner),
		"Header":    "\"" + l.Name + "\" by @" + l.Username,
		"Tabs":      RenderListTabs(q, "/i/lists/"+l.ID),
		"Body":      body,
	})
}

func communityPath(c types.Community) string { return "/i/communities/" + c.ID }

// RenderCommunityTabs renders a community's tabs and hashtags. kind
// selects the active tab: posts (Top), replies (Latest), media or
// userList (About).
func RenderCommunityTabs(kind types.QueryKind, c types.Community) template.HTML {
	path := communityPath(c)
	q := types.Query{Kind: kind}
	var tags []map[string]string
	for _, tag := range c.Hashtags {
		tags = append(tags, map[string]string{
			"Tag": tag, "Href": path + "/hashtag/" + strings.Trim(tag, "#"),
		})
	}
	return exec("communityTabs", map[string]any{
		"Tabs": exec("tabs", map[string]any{"Class": "tab", "Tabs": []tab{
			{tabClass(q, types.QueryPosts), path, "Top"},
			{tabClass(q, types.QueryReplies), path + "/latest", "Latest"},
			{tabClass(q, types.QueryMedia), path + "/media", "Media"},
			{tabClass(q, types.QueryUserList), path + "/about", "About"},
		}}),
		"Tags": tags,
	})
}

// RenderMemberTabs renders a community's All/Moderators member tabs.
func RenderMemberTabs(c types.Community, isModerators bool) template.HTML {
	path := communityPath(c)
	return exec("tabs", map[string]any{"Class": "tab", "Tabs": []tab{
		{activeTab(!isModerators), path + "/members", "All"},
		{activeTab(isModerators), path + "/moderators", "Moderators"},
	}})
}

// RenderHashtagHeader renders the heading of a community hashtag page.
func RenderHashtagHeader(c types.Community, tag string) template.HTML {
	return exec("hashtagHeader", "#"+tag)
}

// RenderCommunityAbout renders a community's about tab.
func RenderCommunityAbout(c types.Community, moderators []types.User) template.HTML {
	return exec("communityAbout", map[string]any{
		"C":          c,
		"Path":       communityPath(c),
		"Created":    "Created " + c.CreatedAt.Format("January 2, 2006") + " by ",
		"Moderators": moderators,
	})
}

// RenderCommunity renders a community page around nav and body.
func RenderCommunity(body, nav template.HTML, c types.Community) template.HTML {
	return exec("community", map[string]any{
		"C":         c,
		"Path":      communityPath(c),
		"BannerURL": utils.PicURL(c.Banner),
		"Members":   insertSep(c.MemberCount),
		"Nav":       nav,
		"Body":      body,
	})
}
