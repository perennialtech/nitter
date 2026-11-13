// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
)

// quote renders s as a JSON string the way Nim's json module does: only
// quotes, backslashes and control characters are escaped.
func quote(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\b':
			b.WriteString(`\b`)
		case c == '\f':
			b.WriteString(`\f`)
		case c == '\t':
			b.WriteString(`\t`)
		case c == '\v':
			b.WriteString(`\u000b`)
		case c == '\r':
			b.WriteString(`\r`)
		case c == '"':
			b.WriteString(`\"`)
		case c == '\\':
			b.WriteString(`\\`)
		case c < 0x20:
			b.WriteString(`\u00`)
			const hex = "0123456789ABCDEF"
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0xf])
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// obj builds a compact JSON object whose keys keep their order, like Nim's
// %* literals serialized with $. Values are string, int, bool or raw.
type obj []kv

type kv struct {
	key   string
	value any
}

// raw is already-serialized JSON.
type raw string

func (o obj) with(key string, value any) obj { return append(o, kv{key, value}) }

func (o obj) String() string {
	var b strings.Builder
	b.WriteByte('{')
	for i, m := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(quote(m.key))
		b.WriteByte(':')
		switch v := m.value.(type) {
		case string:
			b.WriteString(quote(v))
		case int:
			b.WriteString(strconv.Itoa(v))
		case bool:
			b.WriteString(strconv.FormatBool(v))
		case raw:
			b.WriteString(string(v))
		default:
			panic("api: unsupported JSON value")
		}
	}
	b.WriteByte('}')
	return b.String()
}

// format substitutes $1, $2... in tmpl like Nim's strutils.%.
func format(tmpl string, args ...string) string {
	var b strings.Builder
	for i := 0; i < len(tmpl); i++ {
		c := tmpl[i]
		if c != '$' || i+1 >= len(tmpl) {
			b.WriteByte(c)
			continue
		}
		if tmpl[i+1] == '$' {
			b.WriteByte('$')
			i++
			continue
		}
		j := i + 1
		for j < len(tmpl) && tmpl[j] >= '0' && tmpl[j] <= '9' {
			j++
		}
		if j == i+1 {
			b.WriteByte(c)
			continue
		}
		n, _ := strconv.Atoi(tmpl[i+1 : j])
		if n < 1 || n > len(args) {
			panic("api: format argument out of range")
		}
		b.WriteString(args[n-1])
		i = j - 1
	}
	return b.String()
}

// The getters below follow packedjson's: a value of the wrong kind reads as
// the zero value instead of being converted.

func notNull(js gjson.Result) bool { return js.Exists() && js.Type != gjson.Null }

func getStr(js gjson.Result) string {
	if js.Type == gjson.String {
		return js.Str
	}
	return ""
}

func getStrOr(js gjson.Result, def string) string {
	if js.Type == gjson.String {
		return js.Str
	}
	return def
}

func getInt(js gjson.Result) int {
	if js.Type == gjson.Number {
		return int(js.Int())
	}
	return 0
}

func getIntOr(js gjson.Result, def int) int {
	if js.Type == gjson.Number {
		return int(js.Int())
	}
	return def
}

func getFloat(js gjson.Result) float64 {
	if js.Type == gjson.Number {
		return js.Num
	}
	return 0
}

func getBool(js gjson.Result) bool { return js.Type == gjson.True }

// items returns the elements of an array, and nothing for anything else.
func items(js gjson.Result) []gjson.Result {
	if !js.IsArray() {
		return nil
	}
	return js.Array()
}
