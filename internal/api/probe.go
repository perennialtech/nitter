// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"strconv"

	"github.com/perennialtech/nitter/internal/auth"
)

// ProbeResult is the outcome of ProbeSession.
type ProbeResult struct {
	Status    int
	Remaining int
	Limit     int
	// Body holds a short error response, if there was one.
	Body string
}

// ProbeSession requests jack's media timeline with a session, outside the
// pool and without retries, to check whether the session still works.
func ProbeSession(ctx context.Context, s *auth.Session) (ProbeResult, error) {
	req := apiReq{
		oauth:  apiURL{endpoint: graphUserMedia, params: []param{{"variables", format(userMediaVars, "12", "", "20")}, {"features", gqlFeatures}}},
		cookie: apiURL{endpoint: graphUserMedia, params: []param{{"variables", format(userMediaVars, "12", "", "20")}, {"features", gqlFeatures}}},
	}
	t := req.target(s.Kind)
	headers, err := genHeaders(ctx, s, t, false)
	if err != nil {
		return ProbeResult{}, err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodGet, t.url, nil)
	if err != nil {
		return ProbeResult{}, err
	}
	hreq.Header = headers
	res, err := httpClient.Do(hreq)
	if err != nil {
		return ProbeResult{}, err
	}
	defer res.Body.Close()

	r := ProbeResult{Status: res.StatusCode}
	r.Remaining, _ = strconv.Atoi(res.Header.Get("x-rate-limit-remaining"))
	r.Limit, _ = strconv.Atoi(res.Header.Get("x-rate-limit-limit"))
	if res.StatusCode != http.StatusOK {
		var body io.Reader = res.Body
		if res.Header.Get("Content-Encoding") == "gzip" {
			if zr, err := gzip.NewReader(res.Body); err == nil {
				body = zr
			}
		}
		b, _ := io.ReadAll(io.LimitReader(body, 121))
		if len(b) <= 120 {
			r.Body = string(b)
		}
	}
	return r, nil
}
