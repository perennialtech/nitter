// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"context"

	"github.com/tidwall/gjson"

	"github.com/perennialtech/nitter/internal/types"
)

func parseFxConversation(js gjson.Result, after string) *types.Conversation {
	focal := parseFxTweet(js.Get("status"))
	conv := &types.Conversation{
		Tweet:   focal,
		Replies: types.Result[types.Chain]{Beginning: after == "", Bottom: getStr(js.Get("cursor.bottom"))},
	}

	// FxTwitter lists the ancestors out of order, so follow the reply links up.
	ancestors := map[int64]*types.Tweet{}
	for _, status := range js.Get("thread").Array() {
		if t := parseFxTweet(status); t.ID != 0 && t.ID != focal.ID {
			ancestors[t.ID] = t
		}
	}
	for parent := focal.ReplyID; ancestors[parent] != nil; {
		t := ancestors[parent]
		delete(ancestors, parent)
		conv.Before.Content = append(types.Tweets{t}, conv.Before.Content...)
		parent = t.ReplyID
	}

	// Replies keep X's reply chains, flattened in order: a reply to the
	// previous reply continues its chain.
	var chains []types.Chain
	for _, status := range js.Get("replies").Array() {
		t := parseFxTweet(status)
		if t.ID == 0 {
			continue
		}
		if n := len(chains); n > 0 && t.ReplyID != focal.ID {
			last := chains[n-1].Content
			if t.ReplyID == last[len(last)-1].ID {
				chains[n-1].Content = append(last, t)
				continue
			}
		}
		chains = append(chains, types.Chain{Content: types.Tweets{t}})
	}

	// The author's own continuation leads the first page, like X's self-thread.
	if after == "" && len(chains) > 0 && chains[0].Content[0].ReplyID == focal.ID &&
		allByUser(chains[0].Content, focal.User.ID) {
		conv.After = chains[0]
		chains = chains[1:]
	}
	conv.Replies.Content = chains
	return conv
}

func allByUser(tweets types.Tweets, userID string) bool {
	for _, t := range tweets {
		if t.User.ID != userID {
			return false
		}
	}
	return true
}

// fxConversation fetches a post with its ancestors and replies. FxTwitter
// ranks replies by likes or recency; X's Relevance ranking comes out as
// likes.
func fxConversation(ctx context.Context, id string, mode types.RankingMode, after string) (*types.Conversation, error) {
	ranking := "likes"
	if mode == types.RankingRecency {
		ranking = "recency"
	}
	params := []param{{"ranking_mode", ranking}}
	if after != "" {
		params = append(params, param{"cursor", after})
	}
	js, err := fxGet(ctx, "conversation", "conversation/"+id, params, false)
	if err != nil {
		return nil, err
	}
	conv := parseFxConversation(js, after)
	if conv.Tweet.ID == 0 {
		return nil, &fxError{"FxTwitter conversation: no status in the response"}
	}
	return conv, nil
}
