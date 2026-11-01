# SPDX-License-Identifier: AGPL-3.0-only
import std/[json, strutils, times, unittest]
import ../src/[auth, types]

let request = ApiReq(cookie: ApiUrl(endpoint: "UserTweets"))

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
