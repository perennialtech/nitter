// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/perennialtech/nitter/internal/auth"
	"github.com/perennialtech/nitter/internal/config"
)

func TestMain(m *testing.M) {
	if os.Getenv("NITTER_TEST_LOG") == "" {
		log.SetOutput(io.Discard)
	}
	// tests that need transaction IDs turn them back on
	disableTid = true
	os.Exit(m.Run())
}

// fakeUpstream routes every upstream request, whatever its host, to
// handler over TLS.
func fakeUpstream(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	addr := srv.Listener.Addr().String()
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", addr)
		},
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	t.Cleanup(tr.CloseIdleConnections)
	useClient(t, &http.Client{Transport: tr})
}

var sessionFiles atomic.Int32

// addCookieSessions adds n cookie sessions to the account pool, which tests
// share.
func addCookieSessions(t *testing.T, n int) {
	t.Helper()
	var lines []string
	for i := range n {
		id := int(sessionFiles.Add(1))*100 + i
		lines = append(lines, fmt.Sprintf(`{"kind":"cookie","id":"%d","auth_token":"auth%d","ct0":"csrf%d"}`, id, id, id))
	}
	path := filepath.Join(t.TempDir(), "sessions.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := auth.Init(&config.Config{}, path); err != nil {
		t.Fatal(err)
	}
}

// saveSettings restores the Init settings after a test that changes them.
func saveSettings(t *testing.T) {
	client, tid, proxy, retries, delay, timeout, guest, trends :=
		httpClient, disableTid, apiProxy, maxRetries, retryDelay, apiTimeout, guestAuthorization, trendsCacheTTL
	t.Cleanup(func() {
		httpClient, disableTid, apiProxy, maxRetries, retryDelay, apiTimeout, guestAuthorization, trendsCacheTTL =
			client, tid, proxy, retries, delay, timeout, guest, trends
	})
}
