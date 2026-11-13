// SPDX-License-Identifier: AGPL-3.0-only

package views

import (
	"fmt"
	"html/template"
	"slices"
	"strconv"
	"strings"

	"github.com/perennialtech/nitter/internal/flags"
	"github.com/perennialtech/nitter/internal/formatters"
	"github.com/perennialtech/nitter/internal/prefs"
	"github.com/perennialtech/nitter/internal/types"
	"github.com/perennialtech/nitter/internal/utils"
)

// RenderMiniAvatar renders a user's small avatar.
func RenderMiniAvatar(u types.User, p *prefs.Prefs) template.HTML {
	return genImg(formatters.UserPic(u.UserPic, "_mini"), avatarClass(p)+" mini")
}

func renderArticleCard(a *types.ArticlePreview) template.HTML {
	return exec("articleCard", map[string]any{
		"URL": "/i/article/" + strconv.FormatInt(a.TweetID, 10), "A": a,
	})
}

func renderReply(t *types.Tweet) template.HTML {
	return exec("reply", t.Reply)
}

type headerData struct {
	T, Full                 *types.Tweet
	Pinned                  bool
	PinnedLabel             string
	Retweet, Retweeter      string
	AvatarSize, AvatarClass string
}

func renderPhotoAttachment(photo types.Photo, bigThumb bool) template.HTML {
	thumb := photo.URL
	switch {
	case strings.Contains(photo.URL, "name="):
	case bigThumb:
		thumb += mediumWebp
	default:
		thumb += smallWebp
	}
	return exec("photoAttachment", map[string]any{
		"Orig": utils.OrigPicURL(photo.URL), "Thumb": thumb, "Alt": photo.AltText,
	})
}

func isPlaybackEnabled(p *prefs.Prefs, t types.VideoType) bool {
	if t == types.VideoMp4 {
		return p.Mp4Playback
	}
	return p.HlsPlayback
}

func hasMp4URL(v types.Video) bool {
	return slices.ContainsFunc(v.Variants, func(x types.VideoVariant) bool {
		return x.ContentType == types.VideoMp4
	})
}

func renderVideoDisabled(t types.VideoType, path string) template.HTML {
	var button template.HTML
	if t == types.VideoMp4 {
		button = buttonReferer("/enablemp4", "Enable mp4 playback", path, "", "post")
	} else {
		button = buttonReferer("/enablehls", "Enable hls playback", path, "", "post")
	}
	return exec("videoOverlay", button)
}

// bestVariant returns the URL of the last variant of type t with the
// highest key, like Nim's sortedByIt(key)[^1].
func bestVariant(v types.Video, t types.VideoType, key func(types.VideoVariant) int) (string, bool) {
	var best *types.VideoVariant
	for i := range v.Variants {
		x := &v.Variants[i]
		if x.ContentType == t && (best == nil || key(*x) >= key(*best)) {
			best = x
		}
	}
	if best == nil {
		return "", false
	}
	return best.URL, true
}

func videoDownloadURL(v types.Video) string {
	best, ok := bestVariant(v, types.VideoMp4, func(x types.VideoVariant) int { return x.Bitrate })
	if !ok {
		return ""
	}
	if strings.HasPrefix(best, "http") {
		return utils.VidURL(best)
	}
	return best
}

type videoData struct {
	V           types.Video
	Thumb       string
	Overlay     template.HTML
	Playable    bool
	MP4         bool
	Source      string
	Muted       bool
	Duration    string
	DownloadURL string
}

func renderVideoAttachment(v types.Video, p *prefs.Prefs, path string, bigThumb bool) template.HTML {
	playbackType := v.PlaybackType
	if !p.ProxyVideos && hasMp4URL(v) {
		playbackType = types.VideoMp4
	}
	d := videoData{V: v, Muted: p.MuteVideos, MP4: playbackType == types.VideoMp4}
	if bigThumb {
		d.Thumb = mediumPic(v.Thumb)
	} else {
		d.Thumb = smallPic(v.Thumb)
	}
	switch {
	case !v.Available:
		msg := "This media is unavailable"
		if v.Reason == "dmcaed" {
			msg = "This media has been disabled in response to a report by the copyright owner"
		}
		d.Overlay = exec("videoUnavailable", msg)
	case !isPlaybackEnabled(p, playbackType):
		d.Overlay = renderVideoDisabled(playbackType, path)
	default:
		d.Playable = true
		vidURL, _ := bestVariant(v, playbackType, func(x types.VideoVariant) int { return x.Resolution })
		d.Source = vidURL
		if p.ProxyVideos && strings.HasPrefix(vidURL, "http") {
			d.Source = utils.VidURL(vidURL)
		}
		if v.DurationMs > 0 {
			d.Duration = formatters.Duration(v.DurationMs)
		}
	}
	if v.Available {
		d.DownloadURL = videoDownloadURL(v)
	}
	return exec("videoAttachment", d)
}

// RenderVideo renders a video attachment with its title card, if any.
func RenderVideo(v types.Video, p *prefs.Prefs, path string, bigThumb bool) template.HTML {
	return exec("video", map[string]any{
		"V":          v,
		"HasCard":    v.Description != "" || v.Title != "",
		"Attachment": renderVideoAttachment(v, p, path, bigThumb),
	})
}

func renderGifAttachment(g types.Gif, p *prefs.Prefs, path string) template.HTML {
	d := map[string]any{
		"G": g, "P": p, "Thumb": smallPic(g.Thumb), "Src": utils.PicURL(g.URL),
	}
	if !p.Mp4Playback {
		d["Disabled"] = renderVideoDisabled(types.VideoMp4, path)
	}
	return exec("gifAttachment", d)
}

type mediaRow struct {
	Class string
	Style template.CSS
	Items []template.HTML
}

func renderMedia(media []types.Media, p *prefs.Prefs, path string, bigThumb bool) template.HTML {
	if len(media) == 0 {
		return ""
	}
	if len(media) == 1 {
		switch media[0].Kind {
		case types.VideoMedia:
			return RenderVideo(media[0].Video, p, path, bigThumb)
		case types.GifMedia:
			return exec("gif", renderGifAttachment(media[0].Gif, p, path))
		}
	}

	groups := [][]types.Media{media}
	if len(media) >= 3 {
		// Nim's distribute(2): the first group takes the remainder
		half := (len(media) + 1) / 2
		groups = [][]types.Media{media[:half], media[half:]}
	}

	var rows []mediaRow
	for i, group := range groups {
		row := mediaRow{Class: "gallery-row"}
		if i > 0 {
			row.Style = "margin-top: .25em;"
		}
		for _, m := range group {
			if m.Kind != types.PhotoMedia {
				row.Class = "gallery-row mixed-row"
			}
			switch m.Kind {
			case types.PhotoMedia:
				row.Items = append(row.Items, renderPhotoAttachment(m.Photo, bigThumb))
			case types.VideoMedia:
				row.Items = append(row.Items, renderVideoAttachment(m.Video, p, path, bigThumb))
			case types.GifMedia:
				row.Items = append(row.Items, renderGifAttachment(m.Gif, p, path))
			}
		}
		rows = append(rows, row)
	}
	return exec("media", rows)
}

type pollOption struct {
	Leader, Perc, Option string
	Width                template.CSS
}

// pollPercent mimics Nim's (&"{perc:>3.0f}").strip(chars={'.'}): Nim
// formats 5.0 with no decimals as "5.", and the width counts the dot.
func pollPercent(perc float64, width int) string {
	s := strconv.FormatFloat(perc, 'f', 0, 64) + "."
	for len(s) < width {
		s = " " + s
	}
	return strings.Trim(s, ".")
}

func renderPoll(poll *types.Poll) template.HTML {
	var opts []pollOption
	for i, option := range poll.Options {
		val := 0
		if i < len(poll.Values) {
			val = poll.Values[i]
		}
		var perc float64
		if val > 0 {
			perc = float64(val) / float64(poll.Votes) * 100
		}
		o := pollOption{Perc: pollPercent(perc, 3) + "%", Option: option}
		o.Width = template.CSS("width: " + o.Perc + ";")
		if poll.Leader == i {
			o.Leader = " leader"
		}
		opts = append(opts, o)
	}
	return exec("poll", map[string]any{
		"Options": opts,
		"Info":    fmt.Sprintf("%s votes • %s", insertSep(poll.Votes), poll.Status),
	})
}

var smallCards = []types.CardKind{types.CardApp, types.CardPlayer, types.CardSummary, types.CardStoreLink}

func renderCard(c *types.Card, p *prefs.Prefs, path string) template.HTML {
	d := map[string]any{"C": c, "URL": formatters.ReplaceURLs(c.URL, p, ""), "Large": ""}
	if !slices.Contains(smallCards, c.Kind) {
		d["Large"] = " large"
	}
	if c.Video != nil {
		d["Video"] = RenderVideo(*c.Video, p, path, false)
	}
	return exec("card", d)
}

func formatStat(stat int, abbreviate bool) string {
	switch {
	case stat <= 0:
		return ""
	case !abbreviate:
		return insertSep(stat)
	case stat >= 1_000_000:
		return strconv.FormatFloat(float64(stat)/1_000_000, 'f', 1, 64) + "M"
	case stat >= 1_000:
		return strconv.FormatFloat(float64(stat)/1_000, 'f', 1, 64) + "K"
	}
	return strconv.Itoa(stat)
}

// RenderStats renders a tweet's reply, retweet, quote, like and view counts.
func RenderStats(stats types.TweetStats, p *prefs.Prefs, t *types.Tweet) template.HTML {
	base := "/" + t.User.Username + "/status/" + strconv.FormatInt(t.ID, 10)
	source := strings.ReplaceAll(stats.Source, "Twitter for ", "")
	source = strings.ReplaceAll(source, "Twitter ", "")
	ab := p.AbbreviateStats
	return exec("stats", map[string]any{
		"Replies":      formatStat(stats.Replies, ab),
		"Retweets":     formatStat(stats.Retweets, ab),
		"Quotes":       formatStat(stats.Quotes, ab),
		"Likes":        formatStat(stats.Likes, ab),
		"Views":        formatStat(stats.Views, ab),
		"RetweetsURL":  base + "/retweets",
		"QuotesURL":    base + "/quotes",
		"Birdwatch":    t.HasBirdwatch,
		"BirdwatchURL": "/i/birdwatch/t/" + strconv.FormatInt(t.ID, 10),
		"ShowSource":   !p.HideTweetSource,
		"Source":       formatters.StripHTML(source, false),
	})
}

func renderAttribution(u types.User, p *prefs.Prefs, link string) template.HTML {
	href := link
	if href == "" {
		href = "/" + u.Username
	}
	return exec("attribution", map[string]any{
		"Href": href, "User": u, "Avatar": RenderMiniAvatar(u, p),
	})
}

func translateURL(t *types.Tweet, path string, mainTweet bool) string {
	if mainTweet {
		base, _, _ := strings.Cut(path, "#")
		sep := "?"
		if strings.Contains(base, "?") {
			sep = "&"
		}
		return base + sep + "tr=1#m"
	}
	return formatters.TweetLink(t, false) + "?tr=1#m"
}

func withTrParam(u, value string) string {
	base, frag, hasFrag := strings.Cut(u, "#")
	path, query, hasQuery := strings.Cut(base, "?")
	var params []string
	if hasQuery {
		for _, p := range strings.Split(query, "&") {
			if p != "" && !strings.HasPrefix(p, "tr=") {
				params = append(params, p)
			}
		}
	}
	params = append(params, "tr="+value)
	result := path + "?" + strings.Join(params, "&")
	if hasFrag {
		result += "#" + frag
	}
	return result
}

// RenderTranslateLabel renders the "See translation" link, or the label
// above translated text.
func RenderTranslateLabel(active, requested bool, lang, url, target string) template.HTML {
	return exec("translateLabel", map[string]any{
		"Active": active, "Requested": requested, "Lang": lang, "URL": url, "Target": target,
	})
}

func canTranslateOnDemand(t *types.Tweet) bool {
	return flags.OnDemandTranslation && t.IsTranslatable && t.Translation == "" &&
		!(t.Translatable && !t.ShowTrans)
}

// RenderTranslateOnDemandForm renders the button that asks Grok to
// translate a post or note.
func RenderTranslateOnDemandForm(path, id string, note bool, backPath string) template.HTML {
	title := "Ask Grok to translate this post"
	if note {
		title = "Ask Grok to translate this note"
	}
	return exec("translateForm", map[string]any{
		"Path": path, "Sig": utils.TranslationSignature(id, note), "Back": backPath,
		"Title": title, "Lang": utils.LangName(flags.OnDemandTranslationLang),
	})
}

// NoteCanTranslateOnDemand reports whether a community note gets the
// on-demand translation button.
func NoteCanTranslateOnDemand(flagged, translatable, requested bool, translation string) bool {
	return flags.OnDemandTranslation && flagged && translation == "" &&
		!(translatable && !requested)
}

// renderCommunityNote renders a tweet's community note. t may be nil.
func renderCommunityNote(note string, p *prefs.Prefs, t *types.Tweet, transURL string) template.HTML {
	hasTrans := flags.EnableTranslation && t != nil && t.NoteTranslatable
	active := hasTrans && t.NoteTranslation != "" && (t.ShowTrans || p.AutoTranslate)
	text := note
	if active {
		text = t.NoteTranslation
	}
	onDemand := t != nil && t.NoteID != "" &&
		NoteCanTranslateOnDemand(t.NoteIsTranslatable, t.NoteTranslatable, t.ShowTrans, t.NoteTranslation)

	var trans template.HTML
	switch {
	case onDemand:
		trans = RenderTranslateOnDemandForm("/i/birdwatch/n/"+t.NoteID+"/translate", t.NoteID, true,
			"/i/status/"+strconv.FormatInt(t.ID, 10))
	case hasTrans:
		trans = RenderTranslateLabel(active, t.ShowTrans, t.NoteTransLang, transURL, t.NoteTransTarget)
	}
	return exec("communityNote", map[string]any{
		"Trans": trans, "Text": template.HTML(formatters.ReplaceURLs(text, p, "")),
	})
}

func maxID(ids []int64) int64 {
	if len(ids) == 0 {
		return 0
	}
	return slices.Max(ids)
}

func unavailableText(t *types.Tweet) string {
	switch {
	case t.Tombstone != "":
		return t.Tombstone
	case t.Text != "":
		return t.Text
	}
	return "This tweet is unavailable"
}

func renderQuote(q *types.Tweet, p *prefs.Prefs, path, transURL string) template.HTML {
	if !q.Available {
		href := q.ExpURL
		if href == "" {
			href = formatters.TweetLink(q, false)
		}
		return exec("quoteUnavailable", map[string]any{"Href": href, "Text": unavailableText(q)})
	}

	transOn := flags.EnableTranslation && q.Translatable
	transActive := transOn && q.Translation != "" && (q.ShowTrans || p.AutoTranslate)
	text := q.Text
	if transActive {
		text = q.Translation
	}
	d := map[string]any{
		"Q":            q,
		"P":            p,
		"Link":         formatters.TweetLink(q, true),
		"Text":         template.HTML(formatters.ReplaceURLs(text, p, "")),
		"NewerVersion": len(q.History) > 0 && q.ID != maxID(q.History),
	}
	if transOn {
		d["Trans"] = RenderTranslateLabel(transActive, q.ShowTrans, q.TransLang, transURL, "")
	}
	if len(q.Media) > 0 {
		d["Media"] = renderMedia(q.Media, p, path, false)
	}
	if q.Note != "" && !p.HideCommunityNotes {
		d["Note"] = renderCommunityNote(q.Note, p, q, transURL)
	}
	return exec("quote", d)
}

// RenderDisclosures renders the "Made with AI" and paid partnership labels.
func RenderDisclosures(t *types.Tweet) template.HTML {
	return exec("disclosures", t)
}

// RenderLocation renders a tweet's location, or "" if it has none.
func RenderLocation(t *types.Tweet) string {
	place, u := formatters.Location(t.Location)
	if place == "" {
		return ""
	}
	if len(u) <= 1 {
		u = ""
	}
	return string(exec("location", map[string]string{"Place": place, "URL": u}))
}

// TweetOpts are renderTweet's optional parameters.
type TweetOpts struct {
	Class string
	// Index is the tweet's position in its thread; -1 marks the last.
	Index      int
	Last       bool
	MainTweet  bool
	AfterTweet bool
	BigThumb   bool
}

// RenderTweet renders a timeline item.
func RenderTweet(tweet *types.Tweet, p *prefs.Prefs, path string, o TweetOpts) template.HTML {
	divClass := o.Class
	if o.Index == -1 || o.Last {
		divClass = "thread-last " + o.Class
	}

	if !tweet.Available {
		d := map[string]any{
			"Class":    divClass + "unavailable timeline-item",
			"Username": tweet.User.Username,
			"Link":     formatters.TweetLink(tweet, true),
			"Text":     unavailableText(tweet),
		}
		if tweet.Quote != nil {
			d["Quote"] = renderQuote(tweet.Quote, p, path, "")
		}
		return exec("tweetUnavailable", d)
	}

	full := tweet
	pinned := tweet.Pinned
	var retweet, retweeter string
	t := full
	if t.Retweet != nil {
		t = t.Retweet
		retweet = full.User.Fullname
		retweeter = full.User.Username
	}

	header := headerData{
		T: t, Full: full, Pinned: pinned, Retweet: retweet, Retweeter: retweeter,
		AvatarSize: "_bigger", AvatarClass: avatarClass(p), PinnedLabel: "Pinned Tweet",
	}
	if strings.Contains(path, "/i/communities/") {
		header.PinnedLabel = "Pinned by Community mods"
	}
	if !p.AutoplayGifs && strings.HasSuffix(t.User.UserPic, "gif") {
		header.AvatarSize = "_400x400"
	}

	tweetClass := "tweet-content media-body"
	if p.BidiSupport {
		tweetClass += " tweet-bidi"
	}

	transOn := flags.EnableTranslation && t.Translatable
	quoteTrans := t.Quote != nil && (t.Quote.Translatable || t.Quote.NoteTranslatable)
	transURL := ""
	if flags.EnableTranslation && (t.Translatable || t.NoteTranslatable || quoteTrans) {
		transURL = translateURL(t, path, o.MainTweet)
	}
	transActive := transOn && t.Translation != "" && (t.ShowTrans || p.AutoTranslate)
	text := t.Text
	if transActive {
		text = t.Translation
	}

	d := map[string]any{
		"Class":      "timeline-item " + divClass,
		"T":          t,
		"P":          p,
		"MainTweet":  o.MainTweet,
		"Link":       formatters.TweetLink(t, true),
		"Header":     exec("tweetHeader", header),
		"TweetClass": tweetClass,
		"Text":       template.HTML(formatters.ReplaceURLs(text, p, "") + RenderLocation(t)),
	}

	if !o.AfterTweet && o.Index == 0 && len(t.Reply) > 0 &&
		(len(t.Reply) > 1 || t.Reply[0] != t.User.Username || pinned) {
		d["Reply"] = renderReply(t)
	}
	switch {
	case canTranslateOnDemand(t):
		d["Trans"] = RenderTranslateOnDemandForm(formatters.TweetLink(t, false)+"/translate",
			strconv.FormatInt(t.ID, 10), false, "")
	case transOn:
		d["Trans"] = RenderTranslateLabel(transActive, t.ShowTrans, t.TransLang, transURL, t.TransTarget)
	}
	if t.Attribution != nil {
		d["Attribution"] = renderAttribution(*t.Attribution, p, t.AttributionLink)
	}
	if t.Card != nil && t.Card.Kind != types.CardHidden {
		d["Card"] = renderCard(t.Card, p, path)
	}
	if t.ArticlePreview != nil {
		d["Article"] = renderArticleCard(t.ArticlePreview)
	}
	if len(t.Media) > 0 {
		d["Media"] = renderMedia(t.Media, p, path, o.BigThumb)
	}
	if t.Poll != nil {
		d["Poll"] = renderPoll(t.Poll)
	}
	if t.Quote != nil {
		d["Quote"] = renderQuote(t.Quote, p, path, transURL)
	}
	if t.Note != "" && !p.HideCommunityNotes {
		d["Note"] = renderCommunityNote(t.Note, p, t, transURL)
	}
	if t.IsAI || t.IsAd {
		d["Disclosures"] = RenderDisclosures(t)
	}

	hasEdits := len(t.History) > 1
	isLatest := hasEdits && t.ID == maxID(t.History)
	if o.MainTweet {
		if hasEdits && isLatest {
			d["EditedLink"] = formatters.TweetLink(t, false) + "/history"
		}
		if hasEdits && !isLatest {
			d["LatestLink"] = formatters.Link(maxID(t.History), t.User.Username, true)
		}
	}
	if len(t.MediaTags) > 0 {
		d["MediaTags"] = t.MediaTags
	}
	if !p.HideTweetStats {
		stats := t.Stats
		if retweet != "" && stats.Source == "" {
			stats.Source = full.Stats.Source
		}
		d["Stats"] = RenderStats(stats, p, t)
	}
	return exec("tweet", d)
}
