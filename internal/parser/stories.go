// SPDX-License-Identifier: AGPL-3.0-only

package parser

import (
	"strconv"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/perennialtech/nitter/internal/types"
	"github.com/perennialtech/nitter/internal/utils"
)

func storyNumber(n gjson.Result) int64 {
	var raw string
	switch n.Type {
	case gjson.Number:
		raw = n.Raw
	case gjson.String:
		raw = strings.ReplaceAll(n.Str, ",", "")
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return max(value, 0)
}

func ParseStoryPage(js gjson.Result, cursor string) (types.StoryPage, bool) {
	stories := get(js, "data.story_topic.stories")
	items := get(stories, "items")
	if !items.IsArray() {
		return types.StoryPage{}, false
	}
	var page types.StoryPage
	for _, item := range arr(items) {
		r := get(item, "trend_results.result")
		id := str(get(r, "rest_id"))
		if len(id) > 19 || utils.ValidateNumber(id) == "" {
			continue
		}
		core := get(r, "core")
		page.Stories = append(page.Stories, types.Story{
			ID:        id,
			Title:     str(get(core, "name")),
			Category:  str(get(core, "category")),
			Hook:      str(get(core, "hook")),
			PostCount: storyNumber(get(r, "post_count")),
			CreatedMs: storyNumber(get(core, "created_at_ms")),
		})
	}
	for _, path := range []string{"slice_info.next_cursor", "page_info.end_cursor", "next_cursor"} {
		if next := str(get(stories, path)); next != "" {
			if next != cursor {
				page.Bottom = next
			}
			break
		}
	}
	return page, true
}

func ParseStoryDetail(js gjson.Result) types.StoryDetail {
	page := get(js, "data.ai_trend_by_rest_id.result.page")
	detail := types.StoryDetail{
		Title:      str(get(page, "article.title")),
		Summary:    str(get(page, "article.article_text.text")),
		Disclaimer: str(get(page, "disclaimer")),
		UpdatedMs:  storyNumber(get(page, "last_updated_at_ms")),
	}
	for _, tab := range arr(get(page, "post_timelines")) {
		id := str(get(tab, "post_timeline.id"))
		label := str(get(tab, "label"))
		if id != "" && label != "" {
			detail.Timelines = append(detail.Timelines, types.StoryTimeline{ID: id, Label: label})
		}
	}
	return detail
}

func ParseStoryPosts(js gjson.Result, cursor string) types.Timeline {
	tl := types.Timeline{Beginning: cursor == ""}
	for _, i := range arr(get(js, "data.timeline.timeline.instructions")) {
		for _, e := range arr(get(i, "entries")) {
			switch id := entryID(e); {
			case strings.HasPrefix(id, "tweet"):
				tl.Content = addSingles(tl.Content, extractTweetsFromEntry(e))
			case strings.HasPrefix(id, "cursor-bottom"):
				tl.Bottom = str(get(e, "content.value"))
			}
		}
		if typeName(i) == "TimelineReplaceEntry" && strings.HasPrefix(str(get(i, "entry_id_to_replace")), "cursor-bottom") {
			tl.Bottom = str(get(i, "entry.content.value"))
		}
	}
	if tl.Bottom == cursor {
		tl.Bottom = ""
	}
	return tl
}
