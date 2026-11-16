// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"

	"github.com/perennialtech/nitter/internal/parser"
	"github.com/perennialtech/nitter/internal/types"
)

func GraphStories(ctx context.Context, cursor string) (types.StoryPage, error) {
	vars := obj{{"rest_id", "For You"}, {"limit", 25}}
	if cursor != "" {
		vars = vars.with("cursor", cursor)
	}
	u := apiURL{endpoint: graphStoryTopic, params: genParams(vars.String(), "", "{}")}
	js, err := fetch(ctx, apiReq{cookie: u, oauth: u})
	if err != nil {
		return types.StoryPage{}, err
	}
	page, ok := parser.ParseStoryPage(js, cursor)
	if !ok {
		return page, fmt.Errorf("%w: missing story list", types.ErrInternal)
	}
	return page, nil
}

func GraphStoryDetail(ctx context.Context, id string) (types.StoryDetail, error) {
	vars := obj{
		{"trendId", id}, {"includePromotedContent", false}, {"withBirdwatchNotes", false},
		{"withVoice", false}, {"withCommunity", false},
	}
	js, err := fetch(ctx, gqlReq(graphAiTrend, vars.String(), ""))
	if err != nil {
		return types.StoryDetail{}, err
	}
	return parser.ParseStoryDetail(js), nil
}

func GraphStoryPosts(ctx context.Context, timelineID, cursor string) (types.Timeline, error) {
	vars := obj{{"timelineId", timelineID}, {"count", 20}, {"withQuickPromoteEligibilityTweetFields", true}}
	if cursor != "" {
		vars = vars.with("cursor", cursor)
	}
	js, err := fetch(ctx, gqlReq(graphGenericTimelineById, vars.String(), ""))
	if err != nil {
		return types.Timeline{}, err
	}
	return parser.ParseStoryPosts(js, cursor), nil
}

// DefaultStoryTimelines encodes X's Top and Latest timelines for a story
// whose detail response did not provide tabs.
func DefaultStoryTimelines(id uint64) []types.StoryTimeline {
	encode := func(field uint16) string {
		raw := []byte("Timeline:\x0a")
		raw = binary.BigEndian.AppendUint16(raw, field)
		raw = binary.BigEndian.AppendUint64(raw, id)
		return base64.StdEncoding.EncodeToString(append(raw, 0))
	}
	return []types.StoryTimeline{
		{Label: "Top", ID: encode(0x9a)},
		{Label: "Latest", ID: encode(0x9e)},
	}
}
