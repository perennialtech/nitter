# SPDX-License-Identifier: AGPL-3.0-only
import times, sequtils, strutils, uri
import karax/[karaxdsl, vdom]

import renderutils, tweet, timeline
import ".."/[types, formatters, apiutils]

proc translateParam(path: string): string =
  let noFragment = path.split('#')[0]
  noFragment & (if '?' in noFragment: "&" else: "?") & "tr=1"

proc renderBirdwatchNote(note: BirdwatchNote; prefs: Prefs; translate = false;
                         transUrl = ""; backPath = ""): VNode =
  let
    transOn = enableTranslation() and note.translatable
    transActive = transOn and note.translation.len > 0 and
                  (translate or prefs.autoTranslate)
    noteText = if transActive: note.translation else: linkifyBirdwatchUrls(note.text)
    onDemand = note.id.len > 0 and
               noteCanTranslateOnDemand(note.isTranslatable, note.translatable,
                                        translate, note.translation)

  var label = if note.helpful: "Community Note" else: "Proposed Note"
  label &= (if note.misleading: " - Misleading" else: " - Not Misleading")
  var cls = "community-note"
  if not note.helpful: cls &= " proposed"
  cls &= (if note.misleading: " misleading" else: " not-misleading")
  buildHtml(tdiv(class=cls)):
    tdiv(class="community-note-header"):
      icon "group"
      span: text label
      if note.id.len > 0:
        a(class="community-note-permalink", href="/i/birdwatch/n/" & note.id, title="Permalink to this note"):
          icon "link"
    if onDemand:
      renderTranslateOnDemandLink(
        "/i/birdwatch/n/" & note.id & "/translate" &
          (if backPath.len > 0: "?r=" & backPath else: ""),
        "Ask Grok to translate this note")
    elif transOn:
      renderTranslateLabel(transActive, translate, note.transLang, transUrl, note.transTarget)
    tdiv(class="community-note-text", dir="auto"):
      verbatim noteText
    tdiv(class="community-note-meta-sep")
    tdiv(class="community-note-meta"):
      if note.authorAlias.len > 0:
        span(class="community-note-meta-item community-note-author"):
          text "By "
          a(href="/i/communitynotes/u/" & note.authorAlias):
            text note.authorAlias
      if note.createdAt.year > 0:
        span(class="community-note-meta-item community-note-date"):
          text note.createdAt.format("MMM d', 'yyyy' · 'h:mm tt' UTC'")
      # if note.classification.len > 0:
      #   span(class="community-note-meta-item"):
      #     text note.classification
      # if note.misleadingTags.len > 0:
      #   span(class="community-note-meta-item"):
      #     text "Misleading: " & note.misleadingTags.join(", ")
      # if note.helpfulTags.len > 0:
      #   span(class="community-note-meta-item"):
      #     text "Helpful: " & note.helpfulTags.join(", ")
      # if note.decidedBy.len > 0:
      #   span(class="community-note-meta-item"):
      #     text "Decided by " & note.decidedBy

proc renderBirdwatchHistoryNote(note: BirdwatchHistoryNote): VNode =
  var cls = "community-note"
  if not note.helpful: cls &= " proposed"
  buildHtml(tdiv(class=cls)):
    tdiv(class="community-note-header"):
      icon "group"
      span: text (if note.helpful: "Community Note" else: "Proposed Note")
      if note.id.len > 0:
        a(class="community-note-permalink", href="/i/birdwatch/n/" & note.id, title="Permalink to this note"):
          icon "link"
    tdiv(class="community-note-text", dir="auto"):
      verbatim linkifyBirdwatchUrls(note.text)
    tdiv(class="community-note-meta-sep")
    tdiv(class="community-note-meta"):
      if note.tweetUsername.len > 0 and note.tweetId != 0:
        span(class="community-note-meta-item community-note-author"):
          text "On "
          a(href="/" & note.tweetUsername & "/status/" & $note.tweetId):
            text "@" & note.tweetUsername & "'s post"
      if note.createdAt.year > 0:
        span(class="community-note-meta-item community-note-date"):
          text note.createdAt.format("MMM d', 'yyyy' · 'h:mm tt' UTC'")

proc renderBirdwatchHistoryContent*(history: BirdwatchHistory): VNode =
  buildHtml(tdiv(class="birdwatch-notes-list timeline")):
    if history.notes.allIt(it.text.len == 0):
      tdiv(class="timeline-none"):
        text "No notes found for this contributor."
    else:
      for note in history.notes:
        if note.text.len > 0:
          renderBirdwatchHistoryNote(note)

    if history.bottom.len > 0:
      tdiv(class="show-more"):
        a(href="?cursor=" & encodeUrl(history.bottom, usePlus=false)):
          text "Load more"

proc renderBirdwatchHistory*(history: BirdwatchHistory): VNode =
  buildHtml(tdiv(class="timeline-container birdwatch-notes")):
    tdiv(class="timeline-header"):
      text "Notes by " & history.alias
    renderBirdwatchHistoryContent(history)

proc renderBirdwatchSingleNote*(tweet: Tweet; note: BirdwatchNote; prefs: Prefs;
                                path: string; translate = false): VNode =
  buildHtml(tdiv(class="timeline-container birdwatch-notes")):
    if tweet != nil and tweet.id != 0:
      renderTweet(tweet, prefs, path, mainTweet=true)

    tdiv(class="birdwatch-notes-list"):
      tdiv(class="timeline-header"):
        text "Community Note"

      if note.text.len > 0:
        renderBirdwatchNote(note, prefs, translate, translateParam(path),
                            path.split('?')[0].split('#')[0])
      else:
        tdiv(class="timeline-none"):
          text "Note not found."

proc renderCommunityNotesTabs(activeTab: string): VNode =
  buildHtml(ul(class="tab")):
    li(class=(if activeTab == "helpful": "tab-item active" else: "tab-item")):
      a(href="/i/communitynotes"): text "Rated Helpful"
    li(class=(if activeTab == "new": "tab-item active" else: "tab-item")):
      a(href="/i/communitynotes/new"): text "New"

proc renderCommunityNotesTimeline*(tl: Timeline; prefs: Prefs;
                                   path, activeTab: string): VNode =
  buildHtml(tdiv(class="timeline-container birdwatch-notes")):
    tdiv(class="timeline-header"):
      text "Community Notes"
    renderCommunityNotesTabs(activeTab)
    renderTimelineTweets(tl, prefs, path)

proc renderBirdwatchNotes*(tweet: Tweet; notes: BirdwatchNotes; prefs: Prefs;
                           path: string; translate = false): VNode =
  buildHtml(tdiv(class="timeline-container birdwatch-notes")):
    if tweet != nil and tweet.id != 0:
      renderTweet(tweet, prefs, path, mainTweet=true)

    tdiv(class="birdwatch-notes-list"):
      tdiv(class="timeline-header"):
        text "Community Notes"

      if notes.notes.allIt(it.text.len == 0):
        tdiv(class="timeline-none"):
          text "No community notes found for this post."
      else:
        for note in notes.notes:
          if note.text.len > 0:
            renderBirdwatchNote(note, prefs, translate, translateParam(path),
                                path.split('?')[0].split('#')[0])
