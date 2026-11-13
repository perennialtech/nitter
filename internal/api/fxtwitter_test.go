// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tidwall/gjson"

	"github.com/perennialtech/nitter/internal/types"
)

const fxAuthor = `{
  "type": "profile", "id": "11348282", "screen_name": "NASA", "name": "NASA",
  "description": "Making the seemingly impossible, possible.",
  "raw_description": {"text": "Missions at https://t.co/abc and @NASAWebb #space",
    "facets": [{"type": "url", "indices": [12, 28], "original": "https://t.co/abc",
                "replacement": "https://www.nasa.gov/missions", "display": "nasa.gov/missions"}]},
  "location": "Pale Blue Dot", "url": "https://x.com/NASA",
  "avatar_url": "https://pbs.twimg.com/profile_images/1/0ZxKlEKB_200x200.jpg",
  "banner_url": "https://pbs.twimg.com/profile_banners/11348282/1788992290",
  "followers": 92248416, "following": 115, "statuses": 74538, "media_count": 28248,
  "likes": 17106, "protected": false, "joined": "Wed Dec 19 20:20:32 +0000 2007",
  "website": {"url": "http://www.nasa.gov/", "display_url": "nasa.gov"},
  "verification": {"verified": true, "type": "government"}
}`

func status(t *testing.T, body string) string {
	t.Helper()
	s := `{"type": "status", "id": "2120974110335312297", "created_timestamp": 1794514608,
      "author": ` + fxAuthor + `, "replies": 145, "reposts": 576, "quotes": 36,
      "likes": 3974, "views": 397031, "source": null, "embed_card": "tweet", ` + body + "}"
	if !gjson.Valid(s) {
		t.Fatalf("invalid fixture: %s", s)
	}
	return s
}

func fxTweet(t *testing.T, body string) *types.Tweet {
	t.Helper()
	return parseFxTweet(gjson.Parse(status(t, body)))
}

func TestFxUser(t *testing.T) {
	u := parseFxUser(gjson.Parse(fxAuthor))
	if u.ID != "11348282" || u.Username != "NASA" || u.UserPic != "profile_images/1/0ZxKlEKB.jpg" ||
		u.Banner != "profile_banners/11348282/1788992290/1500x500" || u.Tweets != 74538 || u.Media != 28248 ||
		u.Website != "http://www.nasa.gov/" || u.VerifiedType != types.VerifiedGovernment || u.JoinDate.Year() != 2007 {
		t.Fatalf("%+v", u)
	}
	// bio links, mentions and hashtags are expanded
	for _, want := range []string{`href="https://www.nasa.gov/missions"`, `href="/NASAWebb"`, "#space</a>"} {
		if !strings.Contains(u.Bio, want) {
			t.Errorf("bio %q lacks %s", u.Bio, want)
		}
	}
}

func TestFxTextFacets(t *testing.T) {
	tw := fxTweet(t, `
      "raw_text": {"text": "Tethys &amp; Dione via @NASAWebb #Saturn https://t.co/link https://t.co/media",
        "display_text_range": [0, 63],
        "facets": [
          {"type": "mention", "indices": [23, 32], "original": "NASAWebb", "id": "1"},
          {"type": "hashtag", "indices": [33, 40], "original": "Saturn"},
          {"type": "url", "indices": [41, 63], "original": "https://t.co/link",
           "replacement": "https://science.nasa.gov/saturn/", "display": "science.nasa.gov/saturn/"},
          {"type": "media", "indices": [64, 87], "original": "https://t.co/media"}]},
      "media": {"all": [{"type": "photo", "url": "https://pbs.twimg.com/media/HW82tqYWoAAnWCO.jpg?name=orig",
                         "altText": "Saturn"}]}`)
	if tw.ID != 2120974110335312297 || tw.Stats.Retweets != 576 || tw.Stats.Views != 397031 {
		t.Fatalf("%+v", tw)
	}
	if !strings.HasPrefix(tw.Text, `Tethys &amp; Dione via <a href="/NASAWebb"`) ||
		!strings.Contains(tw.Text, `href="/search?f=tweets&q=%23Saturn">#Saturn</a>`) ||
		!strings.Contains(tw.Text, `href="https://science.nasa.gov/saturn/"`) ||
		strings.Contains(tw.Text, "t.co/media") {
		t.Fatalf("text %q", tw.Text)
	}
	if len(tw.Media) != 1 || tw.Media[0].Photo.URL != "media/HW82tqYWoAAnWCO.jpg" || tw.Media[0].Photo.AltText != "Saturn" {
		t.Fatalf("media %+v", tw.Media)
	}
}

func TestFxReplyMentions(t *testing.T) {
	tw := fxTweet(t, `
      "replying_to": {"screen_name": "_itsforkb", "status": "2121073525142507992"},
      "raw_text": {"text": "@_itsforkb @other take care", "display_text_range": [18, 27],
        "facets": [{"type": "mention", "indices": [0, 10], "original": "_itsforkb"},
                   {"type": "mention", "indices": [11, 17], "original": "other"}]}`)
	if tw.ReplyID != 2121073525142507992 || !slices.Equal(tw.Reply, []string{"_itsforkb", "other"}) || tw.Text != "take care" {
		t.Fatalf("%+v", tw)
	}
}

func TestFxQuoteHidesTrailingLink(t *testing.T) {
	tw := fxTweet(t, `
      "raw_text": {"text": "look https://t.co/q", "display_text_range": [0, 19],
        "facets": [{"type": "url", "indices": [5, 19], "original": "https://t.co/q",
                    "replacement": "https://x.com/MCRmyPH/status/2120863244055384470"}]},
      "quote": {"type": "tombstone", "reason": "deleted", "message": "This post was deleted"}`)
	if tw.Text != "look" || tw.Quote == nil || tw.Quote.Text != "This post was deleted" {
		t.Fatalf("%+v", tw)
	}
}

func TestFxVideo(t *testing.T) {
	tw := fxTweet(t, `
      "raw_text": {"text": "", "display_text_range": [0, 0], "facets": []},
      "media": {"all": [{"type": "video", "duration": 29.7,
        "url": "https://video.twimg.com/amplify_video/1/vid/avc1/1080x1350/a.mp4?tag=29",
        "thumbnail_url": "https://pbs.twimg.com/amplify_video_thumb/1/img/b.jpg",
        "formats": [
          {"container": "m3u8", "url": "https://video.twimg.com/amplify_video/1/pl/c.m3u8?tag=29"},
          {"container": "mp4", "bitrate": 632000,
           "url": "https://video.twimg.com/amplify_video/1/vid/avc1/320x400/d.mp4?tag=29"}]}]}`)
	if len(tw.Media) != 1 {
		t.Fatalf("media %+v", tw.Media)
	}
	v := tw.Media[0].Video
	if v.PlaybackType != types.VideoM3u8 {
		t.Errorf("playback type = %q, want %q", v.PlaybackType, types.VideoM3u8)
	}
	if v.Thumb != "amplify_video_thumb/1/img/b.jpg" || v.DurationMs != 29700 || len(v.Variants) != 2 ||
		v.Variants[0].ContentType != types.VideoM3u8 || v.Variants[1].Bitrate != 632000 || v.Variants[1].Resolution != 400 {
		t.Fatalf("video %+v", v)
	}
}

func TestFxRepost(t *testing.T) {
	tw := fxTweet(t, `
      "raw_text": {"text": "orig", "display_text_range": [0, 4], "facets": []},
      "reposted_by": {"id": "12", "name": "jack", "screen_name": "jack"}`)
	if tw.User.Username != "jack" || tw.Retweet == nil || tw.Retweet.User.Username != "NASA" || tw.Retweet.Text != "orig" {
		t.Fatalf("%+v", tw)
	}
}

func TestFxPoll(t *testing.T) {
	ends := time.Now().Add(50 * time.Hour).UTC().Format("2006-01-02T15:04:05.000Z")
	p := parseFxPoll(gjson.Parse(`{"choices":[{"label":"a","count":1},{"label":"b","count":3}],
		"total_votes":4,"ends_at":"` + ends + `"}`))
	if p.Leader != 1 || p.Votes != 4 || p.Status != "2 days" {
		t.Fatalf("%+v", p)
	}
	p = parseFxPoll(gjson.Parse(`{"choices":[],"ends_at":"2001-01-01T00:00:00.000Z"}`))
	if p.Status != "Final results" {
		t.Fatalf("%+v", p)
	}
	if got := nimDuration(time.Hour + 2*time.Minute + 3*time.Second); got != "1 hour, 2 minutes, and 3 seconds" {
		t.Fatalf("nimDuration: %q", got)
	}
	if got := nimDuration(8 * 24 * time.Hour); got != "1 week and 1 day" {
		t.Fatalf("nimDuration: %q", got)
	}
}

func TestFxTimeline(t *testing.T) {
	tl := parseFxTimeline(gjson.Parse(`{"code": 200, "cursor": {"top": "t", "bottom": "b"},
      "results": [`+status(t, `"raw_text": {"text": "a", "display_text_range": [0, 1], "facets": []}`)+"]}"), "")
	if !tl.Beginning || tl.Bottom != "b" || len(tl.Content) != 1 {
		t.Fatalf("%+v", tl)
	}
}

func TestFxUsers(t *testing.T) {
	users := parseFxUsers(gjson.Parse(`{"code": 200, "cursor": {"top": null, "bottom": "b"},
      "results": [`+fxAuthor+"]}"), "c", types.QueryFollowers)
	if users.Beginning || users.Query.Kind != types.QueryFollowers || len(users.Content) != 1 || users.Content[0].Username != "NASA" {
		t.Fatalf("%+v", users)
	}
}
