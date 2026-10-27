# SPDX-License-Identifier: AGPL-3.0-only
import std/[asyncdispatch, httpclient, net, os, strutils]
import ../src/http_client

proc main() {.async.} =
  setHttpProxy(paramStr(1), if paramCount() >= 4: paramStr(4) else: "")
  if paramCount() >= 5 and paramStr(5) == "reject-connect":
    try:
      useHttpClient(newHttpHeaders()):
        discard await c.get(paramStr(2))
    except HttpRequestError:
      return
    raise newException(IOError, "Expected a rejected CONNECT")

  for index in 0 .. 1:
    let headers = newHttpHeaders({"X-Nitter-Proxy-Test": $index})
    if paramStr(2).startsWith("https:"):
      # The synthetic TLS origin uses a test CA; keep production's trust intact.
      let c = newAsyncHttpClient(userAgent="", headers=headers,
        proxy=newProxy(paramStr(1)),
        sslContext=newContext(verifyMode=CVerifyPeer, caFile=paramStr(3)))
      try:
        let response = await c.get(paramStr(2))
        let body = await response.body
        if response.code != Http200 or body != "OK":
          raise newException(IOError, "Unexpected proxy test response")
      finally:
        c.close()
    else:
      useHttpClient(headers):
        let response = await c.get(paramStr(2))
        let body = await response.body
        if response.code != Http200 or body != "OK":
          raise newException(IOError, "Unexpected proxy test response")

waitFor main()
