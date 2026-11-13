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
// pool. Like every outbound client, it takes its proxy from HTTP_PROXY,
// HTTPS_PROXY and NO_PROXY.
var httpClient = newHTTPClient(http.ProxyFromEnvironment)

// errTimeout is returned when an upstream response takes longer than its
// deadline.
var errTimeout = errors.New("upstream response timed out")

// newHTTPClient builds the API client. proxy picks each request's proxy, as
// http.Transport.Proxy does; nil means none.
func newHTTPClient(proxy func(*http.Request) (*url.URL, error)) *http.Client {
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
		Proxy:                 proxy,
	}
	return &http.Client{Transport: t}
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
