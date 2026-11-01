# SPDX-License-Identifier: AGPL-3.0-only
import std/[asyncdispatch, asyncnet, httpclient, net, posix, strutils, unittest]
import ../src/http_client

proc serveOnce(listener: AsyncSocket; mode, verb: string) {.async.} =
  let peer = await listener.accept()
  try:
    doAssert (await peer.recvLine()).startsWith(verb & " /test ")
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

proc checkBoundedRequest(mode: string; verb = "GET") {.async.} =
  let listener = newAsyncSocket()
  listener.bindAddr(Port(0), "127.0.0.1")
  listener.listen()
  let (_, port) = listener.getLocalAddr()
  let
    server = serveOnce(listener, mode, verb)
    client = newAsyncHttpClient()
    url = "http://127.0.0.1:" & $port & "/test"

  try:
    let request = if verb == "POST":
                    client.postBounded(url, "payload", 100)
                  else:
                    client.getBounded(url, 100)
    if mode == "complete":
      let (response, body) = await request
      doAssert response.code == Http200
      doAssert body == "ok"
    else:
      var timedOut = false
      try:
        discard await request
      except IOError as err:
        timedOut = "timed out" in err.msg
      doAssert timedOut, "a stalled " & mode & " must time out"
      doAssert client.getSocket().isClosed, "the upstream socket must close"

    await server
  finally:
    client.close()
    listener.close()

proc checkConnectingTimeout() {.async.} =
  # A full TCP accept backlog keeps the next connect in progress. This is the
  # one phase where Nim's original asyncnet.dial hid its socket until success.
  let listener = net.newSocket()
  listener.bindAddr(Port(0), "127.0.0.1")
  listener.listen(1)
  let (_, port) = listener.getLocalAddr()
  let first = net.newSocket()
  let second = net.newSocket()
  let client = newAsyncHttpClient()

  try:
    first.connect("127.0.0.1", port)
    second.connect("127.0.0.1", port)

    let request = client.getBounded("http://127.0.0.1:" & $port, 250)
    await sleepAsync(20)
    doAssert not request.finished,
      "the request must still be waiting for its TCP connection"
    let pendingSocket = client.getSocket()
    doAssert pendingSocket != nil and not pendingSocket.isClosed,
      "the HTTP client must already own its in-progress socket"
    var connecting = false
    try:
      discard pendingSocket.getPeerAddr()
    except OSError as err:
      # getpeername fails with ENOTCONN until TCP connect completes.
      doAssert err.errorCode.int == ENOTCONN.int, "unexpected socket error"
      connecting = true
    doAssert connecting, "the fixture must stall TCP connect, not HTTP headers"

    var timedOut = false
    try:
      discard await request
    except IOError as err:
      timedOut = "timed out" in err.msg

    doAssert timedOut, "pending TCP connect must respect the deadline"
    doAssert client.getSocket() != nil,
      "pending connection socket must be owned by the HTTP client"
    doAssert client.getSocket().isClosed,
      "deadline must close an in-progress TCP socket"
  finally:
    client.close()
    second.close()
    first.close()
    listener.close()

suite "bounded upstream HTTP operations":
  test "stalled response headers close the socket":
    waitFor checkBoundedRequest("headers")
  test "stalled response bodies close the socket":
    waitFor checkBoundedRequest("body")
  test "complete responses retain normal behaviour":
    waitFor checkBoundedRequest("complete")
  test "POST stalled response headers close the socket":
    waitFor checkBoundedRequest("headers", "POST")
  test "POST stalled response bodies close the socket":
    waitFor checkBoundedRequest("body", "POST")
  test "POST complete responses retain normal behaviour":
    waitFor checkBoundedRequest("complete", "POST")
  test "pending TCP connection closes on timeout":
    waitFor checkConnectingTimeout()
