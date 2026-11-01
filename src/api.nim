# SPDX-License-Identifier: AGPL-3.0-only
import options, asyncdispatch, httpclient, strutils, sequtils, sugar, tables, times
import packedjson
import types, query, formatters, consts, apiutils, parser, parserutils, utils
import experimental/parser

# Helper to generate params object for GraphQL requests
proc genParams(variables: string; fieldToggles = ""; features = gqlFeatures): seq[(string, string)] =
  result.add ("variables", variables)
  result.add ("features", features)
  if fieldToggles.len > 0:
    result.add ("fieldToggles", fieldToggles)

proc apiUrl(endpoint, variables: string; fieldToggles = ""; skipTid = false;
            features = gqlFeatures): ApiUrl =
  return ApiUrl(endpoint: endpoint, params: genParams(variables, fieldToggles, features), skipTid: skipTid)

proc apiReq(endpoint, variables: string; fieldToggles = ""; skipTid = false;
            features = gqlFeatures): ApiReq =
  let url = apiUrl(endpoint, variables, fieldToggles, skipTid, features)
  return ApiReq(cookie: url, oauth: url)

proc cursorParam(after: string): string =
  ## JSON-escape the user-supplied cursor so it cannot break out of the GraphQL
  ## variables object (same input-validation class as the #1411 media SSRF).
  if after.len > 0: "\"cursor\":" & $(%after) & "," else: ""

proc mediaUrl(id, cursor: string): ApiReq =
  return apiReq(graphUserMedia, userMediaVars % [id, cursor, "100"])

proc userTweetsUrl(id: string; cursor: string): ApiReq =
  let endpoint = if isGuestAuth(): graphUserTweets else: graphUserTweetsOriginals
  return apiReq(endpoint, userTweetsVars % [id, cursor, "20"], userTweetsFieldToggles)

proc userTweetsAndRepliesUrl(id: string; cursor: string): ApiReq =
  return apiReq(graphUserTweetsAndReplies, userTweetsAndRepliesVars % [id, cursor], userTweetsFieldToggles)

proc userRepliesUrl(id: string; cursor: string): ApiReq =
  return apiReq(graphUserReplies, userRepliesVars % [id, cursor], userRepliesFieldToggles)

proc userArticlesUrl(id: string; cursor: string): ApiReq =
  return apiReq(graphUserArticles, userArticlesVars % [id, cursor], userTweetsFieldToggles)

proc userRepostsUrl(id: string; cursor: string): ApiReq =
  return apiReq(graphUserReposts, userRepostsVars % [id, cursor], userTweetsFieldToggles)

proc tweetDetailUrl(id, cursor: string; mode = Relevance): ApiReq =
  return apiReq(graphTweetDetail, tweetDetailVars % [id, cursor, $mode], tweetDetailFieldToggles)

proc userUrl(username: string): ApiReq =
  let cookieVars = $(%*{"screen_name": username, "withGrokTranslatedBio": false})
  result = ApiReq(
    cookie: apiUrl(graphUser, cookieVars, tweetDetailFieldToggles),
    oauth: apiUrl(graphUserV2, $(%*{"screen_name": username}))
  )

proc getGraphUser*(username: string): Future[User] {.async.} =
  if username.len == 0: return
  let js = await fetchRaw(userUrl(username))
  result = parseGraphUser(js)

proc getGraphUserById*(id: string): Future[User] {.async.} =
  if id.len == 0 or id.any(c => not c.isDigit): return
  let
    url = apiReq(graphUserById, userByRestIdVars % id)
    js = await fetchRaw(url)
  result = parseGraphUser(js)

proc getAboutAccount*(username: string): Future[AccountInfo] {.async.} =
  if username.len == 0: return
  let
    url = apiReq(graphAboutAccount, $(%*{"screenName": username}))
    js = await fetch(url)
  result = parseAboutAccount(js)

proc restReq(endpoint: string; params: seq[(string, string)] = @[]): ApiReq =
  let url = ApiUrl(endpoint: endpoint, params: params)
  ApiReq(cookie: url, oauth: url)

proc getBroadcastInfo*(id: string): Future[Broadcast] {.async.} =
  if id.len == 0: return
  let
    req = apiReq(graphBroadcast, $(%*{"id": id}))
    js = await fetch(req)
  result = parseBroadcastInfo(js)

proc fetchBroadcastStream*(mediaKey: string): Future[string] {.async.} =
  if mediaKey.len == 0: return
  let
    streamReq = restReq(restLiveStream & mediaKey)
    streamJs = await fetch(streamReq)
  result = streamJs{"source", "noRedirectPlaybackUrl"}.getStr(
    streamJs{"source", "location"}.getStr)

proc getAudioSpace*(id: string): Future[AudioSpace] {.async.} =
  if id.len == 0: return
  let
    variables = %*{
      "id": id,
      "isMetatagsQuery": false,
      "withReplays": true,
      "withListeners": true
    }
    req = apiReq(graphAudioSpace, $variables)
    js = await fetch(req)
  result = parseAudioSpace(js)

proc getGraphUserTweets*(id: string; kind: TimelineKind; after=""): Future[Profile] {.async.} =
  if id.len == 0: return
  let
    cursor = cursorParam(after)
    url = case kind
      of TimelineKind.tweets: userTweetsUrl(id, cursor)
      of TimelineKind.replies: userRepliesUrl(id, cursor)
      of TimelineKind.media: mediaUrl(id, cursor)
      of TimelineKind.articles: userArticlesUrl(id, cursor)
      of TimelineKind.reposts: userRepostsUrl(id, cursor)
    js = await fetch(url)
  result = parseGraphTimeline(js, after)

proc getGraphCommunity*(id: string): Future[Community] {.async.} =
  if id.len == 0: return
  let
    url = apiReq(graphCommunity, $(%*{"communityId": id}))
    js = await fetch(url)
  result = parseGraphCommunity(js)

proc getGraphCommunityTweets*(id: string; rankingMode: string; after=""): Future[Timeline] {.async.} =
  if id.len == 0: return
  let
    cursor = cursorParam(after)
    url = apiReq(graphCommunityTweets, communityTweetsVars % [id, cursor, rankingMode])
    js = await fetch(url)
  result = parseGraphCommunityTimeline(js, after)

proc getGraphCommunityMedia*(id: string; after=""): Future[Timeline] {.async.} =
  if id.len == 0: return
  let
    cursor = cursorParam(after)
    url = apiReq(graphCommunityMedia, communityMediaVars % [id, cursor])
    js = await fetch(url)
  result = parseGraphCommunityTimeline(js, after)

proc communitySliceReq(endpoint, variables: string): ApiReq =
  let url = ApiUrl(endpoint: endpoint, params: @[("variables", variables)])
  ApiReq(cookie: url, oauth: url)

proc getGraphCommunityMembers*(id: string; after=""): Future[Result[User]] {.async.} =
  if id.len == 0: return
  let
    cursor = if after.len > 0: $(%after) else: "null"
    url = communitySliceReq(graphCommunityMembers, communityMembersVars % [id, cursor])
    js = await fetch(url)
  result = parseGraphCommunityMembers(js, after)

proc getGraphCommunityModerators*(id: string): Future[Result[User]] {.async.} =
  if id.len == 0: return
  let
    url = communitySliceReq(graphCommunityModerators, communityMembersVars % [id, "null"])
    js = await fetch(url)
  result = parseGraphCommunityMembers(js)

proc getGraphCommunityHashtags*(id, hashtag: string; after=""): Future[Timeline] {.async.} =
  if id.len == 0 or hashtag.len == 0: return
  let
    safeTag = multiReplace(hashtag, ("\"", ""), ("\\", ""))
    cursor = cursorParam(after)
    url = apiReq(graphCommunityHashtags, communityHashtagsVars % [id, cursor, safeTag])
    js = await fetch(url)
  result = parseGraphCommunityTimeline(js, after)

proc getGraphListTweets*(id: string; after=""): Future[Timeline] {.async.} =
  if id.len == 0: return
  let
    cursor = cursorParam(after)
    url = apiReq(graphListTweets, restIdVars % [id, cursor, "20"])
    js = await fetch(url)
  result = parseGraphTimeline(js, after).tweets

proc getGraphListBySlug*(name, list: string): Future[List] {.async.} =
  let
    variables = %*{"screenName": name, "listSlug": list}
    url = apiReq(graphListBySlug, $variables)
    js = await fetch(url)
  result = parseGraphList(js)

proc getGraphList*(id: string): Future[List] {.async.} =
  let 
    url = apiReq(graphListById, $(%*{"listId": id}))
    js = await fetch(url)
  result = parseGraphList(js)

proc getGraphListMembers*(list: List; after=""): Future[Result[User]] {.async.} =
  if list.id.len == 0: return
  var
    variables = %*{
      "listId": list.id,
      "withBirdwatchPivots": false,
      "withDownvotePerspective": false,
      "withReactionsMetadata": false,
      "withReactionsPerspective": false
    }
  if after.len > 0:
    variables["cursor"] = % after
  let 
    url = apiReq(graphListMembers, $variables)
    js = await fetchRaw(url)
  result = parseGraphListMembers(js, after)

proc getGraphUserConnections(userId: string; endpoint: string; kind: QueryKind;
                              after=""): Future[Result[User]] {.async.} =
  if userId.len == 0: return
  var variables = %*{
    "userId": userId,
    "count": 20,
    "includePromotedContent": false,
    "withGrokTranslatedBio": true
  }
  if after.len > 0:
    variables["cursor"] = %after
  let
    url = apiReq(endpoint, $variables)
    js = await fetchRaw(url)
  result = parseGraphFollowers(js, after, kind)

proc getGraphFollowers*(userId: string; after=""): Future[Result[User]] {.async.} =
  result = await getGraphUserConnections(userId, graphFollowers, followers, after)

proc getGraphFollowing*(userId: string; after=""): Future[Result[User]] {.async.} =
  result = await getGraphUserConnections(userId, graphFollowing, following, after)

proc getGraphTweetResult*(id: string): Future[Tweet] {.async.} =
  if id.len == 0: return
  let
    url = apiReq(graphTweetResult, $(%*{"rest_id": id}))
    js = await fetch(url)
  result = parseGraphTweetResult(js)

proc getTweetByRestId*(id: string): Future[Tweet] {.async.} =
  if id.len == 0: return
  let
    url = apiReq(graphTweetResultByRestId, tweetByRestIdVars % id, articleFieldToggles)
    js = await fetch(url)
  result = parseTweetByRestId(js)

proc getTweetTranslation*(id: string): Future[Tweet] {.async.} =
  if id.len == 0: return
  let
    url = apiReq(graphTweetResultByRestId, tweetTransVars % id,
                 tweetTransFieldToggles)
    js = await fetch(url)
  result = parseTweetByRestId(js)

type
  GrokLink = tuple[url, expanded: string]

  OnDemandTrans = object
    text, lang: string
    at: int

const
  onDemandCacheTtl = 3600
  onDemandCacheMax = 500

var onDemandCache: Table[string, OnDemandTrans]

proc parseGrokTranslation(body: string): tuple[text, lang: string; links: seq[GrokLink]] =
  for line in body.splitLines:
    var l = line.strip
    if l.startsWith("data:"):
      l = l[5 .. ^1].strip
    if l.len == 0 or l[0] != '{': continue
    try:
      let res = parseJson(l){"result"}
      result.text.add res{"text"}.getStr
      if result.lang.len == 0:
        result.lang = res{"source_language"}.getStr
      let entities = res{"rich_text_entities"}
      if entities.kind == JArray:
        for e in entities:
          let r = e{"ref"}
          result.links.add (
            url: r{"url"}.getStr,
            expanded: r{"expandedUrl"}.getStr(r{"expanded_url"}.getStr)
          )
    except CatchableError:
      discard

proc cacheOnDemand(key, text, lang: string) =
  let now = epochTime().int
  if onDemandCache.len >= onDemandCacheMax:
    var expired: seq[string]
    for k, v in onDemandCache:
      if now - v.at > onDemandCacheTtl: expired.add k
    for k in expired: onDemandCache.del k
    if onDemandCache.len >= onDemandCacheMax:
      onDemandCache.clear()
  onDemandCache[key] = OnDemandTrans(text: text, lang: lang, at: now)

proc cachedOnDemand(key: string; found: var OnDemandTrans): bool =
  if key notin onDemandCache: return
  found = onDemandCache[key]
  if epochTime().int - found.at > onDemandCacheTtl:
    onDemandCache.del key
    return
  result = true

proc applyOnDemandCache*(note: var BirdwatchNote) =
  if not onDemandTranslation() or not note.isTranslatable or note.id.len == 0: return
  var cached: OnDemandTrans
  if not cachedOnDemand("n" & note.id, cached): return
  note.translation = cached.text
  note.transLang = cached.lang
  note.transTarget = onDemandTranslationLang()
  note.translatable = true

proc applyOnDemandCache*(tweet: Tweet) =
  if not onDemandTranslation() or tweet == nil: return

  var cached: OnDemandTrans
  if tweet.isTranslatable and tweet.translation.len == 0 and cachedOnDemand($tweet.id, cached):
    tweet.translation = cached.text
    tweet.transLang = cached.lang
    tweet.transTarget = onDemandTranslationLang()
    tweet.translatable = true

  if tweet.noteIsTranslatable and tweet.noteId.len > 0 and tweet.noteTranslation.len == 0 and
     cachedOnDemand("n" & tweet.noteId, cached):
    tweet.noteTranslation = cached.text
    tweet.noteTransLang = cached.lang
    tweet.noteTransTarget = onDemandTranslationLang()
    tweet.noteTranslatable = true

  if tweet.quote.isSome:
    let quote = tweet.quote.get
    if quote.noteIsTranslatable and quote.noteId.len > 0 and quote.noteTranslation.len == 0 and
       cachedOnDemand("n" & quote.noteId, cached):
      quote.noteTranslation = cached.text
      quote.noteTransLang = cached.lang
      quote.noteTransTarget = onDemandTranslationLang()
      quote.noteTranslatable = true

proc translateTweet*(tweet: Tweet) {.async.} =
  if not enableTranslation() or tweet == nil: return

  var quoted: Tweet
  if tweet.quote.isSome:
    let quote = tweet.quote.get
    if quote.translatable or quote.noteTranslatable or
       (onDemandTranslation() and quote.noteIsTranslatable):
      quoted = quote

  let
    onDemand = onDemandTranslation() and tweet.isTranslatable
    onDemandNote = onDemandTranslation() and tweet.noteIsTranslatable

  if not (tweet.translatable or tweet.noteTranslatable or onDemand or onDemandNote or
          quoted != nil): return
  tweet.showTrans = true
  if quoted != nil: quoted.showTrans = true

  applyOnDemandCache(tweet)

  proc missing(t: Tweet; onDemand = false): bool =
    t != nil and (((t.translatable or onDemand) and t.translation.len == 0) or
                  (t.noteTranslatable and t.noteTranslation.len == 0))

  if not (tweet.missing(onDemand) or quoted.missing): return

  try:
    let trans = await getTweetTranslation($tweet.id)
    if trans == nil: return
    if trans.translation.len > 0:
      tweet.translation = trans.translation
      tweet.transLang = trans.transLang
      tweet.translatable = true
    if trans.noteTranslation.len > 0:
      tweet.noteTranslation = trans.noteTranslation
      tweet.noteTransLang = trans.noteTransLang
    if quoted != nil and trans.quote.isSome:
      let tq = trans.quote.get
      if quoted.translation.len == 0 and tq.translation.len > 0:
        quoted.translation = tq.translation
        quoted.transLang = tq.transLang
      if quoted.noteTranslation.len == 0 and tq.noteTranslation.len > 0:
        quoted.noteTranslation = tq.noteTranslation
        quoted.noteTransLang = tq.noteTransLang
  except CatchableError:
    echo "Translation fetch failed for ", tweet.id, ": ", getCurrentExceptionMsg()

proc translateOnDemand*(id: string; note = false): Future[bool] {.async.} =
  if not onDemandTranslation() or id.len == 0: return
  try:
    let
      body = await postGrokTranslation(id, onDemandTranslationLang(),
                                       if note: "COMMUNITY_NOTE" else: "POST")
      parsed = body.parseGrokTranslation
    result = parsed.text.len > 0
    if result:
      cacheOnDemand((if note: "n" & id else: id),
                    expandGrokTranslation(parsed.text, parsed.links), parsed.lang)
    elif body.len > 0:
      echo "[translation] no translated text in grok response for ", id, ": ",
           body[0 ..< min(body.len, 300)]
  except CatchableError:
    echo "[translation] on-demand translation failed for ", id, ": ", getCurrentExceptionMsg()

proc getGraphTweet(id: string; after=""; mode = Relevance): Future[Conversation] {.async.} =
  if id.len == 0: return
  let
    cursor = cursorParam(after)
    js = await fetch(tweetDetailUrl(id, cursor, mode))
  result = parseGraphConversation(js, id)

proc getReplies*(id, after: string; mode = Relevance): Future[Result[Chain]] {.async.} =
  result = (await getGraphTweet(id, after, mode)).replies
  result.beginning = after.len == 0

proc getTweet*(id: string; after=""; mode = Relevance): Future[Conversation] {.async.} =
  result = await getGraphTweet(id, mode=mode)
  if after.len > 0:
    result.replies = await getReplies(id, after, mode)

proc getGraphEditHistory*(id: string): Future[EditHistory] {.async.} =
  if id.len == 0: return
  let
    url = apiReq(graphTweetEditHistory, tweetEditHistoryVars % id)
    js = await fetch(url)
  result = parseGraphEditHistory(js, id)

proc getGraphRetweeters*(id: string; after=""): Future[UsersTimeline] {.async.} =
  if id.len == 0: return
  let
    cursor = cursorParam(after)
    variables = reactorsVars % [id, cursor]
    url = apiReq(graphRetweeters, variables)
    js = await fetch(url)
  result = parseGraphRetweetersTimeline(js, after)

proc getGraphBirdwatchNotes*(id: string): Future[BirdwatchNotes] {.async.} =
  if id.len == 0: return
  let
    url = apiReq(graphBirdwatchNotes, birdwatchNotesVars % id, features=gqlFeatures)
    js = await fetch(url)
  result = parseBirdwatchNotes(js)

proc getGraphBirdwatchHistory*(alias: string; after=""): Future[BirdwatchHistory] {.async.} =
  if alias.len == 0: return
  let
    cursor = cursorParam(after)
    url = apiReq(graphBirdwatchContributorNotes, birdwatchContributorNotesVars % [alias, cursor],
                 features=gqlFeatures)
    js = await fetch(url)
  result = parseBirdwatchHistory(js)

proc getGraphBirdwatchOneNote*(noteId: string): Future[BirdwatchSingleNote] {.async.} =
  if noteId.len == 0: return
  let
    url = apiReq(graphBirdwatchOneNote, birdwatchOneNoteVars % noteId, features=gqlFeatures)
    js = await fetch(url)
  result = parseBirdwatchOneNote(js)

proc getGraphBirdwatchGlobalTimeline*(): Future[Timeline] {.async.} =
  let
    url = apiReq(graphBirdwatchGlobalTimeline, birdwatchGlobalTimelineVars, features=gqlFeatures)
    js = await fetch(url)
  result = parseBirdwatchGlobalTimeline(js)

proc getGraphCommunityNotesNew*(): Future[Timeline] {.async.} =
  let
    url = apiReq(graphGenericTimelineById, genericTimelineVars % communityNotesNewTimelineId,
                 features=gqlFeatures)
    js = await fetch(url)
  result = parseGenericTweetTimeline(js)

proc getGraphTweetSearch*(query: Query; after=""): Future[Timeline] {.async.} =
  # workaround for #1372
  let maxId =
    if not after.startsWith("maxid:"): ""
    else: validateNumber(after[6..^1])

  let q = genQueryParam(query, maxId)
  if q.len == 0 or q == emptyQuery:
    return Timeline(query: query, beginning: true)

  let product =
    case query.kind
    of top: "Top"
    # profile media feeds (RSS, multi-user timelines) must stay chronological
    of media: (if query.fromUser.len == 0: "Media" else: "Latest")
    else: "Latest"

  var
    variables = %*{
      "rawQuery": q,
      "count": 20,
      "querySource": "typed_query",
      "product": product,
      "withGrokTranslatedBio":true,
      "withQuickPromoteEligibilityTweetFields":false,
      "withBirdwatchNotes":true
    }

  if after.len > 0 and maxId.len == 0:
    variables["cursor"] = % after
  let
    url = apiReq(graphSearchTimeline, $variables)
    js = await fetch(url)
  result = parseGraphSearch[Tweets](js, after)
  result.query = query

  # when no more items are available the API just returns the last page in
  # full. this detects that and clears the page instead.
  let prefix = min(64, min(after.len, result.bottom.len))
  if prefix > 0 and maxId.len == 0 and
     after[0..<prefix] == result.bottom[0..<prefix]:
    result.content.setLen(0)

proc getGraphProductSearch[T](query: Query; product: string;
                              after=""): Future[Result[T]] {.async.} =
  if query.text.len == 0:
    return Result[T](query: query, beginning: true)

  var
    variables = %*{
      "rawQuery": query.text,
      "count": 20,
      "querySource": "typed_query",
      "product": product,
      "withGrokTranslatedBio":true,
      "withQuickPromoteEligibilityTweetFields":false
    }
  if after.len > 0:
    variables["cursor"] = % after

  let
    url = apiReq(graphSearchTimeline, $variables)
    js = await fetch(url)
  result = parseGraphSearch[T](js, after)
  result.query = query

proc getGraphUserSearch*(query: Query; after=""): Future[Result[User]] =
  getGraphProductSearch[User](query, "People", after)

proc getGraphListSearch*(query: Query; after=""): Future[Result[ListSearchResult]] =
  getGraphProductSearch[ListSearchResult](query, "Lists", after)

proc getPhotoRail*(id: string): Future[PhotoRail] {.async.} =
  if id.len == 0: return
  let js = await fetch(mediaUrl(id, ""))
  result = parseGraphPhotoRail(js)

proc getGraphArticle*(id: string): Future[Article] {.async.} =
  if id.len == 0: return
  let
    url = apiReq(graphTweetResultByRestId, articleVars % id, articleFieldToggles)
    json = await fetchRaw(url)
  result = parseGraphArticle(json)

proc getGraphTweetResults*(ids: seq[string]): Future[seq[Tweet]] {.async.} =
  if ids.len == 0: return
  let
    idsJson = "[" & ids.mapIt("\"" & it & "\"").join(",") & "]"
    url = apiReq(graphTweetResultsByRestIds, articleBatchVars % idsJson, articleFieldToggles)
    js = await fetch(url)
  result = parseGraphTweetResults(js)

proc resolve*(url: string; prefs: Prefs): Future[string] {.async.} =
  let client = newAsyncHttpClient(maxRedirects=0)
  try:
    let resp = await client.request(url, HttpHead)
    result = resp.headers["location"].replaceUrls(prefs)
  except:
    discard
  finally:
    client.close()
