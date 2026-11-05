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
  translationRetrySeconds* = 60
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
  guestAuthorization: string
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

proc setGuestAuth*(token: string; poolSize: int) =
  let token = token.strip
  if token.len == 0:
    raise newException(ValueError, "guestAuth requires NITTER_GUEST_BEARER_TOKEN")
  guestAuthorization = "Bearer " & token
  setGuestPoolSize(poolSize)

proc isGuestAuth*(): bool =
  guestAuthorization.len > 0

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
    result["authorization"] = guestAuthorization
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
    "authorization": guestAuthorization,
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

proc finishGuestActivation(activation: Future[Session]) {.async.} =
  var fresh: Session
  try:
    fresh = await activation
  except CatchableError:
    guestActivation = nil
    notifyGuestPoolChange()
    return
  guestActivation = nil
  addGuestSession(fresh)

proc getReadyGuestSession*(req: ApiReq;
                           activate: GuestActivator = activateGuestToken): Future[Session] {.async.} =
  var activationFailed = false
  while true:
    result = getGuestSession(req)
    if not result.isNil: return

    # Register before changing the pool: an injected activator may complete
    # synchronously, and its notification must not be missed.
    let changed = waitForGuestPoolChange()
    var activation = guestActivation
    if activation.isNil:
      if activationFailed:
        if not hasBusyGuestSession(req):
          raise rateLimitError()
      else:
        var stale = findIdleStaleGuestSession(req)
        if not stale.isNil:
          invalidate(stale)

        if not guestPoolFull():
          activation = activate()
          guestActivation = activation
          asyncCheck finishGuestActivation(activation)

    await changed
    result = getGuestSession(req)
    if not result.isNil: return
    if not activation.isNil and activation.finished and activation.failed:
      activationFailed = true
      if not hasBusyGuestSession(req):
        raise rateLimitError()

proc getAndValidateSession*(req: ApiReq): Future[Session] {.async.} =
  if isGuestAuth():
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

proc recordRateLimit(session: Session; req: ApiReq; headers: HttpHeaders) =
  if headers.getOrDefault(npCache) notin ["HIT", "STALE"] and headers.hasKey(rlRemaining):
    let
      remaining = parseInt(headers.getOrDefault(rlRemaining))
      reset = parseInt(headers.getOrDefault(rlReset))
      limit = parseInt(headers.getOrDefault(rlLimit))
    session.setRateLimit(req, remaining, reset, limit)

proc recordTranslationRateLimit*(session: Session; req: ApiReq; status: HttpCode;
                                 headers: HttpHeaders) =
  if status != Http429:
    try:
      recordRateLimit(session, req, headers)
    except ValueError:
      discard
    return

  proc headerInt(name: string; fallback: int): int =
    try: parseInt(headers.getOrDefault(name))
    except ValueError: fallback

  let
    now = epochTime().int
    reset = headerInt(rlReset, 0)
    retryAfter = max(1, headerInt("retry-after", translationRetrySeconds))
    retryAt = if reset > now: reset else: now + retryAfter
  session.setRateLimit(req, 0, retryAt, headerInt(rlLimit, 0))

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

    recordRateLimit(session, req, resp.headers)

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

proc grokPost(headers: HttpHeaders; body: string): Future[(AsyncResponse, string)] {.async.} =
  useHttpClient(headers):
    result = await c.postBounded(grokTranslationUrl, body, grokTranslationTimeoutMs)
    if result[0].code == Http503:
      raise newException(BadClientError, "Bad client")

proc postGrokTranslation*(id, dstLang: string; contentType = "POST"): Future[string] {.async.} =
  if isGuestAuth():
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
      (resp, payload) = await grokPost(headers, body)
    recordTranslationRateLimit(session, req, resp.code, resp.headers)
    if resp.code != Http200:
      echo "[translation] grok returned ", resp.status, " for post ", id,
           ", session: ", session.pretty
      return ""

    result =
      if resp.headers.getOrDefault("content-encoding") == "gzip": uncompress(payload, dfGzip)
      else: payload
  finally:
    release(session)
