// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tidwall/gjson"

	"github.com/perennialtech/nitter/internal/flags"
	"github.com/perennialtech/nitter/internal/types"
)

func TestProfileRSSEndpoints(t *testing.T) {
	// ordinary profile RSS retains reposts
	if e := userTweetsURL("123", "", true).cookie.endpoint; e != graphUserTweets {
		t.Errorf("RSS tweets endpoint %s", e)
	}
	if e := userTweetsURL("123", "", false).cookie.endpoint; e != graphUserTweetsOriginals {
		t.Errorf("tweets endpoint %s", e)
	}
	// with_replies RSS retains ordinary posts and replies
	if e := userRepliesURL("123", "", true).cookie.endpoint; e != graphUserTweetsAndReplies {
		t.Errorf("RSS replies endpoint %s", e)
	}
	if e := userRepliesURL("123", "", false).cookie.endpoint; e != graphUserReplies {
		t.Errorf("replies endpoint %s", e)
	}
}

func TestTemplatesRenderValidJSON(t *testing.T) {
	if strings.ContainsAny(gqlFeatures, " \n") || !gjson.Valid(gqlFeatures) {
		t.Fatalf("gqlFeatures: %s", gqlFeatures)
	}
	cursor := cursorParam(`a"},"x":{"`)
	for name, vars := range map[string]string{
		"tweetDetail":      format(tweetDetailVars, "1", cursor, "Relevance"),
		"userTweets":       format(userTweetsVars, "1", cursor, "20"),
		"userMedia":        format(userMediaVars, "1", "", "100"),
		"communityMembers": format(communityMembersVars, "1", "null"),
		"communityHashtag": format(communityHashtagsVars, "1", cursor, "tag"),
		"articleBatch":     format(articleBatchVars, `["1","2"]`),
		"restId":           format(restIdVars, "1", cursor, "20"),
	} {
		js := gjson.Parse(vars)
		if !gjson.Valid(vars) || len(js.Map()) == 0 || js.Get("x").Exists() {
			t.Errorf("%s: %s", name, vars)
		}
	}
	if got := format(tweetDetailVars, "1", "", "Recency"); !strings.HasPrefix(got, `{"focalTweetId":"1","referrer":"profile",`) ||
		!strings.Contains(got, `"rankingMode":"Recency"`) {
		t.Errorf("tweetDetailVars: %s", got)
	}
	if got := userURL(`a"b`).cookie.params[0].value; got != `{"screen_name":"a\"b","withGrokTranslatedBio":false}` {
		t.Errorf("user variables: %s", got)
	}
}

// onDemandFlags turns on-demand translation on for a test, with an empty
// cache.
func onDemandFlags(t *testing.T) {
	onDemand.Lock()
	clear(onDemand.cache)
	onDemand.Unlock()
	old, lang := flags.OnDemandTranslation, flags.OnDemandTranslationLang
	flags.OnDemandTranslation, flags.OnDemandTranslationLang = true, "en"
	t.Cleanup(func() { flags.OnDemandTranslation, flags.OnDemandTranslationLang = old, lang })
}

func TestTranslationCoalescing(t *testing.T) {
	onDemandFlags(t)
	gate := make(chan struct{})
	var posts atomic.Int32
	post := func(ctx context.Context, id, lang, contentType string) (string, error) {
		if id != "123" || lang != "en" || contentType != "POST" {
			t.Errorf("posted %s %s %s", id, lang, contentType)
		}
		posts.Add(1)
		<-gate
		return `data: {"result":{"text":"translated","source_language":"ja"}}`, nil
	}

	var wg sync.WaitGroup
	results := make([]bool, 2)
	for i := range results {
		wg.Go(func() {
			ok, err := translateOnDemand(context.Background(), "123", false, post)
			results[i] = ok && err == nil
		})
	}
	time.Sleep(30 * time.Millisecond)
	if posts.Load() != 1 {
		t.Fatalf("duplicate in-flight translations must be coalesced, got %d posts", posts.Load())
	}
	close(gate)
	wg.Wait()
	if !results[0] || !results[1] {
		t.Fatalf("results %v", results)
	}
	if ok, _ := translateOnDemand(context.Background(), "123", false, post); !ok || posts.Load() != 1 {
		t.Fatal("cached translation must avoid another upstream POST")
	}

	tweet := &types.Tweet{ID: 123, IsTranslatable: true}
	ApplyOnDemandCache(tweet)
	if tweet.Translation != "translated" || tweet.TransLang != "ja" || tweet.TransTarget != "en" || !tweet.Translatable {
		t.Fatalf("cached translation not applied: %+v", tweet)
	}
}

func TestTranslationFailureIsCoalescedAndNegativelyCached(t *testing.T) {
	onDemandFlags(t)
	gate := make(chan struct{})
	var posts atomic.Int32
	post := func(ctx context.Context, id, lang, contentType string) (string, error) {
		if contentType != "COMMUNITY_NOTE" {
			t.Errorf("content type %s", contentType)
		}
		posts.Add(1)
		<-gate
		return "", errors.New("mock upstream failure")
	}

	var wg sync.WaitGroup
	var succeeded atomic.Int32
	for range 2 {
		wg.Go(func() {
			if ok, _ := translateOnDemand(context.Background(), "456", true, post); ok {
				succeeded.Add(1)
			}
		})
	}
	time.Sleep(30 * time.Millisecond)
	close(gate)
	wg.Wait()
	if succeeded.Load() != 0 || posts.Load() != 1 {
		t.Fatalf("succeeded %d, posts %d", succeeded.Load(), posts.Load())
	}
	if ok, _ := translateOnDemand(context.Background(), "456", true, post); ok || posts.Load() != 1 {
		t.Fatal("failed translations must be briefly negative-cached")
	}
}

func TestParseGrokTranslation(t *testing.T) {
	g := parseGrokTranslation("data: {\"result\":{\"text\":\"a \",\"source_language\":\"ja\"}}\r\n" +
		"not json\n" +
		`{"result":{"text":"b","source_language":"fr","rich_text_entities":[{"ref":{"url":"https://t.co/x","expanded_url":"https://e.org"}}]}}`)
	if g.text != "a b" || g.lang != "ja" || len(g.links) != 1 || g.links[0].Expanded != "https://e.org" {
		t.Fatalf("%+v", g)
	}
}

func resetTrends() {
	trendsState.Lock()
	clear(trendsState.cache)
	trendsState.Unlock()
}

func TestTrendsShareRefreshesAndCacheFailures(t *testing.T) {
	resetTrends()
	gate := make(chan struct{})
	var requests atomic.Int32
	fetch := func(ctx context.Context, woeid int) []types.Trend {
		if woeid != 23424977 {
			t.Errorf("woeid %d", woeid)
		}
		requests.Add(1)
		<-gate
		return nil
	}

	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			if trends, err := getTrends(context.Background(), 23424977, 10, fetch); err != nil || len(trends) != 0 {
				t.Errorf("got %v, %v", trends, err)
			}
		})
	}
	time.Sleep(30 * time.Millisecond)
	if requests.Load() != 1 {
		t.Fatalf("concurrent requests must share a refresh, got %d", requests.Load())
	}
	close(gate)
	wg.Wait()
	if trends, _ := getTrends(context.Background(), 23424977, 10, fetch); len(trends) != 0 || requests.Load() != 1 {
		t.Fatal("a failed refresh must be cached")
	}
}

func TestTrendsServeStaleAndCap(t *testing.T) {
	resetTrends()
	saveSettings(t)
	trendsCacheTTL = 0
	woeid := 1
	fresh := []types.Trend{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	ok := true
	fetch := func(ctx context.Context, woeid int) []types.Trend {
		if ok {
			return fresh
		}
		return nil
	}
	if got, _ := getTrends(context.Background(), woeid, 2, fetch); len(got) != 2 || got[1].Name != "b" {
		t.Fatalf("got %v", got)
	}
	ok = false
	if got, _ := getTrends(context.Background(), woeid, -1, fetch); len(got) != 3 {
		t.Fatalf("a failed refresh must serve the previous trends, got %v", got)
	}
}

func TestFetchTrends(t *testing.T) {
	fakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "api.twitter.com" || r.URL.Path != "/1.1/trends/place.json" || r.URL.RawQuery != "id=1" ||
			r.Header.Get("Authorization") != bearerToken {
			t.Errorf("request %s %s %v", r.Host, r.URL, r.Header)
		}
		w.Write([]byte(`[{"trends":[{"name":"#Go","query":"%23Go+lang","tweet_volume":1200},
			{"name":"Nim","tweet_volume":null}]}]`))
	})
	got := fetchTrendsUncached(context.Background(), 1)
	want := []types.Trend{{Name: "#Go", SearchQuery: "#Go lang", TweetVolume: 1200}, {Name: "Nim", SearchQuery: "Nim", TweetVolume: -1}}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %+v", got)
	}
}

func TestAPIStats(t *testing.T) {
	resetAPIStats()
	recordCall("fx", "profile", "ok")
	recordCall("fx", "profile", "ok")
	recordCall("sessions", "UserTweets", "limited")
	if got := formatAPIStats(); got != "fx profile ok=2, sessions UserTweets limited=1" {
		t.Fatalf("got %q", got)
	}
	recordFxMinute(7)
	if got := formatAPIStats(); !strings.HasSuffix(got, "; fx peak/min=7") {
		t.Fatalf("got %q", got)
	}
	resetAPIStats()
}

func TestTweetSearchEmptyQuery(t *testing.T) {
	tl, err := GraphTweetSearch(context.Background(), types.Query{Kind: types.QueryTweets, Excludes: []string{}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !tl.Beginning {
		t.Fatalf("an empty search returns the first, empty page: %+v", tl)
	}
}

func TestTweetSearchDropsRepeatedLastPage(t *testing.T) {
	fakeUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "api.fxtwitter.com" || r.URL.Query().Get("feed") != "latest" || r.URL.Query().Get("cursor") != "same" {
			t.Errorf("request %s %s", r.Host, r.URL)
		}
		w.Write([]byte(`{"code":200,"cursor":{"bottom":"same"},"results":[` + status(t, `"raw_text":{"text":"a","display_text_range":[0,1],"facets":[]}`) + `]}`))
	})
	q := types.Query{Kind: types.QueryTweets, Text: "nitter"}
	tl, err := GraphTweetSearch(context.Background(), q, "same")
	if err != nil || len(tl.Content) != 0 || tl.Query.Text != "nitter" {
		t.Fatalf("got %+v, %v", tl, err)
	}
}
