# SPDX-License-Identifier: AGPL-3.0-only
import uri, strutils, httpclient, os, hashes, base64, re, times, posix
import asynchttpserver, asyncstreams, asyncfile, asyncnet
import asyncdispatch

import jester

import router_utils
import ".."/[types, formatters, utils]

export asynchttpserver, asyncstreams, asyncfile, asyncnet
export httpclient, os, strutils, asyncstreams, base64, re

const
  m3u8Mime* = "application/vnd.apple.mpegurl"
  maxAge* = "max-age=604800"
  mediaIdleTimeoutMs = 5000
  mediaDisconnectPollMs = 100

type MediaStreamWatch = ref object
  done: bool
  streaming: bool
  lastProgress: float
  pendingRead: Future[(bool, string)]
  forwarded: BiggestInt
  forwardedChanged: Future[void]
  disconnected: bool

proc peerDisconnected(req: jester.Request): bool =
  let socket = req.getNativeReq().client
  if socket.isClosed:
    return true

  # A FIN also occurs when the client half-closes its sending side but
  # still expects the response. POLLHUP/POLLERR indicate a broken socket;
  # unlike reading EOF, these do not mistake a half-close for abandonment.
  var fd = TPollfd(fd: socket.getFd().cint, events: 0)
  let ready = posix.poll(addr fd, Tnfds(1), 0)
  return ready > 0 and (fd.revents and (POLLHUP or POLLERR or POLLNVAL)) != 0

proc signalForwarded(watch: MediaStreamWatch) =
  if watch.forwardedChanged != nil and not watch.forwardedChanged.finished:
    watch.forwardedChanged.complete()

proc waitForForwarded(watch: MediaStreamWatch; received: BiggestInt): Future[void] {.async.} =
  # AsyncHttpClient fills an unbounded FutureStream independently of its
  # reader. The progress callback is awaited by the socket reader, so pause
  # upstream when it gets ahead of the downstream send.
  while watch.streaming and not watch.done and watch.forwarded < received:
    # HTTP chunk framing is included in progress but not in the body stream.
    # If the downstream reader is waiting for new data, nothing remains to
    # forward, so allow the upstream parser to read the next chunk.
    if watch.pendingRead != nil and not watch.pendingRead.finished:
      return
    watch.forwardedChanged = newFuture[void]("media forwarded")
    await watch.forwardedChanged

proc watchMediaStream(watch: MediaStreamWatch; upstream: AsyncHttpClient;
                      downstream: jester.Request) {.async.} =
  # Monitor the downstream even before the upstream responds. Otherwise a
  # disconnected client can leave several timed-out retries holding sockets.
  try:
    while not watch.done:
      await sleepAsync(mediaDisconnectPollMs)
      if watch.done:
        return
      let disconnected = downstream.peerDisconnected()
      let stalled = watch.streaming and epochTime() - watch.lastProgress >=
          mediaIdleTimeoutMs.float / 1000
      if disconnected or stalled:
        if disconnected:
          watch.disconnected = true
        else:
          echo "[media] Streaming stalled; closing upstream and downstream"
        watch.done = true
        watch.signalForwarded()
        # A TLS shutdown can raise; still close the downstream and wake the
        # currently blocked read. Do not fail FutureStream itself: it may
        # already be completed while its queue still contains unread data.
        try:
          upstream.close()
        except CatchableError:
          echo "[media] Upstream shutdown failed: ", getCurrentExceptionMsg()
        try:
          downstream.close()
        except CatchableError:
          echo "[media] Downstream shutdown failed: ", getCurrentExceptionMsg()
        if watch.pendingRead != nil and not watch.pendingRead.finished:
          watch.pendingRead.fail(newException(IOError, "Media stream interrupted"))
        return
  except CatchableError:
    echo "[media] Stream watchdog error: ", getCurrentExceptionMsg()

proc safeFetch*(url: string): Future[string] {.async.} =
  # maxRedirects=0: the caller already validated the host, so never follow a
  # redirect off the allowlisted host (would re-open the #1411 SSRF).
  let client = newAsyncHttpClient(maxRedirects = 0)
  try: result = await client.getContent(url)
  except: discard
  finally: client.close()

template respond*(req: asynchttpserver.Request; headers) =
  var msg = "HTTP/1.1 200 OK\c\L"
  for k, v in headers:
    msg.add(k & ": " & v & "\c\L")

  msg.add "\c\L"
  yield req.client.send(msg)

proc proxyMedia*(req: jester.Request; url: string): Future[HttpCode] {.async.} =
  result = Http200
  let request = req.getNativeReq()
  var fetchUrl = url

  for attempt in 0 .. 2:
    let client = newAsyncHttpClient(maxRedirects = 0)
    let watch = MediaStreamWatch(lastProgress: epochTime())
    client.onProgressChanged = proc(total, progress, speed: BiggestInt): Future[void] =
      waitForForwarded(watch, progress)
    asyncCheck watchMediaStream(watch, client, req)
    var shouldRetry = false
    try:
      let resFut = client.get(fetchUrl)
      let completed = await withTimeout(resFut, 5000)
      if watch.disconnected:
        return Http200
      if not completed:
        if attempt < 2:
          echo "[media] Retry $1/2, timeout after 5s, url: $2" % [$(attempt + 1), fetchUrl]
          shouldRetry = true
        else:
          echo "[media] Proxying timeout after 5s, url: $1" % [fetchUrl]
          return Http504
      else:
        let res = resFut.read()
        if res.status != "200 OK":
          if res.status == "404 Not Found":
            return Http404
          if res.status.startsWith("30") and res.headers.hasKey("location"):
            let location = res.headers["location", 0]
            if isTwitterUrl(location):
              fetchUrl = location
              shouldRetry = true
              continue
            else:
              return Http403
          if attempt < 2:
            echo "[media] Retry $1/2, status: $2, url: $3" % [$(attempt + 1), res.status, fetchUrl]
            shouldRetry = true
          else:
            echo "[media] Proxying failed, status: $1, url: $2" % [res.status, fetchUrl]
            return Http404
        else:
          let hashed = $hash(url)
          if request.headers.getOrDefault("If-None-Match") == hashed:
            return Http304

          let contentLength =
            if res.headers.hasKey("content-length"):
              res.headers["content-length", 0]
            else:
              ""

          let headers = newHttpHeaders({
            "content-type": res.headers["content-type", 0],
            "connection": "close",
            "cache-control": maxAge,
            "etag": hashed
          })
          if contentLength.len > 0:
            headers["content-length"] = contentLength

          # Raw-mode responses bypass Jester's socket lifecycle. Close through
          # Jester so httpbeast also unregisters the socket from its selector.
          # The upstream header negotiation may take nearly five seconds.
          # The stream idle deadline must start when the body is handed off,
          # not when the initial HTTP request was created.
          watch.lastProgress = epochTime()
          watch.streaming = true
          try:
            respond(request, headers)
            var (hasValue, data) = (true, "")
            while hasValue:
              let readFut = res.bodyStream.read()
              watch.pendingRead = readFut
              if not readFut.finished:
                watch.signalForwarded()
              try:
                (hasValue, data) = await readFut
              finally:
                watch.pendingRead = nil
              if hasValue:
                watch.lastProgress = epochTime()
                await request.client.send(data)
                watch.forwarded += data.len
                watch.signalForwarded()
                watch.lastProgress = epochTime()
          except CatchableError:
            echo "[media] Stream interrupted, url: ", fetchUrl, ", error: ", getCurrentExceptionMsg()
          finally:
            watch.done = true
            watch.signalForwarded()
            req.close()
          return Http200
    except CatchableError:
      # A disconnect closes the upstream while headers may still be pending.
      # Do not retry or let Jester write an error to the closed socket.
      if watch.disconnected:
        return Http200
      if attempt < 2:
        echo "[media] Retry $1/2, error: $2, url: $3" % [$(attempt + 1), getCurrentExceptionMsg(), fetchUrl]
        shouldRetry = true
      else:
        echo "[media] Proxying exception, error: $1, url: $2" % [getCurrentExceptionMsg(), fetchUrl]
        result = Http404
    finally:
      watch.done = true
      watch.signalForwarded()
      client.close()
    if not shouldRetry:
      break

template check*(code): untyped =
  if code != Http200:
    resp code
  else:
    enableRawMode()
    break route

proc decoded*(req: jester.Request; index: int): string =
  let
    based = req.matches[0].len > 1
    encoded = req.matches[index]
  if based: decode(encoded)
  else: decodeUrl(encoded)

proc normalizeImgUrl*(url: var string) =
  if not url.startsWith("http"):
    if "twimg.com" notin url:
      url.insert(twimg)
    url.insert(https)

proc createMediaRouter*(cfg: Config) =
  router media:
    get "/pic/?":
      resp Http404

    get re"^\/pic\/orig\/(enc)?\/?(.+)":
      var url = decoded(request, 1)
      cond "/amplify_video/" notin url
      normalizeImgUrl(url)
      url.add("?name=orig")

      let uri = parseUri(url)
      cond isTwitterUrl(uri) == true

      let code = await proxyMedia(request, url)
      check code

    get re"^\/pic\/(enc)?\/?(.+)":
      var url = decoded(request, 1)
      cond "/amplify_video/" notin url
      normalizeImgUrl(url)

      let uri = parseUri(url)
      cond isTwitterUrl(uri) == true

      let code = await proxyMedia(request, url)
      check code

    get re"^\/video\/(enc)?\/?(.+)\/(.+)$":
      let url = decoded(request, 2)
      cond isTwitterUrl(url)

      if getHmac(url) != request.matches[1]:
        resp Http403, showError("Failed to verify signature", cfg)

      if ".mp4" in url or ".ts" in url or ".m4s" in url or ".aac" in url:
        let code = await proxyMedia(request, url)
        check code

      var content: string
      if ".vmap" in url:
        let m3u8 = getM3u8Url(await safeFetch(url))
        if m3u8.len > 0:
          content = await safeFetch(url)
        else:
          resp Http404

      if ".m3u8" in url:
        let vid = await safeFetch(url)
        content = proxifyVideo(vid, requestPrefs().proxyVideos, url)

      resp content, m3u8Mime
