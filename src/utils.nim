# SPDX-License-Identifier: AGPL-3.0-only
import sequtils, strutils, strformat, uri, tables, base64
import nimcrypto

var
  hmacKey: string
  base64Media = false

const
  https* = "https://"
  twimg* = "pbs.twimg.com/"
  nitterParams* = ["name", "tab", "id", "list", "referer", "scroll", "prefs"]
  twitterDomains = @[
    "twitter.com",
    "pic.twitter.com",
    "twimg.com",
    "abs.twimg.com",
    "pbs.twimg.com",
    "video.twimg.com",
    "x.com",
    "pscp.tv",
    "video.pscp.tv"
  ]

const
  langNames = {
    "af": "Afrikaans", "sq": "Albanian", "am": "Amharic", "ar": "Arabic",
    "hy": "Armenian", "az": "Azerbaijani", "eu": "Basque", "be": "Belarusian",
    "bn": "Bengali", "bs": "Bosnian", "bg": "Bulgarian", "ca": "Catalan",
    "ceb": "Cebuano", "zh": "Chinese", "co": "Corsican", "hr": "Croatian",
    "cs": "Czech", "da": "Danish", "nl": "Dutch", "en": "English",
    "eo": "Esperanto", "et": "Estonian", "fi": "Finnish", "fr": "French",
    "fy": "Frisian", "gl": "Galician", "ka": "Georgian", "de": "German",
    "el": "Greek", "gu": "Gujarati", "ht": "Haitian Creole", "ha": "Hausa",
    "haw": "Hawaiian", "he": "Hebrew", "iw": "Hebrew", "hi": "Hindi",
    "hmn": "Hmong", "hu": "Hungarian", "is": "Icelandic", "ig": "Igbo",
    "id": "Indonesian", "in": "Indonesian", "ga": "Irish", "it": "Italian",
    "ja": "Japanese", "jv": "Javanese", "kn": "Kannada", "kk": "Kazakh",
    "km": "Khmer", "rw": "Kinyarwanda", "ko": "Korean", "ku": "Kurdish",
    "ky": "Kyrgyz", "lo": "Lao", "la": "Latin", "lv": "Latvian",
    "lt": "Lithuanian", "lb": "Luxembourgish", "mk": "Macedonian", "mg": "Malagasy",
    "ms": "Malay", "ml": "Malayalam", "mt": "Maltese", "mi": "Maori",
    "mr": "Marathi", "mn": "Mongolian", "my": "Myanmar", "ne": "Nepali",
    "no": "Norwegian", "ny": "Nyanja", "or": "Odia", "ps": "Pashto",
    "fa": "Persian", "pl": "Polish", "pt": "Portuguese", "pa": "Punjabi",
    "ro": "Romanian", "ru": "Russian", "sm": "Samoan", "gd": "Scots Gaelic",
    "sr": "Serbian", "st": "Sesotho", "sn": "Shona", "sd": "Sindhi",
    "si": "Sinhala", "sk": "Slovak", "sl": "Slovenian", "so": "Somali",
    "es": "Spanish", "su": "Sundanese", "sw": "Swahili", "sv": "Swedish",
    "tl": "Filipino", "tg": "Tajik", "ta": "Tamil", "tt": "Tatar",
    "te": "Telugu", "th": "Thai", "tr": "Turkish", "tk": "Turkmen",
    "uk": "Ukrainian", "ur": "Urdu", "ug": "Uyghur", "uz": "Uzbek",
    "vi": "Vietnamese", "cy": "Welsh", "xh": "Xhosa", "yi": "Yiddish",
    "yo": "Yoruba", "zu": "Zulu"
  }.toTable

proc langName*(code: string): string =
  let lang = code.toLowerAscii
  if lang in langNames: return langNames[lang]
  let base = lang.split({'-', '_'})[0]
  result = langNames.getOrDefault(base, code)

proc setHmacKey*(key: string) =
  hmacKey = key

proc setProxyEncoding*(state: bool) =
  base64Media = state

proc getHmac*(data: string): string =
  ($hmac(sha256, hmacKey, data))[0 .. 12]

proc getVidUrl*(link: string): string =
  if link.len == 0: return
  let sig = getHmac(link)
  if base64Media:
    &"/video/enc/{sig}/{encode(link, safe=true)}"
  else:
    &"/video/{sig}/{encodeUrl(link)}"

proc getPicUrl*(link: string): string =
  if link.len == 0: return
  if base64Media:
    &"/pic/enc/{encode(link, safe=true)}"
  else:
    &"/pic/{encodeUrl(link)}"

proc getOrigPicUrl*(link: string): string =
  if link.len == 0: return
  if base64Media:
    &"/pic/orig/enc/{encode(link, safe=true)}"
  else:
    &"/pic/orig/{encodeUrl(link)}"

proc filterParams*(params: Table): seq[(string, string)] =
  for p in params.pairs():
    if p[1].len > 0 and p[0] notin nitterParams:
      result.add p

proc isTwitterUrl*(uri: Uri): bool =
  uri.scheme in ["http", "https"] and
    (uri.hostname in twitterDomains or uri.hostname.endsWith(".video.pscp.tv"))

proc isTwitterUrl*(url: string): bool =
  isTwitterUrl(parseUri(url))

const twitterPageHosts = [
  "twitter.com", "www.twitter.com", "mobile.twitter.com",
  "x.com", "www.x.com", "mobile.x.com"
]

proc localizeTwitterLink*(url: string): string =
  let uri = parseUri(url)
  if uri.scheme in ["http", "https"] and uri.hostname in twitterPageHosts:
    result = uri.path
    if uri.query.len > 0: result &= "?" & uri.query
    if uri.anchor.len > 0: result &= "#" & uri.anchor
  else:
    result = url

proc extractUsername*(url: string): string =
  let parts = parseUri(url).path.strip(chars = {'/'}).split('/')
  if parts.len >= 3 and parts[1] == "status":
    return parts[0]

proc validateNumber*(value: string): string =
  if value.anyIt(not it.isDigit):
    return ""
  return value
