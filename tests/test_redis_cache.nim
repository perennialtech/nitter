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
                 redisConns: 1, redisMaxConns: 2)
  var keys: seq[string]

  try:
    # Model an existing cache whose historical migrations have completed.
    # On fresh Redis, the old flatty migration would delete pid:* fixtures.
    for marker in ["flatty", "snappyRss", "userBuckets", "profileDates",
                   "profileStats", "userType", "verifiedType"]:
      await conn.setk(marker, "true")
    # An obsolete completion marker must never suppress later reconciliation.
    await conn.setk("pidExpiryV1", "true")
    # Existing installations may already have thousands of permanent hashes.
    # More than one SCAN batch must be visited to give all of them an expiry.
    for i in 0 ..< 800:
      let
        name = prefix & "_legacy_" & $i
        key = "pid:" & $(hash(name) div 1_000_000)
      keys.add key
      discard await conn.hSet(key, name, $i)
      doAssert (await conn.ttl(key)) == -1

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
      name = prefix & "_new"
      key = "pid:" & $(hash(name) div 1_000_000)
    keys.add key
    await cache(User(id: "12345", username: name,
                     joinDate: fromUnix(0).utc))
    doAssert (await conn.hGet(key, name)) == "12345"
    doAssert (await conn.ttl(key)) > 0, "new user ID hashes must expire"
  finally:
    for key in keys:
      discard await conn.del(key)
    await conn.close()

suite "user ID Redis cache expiration":
  test "new and legacy ID mappings expire":
    waitFor testUserIdExpiry()
