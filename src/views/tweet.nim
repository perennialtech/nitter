# SPDX-License-Identifier: AGPL-3.0-only
import strutils, sequtils, strformat, options, algorithm
import karax/[karaxdsl, vdom, vstyles]
from jester import Request

import renderutils
import ".."/[types, utils, formatters, apiutils]

const doctype = "<!DOCTYPE html>\n"

proc renderMiniAvatar*(user: User; prefs: Prefs): VNode =
  genImg(user.getUserPic("_mini"), class=(prefs.getAvatarClass & " mini"))

proc renderArticleCard(preview: ArticlePreview; prefs: Prefs): VNode =
  let url = "/i/article/" & $preview.tweetId
  buildHtml(tdiv(class="article-card card large")):
    a(class="card-container", href=url):
      if preview.coverImage.len > 0:
        tdiv(class="card-image-container"):
          tdiv(class="card-image"):
            genImg(preview.coverImage)
          span(class="article-card-badge"): text "Article"
      tdiv(class="card-content-container"):
        tdiv(class="card-content"):
          h2(class="card-title"): text preview.title
          if preview.previewText.len > 0:
            p(class="card-description"): text preview.previewText

proc renderHeader(tweet: Tweet; retweet: string; retweeter: string; retweetTweet: Tweet;
                   pinned: bool; prefs: Prefs; path = ""): VNode =
  buildHtml(tdiv):
    if pinned:
      let pinnedLabel =
        if "/i/communities/" in path: "Pinned by Community mods"
        else: "Pinned Tweet"
      tdiv(class="pinned"):
        span: icon("pin", pinnedLabel)
    elif retweet.len > 0:
      tdiv(class="retweet-header"):
        a(href="/" & retweeter):
          icon("retweet", retweet & " retweeted")
        if showRetweetTimeEnabled():
          span(class="tweet-date retweet-date"):
            span(class="retweet-date-sep"):
              text " · "
            a(href=getLink(retweetTweet), title=retweetTweet.getTime):
              text retweetTweet.getShortTime

    tdiv(class="tweet-header"):
      a(class="tweet-avatar", href=("/" & tweet.user.username)):
        var size = "_bigger"
        if not prefs.autoplayGifs and tweet.user.userPic.endsWith("gif"):
          size = "_400x400"
        genImg(tweet.user.getUserPic(size), class=prefs.getAvatarClass)

      tdiv(class="tweet-name-row"):
        tdiv(class="fullname-and-username"):
          linkUser(tweet.user, class="fullname")
          verifiedIcon(tweet.user)
          linkUser(tweet.user, class="username")

        span(class="tweet-date"):
          a(href=getLink(tweet), title=tweet.getTime):
            text tweet.getShortTime

      profileLabel(tweet.user)

proc renderAltText(altText: string): VNode =
  buildHtml(p(class="alt-text")):
    text "ALT  " & altText

proc renderPhotoAttachment(photo: Photo; bigThumb=false): VNode =
  buildHtml(tdiv(class="attachment")):
    let
      named = "name=" in photo.url
      thumb = if named: photo.url
              elif bigThumb: photo.url & mediumWebp
              else: photo.url & smallWebp
    a(href=getOrigPicUrl(photo.url), class="still-image", target="_blank"):
      genImg(thumb, alt=photo.altText)
    if photo.altText.len > 0:
      renderAltText(photo.altText)

proc isPlaybackEnabled(prefs: Prefs; playbackType: VideoType): bool =
  case playbackType
  of mp4: prefs.mp4Playback
  of m3u8, vmap: prefs.hlsPlayback

proc hasMp4Url(video: Video): bool =
  video.variants.anyIt(it.contentType == mp4)

proc renderVideoDisabled(playbackType: VideoType; path=""): VNode =
  buildHtml(tdiv(class="video-overlay")):
    case playbackType
    of mp4:
      buttonReferer "/enablemp4", "Enable mp4 playback", path
    of m3u8, vmap:
      buttonReferer "/enablehls", "Enable hls playback", path

proc renderVideoUnavailable(video: Video): VNode =
  buildHtml(tdiv(class="video-overlay")):
    case video.reason
    of "dmcaed":
      p: text "This media has been disabled in response to a report by the copyright owner"
    else:
      p: text "This media is unavailable"

proc getVideoDownloadUrl(videoData: Video): string =
  let mp4Vars = videoData.variants.filterIt(it.contentType == mp4)
  if mp4Vars.len == 0: return ""
  let best = mp4Vars.sortedByIt(it.bitrate)[^1].url
  if best.startsWith("http"): getVidUrl(best) else: best

proc renderVideoAttachment(videoData: Video; prefs: Prefs; path=""; bigThumb=false): VNode =
  let
    playbackType = if not prefs.proxyVideos and videoData.hasMp4Url: mp4
                   else: videoData.playbackType
    thumb = if bigThumb: getMediumPic(videoData.thumb) else: getSmallPic(videoData.thumb)

  buildHtml(tdiv(class="attachment")):
    if not videoData.available:
      img(src=thumb, loading="lazy")
      renderVideoUnavailable(videoData)
    elif not prefs.isPlaybackEnabled(playbackType):
      img(src=thumb, loading="lazy")
      renderVideoDisabled(playbackType, path)
    else:
      let
        vars = videoData.variants.filterIt(it.contentType == playbackType)
        vidUrl = vars.sortedByIt(it.resolution)[^1].url
        source = if prefs.proxyVideos and vidUrl.startsWith("http"):
                   getVidUrl(vidUrl) else: vidUrl
      case playbackType
      of mp4:
        video(poster=thumb, controls="", muted=prefs.muteVideos):
          source(src=source, `type`="video/mp4")
      of m3u8, vmap:
        video(poster=thumb, data-url=source, data-autoload="false", muted=prefs.muteVideos)
        verbatim "<div class=\"video-overlay\" onclick=\"playVideo(this)\">"
        tdiv(class="overlay-circle"): span(class="overlay-triangle")
        if videoData.durationMs > 0:
          tdiv(class="overlay-duration"): text getDuration(videoData)
        verbatim "</div>"
    if videoData.available:
      let dlUrl = getVideoDownloadUrl(videoData)
      if dlUrl.len > 0:
        a(class="video-download", href=dlUrl, download="",
          title="Download video"): icon "download-alt"

proc renderVideo*(video: Video; prefs: Prefs; path: string; bigThumb=false): VNode =
  let hasCardContent = video.description.len > 0 or video.title.len > 0

  buildHtml(tdiv(class="attachments card")):
    tdiv(class=("gallery-video" & (if hasCardContent: " card-container" else: ""))):
      renderVideoAttachment(video, prefs, path, bigThumb)
      if hasCardContent:
        tdiv(class="card-content"):
          h2(class="card-title"): text video.title
          if video.description.len > 0:
            p(class="card-description"): text video.description

proc renderGifAttachment(gif: Gif; prefs: Prefs; path=""): VNode =
  let thumb = getSmallPic(gif.thumb)

  buildHtml(tdiv(class="attachment")):
    if not prefs.mp4Playback:
      img(src=thumb, loading="lazy")
      renderVideoDisabled(mp4, path)
    elif prefs.autoplayGifs:
      video(class="gif", poster=thumb, autoplay="", muted="", loop=""):
        source(src=getPicUrl(gif.url), `type`="video/mp4")
    else:
      video(class="gif", poster=thumb, controls="", muted="", loop=""):
        source(src=getPicUrl(gif.url), `type`="video/mp4")
    if gif.altText.len > 0:
      renderAltText(gif.altText)

proc renderGif(gif: Gif; prefs: Prefs; path=""): VNode =
  buildHtml(tdiv(class="attachments media-gif")):
    renderGifAttachment(gif, prefs, path)

proc renderMedia(media: seq[Media]; prefs: Prefs; path: string; bigThumb=false): VNode =
  if media.len == 0:
    return nil

  if media.len == 1:
    let item = media[0]
    if item.kind == videoMedia:
      return renderVideo(item.video, prefs, path, bigThumb)
    if item.kind == gifMedia:
      return renderGif(item.gif, prefs, path)

  let
    groups = if media.len < 3: @[media]
             else: media.distribute(2)

  buildHtml(tdiv(class="attachments")):
    for i, mediaGroup in groups:
      let margin = if i > 0: ".25em" else: ""
      let rowClass = "gallery-row" &
                     (if mediaGroup.allIt(it.kind == photoMedia): "" else: " mixed-row")
      tdiv(class=rowClass, style={marginTop: margin}):
        for mediaItem in mediaGroup:
          case mediaItem.kind
          of photoMedia:
            renderPhotoAttachment(mediaItem.photo, bigThumb)
          of videoMedia:
            renderVideoAttachment(mediaItem.video, prefs, path, bigThumb)
          of gifMedia:
            renderGifAttachment(mediaItem.gif, prefs, path)

proc renderPoll(poll: Poll): VNode =
  buildHtml(tdiv(class="poll")):
    for i in 0 ..< poll.options.len:
      let
        leader = if poll.leader == i: " leader" else: ""
        val = poll.values[i]
        perc = if val > 0: val / poll.votes * 100 else: 0
        percStr = (&"{perc:>3.0f}").strip(chars={'.'}) & '%'
      tdiv(class=("poll-meter" & leader)):
        span(class="poll-choice-bar", style={width: percStr})
        span(class="poll-choice-value"): text percStr
        span(class="poll-choice-option"): text poll.options[i]
    span(class="poll-info"):
      text &"{insertSep($poll.votes, ',')} votes • {poll.status}"

proc renderCardImage(card: Card): VNode =
  buildHtml(tdiv(class="card-image-container")):
    tdiv(class="card-image"):
      genImg(card.image)
      if card.kind == player:
        tdiv(class="card-overlay"):
          tdiv(class="overlay-circle"):
            span(class="overlay-triangle")

proc renderCardContent(card: Card): VNode =
  buildHtml(tdiv(class="card-content")):
    h2(class="card-title"): text card.title
    if card.text.len > 0:
      p(class="card-description"): text card.text
    if card.dest.len > 0:
      span(class="card-destination"): text card.dest

proc renderCard(card: Card; prefs: Prefs; path: string): VNode =
  const smallCards = {app, player, summary, storeLink}
  let large = if card.kind notin smallCards: " large" else: ""
  let url = replaceUrls(card.url, prefs)

  buildHtml(tdiv(class=("card" & large))):
    if card.video.isSome:
      tdiv(class="card-container"):
        renderVideo(get(card.video), prefs, path)
        a(class="card-content-container", href=url):
          renderCardContent(card)
    else:
      a(class="card-container", href=url):
        if card.image.len > 0:
          renderCardImage(card)
        tdiv(class="card-content-container"):
          renderCardContent(card)

func formatStat(stat: int; abbreviate: bool): string =
  if stat <= 0: return ""
  if not abbreviate:
    return insertSep($stat, ',')
  if stat >= 1_000_000:
    formatFloat(stat.float / 1_000_000, ffDecimal, 1) & "M"
  elif stat >= 1_000:
    formatFloat(stat.float / 1_000, ffDecimal, 1) & "K"
  else:
    $stat

proc renderStats*(stats: TweetStats; prefs: Prefs; tweet: Tweet): VNode =
  buildHtml(tdiv(class="tweet-stats")):
    span(class="tweet-stat"): icon "comment", formatStat(stats.replies, prefs.abbreviateStats)
    a(href="/" & tweet.user.username & "/status/" & $tweet.id & "/retweets", class="tweet-stat", title="Retweets"):
      icon "retweet", formatStat(stats.retweets, prefs.abbreviateStats)
    a(href="/" & tweet.user.username & "/status/" & $tweet.id & "/quotes", class="tweet-stat", title="Quote Tweets"):
      icon "quote", formatStat(stats.quotes, prefs.abbreviateStats)
    span(class="tweet-stat"): icon "heart", formatStat(stats.likes, prefs.abbreviateStats)
    span(class="tweet-stat"): icon "views", formatStat(stats.views, prefs.abbreviateStats)
    if tweet.hasBirdwatch:
      a(href="/i/birdwatch/t/" & $tweet.id, class="tweet-stat", title="Community Notes"):
        icon "group"
    if not prefs.hideTweetSource:
      span(class="tweet-stat source"): text stripHtml(stats.source.replace("Twitter for ", "").replace("Twitter ", ""))

proc renderReply(tweet: Tweet): VNode =
  buildHtml(tdiv(class="replying-to")):
    text "Replying to "
    for i, u in tweet.reply:
      if i > 0: text " "
      a(href=("/" & u)): text "@" & u

proc renderAttribution(user: User; prefs: Prefs; link = ""): VNode =
  let href = if link.len > 0: link else: "/" & user.username
  buildHtml(a(class="attribution", href=href)):
    renderMiniAvatar(user, prefs)
    strong: text user.fullname
    verifiedIcon(user)

proc renderMediaTags(tags: seq[User]): VNode =
  buildHtml(tdiv(class="media-tag-block")):
    icon "user"
    for i, p in tags:
      a(class="media-tag", href=("/" & p.username), title=p.username):
        text p.fullname
      if i < tags.high:
        text ", "

proc renderLatestPost(username: string; id: int64): VNode =
  buildHtml(tdiv(class="latest-post-version")):
    text "There's a new version of this post. "
    a(href=getLink(id, username)):
      text "See the latest post"

proc translateUrl(tweet: Tweet; path: string; mainTweet: bool): string =
  if mainTweet:
    let base = path.split('#')[0]
    return base & (if '?' in base: "&" else: "?") & "tr=1#m"
  getLink(tweet, focus=false) & "?tr=1#m"

proc withTrParam(url, value: string): string =
  let
    parts = url.split('#', 1)
    base = parts[0].split('?', 1)
  var params: seq[string]
  if base.len > 1:
    params = base[1].split('&').filterIt(it.len > 0 and not it.startsWith("tr="))
  params.add "tr=" & value
  result = base[0] & "?" & params.join("&")
  if parts.len > 1:
    result &= "#" & parts[1]

proc renderTranslateLabel*(active, requested: bool; lang, url: string;
                           target = ""): VNode =
  buildHtml(tdiv(class="tweet-translate")):
    if active:
      if target.len > 0:
        text "Translated to " & langName(target)
      elif lang.len > 0:
        text "Translated from " & langName(lang)
      else:
        text "Translated"
      if url.len > 0:
        text " · "
        a(href=url.withTrParam("0")): text "show original"
    elif requested:
      text "Translation unavailable"
    else:
      a(href=url): text "See translation"

proc canTranslateOnDemand(tweet: Tweet): bool =
  onDemandTranslation() and tweet.isTranslatable and tweet.translation.len == 0 and
    not (tweet.translatable and not tweet.showTrans)

proc renderTranslateOnDemandLink*(href, title: string): VNode =
  buildHtml(tdiv(class="tweet-translate tweet-translate-ondemand")):
    a(href=href, title=title):
      text "Translate to " & langName(onDemandTranslationLang())

proc renderTranslateOnDemand(tweet: Tweet): VNode =
  renderTranslateOnDemandLink(getLink(tweet, focus=false) & "/translate",
                              "Ask Grok to translate this post")

proc noteCanTranslateOnDemand*(flagged, translatable, requested: bool;
                               translation: string): bool =
  onDemandTranslation() and flagged and translation.len == 0 and
    not (translatable and not requested)

proc renderCommunityNote(note: string; prefs: Prefs; tweet: Tweet = nil;
                         transUrl = ""): VNode =
  let
    hasTrans = enableTranslation() and tweet != nil and tweet.noteTranslatable
    active = hasTrans and tweet.noteTranslation.len > 0 and
             (tweet.showTrans or prefs.autoTranslate)
    text = if active: tweet.noteTranslation else: note
    onDemand = tweet != nil and tweet.noteId.len > 0 and
               noteCanTranslateOnDemand(tweet.noteIsTranslatable, tweet.noteTranslatable,
                                        tweet.showTrans, tweet.noteTranslation)

  buildHtml(tdiv(class="community-note")):
    tdiv(class="community-note-header"):
      icon "group"
      span: text "Community note"
    if onDemand:
      renderTranslateOnDemandLink(
        "/i/birdwatch/n/" & tweet.noteId & "/translate?r=/i/status/" & $tweet.id,
        "Ask Grok to translate this note")
    elif hasTrans:
      renderTranslateLabel(active, tweet.showTrans, tweet.noteTransLang, transUrl,
                           tweet.noteTransTarget)
    tdiv(class="community-note-text", dir="auto"):
      verbatim replaceUrls(text, prefs)

proc renderQuoteMedia(quote: Tweet; prefs: Prefs; path: string): VNode =
  buildHtml(tdiv(class="quote-media-container")):
    renderMedia(quote.media, prefs, path)

proc renderQuote(quote: Tweet; prefs: Prefs; path: string; transUrl = ""): VNode =
  if not quote.available:
    let href = if quote.expUrl.len > 0: quote.expUrl else: getLink(quote, focus=false)
    return buildHtml(tdiv(class="quote unavailable")):
      a(class="unavailable-quote", href=href):
        if quote.tombstone.len > 0:
          text quote.tombstone
        elif quote.text.len > 0:
          text quote.text
        else:
          text "This tweet is unavailable"

  let
    transOn = enableTranslation() and quote.translatable
    transActive = transOn and quote.translation.len > 0 and
                  (quote.showTrans or prefs.autoTranslate)
    quoteText = if transActive: quote.translation else: quote.text

  buildHtml(tdiv(class="quote quote-big")):
    a(class="quote-link", href=getLink(quote))

    tdiv(class="tweet-name-row"):
      tdiv(class="fullname-and-username"):
        renderMiniAvatar(quote.user, prefs)
        linkUser(quote.user, class="fullname")
        verifiedIcon(quote.user)
        linkUser(quote.user, class="username")

      span(class="tweet-date"):
        a(href=getLink(quote), title=quote.getTime):
          text quote.getShortTime

    profileLabel(quote.user)

    if quote.reply.len > 0:
      renderReply(quote)

    if transOn:
      renderTranslateLabel(transActive, quote.showTrans, quote.transLang, transUrl)

    if quote.text.len > 0:
      tdiv(class="quote-text", dir="auto"):
        verbatim replaceUrls(quoteText, prefs)

    if quote.media.len > 0:
      renderQuoteMedia(quote, prefs, path)

    if quote.articlePreview.isSome:
      renderArticleCard(quote.articlePreview.get(), prefs)

    if quote.note.len > 0 and not prefs.hideCommunityNotes:
      renderCommunityNote(quote.note, prefs, quote, transUrl)

    if quote.hasThread:
      a(class="show-thread", href=getLink(quote)):
        text "Show this thread"

    if quote.history.len > 0 and quote.id != max(quote.history):
      tdiv(class="quote-latest"):
        text "There's a new version of this post"

proc renderDisclosures*(tweet: Tweet): VNode =
  buildHtml(tdiv(class="disclosures")):
    if tweet.isAI:
      span(data-disclosure="ai"):
        icon "attention-circled", "Made with AI"
    if tweet.isAd:
      span(data-disclosure="ad"):
        icon "attention-circled", "Paid partnership (ad)"

proc renderLocation*(tweet: Tweet): string =
  let (place, url) = tweet.getLocation()
  if place.len == 0: return
  let node = buildHtml(span(class="tweet-geo")):
    text " – at "
    if url.len > 1:
      a(href=url): text place
    else:
      text place
  return $node

proc renderTweet*(tweet: Tweet; prefs: Prefs; path: string; class=""; index=0;
                  last=false; mainTweet=false; afterTweet=false;
                  bigThumb=false): VNode =
  var divClass = class
  if index == -1 or last:
    divClass = "thread-last " & class

  if not tweet.available:
    return buildHtml(tdiv(class=divClass & "unavailable timeline-item", data-username=tweet.user.username)):
      a(class="unavailable-box", href=getLink(tweet)):
        if tweet.tombstone.len > 0:
          text tweet.tombstone
        elif tweet.text.len > 0:
          text tweet.text
        else:
          text "This tweet is unavailable"

      if tweet.quote.isSome:
        renderQuote(tweet.quote.get(), prefs, path)

  let
    fullTweet = tweet
    pinned = tweet.pinned

  var retweet: string
  var retweeter: string
  var tweet = fullTweet
  if tweet.retweet.isSome:
    tweet = tweet.retweet.get
    retweet = fullTweet.user.fullname
    retweeter = fullTweet.user.username

  buildHtml(tdiv(class=("timeline-item " & divClass), data-username=tweet.user.username)):
    if not mainTweet:
      a(class="tweet-link", href=getLink(tweet))

    tdiv(class="tweet-body"):
      renderHeader(tweet, retweet, retweeter, fullTweet, pinned, prefs, path)

      if not afterTweet and index == 0 and tweet.reply.len > 0 and
         (tweet.reply.len > 1 or tweet.reply[0] != tweet.user.username or pinned):
        renderReply(tweet)

      var tweetClass = "tweet-content media-body"
      if prefs.bidiSupport:
        tweetClass &= " tweet-bidi"

      let
        transOn = enableTranslation() and tweet.translatable
        quoteTrans = tweet.quote.isSome and
                     (tweet.quote.get.translatable or tweet.quote.get.noteTranslatable)
        transUrl = if enableTranslation() and
                      (tweet.translatable or tweet.noteTranslatable or quoteTrans):
                     translateUrl(tweet, path, mainTweet)
                   else: ""
        transActive = transOn and tweet.translation.len > 0 and
                      (tweet.showTrans or prefs.autoTranslate)
        tweetText = if transActive: tweet.translation else: tweet.text

      if tweet.canTranslateOnDemand:
        renderTranslateOnDemand(tweet)
      elif transOn:
        renderTranslateLabel(transActive, tweet.showTrans, tweet.transLang, transUrl,
                             tweet.transTarget)

      tdiv(class=tweetClass, dir="auto"):
        verbatim replaceUrls(tweetText, prefs) & renderLocation(tweet)

      if tweet.attribution.isSome:
        renderAttribution(tweet.attribution.get(), prefs, tweet.attributionLink)

      if tweet.card.isSome and tweet.card.get().kind != hidden:
        renderCard(tweet.card.get(), prefs, path)

      if tweet.articlePreview.isSome:
        renderArticleCard(tweet.articlePreview.get(), prefs)

      if tweet.media.len > 0:
        renderMedia(tweet.media, prefs, path, bigThumb)

      if tweet.poll.isSome:
        renderPoll(tweet.poll.get())

      if tweet.quote.isSome:
        renderQuote(tweet.quote.get(), prefs, path, transUrl)

      if tweet.note.len > 0 and not prefs.hideCommunityNotes:
        renderCommunityNote(tweet.note, prefs, tweet, transUrl)

      if tweet.isAI or tweet.isAd:
        renderDisclosures(tweet)

      let
        hasEdits = tweet.history.len > 1
        isLatest = hasEdits and tweet.id == max(tweet.history)

      if mainTweet:
        p(class="tweet-published"): 
          if hasEdits and isLatest:
            a(href=(getLink(tweet, focus=false) & "/history")):
              text &"Last edited {getTime(tweet)}"
          else:
            text &"{getTime(tweet)}"

        if hasEdits and not isLatest:
          renderLatestPost(tweet.user.username, max(tweet.history))

      if tweet.mediaTags.len > 0:
        renderMediaTags(tweet.mediaTags)

      if not prefs.hideTweetStats:
        var stats = tweet.stats
        if retweet.len > 0 and stats.source.len == 0:
          stats.source = fullTweet.stats.source
        renderStats(stats, prefs, tweet)
