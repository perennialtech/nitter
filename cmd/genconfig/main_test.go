// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/perennialtech/nitter/internal/config"
	"github.com/perennialtech/nitter/internal/prefs"
)

// TestGeneratedFilesAreCurrent fails when a committed example file differs
// from what genconfig writes, such as after a default or description
// changed without make generate.
func TestGeneratedFilesAreCurrent(t *testing.T) {
	for _, f := range files() {
		got, err := os.ReadFile(filepath.Join("..", "..", f.name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, f.data) {
			t.Errorf("%s is out of date; run make generate", f.name)
		}
	}
}

// TestExampleConfLoadsDefaults checks that every value the example shows
// is read back as the default it documents.
func TestExampleConfLoadsDefaults(t *testing.T) {
	got, err := config.Load(filepath.Join("..", "..", "nitter.example.conf"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := config.FromFile(config.File{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Preferences) != len(prefs.All()) {
		t.Errorf("[Preferences] has %d keys, want %d", len(got.Preferences), len(prefs.All()))
	}
	builtin := prefs.Defaults
	t.Cleanup(func() { prefs.Defaults = builtin })
	if err := prefs.Init(got); err != nil {
		t.Fatal(err)
	}
	if prefs.Defaults != builtin {
		t.Errorf("[Preferences] loads as %+v, want %+v", prefs.Defaults, builtin)
	}

	got.Preferences, want.Preferences = nil, nil
	if !reflect.DeepEqual(got, want) {
		t.Errorf("nitter.example.conf loads as\n%+v\nwant the defaults\n%+v", *got, *want)
	}
}
