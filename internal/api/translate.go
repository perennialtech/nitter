// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/gjson"

	"github.com/perennialtech/nitter/internal/flags"
	"github.com/perennialtech/nitter/internal/parser"
	"github.com/perennialtech/nitter/internal/types"
)

const (
	onDemandCacheTTL = 3600
	onDemandCacheMax = 500
)

type onDemandTrans struct {
	text, lang string
	at         int64
}

// onDemandCall is a translation that concurrent requests for one post or
// note share.
type onDemandCall struct {
	done chan struct{}
	ok   bool
}

// onDemand caches Grok translations by post ID, or "n" + note ID.
var onDemand struct {
	sync.Mutex
	cache   map[string]onDemandTrans
	pending map[string]*onDemandCall
}

type grokTranslation struct {
	text, lang string
	links      []parser.GrokLink
}

// parseGrokTranslation joins the streamed chunks of a Grok translation.
func parseGrokTranslation(body string) grokTranslation {
	var g grokTranslation
	for _, line := range strings.Split(body, "\n") {
		l := strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(l, "data:"); ok {
			l = strings.TrimSpace(rest)
		}
		if l == "" || l[0] != '{' || !gjson.Valid(l) {
			continue
		}
		res := gjson.Get(l, "result")
		g.text += getStr(res.Get("text"))
		if g.lang == "" {
			g.lang = getStr(res.Get("source_language"))
		}
		for _, e := range items(res.Get("rich_text_entities")) {
			r := e.Get("ref")
			g.links = append(g.links, parser.GrokLink{
				URL:      getStr(r.Get("url")),
				Expanded: getStrOr(r.Get("expandedUrl"), getStr(r.Get("expanded_url"))),
			})
		}
	}
	return g
}

func cacheOnDemand(key, text, lang string) {
	onDemand.Lock()
	defer onDemand.Unlock()
	now := time.Now().Unix()
	if onDemand.cache == nil {
		onDemand.cache = map[string]onDemandTrans{}
	}
	if len(onDemand.cache) >= onDemandCacheMax {
		for k, v := range onDemand.cache {
			if now-v.at > onDemandCacheTTL {
				delete(onDemand.cache, k)
			}
		}
		if len(onDemand.cache) >= onDemandCacheMax {
			clear(onDemand.cache)
		}
	}
	// long-lived; don't pin the response body
	onDemand.cache[key] = onDemandTrans{text: strings.Clone(text), lang: strings.Clone(lang), at: now}
}

func cachedOnDemand(key string) (onDemandTrans, bool) {
	onDemand.Lock()
	defer onDemand.Unlock()
	return cachedOnDemandLocked(key)
}

func cachedOnDemandLocked(key string) (onDemandTrans, bool) {
	found, ok := onDemand.cache[key]
	if !ok {
		return found, false
	}
	ttl := int64(onDemandCacheTTL)
	if found.text == "" {
		ttl = translationRetrySeconds
	}
	if time.Now().Unix()-found.at > ttl {
		delete(onDemand.cache, key)
		return found, false
	}
	return found, true
}

// ApplyOnDemandNoteCache fills in a cached on-demand translation of note.
func ApplyOnDemandNoteCache(note *types.BirdwatchNote) {
	if !flags.OnDemandTranslation || !note.IsTranslatable || note.ID == "" {
		return
	}
	cached, ok := cachedOnDemand("n" + note.ID)
	if !ok {
		return
	}
	note.Translation = cached.text
	note.TransLang = cached.lang
	note.TransTarget = flags.OnDemandTranslationLang
	note.Translatable = true
}

// ApplyOnDemandCache fills in cached on-demand translations of tweet, its
// community note and its quote's community note.
func ApplyOnDemandCache(tweet *types.Tweet) {
	if !flags.OnDemandTranslation || tweet == nil {
		return
	}
	lang := flags.OnDemandTranslationLang

	if tweet.IsTranslatable && tweet.Translation == "" {
		if cached, ok := cachedOnDemand(strconv.FormatInt(tweet.ID, 10)); ok {
			tweet.Translation = cached.text
			tweet.TransLang = cached.lang
			tweet.TransTarget = lang
			tweet.Translatable = true
		}
	}

	applyNote := func(t *types.Tweet) {
		if !t.NoteIsTranslatable || t.NoteID == "" || t.NoteTranslation != "" {
			return
		}
		if cached, ok := cachedOnDemand("n" + t.NoteID); ok {
			t.NoteTranslation = cached.text
			t.NoteTransLang = cached.lang
			t.NoteTransTarget = lang
			t.NoteTranslatable = true
		}
	}
	applyNote(tweet)
	if tweet.Quote != nil {
		applyNote(tweet.Quote)
	}
}

// TranslateTweet fills in Twitter's own translations of tweet, its
// community note and its quote when they are translatable, and cached
// on-demand ones. Failures are logged; the tweet is then shown untranslated.
func TranslateTweet(ctx context.Context, tweet *types.Tweet) {
	if !flags.EnableTranslation || tweet == nil {
		return
	}

	var quoted *types.Tweet
	if q := tweet.Quote; q != nil &&
		(q.Translatable || q.NoteTranslatable || (flags.OnDemandTranslation && q.NoteIsTranslatable)) {
		quoted = q
	}

	onDemandPost := flags.OnDemandTranslation && tweet.IsTranslatable
	onDemandNote := flags.OnDemandTranslation && tweet.NoteIsTranslatable
	if !(tweet.Translatable || tweet.NoteTranslatable || onDemandPost || onDemandNote || quoted != nil) {
		return
	}
	tweet.ShowTrans = true
	if quoted != nil {
		quoted.ShowTrans = true
	}

	ApplyOnDemandCache(tweet)

	missing := func(t *types.Tweet, onDemand bool) bool {
		return t != nil && (((t.Translatable || onDemand) && t.Translation == "") ||
			(t.NoteTranslatable && t.NoteTranslation == ""))
	}
	if !(missing(tweet, onDemandPost) || missing(quoted, false)) {
		return
	}

	trans, err := TweetTranslation(ctx, strconv.FormatInt(tweet.ID, 10))
	if err != nil {
		log.Printf("Translation fetch failed for %d: %v", tweet.ID, err)
		return
	}
	if trans == nil {
		return
	}
	if trans.Translation != "" {
		tweet.Translation = trans.Translation
		tweet.TransLang = trans.TransLang
		tweet.Translatable = true
	}
	if trans.NoteTranslation != "" {
		tweet.NoteTranslation = trans.NoteTranslation
		tweet.NoteTransLang = trans.NoteTransLang
	}
	if tq := trans.Quote; quoted != nil && tq != nil {
		if quoted.Translation == "" && tq.Translation != "" {
			quoted.Translation = tq.Translation
			quoted.TransLang = tq.TransLang
		}
		if quoted.NoteTranslation == "" && tq.NoteTranslation != "" {
			quoted.NoteTranslation = tq.NoteTranslation
			quoted.NoteTransLang = tq.NoteTransLang
		}
	}
}

// translationPoster posts a translation request; PostGrokTranslation in
// production.
type translationPoster func(ctx context.Context, id, lang, contentType string) (string, error)

func translateUncached(ctx context.Context, id, key string, note bool, post translationPoster) bool {
	contentType := "POST"
	if note {
		contentType = "COMMUNITY_NOTE"
	}
	ok := false
	body, err := post(ctx, id, flags.OnDemandTranslationLang, contentType)
	if err != nil {
		log.Printf("[translation] on-demand translation failed for %s: %v", id, err)
	} else {
		parsed := parseGrokTranslation(body)
		ok = parsed.text != ""
		if ok {
			cacheOnDemand(key, parser.ExpandGrokTranslation(parsed.text, parsed.links), parsed.lang)
		} else if body != "" {
			log.Printf("[translation] no translated text in grok response for %s: %s", id, body[:min(len(body), 300)])
		}
	}
	if !ok {
		// Cache failures briefly too, to avoid repeated public requests to an
		// unavailable or rate-limited upstream endpoint.
		cacheOnDemand(key, "", "")
	}
	return ok
}

// TranslateOnDemand asks Grok to translate the post id, or the community
// note id when note is set, into onDemandTranslationLang, and caches the
// result for ApplyOnDemandCache. It reports whether a translation is now
// cached. Concurrent requests for the same post or note share one request.
func TranslateOnDemand(ctx context.Context, id string, note bool) (bool, error) {
	return translateOnDemand(ctx, id, note, PostGrokTranslation)
}

func translateOnDemand(ctx context.Context, id string, note bool, post translationPoster) (bool, error) {
	if !flags.OnDemandTranslation || id == "" {
		return false, nil
	}
	key := id
	if note {
		key = "n" + id
	}

	onDemand.Lock()
	if cached, ok := cachedOnDemandLocked(key); ok {
		onDemand.Unlock()
		return cached.text != "", nil
	}
	call, ok := onDemand.pending[key]
	if !ok {
		call = &onDemandCall{done: make(chan struct{})}
		if onDemand.pending == nil {
			onDemand.pending = map[string]*onDemandCall{}
		}
		onDemand.pending[key] = call
		go func() {
			ok := translateUncached(context.WithoutCancel(ctx), id, key, note, post)
			onDemand.Lock()
			delete(onDemand.pending, key)
			call.ok = ok
			close(call.done)
			onDemand.Unlock()
		}()
	}
	onDemand.Unlock()

	select {
	case <-call.done:
		return call.ok, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}
