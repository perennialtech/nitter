// SPDX-License-Identifier: AGPL-3.0-only

// Package auth holds the account and guest session pools: session selection,
// per-endpoint rate limit tracking, guest token rotation, and the health and
// debug reports.
package auth

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/perennialtech/nitter/internal/config"
	"github.com/perennialtech/nitter/internal/types"
)

const (
	hourInSeconds      = 60 * 60
	GuestMaxReqs       = 50
	GuestMaxAgeSeconds = 2 * hourInSeconds

	// GraphUserTweetsV2 keeps working for sessions that other endpoints
	// have rate limited.
	GraphUserTweetsV2 = "LE3eTyeqhBh2g-fX85O2eQ/UserWithProfileTweetsQueryV2"
)

// mu guards the pools, the settings below and every Session's bookkeeping.
var (
	mu          sync.Mutex
	sessionPool []*Session
	guestPool   []*Session
	// guestChanged is closed, and replaced, whenever the guest pool or a
	// guest session's availability changes.
	guestChanged    chan struct{}
	guestActivation *activation
	guestPoolSize   = 1
	// max requests at a time per session to avoid race conditions
	maxConcurrentReqs = 2
)

func logf(format string, args ...any) {
	log.Printf("[sessions] "+format, args...)
}

func now() int64 { return time.Now().Unix() }

// Init applies the session settings in cfg and, unless the instance uses
// guest tokens, loads the account sessions from the JSONL file at path.
func Init(cfg *config.Config, path string) error {
	mu.Lock()
	if cfg.MaxConcurrentReqs > 0 {
		maxConcurrentReqs = cfg.MaxConcurrentReqs
	}
	if cfg.GuestPoolSize > 0 {
		guestPoolSize = cfg.GuestPoolSize
	}
	mu.Unlock()

	if cfg.GuestAuth {
		return nil
	}
	if strings.HasSuffix(path, ".json") {
		return fmt.Errorf(".json is not supported, the file must be a valid JSONL file ending in .jsonl")
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		logf("%s not found, serving only what FxTwitter can", path)
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	logf("parsing JSONL account sessions file: %s", path)
	var sessions []*Session
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for n := 1; sc.Scan(); n++ {
		s, err := ParseSession(sc.Text())
		if err != nil {
			return fmt.Errorf("%s:%d: %w", path, n, err)
		}
		sessions = append(sessions, s)
	}
	if err := sc.Err(); err != nil {
		return err
	}

	mu.Lock()
	sessionPool = append(sessionPool, sessions...)
	mu.Unlock()
	logf("successfully added %d valid account sessions", len(sessions))
	return nil
}

func guestPoolFull() bool {
	return len(guestPool) >= guestPoolSize
}

func needsRotation(s *Session) bool {
	if s == nil || s.Kind != Guest {
		return false
	}
	return s.guestReqs >= GuestMaxReqs || now()-s.guestCreated >= GuestMaxAgeSeconds
}

func isLimited(s *Session, req Req) bool {
	if s == nil {
		return true
	}
	api := req.Endpoint(s)
	if s.limited && api != GraphUserTweetsV2 {
		if now()-s.limitedAt > hourInSeconds {
			s.limited = false
			logf("resetting limit: %s", s)
			return false
		}
		return true
	}
	if limit, ok := s.apis[api]; ok {
		return limit.Remaining <= 10 && limit.Reset > now()
	}
	return false
}

func isReady(s *Session, req Req) bool {
	return s != nil && s.pending <= maxConcurrentReqs && !isLimited(s, req)
}

func isGuestReady(s *Session, req Req) bool {
	return s != nil && s.pending < maxConcurrentReqs && !needsRotation(s) && !isLimited(s, req)
}

func notifyGuestPoolChange() {
	if guestChanged != nil {
		close(guestChanged)
		guestChanged = nil
	}
}

func waitForGuestPoolChange() <-chan struct{} {
	if guestChanged == nil {
		guestChanged = make(chan struct{})
	}
	return guestChanged
}

// AddGuestSession adds s to the guest pool.
func AddGuestSession(s *Session) {
	mu.Lock()
	defer mu.Unlock()
	addGuestSession(s)
}

func addGuestSession(s *Session) {
	guestPool = append(guestPool, s)
	notifyGuestPoolChange()
}

// RegisterGuestRequest counts a request made with a guest token towards its
// rotation.
func RegisterGuestRequest(s *Session) {
	if s == nil || s.Kind != Guest {
		return
	}
	mu.Lock()
	s.guestReqs++
	mu.Unlock()
}

// Invalidate removes s from its pool. The caller must not Release s
// afterwards.
func Invalidate(s *Session) {
	if s == nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	invalidate(s)
}

func invalidate(s *Session) {
	logf("invalidating: %s", s)
	// TODO: This isn't sufficient, but it works for now
	if s.Kind == Guest {
		if i := slices.Index(guestPool, s); i > -1 {
			guestPool = slices.Delete(guestPool, i, i+1)
			notifyGuestPoolChange()
		}
		return
	}
	if i := slices.Index(sessionPool, s); i > -1 {
		sessionPool = slices.Delete(sessionPool, i, i+1)
	}
}

// Release ends a request that acquired s.
func Release(s *Session) {
	if s == nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	s.pending--
	if s.Kind == Guest {
		notifyGuestPoolChange()
	}
}

func getGuestSession(req Req) *Session {
	for _, s := range guestPool {
		if isGuestReady(s, req) {
			s.pending++
			return s
		}
	}
	return nil
}

func hasBusyGuestSession(req Req) bool {
	for _, s := range guestPool {
		if s.pending > 0 && !needsRotation(s) && !isLimited(s, req) {
			return true
		}
	}
	return false
}

func findIdleStaleGuestSession(req Req) *Session {
	for _, s := range guestPool {
		if s.pending == 0 && (needsRotation(s) || isLimited(s, req)) {
			return s
		}
	}
	return nil
}

// GuestActivator activates a new guest token.
type GuestActivator func(ctx context.Context) (*Session, error)

type activation struct {
	done chan struct{}
	err  error
}

func (a *activation) failed() bool {
	select {
	case <-a.done:
		return a.err != nil
	default:
		return false
	}
}

// runActivation activates one guest token on behalf of every waiter. It is
// not tied to the request that started it, since other requests wait on it.
func runActivation(ctx context.Context, a *activation, activate GuestActivator) {
	fresh, err := activate(context.WithoutCancel(ctx))
	mu.Lock()
	defer mu.Unlock()
	if guestActivation == a {
		guestActivation = nil
	}
	if err != nil {
		a.err = err
		close(a.done)
		notifyGuestPoolChange()
		return
	}
	close(a.done)
	addGuestSession(fresh)
}

// GetReadyGuestSession acquires a guest session for req, activating new
// guest tokens while the pool has room and rotating out idle stale ones. At
// most one activation runs at a time; concurrent callers wait for it, or for
// a busy session to be released. It fails with types.ErrRateLimited when
// activation failed and no healthy guest session will become available.
func GetReadyGuestSession(ctx context.Context, req Req, activate GuestActivator) (*Session, error) {
	activationFailed := false
	for {
		mu.Lock()
		if s := getGuestSession(req); s != nil {
			mu.Unlock()
			return s, nil
		}

		changed := waitForGuestPoolChange()
		act := guestActivation
		if act == nil {
			if activationFailed {
				if !hasBusyGuestSession(req) {
					mu.Unlock()
					return nil, types.ErrRateLimited
				}
			} else {
				if stale := findIdleStaleGuestSession(req); stale != nil {
					invalidate(stale)
				}
				if !guestPoolFull() {
					act = &activation{done: make(chan struct{})}
					guestActivation = act
					go runActivation(ctx, act, activate)
				}
			}
		}
		mu.Unlock()

		select {
		case <-changed:
		case <-ctx.Done():
			return nil, ctx.Err()
		}

		mu.Lock()
		if s := getGuestSession(req); s != nil {
			mu.Unlock()
			return s, nil
		}
		if act != nil && act.failed() {
			activationFailed = true
			if !hasBusyGuestSession(req) {
				mu.Unlock()
				return nil, types.ErrRateLimited
			}
		}
		mu.Unlock()
	}
}

// GetSession acquires a random account session that is ready for req. It
// chooses among the ready sessions: drawing len(sessionPool) times from the
// whole pool misses a lone ready session about a third of the time.
func GetSession(req Req) (*Session, error) {
	mu.Lock()
	defer mu.Unlock()
	var ready []*Session
	for _, s := range sessionPool {
		if isReady(s, req) {
			ready = append(ready, s)
		}
	}
	if len(ready) == 0 {
		logf("no sessions available for API: %s", req.Cookie)
		return nil, types.ErrNoSessions
	}
	s := ready[rand.IntN(len(ready))]
	s.pending++
	return s, nil
}

// selectCookieSession picks the least busy ready cookie session. Grok
// translation requires a cookie session, and sampling the complete account
// pool can miss every cookie by chance, even when one is available.
func selectCookieSession(sessions []*Session, req Req) (*Session, error) {
	var best *Session
	for _, s := range sessions {
		if s.Kind != Cookie || s.pending >= maxConcurrentReqs || isLimited(s, req) {
			continue
		}
		if best == nil || s.pending < best.pending {
			best = s
		}
	}
	if best == nil {
		return nil, types.ErrNoSessions
	}
	best.pending++
	return best, nil
}

// GetCookieSession acquires the least busy cookie session ready for req.
func GetCookieSession(req Req) (*Session, error) {
	mu.Lock()
	defer mu.Unlock()
	return selectCookieSession(sessionPool, req)
}

// SetLimited marks s as rate limited on every endpoint but
// GraphUserTweetsV2, for an hour.
func SetLimited(s *Session, req Req) {
	mu.Lock()
	defer mu.Unlock()
	api := req.Endpoint(s)
	s.limited = true
	s.limitedAt = now()
	logf("rate limited by api: %s, reqs left: %d, %s", api, s.apis[api].Remaining, s)
}

// SetRateLimit records the rate limit Twitter reported for req's endpoint.
func SetRateLimit(s *Session, req Req, remaining int, reset int64, limit int) {
	mu.Lock()
	defer mu.Unlock()
	api := req.Endpoint(s)
	// concurrent responses can arrive out of order; keep the newest values
	if rl, ok := s.apis[api]; ok {
		if rl.Reset >= reset && rl.Remaining < remaining {
			return
		}
		if rl.Reset == reset && rl.Remaining >= remaining {
			rl.Remaining = remaining
			s.apis[api] = rl
			return
		}
	}
	if s.apis == nil {
		s.apis = map[string]RateLimit{}
	}
	s.apis[api] = RateLimit{Limit: limit, Remaining: remaining, Reset: reset}
}
