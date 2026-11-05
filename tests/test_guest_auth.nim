# SPDX-License-Identifier: AGPL-3.0-only
import std/[asyncdispatch, httpcore, json, strutils, times, unittest, uri]
import ../src/[auth, apiutils, types]

let request = ApiReq(cookie: ApiUrl(endpoint: "UserTweets"))

proc checkConcurrentGuestActivation() {.async.} =
  let gate = newFuture[Session]("guest activation")
  var activations = 0
  proc activate(): Future[Session] =
    inc activations
    gate

  setGuestPoolSize(1)
  let
    first = getReadyGuestSession(request, activate)
    second = getReadyGuestSession(request, activate)
  doAssert activations == 1, "concurrent calls must share one activation"

  var guestSession = Session(kind: guest, guestToken: "shared",
                             guestCreated: epochTime().int)
  gate.complete(guestSession)
  try:
    let
      a = await first
      b = await second
    doAssert a == guestSession and b == guestSession
    doAssert guestSession.pending == 2
    doAssert getSessionPoolHealth()["sessions"]["guest"]["total"].getInt == 1
    release(a)
    release(b)
  finally:
    invalidate(guestSession)

proc checkConcurrentGuestPoolGrowth() {.async.} =
  let gates = [newFuture[Session]("first activation"),
               newFuture[Session]("second activation")]
  var activations = 0
  proc activate(): Future[Session] =
    result = gates[activations]
    inc activations

  setGuestPoolSize(2)
  setMaxConcurrentReqs(1)
  var
    a = Session(kind: guest, guestToken: "first", guestCreated: epochTime().int)
    b = Session(kind: guest, guestToken: "second", guestCreated: epochTime().int)
  try:
    let
      first = getReadyGuestSession(request, activate)
      second = getReadyGuestSession(request, activate)
    doAssert activations == 1, "activations must remain serialized"

    gates[0].complete(a)
    doAssert (await first) == a
    await sleepAsync(0)
    doAssert activations == 2, "a waiter must use free pool capacity"
    gates[1].complete(b)
    doAssert (await second) == b
    release(a)
    release(b)
  finally:
    invalidate(a)
    invalidate(b)
    setGuestPoolSize(1)
    setMaxConcurrentReqs(2)

proc checkFailedGuestActivation() {.async.} =
  let gate = newFuture[Session]("failed guest activation")
  var activations = 0
  proc activate(): Future[Session] =
    inc activations
    gate

  let
    first = getReadyGuestSession(request, activate)
    second = getReadyGuestSession(request, activate)
  doAssert activations == 1
  gate.fail(newException(IOError, "upstream unavailable"))

  for pending in [first, second]:
    var refused = false
    try:
      discard await pending
    except RateLimitError:
      refused = true
    doAssert refused, "activation failures must propagate to all waiters"

  let recovered = newFuture[Session]("recovered guest activation")
  proc retry(): Future[Session] = recovered
  let next = getReadyGuestSession(request, retry)
  var guestSession = Session(kind: guest, guestToken: "recovered",
                             guestCreated: epochTime().int)
  recovered.complete(guestSession)
  try:
    doAssert (await next) == guestSession
    release(guestSession)
  finally:
    invalidate(guestSession)

suite "guest sessions":
  test "guest mode uses the runtime bearer token":
    expect ValueError:
      setGuestAuth(" ", 1)
    check not isGuestAuth()
    setGuestAuth("guest-bearer-fixture", 1)
    let
      session = Session(kind: guest, guestToken: "guest-fixture")
      headers = waitFor genHeaders(session, parseUri("https://x.com/i/api/graphql/test"), true)
    check headers["authorization"] == "Bearer guest-bearer-fixture"
    check headers["x-guest-token"] == "guest-fixture"

  test "guest authentication tokens are absent from diagnostics":
    var guestSession = Session(kind: guest, guestToken: "sensitive-guest-token",
                               guestCreated: epochTime().int)
    addGuestSession(guestSession)
    try:
      check guestSession.pretty == "guest"
      let debug = $getSessionPoolDebug()
      check "guest-0" in debug
      check "sensitive-guest-token" notin debug
    finally:
      invalidate(guestSession)

  test "a busy guest does not hide an available guest":
    var busy = Session(kind: guest, guestToken: "busy",
                       guestCreated: epochTime().int, pending: 2)
    var ready = Session(kind: guest, guestToken: "ready",
                        guestCreated: epochTime().int)
    addGuestSession(busy)
    addGuestSession(ready)
    try:
      let selected = getGuestSession(request)
      check selected == ready
      check selected.pending == 1
      release(selected)
      check selected.pending == 0
    finally:
      invalidate(busy)
      invalidate(ready)

  test "expired guest sessions are not selected":
    var expired = Session(kind: guest, guestToken: "expired",
                          guestCreated: epochTime().int - guestMaxAgeSeconds - 1)
    addGuestSession(expired)
    try:
      check expired.needsRotation
      check getGuestSession(request).isNil
    finally:
      invalidate(expired)

  test "concurrent activation respects configured pool size":
    waitFor checkConcurrentGuestActivation()

  test "waiting callers can fill free guest pool slots":
    waitFor checkConcurrentGuestPoolGrowth()

  test "failed activation releases the reservation":
    waitFor checkFailedGuestActivation()

  test "cookie selection does not accidentally choose OAuth":
    let sessions = @[
      Session(kind: oauth, oauthToken: "oauth", pending: 0),
      Session(kind: oauth, oauthToken: "oauth2", pending: 0),
      Session(kind: cookie, authToken: "auth", ct0: "csrf", pending: 0)
    ]
    let selected = selectCookieSession(sessions, request)
    check selected == sessions[2]
    check selected.pending == 1
    release(selected)
    check selected.pending == 0

  test "cookie selection respects busy sessions":
    let sessions = @[
      Session(kind: cookie, authToken: "busy", ct0: "csrf", pending: 2),
      Session(kind: cookie, authToken: "ready", ct0: "csrf", pending: 0)
    ]
    let selected = selectCookieSession(sessions, request)
    check selected == sessions[1]
    release(selected)

  test "Grok 429s cool down only the translation endpoint":
    let
      translation = ApiReq(cookie: ApiUrl(endpoint: "2/grok/translation.json"))
      sessions = [
        Session(kind: cookie, authToken: "first", ct0: "csrf"),
        Session(kind: cookie, authToken: "second", ct0: "csrf")
      ]
      headers = newHttpHeaders({"retry-after": "120"})
    recordTranslationRateLimit(sessions[0], translation, Http429, headers)
    let
      nextTranslation = selectCookieSession(sessions, translation)
      nextTimeline = selectCookieSession(sessions, request)
    check nextTranslation == sessions[1]
    check nextTimeline == sessions[0]
    release(nextTranslation)
    release(nextTimeline)
