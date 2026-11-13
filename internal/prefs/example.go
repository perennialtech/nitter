// SPDX-License-Identifier: AGPL-3.0-only

package prefs

import (
	"strconv"
	"strings"

	"github.com/perennialtech/nitter/internal/config"
)

// ExampleSection documents the [Preferences] section of nitter.conf.
func ExampleSection() config.Section {
	s := config.Section{Name: "Preferences",
		Doc: "Instance-wide defaults for the preferences on /settings. Visitors' own " +
			"settings, kept in cookies, override them."}
	for _, p := range All() {
		e := config.Entry{Key: p.Name, Doc: strings.TrimSuffix(p.Label, ".") + "."}
		if p.Doc != "" {
			e.Doc += " " + p.Doc
		}
		switch p.Kind {
		case Checkbox:
			e.Value = strconv.FormatBool(p.DefaultState)
		case Select:
			e.Value = config.Quote(p.DefaultOption)
			if len(p.Options) > 0 {
				e.Doc += " One of " + strings.Join(p.Options, ", ") + "."
			}
		case Input:
			e.Value = config.Quote(p.DefaultInput)
		}
		s.Entries = append(s.Entries, e)
	}
	return s
}
