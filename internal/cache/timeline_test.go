// SPDX-License-Identifier: AGPL-3.0-only

package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/perennialtech/nitter/internal/types"
)

func TestMultiUserTimelineFilterPaginateAndDedupe(t *testing.T) {
	useCache(t, 1<<20)
	origID, origTweets := lookupUserID, lookupUserTweets
	t.Cleanup(func() {
		lookupUserID = origID
		lookupUserTweets = origTweets
	})

	lookupUserID = func(_ context.Context, name string) (string, error) {
		if name == "alice" {
			return "1", nil
		}
		return "", nil
	}

	gate := make(chan struct{})
	var p1Calls atomic.Int32
	lookupUserTweets = func(_ context.Context, id string, kind types.TimelineKind, cursor string, _ bool) (types.Profile, error) {
		switch cursor {
		case "":
			if p1Calls.Add(1) == 1 {
				<-gate
			}
			return types.Profile{Tweets: types.Timeline{
				Bottom: "c2",
				Content: []types.Tweets{
					{{ID: 100, Text: "no match", Available: true}},
					{{ID: 90, Text: "golang release", Available: true}},
				},
			}}, nil
		case "c2":
			return types.Profile{Tweets: types.Timeline{
				Content: []types.Tweets{
					{{ID: 80, Text: "another golang post", Available: true}},
				},
			}}, nil
		default:
			return types.Profile{}, nil
		}
	}

	q := types.Query{Kind: types.QueryTweets, Text: "from:alice golang"}
	var wg sync.WaitGroup
	results := make([]types.Timeline, 2)
	for i := range results {
		wg.Go(func() {
			tl, err := MultiUserTimeline(t.Context(), q, []string{"alice"}, types.TimelineTweets, "")
			if err != nil {
				t.Errorf("MultiUserTimeline: %v", err)
			}
			results[i] = tl
		})
	}
	time.Sleep(25 * time.Millisecond)
	close(gate)
	wg.Wait()

	if p1Calls.Load() != 1 {
		t.Fatalf("expected singleflight dedupe on page 1, got %d calls", p1Calls.Load())
	}
	for i, tl := range results {
		if len(tl.Content) != 2 || tl.Content[0][0].ID != 90 || tl.Content[1][0].ID != 80 {
			t.Fatalf("result[%d] = %+v", i, tl.Content)
		}
		if tl.Bottom != "" {
			t.Fatalf("expected exhausted bottom cursor, got %q", tl.Bottom)
		}
	}

	// Second call hits filtered timeline cache
	if _, err := MultiUserTimeline(t.Context(), q, []string{"alice"}, types.TimelineTweets, ""); err != nil {
		t.Fatal(err)
	}
	if p1Calls.Load() != 1 {
		t.Fatalf("expected cached filtered timeline hit, got %d calls", p1Calls.Load())
	}
}

func TestUserMediaTimelineSensitiveFallback(t *testing.T) {
	useCache(t, 1<<20)
	origID, origTweets := lookupUserID, lookupUserTweets
	t.Cleanup(func() {
		lookupUserID = origID
		lookupUserTweets = origTweets
	})

	lookupUserID = func(_ context.Context, name string) (string, error) {
		return "42", nil
	}
	lookupUserTweets = func(_ context.Context, id string, kind types.TimelineKind, cursor string, _ bool) (types.Profile, error) {
		if kind == types.TimelineMedia {
			return types.Profile{Tweets: types.Timeline{Beginning: true}}, nil
		}
		return types.Profile{Tweets: types.Timeline{
			Content: []types.Tweets{
				{{ID: 10, Text: "text only", Available: true}},
				{{ID: 9, Text: "with photo", Available: true, Sensitive: true,
					Media: []types.Media{{Kind: types.PhotoMedia, Photo: types.Photo{URL: "a.jpg"}}}}},
			},
		}}, nil
	}

	q := types.Query{Kind: types.QueryMedia, FromUser: []string{"alice"}, ProfileMedia: true}
	profile, err := UserMediaTimeline(t.Context(), "42", "alice", q, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Tweets.Content) != 1 || profile.Tweets.Content[0][0].ID != 9 {
		t.Fatalf("expected sensitive media fallback tweet 9, got %+v", profile.Tweets.Content)
	}
}

func TestTweetSearchCachesNonEmptyPages(t *testing.T) {
	useCache(t, 1<<20)
	origSearch := lookupTweetSearch
	t.Cleanup(func() { lookupTweetSearch = origSearch })

	calls := 0
	lookupTweetSearch = func(_ context.Context, q types.Query, _ string) (types.Timeline, error) {
		calls++
		if q.Text == "empty" {
			return types.Timeline{Beginning: true, Query: q}, nil
		}
		return types.Timeline{
			Beginning: true,
			Query:     q,
			Content:   []types.Tweets{{{ID: 42, Text: "cached tweet"}}},
		}, nil
	}

	q := types.Query{Kind: types.QueryTweets, Text: "nitter"}
	tl1, err := TweetSearch(context.Background(), q, "")
	if err != nil || len(tl1.Content) != 1 {
		t.Fatalf("first TweetSearch = %+v, %v", tl1, err)
	}
	tl2, err := TweetSearch(context.Background(), q, "")
	if err != nil || len(tl2.Content) != 1 {
		t.Fatalf("second TweetSearch = %+v, %v", tl2, err)
	}
	if calls != 1 {
		t.Fatalf("lookupTweetSearch called %d times, want 1 (cached)", calls)
	}

	emptyQ := types.Query{Kind: types.QueryTweets, Text: "empty"}
	_, _ = TweetSearch(context.Background(), emptyQ, "")
	_, _ = TweetSearch(context.Background(), emptyQ, "")
	if calls != 3 {
		t.Fatalf("empty search should not be cached: calls = %d, want 3", calls)
	}
}

func TestMultiUserTimelinePartialFailure(t *testing.T) {
	useCache(t, 1<<20)
	origUserID := lookupUserID
	origUserTweets := lookupUserTweets
	t.Cleanup(func() {
		lookupUserID = origUserID
		lookupUserTweets = origUserTweets
	})

	lookupUserID = func(_ context.Context, name string) (string, error) {
		return "id-" + name, nil
	}
	calls := 0
	lookupUserTweets = func(_ context.Context, id string, _ types.TimelineKind, _ string, _ bool) (types.Profile, error) {
		calls++
		if id == "id-broken" {
			return types.Profile{}, errors.New("upstream failed")
		}
		return types.Profile{
			Tweets: types.Timeline{
				Content: []types.Tweets{{{ID: 100, Text: "ok tweet"}}},
			},
		}, nil
	}

	q := types.Query{Kind: types.QueryTweets, Text: "ok"}
	tl, err := MultiUserTimeline(context.Background(), q, []string{"good", "broken"}, types.TimelineTweets, "")
	if err != nil {
		t.Fatalf("MultiUserTimeline returned error on partial failure: %v", err)
	}
	if !tl.Failed {
		t.Fatalf("expected tl.Failed=true on partial failure")
	}
	if len(tl.Content) != 1 || tl.Content[0][0].ID != 100 {
		t.Fatalf("expected partial results from healthy user, got %+v", tl.Content)
	}

	// Failed first page must not be cached.
	before := calls
	_, err = MultiUserTimeline(context.Background(), q, []string{"good", "broken"}, types.TimelineTweets, "")
	if err != nil {
		t.Fatalf("second MultiUserTimeline error: %v", err)
	}
	if calls == before {
		t.Fatalf("failed first-page MultiUserTimeline should not have been cached")
	}
}
