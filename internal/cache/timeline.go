// SPDX-License-Identifier: AGPL-3.0-only

package cache

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/perennialtech/nitter/internal/api"
	"github.com/perennialtech/nitter/internal/query"
	"github.com/perennialtech/nitter/internal/types"
)

const (
	multiUserPageSize       = 20
	MaxMultiUsers           = 20
	maxFetchRounds          = 10
	maxFilteredFetchRounds  = 6
	filteredSearchCacheTime = 2 * time.Minute
	searchPageCacheTime     = 45 * time.Second

	MediaFallbackPrefix   = "mf:"
	MultiUserCursorPrefix = "mu:"
)

var (
	lookupUserID      = UserID
	lookupUserTweets  = UserTweets
	lookupTweetSearch = api.GraphTweetSearch
)

type multiUserState struct {
	UserID    string `json:"u"`
	Cursor    string `json:"c,omitempty"`
	Cut       int64  `json:"t"`
	Exhausted bool   `json:"e,omitempty"`
}

func itemID(thread types.Tweets) int64 {
	for _, t := range thread {
		if t != nil && t.ID > 0 {
			return t.ID
		}
	}
	return 0
}

func encodeMultiUserCursor(states []multiUserState) string {
	data, err := json.Marshal(states)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeMultiUserCursor(cursor string) ([]multiUserState, bool) {
	if cursor == "" {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, false
	}
	var states []multiUserState
	if err := json.Unmarshal(raw, &states); err != nil || len(states) == 0 {
		return nil, false
	}
	for i := range states {
		if states[i].Cut <= 0 {
			states[i].Cut = math.MaxInt64
		}
	}
	return states, true
}

// TimelineKindFor maps a QueryKind to the corresponding user timeline endpoint kind.
func TimelineKindFor(q types.Query) types.TimelineKind {
	switch q.Kind {
	case types.QueryReplies:
		return types.TimelineReplies
	case types.QueryMedia:
		return types.TimelineMedia
	case types.QueryArticles:
		return types.TimelineArticles
	case types.QueryReposts:
		return types.TimelineReposts
	default:
		return types.TimelineTweets
	}
}

func threadMatches(thread types.Tweets, q types.Query) bool {
	for _, t := range thread {
		if query.MatchesQuery(t, q) {
			return true
		}
	}
	return false
}

type indexedThread struct {
	idx    int
	thread types.Tweets
}

func dedupeThreads(threads []indexedThread) []indexedThread {
	seen := make(map[int64]struct{}, len(threads))
	out := make([]indexedThread, 0, len(threads))
	for _, item := range threads {
		id := itemID(item.thread)
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, item)
	}
	return out
}

func filteredTimelineKey(q types.Query, kind types.TimelineKind, names []string, after string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s|%s",
		query.Param(q, ""), kind, strings.Join(names, ","), after)))
	return "fs:" + hex.EncodeToString(sum[:16])
}

func fetchMultiUserTimeline(ctx context.Context, q types.Query, names []string,
	kind types.TimelineKind, after string) (types.Timeline, error) {
	q.Text = query.ApplySearchOperators(&q, q.Text)
	fetchKind := kind
	if kind == types.TimelineMedia {
		fetchKind = types.TimelineTweets
		if len(q.Filters) == 0 && query.MediaLinkTerm(q) == "" {
			q.Filters = []string{"twimg", "native_video"}
			q.Sep = "OR"
		}
	}

	filtered := query.HasSearchInput(q) || q.Kind == types.QueryMedia
	maxRounds := maxFetchRounds
	if filtered {
		maxRounds = maxFilteredFetchRounds
	}

	result := types.Timeline{Beginning: after == "", Query: q}

	var states []multiUserState
	if decoded, ok := decodeMultiUserCursor(after); ok {
		states = decoded
	} else {
		seenIDs := map[string]struct{}{}
		for _, name := range names {
			if len(states) >= MaxMultiUsers {
				break
			}
			id, err := lookupUserID(ctx, name)
			if err != nil {
				return types.Timeline{}, err
			}
			if id == "" || id == "suspended" {
				continue
			}
			if _, ok := seenIDs[id]; ok {
				continue
			}
			seenIDs[id] = struct{}{}
			states = append(states, multiUserState{UserID: id, Cut: math.MaxInt64})
		}
	}
	if len(states) == 0 {
		return result, nil
	}

	var displayed []types.Tweets
	truncated := false
	failed := false

	for range maxRounds {
		var candidates []indexedThread
		pages := make([]string, len(states))
		counts := make([]int, len(states))
		failedBefore := failed
		failed = false

		for i, state := range states {
			if state.Exhausted {
				continue
			}
			profile, err := lookupUserTweets(ctx, state.UserID, fetchKind, state.Cursor, false)
			if err != nil {
				if ctx.Err() != nil {
					return types.Timeline{}, ctx.Err()
				}
				failed = true
				continue
			}
			for _, thread := range profile.Tweets.Content {
				id := itemID(thread)
				if id <= 0 {
					continue
				}
				if id < state.Cut && threadMatches(thread, q) {
					candidates = append(candidates, indexedThread{idx: i, thread: thread})
					counts[i]++
				}
			}
			if profile.Tweets.Bottom == "" || profile.Tweets.Bottom == state.Cursor {
				states[i].Exhausted = true
			}
			pages[i] = profile.Tweets.Bottom
		}

		candidates = dedupeThreads(candidates)
		slices.SortStableFunc(candidates, func(a, b indexedThread) int {
			idA, idB := itemID(a.thread), itemID(b.thread)
			switch {
			case idA > idB:
				return -1
			case idA < idB:
				return 1
			default:
				return 0
			}
		})

		remaining := multiUserPageSize - len(displayed)
		take := min(remaining, len(candidates))
		if len(candidates) > take {
			truncated = true
		}

		shown := make([]int, len(states))
		shownMin := make([]int64, len(states))
		for i := range shownMin {
			shownMin[i] = math.MaxInt64
		}
		for _, item := range candidates[:take] {
			displayed = append(displayed, item.thread)
			shown[item.idx]++
			if id := itemID(item.thread); id < shownMin[item.idx] {
				shownMin[item.idx] = id
			}
		}

		progressed := false
		for i := range states {
			if shown[i] > 0 && shownMin[i] < states[i].Cut {
				states[i].Cut = shownMin[i]
			}
			if shown[i] == counts[i] {
				if !states[i].Exhausted && states[i].Cursor != pages[i] {
					states[i].Cursor = pages[i]
					progressed = true
				}
			}
		}

		if len(displayed) >= multiUserPageSize {
			break
		}
		failed = failed || failedBefore
		if failed || !progressed {
			break
		}
	}

	result.Content = displayed
	result.Failed = failed

	hasMore := truncated
	for _, state := range states {
		if !state.Exhausted && state.Cursor != "" {
			hasMore = true
			break
		}
	}
	if hasMore && (len(displayed) > 0 || filtered) {
		result.Bottom = encodeMultiUserCursor(states)
	}
	return result, nil
}

// MultiUserTimeline fetches and merges timelines for names, applying local search
// operators and filters with singleflight deduplication and short-TTL caching.
func MultiUserTimeline(ctx context.Context, q types.Query, names []string,
	kind types.TimelineKind, after string) (types.Timeline, error) {
	qCopy := q
	qCopy.Text = query.ApplySearchOperators(&qCopy, qCopy.Text)
	filtered := query.HasSearchInput(qCopy) || qCopy.Kind == types.QueryMedia
	if !filtered || len(names) == 0 || entries == nil {
		return fetchMultiUserTimeline(ctx, q, names, kind, after)
	}

	key := filteredTimelineKey(qCopy, kind, names, after)
	tl, err := cached(ctx, key, func() (types.Timeline, time.Duration, error) {
		tl, err := fetchMultiUserTimeline(ctx, q, names, kind, after)
		if err != nil {
			return tl, 0, err
		}
		cacheable := !tl.Failed || after != ""
		if cacheable && (len(tl.Content) > 0 || (after != "" && tl.Bottom != "")) {
			return tl, filteredSearchCacheTime, nil
		}
		return tl, 0, nil
	})
	if err != nil {
		return types.Timeline{}, err
	}
	tl.Query = qCopy
	return tl, nil
}

// UserMediaTimeline fetches a profile's media timeline, falling back to filtering
// the user's regular tweets when UserMedia returns an empty first page (for example,
// on accounts whose media is flagged as sensitive).
func UserMediaTimeline(ctx context.Context, userID, name string, q types.Query, after string) (types.Profile, error) {
	fallbackCursor, useFallback := strings.CutPrefix(after, MediaFallbackPrefix)
	if !useFallback {
		profile, err := lookupUserTweets(ctx, userID, types.TimelineMedia, after, false)
		if err != nil || after != "" || profile.User.Protected || len(profile.Tweets.Content) > 0 {
			profile.Tweets.Query = q
			return profile, err
		}
		useFallback = true
	}

	fq := q
	fq.Kind = types.QueryMedia
	if len(fq.Filters) == 0 {
		fq.Filters = []string{"twimg", "native_video"}
		fq.Sep = "OR"
	}
	tl, err := MultiUserTimeline(ctx, fq, []string{name}, types.TimelineMedia, fallbackCursor)
	if err != nil {
		return types.Profile{}, err
	}
	tl.Query = q
	tl.Beginning = after == ""
	if tl.Bottom != "" {
		tl.Bottom = MediaFallbackPrefix + tl.Bottom
	}
	return types.Profile{Tweets: tl}, nil
}

func searchPageKey(q types.Query, after string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s",
		query.Param(q, ""), q.Kind, after)))
	return "ts:" + hex.EncodeToString(sum[:16])
}

// TweetSearch fetches a page of tweet search results with singleflight
// deduplication and short-TTL caching of non-empty pages.
func TweetSearch(ctx context.Context, q types.Query, after string) (types.Timeline, error) {
	if entries == nil {
		return lookupTweetSearch(ctx, q, after)
	}
	key := searchPageKey(q, after)
	tl, err := cached(ctx, key, func() (types.Timeline, time.Duration, error) {
		tl, err := lookupTweetSearch(ctx, q, after)
		if err != nil {
			return tl, 0, err
		}
		if len(tl.Content) > 0 {
			return tl, searchPageCacheTime, nil
		}
		return tl, 0, nil
	})
	if err != nil {
		return types.Timeline{}, err
	}
	tl.Query = q
	return tl, nil
}
