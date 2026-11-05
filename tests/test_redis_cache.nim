# SPDX-License-Identifier: AGPL-3.0-only
import std/[asyncdispatch, hashes, os, strutils, times, unittest]
from std/net import Port
import redis
import ../src/[redis_cache, types]

proc testUserIdExpiry() {.async.} =
  let
    port = parseInt(getEnv("NITTER_TEST_REDIS_PORT", "6379"))
    conn = await openAsync("127.0.0.1", Port(port))
    prefix = "nitter_expiry_test_" & $getTime().toUnix & "_" & $getCurrentProcessId()
    cfg = Config(redisHost: "127.0.0.1", redisPort: port,
                 redisConns: 1, redisMaxConns: 2, listCacheTime: 10)
    profileName = prefix & "_new"
  var
    keys: seq[string]
    markers: seq[tuple[key, value: string; expiresAt: int64]]

  try:
    # Model an existing cache whose historical migrations have completed.
    # On fresh Redis, the old flatty migration would delete pid:* fixtures.
    # An obsolete completion marker must never suppress later reconciliation.
    for marker in ["flatty", "snappyRss", "userBuckets", "profileDates",
                   "profileStats", "userType", "verifiedType", "pidExpiryV1"]:
      let
        previousValue = await conn.get(marker)
        previousTtl = int64(await conn.ttl(marker))
        expiresAt = (if previousTtl > 0:
                       getTime().toUnix + previousTtl
                     else: previousTtl)
      markers.add (marker, previousValue, expiresAt)
      await conn.setk(marker, "true")
    # Existing installations may already have thousands of permanent hashes.
    # More than one SCAN batch must be visited to give all of them an expiry.
    for i in 0 ..< 800:
      let
        name = prefix & "_legacy_" & $i
        key = "pid:" & $(hash(name) div 1_000_000)
      keys.add key
      discard await conn.hSet(key, name, $i)
      doAssert (await conn.ttl(key)) == -1

    setCacheTimes(cfg)
    await initRedisPool(cfg)

    var expired = false
    for attempt in 0 ..< 200:
      expired = true
      for key in keys:
        if (await conn.ttl(key)) <= 0:
          expired = false
          break
      if expired:
        break
      await sleepAsync(10)
    doAssert expired, "legacy user ID hashes must acquire a TTL"
    # Running a second pass must retain existing TTLs and catch late writes.
    discard await conn.expire(keys[0], 20)
    let lateName = prefix & "_late"
    let lateKey = "pid:" & $(hash(lateName) div 1_000_000)
    keys.add lateKey
    discard await conn.hSet(lateKey, lateName, "late")
    doAssert (await conn.ttl(lateKey)) == -1
    await expireLegacyUserIdsOnce()
    doAssert (await conn.ttl(lateKey)) > 0
    doAssert (await conn.ttl(keys[0])) <= 20,
      "reconciliation must not extend existing TTLs"

    let
      key = "pid:" & $(hash(profileName) div 1_000_000)
    keys.add key
    await cache(User(id: "12345", username: profileName,
                     joinDate: fromUnix(0).utc))
    doAssert (await conn.hGet(key, profileName)) == "12345"
    doAssert (await conn.ttl(key)) > 0, "new user ID hashes must expire"

    # Flatty has no schema version. An old positional User payload is not
    # readable after the User layout changes.
    let legacyValue = "old positional User cache"
    await conn.setk("p:" & profileName, legacyValue)
    await cache(User(id: "67890", username: profileName,
                     parodyLabel: "Fan", joinDate: fromUnix(0).utc))
    doAssert (await conn.get("p:" & profileName)) == legacyValue,
      "new User caching must not overwrite the old binary namespace"
    let cachedUser = await getCachedUser(profileName, fetch=false)
    doAssert cachedUser.id == "67890"
    doAssert cachedUser.parodyLabel == "Fan"

    # Community entries embed User values and share its binary layout.
    let communityId = prefix & "_community"
    await conn.setk("cm:" & communityId, legacyValue)
    await cache(Community(id: communityId, createdAt: fromUnix(0).utc,
                          creator: User(username: profileName,
                                        joinDate: fromUnix(0).utc)))
    doAssert (await conn.get("cm:" & communityId)) == legacyValue,
      "new Community caching must not overwrite the old binary namespace"
    let cachedCommunity = await getCachedCommunity(communityId)
    doAssert cachedCommunity.creator.username == profileName

    # Broadcasts and audio spaces also embed the changed User object.
    let broadcastId = prefix & "_broadcast"
    await conn.setk("bc:" & broadcastId, legacyValue)
    await cache(Broadcast(id: broadcastId,
                          startTime: fromUnix(0).utc,
                          endTime: fromUnix(0).utc,
                          user: User(joinDate: fromUnix(0).utc)))
    doAssert (await conn.get("bc:" & broadcastId)) == legacyValue,
      "new Broadcast caching must not overwrite the old binary namespace"

    let spaceId = prefix & "_space"
    await conn.setk("sp:" & spaceId, legacyValue)
    await cache(AudioSpace(id: spaceId, state: "ENDED",
                           startTime: fromUnix(0).utc,
                           endTime: fromUnix(0).utc,
                           creator: User(joinDate: fromUnix(0).utc)))
    doAssert (await conn.get("sp:" & spaceId)) == legacyValue,
      "new AudioSpace caching must not overwrite the old binary namespace"

  finally:
    try:
      for key in keys:
        discard await conn.del(key)
      discard await conn.del("p:" & profileName)
      discard await conn.del("p2:" & profileName)
      discard await conn.del("cm:" & (prefix & "_community"))
      discard await conn.del("cm2:" & (prefix & "_community"))
      discard await conn.del("bc:" & (prefix & "_broadcast"))
      discard await conn.del("bc2:" & (prefix & "_broadcast"))
      discard await conn.del("sp:" & (prefix & "_space"))
      discard await conn.del("sp2:" & (prefix & "_space"))
      for marker in markers:
        if marker.expiresAt == -2 or marker.expiresAt == 0 or
           (marker.expiresAt > 0 and marker.expiresAt <= getTime().toUnix):
          discard await conn.del(marker.key)
        else:
          await conn.setk(marker.key, marker.value)
          if marker.expiresAt > 0:
            discard await conn.expireAt(marker.key, int(marker.expiresAt))
    finally:
      await conn.close()

suite "Redis cache compatibility and expiration":
  test "user IDs expire and changed records have separate cache keys":
    waitFor testUserIdExpiry()
