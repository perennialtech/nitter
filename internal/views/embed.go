// SPDX-License-Identifier: AGPL-3.0-only

package views

import (
	"html/template"
	"net/http"
	"strconv"

	"github.com/perennialtech/nitter/internal/config"
	"github.com/perennialtech/nitter/internal/formatters"
	"github.com/perennialtech/nitter/internal/prefs"
	"github.com/perennialtech/nitter/internal/types"
)

func renderEmbedPage(head, body template.HTML) string {
	return string(exec("embedPage", map[string]any{"Head": head, "Body": body, "JS": embedResizeJS}))
}

// RenderVideoEmbed renders the /i/videos/<id> player page for a tweet's
// first video. The tweet must have a video.
func RenderVideoEmbed(t *types.Tweet, cfg *config.Config, r *http.Request) string {
	video := t.Videos()[0]
	p := &prefs.Prefs{HlsPlayback: true, Mp4Playback: true, ProxyVideos: prefs.Defaults.ProxyVideos}
	head := RenderHead(p, cfg, r, PageOpts{
		Video: formatters.VideoEmbed(cfg, t.ID), Images: []string{video.Thumb},
	})
	return renderEmbedPage(head, exec("videoEmbed", map[string]any{
		"Video":    RenderVideo(video, p, "", false),
		"Link":     formatters.TweetLink(t, true),
		"Hostname": cfg.Hostname,
	}))
}

// RenderTweetEmbed renders the embeddable tweet page.
func RenderTweetEmbed(t *types.Tweet, path string, p *prefs.Prefs, cfg *config.Config, r *http.Request) string {
	return renderEmbedPage(RenderHead(p, cfg, r, PageOpts{}), exec("tweetEmbed", map[string]any{
		"Tweet":    RenderTweet(t, p, path, TweetOpts{MainTweet: true}),
		"Link":     formatters.TweetLink(t, true),
		"Hostname": cfg.Hostname,
	}))
}

// RenderErrorEmbed renders an embed page showing msg. tweetID and
// username may be empty.
func RenderErrorEmbed(msg string, p *prefs.Prefs, cfg *config.Config, r *http.Request,
	tweetID, username string) string {
	link := "/"
	if tweetID != "" {
		if username != "" {
			link = "/" + username + "/status/" + tweetID
		} else {
			link = "/i/status/" + tweetID
		}
	}
	return renderEmbedPage(RenderHead(p, cfg, r, PageOpts{}), exec("errorEmbed", map[string]any{
		"Error": msg, "Link": link, "Hostname": cfg.Hostname,
	}))
}

// RenderOembedIframe renders the iframe in an oEmbed response.
func RenderOembedIframe(embedURL string, maxwidth int) string {
	return execText("oembed", map[string]string{"URL": embedURL, "MaxWidth": strconv.Itoa(maxwidth)})
}
