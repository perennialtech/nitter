# SPDX-License-Identifier: AGPL-3.0-only
import std/[asyncdispatch, httpclient, net, os, strutils]
import ../src/[http_pool, types]

proc main() {.async.} =
  setMaxHttpConns(1)
  setHttpProxy(paramStr(1), "")
  let pool = HttpPool()
  if paramStr(2).startsWith("https:"):
    # Trust only the test CA without changing production's default trust store.
    pool.conns.add newAsyncHttpClient(userAgent="", proxy=newProxy(paramStr(1)),
      sslContext=newContext(verifyMode=CVerifyPeer, caFile=paramStr(3)))
  try:
    for index in 0 .. 1:
      let headers = newHttpHeaders({"X-Nitter-Proxy-Test": $index})
      pool.use(headers):
        let
          response = await c.get(paramStr(2))
          body = await response.body
        if response.code != Http200 or body != "OK":
          raise newException(IOError, "Unexpected proxy test response")
  finally:
    for client in pool.conns:
      client.close()

waitFor main()
