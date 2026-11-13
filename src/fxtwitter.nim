# SPDX-License-Identifier: AGPL-3.0-only
## Client for the public FxTwitter API (https://docs.fxembed.com/api/), which
## serves X data without Nitter's own account sessions.
import std/[asyncdispatch, httpclient, strutils, options, times, uri]
import packedjson
import types, parserutils, utils, http_client, apistats, apiutils

type
  FxError* = object of CatchableError

const
  fxBase = "https://api.fxtwitter.com/2/"
  # https://docs.fxembed.com/api/introduction/#rate-limiting
  fxRequestsPerMinute = 1000

var fxMinute, fxMinuteReqs: int

proc fxError(label, outcome, msg: string): ref FxError =
  recordCall("fx", label, outcome)
  newException(FxError, "FxTwitter " & label & ": " & msg)

proc fxGet(label, path: string; params: seq[(string, string)] = @[];
           allowNotFound = false): Future[JsonNode] {.async.} =
  ## `label` names the endpoint in the call stats. Raises FxError for anything
  ## but a 200, or a 404 when `allowNotFound` is set.
  let minute = epochTime().int div 60
  if minute != fxMinute:
    fxMinute = minute
    fxMinuteReqs = 0
  if fxMinuteReqs >= fxRequestsPerMinute:
    raise fxError(label, "limited", "over the per-minute rate limit")
  inc fxMinuteReqs
  fxPeakPerMinute = max(fxPeakPerMinute, fxMinuteReqs)

  var url = fxBase & path
  if params.len > 0:
    url.add "?" & encodeQuery(params)

  let headers = newHttpHeaders({"accept": "application/json", "user-agent": "Nitter"})
  var
    resp: AsyncResponse
    body: string
  try:
    useHttpClient(headers):
      (resp, body) = await c.getBounded(url, getApiTimeoutMs())
  except CatchableError as e:
    raise fxError(label, "error", e.msg)

  if resp.code == Http429:
    # Stop sending requests until the next minute window.
    fxMinuteReqs = fxRequestsPerMinute
    raise fxError(label, "limited", "429")

  var js: JsonNode
  try:
    js = parseJson(body)
  except CatchableError:
    raise fxError(label, "error", resp.status & ", body is not JSON")

  # The body's code mirrors the HTTP status and is set even on a 200.
  case js{"code"}.getInt(resp.code.int)
  of 200:
    recordCall("fx", label, "ok")
    result = js
  of 404:
    if not allowNotFound:
      raise fxError(label, "notfound", js{"message"}.getStr)
    recordCall("fx", label, "notfound")
    result = js
  else:
    raise fxError(label, "error", resp.status & ", " & js{"message"}.getStr)

proc fxImage(url: string): string =
  ## Nitter stores pbs.twimg.com URLs without the scheme and host.
  result = url
  result.removePrefix(https)
  result.removePrefix(twimg)

proc parseFxUser*(js: JsonNode): User =
  if js.kind != JObject: return
  result = User(
    id: js{"id"}.getStr,
    username: js{"screen_name"}.getStr,
    fullname: js{"name"}.getStr,
    location: js{"location"}.getStr,
    website: js{"website", "url"}.getStr,
    userPic: js{"avatar_url"}.getStr.fxImage.multiReplace(("_normal", ""), ("_200x200", "")),
    following: js{"following"}.getInt,
    followers: js{"followers"}.getInt,
    tweets: js{"statuses"}.getInt,
    likes: js{"likes"}.getInt,
    media: js{"media_count"}.getInt,
    protected: js{"protected"}.getBool,
    joinDate: js{"joined"}.getTime
  )

  let banner = js{"banner_url"}.getStr.fxImage
  if banner.len > 0:
    result.banner = banner & "/1500x500"

  if js{"verification", "verified"}.getBool:
    result.verifiedType =
      case js{"verification", "type"}.getStr
      of "government": government
      of "organization", "business": business
      else: blue

  result.expandFxBio(js{"raw_description"})

proc parseFxVideo(js: JsonNode): Video =
  result = Video(
    available: true,
    thumb: js{"thumbnail_url"}.getStr.fxImage,
    title: js{"altText"}.getStr,
    durationMs: int(js{"duration"}.getFloat * 1000)
  )
  with formats, js{"formats"}:
    for f in formats:
      let url = f{"url"}.getStr
      if f{"container"}.getStr == "m3u8":
        result.variants.add VideoVariant(contentType: m3u8, url: url)
      else:
        result.variants.add VideoVariant(contentType: mp4, url: url,
                                         bitrate: f{"bitrate"}.getInt,
                                         resolution: getMp4Resolution(url))

proc parseFxMedia(js: JsonNode; tweet: Tweet) =
  with all, js{"all"}:
    for m in all:
      case m{"type"}.getStr
      of "photo":
        tweet.media.add Media(kind: photoMedia, photo: Photo(
          url: m{"url"}.getStr.split('?')[0].fxImage,
          altText: m{"altText"}.getStr
        ))
      of "video":
        tweet.media.add Media(kind: videoMedia, video: parseFxVideo(m))
      of "gif":
        tweet.media.add Media(kind: gifMedia, gif: Gif(
          url: m{"url"}.getStr.fxImage,
          thumb: m{"thumbnail_url"}.getStr.fxImage,
          altText: m{"altText"}.getStr
        ))
      else: discard

proc parseFxPoll(js: JsonNode): Poll =
  with choices, js{"choices"}:
    for c in choices:
      result.options.add c{"label"}.getStr
      result.values.add c{"count"}.getInt
  result.votes = js{"total_votes"}.getInt
  if result.values.len > 0:
    result.leader = result.values.find(max(result.values))

  result.status = "Final results"
  try:
    let ends = parse(js{"ends_at"}.getStr, "yyyy-MM-dd'T'HH:mm:ss'.'fff'Z'", utc())
    if ends > now():
      let timeLeft = $(ends - now())
      result.status = timeLeft[0 ..< max(0, timeLeft.find(","))]
      if result.status.len == 0: result.status = timeLeft
  except TimeParseError:
    discard

proc parseFxCard(js: JsonNode; embedCard: string): Card =
  Card(
    kind: if embedCard == "summary": summary else: summaryLarge,
    url: js{"url"}.getStr,
    dest: js{"domain"}.getStr,
    title: js{"title"}.getStr,
    text: js{"description"}.getStr,
    image: js{"image", "url"}.getStr.fxImage
  )

proc parseFxTweet*(js: JsonNode): Tweet =
  if js.kind != JObject: return Tweet()
  if js{"type"}.getStr == "tombstone":
    return Tweet(text: js{"message"}.getStr)

  result = Tweet(
    id: js{"id"}.getId,
    replyId: js{"replying_to", "status"}.getId,
    user: parseFxUser(js{"author"}),
    time: fromUnix(js{"created_timestamp"}.getBiggestInt).utc,
    available: true,
    stats: TweetStats(
      replies: js{"replies"}.getInt,
      retweets: js{"reposts"}.getInt,
      quotes: js{"quotes"}.getInt,
      likes: js{"likes"}.getInt,
      views: js{"views"}.getInt,
      source: js{"source"}.getStr
    )
  )

  with quote, js{"quote"}:
    result.quote = some parseFxTweet(quote)
  with poll, js{"poll"}:
    result.poll = some parseFxPoll(poll)
  with card, js{"card"}:
    result.card = some parseFxCard(card, js{"embed_card"}.getStr)
  with media, js{"media"}:
    parseFxMedia(media, result)
  with note, js{"community_note"}:
    result.note = expandFxNote(note)

  let article = js{"article"}
  let hasArticle = article{"title"}.getStr.len > 0
  if hasArticle:
    result.articlePreview = some ArticlePreview(
      title: article{"title"}.getStr,
      previewText: article{"preview_text"}.getStr,
      coverImage: article{"cover_media", "media_info", "original_img_url"}.getStr.fxImage,
      tweetId: result.id
    )

  result.expandFxText(js{"raw_text"}, js{"replying_to", "screen_name"}.getStr,
                      hideTwitter = result.quote.isSome, hideArticle = hasArticle)

  # Reposts arrive as the original post, naming the reposter.
  with reposter, js{"reposted_by"}:
    let original = result
    result = Tweet(
      id: original.id,
      time: original.time,
      available: true,
      user: User(
        id: reposter{"id"}.getStr,
        username: reposter{"screen_name"}.getStr,
        fullname: reposter{"name"}.getStr,
        userPic: reposter{"avatar_url"}.getStr.fxImage.multiReplace(("_normal", ""), ("_200x200", ""))
      ),
      retweet: some original
    )

proc parseFxTimeline*(js: JsonNode; after: string): Timeline =
  result = Timeline(beginning: after.len == 0, bottom: js{"cursor", "bottom"}.getStr)
  with results, js{"results"}:
    for status in results:
      let tweet = parseFxTweet(status)
      if tweet.id != 0:
        result.content.add @[tweet]

proc parseFxUsers*(js: JsonNode; after: string; kind = users): Result[User] =
  result = Result[User](beginning: after.len == 0, query: Query(kind: kind),
                        bottom: js{"cursor", "bottom"}.getStr)
  with results, js{"results"}:
    for user in results:
      let parsed = parseFxUser(user)
      if parsed.id.len > 0:
        result.content.add parsed

proc pageParams(count: int; after: string): seq[(string, string)] =
  result.add ("count", $count)
  if after.len > 0:
    result.add ("cursor", after)

proc getFxUser*(handle: string): Future[User] {.async.} =
  ## `handle` is a username, or `id:<rest_id>`.
  let js = await fxGet("profile", "profile/" & encodeUrl(handle), allowNotFound = true)
  if js{"code"}.getInt == 404:
    if js{"reason"}.getStr == "suspended":
      return User(suspended: true)
    return User()
  result = parseFxUser(js{"user"})

proc getFxUserTimeline*(userId, tab: string; count: int; after = "";
                        params: seq[(string, string)] = @[]): Future[Timeline] {.async.} =
  ## `tab` is `statuses` or `media`.
  let js = await fxGet("profile/" & tab, "profile/id:" & userId & "/" & tab,
                       pageParams(count, after) & params)
  result = parseFxTimeline(js, after)

proc getFxSearch*(query, feed: string; count: int; after = ""): Future[Timeline] {.async.} =
  ## `feed` is `latest`, `top` or `media`.
  let js = await fxGet("search", "search",
                       @[("q", query), ("feed", feed)] & pageParams(count, after))
  result = parseFxTimeline(js, after)

proc getFxUserSearch*(query: string; count: int; after = ""): Future[Result[User]] {.async.} =
  let js = await fxGet("search/users", "search/users",
                       @[("q", query)] & pageParams(count, after))
  result = parseFxUsers(js, after)

proc getFxConnections*(userId: string; kind: QueryKind; count: int;
                       after = ""): Future[Result[User]] {.async.} =
  ## `kind` is `followers` or `following`.
  let tab = $kind
  let js = await fxGet("profile/" & tab, "profile/id:" & userId & "/" & tab,
                       pageParams(count, after))
  result = parseFxUsers(js, after, kind)

proc getFxReposters*(id: string; count: int; after = ""): Future[UsersTimeline] {.async.} =
  let js = await fxGet("status/reposts", "status/" & id & "/reposts", pageParams(count, after))
  result = parseFxUsers(js, after)
