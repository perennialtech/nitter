// SPDX-License-Identifier: AGPL-3.0-only

package api

// The call stats count upstream API calls by backend, endpoint and outcome,
// and log the totals periodically, so the logs show which calls FxTwitter
// serves and which still reach the session pool.

import (
	"cmp"
	"context"
	"log"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const statsInterval = 5 * time.Minute

var stats struct {
	sync.Mutex
	calls           map[string]int
	fxPeakPerMinute int
}

func recordCall(backend, endpoint, outcome string) {
	stats.Lock()
	defer stats.Unlock()
	if stats.calls == nil {
		stats.calls = map[string]int{}
	}
	stats.calls[backend+" "+endpoint+" "+outcome]++
}

func recordFxMinute(reqs int) {
	stats.Lock()
	stats.fxPeakPerMinute = max(stats.fxPeakPerMinute, reqs)
	stats.Unlock()
}

// formatAPIStats lists the counts, largest first.
func formatAPIStats() string {
	stats.Lock()
	defer stats.Unlock()
	return formatAPIStatsLocked()
}

func formatAPIStatsLocked() string {
	keys := make([]string, 0, len(stats.calls))
	for k := range stats.calls {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		if c := cmp.Compare(stats.calls[b], stats.calls[a]); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + strconv.Itoa(stats.calls[k])
	}
	result := strings.Join(parts, ", ")
	if stats.fxPeakPerMinute > 0 {
		result += "; fx peak/min=" + strconv.Itoa(stats.fxPeakPerMinute)
	}
	return result
}

func resetAPIStats() {
	stats.Lock()
	defer stats.Unlock()
	clear(stats.calls)
	stats.fxPeakPerMinute = 0
}

// StartStatsLogger logs and resets the call stats every five minutes until
// ctx is done.
func StartStatsLogger(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(statsInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			stats.Lock()
			if len(stats.calls) > 0 {
				log.Printf("[api] last %dm: %s", int(statsInterval.Minutes()), formatAPIStatsLocked())
				clear(stats.calls)
				stats.fxPeakPerMinute = 0
			}
			stats.Unlock()
		}
	}()
}
