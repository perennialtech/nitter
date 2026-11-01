# SPDX-License-Identifier: AGPL-3.0-only
import std/[asyncdispatch, asyncnet, httpclient, net, strutils, unittest]
import ../src/http_client

proc serveOnce(listener: AsyncSocket; mode: string) {.async.} =
  let peer = await listener.accept()
  try:
    while true:
      let line = await peer.recvLine()
      if line.strip().len == 0:
        break

    if mode == "body":
      await peer.send("HTTP/1.1 200 OK\r\nContent-Length: 10\r\n\r\n")
      await sleepAsync(350)
    elif mode == "headers":
      await sleepAsync(350)
    else:
      await peer.send("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
  finally:
    peer.close()

proc checkBoundedRequest(mode: string) {.async.} =
  let listener = newAsyncSocket()
  listener.bindAddr(Port(0), "127.0.0.1")
  listener.listen()
  let (_, port) = listener.getLocalAddr()
  let
    server = serveOnce(listener, mode)
    client = newAsyncHttpClient()
    url = "http://127.0.0.1:" & $port & "/test"

  try:
    if mode == "complete":
      let (response, body) = await client.getBounded(url, 100)
      doAssert response.code == Http200
      doAssert body == "ok"
    else:
      var timedOut = false
      try:
        discard await client.getBounded(url, 100)
      except IOError as err:
        timedOut = "timed out" in err.msg
      doAssert timedOut, "a stalled " & mode & " must time out"
      doAssert client.getSocket().isClosed, "the upstream socket must close"

    await server
  finally:
    client.close()
    listener.close()

suite "bounded upstream HTTP operations":
  test "stalled response headers close the socket":
    waitFor checkBoundedRequest("headers")
  test "stalled response bodies close the socket":
    waitFor checkBoundedRequest("body")
  test "complete responses retain normal behaviour":
    waitFor checkBoundedRequest("complete")
