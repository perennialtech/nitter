# SPDX-License-Identifier: AGPL-3.0-only
import std/[asyncdispatch, unittest]
import ../src/[api, apiutils, consts]

proc checkTranslationCoalescing() {.async.} =
  setOnDemandTranslation(true, "en")
  let gate = newFuture[string]("mock Grok translation")
  var posts = 0
  proc translate(id, language, contentType: string): Future[string] =
    doAssert id == "123" and language == "en" and contentType == "POST"
    inc posts
    gate

  let
    first = translateOnDemand("123", post=translate)
    second = translateOnDemand("123", post=translate)
  doAssert posts == 1, "duplicate in-flight translations must be coalesced"
  gate.complete("data: {\"result\":{\"text\":\"translated\",\"source_language\":\"ja\"}}")
  doAssert (await first)
  doAssert (await second)
  doAssert (await translateOnDemand("123", post=translate))
  doAssert posts == 1, "cached translation must avoid another upstream POST"

proc checkTranslationFailure() {.async.} =
  setOnDemandTranslation(true, "en")
  let gate = newFuture[string]("failing translation")
  var posts = 0
  proc translate(id, language, contentType: string): Future[string] =
    inc posts
    gate

  let
    first = translateOnDemand("456", post=translate)
    second = translateOnDemand("456", post=translate)
  doAssert posts == 1
  gate.fail(newException(IOError, "mock upstream failure"))
  doAssert not (await first)
  doAssert not (await second)
  doAssert not (await translateOnDemand("456", post=translate))
  doAssert posts == 1, "failed translations must be briefly negative-cached"

suite "profile RSS compatibility":
  test "ordinary profile RSS retains reposts":
    check userTweetsUrl("123", "", rss=true).cookie.endpoint == graphUserTweets
    check userTweetsUrl("123", "").cookie.endpoint == graphUserTweetsOriginals
  test "with_replies RSS retains ordinary posts and replies":
    check userRepliesUrl("123", "", rss=true).cookie.endpoint == graphUserTweetsAndReplies
    check userRepliesUrl("123", "").cookie.endpoint == graphUserReplies

suite "on-demand translations":
  test "cached and concurrent translations share one upstream request":
    waitFor checkTranslationCoalescing()
  test "failed translations are coalesced and negatively cached":
    waitFor checkTranslationFailure()
