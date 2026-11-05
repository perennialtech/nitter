# SPDX-License-Identifier: AGPL-3.0-only
import std/[asyncdispatch, strutils, unittest]
import packedjson
import ../src/[api, apiutils, consts, formatters, parserutils, trends, utils]

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

suite "upstream link rendering":
  test "translated URLs escape HTML attributes":
    let entities = parseJson("""{"urls":[{"indices":[4,8],"expanded_url":"https://example.org/\" onmouseover=\"alert(1)"}]}""")
    let html = expandTranslation("see link", entities)
    check "href=\"https://example.org/&quot; onmouseover=&quot;alert(1)\"" in html
    check "\" onmouseover=\"" notin html

  test "translated notes leave non-web URLs as text":
    let entities = parseJson("""[{"from_index":4,"to_index":8,"ref":{"url":"javascript:alert(1)"}}]""")
    check expandNoteTranslation("see link", entities) == "see link"

  test "invalid note ranges preserve the escaped original text":
    let entities = parseJson("""[{"fromIndex":4,"toIndex":6,"ref":{"url":"https://example.org"}}]""")
    check expandBirdwatchEntities("<b>", entities) == "&lt;b&gt;"
    check expandBirdwatchEntities("<b>", parseJson("null")) == "&lt;b&gt;"

  test "note entity offsets link the full text and support both field spellings":
    let
      camel = parseJson("""[{"fromIndex":0,"toIndex":4,"ref":{"url":"https://x.com/test"}}]""")
      snake = parseJson("""[{"from_index":0,"to_index":4,"ref":{"url":"https://x.com/test"}}]""")
      expected = "<a href=\"/test\">link</a>"
    check expandBirdwatchEntities("link", camel) == expected
    check expandBirdwatchEntities("link", snake) == expected

  test "Twitter paths remain on their original host if they resemble a hostname":
    let url = "https://x.com//example.org/path"
    check localizeTwitterLink(url) == url
    check localizeTwitterLink("https://x.com/\\example.org/path") ==
      "https://x.com/\\example.org/path"
    check localizeTwitterLink("https://x.com?src=1") == "/?src=1"
    check linkifyBirdwatchUrls(url) ==
      "<a href=\"" & url & "\">" & url & "</a>"

proc checkTrendsFailure() {.async.} =
  let gate = newFuture[seq[Trend]]("failed trends request")
  var requests = 0
  proc fetch(woeid: int): Future[seq[Trend]] =
    doAssert woeid == 23424977
    inc requests
    gate

  let
    first = getTrends(23424977, 10, fetch)
    second = getTrends(23424977, 10, fetch)
  doAssert requests == 1
  gate.complete(@[])
  doAssert (await first).len == 0
  doAssert (await second).len == 0
  doAssert (await getTrends(23424977, 10, fetch)).len == 0
  doAssert requests == 1

suite "homepage trends":
  test "concurrent requests share refreshes and failures are cached":
    waitFor checkTrendsFailure()

suite "translated links":
  test "upstream URL text cannot break out of the href attribute":
    let link = "https://t.co/example"
    let expanded = "https://example.com/?x=\" onmouseover=\"alert(1)&y=2"
    let html = expandGrokTranslation(link, @[(url: link, expanded: expanded)])
    check html == "<a href=\"https://example.com/?x=&quot; onmouseover=&quot;alert(1)&amp;y=2\">" &
                  "https://example.com/?x=&quot; onmouseover=&quot;alert(1)&amp;y=2</a>"
  test "unsafe schemes remain plain text":
    let link = "https://t.co/example"
    check expandGrokTranslation(link, @[(url: link, expanded: "javascript:alert(1)")]) == link
  test "link every occurrence in the original translated text":
    let
      source = "https://example.com/?a=1&b=2"
      target = "https://example.org/"
      anchor = "<a href=\"" & target & "\">" & target & "</a>"
      translated = expandGrokTranslation("猫 " & source & " and " & source,
                                         @[(url: source, expanded: target)])
    check translated == "猫 " & anchor & " and " & anchor
