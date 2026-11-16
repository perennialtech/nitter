// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"context"
	"strings"

	"github.com/perennialtech/nitter/internal/parser"
	"github.com/perennialtech/nitter/internal/types"
	"github.com/tidwall/gjson"
)

const (
	graphCommunitySearch = "HKgnJbw10TZW41Na4ToaSw/CommunitiesSearchQuery"
	graphCommunityTop    = "NmHwTyC9Og3VP8Kl5cMYcA/GlobalCommunitiesPostSearchTimeline"
	graphCommunityLatest = "LZT5Y3qRJDPEIvSXL7RyHg/GlobalCommunitiesLatestPostSearchTimeline"
)

func communitySearch(ctx context.Context, endpoint, key, text, after string) (gjson.Result, error) {
	vars := obj{}.with(key, text)
	if after != "" {
		vars = vars.with("cursor", after)
	}
	req := gqlReq(endpoint, vars.String(), "")
	req.search = true
	return fetch(ctx, req)
}

func GraphCommunitySearch(ctx context.Context, q types.Query, after string) (types.Result[types.Community], error) {
	if strings.TrimSpace(q.Text) == "" {
		return types.Result[types.Community]{Query: q, Beginning: true}, nil
	}
	js, err := communitySearch(ctx, graphCommunitySearch, "query", q.Text, after)
	if err != nil {
		return types.Result[types.Community]{}, err
	}
	result := parser.ParseGraphCommunitySearch(js, after)
	result.Query = q
	return result, nil
}

func GraphCommunityPostSearch(ctx context.Context, q types.Query, after string) (types.Timeline, error) {
	if strings.TrimSpace(q.Text) == "" {
		return types.Timeline{Query: q, Beginning: true}, nil
	}
	endpoint := graphCommunityTop
	if q.Kind == types.QueryCommunitiesLatest {
		endpoint = graphCommunityLatest
	}
	js, err := communitySearch(ctx, endpoint, "rawQuery", q.Text, after)
	if err != nil {
		return types.Timeline{}, err
	}
	result := parser.ParseGraphSearchTweets(js, after)
	result.Query = q
	prefix := min(64, len(after), len(result.Bottom))
	if prefix > 0 && after[:prefix] == result.Bottom[:prefix] {
		result.Bottom = ""
		result.Content = nil
	}
	return result, nil
}
