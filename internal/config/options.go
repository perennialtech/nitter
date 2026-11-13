// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Option is one nitter.conf key. FromFile reads every key through these
// definitions, and cmd/genconfig writes nitter.example.conf from them, so
// the parser, the defaults and the documentation can't disagree.
type Option struct {
	Section string
	Key     string
	// Default is the value FromFile uses when the key is missing or empty.
	Default string
	Doc     string

	kind kind
	set  func(c *Config, v string) error
}

type kind int

const (
	kindString kind = iota
	kindInt
	kindBool
)

// Literal is the default as it is written in nitter.conf.
func (o Option) Literal() string {
	if o.kind == kindString {
		return Quote(o.Default)
	}
	return o.Default
}

// Quote writes s as a double-quoted value that Parse reads back as s.
func Quote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`)
	return `"` + r.Replace(s) + `"`
}

func str(section, key string, field func(*Config) *string, def, doc string) Option {
	return Option{Section: section, Key: key, Default: def, Doc: doc, kind: kindString,
		set: func(c *Config, v string) error {
			*field(c) = v
			return nil
		}}
}

func integer(section, key string, field func(*Config) *int, def int, doc string) Option {
	return Option{Section: section, Key: key, Default: strconv.Itoa(def), Doc: doc, kind: kindInt,
		set: func(c *Config, v string) error {
			n, err := strconv.Atoi(v)
			*field(c) = n
			return err
		}}
}

func boolean(section, key string, field func(*Config) *bool, def bool, doc string) Option {
	return Option{Section: section, Key: key, Default: strconv.FormatBool(def), Doc: doc, kind: kindBool,
		set: func(c *Config, v string) error {
			b, err := ParseBool(v)
			*field(c) = b
			return err
		}}
}

// sectionDocs describes the sections in Options, in file order.
// [Preferences] is documented by package prefs.
var sectionDocs = []struct{ Name, Doc string }{
	{"Server", "How Nitter listens, and the public address it puts in links."},
	{"Cache", "How long API results stay cached in memory, and how much memory the cache may use."},
	{"Config", "Upstream requests, sessions and the features the instance offers."},
}

// rssFeed defines a per-feed RSS switch, which enableRSS overrides.
func rssFeed(key string, field func(*Config) *bool, feeds string) Option {
	return boolean("Config", key, field, true,
		"Serve the RSS feeds at "+feeds+". When off, they answer 403 and "+
			"aren't linked.")
}

// Options lists every nitter.conf key outside [Preferences], in the order
// nitter.example.conf shows them.
var Options = []Option{
	str("Server", "hostname", func(c *Config) *string { return &c.Hostname }, "nitter.net",
		"Public host name of this instance, with a port if it isn't the default "+
			"for the scheme, such as nitter.example.com or localhost:8080. Absolute "+
			"links in RSS feeds, embeds, OpenSearch, oEmbed and the preferences URL "+
			"are built from it."),
	str("Server", "title", func(c *Config) *string { return &c.Title }, "Nitter",
		"Instance name, shown in page titles, the navbar, RSS feeds, OpenSearch and oEmbed."),
	str("Server", "address", func(c *Config) *string { return &c.Address }, "0.0.0.0",
		"IP address to listen on. 0.0.0.0 listens on every interface; 127.0.0.1 "+
			"only accepts connections from the same host, such as a local reverse "+
			"proxy. nitter healthcheck connects to this address, or to 127.0.0.1 "+
			"when it is unspecified."),
	integer("Server", "port", func(c *Config) *int { return &c.Port }, 8080,
		"TCP port to serve plain HTTP on. nitter healthcheck connects to it too."),
	boolean("Server", "https", func(c *Config) *bool { return &c.UseHTTPS }, true,
		"Whether visitors reach the instance over HTTPS, usually through a reverse "+
			"proxy; Nitter itself always serves plain HTTP. When on, absolute links use "+
			"https:// and the preference cookies are marked Secure, which browsers only "+
			"keep over HTTPS or on localhost. Turn it off when serving plain HTTP "+
			"elsewhere, or preferences won't stick."),
	str("Server", "staticDir", func(c *Config) *string { return &c.StaticDir }, "./public",
		"Directory of static files served at /: stylesheets, scripts, fonts and "+
			"the about page. Themes are the stylesheets in its css/themes. Relative "+
			"to the working directory."),

	integer("Cache", "listMinutes", func(c *Config) *int { return &c.ListCacheTime }, 120,
		"Minutes to cache list and community details and community moderators. "+
			"Their timelines aren't cached, so this can stay high."),
	integer("Cache", "rssMinutes", func(c *Config) *int { return &c.RssCacheTime }, 10,
		"Minutes to cache each generated RSS feed. Feed readers see new tweets "+
			"at most this late."),
	integer("Cache", "maxMegabytes", func(c *Config) *int { return &c.CacheMaxMB }, 128,
		"Memory limit of the cache, in MiB of compressed entries, shared by "+
			"profiles, lists, communities, RSS feeds and the rest. Entries are "+
			"evicted early when it is full. Must be positive."),

	str("Config", "hmacKey", func(c *Config) *string { return &c.HmacKey }, "secretkey",
		"Secret key that signs proxied video URLs and on-demand translation "+
			"requests, so only links this instance generated are served. Set a "+
			"unique random value, such as the output of openssl rand -hex 32. With "+
			"the default, signatures can be forged: Nitter logs a warning and "+
			"leaves onDemandTranslation off. Changing it breaks video links already "+
			"handed out, such as those in cached RSS feeds."),
	boolean("Config", "base64Media", func(c *Config) *bool { return &c.Base64Media }, false,
		"Put proxied image and video URLs in paths as base64 (/pic/enc/..., "+
			"/video/enc/...) instead of percent-encoding them. Both forms are always "+
			"served; this only chooses the one in generated links."),
	boolean("Config", "enableRSS", func(c *Config) *bool { return &c.EnableRSS }, true,
		"Master switch for RSS. When off, every feed below is off, whatever its own setting."),
	rssFeed("enableRSSUserTweets", func(c *Config) *bool { return &c.EnableRSSUserTweets }, "/<user>/rss"),
	rssFeed("enableRSSUserReplies", func(c *Config) *bool { return &c.EnableRSSUserReplies }, "/<user>/with_replies/rss"),
	rssFeed("enableRSSUserReposts", func(c *Config) *bool { return &c.EnableRSSUserReposts }, "/<user>/reposts/rss"),
	rssFeed("enableRSSUserMedia", func(c *Config) *bool { return &c.EnableRSSUserMedia }, "/<user>/media/rss"),
	rssFeed("enableRSSUserArticles", func(c *Config) *bool { return &c.EnableRSSUserArticles }, "/<user>/articles/rss"),
	rssFeed("enableRSSSearch", func(c *Config) *bool { return &c.EnableRSSSearch }, "/search/rss and /<user>/search/rss"),
	rssFeed("enableRSSList", func(c *Config) *bool { return &c.EnableRSSList }, "/i/lists/<id>/rss and /<user>/lists/<slug>/rss"),
	boolean("Config", "enableRSSCommunityNotes", func(c *Config) *bool { return &c.EnableRSSCommunityNotes }, true,
		"Serve the RSS feeds at /i/communitynotes/rss and "+
			"/i/communitynotes/new/rss. When off, or with guestAuth, they answer "+
			"403 and aren't linked."),
	boolean("Config", "enableTrends", func(c *Config) *bool { return &c.EnableTrends }, false,
		"Show a panel of trending topics with two tabs on the homepage. Trends "+
			"are fetched without using a session."),
	str("Config", "trendsTab1Name", func(c *Config) *string { return &c.TrendsTab1Name }, "United States",
		"Label of the first trends tab, which is shown when the homepage loads."),
	integer("Config", "trendsTab1Woeid", func(c *Config) *int { return &c.TrendsTab1Woeid }, 23424977,
		"WOEID (Where On Earth ID) of the place whose trends the first tab shows. "+
			"tools/trending-woeids.md lists the ones that return trends; 1 is worldwide."),
	str("Config", "trendsTab2Name", func(c *Config) *string { return &c.TrendsTab2Name }, "Worldwide",
		"Label of the second trends tab."),
	integer("Config", "trendsTab2Woeid", func(c *Config) *int { return &c.TrendsTab2Woeid }, 1,
		"WOEID of the place whose trends the second tab shows, as for trendsTab1Woeid."),
	integer("Config", "trendsCacheMinutes", func(c *Config) *int { return &c.TrendsCacheMinutes }, 15,
		"Minutes to keep each place's trends before fetching them again."),
	integer("Config", "trendsMaxItems", func(c *Config) *int { return &c.TrendsMaxItems }, 15,
		"Trends to show per tab, at most. The API returns about 50; a negative value shows all of them."),
	boolean("Config", "enableDebug", func(c *Config) *bool { return &c.EnableDebug }, false,
		"Serve /.sessions, a JSON dump of every session's state, pending requests "+
			"and rate limits. It identifies the accounts, so don't turn it on for a "+
			"public instance. /.health is always served."),
	boolean("Config", "showCommunityNotesNav", func(c *Config) *bool { return &c.ShowCommunityNotesNav }, true,
		"Show a navbar icon linking to the community notes page. It is hidden with guestAuth regardless."),
	boolean("Config", "showRetweetTime", func(c *Config) *bool { return &c.ShowRetweetTime }, true,
		"Show when a tweet was retweeted, linked to the retweet, next to \"user retweeted\"."),
	boolean("Config", "enableTranslation", func(c *Config) *bool { return &c.EnableTranslation }, true,
		"Show \"See translation\" links, and the translations Twitter provides for "+
			"tweets and community notes."),
	boolean("Config", "onDemandTranslation", func(c *Config) *bool { return &c.OnDemandTranslation }, false,
		"Offer Grok translations, made with a cookie session, of tweets and notes "+
			"Twitter didn't translate. Needs enableTranslation, cookie sessions "+
			"(not guestAuth) and a non-default hmacKey; without them Nitter logs why "+
			"and leaves it off."),
	str("Config", "onDemandTranslationLang", func(c *Config) *string { return &c.OnDemandTranslationLang }, "en",
		"Language code onDemandTranslation translates into, such as en, de or pt-BR."),
	boolean("Config", "guestAuth", func(c *Config) *bool { return &c.GuestAuth }, false,
		"Use guest tokens instead of the account sessions in the sessions file, "+
			"which isn't read then. Needs NITTER_GUEST_BEARER_TOKEN. Pages and tabs "+
			"guests can't load, such as community notes, are hidden."),
	integer("Config", "guestPoolSize", func(c *Config) *int { return &c.GuestPoolSize }, 1,
		"Guest tokens to keep activated at once with guestAuth. Less than 1 means 1."),
	str("Config", "apiProxy", func(c *Config) *string { return &c.APIProxy }, "",
		"Address of a nitter-proxy instance, such as localhost:7000, that "+
			"forwards Twitter API requests: https://api.x.com/... is requested as "+
			"http://localhost:7000/api.x.com/.... Requests to the 1.1 REST API go "+
			"directly. http:// is assumed without a scheme. Empty sends everything directly."),
	boolean("Config", "disableTid", func(c *Config) *bool { return &c.DisableTid }, false,
		"Don't generate the x-client-transaction-id header, and authenticate "+
			"cookie sessions with the bearer token that doesn't need it. Try this "+
			"if cookie sessions fail."),
	integer("Config", "maxConcurrentReqs", func(c *Config) *int { return &c.MaxConcurrentReqs }, 2,
		"Requests each session may have in flight at once. When every account "+
			"session is at the limit, further requests fail until one finishes; guest "+
			"requests wait instead. Less than 1 means 2."),
	integer("Config", "maxRetries", func(c *Config) *int { return &c.MaxRetries }, 1,
		"Attempts at an API request while sessions are rate limited, each with "+
			"another session. 1 tries once without retrying; 0 or less fails every "+
			"session request as rate limited."),
	integer("Config", "retryDelayMs", func(c *Config) *int { return &c.RetryDelayMs }, 150,
		"Milliseconds to wait before retrying a rate-limited request."),
	integer("Config", "apiTimeoutMs", func(c *Config) *int { return &c.APITimeoutMs }, 15000,
		"Milliseconds an upstream request may take, from connecting to the end "+
			"of the response, before it is abandoned and its connection closed. "+
			"Values under 1000 mean 1000."),
}

// EnvVar is one environment variable Nitter reads. cmd/genconfig writes
// nitter.example.env from EnvVars.
type EnvVar struct {
	Name string
	// Default is used when the variable is unset or empty.
	Default string
	Doc     string
	// Secret marks values that grant access and must be kept private.
	Secret bool
	// Stdlib marks the standard variables Go's standard library reads
	// itself, rather than Nitter through Value.
	Stdlib bool
}

// Value is the variable's value, or Default when it is unset or empty.
func (e EnvVar) Value() string {
	if v := getenv(e.Name); v != "" {
		return v
	}
	return e.Default
}

var (
	ConfFile = EnvVar{Name: "NITTER_CONF_FILE", Default: "./nitter.conf",
		Doc: "Path of the config file, relative to the working directory. nitter " +
			"healthcheck reads it too, to find the port."}
	SessionsFile = EnvVar{Name: "NITTER_SESSIONS_FILE", Default: "./sessions.jsonl",
		Doc: "Path of the account sessions file, with one JSON session per line " +
			"(see the README). It must not end in .json. It isn't read with guestAuth."}
	GuestBearerToken = EnvVar{Name: "NITTER_GUEST_BEARER_TOKEN", Secret: true,
		Doc: "Bearer token that guest requests authenticate with, without the " +
			"\"Bearer \" prefix. Required when guestAuth is on, and ignored otherwise."}

	// Every outbound client uses http.ProxyFromEnvironment, which reads
	// these.
	httpsProxy = EnvVar{Name: "HTTPS_PROXY", Stdlib: true,
		Doc: "Proxy for HTTPS requests, which is every request to X, FxTwitter " +
			"and twimg media, as a URL such as http://127.0.0.1:3128 or " +
			"socks5://127.0.0.1:1080; http:// is assumed without a scheme. Put " +
			"credentials in it as user:password@, and keep it secret then. " +
			"https_proxy works too. Empty sends requests directly."}
	httpProxy = EnvVar{Name: "HTTP_PROXY", Stdlib: true,
		Doc: "Proxy for plain HTTP requests, such as those to a non-local apiProxy, " +
			"in the same form as HTTPS_PROXY. http_proxy works too."}
	noProxy = EnvVar{Name: "NO_PROXY", Stdlib: true,
		Doc: "Comma-separated hosts, domains (.example.com), IP addresses and " +
			"CIDR ranges to reach without the proxy; * matches everything. Requests " +
			"to localhost and loopback addresses never use the proxy. no_proxy works too."}
)

// EnvVars lists every environment variable Nitter reads.
var EnvVars = []EnvVar{ConfFile, SessionsFile, GuestBearerToken, httpsProxy, httpProxy, noProxy}

func (o Option) apply(c *Config, f File) error {
	v := f[o.Section][o.Key]
	if v == "" {
		v = o.Default
	}
	if err := o.set(c, v); err != nil {
		return fmt.Errorf("%s.%s: %w", o.Section, o.Key, err)
	}
	return nil
}

// getenv is os.Getenv; every environment variable is read through EnvVar.
var getenv = os.Getenv
