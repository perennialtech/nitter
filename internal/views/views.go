// SPDX-License-Identifier: AGPL-3.0-only

// Package views renders nitter's pages, RSS feeds and embeds. It is a port
// of the Karax views in src/views: HTML structure, class names and links
// match the Nim output so the integration tests in tests/ apply unchanged.
//
// All templates are parsed at init. A failed template execution is a
// programmer error and panics.
package views

import (
	"embed"
	"html"
	"html/template"
	"io/fs"
	"net/url"
	"strconv"
	"strings"
	texttemplate "text/template"

	"github.com/perennialtech/nitter/internal/flags"
	"github.com/perennialtech/nitter/internal/formatters"
	"github.com/perennialtech/nitter/internal/prefs"
	"github.com/perennialtech/nitter/internal/types"
	"github.com/perennialtech/nitter/internal/utils"
)

//go:embed templates
var templateFS embed.FS

var (
	htmlT *template.Template
	textT *texttemplate.Template
)

func init() {
	htmlT = template.Must(template.New("").Option("missingkey=zero").Funcs(htmlFuncs()).
		ParseFS(templateFS, "templates/*.html"))
	sub, err := fs.Sub(templateFS, "templates/text")
	if err != nil {
		panic(err)
	}
	textT = texttemplate.Must(texttemplate.New("").Funcs(textFuncs()).ParseFS(sub, "*"))
}

// exec renders a named HTML template.
func exec(name string, data any) template.HTML {
	var b strings.Builder
	if err := htmlT.ExecuteTemplate(&b, name, data); err != nil {
		panic("views: " + err.Error())
	}
	return template.HTML(b.String())
}

// execText renders a named text template (RSS, OpenSearch, oEmbed).
func execText(name string, data any) string {
	var b strings.Builder
	if err := textT.ExecuteTemplate(&b, name, data); err != nil {
		panic("views: " + err.Error())
	}
	return b.String()
}

func htmlFuncs() template.FuncMap {
	return template.FuncMap{
		// Karax emitted attribute values as-is; template.URL keeps
		// html/template from replacing non-http(s) URLs with #ZgotmplZ.
		"url":               func(s string) template.URL { return template.URL(s) },
		"icon":              icon,
		"linkUser":          linkUser,
		"verifiedIcon":      verifiedIcon,
		"profileLabel":      profileLabel,
		"genImg":            genImg,
		"hiddenField":       hiddenField,
		"genCheckbox":       genCheckbox,
		"genInput":          genInput,
		"genDate":           genDate,
		"genNumberInput":    genNumberInput,
		"miniAvatar":        RenderMiniAvatar,
		"userPic":           formatters.UserPic,
		"time":              formatters.Time,
		"shortTime":         formatters.ShortTime,
		"tweetLink":         formatters.TweetLink,
		"langName":          utils.LangName,
		"encodeURL":         utils.EncodeURL,
		"participantAvatar": participantAvatar,
		"withTr":            withTrParam,
		"notLast":           func(i, n int) bool { return i < n-1 },
		"inc":               func(i int) int { return i + 1 },
		"showRetweetTime":   func() bool { return flags.ShowRetweetTime },
		"renderToTop":       RenderToTop,
		"articleCard":       renderArticleCard,
		"reply":             renderReply,
	}
}

const (
	smallWebp  = "?name=small&format=webp"
	mediumWebp = "?name=medium&format=webp"
)

func smallPic(u string) string {
	if u == "" {
		return ""
	}
	if !strings.Contains(u, "?") && !strings.HasSuffix(u, "placeholder.png") {
		u += smallWebp
	}
	return utils.PicURL(u)
}

func mediumPic(u string) string {
	if u == "" {
		return ""
	}
	if !strings.Contains(u, "?") && !strings.HasSuffix(u, "placeholder.png") {
		u += mediumWebp
	}
	return utils.PicURL(u)
}

// insertSep formats n with thousands separators, like Nim's insertSep($n, ',').
func insertSep(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// encodeURLNoPlus is Nim's encodeUrl(s, usePlus=false): like
// url.QueryEscape, but spaces become %20.
func encodeURLNoPlus(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

// xmlEscape is Nim's xmltree.escape.
func xmlEscape(s string) string {
	return strings.NewReplacer(
		"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;",
	).Replace(s)
}

type iconData struct {
	Class, Title, Href, Label string
}

// icon renders renderutils.icon. kv holds optional key/value pairs:
// "label", "title", "class" and "href".
func icon(name string, kv ...string) template.HTML {
	d := iconData{Class: "icon-" + name}
	for i := 0; i+1 < len(kv); i += 2 {
		switch kv[i] {
		case "label":
			d.Label = kv[i+1]
		case "title":
			d.Title = kv[i+1]
		case "class":
			if kv[i+1] != "" {
				d.Class += " " + kv[i+1]
			}
		case "href":
			d.Href = kv[i+1]
		default:
			panic("views: unknown icon option " + kv[i])
		}
	}
	return exec("icon", d)
}

func isVerified(u types.User) bool {
	return u.VerifiedType != types.VerifiedNone && u.VerifiedType != ""
}

func verifiedIcon(u types.User) template.HTML {
	if !isVerified(u) {
		return ""
	}
	return exec("verifiedIcon", strings.ToLower(string(u.VerifiedType)))
}

func profileLabel(u types.User) template.HTML {
	if u.ParodyLabel == "" {
		return ""
	}
	return exec("profileLabel", u.ParodyLabel+" account")
}

type linkUserData struct {
	Href, Class, Text string
	Lock              bool
}

func linkUser(u types.User, class string) template.HTML {
	isName := !strings.Contains(class, "username")
	d := linkUserData{Href: "/" + u.Username, Class: class}
	if isName {
		d.Text = u.Fullname
		d.Lock = u.Protected
	} else {
		d.Text = "@" + u.Username
	}
	return exec("linkUser", d)
}

func hiddenField(name, value string) template.HTML {
	return exec("hiddenField", map[string]string{"Name": name, "Value": value})
}

func buttonReferer(action, text, path, class, method string) template.HTML {
	return exec("buttonReferer", map[string]string{
		"Action": action, "Text": text, "Path": path, "Class": class, "Method": method,
	})
}

type prefFieldData struct {
	Pref, Label, State, Placeholder, Class, Min string
	Checked, Autofocus                          bool
	Options                                     []string
}

func genCheckbox(pref, label string, state bool) template.HTML {
	return exec("genCheckbox", prefFieldData{Pref: pref, Label: label, Checked: state})
}

func genInput(pref, label, state, placeholder, class string, autofocus bool) template.HTML {
	return exec("genInput", prefFieldData{
		Pref: pref, Label: label, State: state, Placeholder: placeholder, Class: class,
		Autofocus: autofocus && state == "",
	})
}

func genSelect(pref, label, state string, options []string) template.HTML {
	return exec("genSelect", prefFieldData{Pref: pref, Label: label, State: state, Options: options})
}

func genDate(pref, state string) template.HTML {
	return exec("genDate", prefFieldData{Pref: pref, State: state})
}

func genNumberInput(pref, label, state, placeholder, class string, autofocus bool, min string) template.HTML {
	return exec("genNumberInput", prefFieldData{
		Pref: pref, Label: label, State: state, Placeholder: placeholder, Class: class,
		Autofocus: autofocus && state == "", Min: min,
	})
}

// genImg renders renderutils.genImg; opt is the optional class and alt text.
func genImg(u string, opt ...string) template.HTML {
	d := map[string]string{"Src": utils.PicURL(u)}
	if len(opt) > 0 {
		d["Class"] = opt[0]
	}
	if len(opt) > 1 {
		d["Alt"] = opt[1]
	}
	return exec("genImg", d)
}

// kindOf returns q's kind, defaulting to posts like Nim's zero Query.
func kindOf(q types.Query) types.QueryKind {
	if q.Kind == "" {
		return types.QueryPosts
	}
	return q.Kind
}

func tabClass(q types.Query, tab types.QueryKind) string {
	if kindOf(q) == tab {
		return "tab-item active"
	}
	return "tab-item"
}

func activeTab(active bool) string {
	if active {
		return "tab-item active"
	}
	return "tab-item"
}

func avatarClass(p *prefs.Prefs) string {
	if p.SquareAvatars {
		return "avatar"
	}
	return "avatar round"
}

// escapeText escapes text for the HTML built directly in Go (articles).
func escapeText(s string) string { return html.EscapeString(s) }
