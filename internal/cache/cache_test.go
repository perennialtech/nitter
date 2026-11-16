// SPDX-License-Identifier: AGPL-3.0-only
package cache

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/tidwall/gjson"

	"github.com/perennialtech/nitter/internal/api"
	"github.com/perennialtech/nitter/internal/auth"
	"github.com/perennialtech/nitter/internal/config"
	"github.com/perennialtech/nitter/internal/types"
)

func useCache(t *testing.T, maxBytes uint64) {
	c := newCache(maxBytes)
	entries = c
	t.Cleanup(func() { c.StopAllGoroutines() })
}

// counter returns a fetch that returns v for ttl and counts its calls.
func counter[T any](v T, ttl time.Duration, calls *atomic.Int32) func() (T, time.Duration, error) {
	return func() (T, time.Duration, error) {
		calls.Add(1)
		return v, ttl, nil
	}
}

func TestEntriesExpire(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		useCache(t, 1<<20)
		ctx := t.Context()
		var calls atomic.Int32
		for range 2 {
			if got, _ := cached(ctx, "k", counter("v", time.Minute, &calls)); got != "v" {
				t.Fatalf("got %q, want v", got)
			}
		}
		if n := calls.Load(); n != 1 {
			t.Fatalf("fetched %d times before the TTL, want 1", n)
		}
		time.Sleep(time.Minute + time.Second)
		cached(ctx, "k", counter("v", time.Minute, &calls))
		if n := calls.Load(); n != 2 {
			t.Fatalf("fetched %d times after the TTL, want 2", n)
		}
	})
}

func TestZeroTTLIsNotCached(t *testing.T) {
	useCache(t, 1<<20)
	var calls atomic.Int32
	for range 2 {
		cached(t.Context(), "k", counter("v", 0, &calls))
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("fetched %d times, want 2", n)
	}
}

func TestUserTweetsCache(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		vars := gjson.Parse(r.URL.Query().Get("variables"))
		switch vars.Get("cursor").String() {
		case "empty":
			fmt.Fprint(w, `{"data":{"user":{"result":{"timeline":{"timeline":{"instructions":[]}}}}}}`)
		case "invalid":
			fmt.Fprint(w, `{"data":{"user":{"result":{}}}}`)
		default:
			operation := r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:]
			bottom := vars.Get("userId").String() + "/" + operation + "/" + vars.Get("cursor").String()
			fmt.Fprintf(w, `{"data":{"user":{"result":{"timeline":{"timeline":{"instructions":[{"type":"TimelineAddEntries","entries":[{"entryId":"cursor-bottom-1","content":{"value":%q}}]}]}}}}}}`, bottom)
		}
	}))
	t.Cleanup(upstream.Close)

	cfg := &config.Config{APIProxy: upstream.URL, DisableTid: true, MaxRetries: 1, CacheMaxMB: 1}
	path := filepath.Join(t.TempDir(), "sessions.jsonl")
	if err := os.WriteFile(path, []byte(`{"kind":"cookie","auth_token":"fixture","ct0":"fixture"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := auth.Init(cfg, path); err != nil {
		t.Fatal(err)
	}
	if err := api.Init(cfg, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { api.Init(&config.Config{}, "") })

	for _, enabled := range []bool{false, true} {
		if enabled {
			cfg.UserTweetsCacheTime = 5
		}
		Init(cfg)
		c := entries
		t.Cleanup(func() { c.StopAllGoroutines() })
		before := calls.Load()
		for _, tc := range []struct {
			id, cursor string
			kind       types.TimelineKind
			bottom     string
		}{
			{"123", "", types.TimelineTweets, "123/UserOriginalsTimeline/"},
			{"456", "", types.TimelineTweets, "456/UserOriginalsTimeline/"},
			{"123", "other+/_=", types.TimelineTweets, "123/UserOriginalsTimeline/other+/_="},
			{"123", "", types.TimelineReplies, "123/UserRepliesTimeline/"},
			{"123", "empty", types.TimelineTweets, ""},
			{"123", "invalid", types.TimelineTweets, ""},
		} {
			for range 2 {
				p, err := UserTweets(t.Context(), tc.id, tc.kind, tc.cursor, false)
				if tc.cursor == "invalid" {
					if !errors.Is(err, types.ErrRateLimited) {
						t.Fatalf("incomplete timeline error = %v, want ErrRateLimited", err)
					}
					continue
				}
				if err != nil || p.Tweets.Bottom != tc.bottom || p.Tweets.Beginning != (tc.cursor == "") {
					t.Fatalf("timeline %s %q = %+v, %v", tc.id, tc.cursor, p.Tweets, err)
				}
			}
		}
		want := int32(12)
		if enabled {
			want = 8 // four pages cached, two empty or invalid results refetched
		}
		if got := calls.Load() - before; got != want {
			t.Fatalf("cache enabled %v: %d upstream requests, want %d", enabled, got, want)
		}
	}
	t.Cleanup(func() { userTweetsCacheTime = 0 })
}

func TestEvictsToTheCap(t *testing.T) {
	const maxBytes = 64 << 10
	useCache(t, maxBytes)
	const n = 100
	for i := range n {
		// Random bytes don't compress: each entry weighs about 5 KB.
		value := make([]byte, 4<<10)
		rand.Read(value)
		store(fmt.Sprint("k", i), time.Hour, value)
	}
	entries.CleanUp()
	if w := entries.WeightedSize(); w > maxBytes {
		t.Fatalf("cache holds %d bytes, over its %d byte cap", w, maxBytes)
	}
	if c := entries.EstimatedSize(); c == 0 || c >= n {
		t.Fatalf("cache holds %d of %d entries, want some evicted and some kept", c, n)
	}
}

func TestRssRoundTrip(t *testing.T) {
	useCache(t, 1<<20)
	rssCacheTime = time.Minute
	ctx := t.Context()
	var calls atomic.Int32
	feed := types.Rss{
		Feed:   `<?xml version="1.0"?><rss><channel><title>"a" & b</title></channel></rss>`,
		Cursor: "1234567890",
	}
	build := func() (types.Rss, error) {
		calls.Add(1)
		return feed, nil
	}
	for range 2 {
		got, err := Rss(ctx, "twitter:jack", build)
		if err != nil || got != feed {
			t.Fatalf("got %+v, %v; want %+v", got, err, feed)
		}
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("built %d times, want 1", n)
	}

	// A feed without a cursor wasn't found, and isn't cached.
	notFound := func() (types.Rss, error) {
		calls.Add(1)
		return types.Rss{}, nil
	}
	calls.Store(0)
	for range 2 {
		Rss(ctx, "twitter:nobody", notFound)
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("built a missing feed %d times, want 2", n)
	}
}

func TestConcurrentMissesShareAFetch(t *testing.T) {
	useCache(t, 1<<20)
	ctx := t.Context()
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	fetch := func() (string, time.Duration, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return "v", time.Minute, nil
	}

	const n = 10
	var wg sync.WaitGroup
	results := make([]string, n)
	get := func(i int) {
		defer wg.Done()
		results[i], _ = cached(ctx, "k", fetch)
	}
	wg.Add(n)
	go get(0)
	<-started
	for i := 1; i < n; i++ {
		go get(i)
	}
	time.Sleep(50 * time.Millisecond) // let the rest join the fetch
	close(release)
	wg.Wait()

	if c := calls.Load(); c != 1 {
		t.Fatalf("fetched %d times, want 1", c)
	}
	for i, r := range results {
		if r != "v" {
			t.Fatalf("request %d got %q, want v", i, r)
		}
	}
}

func TestCanceledFetchIsRetriedByWaiters(t *testing.T) {
	useCache(t, 1<<20)
	leaderCtx, cancel := context.WithCancel(t.Context())
	started := make(chan struct{})
	var calls atomic.Int32
	leaderErr := make(chan error)
	go func() {
		_, err := cached(leaderCtx, "k", func() (string, time.Duration, error) {
			calls.Add(1)
			close(started)
			<-leaderCtx.Done()
			return "", 0, leaderCtx.Err()
		})
		leaderErr <- err
	}()
	<-started

	got := make(chan string)
	go func() {
		v, err := cached(t.Context(), "k", counter("v", time.Minute, &calls))
		if err != nil {
			t.Error(err)
		}
		got <- v
	}()
	time.Sleep(50 * time.Millisecond) // let the waiter join the fetch
	cancel()

	if err := <-leaderErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("leader got %v, want context.Canceled", err)
	}
	if v := <-got; v != "v" {
		t.Fatalf("waiter got %q, want v", v)
	}
	if c := calls.Load(); c != 2 {
		t.Fatalf("fetched %d times, want 2", c)
	}
}
