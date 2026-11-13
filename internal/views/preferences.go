// SPDX-License-Identifier: AGPL-3.0-only

package views

import (
	"html/template"

	"github.com/perennialtech/nitter/internal/flags"
	"github.com/perennialtech/nitter/internal/prefs"
	"github.com/perennialtech/nitter/internal/types"
)

func renderPrefs(p *prefs.Prefs, themes []string) template.HTML {
	var out template.HTML
	for _, c := range prefs.Categories {
		out += exec("legend", c.Name)
		for _, pref := range c.Prefs {
			if pref.Name == "autoTranslate" && flags.GuestAuth ||
				pref.Name == "guestSearchFallback" && !flags.GuestAuth {
				continue
			}
			switch pref.Kind {
			case prefs.Checkbox:
				out += genCheckbox(pref.Name, pref.Label, p.Bool(pref.Name))
			case prefs.Input:
				out += genInput(pref.Name, pref.Label, p.String(pref.Name), pref.Placeholder, "", true)
			case prefs.Select:
				options := pref.Options
				if pref.Name == "theme" {
					options = themes
				}
				out += genSelect(pref.Name, pref.Label, p.String(pref.Name), options)
			}
		}
	}
	return out
}

// RenderPreferences renders the settings page. themes are the theme
// names derived from public/css/themes (see the preferences route).
func RenderPreferences(p *prefs.Prefs, path string, themes []string, prefsURL string) template.HTML {
	return exec("preferences", map[string]any{
		"Path":     path,
		"Prefs":    renderPrefs(p, themes),
		"PrefsURL": prefsURL,
		"Reset":    buttonReferer("/resetprefs", "Reset preferences", path, "pref-reset", "post"),
	})
}

func renderTrendsList(trends []types.Trend) template.HTML {
	return exec("trendsList", trends)
}

// RenderTrendsTabs renders the two trend location tabs.
func RenderTrendsTabs(tab1Name string, tab1 []types.Trend, tab2Name string, tab2 []types.Trend) template.HTML {
	return exec("trendsTabs", map[string]any{
		"Tab1Name": tab1Name, "Tab1": renderTrendsList(tab1),
		"Tab2Name": tab2Name, "Tab2": renderTrendsList(tab2),
	})
}

// RenderHomeBody renders the home page with search and trends.
func RenderHomeBody(tab1Name string, tab1 []types.Trend, tab2Name string, tab2 []types.Trend,
	search template.HTML) template.HTML {
	return exec("homeBody", map[string]any{
		"Search": search, "Trends": RenderTrendsTabs(tab1Name, tab1, tab2Name, tab2),
	})
}
