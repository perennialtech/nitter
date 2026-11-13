// SPDX-License-Identifier: AGPL-3.0-only

// Package config loads nitter.conf and the environment variables Nitter
// reads. Options and EnvVars define and document every setting;
// nitter.example.conf and nitter.example.env are generated from them.
package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Address   string
	Port      int
	UseHTTPS  bool
	Title     string
	Hostname  string
	StaticDir string

	HmacKey     string
	Base64Media bool
	// EnableRSS is the enableRSS master switch; FromFile turns off every
	// EnableRSS* feed when it is off.
	EnableRSS               bool
	EnableRSSUserTweets     bool
	EnableRSSUserReplies    bool
	EnableRSSUserMedia      bool
	EnableRSSUserArticles   bool
	EnableRSSUserReposts    bool
	EnableRSSSearch         bool
	EnableRSSList           bool
	EnableRSSCommunityNotes bool
	EnableDebug             bool
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
	CacheMaxMB    int

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

// FromFile reads every key in Options from f, using the default of each
// one that is missing or empty.
func FromFile(f File) (*Config, error) {
	c := &Config{Preferences: f["Preferences"]}
	for _, o := range Options {
		if err := o.apply(c, f); err != nil {
			return nil, err
		}
	}
	if !c.EnableRSS {
		c.EnableRSSUserTweets = false
		c.EnableRSSUserReplies = false
		c.EnableRSSUserMedia = false
		c.EnableRSSUserArticles = false
		c.EnableRSSUserReposts = false
		c.EnableRSSSearch = false
		c.EnableRSSList = false
		c.EnableRSSCommunityNotes = false
	}
	if c.CacheMaxMB <= 0 {
		return nil, fmt.Errorf("Cache.maxMegabytes: must be positive, got %d", c.CacheMaxMB)
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
