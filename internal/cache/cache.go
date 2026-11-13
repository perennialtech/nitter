// SPDX-License-Identifier: AGPL-3.0-only

// Package cache stores users, lists, photo rails, broadcasts, spaces,
// communities and RSS feeds in Redis, fetching them from the API on a miss.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/s2"
	"github.com/redis/go-redis/v9"

	"github.com/perennialtech/nitter/internal/api"
	"github.com/perennialtech/nitter/internal/config"
	"github.com/perennialtech/nitter/internal/types"
)

const baseCacheTime = time.Hour

var (
	rdb           *redis.Client
	rssCacheTime  time.Duration
	listCacheTime time.Duration
)

// Init connects to Redis and checks the connection.
func Init(ctx context.Context, cfg *config.Config) error {
	rssCacheTime = time.Duration(cfg.RssCacheTime) * time.Minute
	listCacheTime = time.Duration(cfg.ListCacheTime) * time.Minute
	rdb = redis.NewClient(&redis.Options{
		Addr:         cfg.RedisHost + ":" + strconv.Itoa(cfg.RedisPort),
		Password:     cfg.RedisPassword,
		PoolSize:     cfg.RedisMaxConns,
		MinIdleConns: cfg.RedisConns,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		return err
	}
	// Keep the user ID buckets in Redis's compact hash encoding.
	if err := rdb.ConfigSet(ctx, "hash-max-ziplist-entries", "1000").Err(); err != nil {
		log.Printf("[cache] can't set hash-max-ziplist-entries: %v", err)
	}
	return nil
}

func encode(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		// Only cached domain types are encoded; they always marshal.
		panic(err)
	}
	return s2.Encode(nil, data)
}

// decode reports whether data held a valid value. A bad entry is logged
// and treated as a miss.
func decode(data []byte, v any) bool {
	raw, err := s2.Decode(nil, data)
	if err == nil {
		err = json.Unmarshal(raw, v)
	}
	if err != nil {
		log.Printf("[cache] decoding %T failed: %v", v, err)
		return false
	}
	return true
}

// get returns the cached bytes for key, or nil on a miss.
func get(ctx context.Context, key string) ([]byte, error) {
	data, err := rdb.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	return data, err
}

func load(ctx context.Context, key string, v any) (bool, error) {
	data, err := get(ctx, key)
	if err != nil || data == nil {
		return false, err
	}
	return decode(data, v), nil
}

func store(ctx context.Context, key string, ttl time.Duration, v any) error {
	return rdb.Set(ctx, key, encode(v), ttl).Err()
}

// User IDs are grouped into hash buckets, which Redis stores compactly up
// to hash-max-ziplist-entries (set in Init).
func uidKey(name string) string {
	h := fnv.New64a()
	h.Write([]byte(name))
	return "pid:" + strconv.FormatUint(h.Sum64()%1_000_000, 10)
}

func userKey(name string) string { return "p:" + name }

func cacheUserID(ctx context.Context, username, id string) error {
	if username == "" || id == "" {
		return nil
	}
	name := strings.ToLower(username)
	key := uidKey(name)
	_, err := rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.HSet(ctx, key, name, id)
		p.Expire(ctx, key, baseCacheTime)
		return nil
	})
	return err
}

func CacheList(ctx context.Context, l types.List) error {
	return store(ctx, "l:"+l.ID, listCacheTime, l)
}

func CachePhotoRail(ctx context.Context, rail types.PhotoRail, name string) error {
	return store(ctx, "pr2:"+strings.ToLower(name), 2*baseCacheTime, rail)
}

func CacheUser(ctx context.Context, u types.User) error {
	if u.Username == "" {
		return nil
	}
	name := strings.ToLower(u.Username)
	if err := cacheUserID(ctx, name, u.ID); err != nil {
		return err
	}
	return store(ctx, userKey(name), baseCacheTime, u)
}

// UserID returns the ID for username, or "suspended" if the account is.
func UserID(ctx context.Context, username string) (string, error) {
	name := strings.ToLower(username)
	id, err := rdb.HGet(ctx, uidKey(name), name).Result()
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, redis.Nil) {
		return "", err
	}
	u, err := api.GraphUser(ctx, username)
	if err != nil {
		return "", err
	}
	if u.Suspended {
		return "suspended", nil
	}
	return u.ID, CacheUser(ctx, u)
}

// User returns the cached profile for username. On a miss it's fetched if
// fetch is set, and the zero User is returned otherwise.
func User(ctx context.Context, username string, fetch bool) (types.User, error) {
	var u types.User
	if ok, err := load(ctx, userKey(strings.ToLower(username)), &u); ok || err != nil {
		return u, err
	}
	if !fetch {
		return types.User{}, nil
	}
	u, err := api.GraphUser(ctx, username)
	if err != nil {
		return u, err
	}
	return u, CacheUser(ctx, u)
}

func Username(ctx context.Context, userID string) (string, error) {
	key := "i:" + userID
	data, err := get(ctx, key)
	if err != nil || data != nil {
		return string(data), err
	}
	u, err := api.GraphUserByID(ctx, userID)
	if err != nil || u.Username == "" {
		return u.Username, err
	}
	if err := rdb.Set(ctx, key, u.Username, baseCacheTime).Err(); err != nil {
		return "", err
	}
	if u.ID != "" {
		if err := CacheUser(ctx, u); err != nil {
			return "", err
		}
	}
	return u.Username, nil
}

// Broadcast returns a broadcast with a freshly fetched stream URL, which
// expires too quickly to cache.
func Broadcast(ctx context.Context, id string) (types.Broadcast, error) {
	var b types.Broadcast
	if id == "" {
		return b, nil
	}
	ok, err := load(ctx, "bc:"+id, &b)
	if err != nil {
		return b, err
	}
	if !ok {
		if b, err = api.BroadcastInfo(ctx, id); err != nil {
			return b, err
		}
		if b.ID != "" {
			if err := store(ctx, "bc:"+b.ID, baseCacheTime, b); err != nil {
				return b, err
			}
		}
	}
	b.M3u8URL, err = api.BroadcastStream(ctx, b.MediaKey)
	return b, err
}

func AudioSpace(ctx context.Context, id string) (types.AudioSpace, error) {
	var s types.AudioSpace
	if id == "" {
		return s, nil
	}
	ok, err := load(ctx, "sp:"+id, &s)
	if err != nil {
		return s, err
	}
	if !ok {
		if s, err = api.AudioSpace(ctx, id); err != nil {
			return s, err
		}
		if s.ID != "" {
			ttl := baseCacheTime
			if s.State == "RUNNING" {
				ttl = baseCacheTime / 6
			}
			if err := store(ctx, "sp:"+s.ID, ttl, s); err != nil {
				return s, err
			}
		}
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
	if ok, err := load(ctx, key, &info); ok || err != nil || !fetch {
		return info, err
	}
	info, err := api.AboutAccount(ctx, username)
	if err != nil {
		return info, err
	}
	return info, store(ctx, key, 24*baseCacheTime, info)
}

func PhotoRail(ctx context.Context, id string) (types.PhotoRail, error) {
	var rail types.PhotoRail
	if id == "" {
		return rail, nil
	}
	if ok, err := load(ctx, "pr2:"+strings.ToLower(id), &rail); ok || err != nil {
		return rail, err
	}
	rail, err := api.PhotoRail(ctx, id)
	if err != nil {
		return rail, err
	}
	return rail, CachePhotoRail(ctx, rail, id)
}

func Community(ctx context.Context, id string) (types.Community, error) {
	var c types.Community
	if id == "" {
		return c, nil
	}
	if ok, err := load(ctx, "cm:"+id, &c); ok || err != nil {
		return c, err
	}
	c, err := api.GraphCommunity(ctx, id)
	if err != nil || c.ID == "" {
		return c, err
	}
	return c, store(ctx, "cm:"+c.ID, listCacheTime, c)
}

func CommunityModerators(ctx context.Context, id string) ([]types.User, error) {
	var mods []types.User
	if id == "" {
		return mods, nil
	}
	if ok, err := load(ctx, "cmm:"+id, &mods); ok || err != nil {
		return mods, err
	}
	res, err := api.GraphCommunityModerators(ctx, id)
	if err != nil {
		return nil, err
	}
	return res.Content, store(ctx, "cmm:"+id, listCacheTime, res.Content)
}

// List looks a list up by id, or by owner username and slug when id is empty.
func List(ctx context.Context, username, slug, id string) (types.List, error) {
	var l types.List
	if id != "" {
		if ok, err := load(ctx, "l:"+id, &l); ok || err != nil {
			return l, err
		}
	}
	var err error
	if id != "" {
		l, err = api.GraphList(ctx, id)
	} else {
		l, err = api.GraphListBySlug(ctx, username, slug)
	}
	if err != nil {
		return l, err
	}
	return l, CacheList(ctx, l)
}

func CacheRss(ctx context.Context, query string, rss types.Rss) error {
	key := "rss:" + query
	_, err := rdb.Pipelined(ctx, func(p redis.Pipeliner) error {
		p.HSet(ctx, key, "min", rss.Cursor)
		if rss.Cursor != "suspended" {
			p.HSet(ctx, key, "rss", s2.Encode(nil, []byte(rss.Feed)))
		}
		p.Expire(ctx, key, rssCacheTime)
		return nil
	})
	return err
}

// Rss returns a cached feed. An empty Cursor means a miss.
func Rss(ctx context.Context, key string) (types.Rss, error) {
	var rss types.Rss
	k := "rss:" + key
	vals, err := rdb.HMGet(ctx, k, "min", "rss").Result()
	if err != nil {
		return rss, err
	}
	cursor, _ := vals[0].(string)
	if len(cursor) <= 2 {
		return rss, nil
	}
	rss.Cursor = cursor
	if feed, _ := vals[1].(string); cursor != "suspended" && feed != "" {
		raw, err := s2.Decode(nil, []byte(feed))
		if err != nil {
			log.Printf("[cache] decoding RSS %s failed: %v", key, err)
			return types.Rss{}, nil
		}
		rss.Feed = string(raw)
	}
	return rss, nil
}
