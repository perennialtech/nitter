// SPDX-License-Identifier: AGPL-3.0-only

// Package cache keeps users, lists, photo rails, broadcasts, spaces,
// communities and RSS feeds in memory, fetching them from the API on a miss.
package cache

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/klauspost/compress/s2"
	"github.com/maypok86/otter/v2"
	"golang.org/x/sync/singleflight"

	"github.com/perennialtech/nitter/internal/api"
	"github.com/perennialtech/nitter/internal/config"
	"github.com/perennialtech/nitter/internal/types"
)

const baseCacheTime = time.Hour

// entry is an s2-compressed JSON value and how long to keep it. Holding
// bytes rather than values lets the cache weigh entries by size, and gives
// every reader its own copy.
type entry struct {
	data []byte
	ttl  time.Duration
}

var (
	entries       *otter.Cache[string, entry]
	flights       singleflight.Group
	rssCacheTime  time.Duration
	listCacheTime time.Duration
)

// Init creates the cache, holding at most cfg.CacheMaxMB megabytes of keys
// and compressed values.
func Init(cfg *config.Config) {
	rssCacheTime = time.Duration(cfg.RssCacheTime) * time.Minute
	listCacheTime = time.Duration(cfg.ListCacheTime) * time.Minute
	entries = newCache(uint64(cfg.CacheMaxMB) << 20)
}

func newCache(maxBytes uint64) *otter.Cache[string, entry] {
	return otter.Must(&otter.Options[string, entry]{
		MaximumWeight: maxBytes,
		Weigher: func(key string, e entry) uint32 {
			return uint32(len(key) + len(e.data))
		},
		ExpiryCalculator: otter.ExpiryWritingFunc(func(e otter.Entry[string, entry]) time.Duration {
			return e.Value.ttl
		}),
	})
}

func encode(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		// Only cached domain types are encoded; they always marshal.
		panic(err)
	}
	return s2.Encode(nil, data)
}

func decode(data []byte, v any) {
	raw, err := s2.Decode(nil, data)
	if err == nil {
		err = json.Unmarshal(raw, v)
	}
	if err != nil {
		// The cache only holds what encode produced.
		panic(err)
	}
}

func load(key string, v any) bool {
	e, ok := entries.GetIfPresent(key)
	if ok {
		decode(e.data, v)
	}
	return ok
}

func store(key string, ttl time.Duration, v any) {
	entries.Set(key, entry{encode(v), ttl})
}

// flight is the outcome of a fetch shared by concurrent misses for a key.
type flight struct {
	data     []byte
	canceled bool
}

// cached returns the value at key. On a miss it calls fetch and caches the
// result for the duration fetch returns, unless that's zero. Concurrent
// misses for a key share one fetch, made with the context of the request
// that started it.
func cached[T any](ctx context.Context, key string, fetch func() (T, time.Duration, error)) (T, error) {
	var v T
	if load(key, &v) {
		return v, nil
	}
	for {
		res, err, _ := flights.Do(key, func() (any, error) {
			// The previous fetch for key may have finished since the miss.
			if e, ok := entries.GetIfPresent(key); ok {
				return flight{data: e.data}, nil
			}
			v, ttl, err := fetch()
			if err != nil {
				return flight{canceled: ctx.Err() != nil}, err
			}
			data := encode(v)
			if ttl > 0 {
				entries.Set(key, entry{data, ttl})
			}
			return flight{data: data}, nil
		})
		f := res.(flight)
		// When the request that started a fetch goes away, the fetch fails
		// for every request sharing it. Those still waiting fetch again.
		if f.canceled && ctx.Err() == nil {
			continue
		}
		if err != nil {
			return v, err
		}
		decode(f.data, &v)
		return v, nil
	}
}

func userKey(name string) string   { return "p:" + strings.ToLower(name) }
func userIDKey(name string) string { return "pid:" + strings.ToLower(name) }

// UserID returns the ID for username, or "suspended" if the account is.
func UserID(ctx context.Context, username string) (string, error) {
	return cached(ctx, userIDKey(username), func() (string, time.Duration, error) {
		u, err := api.GraphUser(ctx, username)
		switch {
		case err != nil:
			return "", 0, err
		case u.Suspended:
			return "suspended", 0, nil
		case u.Username != "":
			store(userKey(u.Username), baseCacheTime, u)
		}
		if u.ID == "" {
			return "", 0, nil
		}
		return u.ID, baseCacheTime, nil
	})
}

// User returns the cached profile for username. On a miss it's fetched if
// fetch is set, and the zero User is returned otherwise.
func User(ctx context.Context, username string, fetch bool) (types.User, error) {
	if !fetch {
		var u types.User
		load(userKey(username), &u)
		return u, nil
	}
	return cached(ctx, userKey(username), func() (types.User, time.Duration, error) {
		u, err := api.GraphUser(ctx, username)
		if err != nil || u.Username == "" {
			return u, 0, err
		}
		if u.ID != "" {
			store(userIDKey(u.Username), baseCacheTime, u.ID)
		}
		return u, baseCacheTime, nil
	})
}

func Username(ctx context.Context, userID string) (string, error) {
	return cached(ctx, "i:"+userID, func() (string, time.Duration, error) {
		u, err := api.GraphUserByID(ctx, userID)
		if err != nil || u.Username == "" {
			return "", 0, err
		}
		if u.ID != "" {
			store(userIDKey(u.Username), baseCacheTime, u.ID)
			store(userKey(u.Username), baseCacheTime, u)
		}
		return u.Username, baseCacheTime, nil
	})
}

// Broadcast returns a broadcast with a freshly fetched stream URL, which
// expires too quickly to cache.
func Broadcast(ctx context.Context, id string) (types.Broadcast, error) {
	if id == "" {
		return types.Broadcast{}, nil
	}
	b, err := cached(ctx, "bc:"+id, func() (types.Broadcast, time.Duration, error) {
		b, err := api.BroadcastInfo(ctx, id)
		if err != nil || b.ID == "" {
			return b, 0, err
		}
		return b, baseCacheTime, nil
	})
	if err != nil {
		return b, err
	}
	b.M3u8URL, err = api.BroadcastStream(ctx, b.MediaKey)
	return b, err
}

func AudioSpace(ctx context.Context, id string) (types.AudioSpace, error) {
	if id == "" {
		return types.AudioSpace{}, nil
	}
	s, err := cached(ctx, "sp:"+id, func() (types.AudioSpace, time.Duration, error) {
		s, err := api.AudioSpace(ctx, id)
		switch {
		case err != nil || s.ID == "":
			return s, 0, err
		case s.State == "RUNNING":
			return s, baseCacheTime / 6, nil
		}
		return s, baseCacheTime, nil
	})
	if err != nil {
		return s, err
	}
	s.M3u8URL, err = api.BroadcastStream(ctx, s.MediaKey)
	return s, err
}

func AccountInfo(ctx context.Context, username string, fetch bool) (types.AccountInfo, error) {
	var info types.AccountInfo
	if username == "" {
		return info, nil
	}
	key := "ai:" + strings.ToLower(username)
	if !fetch {
		load(key, &info)
		return info, nil
	}
	return cached(ctx, key, func() (types.AccountInfo, time.Duration, error) {
		info, err := api.AboutAccount(ctx, username)
		return info, 24 * baseCacheTime, err
	})
}

func PhotoRail(ctx context.Context, id string) (types.PhotoRail, error) {
	if id == "" {
		return types.PhotoRail{}, nil
	}
	return cached(ctx, "pr2:"+strings.ToLower(id), func() (types.PhotoRail, time.Duration, error) {
		rail, err := api.PhotoRail(ctx, id)
		return rail, 2 * baseCacheTime, err
	})
}

func Community(ctx context.Context, id string) (types.Community, error) {
	if id == "" {
		return types.Community{}, nil
	}
	return cached(ctx, "cm:"+id, func() (types.Community, time.Duration, error) {
		c, err := api.GraphCommunity(ctx, id)
		if err != nil || c.ID == "" {
			return c, 0, err
		}
		return c, listCacheTime, nil
	})
}

func CommunityModerators(ctx context.Context, id string) ([]types.User, error) {
	if id == "" {
		return nil, nil
	}
	return cached(ctx, "cmm:"+id, func() ([]types.User, time.Duration, error) {
		res, err := api.GraphCommunityModerators(ctx, id)
		return res.Content, listCacheTime, err
	})
}

// List looks a list up by id, or by owner username and slug when id is empty.
func List(ctx context.Context, username, slug, id string) (types.List, error) {
	if id != "" {
		return cached(ctx, "l:"+id, func() (types.List, time.Duration, error) {
			l, err := api.GraphList(ctx, id)
			if err != nil || l.ID == "" {
				return l, 0, err
			}
			return l, listCacheTime, nil
		})
	}
	l, err := api.GraphListBySlug(ctx, username, slug)
	if err == nil && l.ID != "" {
		store("l:"+l.ID, listCacheTime, l)
	}
	return l, err
}

// Rss returns the feed cached at key, or builds and caches it. A feed with
// a Cursor of two characters or fewer isn't cached.
func Rss(ctx context.Context, key string, build func() (types.Rss, error)) (types.Rss, error) {
	return cached(ctx, "rss:"+key, func() (types.Rss, time.Duration, error) {
		rss, err := build()
		if err != nil || len(rss.Cursor) <= 2 {
			return rss, 0, err
		}
		return rss, rssCacheTime, nil
	})
}
