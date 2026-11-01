# SPDX-License-Identifier: AGPL-3.0-only
import std/[asyncdispatch, json, strutils, times, unittest]
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
