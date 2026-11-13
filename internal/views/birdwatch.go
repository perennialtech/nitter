// SPDX-License-Identifier: AGPL-3.0-only

package views

import (
	"html/template"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/perennialtech/nitter/internal/flags"
	"github.com/perennialtech/nitter/internal/formatters"
	"github.com/perennialtech/nitter/internal/prefs"
	"github.com/perennialtech/nitter/internal/types"
)

func translateParam(path string) string {
	noFragment, _, _ := strings.Cut(path, "#")
	sep := "?"
	if strings.Contains(noFragment, "?") {
		sep = "&"
	}
	return noFragment + sep + "tr=1"
}

func noteDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("Jan 2, 2006 · 3:04 PM") + " UTC"
}

func renderBirdwatchNote(note types.BirdwatchNote, p *prefs.Prefs, translate bool, transURL, backPath string) template.HTML {
	transOn := flags.EnableTranslation && note.Translatable
	transActive := transOn && note.Translation != "" && (translate || p.AutoTranslate)
	text := formatters.LinkifyBirdwatchURLs(note.Text)
	if transActive {
		text = note.Translation
	}
	onDemand := note.ID != "" &&
		NoteCanTranslateOnDemand(note.IsTranslatable, note.Translatable, translate, note.Translation)

	label := "Proposed Note"
	cls := "community-note proposed"
	if note.Helpful {
		label = "Community Note"
		cls = "community-note"
	}
	if note.Misleading {
		label += " - Misleading"
		cls += " misleading"
	} else {
		label += " - Not Misleading"
		cls += " not-misleading"
	}

	var trans template.HTML
	switch {
	case onDemand:
		trans = RenderTranslateOnDemandForm("/i/birdwatch/n/"+note.ID+"/translate", note.ID, true, backPath)
	case transOn:
		trans = RenderTranslateLabel(transActive, translate, note.TransLang, transURL, note.TransTarget)
	}

	var meta []template.HTML
	if note.AuthorAlias != "" {
		meta = append(meta, exec("noteAuthor", note.AuthorAlias))
	}
	if d := noteDate(note.CreatedAt); d != "" {
		meta = append(meta, exec("noteDate", d))
	}
	return exec("birdwatchNote", map[string]any{
		"Class": cls, "Label": label, "ID": note.ID, "Trans": trans,
		"Text": template.HTML(text), "Meta": meta,
	})
}

func renderBirdwatchHistoryNote(note types.BirdwatchHistoryNote) template.HTML {
	label := "Proposed Note"
	cls := "community-note proposed"
	if note.Helpful {
		label = "Community Note"
		cls = "community-note"
	}
	var meta []template.HTML
	if note.TweetUsername != "" && note.TweetID != 0 {
		meta = append(meta, exec("noteTweet", map[string]string{
			"Href": "/" + note.TweetUsername + "/status/" + strconv.FormatInt(note.TweetID, 10),
			"Text": "@" + note.TweetUsername + "'s post",
		}))
	}
	if d := noteDate(note.CreatedAt); d != "" {
		meta = append(meta, exec("noteDate", d))
	}
	return exec("birdwatchNote", map[string]any{
		"Class": cls, "Label": label, "ID": note.ID,
		"Text": template.HTML(formatters.LinkifyBirdwatchURLs(note.Text)), "Meta": meta,
	})
}

// RenderBirdwatchHistoryContent renders a contributor's notes and the
// "Load more" link.
func RenderBirdwatchHistoryContent(history types.BirdwatchHistory) template.HTML {
	d := map[string]any{}
	if !slices.ContainsFunc(history.Notes, func(n types.BirdwatchHistoryNote) bool { return n.Text != "" }) {
		d["None"] = "No notes found for this contributor."
	} else {
		var notes []template.HTML
		for _, n := range history.Notes {
			if n.Text != "" {
				notes = append(notes, renderBirdwatchHistoryNote(n))
			}
		}
		d["Notes"] = notes
	}
	if history.Bottom != "" {
		d["More"] = "?cursor=" + encodeURLNoPlus(history.Bottom)
	}
	return exec("birdwatchHistoryContent", d)
}

// RenderBirdwatchHistory renders a community notes contributor's page.
func RenderBirdwatchHistory(history types.BirdwatchHistory) template.HTML {
	return exec("birdwatchContainer", map[string]any{
		"Parts": []template.HTML{
			exec("timelineHeader", "Notes by "+history.Alias),
			RenderBirdwatchHistoryContent(history),
		},
	})
}

func backPath(path string) string {
	p, _, _ := strings.Cut(path, "?")
	p, _, _ = strings.Cut(p, "#")
	return p
}

// RenderBirdwatchSingleNote renders a single community note with its
// tweet. tweet may be nil.
func RenderBirdwatchSingleNote(tweet *types.Tweet, note types.BirdwatchNote, p *prefs.Prefs,
	path string, translate bool) template.HTML {
	var parts []template.HTML
	if tweet != nil && tweet.ID != 0 {
		parts = append(parts, RenderTweet(tweet, p, path, TweetOpts{MainTweet: true}))
	}
	list := []template.HTML{exec("timelineHeader", "Community Note")}
	if note.Text != "" {
		list = append(list, renderBirdwatchNote(note, p, translate, translateParam(path), backPath(path)))
	} else {
		list = append(list, exec("timelineNone", "Note not found."))
	}
	parts = append(parts, exec("birdwatchList", list))
	return exec("birdwatchContainer", map[string]any{"Parts": parts})
}

// RenderCommunityNotesTimeline renders the community notes timeline.
// activeTab is "helpful" or "new".
func RenderCommunityNotesTimeline(tl types.Timeline, p *prefs.Prefs, path, activeTab string) template.HTML {
	return exec("birdwatchContainer", map[string]any{
		"Parts": []template.HTML{
			exec("timelineHeader", "Community Notes"),
			exec("tabs", map[string]any{"Class": "tab", "Tabs": []tab{
				{activeTab0(activeTab, "helpful"), "/i/communitynotes", "Rated Helpful"},
				{activeTab0(activeTab, "new"), "/i/communitynotes/new", "New"},
			}}),
			RenderTimelineTweets(tl, p, path, nil),
		},
	})
}

// RenderBirdwatchNotes renders a tweet's community notes. tweet may be nil.
func RenderBirdwatchNotes(tweet *types.Tweet, notes types.BirdwatchNotes, p *prefs.Prefs,
	path string, translate bool) template.HTML {
	var parts []template.HTML
	if tweet != nil && tweet.ID != 0 {
		parts = append(parts, RenderTweet(tweet, p, path, TweetOpts{MainTweet: true}))
	}
	list := []template.HTML{exec("timelineHeader", "Community Notes")}
	if !slices.ContainsFunc(notes.Notes, func(n types.BirdwatchNote) bool { return n.Text != "" }) {
		list = append(list, exec("timelineNone", "No community notes found for this post."))
	} else {
		for _, n := range notes.Notes {
			if n.Text != "" {
				list = append(list, renderBirdwatchNote(n, p, translate, translateParam(path), backPath(path)))
			}
		}
	}
	parts = append(parts, exec("birdwatchList", list))
	return exec("birdwatchContainer", map[string]any{"Parts": parts})
}
