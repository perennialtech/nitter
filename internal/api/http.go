// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// httpClient sends every upstream request, so they share its connection
// pool. Init replaces it to apply the configured proxy.
var httpClient = newHTTPClient(nil)

// errTimeout is returned when an upstream response takes longer than its
// deadline.
var errTimeout = errors.New("upstream response timed out")

func newHTTPClient(proxy *url.URL) *http.Client {
	// The private API is known to work over HTTP/1.1, which Nim used; an
	// HTTP/2 client looks different to X's bot detection. This transport is
	// built rather than cloned from http.DefaultTransport, whose TLS config
	// keeps offering h2 over ALPN even when HTTP/2 is turned off.
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	t := &http.Transport{
		DialContext:  (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		Protocols:    protocols,
		MaxIdleConns: 100,
		// Nearly every request goes to the same few hosts, concurrently; the
		// default of 2 idle connections per host would redial most of them.
		MaxIdleConnsPerHost:   64,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	if proxy != nil {
		t.Proxy = http.ProxyURL(proxy)
	}
	return &http.Client{Transport: t}
}

// parseProxy builds the proxy URL from the proxy and proxyAuth settings.
// proxyAuth, "username:password", replaces any credentials in the URL.
func parseProxy(rawURL, auth string) (*url.URL, error) {
	if rawURL == "" {
		return nil, nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("proxy: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("proxy must be a URL such as http://host:port, got %q", rawURL)
	}
	if auth != "" {
		user, pass, ok := strings.Cut(auth, ":")
		if !ok {
			return nil, errors.New("proxyAuth must contain username:password")
		}
		u.User = url.UserPassword(user, pass)
	}
	return u, nil
}

type response struct {
	status int
	header http.Header
	body   string
}

// doRequest sends one request and reads its whole body. timeout bounds the
// request from dialing to the end of the body. A gzip body the caller asked
// for with its own Accept-Encoding is decompressed.
func doRequest(ctx context.Context, method, target string, header http.Header,
	body string, timeout time.Duration) (*response, error) {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(reqCtx, method, target, reader)
	if err != nil {
		return nil, err
	}
	if header != nil {
		req.Header = header.Clone()
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, requestError(ctx, reqCtx, err)
	}
	defer resp.Body.Close()

	var b strings.Builder
	if _, err := io.Copy(&b, resp.Body); err != nil {
		return nil, requestError(ctx, reqCtx, err)
	}
	data := b.String()
	if data != "" && resp.Header.Get("Content-Encoding") == "gzip" {
		if data, err = gunzip(data); err != nil {
			return nil, err
		}
	}
	return &response{status: resp.StatusCode, header: resp.Header, body: data}, nil
}

func gunzip(data string) (string, error) {
	gz, err := gzip.NewReader(strings.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("gzip body: %w", err)
	}
	var b strings.Builder
	if _, err := io.Copy(&b, gz); err != nil {
		return "", fmt.Errorf("gzip body: %w", err)
	}
	return b.String(), nil
}

func requestError(ctx, reqCtx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(reqCtx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%w: %w", errTimeout, err)
	}
	return err
}
