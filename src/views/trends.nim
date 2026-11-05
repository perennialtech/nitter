# SPDX-License-Identifier: AGPL-3.0-only
import strformat, uri
import karax/[karaxdsl, vdom]

import ../trends

proc renderTrendsList(trends: seq[Trend]): VNode =
  buildHtml(ul(class="trends-list")):
    if trends.len == 0:
      li(class="trends-empty"): text "No trends available right now."
    else:
      for t in trends:
        li(class="trend-item"):
          a(href=("/search?q=" & encodeUrl(t.searchQuery))):
            span(class="trend-name"): text t.name
            if t.tweetVolume >= 0:
              span(class="trend-volume"): text &"{t.tweetVolume} tweets"

proc renderTrendsTabs*(tab1Name: string, tab1: seq[Trend],
                       tab2Name: string, tab2: seq[Trend]): VNode =
  buildHtml(tdiv(class="trends-panel")):
    input(`type`="radio", name="trends-tab", id="trends-tab-1",
          class="trends-tab-radio", checked="checked")
    input(`type`="radio", name="trends-tab", id="trends-tab-2",
          class="trends-tab-radio")

    tdiv(class="trends-tab-labels"):
      label(`for`="trends-tab-1", class="trends-tab-label"): text tab1Name
      label(`for`="trends-tab-2", class="trends-tab-label"): text tab2Name

    tdiv(class="trends-tab-panel", id="trends-panel-1"):
      renderTrendsList(tab1)
    tdiv(class="trends-tab-panel", id="trends-panel-2"):
      renderTrendsList(tab2)

proc renderHomeBody*(tab1Name: string, tab1: seq[Trend],
                     tab2Name: string, tab2: seq[Trend]; search: VNode): VNode =
  buildHtml(tdiv(class="home-panel")):
    search
    renderTrendsTabs(tab1Name, tab1, tab2Name, tab2)
