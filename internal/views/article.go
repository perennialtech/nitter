// SPDX-License-Identifier: AGPL-3.0-only

package views

import (
	"html/template"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/perennialtech/nitter/internal/formatters"
	"github.com/perennialtech/nitter/internal/prefs"
	"github.com/perennialtech/nitter/internal/types"
	"github.com/perennialtech/nitter/internal/utils"
)

// node is a minimal element tree for article paragraphs, which Karax
// built node by node.
type node struct {
	tag   string // "" for a text node
	text  string
	attrs [][2]string
	html  template.HTML // pre-rendered content, emitted as-is
	kids  []*node
}

func textNode(s string) *node { return &node{text: s} }

func elem(tag string, kids ...*node) *node { return &node{tag: tag, kids: kids} }

func (n *node) add(kids ...*node) { n.kids = append(n.kids, kids...) }

func (n *node) setAttr(k, v string) {
	for i := range n.attrs {
		if n.attrs[i][0] == k {
			n.attrs[i][1] = v
			return
		}
	}
	n.attrs = append(n.attrs, [2]string{k, v})
}

var voidTags = []string{"br", "hr", "img"}

func (n *node) render(b *strings.Builder) {
	switch {
	case n.html != "":
		b.WriteString(string(n.html))
		return
	case n.tag == "":
		b.WriteString(escapeText(n.text))
		return
	}
	b.WriteString("<" + n.tag)
	for _, a := range n.attrs {
		b.WriteString(" " + a[0] + `="` + escapeText(a[1]) + `"`)
	}
	b.WriteString(">")
	if slices.Contains(voidTags, n.tag) {
		return
	}
	for _, k := range n.kids {
		k.render(b)
	}
	b.WriteString("</" + n.tag + ">")
}

func renderAtomicParagraph(par types.ArticleParagraph, a *types.Article, tweets map[int64]*types.Tweet,
	path string, p *prefs.Prefs) template.HTML {
	if len(par.EntityRanges) == 0 {
		return ""
	}
	entity, ok := a.Entities[par.EntityRanges[0].Key]
	if !ok {
		return ""
	}
	switch entity.Kind {
	case "MEDIA":
		var items []map[string]string
		for _, id := range entity.MediaIDs {
			m := a.Media[id]
			if m.URL == "" {
				continue
			}
			items = append(items, map[string]string{
				"Kind": m.Kind, "Vid": utils.VidURL(m.URL), "Orig": utils.OrigPicURL(m.URL),
				"Small": smallPic(m.URL),
			})
		}
		return exec("articleMedia", map[string]any{"Items": items, "Caption": entity.Caption})
	case "TWEET":
		id, _ := strconv.ParseInt(entity.TweetID, 10, 64)
		if t := tweets[id]; t != nil {
			return RenderTweet(t, p, path, TweetOpts{})
		}
	case "MARKDOWN":
		content := entity.Markdown
		if strings.HasPrefix(content, "```") {
			if nl := strings.IndexByte(content, '\n'); nl >= 0 {
				content = content[nl+1:]
			}
			content = strings.TrimSuffix(content, "```")
			content = strings.TrimSpace(content)
		}
		return exec("articleCode", content)
	case "DIVIDER":
		return `<hr class="article-divider">`
	}
	return ""
}

func wrapStyle(n *node, style int) *node {
	if style&(1<<4) != 0 {
		n = elem("code", n)
	}
	if style&(1<<0) != 0 {
		n = elem("strong", n)
	}
	if style&(1<<1) != 0 {
		n = elem("em", n)
	}
	if style&(1<<2) != 0 {
		n = elem("del", n)
	}
	if style&(1<<3) != 0 {
		n = elem("u", n)
	}
	return n
}

func isWordByte(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_'
}

// addContent adds text to target, splitting lines with <br> and linking
// @mentions.
func addContent(target *node, content string, style int) {
	for li, line := range strings.Split(content, "\n") {
		if li > 0 {
			target.add(elem("br"))
		}
		pos := 0
		for pos < len(line) {
			at := strings.IndexByte(line[pos:], '@')
			if at == -1 {
				target.add(wrapStyle(textNode(line[pos:]), style))
				break
			}
			at += pos
			if at > 0 && isWordByte(line[at-1]) {
				target.add(wrapStyle(textNode(line[pos:at+1]), style))
				pos = at + 1
				continue
			}
			j := at + 1
			for j < len(line) && j-at-1 < 15 && isWordByte(line[j]) {
				j++
			}
			if j == at+1 {
				target.add(wrapStyle(textNode(line[pos:at+1]), style))
				pos = at + 1
				continue
			}
			if at > pos {
				target.add(wrapStyle(textNode(line[pos:at]), style))
			}
			username := line[at+1 : j]
			link := elem("a", wrapStyle(textNode("@"+username), style))
			link.setAttr("href", "/"+username)
			target.add(link)
			pos = j
		}
	}
}

var styleBits = map[string]int{
	"Bold": 1 << 0, "Italic": 1 << 1, "Strikethrough": 1 << 2, "Underline": 1 << 3, "Code": 1 << 4,
}

func applyInlineStyles(target *node, runes []rune, start, length int, styles []types.ArticleStyle) {
	end := min(start+length, len(runes))
	start = min(start, end)
	if len(styles) == 0 {
		addContent(target, string(runes[start:end]), 0)
		return
	}
	lastStyle, lastStart := 0, start
	for i := start; i < end; i++ {
		style := 0
		for _, sr := range styles {
			if sr.Offset <= i && sr.Offset+sr.Length > i {
				style |= styleBits[sr.Style]
			}
		}
		if style != lastStyle {
			if i > lastStart {
				addContent(target, string(runes[lastStart:i]), lastStyle)
			}
			lastStyle, lastStart = style, i
		}
	}
	if lastStart < end {
		addContent(target, string(runes[lastStart:end]), lastStyle)
	}
}

func renderTextParagraph(par types.ArticleParagraph, a *types.Article) *node {
	var result *node
	switch par.Kind {
	case "header-one":
		result = elem("h1")
	case "header-two":
		result = elem("h2")
	case "header-three":
		result = elem("h3")
	case "ordered-list-item", "unordered-list-item":
		result = elem("li")
	case "blockquote":
		result = elem("blockquote")
	case "code-block":
		return elem("pre", elem("code", textNode(par.Text)))
	default:
		result = elem("p")
	}

	runes := []rune(par.Text)
	last := 0
	for _, er := range par.EntityRanges {
		if er.Offset > last {
			applyInlineStyles(result, runes, last, er.Offset-last, par.InlineStyles)
		}
		last = er.Offset + er.Length

		target := result
		if entity, ok := a.Entities[er.Key]; ok && entity.Kind == "LINK" {
			if u, err := url.Parse(entity.URL); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
				target = elem("a")
				if utils.IsTwitterURL(entity.URL) {
					target.setAttr("href", u.EscapedPath())
				} else {
					target.setAttr("href", entity.URL)
				}
			}
		}
		applyInlineStyles(target, runes, er.Offset, er.Length, par.InlineStyles)
		if target != result {
			result.add(target)
		}
	}
	if last < len(runes) {
		applyInlineStyles(result, runes, last, len(runes)-last, par.InlineStyles)
	}

	if par.Kind == "blockquote" && len(result.kids) > 0 {
		lastChild := result.kids[len(result.kids)-1]
		if lastChild.tag == "strong" && len(lastChild.kids) > 0 && lastChild.kids[0].tag == "" {
			lastChild.setAttr("class", "blockquote-attribution")
		}
	}
	return result
}

// RenderArticle renders an X article. tweets holds the tweets the article
// embeds, by ID; tweetID is the article's own tweet.
func RenderArticle(a *types.Article, tweets map[int64]*types.Tweet, path string, p *prefs.Prefs,
	tweetID string) template.HTML {
	author := a.User
	var stats template.HTML
	if !p.HideTweetStats {
		statsID, _ := strconv.ParseInt(tweetID, 10, 64)
		stats = RenderStats(a.Stats, p, &types.Tweet{ID: statsID, User: author})
	}

	body := elem("")
	var list *node
	listKind := ""
	for _, par := range a.Paragraphs {
		isListItem := par.Kind == "ordered-list-item" || par.Kind == "unordered-list-item"
		if !isListItem && list != nil {
			body.add(list)
			list, listKind = nil, ""
		}
		switch {
		case par.Kind == "atomic":
			body.add(&node{html: renderAtomicParagraph(par, a, tweets, path, p)})
		case isListItem:
			if par.Kind != listKind {
				if list != nil {
					body.add(list)
				}
				if par.Kind == "ordered-list-item" {
					list = elem("ol")
				} else {
					list = elem("ul")
				}
				listKind = par.Kind
			}
			list.add(renderTextParagraph(par, a))
		default:
			body.add(renderTextParagraph(par, a))
		}
	}
	if list != nil {
		body.add(list)
	}
	var b strings.Builder
	for _, k := range body.kids {
		k.render(&b)
	}

	return exec("article", map[string]any{
		"A":           a,
		"Author":      author,
		"AvatarClass": avatarClass(p),
		"DateURL":     "/" + author.Username + "/status/" + tweetID,
		"Date":        formatters.ShortTime(a.Time),
		"Stats":       stats,
		"CoverURL":    utils.OrigPicURL(a.CoverImage),
		"CoverSrc":    smallPic(a.CoverImage),
		"Body":        template.HTML(b.String()),
	})
}
