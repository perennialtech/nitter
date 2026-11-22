// SPDX-License-Identifier: AGPL-3.0-only

package cache

import (
	"context"
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
