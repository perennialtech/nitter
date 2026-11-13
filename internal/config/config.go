// SPDX-License-Identifier: AGPL-3.0-only

// Package config loads nitter.conf. See nitter.example.conf for every key.
package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Address   string
	Port      int
	UseHTTPS  bool
	Title     string
	Hostname  string
	StaticDir string

	HmacKey                 string
	Base64Media             bool
	MinTokens               int
	EnableRSSUserTweets     bool
	EnableRSSUserReplies    bool
	EnableRSSUserMedia      bool
	EnableRSSUserArticles   bool
	EnableRSSUserReposts    bool
	EnableRSSSearch         bool
	EnableRSSList           bool
	EnableRSSCommunityNotes bool
	EnableDebug             bool
	Proxy                   string
	ProxyAuth               string
	APIProxy                string
	DisableTid              bool
	MaxConcurrentReqs       int
	MaxRetries              int
	RetryDelayMs            int
	APITimeoutMs            int
	GuestAuth               bool
	GuestPoolSize           int
	EnableTrends            bool
	TrendsTab1Name          string
	TrendsTab1Woeid         int
	TrendsTab2Name          string
	TrendsTab2Woeid         int
	TrendsCacheMinutes      int
	TrendsMaxItems          int
	ShowCommunityNotesNav   bool
	ShowRetweetTime         bool
	EnableTranslation       bool
	OnDemandTranslation     bool
	OnDemandTranslationLang string

	RssCacheTime  int
	ListCacheTime int

	RedisHost     string
	RedisPort     int
	RedisConns    int
	RedisMaxConns int
	RedisPassword string

	// Preferences holds the raw [Preferences] section, which overrides the
	// default value of each preference.
	Preferences map[string]string
}

// File is a parsed nitter.conf: section -> key -> value.
type File map[string]map[string]string

// Parse reads the INI-like format used by Nim's parsecfg: [Section] headers,
// key = value pairs with optional double-quoted values, and # comments.
func Parse(text string) (File, error) {
	f := File{}
	section := ""
	sc := bufio.NewScanner(strings.NewReader(text))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' {
			end := strings.IndexByte(line, ']')
			if end < 0 {
				return nil, fmt.Errorf("line %d: unterminated section header", n)
			}
			section = strings.TrimSpace(line[1:end])
			continue
		}
		key, rest, ok := strings.Cut(line, "=")
		if !ok {
			key, rest, ok = strings.Cut(line, ":")
		}
		if !ok {
			return nil, fmt.Errorf("line %d: expected key = value", n)
		}
		val, err := parseValue(strings.TrimSpace(rest))
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		if f[section] == nil {
			f[section] = map[string]string{}
		}
		f[section][strings.TrimSpace(key)] = val
	}
	return f, sc.Err()
}

func parseValue(s string) (string, error) {
	if strings.HasPrefix(s, `"`) {
		var b strings.Builder
		for i := 1; i < len(s); i++ {
			c := s[i]
			switch {
			case c == '\\' && i+1 < len(s):
				i++
				switch s[i] {
				case 'n':
					b.WriteByte('\n')
				case 't':
					b.WriteByte('\t')
				default:
					b.WriteByte(s[i])
				}
			case c == '"':
				return b.String(), nil
			default:
				b.WriteByte(c)
			}
		}
		return "", fmt.Errorf("unterminated string")
	}
	if i := strings.IndexByte(s, '#'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s), nil
}

type getter struct {
	f   File
	err error
}

func (g *getter) str(section, key, def string) string {
	if v := g.f[section][key]; v != "" {
		return v
	}
	return def
}

func (g *getter) int(section, key string, def int) int {
	v := g.f[section][key]
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil && g.err == nil {
		g.err = fmt.Errorf("%s.%s: %w", section, key, err)
	}
	return n
}

func (g *getter) bool(section, key string, def bool) bool {
	v := g.f[section][key]
	if v == "" {
		return def
	}
	b, err := ParseBool(v)
	if err != nil && g.err == nil {
		g.err = fmt.Errorf("%s.%s: %w", section, key, err)
	}
	return b
}

// ParseBool accepts the same spellings as Nim's parseBool.
func ParseBool(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "y", "yes", "true", "1", "on":
		return true, nil
	case "n", "no", "false", "0", "off":
		return false, nil
	}
	return false, fmt.Errorf("invalid boolean %q", s)
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	f, err := Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return FromFile(f)
}

func FromFile(f File) (*Config, error) {
	g := &getter{f: f}
	rss := g.bool("Config", "enableRSS", true)
	c := &Config{
		Address:   g.str("Server", "address", "0.0.0.0"),
		Port:      g.int("Server", "port", 8080),
		UseHTTPS:  g.bool("Server", "https", true),
		StaticDir: g.str("Server", "staticDir", "./public"),
		Title:     g.str("Server", "title", "Nitter"),
		Hostname:  g.str("Server", "hostname", "nitter.net"),

		ListCacheTime: g.int("Cache", "listMinutes", 120),
		RssCacheTime:  g.int("Cache", "rssMinutes", 10),
		RedisHost:     g.str("Cache", "redisHost", "localhost"),
		RedisPort:     g.int("Cache", "redisPort", 6379),
		RedisConns:    g.int("Cache", "redisConnections", 20),
		RedisMaxConns: g.int("Cache", "redisMaxConnections", 30),
		RedisPassword: g.str("Cache", "redisPassword", ""),

		HmacKey:                 g.str("Config", "hmacKey", "secretkey"),
		Base64Media:             g.bool("Config", "base64Media", false),
		MinTokens:               g.int("Config", "tokenCount", 10),
		EnableRSSUserTweets:     rss && g.bool("Config", "enableRSSUserTweets", true),
		EnableRSSUserReplies:    rss && g.bool("Config", "enableRSSUserReplies", true),
		EnableRSSUserMedia:      rss && g.bool("Config", "enableRSSUserMedia", true),
		EnableRSSUserArticles:   rss && g.bool("Config", "enableRSSUserArticles", true),
		EnableRSSUserReposts:    rss && g.bool("Config", "enableRSSUserReposts", true),
		EnableRSSSearch:         rss && g.bool("Config", "enableRSSSearch", true),
		EnableRSSList:           rss && g.bool("Config", "enableRSSList", true),
		EnableRSSCommunityNotes: rss && g.bool("Config", "enableRSSCommunityNotes", true),
		EnableDebug:             g.bool("Config", "enableDebug", false),
		Proxy:                   g.str("Config", "proxy", ""),
		ProxyAuth:               g.str("Config", "proxyAuth", ""),
		APIProxy:                g.str("Config", "apiProxy", ""),
		DisableTid:              g.bool("Config", "disableTid", false),
		MaxConcurrentReqs:       g.int("Config", "maxConcurrentReqs", 2),
		MaxRetries:              g.int("Config", "maxRetries", 1),
		RetryDelayMs:            g.int("Config", "retryDelayMs", 150),
		APITimeoutMs:            g.int("Config", "apiTimeoutMs", 15000),
		GuestAuth:               g.bool("Config", "guestAuth", false),
		GuestPoolSize:           g.int("Config", "guestPoolSize", 1),
		EnableTrends:            g.bool("Config", "enableTrends", false),
		TrendsTab1Name:          g.str("Config", "trendsTab1Name", "United States"),
		TrendsTab1Woeid:         g.int("Config", "trendsTab1Woeid", 23424977),
		TrendsTab2Name:          g.str("Config", "trendsTab2Name", "Worldwide"),
		TrendsTab2Woeid:         g.int("Config", "trendsTab2Woeid", 1),
		TrendsCacheMinutes:      g.int("Config", "trendsCacheMinutes", 15),
		TrendsMaxItems:          g.int("Config", "trendsMaxItems", 15),
		ShowCommunityNotesNav:   g.bool("Config", "showCommunityNotesNav", true),
		ShowRetweetTime:         g.bool("Config", "showRetweetTime", true),
		EnableTranslation:       g.bool("Config", "enableTranslation", true),
		OnDemandTranslation:     g.bool("Config", "onDemandTranslation", false),
		OnDemandTranslationLang: g.str("Config", "onDemandTranslationLang", "en"),

		Preferences: f["Preferences"],
	}
	if g.err != nil {
		return nil, g.err
	}
	return c, nil
}

// URLPrefix is the instance's external origin, used for absolute links.
func (c *Config) URLPrefix() string {
	if c.UseHTTPS {
		return "https://" + c.Hostname
	}
	return "http://" + c.Hostname
}

// HasHmacKey reports whether the operator replaced the insecure default key.
func (c *Config) HasHmacKey() bool {
	return c.HmacKey != "" && c.HmacKey != "secretkey"
}
