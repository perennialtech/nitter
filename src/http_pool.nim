# SPDX-License-Identifier: AGPL-3.0-only
import std/[httpclient, strutils, uri]

type
  HttpPool* = ref object
    conns*: seq[AsyncHttpClient]

var
  maxConns: int
  proxy: Proxy

proc setMaxHttpConns*(n: int) =
  maxConns = n

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

proc release*(pool: HttpPool; client: AsyncHttpClient; badClient=false) =
  if pool.conns.len >= maxConns or badClient:
    try: client.close()
    except: discard
  elif client != nil:
    pool.conns.insert(client)

proc acquire*(pool: HttpPool; heads: HttpHeaders): AsyncHttpClient =
  if pool.conns.len == 0:
    result = newAsyncHttpClient(userAgent="", headers=heads, proxy=proxy)
  else:
    result = pool.conns.pop()
    result.headers = heads

template use*(pool: HttpPool; heads: HttpHeaders; body: untyped): untyped =
  var
    c {.inject.} = pool.acquire(heads)
    badClient {.inject.} = false

  try:
    body
  except BadClientError, ProtocolError:
    # Twitter returned 503 or closed the connection, we need a new client
    pool.release(c, true)
    badClient = false
    c = pool.acquire(heads)
    body
  finally:
    pool.release(c, badClient)
