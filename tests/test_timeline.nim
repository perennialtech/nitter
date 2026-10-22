# SPDX-License-Identifier: AGPL-3.0-only
import std/[options, strutils, times, unittest]
import karax/vdom
import ../src/types
import ../src/views/[profile, search, timeline]

suite "pinned tweet rendering":
  let
    tweets = Timeline(beginning: true)
    pinned = Tweet(id: 1, text: "Pinned regression tweet", pinned: true,
                   time: fromUnix(0).utc,
                   user: User(username: "nim_lang", fullname: "Nim",
                              joinDate: fromUnix(0).utc))

  test "an absent pinned tweet is not rendered":
    let html = $renderTimelineTweets(tweets, Prefs(), "/nim_lang")
    check "Pinned regression tweet" notin html

  test "a pinned tweet is rendered":
    let html = $renderTimelineTweets(tweets, Prefs(), "/nim_lang", pinned)
    check "Pinned regression tweet" in html

  test "hidden pinned tweets are not rendered":
    let html = $renderTimelineTweets(tweets, Prefs(hidePins: true), "/nim_lang", pinned)
    check "Pinned regression tweet" notin html

  test "search rendering forwards the pinned tweet":
    let html = $renderTweetSearch(tweets, Prefs(), "/nim_lang", pinned)
    check "Pinned regression tweet" in html

  test "profile rendering unwraps the pinned tweet":
    var profile = Profile(user: pinned.user, tweets: tweets, pinned: some(pinned))
    let html = $renderProfile(profile, Prefs(), "/nim_lang")
    check "Pinned regression tweet" in html
