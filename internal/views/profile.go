// SPDX-License-Identifier: AGPL-3.0-only

package views

import (
	"html/template"
	"strings"

	"github.com/perennialtech/nitter/internal/formatters"
	"github.com/perennialtech/nitter/internal/prefs"
	"github.com/perennialtech/nitter/internal/types"
	"github.com/perennialtech/nitter/internal/utils"
)

type statData struct {
	Class, Header, Num, Href string
}

// RenderUserCard renders the profile card in a profile's sidebar.
func RenderUserCard(u types.User, p *prefs.Prefs, info types.AccountInfo) template.HTML {
	size := "_400x400"
	if p.AutoplayGifs && strings.HasSuffix(u.UserPic, "gif") {
		size = ""
	}
	d := map[string]any{
		"U":         u,
		"Info":      info,
		"AvatarURL": utils.PicURL(formatters.UserPic(u.UserPic, "")),
		"Avatar":    formatters.UserPic(u.UserPic, size),
		"Bio":       template.HTML(formatters.ReplaceURLs(u.Bio, p, "")),
		"JoinURL":   "/" + u.Username + "/about",
		"JoinFull":  formatters.JoinDateFull(u),
		"JoinDate":  formatters.JoinDate(u),
		"Stats": []statData{
			{"posts", "Tweets", insertSep(u.Tweets), ""},
			{"following", "Following", insertSep(u.Following), "/" + u.Username + "/following"},
			{"followers", "Followers", insertSep(u.Followers), "/" + u.Username + "/followers"},
			{"likes", "Likes", insertSep(u.Likes), ""},
		},
	}
	if u.Location != "" {
		place, link := formatters.Location(u.Location)
		switch {
		case len(link) > 1:
		case strings.Contains(place, "://"):
			link = place
		default:
			link = ""
		}
		d["Place"], d["PlaceURL"] = place, link
	}
	if u.Website != "" {
		website := formatters.ReplaceURLs(u.Website, p, "")
		d["Website"], d["WebsiteText"] = website, formatters.ShortLink(website)
	}
	return exec("userCard", d)
}

type railPhoto struct{ Href, Pic string }

func renderPhotoRail(profile *types.Profile) template.HTML {
	var photos []railPhoto
	for i, photo := range profile.PhotoRail {
		if i == 16 {
			break
		}
		suffix := ":thumb"
		if strings.Contains(photo.URL, "format") || strings.Contains(photo.URL, "placeholder") {
			suffix = ""
		}
		photos = append(photos, railPhoto{
			Href: "/" + profile.User.Username + "/status/" + photo.TweetID + "#m",
			Pic:  photo.URL + suffix,
		})
	}
	return exec("photoRail", map[string]any{
		"MediaURL": "/" + profile.User.Username + "/media",
		"Label":    insertSep(profile.User.Media) + " Photos and videos",
		"Photos":   photos,
	})
}

func renderBanner(banner string) template.HTML {
	d := map[string]any{"Banner": banner}
	switch {
	case banner == "":
	case strings.HasPrefix(banner, "#"):
		d["Color"] = template.CSS("background-color: " + banner + ";")
	default:
		d["URL"] = utils.PicURL(banner)
	}
	return exec("banner", d)
}

// RenderProtected renders the notice for a protected account's tweets.
func RenderProtected(username string) template.HTML {
	return exec("protected", "Only confirmed followers have access to @"+username+"'s tweets.")
}

// RenderProfile renders a profile page. Like the Nim proc it sets the
// timeline query's FromUser to the profile's user.
func RenderProfile(profile *types.Profile, p *prefs.Prefs, path string) template.HTML {
	profile.Tweets.Query.FromUser = []string{profile.User.Username}
	q := profile.Tweets.Query
	isGallery := q.Kind == types.QueryMedia && q.View == "gallery"
	d := map[string]any{"Class": "profile-tabs"}
	if isGallery {
		d["Class"] = "profile-tabs media-only"
	}
	if !isGallery && !p.HideBanner {
		d["Banner"] = renderBanner(profile.User.Banner)
	}
	if !isGallery {
		d["TabClass"] = profileTabClass(p)
		card := RenderUserCard(profile.User, p, profile.AccountInfo)
		if len(profile.PhotoRail) > 0 {
			card += renderPhotoRail(profile)
		}
		d["Card"] = card
	}
	if profile.User.Protected {
		d["Body"] = RenderProtected(profile.User.Username)
	} else {
		d["Body"] = RenderTweetSearch(profile.Tweets, p, path, profile.Pinned)
	}
	return exec("profile", d)
}

func profileTabClass(p *prefs.Prefs) string {
	if p.StickyProfile {
		return "profile-tab sticky"
	}
	return "profile-tab"
}

// RenderUserList renders a profile's followers or following list.
// activeTab is "Followers" or "Following".
func RenderUserList(u types.User, results types.UsersTimeline, p *prefs.Prefs, path, activeTab string) template.HTML {
	// all results fit on the first page
	if results.Beginning {
		expected := u.Following
		if activeTab == "Followers" {
			expected = u.Followers
		}
		if len(results.Content) >= expected {
			results.Bottom = ""
		}
	}
	d := map[string]any{
		"Class":    "profile-tabs",
		"TabClass": profileTabClass(p),
		"Card":     RenderUserCard(u, p, types.AccountInfo{}),
	}
	if !p.HideBanner {
		d["Banner"] = renderBanner(u.Banner)
	}
	tabs := exec("tabs", map[string]any{"Class": "tab", "Tabs": []tab{
		{activeTab0(activeTab, "Following"), "/" + u.Username + "/following", "Following"},
		{activeTab0(activeTab, "Followers"), "/" + u.Username + "/followers", "Followers"},
	}})
	d["Body"] = exec("container", map[string]any{"Class": "timeline-container", "Parts": []template.HTML{
		tabs, RenderTimelineUsers(results, p, path),
	}})
	return exec("profile", d)
}

func activeTab0(active, name string) string { return activeTab(active == name) }
