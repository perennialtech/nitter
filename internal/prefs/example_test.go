// SPDX-License-Identifier: AGPL-3.0-only

package prefs

import (
	"slices"
	"testing"
)

func TestPreferencesAreDocumented(t *testing.T) {
	entries := ExampleSection().Entries
	if len(entries) != len(All()) {
		t.Fatalf("%d entries for %d preferences", len(entries), len(All()))
	}
	for i, p := range All() {
		if p.Label == "" || entries[i].Key != p.Name {
			t.Errorf("%s: undocumented", p.Name)
		}
		if p.Kind == Select && len(p.Options) > 0 && !slices.Contains(p.Options, p.DefaultOption) {
			t.Errorf("%s: default %q isn't one of its options", p.Name, p.DefaultOption)
		}
	}
}
