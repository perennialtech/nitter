# SPDX-License-Identifier: AGPL-3.0-only
import std/[asyncdispatch, httpclient, strutils, uri, asyncnet]
import types

when not declared(nitterAsyncSocketOwnershipPatched):
  {.error: "Apply tools/nim-httpclient-backpressure.patch to Nim before compiling Nitter".}

var proxy: Proxy

proc setHttpProxy*(url: string; auth: string) =
  if url.len > 0:
    var proxyUrl = parseUri(url)
    if auth.len > 0:
      let parts = auth.split(':', maxsplit=1)
      if parts.len != 2:
        raise newException(ValueError, "proxyAuth must contain username:password")
      proxyUrl.username = parts[0]
      proxyUrl.password = parts[1]
    proxy = newProxy(proxyUrl)
  else:
    proxy = nil

proc closeClient(client: AsyncHttpClient) =
  # A failed CONNECT can leave a socket open before httpclient marks the
  # connection as established. Its close() only closes connected sockets.
  client.close()
  let socket = client.getSocket()
  if socket != nil and not socket.isClosed:
    socket.close()

proc getBounded*(client: AsyncHttpClient; url: string;
                 timeoutMs: int): Future[(AsyncResponse, string)] {.async.} =
  # withTimeout does not cancel its future. Close the socket on timeout so a
  # stalled response cannot retain its async reader or a session indefinitely.
  let headers = client.get(url)
  if not await withTimeout(headers, timeoutMs):
    closeClient(client)
    raise newException(IOError, "Upstream API response headers timed out")

  let response = headers.read()
  let body = response.body
  if not await withTimeout(body, timeoutMs):
    closeClient(client)
    raise newException(IOError, "Upstream API response body timed out")

  result = (response, body.read())

proc postBounded*(client: AsyncHttpClient; url, payload: string;
                  timeoutMs: int): Future[(AsyncResponse, string)] {.async.} =
  let headers = client.post(url, payload)
  if not await withTimeout(headers, timeoutMs):
    closeClient(client)
    raise newException(IOError, "Upstream API response headers timed out")

  let response = headers.read()
  let body = response.body
  if not await withTimeout(body, timeoutMs):
    closeClient(client)
    raise newException(IOError, "Upstream API response body timed out")

  result = (response, body.read())

template useHttpClient*(heads: HttpHeaders; body: untyped): untyped =
  bind BadClientError, ProtocolError, closeClient
  var c {.inject.} = newAsyncHttpClient(userAgent="", headers=heads, proxy=proxy)

  try:
    try:
      body
    except BadClientError, ProtocolError:
      # A failed tunnel may not be reusable; retry once on a fresh connection.
      closeClient(c)
      c = newAsyncHttpClient(userAgent="", headers=heads, proxy=proxy)
      body
  finally:
    # The proxy closes idle tunnels; never retain dead sockets in a client pool.
    closeClient(c)
