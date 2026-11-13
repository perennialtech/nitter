// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/gjson"

	"github.com/perennialtech/nitter/internal/types"
)

const trendsURL = "https://api.twitter.com/" + restTrends

var trendsCacheTTL = 15 * time.Minute

type trendsEntry struct {
	trends  []types.Trend
	fetched time.Time
}

// trendsCall is a refresh that concurrent requests for one woeid share.
type trendsCall struct {
	done   chan struct{}
	trends []types.Trend
}

var trendsState struct {
	sync.Mutex
	cache   map[int]trendsEntry
	pending map[int]*trendsCall
}

type trendsFetcher func(ctx context.Context, woeid int) []types.Trend

// fetchTrendsUncached fetches the trends for woeid, or nothing on failure.
// The endpoint needs no account or guest session, so it bypasses the pool
// rather than spending a request from it.
func fetchTrendsUncached(ctx context.Context, woeid int) []types.Trend {
	h := http.Header{}
	h.Set("authorization", bearerToken)
	h.Set("accept", "*/*")
	h.Set("accept-language", "en-US,en;q=0.9")
	h.Set("user-agent", userAgent)

	resp, err := doRequest(ctx, http.MethodGet, trendsURL+"?id="+strconv.Itoa(woeid), h, "", apiTimeout)
	if err != nil {
		log.Printf("[trends] error fetching woeid %d: %v", woeid, err)
		return nil
	}
	if resp.status != http.StatusOK {
		log.Printf("[trends] fetch failed for woeid %d, status: %d", woeid, resp.status)
		return nil
	}
	if !gjson.Valid(resp.body) {
		log.Printf("[trends] error fetching woeid %d: invalid JSON", woeid)
		return nil
	}
	js := gjson.Parse(resp.body)
	list := items(js)
	if len(list) == 0 || !list[0].Get("trends").Exists() {
		log.Printf("[trends] unexpected response shape for woeid %d", woeid)
		return nil
	}

	var result []types.Trend
	for _, t := range items(list[0].Get("trends")) {
		vol := -1
		if v := t.Get("tweet_volume"); notNull(v) {
			vol = getInt(v)
		}
		nameJs := t.Get("name")
		if !nameJs.Exists() {
			log.Printf("[trends] error fetching woeid %d: trend without a name", woeid)
			return nil
		}
		// cached for trendsCacheTTL; don't pin the response body
		name := strings.Clone(getStr(nameJs))
		searchQuery := name
		// take the already decoded query string
		if q := t.Get("query"); q.Type == gjson.String {
			if decoded, err := url.QueryUnescape(q.Str); err == nil {
				searchQuery = strings.Clone(decoded)
			} else {
				searchQuery = strings.Clone(q.Str)
			}
		}
		result = append(result, types.Trend{Name: name, SearchQuery: searchQuery, TweetVolume: vol})
	}
	return result
}

// Trends returns up to maxItems (all if negative) trends for woeid, cached
// for trendsCacheMinutes. A failed refresh is cached too, serving the
// previous trends if there were any.
func Trends(ctx context.Context, woeid, maxItems int) ([]types.Trend, error) {
	return getTrends(ctx, woeid, maxItems, fetchTrendsUncached)
}

func getTrends(ctx context.Context, woeid, maxItems int, fetch trendsFetcher) ([]types.Trend, error) {
	capped := func(t []types.Trend) []types.Trend {
		if maxItems >= 0 && len(t) > maxItems {
			return t[:maxItems:maxItems]
		}
		return t[:len(t):len(t)]
	}

	s := &trendsState
	s.Lock()
	if e, ok := s.cache[woeid]; ok && time.Since(e.fetched) < trendsCacheTTL {
		s.Unlock()
		return capped(e.trends), nil
	}
	call, ok := s.pending[woeid]
	if !ok {
		call = &trendsCall{done: make(chan struct{})}
		if s.pending == nil {
			s.pending = map[int]*trendsCall{}
			s.cache = map[int]trendsEntry{}
		}
		s.pending[woeid] = call
		go refreshTrends(context.WithoutCancel(ctx), woeid, fetch, call)
	}
	s.Unlock()

	select {
	case <-call.done:
		return capped(call.trends), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func refreshTrends(ctx context.Context, woeid int, fetch trendsFetcher, call *trendsCall) {
	fresh := fetch(ctx, woeid)
	s := &trendsState
	s.Lock()
	defer s.Unlock()
	if len(fresh) == 0 {
		fresh = s.cache[woeid].trends
	}
	s.cache[woeid] = trendsEntry{trends: fresh, fetched: time.Now()}
	delete(s.pending, woeid)
	call.trends = fresh
	close(call.done)
}
