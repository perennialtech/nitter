// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"fmt"
	"slices"
	"testing"

	"github.com/tidwall/gjson"

	"github.com/perennialtech/nitter/internal/types"
)

func fxPost(id, user, replyTo string) string {
	reply := "null"
	if replyTo != "" {
		reply = `{"screen_name": "x", "status": "` + replyTo + `"}`
	}
	return fmt.Sprintf(`{"type": "status", "id": %q, "created_timestamp": 1794514608,
      "author": {"type": "profile", "id": %q, "screen_name": "u%s", "name": "u",
        "raw_description": {"text": "", "facets": []}},
      "replying_to": %s,
      "raw_text": {"text": "post %s", "display_text_range": [0, 7], "facets": []}}`,
		id, user, user, reply, id)
}

// fxConversationJSON is a thread in the order FxTwitter returns it: parent,
// root, then the focal post.
var fxConversationJSON = gjson.Parse(`{"code": 200, "cursor": {"bottom": "next"},
	"status": ` + fxPost("30", "1", "20") + `,
	"thread": [` + fxPost("20", "2", "10") + `, ` + fxPost("10", "1", "") + `, ` + fxPost("30", "1", "20") + `],
	"replies": [` + fxPost("31", "1", "30") + `, ` + fxPost("32", "1", "31") + `, ` +
	fxPost("40", "3", "30") + `, ` + fxPost("41", "1", "40") + `, ` + fxPost("50", "4", "30") + `]}`)

func ids(tweets types.Tweets) []int64 {
	var out []int64
	for _, t := range tweets {
		out = append(out, t.ID)
	}
	return out
}

func chainIDs(chains []types.Chain) [][]int64 {
	var out [][]int64
	for _, c := range chains {
		out = append(out, ids(c.Content))
	}
	return out
}

func TestFxConversationOrdersAncestorsFromRoot(t *testing.T) {
	conv := parseFxConversation(fxConversationJSON, "")
	if conv.Tweet.ID != 30 || !slices.Equal(ids(conv.Before.Content), []int64{10, 20}) {
		t.Fatalf("tweet %d, before %v", conv.Tweet.ID, ids(conv.Before.Content))
	}
}

func TestFxConversationSelfThread(t *testing.T) {
	conv := parseFxConversation(fxConversationJSON, "")
	if got := ids(conv.After.Content); !slices.Equal(got, []int64{31, 32}) {
		t.Fatalf("after = %v", got)
	}
}

func TestFxConversationRegroupsReplyChains(t *testing.T) {
	conv := parseFxConversation(fxConversationJSON, "")
	got := chainIDs(conv.Replies.Content)
	want := [][]int64{{40, 41}, {50}}
	if !conv.Replies.Beginning || conv.Replies.Bottom != "next" ||
		!slices.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("replies = %v, beginning %v, bottom %q", got, conv.Replies.Beginning, conv.Replies.Bottom)
	}
}

func TestFxConversationLaterPagesHaveNoSelfThread(t *testing.T) {
	conv := parseFxConversation(fxConversationJSON, "c")
	if len(conv.After.Content) != 0 || conv.Replies.Beginning || len(conv.Replies.Content) != 3 {
		t.Fatalf("after %v, beginning %v, replies %v", ids(conv.After.Content), conv.Replies.Beginning,
			chainIDs(conv.Replies.Content))
	}
}
