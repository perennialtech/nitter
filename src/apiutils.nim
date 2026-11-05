# SPDX-License-Identifier: AGPL-3.0-only
import httpclient, asyncdispatch, options, strutils, uri, times, math, tables
import jsony, packedjson, zippy, oauth/oauth1
import types, auth, consts, parserutils, http_client, tid
import experimental/types/common

const
  rlRemaining = "x-rate-limit-remaining"
  rlReset = "x-rate-limit-reset"
  rlLimit = "x-rate-limit-limit"
  npCache = "x-np-cache"
  errorsToSkip = {null, doesntExist, tweetNotFound, timeout, unauthorized, badRequest}

proc isCloudflareHtml*(body: string): bool =
  ## Detect Cloudflare HTML error pages returned instead of JSON
  if body.len < 14 or body[0] != '<': return false
  body[0 ..< 14].toLowerAscii() == "<!doctype html" and "Cloudflare" in body

proc cfTitle*(body: string): string =
  ## Extract <title> from Cloudflare HTML for log diagnostics
  let start = body.find("<title>")
  if start < 0: return "unknown"
  let contentStart = start + 7
  let stop = body.find("</title>", contentStart)
  if stop < 0: return "unknown"
  body[contentStart ..< stop].splitWhitespace().join(" ")

var
  disableTid: bool
  apiProxy: string
  maxRetries: int
  retryDelayMs: int
  apiTimeoutMs: int = 15000
  useGuestAuth: bool
  showRetweetTime: bool = true
  translationEnabled: bool = true
  onDemandTransEnabled: bool
  onDemandTransLang = "en"
  guestActivation: Future[Session]

const guestActivateUrl = "https://api.x.com/1.1/guest/activate.json"

proc setDisableTid*(disable: bool) =
  disableTid = disable

proc setMaxRetries*(n: int) =
  maxRetries = n

proc setRetryDelayMs*(ms: int) =
  retryDelayMs = ms

proc setApiTimeoutMs*(ms: int) =
  apiTimeoutMs = max(ms, 1000)

proc getApiTimeoutMs*(): int =
  apiTimeoutMs

proc setGuestAuth*(enabled: bool; poolSize = 1) =
  if enabled and guestBearerToken.len == 0:
    echo "[sessions] ERROR: guestAuth is enabled but guestBearerToken is empty in consts.nim. Guest mode requires a valid bearer token."
    quit 1
  useGuestAuth = enabled
  if enabled:
    setGuestPoolSize(poolSize)

proc isGuestAuth*(): bool =
  useGuestAuth

proc setShowRetweetTime*(enabled: bool) =
  showRetweetTime = enabled

proc showRetweetTimeEnabled*(): bool =
  showRetweetTime

proc setEnableTranslation*(enabled: bool) =
  translationEnabled = enabled

proc enableTranslation*(): bool =
  translationEnabled

proc setOnDemandTranslation*(enabled: bool; lang: string) =
  onDemandTransEnabled = enabled
  if lang.len > 0: onDemandTransLang = lang

proc onDemandTranslation*(): bool =
  onDemandTransEnabled

proc onDemandTranslationLang*(): string =
  onDemandTransLang

proc setApiProxy*(url: string) =
  apiProxy = ""
  if url.len > 0:
    apiProxy = url.strip(chars={'/'}) & "/"
    if "http" notin apiProxy:
      apiProxy = "http://" & apiProxy

proc toUrl*(req: ApiReq; sessionKind: SessionKind): Uri =
  let url = case sessionKind
    of oauth:  req.oauth
    of cookie, guest: req.cookie
  let base = case sessionKind
    of oauth:  "https://api.x.com"
    of cookie, guest: "https://x.com/i/api"
  let prefix =
    if url.endpoint.startsWith("1.1/") or url.endpoint.startsWith("2/"): ""
    else: "graphql/"
  parseUri(base) / (prefix & url.endpoint & url.suffix) ? url.params

proc isRestV2(path: string): bool =
  "/2/timeline/" in path

proc getOauthHeader(url, oauthToken, oauthTokenSecret: string): string =
  let
    encodedUrl = url.replace(",", "%2C").replace("+", "%20")
    params = OAuth1Parameters(
      consumerKey: consumerKey,
      signatureMethod: "HMAC-SHA1",
      timestamp: $int(round(epochTime())),
      nonce: "0",
      isIncludeVersionToHeader: true,
      token: oauthToken
    )
    signature = getSignature(HttpGet, encodedUrl, "", params, consumerSecret, oauthTokenSecret)

  params.signature = percentEncode(signature)

  return getOauth1RequestHeader(params)["authorization"]

proc getCookieHeader(authToken, ct0: string): string =
  "auth_token=" & authToken & "; ct0=" & ct0

proc genHeaders*(session: Session, url: Uri, skipTid: bool): Future[HttpHeaders] {.async.} =
  result = newHttpHeaders({
    "accept": "*/*",
    "accept-encoding": "gzip",
    "accept-language": "en-US,en;q=0.9",
    "content-type": "application/json",
    "origin": "https://x.com",
    "user-agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36",
    "x-twitter-active-user": "yes",
    "x-twitter-client-language": "en",
    "priority": "u=1, i"
  }, titleCase=true)

  case session.kind
  of SessionKind.oauth:
    result["authorization"] = getOauthHeader($url, session.oauthToken, session.oauthSecret)
  of SessionKind.guest:
    result["x-guest-token"] = session.guestToken
    result["referer"] = "https://x.com/"
    result["sec-ch-ua"] = """"Google Chrome";v="142", "Chromium";v="142", "Not A(Brand";v="24""""
    result["sec-ch-ua-mobile"] = "?0"
    result["sec-ch-ua-platform"] = "Windows"
    result["sec-fetch-dest"] = "empty"
    result["sec-fetch-mode"] = "cors"
    result["sec-fetch-site"] = "same-origin"
    result["authorization"] = guestBearerToken
    if not (disableTid or skipTid or "/1.1/" in url.path or isRestV2(url.path)):
      result["x-client-transaction-id"] = await genTid(url.path)
    registerGuestRequest(session)
  of SessionKind.cookie:
    result["x-twitter-auth-type"] = "OAuth2Session"
    result["x-csrf-token"] = session.ct0
    result["cookie"] = getCookieHeader(session.authToken, session.ct0)
    result["referer"] = "https://x.com/"
    result["sec-ch-ua"] = """"Google Chrome";v="142", "Chromium";v="142", "Not A(Brand";v="24""""
    result["sec-ch-ua-mobile"] = "?0"
    result["sec-ch-ua-platform"] = "Windows"
    result["sec-fetch-dest"] = "empty"
    result["sec-fetch-mode"] = "cors"
    result["sec-fetch-site"] = "same-origin"
    if isRestV2(url.path):
      result["authorization"] = bearerToken
    elif disableTid or skipTid or "/1.1/" in url.path:
      result["authorization"] = bearerToken2
    else:
      result["authorization"] = bearerToken
      result["x-client-transaction-id"] = await genTid(url.path)

proc activateGuestToken(): Future[Session] {.async.} =
  ## POST to the guest activation endpoint and wrap the returned token in a
  ## fresh guest Session, ready to be added to the pool
  let headers = newHttpHeaders({
    "authorization": guestBearerToken,
    "content-type": "application/x-www-form-urlencoded",
    "accept": "*/*",
    "accept-language": "en-US,en;q=0.9",
    "origin": "https://x.com",
    "referer": "https://x.com/",
    "user-agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36",
    "sec-ch-ua": """"Google Chrome";v="142", "Chromium";v="142", "Not A(Brand";v="24"""",
    "sec-ch-ua-mobile": "?0",
    "sec-ch-ua-platform": "Windows",
    "sec-fetch-dest": "empty",
    "sec-fetch-mode": "cors",
    "sec-fetch-site": "same-origin",
    "x-twitter-active-user": "yes",
    "x-twitter-client-language": "en"
  })
  useHttpClient(headers):
    let (resp, body) = await c.postBounded(guestActivateUrl, "", apiTimeoutMs)

    if resp.status != $Http200:
      echo "[sessions] guest activation failed, status: ", resp.status
      raise rateLimitError()

    let js = parseJson(body)
    if not js.hasKey("guest_token"):
      echo "[sessions] guest activation returned no token"
      raise rateLimitError()
    let token = js["guest_token"].getStr

    if token.len == 0:
      echo "[sessions] guest activation returned an empty token"
      raise rateLimitError()

    echo "[sessions] activated new guest token"
    result = Session(kind: guest, guestToken: token, guestCreated: epochTime().int, guestReqs: 0)

type GuestActivator* = proc(): Future[Session] {.closure.}

proc getReadyGuestSession*(req: ApiReq;
                           activate: GuestActivator = activateGuestToken): Future[Session] {.async.} =
  while true:
    result = getGuestSession(req)
    if not result.isNil: return

    # Check for another free slot after the current activation. A burst can
    # fill the new session before every waiter has acquired a lease.
    let activation = guestActivation
    if activation.isNil: break
    try:
      discard await activation
    except CatchableError:
      raise rateLimitError()

  var stale = findStaleGuestSession(req)
  if not stale.isNil and stale.pending == 0:
    invalidate(stale)

  if guestPoolFull():
    await sleepAsync(200)
    raise rateLimitError()

  let activation = activate()
  guestActivation = activation
  try:
    try:
      let fresh = await activation
      addGuestSession(fresh)
      inc fresh.pending
      result = fresh
    except CatchableError:
      raise rateLimitError()
  finally:
    guestActivation = nil

proc getAndValidateSession*(req: ApiReq): Future[Session] {.async.} =
  if useGuestAuth:
    result = await getReadyGuestSession(req)
    if result.isNil or result.guestToken.len == 0:
      echo "[sessions] Empty guest token, session: ", result.pretty
      raise rateLimitError()
    return

  result = await getSession(req)
  case result.kind
  of SessionKind.oauth:
    if result.oauthToken.len == 0:
      echo "[sessions] Empty oauth token, session: ", result.pretty
      raise rateLimitError()
  of SessionKind.guest:
    discard # guest sessions never come from the account pool
  of SessionKind.cookie:
    if result.authToken.len == 0 or result.ct0.len == 0:
      echo "[sessions] Empty cookie credentials, session: ", result.pretty
      raise rateLimitError()

template fetchImpl(result, fetchBody) {.dirty.} =
  try:
    var resp: AsyncResponse
    let skipTid = case session.kind
      of oauth: req.oauth.skipTid
      of cookie, guest: req.cookie.skipTid
    let headers = await genHeaders(session, url, skipTid)

    useHttpClient(headers):
      template getContent =
        var target = $url
        if apiProxy.len > 0 and "/1.1/" notin url.path:
          target = target.replace("https://", apiProxy)
        (resp, result) = await c.getBounded(target, apiTimeoutMs)

      getContent()

      if resp.status == $Http503:
        raise newException(BadClientError, "Bad client")

      if resp.status == $Http404 and result.len == 0:
        echo "[sessions] transient 404 (empty body), retrying: ", url.path, ", session: ", session.pretty
        raise rateLimitError()

    let cacheStatus = resp.headers.getOrDefault(npCache)
    if cacheStatus notin ["HIT", "STALE"] and resp.headers.hasKey(rlRemaining):
      let
        remaining = parseInt(resp.headers[rlRemaining])
        reset = parseInt(resp.headers[rlReset])
        limit = parseInt(resp.headers[rlLimit])
      session.setRateLimit(req, remaining, reset, limit)

    if result.len > 0:
      if resp.headers.getOrDefault("content-encoding") == "gzip":
        result = uncompress(result, dfGzip)

      if isCloudflareHtml(result):
        echo "[cloudflare] ", resp.status, " (", cfTitle(result), "), API: ", url.path, ", session: ", session.pretty
        raise rateLimitError()

      if result.startsWith("{\"errors"):
        let errors = result.fromJson(Errors)
        if errors notin errorsToSkip:
          echo "Fetch error, API: ", url.path, ", errors: ", errors, ", session: ", session.pretty
          if errors in {expiredToken, badToken, locked}:
            invalidate(session)
            raise rateLimitError()
          elif errors in {rateLimited}:
            # rate limit hit, resets after 24 hours
            setLimited(session, req)
            if session.kind == guest:
              invalidate(session)
            raise rateLimitError()
      elif result.startsWith("429 Too Many Requests"):
        echo "[sessions] 429 error, API: ", url.path, ", session: ", session.pretty
        raise rateLimitError()

    fetchBody

    if resp.status == $Http400:
      echo "ERROR 400, ", url.path, ": ", result, ", session: ", session.pretty
      raise newException(InternalError, $url)
  except InternalError as e:
    raise e
  except BadClientError as e:
    raise e
  except OSError as e:
    raise e
  except Exception as e:
    let s = session.pretty
    echo "error: ", e.name, ", msg: ", e.msg, ", session: ", s, ", url: ", url
    raise rateLimitError()
  finally:
    release(session)

template retry(bod) {.dirty.} =
  var session: Session
  var retrySuccess = false
  for i in 0 ..< maxRetries:
    try:
      session = nil
      bod
      retrySuccess = true
      break
    except RateLimitError:
      let api = if session.isNil: req.cookie.endpoint
                else: req.endpoint(session)
      if session.isNil:
        echo "[sessions] Rate limited, retrying ", api,
             " request (", i, "/", maxRetries, ")..."
      else:
        echo "[sessions] Rate limited, retrying ", api,
             " request (", i, "/", maxRetries, ")..., session: ", session.pretty
      session = nil
      if retryDelayMs > 0:
        await sleepAsync(retryDelayMs)
  if not retrySuccess:
    raise rateLimitError()

proc fetch*(req: ApiReq): Future[JsonNode] {.async.} =
  retry:
    var body: string
    session = await getAndValidateSession(req)

    let url = req.toUrl(session.kind)

    fetchImpl body:
      if body.startsWith('{') or body.startsWith('['):
        result = parseJson(body)
        # print request url for debugging
        #echo " ==== request: ", url
      else:
        echo resp.status, ": ", body, " --- url: ", url, ", session: ", session.pretty
        result = newJNull()

      let error = result.getError
      if error != null and error notin errorsToSkip:
        echo "Fetch error, API: ", url.path, ", error: ", error, ", session: ", session.pretty
        if error in {expiredToken, badToken, locked}:
          invalidate(session)
          raise rateLimitError()

proc fetchRaw*(req: ApiReq): Future[string] {.async.} =
  retry:
    session = await getAndValidateSession(req)
    let url = req.toUrl(session.kind)

    fetchImpl result:
      if not (result.startsWith('{') or result.startsWith('[')):
        echo resp.status, ": ", result, " --- url: ", url, ", session: ", session.pretty
        result.setLen(0)

const
  grokTranslationUrl = "https://api.x.com/2/grok/translation.json"
  grokTranslationTimeoutMs = 45_000

type GrokResponse = tuple[status, body: string]

proc grokPost(headers: HttpHeaders; body: string): Future[GrokResponse] {.async.} =
  useHttpClient(headers):
    let (resp, payload) = await c.postBounded(grokTranslationUrl, body, grokTranslationTimeoutMs)
    result.status = resp.status
    result.body = payload
    if resp.headers.getOrDefault("content-encoding") == "gzip":
      result.body = uncompress(result.body, dfGzip)
    if resp.status == $Http503:
      raise newException(BadClientError, "Bad client")

proc postGrokTranslation*(id, dstLang: string; contentType = "POST"): Future[string] {.async.} =
  if useGuestAuth:
    raise newException(InternalError, "translation requires cookie sessions")

  let req = ApiReq(
    cookie: ApiUrl(endpoint: "2/grok/translation.json", skipTid: true),
    oauth: ApiUrl(endpoint: "2/grok/translation.json", skipTid: true)
  )

  let session = getCookieSession(req)

  try:
    var headers = await genHeaders(session, parseUri(grokTranslationUrl), true)
    headers["authorization"] = bearerToken
    headers["content-type"] = "text/plain;charset=UTF-8"

    let
      body = $(%*{"content_type": contentType, "id": id, "dst_lang": dstLang})
      resp = await grokPost(headers, body)
    if not resp.status.startsWith("200"):
      echo "[translation] grok returned ", resp.status, " for post ", id, ": ",
           resp.body[0 ..< min(resp.body.len, 300)], ", session: ", session.pretty
      return ""

    result = resp.body
  finally:
    release(session)
