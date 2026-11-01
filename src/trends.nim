# SPDX-License-Identifier: AGPL-3.0-only
import httpclient, asyncdispatch, json, times, tables, uri
import consts, http_client, apiutils

type
  Trend* = object
    name*: string
    searchQuery*: string
    tweetVolume*: int

  TrendsCache = object
    trends: seq[Trend]
    fetched: int64

var
  cache: Table[int, TrendsCache]
  cacheSeconds = 15 * 60

proc setTrendsCacheMinutes*(minutes: int) =
  cacheSeconds = minutes * 60

const trendsUrl = "https://api.twitter.com/" & restTrends

proc fetchTrendsUncached(woeid: int): Future[seq[Trend]] {.async.} =
  # this endpoint doesn't need an account/guest session, so we bypass apiutils
  # session pool rather than wasting a request from it
  let headers = newHttpHeaders({
    "authorization": bearerToken,
    "accept": "*/*",
    "accept-language": "en-US,en;q=0.9",
    "user-agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36"
  })
  try:
    useHttpClient(headers):
      let (resp, body) = await c.getBounded(trendsUrl & "?id=" & $woeid,
                                             getApiTimeoutMs())

      if resp.status != $Http200:
        echo "[trends] fetch failed for woeid ", woeid, ", status: ", resp.status
        return @[]

      let js = parseJson(body)
      if js.kind != JArray or js.len == 0 or not js[0].hasKey("trends"):
        echo "[trends] unexpected response shape for woeid ", woeid
        return @[]

      for t in js[0]["trends"]:
        var vol = -1
        if t.hasKey("tweet_volume") and t["tweet_volume"].kind != JNull:
          vol = t["tweet_volume"].getInt
        let name = t["name"].getStr
        # take the already decoded query string
        let searchQuery =
          if t.hasKey("query") and t["query"].kind == JString:
            decodeUrl(t["query"].getStr)
          else:
            name
        result.add Trend(
          name: name,
          searchQuery: searchQuery,
          tweetVolume: vol
        )
  except Exception as e:
    echo "[trends] error fetching woeid ", woeid, ": ", e.msg
    result = @[]

proc getTrends*(woeid: int; maxItems: int): Future[seq[Trend]] {.async.} =
  let now = epochTime().int64

  template capped(s: seq[Trend]): seq[Trend] =
    if maxItems >= 0 and s.len > maxItems: s[0 ..< maxItems] else: s

  if cache.hasKey(woeid):
    let entry = cache[woeid]
    if now - entry.fetched < cacheSeconds:
      return capped(entry.trends)

  let fresh = await fetchTrendsUncached(woeid)
  if fresh.len > 0:
    cache[woeid] = TrendsCache(trends: fresh, fetched: now)
    return capped(fresh)

  if cache.hasKey(woeid):
    return capped(cache[woeid].trends)
  return @[]
