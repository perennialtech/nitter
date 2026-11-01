#SPDX-License-Identifier: AGPL-3.0-only
import std/[asyncdispatch, times, json, random, strutils, tables, packedsets, os]
import types, consts
import experimental/parser/session

const
  hourInSeconds = 60 * 60
  guestMaxReqs* = 50
  guestMaxAgeSeconds* = 2 * hourInSeconds

var
  sessionPool: seq[Session]
  guestPool: seq[Session]
  guestPoolSize = 1
  enableLogging = false
  # max requests at a time per session to avoid race conditions
  maxConcurrentReqs = 2

proc setMaxConcurrentReqs*(reqs: int) =
  if reqs > 0:
    maxConcurrentReqs = reqs

proc setGuestPoolSize*(size: int) =
  if size > 0:
    guestPoolSize = size

template log(str: varargs[string, `$`]) =
  echo "[sessions] ", str.join("")

proc endpoint*(req: ApiReq; session: Session): string =
  case session.kind
  of oauth: req.oauth.endpoint
  of cookie, guest: req.cookie.endpoint

proc pretty*(session: Session): string =
  if session.isNil:
    return "<null>"

  if session.kind == guest:
    return "guest"
  elif session.id > 0 and session.username.len > 0:
    result = $session.id & " (" & session.username & ")"
  elif session.username.len > 0:
    result = session.username
  elif session.id > 0:
    result = $session.id
  else:
    result = "<unknown>"
  result = $session.kind & " " & result

proc snowflakeToEpoch(flake: int64): int64 =
  int64(((flake shr 22) + 1288834974657) div 1000)

proc needsRotation*(session: Session): bool =
  if session.isNil or session.kind != guest:
    return false
  session.guestReqs >= guestMaxReqs or
    (epochTime().int - session.guestCreated) >= guestMaxAgeSeconds

proc getSessionPoolHealth*(): JsonNode =
  let now = epochTime().int

  var
    totalReqs = 0
    limited: PackedSet[int64]
    reqsPerApi: Table[string, int]
    oldest = now.int64
    newest = 0'i64
    average = 0'i64
    oauthTotal, cookieTotal = 0
    oauthLimited, cookieLimited = 0
    guestTotal, guestLimited, guestNeedsRotation = 0

  for session in sessionPool:
    let created = snowflakeToEpoch(session.id)
    if created > newest:
      newest = created
    if created < oldest:
      oldest = created
    average += created

    case session.kind
    of oauth: inc oauthTotal
    of cookie: inc cookieTotal
    of guest: discard

    if session.limited:
      limited.incl session.id
      case session.kind
      of oauth: inc oauthLimited
      of cookie: inc cookieLimited
      of guest: discard

    for api in session.apis.keys:
      let
        apiStatus = session.apis[api]
        reqs = apiStatus.limit - apiStatus.remaining

      # no requests made with this session and endpoint since the limit reset
      if apiStatus.reset < now:
        continue

      reqsPerApi.mgetOrPut($api, 0).inc reqs
      totalReqs.inc reqs

  guestTotal = guestPool.len
  for session in guestPool:
    if session.limited: inc guestLimited
    if session.needsRotation: inc guestNeedsRotation

    for api in session.apis.keys:
      let
        apiStatus = session.apis[api]
        reqs = apiStatus.limit - apiStatus.remaining

      if apiStatus.reset < now:
        continue

      reqsPerApi.mgetOrPut("guest:" & $api, 0).inc reqs
      totalReqs.inc reqs

  if sessionPool.len > 0:
    average = average div sessionPool.len
  else:
    oldest = 0
    average = 0

  return %*{
    "sessions": %*{
      "total": sessionPool.len,
      "limited": limited.card,
      "oauth": %*{"total": oauthTotal, "limited": oauthLimited},
      "cookie": %*{"total": cookieTotal, "limited": cookieLimited},
      "guest": %*{
        "total": guestTotal,
        "limited": guestLimited,
        "pendingRotation": guestNeedsRotation,
        "poolSize": guestPoolSize
      },
      "oldest": $fromUnix(oldest),
      "newest": $fromUnix(newest),
      "average": $fromUnix(average)
    },
    "requests": %*{
      "total": totalReqs,
      "apis": reqsPerApi
    }
  }

proc getSessionPoolDebug*(): JsonNode =
  let now = epochTime().int
  var list = newJObject()

  for i, session in sessionPool:
    let idStr = $session.id
    let sessionJson = %*{
      "kind": $session.kind,
      "userId": (if idStr.len > 4: "..." & idStr[^4..^1] else: idStr),
      "apis": newJObject(),
      "pending": session.pending,
    }

    if session.limited:
      sessionJson["limited"] = %true

    for api in session.apis.keys:
      let
        apiStatus = session.apis[api]
        obj = %*{}

      if apiStatus.reset > now.int:
        obj["remaining"] = %apiStatus.remaining
        obj["reset"] = %apiStatus.reset

      if "remaining" notin obj:
        continue

      sessionJson{"apis", $api} = obj

    list["acct-" & $i] = sessionJson

  for i, session in guestPool:
    let sessionJson = %*{
      "kind": $session.kind,
      "apis": newJObject(),
      "pending": session.pending,
      "requests": session.guestReqs,
      "ageSeconds": epochTime().int - session.guestCreated,
      "needsRotation": session.needsRotation
    }

    if session.limited:
      sessionJson["limited"] = %true

    for api in session.apis.keys:
      let
        apiStatus = session.apis[api]
        obj = %*{}

      if apiStatus.reset > now.int:
        obj["remaining"] = %apiStatus.remaining
        obj["reset"] = %apiStatus.reset

      if "remaining" notin obj:
        continue

      sessionJson{"apis", $api} = obj

    list["guest-" & $i] = sessionJson

  return %list

proc rateLimitError*(): ref RateLimitError =
  newException(RateLimitError, "rate limited")

proc noSessionsError*(): ref NoSessionsError =
  newException(NoSessionsError, "no sessions available")

proc isLimited(session: Session; req: ApiReq): bool =
  if session.isNil:
    return true

  let api = req.endpoint(session)
  if session.limited and api != graphUserTweetsV2:
    if (epochTime().int - session.limitedAt) > hourInSeconds:
      session.limited = false
      log "resetting limit: ", session.pretty
      return false
    else:
      return true

  if api in session.apis:
    let limit = session.apis[api]
    return limit.remaining <= 10 and limit.reset > epochTime().int
  else:
    return false

proc isReady(session: Session; req: ApiReq): bool =
  not (session.isNil or session.pending > maxConcurrentReqs or session.isLimited(req))

proc isGuestReady(session: Session; req: ApiReq): bool =
  not (session.isNil or session.pending >= maxConcurrentReqs or
       session.needsRotation or session.isLimited(req))

proc guestPoolFull*(): bool =
  guestPool.len >= guestPoolSize

proc addGuestSession*(session: Session) =
  guestPool.add session

proc registerGuestRequest*(session: Session) =
  if not session.isNil and session.kind == guest:
    inc session.guestReqs

proc invalidate*(session: var Session) =
  if session.isNil: return
  log "invalidating: ", session.pretty

  # TODO: This isn't sufficient, but it works for now
  case session.kind
  of guest:
    let idx = guestPool.find(session)
    if idx > -1: guestPool.delete(idx)
  of oauth, cookie:
    let idx = sessionPool.find(session)
    if idx > -1: sessionPool.delete(idx)
  session = nil

proc release*(session: Session) =
  if session.isNil: return
  dec session.pending

proc getGuestSession*(req: ApiReq): Session =
  for session in guestPool:
    if session.isGuestReady(req):
      inc session.pending
      return session

proc findStaleGuestSession*(req: ApiReq): Session =
  for session in guestPool:
    if session.needsRotation or session.isLimited(req):
      return session

proc getSession*(req: ApiReq): Future[Session] {.async.} =
  for i in 0 ..< sessionPool.len:
    if result.isReady(req): break
    result = sessionPool.sample()

  if not result.isNil and result.isReady(req):
    inc result.pending
  else:
    if result.isNil:
      log "no sessions available for API: ", req.cookie.endpoint
    else:
      log "no sessions available for API: ", req.endpoint(result), ", last tried: ", result.pretty
    raise noSessionsError()

proc selectCookieSession*(sessions: openArray[Session]; req: ApiReq): Session =
  ## Grok translation requires a cookie session. Choosing from the complete
  ## account pool can miss every cookie by chance, even when one is available.
  for session in sessions:
    if session.kind != cookie or session.pending >= maxConcurrentReqs or
       session.isLimited(req):
      continue
    if result.isNil or session.pending < result.pending:
      result = session
  if result.isNil:
    raise noSessionsError()
  inc result.pending

proc getCookieSession*(req: ApiReq): Session =
  selectCookieSession(sessionPool, req)

proc setLimited*(session: Session; req: ApiReq) =
  let api = req.endpoint(session)
  session.limited = true
  session.limitedAt = epochTime().int
  log "rate limited by api: ", api, ", reqs left: ", session.apis[api].remaining, ", ", session.pretty

proc setRateLimit*(session: Session; req: ApiReq; remaining, reset, limit: int) =
  # avoid undefined behavior in race conditions
  let api = req.endpoint(session)
  if api in session.apis:
    let rateLimit = session.apis[api]
    if rateLimit.reset >= reset and rateLimit.remaining < remaining:
      return
    if rateLimit.reset == reset and rateLimit.remaining >= remaining:
      session.apis[api].remaining = remaining
      return

  session.apis[api] = RateLimit(limit: limit, remaining: remaining, reset: reset)

proc initSessionPool*(cfg: Config; path: string) =
  enableLogging = cfg.enableDebug

  if path.endsWith(".json"):
    log "ERROR: .json is not supported, the file must be a valid JSONL file ending in .jsonl"
    quit 1

  if not fileExists(path):
    log "ERROR: ", path, " not found. This file is required to authenticate API requests."
    quit 1

  log "parsing JSONL account sessions file: ", path
  for line in path.lines:
    sessionPool.add parseSession(line)

  log "successfully added ", sessionPool.len, " valid account sessions"
