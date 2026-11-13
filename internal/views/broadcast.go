// SPDX-License-Identifier: AGPL-3.0-only

package views

import (
	"html/template"
	"strconv"
	"strings"
	"time"

	"github.com/perennialtech/nitter/internal/formatters"
	"github.com/perennialtech/nitter/internal/prefs"
	"github.com/perennialtech/nitter/internal/types"
	"github.com/perennialtech/nitter/internal/utils"
)

func streamSource(p *prefs.Prefs, m3u8 string) string {
	if p.ProxyVideos && strings.HasPrefix(m3u8, "http") {
		return utils.VidURL(m3u8)
	}
	return m3u8
}

func stateText(live bool, end time.Time, state string) string {
	switch {
	case live:
		return "LIVE"
	case end.Year() > 1:
		return "Ended " + end.Format("Jan 2, 2006")
	case state != "":
		return state
	}
	return "Ended"
}

func streamDuration(start, end time.Time, offsetMs int) string {
	if start.Year() <= 1 || end.Year() <= 1 {
		return ""
	}
	if ms := int(end.Sub(start).Milliseconds()) - offsetMs; ms > 0 {
		return formatters.Duration(ms)
	}
	return ""
}

// RenderBroadcast renders a live broadcast or its replay.
func RenderBroadcast(bc types.Broadcast, p *prefs.Prefs, path string) template.HTML {
	live := bc.State == "RUNNING"
	d := map[string]any{
		"BC":        bc,
		"P":         p,
		"Live":      live,
		"Thumb":     utils.PicURL(bc.Thumb),
		"Source":    streamSource(p, bc.M3u8URL),
		"Start":     strconv.Itoa(bc.ReplayStart),
		"State":     stateText(live, bc.EndTime, bc.State),
		"Duration":  streamDuration(bc.StartTime, bc.EndTime, bc.ReplayStart*1000),
		"EnableHLS": buttonReferer("/enablehls", "Enable hls playback", path, "", "post"),
		"Avatar":    formatters.UserPic(bc.User.UserPic, "_bigger"),
	}
	if bc.TotalWatched > 0 {
		d["Views"] = insertSep(bc.TotalWatched) + " views"
	}
	return exec("broadcast", d)
}

type participantData struct {
	P    types.SpaceParticipant
	Role string
}

// RenderSpace renders an audio space.
func RenderSpace(sp types.AudioSpace, p *prefs.Prefs, path string) template.HTML {
	live := sp.State == "RUNNING"
	listeners := sp.TotalLiveListeners
	if sp.TotalReplayWatched > 0 {
		listeners = sp.TotalReplayWatched
	}
	var participants []participantData
	for _, a := range sp.Admins {
		role := "Co-host"
		if a.Username == sp.Creator.Username {
			role = "Host"
		}
		participants = append(participants, participantData{a, role})
	}
	for _, s := range sp.Speakers {
		participants = append(participants, participantData{s, ""})
	}
	d := map[string]any{
		"SP":           sp,
		"P":            p,
		"Live":         live,
		"Source":       streamSource(p, sp.M3u8URL),
		"State":        stateText(live, sp.EndTime, sp.State),
		"Duration":     streamDuration(sp.StartTime, sp.EndTime, 0),
		"EnableHLS":    buttonReferer("/enablehls", "Enable hls playback", path, "", "post"),
		"Participants": participants,
	}
	if listeners > 0 {
		d["Listeners"] = insertSep(listeners) + " listeners"
	}
	return exec("space", d)
}

func participantAvatar(u string) string { return strings.ReplaceAll(u, "_normal", "_bigger") }
