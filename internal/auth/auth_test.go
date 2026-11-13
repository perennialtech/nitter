// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/perennialtech/nitter/internal/config"
	"github.com/perennialtech/nitter/internal/types"
)

var request = Req{Cookie: "UserTweets"}

// setup clears the pools and applies the given settings for one test.
func setup(t *testing.T, poolSize, maxReqs int) {
	t.Helper()
	reset := func() {
		mu.Lock()
		defer mu.Unlock()
		sessionPool, guestPool = nil, nil
		guestChanged, guestActivation = nil, nil
		guestPoolSize, maxConcurrentReqs = 1, 2
	}
	reset()
	mu.Lock()
	guestPoolSize, maxConcurrentReqs = poolSize, maxReqs
	mu.Unlock()
	t.Cleanup(reset)
}

func newGuest(token string, created int64, pending int) *Session {
	return &Session{Kind: Guest, GuestToken: token, guestCreated: created, pending: pending}
}

func pendingOf(s *Session) int {
	mu.Lock()
	defer mu.Unlock()
	return s.pending
}

func guestTotal(t *testing.T) int {
	t.Helper()
	var health struct {
		Sessions struct {
			Guest struct{ Total int }
		}
	}
	if err := json.Unmarshal(HealthJSON(), &health); err != nil {
		t.Fatal(err)
	}
	return health.Sessions.Guest.Total
}

type acquisition struct {
	s   *Session
	err error
}

// acquire starts GetReadyGuestSession in the background.
func acquire(t *testing.T, activate GuestActivator) chan acquisition {
	ch := make(chan acquisition, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s, err := GetReadyGuestSession(context.Background(), request, activate)
		ch <- acquisition{s, err}
	}()
	t.Cleanup(func() {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("a waiter is still waiting for a guest session")
		}
	})
	return ch
}

// gate is an activation the test completes or fails by hand.
type gate struct {
	calls atomic.Int32
	ch    chan acquisition
}

func newGate() *gate { return &gate{ch: make(chan acquisition, 1)} }

func (g *gate) activate(ctx context.Context) (*Session, error) {
	g.calls.Add(1)
	r := <-g.ch
	return r.s, r.err
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// settle gives background goroutines time to reach their next wait.
func settle() { time.Sleep(30 * time.Millisecond) }

func await(t *testing.T, ch chan acquisition) acquisition {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(2 * time.Second):
		t.Fatal("acquisition did not finish")
		return acquisition{}
	}
}

// inOrder yields the results of chs as they finish. Waiters are woken in no
// particular order, and a waiter may only finish once an earlier one
// releases its session.
func inOrder(t *testing.T, chs ...chan acquisition) func(func(acquisition) bool) {
	return func(yield func(acquisition) bool) {
		for range chs {
			var r acquisition
			select {
			case r = <-chs[0]:
			case r = <-chs[1]:
			case <-time.After(2 * time.Second):
				t.Fatal("acquisition did not finish")
			}
			if !yield(r) {
				return
			}
		}
	}
}

func pending(ch chan acquisition) bool {
	select {
	case r := <-ch:
		// put it back for the caller
		ch <- r
		return false
	default:
		return true
	}
}

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	os.Exit(m.Run())
}

func TestConcurrentGuestActivationIsShared(t *testing.T) {
	setup(t, 1, 2)
	g := newGate()
	first, second := acquire(t, g.activate), acquire(t, g.activate)
	eventually(t, "activation", func() bool { return g.calls.Load() == 1 })
	settle()
	if n := g.calls.Load(); n != 1 {
		t.Fatalf("concurrent calls must share one activation, got %d", n)
	}

	guest := NewGuestSession("shared")
	g.ch <- acquisition{s: guest}
	a, b := await(t, first), await(t, second)
	if a.s != guest || b.s != guest {
		t.Fatalf("both callers must get the activated session: %v %v", a, b)
	}
	if p := pendingOf(guest); p != 2 {
		t.Fatalf("pending = %d, want 2", p)
	}
	if n := guestTotal(t); n != 1 {
		t.Fatalf("guest total = %d, want 1", n)
	}
	Release(a.s)
	Release(b.s)
}

func TestWaitingCallersFillFreeGuestPoolSlots(t *testing.T) {
	setup(t, 2, 1)
	g := newGate()
	first, second := acquire(t, g.activate), acquire(t, g.activate)
	eventually(t, "activation", func() bool { return g.calls.Load() == 1 })
	settle()
	if n := g.calls.Load(); n != 1 {
		t.Fatalf("activations must remain serialized, got %d", n)
	}

	a, b := NewGuestSession("first"), NewGuestSession("second")
	g.ch <- acquisition{s: a}
	got := []*Session{}
	select {
	case r := <-first:
		got = append(got, r.s)
	case r := <-second:
		got = append(got, r.s)
	case <-time.After(2 * time.Second):
		t.Fatal("no caller got the first session")
	}
	if got[0] != a {
		t.Fatalf("got %v, want the first session", got[0])
	}
	eventually(t, "a second activation", func() bool { return g.calls.Load() == 2 })
	g.ch <- acquisition{s: b}
	select {
	case r := <-first:
		got = append(got, r.s)
	case r := <-second:
		got = append(got, r.s)
	case <-time.After(2 * time.Second):
		t.Fatal("no caller got the second session")
	}
	if got[1] != b {
		t.Fatalf("got %v, want the second session", got[1])
	}
}

func TestBusyGuestReleasedDuringFullPoolWaitIsReused(t *testing.T) {
	setup(t, 1, 1)
	busy := newGuest("busy", now(), 1)
	AddGuestSession(busy)
	unexpected := func(context.Context) (*Session, error) {
		t.Error("a healthy busy session must be reused")
		return nil, errors.New("unexpected activation")
	}
	ch := acquire(t, unexpected)
	time.Sleep(350 * time.Millisecond)
	if !pending(ch) {
		t.Fatal("acquisition finished while the only session was busy")
	}
	Release(busy)
	r := await(t, ch)
	if r.s != busy || pendingOf(busy) != 1 {
		t.Fatalf("got %v (pending %d), want the released session", r.s, pendingOf(busy))
	}
}

func TestReleasedGuestUsableWhileActivationFails(t *testing.T) {
	setup(t, 2, 1)
	existing := newGuest("existing", now(), 1)
	AddGuestSession(existing)
	g := newGate()
	first, second := acquire(t, g.activate), acquire(t, g.activate)
	eventually(t, "activation", func() bool { return g.calls.Load() == 1 })
	settle()

	Release(existing)
	settle()
	acquiredBefore := !pending(first) || !pending(second)
	g.ch <- acquisition{err: errors.New("activation unavailable")}

	successful := 0
	for r := range inOrder(t, first, second) {
		if r.err != nil {
			if !errors.Is(r.err, types.ErrRateLimited) {
				t.Fatalf("unexpected error: %v", r.err)
			}
			continue
		}
		if r.s != existing {
			t.Fatalf("got %v, want the existing session", r.s)
		}
		successful++
		Release(r.s)
	}
	if !acquiredBefore || successful != 2 || g.calls.Load() != 1 {
		t.Fatalf("all waiters must reuse released sessions despite a failing activation "+
			"(acquired before failure: %v, successful: %d, activations: %d)",
			acquiredBefore, successful, g.calls.Load())
	}
}

func TestFailedActivationWaitsForBusyGuest(t *testing.T) {
	setup(t, 2, 1)
	existing := newGuest("existing", now(), 1)
	AddGuestSession(existing)
	g := newGate()
	first, second := acquire(t, g.activate), acquire(t, g.activate)
	eventually(t, "activation", func() bool { return g.calls.Load() == 1 })
	g.ch <- acquisition{err: errors.New("guest activation rate limited")}
	settle()
	remainedQueued := pending(first) && pending(second)

	Release(existing)
	acquired := 0
	for r := range inOrder(t, first, second) {
		if r.err != nil {
			continue
		}
		if r.s != existing {
			t.Fatalf("got %v, want the existing session", r.s)
		}
		acquired++
		Release(r.s)
	}
	if !remainedQueued || acquired != 2 || g.calls.Load() != 1 {
		t.Fatalf("a failed activation must not reject or retry healthy busy guest waiters "+
			"(queued: %v, acquired: %d, activations: %d)", remainedQueued, acquired, g.calls.Load())
	}
}

func TestIdleStaleGuestReplaceableBehindBusyStaleGuest(t *testing.T) {
	setup(t, 2, 2)
	expiredAt := now() - GuestMaxAgeSeconds - 1
	busy := newGuest("busy", expiredAt, 1)
	idle := newGuest("idle", expiredAt, 0)
	fresh := NewGuestSession("fresh")
	AddGuestSession(busy)
	AddGuestSession(idle)

	s, err := GetReadyGuestSession(context.Background(), request,
		func(context.Context) (*Session, error) { return fresh, nil })
	if err != nil || s != fresh {
		t.Fatalf("got %v, %v; want the fresh session", s, err)
	}
	if pendingOf(busy) != 1 {
		t.Fatal("the busy session must be left alone")
	}
	if n := guestTotal(t); n != 2 {
		t.Fatalf("guest total = %d, want 2", n)
	}
}

func TestFailedActivationPropagatesAndRecovers(t *testing.T) {
	setup(t, 1, 2)
	g := newGate()
	ctx := context.Background()
	first, second := acquire(t, g.activate), acquire(t, g.activate)
	eventually(t, "activation", func() bool { return g.calls.Load() == 1 })
	settle()
	g.ch <- acquisition{err: errors.New("upstream unavailable")}
	for _, ch := range []chan acquisition{first, second} {
		if r := await(t, ch); !errors.Is(r.err, types.ErrRateLimited) {
			t.Fatalf("activation failures must propagate to all waiters, got %v", r)
		}
	}

	guest := NewGuestSession("recovered")
	s, err := GetReadyGuestSession(ctx, request, func(context.Context) (*Session, error) { return guest, nil })
	if err != nil || s != guest {
		t.Fatalf("got %v, %v; want the recovered session", s, err)
	}
}

func TestGuestWaitHonoursContext(t *testing.T) {
	setup(t, 1, 1)
	AddGuestSession(newGuest("busy", now(), 1))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := GetReadyGuestSession(ctx, request, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want the context's error", err)
	}
}

func TestGuestTokensAbsentFromDiagnostics(t *testing.T) {
	setup(t, 1, 2)
	guest := NewGuestSession("sensitive-guest-token")
	AddGuestSession(guest)
	if guest.String() != "guest" {
		t.Fatalf("String() = %q", guest.String())
	}
	debug := string(DebugJSON())
	if !strings.Contains(debug, "guest-0") || strings.Contains(debug, "sensitive-guest-token") {
		t.Fatalf("debug JSON: %s", debug)
	}
}

func TestBusyGuestDoesNotHideAvailableGuest(t *testing.T) {
	setup(t, 1, 2)
	busy := newGuest("busy", now(), 2)
	ready := newGuest("ready", now(), 0)
	AddGuestSession(busy)
	AddGuestSession(ready)
	mu.Lock()
	selected := getGuestSession(request)
	mu.Unlock()
	if selected != ready || pendingOf(ready) != 1 {
		t.Fatalf("got %v, want the ready session", selected)
	}
	Release(selected)
	if pendingOf(ready) != 0 {
		t.Fatal("release must decrement pending")
	}
}

func TestExpiredGuestNotSelected(t *testing.T) {
	setup(t, 1, 2)
	expired := newGuest("expired", now()-GuestMaxAgeSeconds-1, 0)
	AddGuestSession(expired)
	mu.Lock()
	defer mu.Unlock()
	if !needsRotation(expired) || getGuestSession(request) != nil {
		t.Fatal("an expired guest session must not be selected")
	}
}

func TestCookieSelectionDoesNotChooseOAuth(t *testing.T) {
	setup(t, 1, 2)
	sessions := []*Session{
		{Kind: OAuth, OAuthToken: "oauth"},
		{Kind: OAuth, OAuthToken: "oauth2"},
		{Kind: Cookie, AuthToken: "auth", Ct0: "csrf"},
	}
	selected, err := selectCookieSession(sessions, request)
	if err != nil || selected != sessions[2] || selected.pending != 1 {
		t.Fatalf("got %v, %v", selected, err)
	}
	Release(selected)
	if selected.pending != 0 {
		t.Fatal("release must decrement pending")
	}
}

func TestCookieSelectionRespectsBusySessions(t *testing.T) {
	setup(t, 1, 2)
	sessions := []*Session{
		{Kind: Cookie, AuthToken: "busy", Ct0: "csrf", pending: 2},
		{Kind: Cookie, AuthToken: "ready", Ct0: "csrf"},
	}
	selected, err := selectCookieSession(sessions, request)
	if err != nil || selected != sessions[1] {
		t.Fatalf("got %v, %v", selected, err)
	}
	if _, err := selectCookieSession(sessions[:1], request); !errors.Is(err, types.ErrNoSessions) {
		t.Fatalf("got %v, want ErrNoSessions", err)
	}
}

func TestRateLimitedSessionsAreSkipped(t *testing.T) {
	setup(t, 1, 2)
	limited := &Session{Kind: Cookie, AuthToken: "a", Ct0: "c"}
	mu.Lock()
	sessionPool = []*Session{limited}
	mu.Unlock()

	SetRateLimit(limited, request, 5, now()+60, 50)
	if _, err := GetSession(request); !errors.Is(err, types.ErrNoSessions) {
		t.Fatalf("a session with 10 or fewer requests left must not be used, got %v", err)
	}
	other := Req{Cookie: "Other"}
	s, err := GetSession(other)
	if err != nil || s != limited {
		t.Fatalf("other endpoints must still use the session: %v %v", s, err)
	}
	Release(s)

	SetLimited(limited, other)
	if _, err := GetSession(other); !errors.Is(err, types.ErrNoSessions) {
		t.Fatalf("a limited session must not be used, got %v", err)
	}
	if s, err := GetSession(Req{Cookie: GraphUserTweetsV2}); err != nil || s != limited {
		t.Fatalf("UserWithProfileTweetsQueryV2 ignores the limit: %v %v", s, err)
	}
}

func TestLoneReadySessionIsAlwaysFound(t *testing.T) {
	setup(t, 1, 2)
	ready := &Session{Kind: Cookie, AuthToken: "ready", Ct0: "c"}
	mu.Lock()
	for range 19 {
		sessionPool = append(sessionPool, &Session{Kind: Cookie, AuthToken: "busy", Ct0: "c", pending: 3})
	}
	sessionPool = append(sessionPool, ready)
	mu.Unlock()

	for range 200 {
		s, err := GetSession(request)
		if err != nil || s != ready {
			t.Fatalf("got %v, %v; want the one ready session", s, err)
		}
		Release(s)
	}
}

func TestConcurrentAcquireRelease(t *testing.T) {
	setup(t, 1, 2)
	mu.Lock()
	for range 4 {
		sessionPool = append(sessionPool, &Session{Kind: Cookie, AuthToken: "a", Ct0: "c"})
	}
	mu.Unlock()
	var wg sync.WaitGroup
	for range 64 {
		wg.Go(func() {
			for range 100 {
				if s, err := GetSession(request); err == nil {
					SetRateLimit(s, request, 100, now()+60, 150)
					Release(s)
				}
			}
		})
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	for _, s := range sessionPool {
		if s.pending != 0 {
			t.Fatalf("pending = %d after all releases", s.pending)
		}
	}
}

func TestParseSession(t *testing.T) {
	s, err := ParseSession(`{"oauth_token": "123-abc", "oauth_token_secret": "secret", "username": "u"}`)
	if err != nil || s.Kind != OAuth || s.ID != 123 || s.OAuthToken != "123-abc" || s.OAuthSecret != "secret" {
		t.Fatalf("oauth: %+v, %v", s, err)
	}
	s, err = ParseSession(`{"kind": "cookie", "id": "42", "username": "u", "authToken": "auth", "ct0": "csrf"}`)
	if err != nil || s.Kind != Cookie || s.ID != 42 || s.AuthToken != "auth" || s.Ct0 != "csrf" {
		t.Fatalf("cookie: %+v, %v", s, err)
	}
	if s.String() != "cookie 42 (u)" {
		t.Fatalf("String() = %q", s.String())
	}
	for _, bad := range []string{`{"kind": "other"}`, `{"oauth_token": "nodash"}`, `not json`} {
		if _, err := ParseSession(bad); err == nil {
			t.Errorf("%s: want an error", bad)
		}
	}
}

func TestInitLoadsSessions(t *testing.T) {
	setup(t, 1, 2)
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.jsonl")
	data := `{"kind": "cookie", "id": "1", "auth_token": "a", "ct0": "c"}` + "\n" +
		`{"oauth_token": "2-x", "oauth_token_secret": "y"}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{MaxConcurrentReqs: 3, GuestPoolSize: 4}
	if err := Init(cfg, path); err != nil {
		t.Fatal(err)
	}
	var health struct {
		Sessions struct {
			Total  int
			OAuth  struct{ Total int }
			Cookie struct{ Total int }
			Guest  struct{ PoolSize int }
		}
	}
	if err := json.Unmarshal(HealthJSON(), &health); err != nil {
		t.Fatal(err)
	}
	h := health.Sessions
	if h.Total != 2 || h.OAuth.Total != 1 || h.Cookie.Total != 1 || h.Guest.PoolSize != 4 {
		t.Fatalf("health: %+v", h)
	}
	if err := Init(cfg, filepath.Join(dir, "missing.jsonl")); err != nil {
		t.Fatalf("a missing sessions file is not an error: %v", err)
	}
	if err := Init(cfg, filepath.Join(dir, "sessions.json")); err == nil {
		t.Fatal(".json sessions files must be rejected")
	}
}
