// SPDX-License-Identifier: AGPL-3.0-only

// Package utils holds URL signing and link helpers shared by the parser,
// views and routes.
package utils

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"slices"
	"strings"
	"unicode"
)

const (
	HTTPS = "https://"
	Twimg = "pbs.twimg.com/"
)

// NitterParams are query parameters that belong to nitter itself and are
// dropped when building upstream or canonical URLs.
var NitterParams = []string{"name", "tab", "id", "list", "referer", "scroll", "prefs"}

var twitterDomains = []string{
	"twitter.com",
	"pic.twitter.com",
	"twimg.com",
	"abs.twimg.com",
	"pbs.twimg.com",
	"video.twimg.com",
	"x.com",
	"pscp.tv",
	"video.pscp.tv",
}

var (
	hmacKey     string
	base64Media bool
)

// Init sets the media URL signing key and encoding. Call once at startup.
func Init(key string, useBase64 bool) {
	hmacKey = key
	base64Media = useBase64
}

// Hmac returns the truncated HMAC-SHA256 signature used in media URLs.
func Hmac(data string) string {
	m := hmac.New(sha256.New, []byte(hmacKey))
	m.Write([]byte(data))
	return strings.ToUpper(hex.EncodeToString(m.Sum(nil)))[:13]
}

func TranslationSignature(id string, note bool) string {
	if note {
		return Hmac("translation:note:" + id)
	}
	return Hmac("translation:post:" + id)
}

// EncodeURL percent-encodes a whole URL as a single path or query component.
func EncodeURL(s string) string { return url.QueryEscape(s) }

func encodeB64(s string) string { return base64.URLEncoding.EncodeToString([]byte(s)) }

func VidURL(link string) string {
	if link == "" {
		return ""
	}
	sig := Hmac(link)
	if base64Media {
		return "/video/enc/" + sig + "/" + encodeB64(link)
	}
	return "/video/" + sig + "/" + EncodeURL(link)
}

func PicURL(link string) string {
	if link == "" {
		return ""
	}
	if base64Media {
		return "/pic/enc/" + encodeB64(link)
	}
	return "/pic/" + EncodeURL(link)
}

func OrigPicURL(link string) string {
	if link == "" {
		return ""
	}
	if base64Media {
		return "/pic/orig/enc/" + encodeB64(link)
	}
	return "/pic/orig/" + EncodeURL(link)
}

// FilterParams drops empty values and nitter's own parameters.
func FilterParams(params url.Values) url.Values {
	out := url.Values{}
	for k, vs := range params {
		if slices.Contains(NitterParams, k) {
			continue
		}
		for _, v := range vs {
			if v != "" {
				out.Add(k, v)
			}
		}
	}
	return out
}

func IsTwitterURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	host := u.Hostname()
	return slices.Contains(twitterDomains, host) || strings.HasSuffix(host, ".video.pscp.tv")
}

var twitterPageHosts = []string{
	"twitter.com", "www.twitter.com", "mobile.twitter.com",
	"x.com", "www.x.com", "mobile.x.com",
}

// LocalizeTwitterLink turns a twitter.com or x.com page URL into a path on
// this instance, and returns any other URL unchanged.
func LocalizeTwitterLink(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		!slices.Contains(twitterPageHosts, u.Hostname()) ||
		strings.HasPrefix(u.EscapedPath(), "//") || strings.Contains(u.EscapedPath(), `\`) {
		return raw
	}
	out := u.EscapedPath()
	if out == "" {
		out = "/"
	}
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	if u.Fragment != "" {
		out += "#" + u.EscapedFragment()
	}
	return out
}

// PastedTwitterLink returns the path on this instance for a search query
// that is a twitter.com or x.com page link, with or without its scheme, and
// "" for any other query, including a bare host.
func PastedTwitterLink(q string) string {
	q = strings.TrimSpace(q)
	if q == "" || strings.ContainsFunc(q, unicode.IsSpace) {
		return ""
	}
	if !strings.Contains(q, "://") {
		q = HTTPS + q
	}
	if u, err := url.Parse(q); err != nil || strings.Trim(u.Path, "/") == "" {
		return ""
	}
	if path := LocalizeTwitterLink(q); path != q {
		return path
	}
	return ""
}

// ExtractUsername returns the username from a /<user>/status/<id> URL.
func ExtractUsername(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 3 && parts[1] == "status" {
		return parts[0]
	}
	return ""
}

// ValidateNumber returns value if it's all ASCII digits, and "" otherwise.
func ValidateNumber(value string) string {
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return ""
		}
	}
	return value
}

var langNames = map[string]string{
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
	"yo": "Yoruba", "zu": "Zulu",
}

// LangName returns the English name of a language code such as "pt-BR".
func LangName(code string) string {
	lang := strings.ToLower(code)
	if n, ok := langNames[lang]; ok {
		return n
	}
	base, _, _ := strings.Cut(strings.ReplaceAll(lang, "_", "-"), "-")
	if n, ok := langNames[base]; ok {
		return n
	}
	return code
}
