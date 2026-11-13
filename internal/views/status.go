// SPDX-License-Identifier: AGPL-3.0-only

package views

import (
	"html/template"
	"slices"
	"strconv"

	"github.com/perennialtech/nitter/internal/flags"
	"github.com/perennialtech/nitter/internal/formatters"
	"github.com/perennialtech/nitter/internal/prefs"
	"github.com/perennialtech/nitter/internal/types"
	"github.com/perennialtech/nitter/internal/utils"
)

func renderMoreReplies(thread types.Chain) template.HTML {
	last := thread.Content[len(thread.Content)-1]
	link := ""
	if last.Available {
		link = formatters.TweetLink(last, true)
	}
	return exec("moreReplies", link)
}

func renderReplyThread(thread types.Chain, p *prefs.Prefs, path string) template.HTML {
	var items []template.HTML
	for i, t := range thread.Content {
		last := i == len(thread.Content)-1 && !thread.HasMore
		items = append(items, RenderTweet(t, p, path, TweetOpts{Index: i, Last: last}))
	}
	if thread.HasMore {
		items = append(items, renderMoreReplies(thread))
	}
	return exec("replyThread", items)
}

var rankingModes = []struct {
	Mode  types.RankingMode
	Label string
}{
	{types.RankingRelevance, "Relevant"},
	{types.RankingRecency, "Recent"},
	{types.RankingLikes, "Liked"},
}

func renderReplySort(sort types.RankingMode) template.HTML {
	var opts []map[string]string
	for _, m := range rankingModes {
		cls := "reply-sort-option"
		if m.Mode == sort {
			cls = "reply-sort-option active"
		}
		opts = append(opts, map[string]string{
			"Class": cls, "Href": "?sort=" + string(m.Mode) + "#r", "Label": m.Label,
		})
	}
	return exec("replySort", opts)
}

// RenderReplies renders a conversation's reply threads. tweet may be nil;
// sort is RankingRelevance by default.
func RenderReplies(replies types.Result[types.Chain], p *prefs.Prefs, path string,
	tweet *types.Tweet, sort types.RankingMode) template.HTML {
	if sort == "" {
		sort = types.RankingRelevance
	}
	var items []template.HTML
	hasReplies := false
	replyCount := 0
	for _, thread := range replies.Content {
		if len(thread.Content) == 0 || thread.Related {
			continue
		}
		hasReplies = true
		replyCount += len(thread.Content)
		items = append(items, renderReplyThread(thread, p, path))
	}
	if hasReplies && replies.Bottom != "" {
		if tweet == nil || !replies.Beginning || replyCount < tweet.Stats.Replies {
			extra := ""
			if sort != types.RankingRelevance {
				extra = "sort=" + string(sort) + "&"
			}
			items = append(items, RenderMore(types.Query{}, replies.Bottom, "#r", extra))
		}
	}
	return exec("replies", items)
}

func renderRelated(replies types.Result[types.Chain], p *prefs.Prefs, path string) template.HTML {
	var items []template.HTML
	for _, thread := range replies.Content {
		if len(thread.Content) == 0 || !thread.Related {
			continue
		}
		items = append(items, renderReplyThread(thread, p, path))
	}
	return exec("related", items)
}

// RenderConversation renders a tweet page: the thread around the tweet,
// its replies and related tweets. sort is RankingRelevance by default.
func RenderConversation(conv types.Conversation, p *prefs.Prefs, path string, sort types.RankingMode) template.HTML {
	if sort == "" {
		sort = types.RankingRelevance
	}
	hasAfter := len(conv.After.Content) > 0
	threadID := conv.Tweet.ThreadID
	d := map[string]any{}

	if len(conv.Before.Content) > 0 {
		var before []template.HTML
		first := conv.Before.Content[0]
		if threadID != first.ID && (first.ReplyID > 0 || !first.Available) {
			before = append(before, exec("earlier", formatters.TweetLink(first, true)))
		}
		for i, t := range conv.Before.Content {
			before = append(before, RenderTweet(t, p, path, TweetOpts{Index: i}))
		}
		d["Before"] = before
	}

	afterClass := ""
	if hasAfter {
		afterClass = "thread thread-line"
	}
	d["Main"] = RenderTweet(conv.Tweet, p, path, TweetOpts{Class: afterClass, MainTweet: true})

	if hasAfter {
		var after []template.HTML
		total := len(conv.After.Content) - 1
		hasMore := conv.After.HasMore
		for i, t := range conv.After.Content {
			after = append(after, RenderTweet(t, p, path, TweetOpts{
				Index: i, Last: i == total && !hasMore, AfterTweet: true,
			}))
		}
		if hasMore {
			after = append(after, renderMoreReplies(conv.After))
		}
		d["After"] = after
	}

	var rest []template.HTML
	if !p.HideReplies {
		if !conv.Replies.Beginning {
			rest = append(rest, RenderNewer(types.Query{}, formatters.TweetLink(conv.Tweet, true), "#r"))
		}
		if len(conv.Replies.Content) > 0 || conv.Replies.Bottom != "" {
			rest = append(rest, renderReplySort(sort))
			rest = append(rest, RenderReplies(conv.Replies, p, path, conv.Tweet, sort))
		}
		if flags.GuestAuth && len(conv.Before.Content) == 0 {
			link := "/search?q=" + utils.EncodeURL("conversation_id:"+strconv.FormatInt(conv.Tweet.ID, 10))
			rest = append(rest, exec("searchReplies", link))
		}
	}
	if !p.HideRelated && slices.ContainsFunc(conv.Replies.Content, func(c types.Chain) bool {
		return c.Related && len(c.Content) > 0
	}) {
		rest = append(rest, renderRelated(conv.Replies, p, path))
	}
	rest = append(rest, RenderToTop("#m"))
	d["Rest"] = rest
	return exec("conversation", d)
}

// RenderEditHistory renders a tweet's edit history.
func RenderEditHistory(edits types.EditHistory, p *prefs.Prefs, path string) template.HTML {
	var history []template.HTML
	for _, t := range edits.History {
		history = append(history, RenderTweet(t, p, path, TweetOpts{}))
	}
	return exec("editHistory", map[string]any{
		"Latest": RenderTweet(edits.Latest, p, path, TweetOpts{}), "History": history,
	})
}
