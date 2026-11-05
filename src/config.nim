# SPDX-License-Identifier: AGPL-3.0-only
import parsecfg except Config
import types, strutils

proc get*[T](config: parseCfg.Config; section, key: string; default: T): T =
  let val = config.getSectionValue(section, key)
  if val.len == 0: return default

  when T is int: parseInt(val)
  elif T is bool: parseBool(val)
  elif T is string: val

proc getConfig*(path: string): (Config, parseCfg.Config) =
  var cfg = loadConfig(path)

  let masterRss = cfg.get("Config", "enableRSS", true)

  let conf = Config(
    # Server
    address: cfg.get("Server", "address", "0.0.0.0"),
    port: cfg.get("Server", "port", 8080),
    useHttps: cfg.get("Server", "https", true),
    staticDir: cfg.get("Server", "staticDir", "./public"),
    title: cfg.get("Server", "title", "Nitter"),
    hostname: cfg.get("Server", "hostname", "nitter.net"),

    # Cache
    listCacheTime: cfg.get("Cache", "listMinutes", 120),
    rssCacheTime: cfg.get("Cache", "rssMinutes", 10),

    redisHost: cfg.get("Cache", "redisHost", "localhost"),
    redisPort: cfg.get("Cache", "redisPort", 6379),
    redisConns: cfg.get("Cache", "redisConnections", 20),
    redisMaxConns: cfg.get("Cache", "redisMaxConnections", 30),
    redisPassword: cfg.get("Cache", "redisPassword", ""),

    # Config
    hmacKey: cfg.get("Config", "hmacKey", "secretkey"),
    base64Media: cfg.get("Config", "base64Media", false),
    minTokens: cfg.get("Config", "tokenCount", 10),
    enableRSSUserTweets: masterRss and cfg.get("Config", "enableRSSUserTweets", true),
    enableRSSUserReplies: masterRss and cfg.get("Config", "enableRSSUserReplies", true),
    enableRSSUserMedia: masterRss and cfg.get("Config", "enableRSSUserMedia", true),
    enableRSSUserArticles: masterRss and cfg.get("Config", "enableRSSUserArticles", true),
    enableRSSUserReposts: masterRss and cfg.get("Config", "enableRSSUserReposts", true),
    enableRSSSearch: masterRss and cfg.get("Config", "enableRSSSearch", true),
    enableRSSList: masterRss and cfg.get("Config", "enableRSSList", true),
    enableRSSCommunityNotes: masterRss and cfg.get("Config", "enableRSSCommunityNotes", true),
    enableDebug: cfg.get("Config", "enableDebug", false),
    proxy: cfg.get("Config", "proxy", ""),
    proxyAuth: cfg.get("Config", "proxyAuth", ""),
    apiProxy: cfg.get("Config", "apiProxy", ""),
    disableTid: cfg.get("Config", "disableTid", false),
    maxConcurrentReqs: cfg.get("Config", "maxConcurrentReqs", 2),
    maxRetries: cfg.get("Config", "maxRetries", 1),
    retryDelayMs: cfg.get("Config", "retryDelayMs", 150),
    apiTimeoutMs: cfg.get("Config", "apiTimeoutMs", 15000),
    # guest-token auth: bypasses the account sessions.jsonl entirely when enabled
    guestAuth: cfg.get("Config", "guestAuth", false),
    guestPoolSize: cfg.get("Config", "guestPoolSize", 1),

    # trending topics tabs shown on the main page
    enableTrends: cfg.get("Config", "enableTrends", false),
    trendsTab1Name: cfg.get("Config", "trendsTab1Name", "United States"),
    trendsTab1Woeid: cfg.get("Config", "trendsTab1Woeid", 23424977),
    trendsTab2Name: cfg.get("Config", "trendsTab2Name", "Worldwide"),
    trendsTab2Woeid: cfg.get("Config", "trendsTab2Woeid", 1),
    trendsCacheMinutes: cfg.get("Config", "trendsCacheMinutes", 15),
    trendsMaxItems: cfg.get("Config", "trendsMaxItems", 15),
    showCommunityNotesNav: cfg.get("Config", "showCommunityNotesNav", true),
    showRetweetTime: cfg.get("Config", "showRetweetTime", true),
    enableTranslation: cfg.get("Config", "enableTranslation", true),
    onDemandTranslation: cfg.get("Config", "onDemandTranslation", false),
    onDemandTranslationLang: cfg.get("Config", "onDemandTranslationLang", "en")
  )

  return (conf, cfg)
