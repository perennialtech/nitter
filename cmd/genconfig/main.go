// SPDX-License-Identifier: AGPL-3.0-only

// Command genconfig writes nitter.example.conf and nitter.example.env
// from the definitions in internal/config and internal/prefs. Run it from
// the repository root with make generate.
package main

import (
	"log"
	"os"

	"github.com/perennialtech/nitter/internal/config"
	"github.com/perennialtech/nitter/internal/prefs"
)

type file struct {
	name string
	data []byte
}

func files() []file {
	sections := append(config.ExampleSections(), prefs.ExampleSection())
	return []file{
		{"nitter.example.conf", config.ExampleConf(sections)},
		{"nitter.example.env", config.ExampleEnv()},
	}
}

func main() {
	log.SetFlags(0)
	for _, f := range files() {
		if err := os.WriteFile(f.name, f.data, 0o644); err != nil {
			log.Fatal(err)
		}
	}
}
