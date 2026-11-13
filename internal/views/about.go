// SPDX-License-Identifier: AGPL-3.0-only

package views

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/yuin/goldmark"
	gmhtml "github.com/yuin/goldmark/renderer/html"
)

const commitURL = "https://github.com/perennialtech/nitter/commit/"

var (
	aboutHTML     template.HTML
	lpSVG         string
	embedResizeJS template.JS
	version       string
	versionLink   string
)

func init() {
	hash, date := "unknown", ""
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				hash = s.Value
				if len(hash) > 7 {
					hash = hash[:7]
				}
			case "vcs.time":
				if t, err := time.Parse(time.RFC3339, s.Value); err == nil {
					date = t.UTC().Format("2006.01.02")
				}
			}
		}
	}
	versionLink = commitURL + hash
	version = hash
	if date != "" {
		version = date + "-" + hash
	}
}

// Init loads the static files the views embed: the rendered about page,
// the Liberapay logo and the embed resize script. Call once at startup
// with the configured staticDir.
func Init(staticDir string) error {
	lp, err := os.ReadFile(filepath.Join(staticDir, "lp.svg"))
	if err != nil {
		return err
	}
	js, err := os.ReadFile(filepath.Join(staticDir, "js", "embedResize.js"))
	if err != nil {
		return err
	}
	lpSVG = string(lp)
	embedResizeJS = template.JS(js)

	mdPath := filepath.Join(staticDir, "md", "about.md")
	md, err := os.ReadFile(mdPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s not found\n", mdPath)
		aboutHTML = "<h1>About page is missing</h1><br><br>"
		return nil
	}
	var b bytes.Buffer
	gm := goldmark.New(goldmark.WithRendererOptions(gmhtml.WithUnsafe()))
	if err := gm.Convert(md, &b); err != nil {
		return fmt.Errorf("%s: %w", mdPath, err)
	}
	aboutHTML = template.HTML(b.String())
	return nil
}

// RenderAbout renders the about page with the instance's version.
func RenderAbout() template.HTML {
	return exec("about", map[string]any{
		"HTML": aboutHTML, "Version": version, "Link": versionLink,
	})
}

// RenderFeature renders the unsupported feature page.
func RenderFeature() template.HTML {
	return exec("feature", nil)
}
