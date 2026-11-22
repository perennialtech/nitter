// SPDX-License-Identifier: AGPL-3.0-only

package query

import (
	"slices"
	"testing"
	"time"

	"github.com/perennialtech/nitter/internal/types"
)

func TestHasSearchInput(t *testing.T) {
	if HasSearchInput(types.Query{Kind: types.QueryTweets, FromUser: []string{"alice"}}) {
		t.Fatal("expected empty query to have no search input")
	}
	for _, q := range []types.Query{
		{Text: "hello"},
		{Filters: []string{"images"}},
		{Excludes: []string{"replies"}},
		{Includes: []string{"nativeretweets"}},
		{Since: "2026-01-01"},
		{Until: "2026-12-31"},
		{MinLikes: "10"},
	} {
		if !HasSearchInput(q) {
			t.Fatalf("expected HasSearchInput=true for %+v", q)
		}
	}
}

func TestParseFromUsersAndApplyOperators(t *testing.T) {
	text := `from:@Alice from:bob from:alice filter:images -filter:replies include:nativeretweets -include:links since:2026-01-01 until:2026-11-22 min_faves:42 "hello world" -spam`
	users := ParseFromUsers(text, 20)
	if !slices.Equal(users, []string{"Alice", "bob"}) {
		t.Fatalf("ParseFromUsers = %v", users)
	}
	var q types.Query
	rest := ApplySearchOperators(&q, text)
	if rest != `"hello world" -spam` {
		t.Fatalf("remaining text = %q", rest)
	}
	if !slices.Equal(q.Filters, []string{"images"}) ||
		!slices.Equal(q.Excludes, []string{"replies", "links"}) ||
		!slices.Equal(q.Includes, []string{"nativeretweets"}) ||
		q.Since != "2026-01-01" || q.Until != "2026-11-22" || q.MinLikes != "42" {
		t.Fatalf("parsed query = %+v", q)
	}
}

func TestMatchesQuery(t *testing.T) {
	tweet := &types.Tweet{
		ID:      10,
		Text:    "Launching Go 1.27 today! https://example.com/news @gopher",
		Time:    time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC),
		Stats:   types.TweetStats{Likes: 50},
		Media:   []types.Media{{Kind: types.PhotoMedia, Photo: types.Photo{URL: "a.jpg", AltText: "release chart"}}},
		Card:    &types.Card{Kind: types.CardSummary, URL: "https://example.com/news", Title: "Release Notes"},
		ReplyID: 0,
	}

	if !MatchesQuery(tweet, types.Query{Text: "go"}) {
		t.Fatal("expected word-boundary match for 'go'")
	}
	if MatchesQuery(tweet, types.Query{Text: "launch"}) {
		t.Fatal("expected 'launch' not to match substring of 'Launching'")
	}
	if !MatchesQuery(tweet, types.Query{Text: `"release chart"`}) {
		t.Fatal("expected alt text match")
	}
	if !MatchesQuery(tweet, types.Query{Text: "rust OR go -python"}) {
		t.Fatal("expected OR group with exclusion to match")
	}
	if MatchesQuery(tweet, types.Query{Text: "go -today"}) {
		t.Fatal("expected excluded term to reject tweet")
	}
	if !MatchesQuery(tweet, types.Query{
		Since:    "2026-06-01",
		Until:    "2026-06-30",
		MinLikes: "50",
		Filters:  []string{"images", "links", "mentions"},
		Excludes: []string{"replies", "retweets"},
	}) {
		t.Fatal("expected filters, dates, and min_faves to match")
	}
	if MatchesQuery(tweet, types.Query{MinLikes: "51"}) {
		t.Fatal("expected min_faves=51 to reject tweet with 50 likes")
	}
	if MatchesQuery(tweet, types.Query{Since: "2026-06-16"}) {
		t.Fatal("expected since after tweet date to reject tweet")
	}
	if !MatchesQuery(tweet, types.Query{Filters: []string{"native_video", "twimg"}, Sep: "OR"}) {
		t.Fatal("expected OR filter separator to match photo tweet")
	}

	// Retweet media fallback
	rt := &types.Tweet{
		ID: 20,
		Retweet: &types.Tweet{
			ID:    19,
			Media: []types.Media{{Kind: types.VideoMedia, Video: types.Video{Title: "clip"}}},
		},
	}
	if !MatchesQuery(rt, types.Query{Filters: []string{"native_video", "retweets"}}) {
		t.Fatal("expected retweet to match native_video and retweets filters")
	}
}
