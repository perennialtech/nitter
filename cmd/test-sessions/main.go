// SPDX-License-Identifier: AGPL-3.0-only

// Command test-sessions checks each cookie session in a sessions.jsonl
// file by requesting a public media timeline with it.
//
//	go run ./cmd/test-sessions [-delay 0.5] [sessions.jsonl]
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/perennialtech/nitter/internal/api"
	"github.com/perennialtech/nitter/internal/auth"
	"github.com/perennialtech/nitter/internal/config"
)

func main() {
	delay := flag.Float64("delay", 0.5, "seconds to wait between sessions")
	flag.Parse()
	path := "sessions.jsonl"
	if flag.NArg() > 0 {
		path = flag.Arg(0)
	}
	if err := run(path, time.Duration(*delay*float64(time.Second))); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(path string, delay time.Duration) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	var sessions []*auth.Session
	skipped := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		s, err := auth.ParseSession(line)
		switch {
		case err != nil:
			fmt.Println("  [!] Parse error:", err)
		case s.Kind == auth.Cookie:
			sessions = append(sessions, s)
		default:
			skipped++
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if skipped > 0 {
		fmt.Printf("  (skipped %d non-cookie sessions)\n", skipped)
	}
	if len(sessions) == 0 {
		fmt.Println("No cookie sessions found in", path)
		return nil
	}

	// Default settings: no API proxy, transaction IDs enabled.
	if err := api.Init(&config.Config{APITimeoutMs: 15000, MaxRetries: 1}, ""); err != nil {
		return err
	}

	fmt.Printf("Testing %d sessions from %s\n\n", len(sessions), path)
	var failures []string
	valid := 0
	ctx := context.Background()
	for i, s := range sessions {
		prefix := fmt.Sprintf("[%d/%d] %s", i+1, len(sessions), s)
		res, err := api.ProbeSession(ctx, s)
		switch {
		case err != nil:
			detail := "error: " + err.Error()
			failures = append(failures, s.String()+"  "+detail)
			fmt.Println(prefix, " ✗", detail)
		case res.Status == 200:
			valid++
			extra := ""
			if res.Limit > 0 {
				extra = "  remaining=" + strconv.Itoa(res.Remaining) + "/" + strconv.Itoa(res.Limit)
			}
			fmt.Println(prefix, " ✓", res.Status, extra)
		default:
			detail := strconv.Itoa(res.Status)
			failures = append(failures, s.String()+"  "+detail)
			fmt.Println(prefix, " ✗", detail)
		}
		if i < len(sessions)-1 {
			time.Sleep(delay)
		}
	}

	fmt.Printf("\nDone: %d valid, %d invalid  [%d total]\n", valid, len(sessions)-valid, len(sessions))
	if len(failures) > 0 {
		fmt.Println("\nFailures:")
		for _, f := range failures {
			fmt.Println(" ", f)
		}
	}
	return nil
}
