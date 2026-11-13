// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"bytes"
	"encoding/json"
	"strconv"
	"time"
)

// object is a JSON object that keeps its keys in insertion order.
type object []member

type member struct {
	key   string
	value any
}

func (o *object) set(key string, value any) { *o = append(*o, member{key, value}) }

func (o object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(m.key)
		b.Write(k)
		b.WriteByte(':')
		v, err := json.Marshal(m.value)
		if err != nil {
			return nil, err
		}
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func snowflakeToEpoch(flake int64) int64 {
	return ((flake >> 22) + 1288834974657) / 1000
}

func formatEpoch(sec int64) string {
	return time.Unix(sec, 0).Format("2006-01-02T15:04:05-07:00")
}

// HealthJSON reports the size and state of the session pools and the
// requests they made in the current rate limit windows, for /.health.
func HealthJSON() []byte {
	mu.Lock()
	defer mu.Unlock()
	now := now()

	var (
		totalReqs                   int
		limited                     = map[int64]bool{}
		reqsPerAPI                  = map[string]int{}
		oldest                      = now
		newest, average             int64
		oauthTotal, cookieTotal     int
		oauthLimited, cookieLimited int
		guestLimited, guestRotation int
	)

	countReqs := func(s *Session, prefix string) {
		for api, rl := range s.apis {
			// no requests made with this session and endpoint since the limit reset
			if rl.Reset < now {
				continue
			}
			reqs := rl.Limit - rl.Remaining
			reqsPerAPI[prefix+api] += reqs
			totalReqs += reqs
		}
	}

	for _, s := range sessionPool {
		created := snowflakeToEpoch(s.ID)
		newest = max(newest, created)
		oldest = min(oldest, created)
		average += created

		switch s.Kind {
		case OAuth:
			oauthTotal++
		case Cookie:
			cookieTotal++
		}
		if s.limited {
			limited[s.ID] = true
			switch s.Kind {
			case OAuth:
				oauthLimited++
			case Cookie:
				cookieLimited++
			}
		}
		countReqs(s, "")
	}

	for _, s := range guestPool {
		if s.limited {
			guestLimited++
		}
		if needsRotation(s) {
			guestRotation++
		}
		countReqs(s, "guest:")
	}

	if len(sessionPool) > 0 {
		average /= int64(len(sessionPool))
	} else {
		oldest, average = 0, 0
	}

	sessions := object{
		{"total", len(sessionPool)},
		{"limited", len(limited)},
		{"oauth", object{{"total", oauthTotal}, {"limited", oauthLimited}}},
		{"cookie", object{{"total", cookieTotal}, {"limited", cookieLimited}}},
		{"guest", object{
			{"total", len(guestPool)},
			{"limited", guestLimited},
			{"pendingRotation", guestRotation},
			{"poolSize", guestPoolSize},
		}},
		{"oldest", formatEpoch(oldest)},
		{"newest", formatEpoch(newest)},
		{"average", formatEpoch(average)},
	}
	out, _ := json.Marshal(object{
		{"sessions", sessions},
		{"requests", object{{"total", totalReqs}, {"apis", reqsPerAPI}}},
	})
	return out
}

func debugAPIs(s *Session, now int64) object {
	apis := object{}
	for api, rl := range s.apis {
		if rl.Reset > now {
			apis.set(api, object{{"remaining", rl.Remaining}, {"reset", rl.Reset}})
		}
	}
	return apis
}

// DebugJSON reports each session's state without its credentials, for
// /.sessions.
func DebugJSON() []byte {
	mu.Lock()
	defer mu.Unlock()
	now := now()
	list := object{}

	for i, s := range sessionPool {
		id := strconv.FormatInt(s.ID, 10)
		if len(id) > 4 {
			id = "..." + id[len(id)-4:]
		}
		js := object{
			{"kind", s.Kind.String()},
			{"userId", id},
			{"apis", debugAPIs(s, now)},
			{"pending", s.pending},
		}
		if s.limited {
			js.set("limited", true)
		}
		list.set("acct-"+strconv.Itoa(i), js)
	}

	for i, s := range guestPool {
		js := object{
			{"kind", s.Kind.String()},
			{"apis", debugAPIs(s, now)},
			{"pending", s.pending},
			{"requests", s.guestReqs},
			{"ageSeconds", now - s.guestCreated},
			{"needsRotation", needsRotation(s)},
		}
		if s.limited {
			js.set("limited", true)
		}
		list.set("guest-"+strconv.Itoa(i), js)
	}

	out, _ := json.Marshal(list)
	return out
}
