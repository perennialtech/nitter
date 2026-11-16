// SPDX-License-Identifier: AGPL-3.0-only

package cache

import (
	"context"
	"time"

	"github.com/perennialtech/nitter/internal/api"
	"github.com/perennialtech/nitter/internal/types"
)

// Stories caches only the first page, shared by the trends tab and the story
// index. A visitor's sort order is applied after reading from the cache.
func Stories(ctx context.Context, cursor string, ttl time.Duration) (types.StoryPage, error) {
	if cursor != "" {
		return api.GraphStories(ctx, cursor)
	}
	return cached(ctx, "stories:first", func() (types.StoryPage, time.Duration, error) {
		page, err := api.GraphStories(ctx, "")
		if len(page.Stories) == 0 {
			ttl = min(ttl, time.Minute)
		}
		return page, ttl, err
	})
}

func StoryDetail(ctx context.Context, id string, ttl time.Duration) (types.StoryDetail, error) {
	return cached(ctx, "story:"+id, func() (types.StoryDetail, time.Duration, error) {
		detail, err := api.GraphStoryDetail(ctx, id)
		if detail.Title == "" && len(detail.Timelines) == 0 {
			ttl = 0
		}
		return detail, ttl, err
	})
}
