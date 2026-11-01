# SPDX-License-Identifier: AGPL-3.0-only
import asyncdispatch, tables, times, hashes, uri

import jester

import router_utils, timeline
import ../query

include "../views/rss.nimf"

export times, hashes

proc redisKey*(page, name, cursor: string): string =
  result = page & ":" & name
  if cursor.len > 0:
    result &= ":" & cursor

proc timelineRss*(req: Request; cfg: Config; query: Query; prefs: Prefs): Future[Rss] {.async.} =
  var profile: Profile
  let
    name = req.params.getOrDefault("name")
    after = getCursor(req)
    names = getNames(name)

  if names.len == 1:
    profile = await fetchProfile(after, query, prefs, skipRail=true)
  else:
    var q = query
    q.fromUser = names
    profile.tweets = await getGraphTweetSearch(q, after)
    # this is kinda dumb
    profile.user = User(
      username: name,
      fullname: names.join(" | "),
      userpic: "https://abs.twimg.com/sticky/default_profile_images/default_profile.png"
    )

  if profile.user.suspended:
    return Rss(feed: profile.user.username, cursor: "suspended")

  if profile.user.fullname.len > 0:
    let rss = renderTimelineRss(profile, cfg, prefs, multi=(names.len > 1))
    return Rss(feed: rss, cursor: profile.tweets.bottom)

proc communityNotesRss*(tab: string; cfg: Config; prefs: Prefs): Future[Rss] {.async.} =
  let tl = if tab == "new": await getGraphCommunityNotesNew()
           else: await getGraphBirdwatchGlobalTimeline()

  # these timelines have no cursor; the cache treats an empty one as a miss, so
  # use the oldest tweet id as a stand-in for Min-Id
  var cursor = tl.bottom
  if cursor.len < 3 and tl.content.len > 0 and tl.content[^1].len > 0:
    cursor = $tl.content[^1][^1].id

  result = Rss(feed: renderCommunityNotesRss(tl.content, tab, cfg, prefs), cursor: cursor)

template respRss*(rss, page) =
  if rss.cursor.len == 0:
    let info = case page
               of "User": " \"" & @"name" & "\" "
               of "List": " \"" & @"id" & "\" "
               else: " "

    resp Http404, showError(page & info & "not found", cfg)
  elif rss.cursor.len == 9 and rss.cursor == "suspended":
    resp Http404, showError(getSuspended(@"name"), cfg)

  let headers = {"Content-Type": "application/rss+xml; charset=utf-8",
                 "Min-Id": rss.cursor}
  resp Http200, headers, rss.feed

proc createRssRouter*(cfg: Config) =
  router rss:
    get "/search/rss":
      if not cfg.enableRSSSearch:
        resp Http403, showError("RSS feed is disabled", cfg)
      if @"q".len > 200:
        resp Http400, showError("Search input too long.", cfg)

      let
        prefs = requestPrefs()
        query = initQuery(params(request))
      if query.kind notin {QueryKind.tweets, QueryKind.top, QueryKind.media}:
        resp Http400, showError("Only Tweet searches are allowed for RSS feeds.", cfg)

      let
        cursor = getCursor()
        key = redisKey("search", $hash(genQueryUrl(query)), cursor)

      var rss = await getCachedRss(key)
      if rss.cursor.len > 0:
        respRss(rss, "Search")

      let tweets = await getGraphTweetSearch(query, cursor)
      rss.cursor = tweets.bottom
      rss.feed = renderSearchRss(tweets.content, query.text, genQueryUrl(query), cfg, prefs)

      await cacheRss(key, rss)
      respRss(rss, "Search")

    get "/i/communitynotes/rss":
      if not cfg.enableRSSCommunityNotes or cfg.guestAuth:
        resp Http403, showError("RSS feed is disabled", cfg)
      let
        prefs = requestPrefs()
        key = redisKey("communitynotes", "helpful", "")

      var rss = await getCachedRss(key)
      if rss.cursor.len > 0:
        respRss(rss, "Community Notes")

      rss = await communityNotesRss("helpful", cfg, prefs)

      await cacheRss(key, rss)
      respRss(rss, "Community Notes")

    get "/i/communitynotes/new/rss":
      if not cfg.enableRSSCommunityNotes or cfg.guestAuth:
        resp Http403, showError("RSS feed is disabled", cfg)
      let
        prefs = requestPrefs()
        key = redisKey("communitynotes", "new", "")

      var rss = await getCachedRss(key)
      if rss.cursor.len > 0:
        respRss(rss, "Community Notes")

      rss = await communityNotesRss("new", cfg, prefs)

      await cacheRss(key, rss)
      respRss(rss, "Community Notes")

    get "/@name/rss":
      cond '.' notin @"name"
      if not cfg.enableRSSUserTweets:
        resp Http403, showError("RSS feed is disabled", cfg)
      let
        prefs = requestPrefs()
        name = @"name"
        key = redisKey("twitter", name, getCursor())

      var rss = await getCachedRss(key)
      if rss.cursor.len > 0:
        respRss(rss, "User")

      rss = await timelineRss(request, cfg, Query(fromUser: @[name]), prefs)

      await cacheRss(key, rss)
      respRss(rss, "User")

    get "/@name/@tab/rss":
      cond '.' notin @"name"
      cond @"tab" in ["with_replies", "media", "search", "articles", "reposts"]
      # articles/reposts can't be approximated by search, so multi-user is unsupported
      cond not (@"tab" in ["articles", "reposts"] and ',' in @"name")
      if not cfg.tabRssEnabled(@"tab"):
        resp Http403, showError("RSS feed is disabled", cfg)
      let
        prefs = requestPrefs()
        name = @"name"
        tab = @"tab"
        query = request.getQuery(tab, name, prefs)

      let searchKey = if tab != "search": ""
                      else: ":" & $hash(genQueryUrl(query))

      let key = redisKey(tab, name & searchKey, getCursor())

      var rss = await getCachedRss(key)
      if rss.cursor.len > 0:
        respRss(rss, "User")

      rss = await timelineRss(request, cfg, query, prefs)

      await cacheRss(key, rss)
      respRss(rss, "User")

    get "/@name/lists/@slug/rss":
      cond @"name" != "i"
      if not cfg.enableRSSList:
        resp Http403, showError("RSS feed is disabled", cfg)
      let
        slug = decodeUrl(@"slug")
        list = await getCachedList(@"name", slug)
        cursor = getCursor()

      if list.id.len == 0:
        resp Http404, showError("List \"" & @"slug" & "\" not found", cfg)

      let url = "/i/lists/" & list.id & "/rss"
      if cursor.len > 0:
        redirect(url & "?cursor=" & encodeUrl(cursor, false))
      else:
        redirect(url)

    get "/i/lists/@id/rss":
      if not cfg.enableRSSList:
        resp Http403, showError("RSS feed is disabled", cfg)
      let
        prefs = requestPrefs()
        id = @"id"
        cursor = getCursor()
        key = redisKey("lists", id, cursor)

      var rss = await getCachedRss(key)
      if rss.cursor.len > 0:
        respRss(rss, "List")

      let
        list = await getCachedList(id=id)
        timeline = await getGraphListTweets(list.id, cursor)
      rss.cursor = timeline.bottom
      rss.feed = renderListRss(timeline.content, list, cfg, prefs)

      await cacheRss(key, rss)
      respRss(rss, "List")
