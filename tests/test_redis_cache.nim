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
