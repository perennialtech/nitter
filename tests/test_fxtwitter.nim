# SPDX-License-Identifier: AGPL-3.0-only
import std/[options, sequtils, strutils, times, unittest]
import packedjson
import ../src/[types, fxtwitter, apistats]

const author = """{
  "type": "profile", "id": "11348282", "screen_name": "NASA", "name": "NASA",
  "description": "Making the seemingly impossible, possible.",
  "raw_description": {"text": "Missions at https://t.co/abc and @NASAWebb #space",
    "facets": [{"type": "url", "indices": [12, 28], "original": "https://t.co/abc",
                "replacement": "https://www.nasa.gov/missions", "display": "nasa.gov/missions"}]},
  "location": "Pale Blue Dot", "url": "https://x.com/NASA",
  "avatar_url": "https://pbs.twimg.com/profile_images/1/0ZxKlEKB_200x200.jpg",
  "banner_url": "https://pbs.twimg.com/profile_banners/11348282/1788992290",
  "followers": 92248416, "following": 115, "statuses": 74538, "media_count": 28248,
  "likes": 17106, "protected": false, "joined": "Wed Dec 19 20:20:32 +0000 2007",
  "website": {"url": "http://www.nasa.gov/", "display_url": "nasa.gov"},
  "verification": {"verified": true, "type": "government"}
}"""

proc status(body: string): string =
  """{"type": "status", "id": "2120974110335312297", "created_timestamp": 1794514608,
      "author": """ & author & """, "replies": 145, "reposts": 576, "quotes": 36,
      "likes": 3974, "views": 397031, "source": null, "embed_card": "tweet", """ & body & "}"

suite "FxTwitter users":
  let user = parseFxUser(parseJson(author))

  test "profile fields map to a Nitter user":
    check user.id == "11348282"
    check user.username == "NASA"
    check user.userPic == "profile_images/1/0ZxKlEKB.jpg"
    check user.banner == "profile_banners/11348282/1788992290/1500x500"
    check user.tweets == 74538
    check user.media == 28248
    check user.website == "http://www.nasa.gov/"
    check user.verifiedType == government
    check user.joinDate.year == 2007

  test "bio links, mentions and hashtags are expanded":
    check "href=\"https://www.nasa.gov/missions\"" in user.bio
    check "href=\"/NASAWebb\"" in user.bio
    check "#space</a>" in user.bio

suite "FxTwitter posts":
  test "text facets expand and trailing media links are cut":
    let tweet = parseFxTweet(parseJson(status("""
      "raw_text": {"text": "Tethys &amp; Dione via @NASAWebb #Saturn https://t.co/link https://t.co/media",
        "display_text_range": [0, 63],
        "facets": [
          {"type": "mention", "indices": [23, 32], "original": "NASAWebb", "id": "1"},
          {"type": "hashtag", "indices": [33, 40], "original": "Saturn"},
          {"type": "url", "indices": [41, 63], "original": "https://t.co/link",
           "replacement": "https://science.nasa.gov/saturn/", "display": "science.nasa.gov/saturn/"},
          {"type": "media", "indices": [64, 87], "original": "https://t.co/media"}]},
      "media": {"all": [{"type": "photo", "url": "https://pbs.twimg.com/media/HW82tqYWoAAnWCO.jpg?name=orig",
                         "altText": "Saturn"}]}""")))
    check tweet.id == 2120974110335312297
    check tweet.stats.retweets == 576
    check tweet.stats.views == 397031
    check tweet.text.startsWith("Tethys &amp; Dione via <a href=\"/NASAWebb\"")
    check "href=\"/search?f=tweets&q=%23Saturn\">#Saturn</a>" in tweet.text
    check "href=\"https://science.nasa.gov/saturn/\"" in tweet.text
    check "t.co/media" notin tweet.text
    check tweet.media.len == 1
    check tweet.media[0].photo.url == "media/HW82tqYWoAAnWCO.jpg"
    check tweet.media[0].photo.altText == "Saturn"

  test "leading reply mentions become the reply list":
    let tweet = parseFxTweet(parseJson(status("""
      "replying_to": {"screen_name": "_itsforkb", "status": "2121073525142507992"},
      "raw_text": {"text": "@_itsforkb @other take care", "display_text_range": [18, 27],
        "facets": [{"type": "mention", "indices": [0, 10], "original": "_itsforkb"},
                   {"type": "mention", "indices": [11, 17], "original": "other"}]}""")))
    check tweet.replyId == 2121073525142507992
    check tweet.reply == @["_itsforkb", "other"]
    check tweet.text == "take care"

  test "quotes hide their trailing link":
    let tweet = parseFxTweet(parseJson(status("""
      "raw_text": {"text": "look https://t.co/q", "display_text_range": [0, 19],
        "facets": [{"type": "url", "indices": [5, 19], "original": "https://t.co/q",
                    "replacement": "https://x.com/MCRmyPH/status/2120863244055384470"}]},
      "quote": {"type": "tombstone", "reason": "deleted", "message": "This post was deleted"}""")))
    check tweet.text == "look"
    check tweet.quote.isSome
    check tweet.quote.get.text == "This post was deleted"

  test "videos keep their variants":
    let tweet = parseFxTweet(parseJson(status("""
      "raw_text": {"text": "", "display_text_range": [0, 0], "facets": []},
      "media": {"all": [{"type": "video", "duration": 29.7,
        "url": "https://video.twimg.com/amplify_video/1/vid/avc1/1080x1350/a.mp4?tag=29",
        "thumbnail_url": "https://pbs.twimg.com/amplify_video_thumb/1/img/b.jpg",
        "formats": [
          {"container": "m3u8", "url": "https://video.twimg.com/amplify_video/1/pl/c.m3u8?tag=29"},
          {"container": "mp4", "bitrate": 632000,
           "url": "https://video.twimg.com/amplify_video/1/vid/avc1/320x400/d.mp4?tag=29"}]}]}""")))
    check tweet.media.len == 1
    let video = tweet.media[0].video
    check video.thumb == "amplify_video_thumb/1/img/b.jpg"
    check video.durationMs == 29700
    check video.variants.len == 2
    check video.variants[0].contentType == m3u8
    check video.variants[1].bitrate == 632000
    check video.variants[1].resolution == 400

  test "reposts wrap the original post":
    let tweet = parseFxTweet(parseJson(status("""
      "raw_text": {"text": "orig", "display_text_range": [0, 4], "facets": []},
      "reposted_by": {"id": "12", "name": "jack", "screen_name": "jack"}""")))
    check tweet.user.username == "jack"
    check tweet.retweet.isSome
    check tweet.retweet.get.user.username == "NASA"
    check tweet.retweet.get.text == "orig"

suite "FxTwitter lists":
  test "timelines keep the bottom cursor":
    let tl = parseFxTimeline(parseJson("""{"code": 200, "cursor": {"top": "t", "bottom": "b"},
      "results": [""" & status(""""raw_text": {"text": "a", "display_text_range": [0, 1], "facets": []}""") &
      "]}"), "")
    check tl.beginning
    check tl.bottom == "b"
    check tl.content.len == 1

  test "user lists carry their query kind":
    let users = parseFxUsers(parseJson("""{"code": 200, "cursor": {"top": null, "bottom": "b"},
      "results": [""" & author & "]}"), "c", followers)
    check not users.beginning
    check users.query.kind == followers
    check users.content[0].username == "NASA"

suite "API call stats":
  test "calls are counted per backend, endpoint and outcome":
    resetApiStats()
    recordCall("fx", "profile", "ok")
    recordCall("fx", "profile", "ok")
    recordCall("sessions", "UserTweets", "limited")
    check formatApiStats() == "fx profile ok=2, sessions UserTweets limited=1"

proc post(id, user: string; replyTo = ""): string =
  let reply = if replyTo.len > 0: """{"screen_name": "x", "status": """" & replyTo & """"}"""
              else: "null"
  """{"type": "status", "id": """" & id & """", "created_timestamp": 1794514608,
      "author": {"type": "profile", "id": """" & user & """", "screen_name": "u""" & user &
      """", "name": "u", "raw_description": {"text": "", "facets": []}},
      "replying_to": """ & reply & """,
      "raw_text": {"text": "post """ & id & """", "display_text_range": [0, 7], "facets": []}}"""

suite "FxTwitter conversations":
  # Thread order as FxTwitter returns it: parent, root, then the focal post.
  let js = parseJson("""{"code": 200, "cursor": {"bottom": "next"},
    "status": """ & post("30", "1", "20") & """,
    "thread": [""" & post("20", "2", "10") & ", " & post("10", "1") & ", " & post("30", "1", "20") & """],
    "replies": [""" &
      post("31", "1", "30") & ", " & post("32", "1", "31") & ", " &
      post("40", "3", "30") & ", " & post("41", "1", "40") & ", " &
      post("50", "4", "30") & "]}")

  test "ancestors are ordered from the root":
    let conv = parseFxConversation(js)
    check conv.tweet.id == 30
    check conv.before.content.mapIt(it.id) == @[10'i64, 20]

  test "the author's continuation becomes the self-thread":
    let conv = parseFxConversation(js)
    check conv.after.content.mapIt(it.id) == @[31'i64, 32]

  test "replies regroup into chains":
    let conv = parseFxConversation(js)
    check conv.replies.beginning
    check conv.replies.bottom == "next"
    check conv.replies.content.mapIt(it.content.mapIt(it.id)) == @[@[40'i64, 41], @[50'i64]]

  test "later pages have no self-thread":
    let conv = parseFxConversation(js, after = "c")
    check conv.after.content.len == 0
    check not conv.replies.beginning
    check conv.replies.content.len == 3
