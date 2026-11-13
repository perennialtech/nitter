// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// listen starts a raw TCP server; serve handles each connection.
func listen(t *testing.T, serve func(net.Conn)) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				serve(c)
			}()
		}
	}()
	return l.Addr().String()
}

// readRequest reads a request line and headers.
func readRequest(r *bufio.Reader) (string, http.Header, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", nil, err
	}
	h := http.Header{}
	for {
		l, err := r.ReadString('\n')
		if err != nil {
			return "", nil, err
		}
		l = strings.TrimRight(l, "\r\n")
		if l == "" {
			break
		}
		k, v, _ := strings.Cut(l, ":")
		h.Add(k, strings.TrimSpace(v))
	}
	return strings.TrimRight(line, "\r\n"), h, nil
}

func useClient(t *testing.T, c *http.Client) {
	t.Helper()
	old := httpClient
	httpClient = c
	t.Cleanup(func() { httpClient = old })
}

// The upstream deadline covers the whole response: a server that stalls
// before its headers or in its body must not hold the request, and the
// connection must be closed.
func TestBoundedRequests(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		for _, mode := range []string{"headers", "body", "complete"} {
			t.Run(method+" "+mode, func(t *testing.T) {
				useClient(t, newHTTPClient(nil))
				closed := make(chan struct{})
				addr := listen(t, func(c net.Conn) {
					r := bufio.NewReader(c)
					line, h, err := readRequest(r)
					if err != nil || !strings.HasPrefix(line, method+" /test ") {
						t.Errorf("request line %q, %v", line, err)
						return
					}
					if method == http.MethodPost {
						body := make([]byte, len("payload"))
						io.ReadFull(r, body)
						if string(body) != "payload" || h.Get("Content-Length") != "7" {
							t.Errorf("body %q", body)
						}
					}
					switch mode {
					case "body":
						io.WriteString(c, "HTTP/1.1 200 OK\r\nContent-Length: 10\r\n\r\n")
					case "complete":
						io.WriteString(c, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
					}
					// a timed-out client closes its end
					c.SetReadDeadline(time.Now().Add(2 * time.Second))
					if _, err := r.ReadByte(); errors.Is(err, io.EOF) {
						close(closed)
					}
				})

				payload := ""
				if method == http.MethodPost {
					payload = "payload"
				}
				start := time.Now()
				resp, err := doRequest(context.Background(), method, "http://"+addr+"/test", nil, payload,
					100*time.Millisecond)
				if mode == "complete" {
					if err != nil || resp.status != 200 || resp.body != "ok" {
						t.Fatalf("got %+v, %v", resp, err)
					}
					return
				}
				if !errors.Is(err, errTimeout) {
					t.Fatalf("a stalled %s must time out, got %v", mode, err)
				}
				if elapsed := time.Since(start); elapsed > time.Second {
					t.Fatalf("took %v", elapsed)
				}
				select {
				case <-closed:
				case <-time.After(2 * time.Second):
					t.Fatal("the upstream connection must close")
				}
			})
		}
	}
}

func TestRequestHonoursCallerContext(t *testing.T) {
	useClient(t, newHTTPClient(nil))
	addr := listen(t, func(c net.Conn) { io.Copy(io.Discard, c) })
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	_, err := doRequest(ctx, http.MethodGet, "http://"+addr+"/", nil, "", time.Minute)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want the caller's cancellation", err)
	}
}

// selfSigned makes a certificate for host and a pool that trusts it.
func selfSigned(t *testing.T, host string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: host},
		DNSNames:              []string{host},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool
}

type proxied struct {
	line   string
	header http.Header
}

const proxyPath = "/graphql/test?variables=%7B%22screen_name%22%3A%22mobile_test%22%7D&features=%7B%7D"

// proxyServer is an HTTP proxy that answers every request itself with "OK",
// and terminates CONNECT tunnels with cert, recording what it sees.
func proxyServer(t *testing.T, cert tls.Certificate, rejectConnect bool) (string, chan proxied) {
	seen := make(chan proxied, 16)
	addr := listen(t, func(c net.Conn) {
		var conn net.Conn = c
		r := bufio.NewReader(c)
		for {
			line, h, err := readRequest(r)
			if err != nil {
				return
			}
			seen <- proxied{line, h}
			if strings.HasPrefix(line, "CONNECT ") {
				if rejectConnect {
					io.WriteString(conn, "HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n")
					// the client must close the rejected tunnel
					r.ReadByte()
					seen <- proxied{line: "closed"}
					return
				}
				io.WriteString(conn, "HTTP/1.1 200 Connection established\r\n\r\n")
				tc := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{"http/1.1"}})
				conn, r = tc, bufio.NewReader(tc)
				continue
			}
			if n, _ := strconv.Atoi(h.Get("Content-Length")); n > 0 {
				io.CopyN(io.Discard, r, int64(n))
			}
			io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nOK")
		}
	})
	return addr, seen
}

// proxyClient makes the API client use proxyURL, as http.ProxyFromEnvironment
// would with HTTP_PROXY and HTTPS_PROXY set to it.
func proxyClient(t *testing.T, proxyURL string, roots *x509.CertPool) {
	t.Helper()
	u, err := url.Parse(proxyURL)
	if err != nil {
		t.Fatal(err)
	}
	c := newHTTPClient(http.ProxyURL(u))
	c.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: roots}
	useClient(t, c)
}

func next(t *testing.T, seen chan proxied) proxied {
	t.Helper()
	select {
	case p := <-seen:
		return p
	case <-time.After(5 * time.Second):
		t.Fatal("the proxy saw no request")
		return proxied{}
	}
}

func requestTwice(t *testing.T, method, url string) {
	t.Helper()
	for i := range 2 {
		h := http.Header{}
		h.Set("X-Nitter-Proxy-Test", string(rune('0'+i)))
		resp, err := doRequest(context.Background(), method, url, h, "", 5*time.Second)
		if err != nil || resp.status != 200 || resp.body != "OK" {
			t.Fatalf("request %d: %+v, %v", i, resp, err)
		}
	}
}

func checkProxied(t *testing.T, seen chan proxied, method, target string) []proxied {
	t.Helper()
	var got []proxied
	for i := range 2 {
		p := next(t, seen)
		if p.line != method+" "+target+" HTTP/1.1" {
			t.Fatalf("request line %q", p.line)
		}
		if p.header.Get("Host") != "origin.test" || p.header.Get("X-Nitter-Proxy-Test") != string(rune('0'+i)) {
			t.Fatalf("headers %v", p.header)
		}
		got = append(got, p)
	}
	return got
}

func TestHTTPProxyUsesAbsoluteForm(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		addr, seen := proxyServer(t, tls.Certificate{}, false)
		proxyClient(t, "http://"+addr, nil)
		requestTwice(t, method, "http://origin.test"+proxyPath)
		checkProxied(t, seen, method, "http://origin.test"+proxyPath)
	}
}

func TestHTTPSProxyUsesConnect(t *testing.T) {
	cert, roots := selfSigned(t, "origin.test")
	addr, seen := proxyServer(t, cert, false)
	proxyClient(t, "http://user:password@"+addr, roots)
	requestTwice(t, http.MethodGet, "https://origin.test"+proxyPath)

	connect := next(t, seen)
	if connect.line != "CONNECT origin.test:443 HTTP/1.1" || connect.header.Get("Host") != "origin.test:443" {
		t.Fatalf("CONNECT %q %v", connect.line, connect.header)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:password"))
	if connect.header.Get("Proxy-Authorization") != want {
		t.Fatalf("CONNECT must authenticate, got %v", connect.header)
	}
	// both requests share the pooled tunnel, in origin form
	for _, p := range checkProxied(t, seen, http.MethodGet, proxyPath) {
		if p.header.Get("Proxy-Authorization") != "" {
			t.Fatal("tunnelled requests must not carry proxy credentials")
		}
	}
}

func TestHTTPProxyAuth(t *testing.T) {
	for _, tc := range []struct{ urlAuth, want string }{
		{"user:password@", "user:password"},
		{"user:pass%3Aword@", "user:pass:word"},
	} {
		addr, seen := proxyServer(t, tls.Certificate{}, false)
		proxyClient(t, "http://"+tc.urlAuth+addr, nil)
		requestTwice(t, http.MethodGet, "http://origin.test"+proxyPath)
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte(tc.want))
		for _, p := range checkProxied(t, seen, http.MethodGet, "http://origin.test"+proxyPath) {
			if got := p.header.Get("Proxy-Authorization"); got != want {
				t.Fatalf("%+v: Proxy-Authorization %q, want %q", tc, got, want)
			}
		}
	}
}

func TestClientsUseEnvironmentProxy(t *testing.T) {
	// Functions can't be compared, so check the proxy each picks for a
	// request; ProxyFromEnvironment reads the variables once per process,
	// so this compares against what it returns rather than setting them.
	req, _ := http.NewRequest(http.MethodGet, "https://api.x.com/", nil)
	want, wantErr := http.ProxyFromEnvironment(req)
	got, err := httpClient.Transport.(*http.Transport).Proxy(req)
	if fmt.Sprint(got) != fmt.Sprint(want) || (err == nil) != (wantErr == nil) {
		t.Fatalf("API client proxy %v, %v; ProxyFromEnvironment %v, %v", got, err, want, wantErr)
	}
}

func TestRejectedConnectClosesProxyConnection(t *testing.T) {
	addr, seen := proxyServer(t, tls.Certificate{}, true)
	proxyClient(t, "http://"+addr, nil)
	if _, err := doRequest(context.Background(), http.MethodGet, "https://origin.test/path", nil, "",
		5*time.Second); err == nil {
		t.Fatal("a rejected CONNECT must fail the request")
	}
	if p := next(t, seen); p.line != "CONNECT origin.test:443 HTTP/1.1" {
		t.Fatalf("request line %q", p.line)
	}
	if p := next(t, seen); p.line != "closed" {
		t.Fatalf("the client must close the rejected tunnel, got %q", p.line)
	}
}
