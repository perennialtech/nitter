// SPDX-License-Identifier: AGPL-3.0-only

package views

import (
	"html/template"
	"net/url"
	"time"

	"github.com/perennialtech/nitter/internal/formatters"
	"github.com/perennialtech/nitter/internal/prefs"
	"github.com/perennialtech/nitter/internal/types"
)

type TrendsTab struct {
	Label, URL string
	Active     bool
}

type TrendsPage struct {
	Title, Cursor string
	Tabs, Sort    []TrendsTab
	Trends        []types.Trend
	Stories       *types.StoryPage
	Params        url.Values
}

func storyTime(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return formatters.Time(time.UnixMilli(ms).UTC())
}

func RenderTrendsPage(page TrendsPage) template.HTML {
	var results template.HTML
	if page.Stories == nil {
		results = renderTrendsList(page.Trends)
	} else {
		list := resultsData{}
		empty := len(page.Stories.Stories) == 0
		for _, story := range page.Stories.Stories {
			list.Items = append(list.Items, exec("storyItem", map[string]any{
				"Story": story,
				"Count": insertSep(int(story.PostCount)),
				"Date":  storyTime(story.CreatedMs),
			}))
		}
		if !empty {
			list.ToTop = RenderToTop("#")
		}
		if page.Cursor != "" {
			href := "?"
			if params := page.Params.Encode(); params != "" {
				href += params
			}
			list.Newer = exec("newer", href)
		}
		if !empty && page.Stories.Bottom != "" {
			params := url.Values{"cursor": {page.Stories.Bottom}}
			for k, v := range page.Params {
				params[k] = v
			}
			list.More = exec("more", "?"+params.Encode())
		}
		if empty {
			if page.Cursor == "" {
				list.Footer = exec("emptyStories", nil)
			} else {
				list.Footer = exec("noMore", nil)
			}
		}
		results = exec("timelineResults", list)
	}
	return exec("trendsPage", map[string]any{"Page": page, "Results": results})
}

func RenderStory(detail types.StoryDetail, active int, posts types.Timeline, p *prefs.Prefs, path string) template.HTML {
	var tabs []TrendsTab
	var extra string
	for i, tab := range detail.Timelines {
		link := path
		if i != 0 {
			params := url.Values{"timeline": {tab.ID}}.Encode()
			link += "?" + params
			if active == i {
				extra = params + "&"
			}
		}
		tabs = append(tabs, TrendsTab{Label: tab.Label, URL: link, Active: active == i})
	}
	return exec("storyDetail", map[string]any{
		"Detail": detail, "Updated": storyTime(detail.UpdatedMs), "Tabs": tabs,
		"Posts": renderTimelineTweets(posts, p, path, nil, extra),
	})
}
