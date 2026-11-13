// SPDX-License-Identifier: AGPL-3.0-only

package api

// x-client-transaction-id generation, from key pairs published at pairsURL.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/gjson"
)

const (
	defaultKeyword = "obfiowerehiring"
	pairsURL       = "https://raw.githubusercontent.com/fa0311/x-client-transaction-id-pair-dict/refs/heads/main/pair.json"
	pairsTTL       = time.Hour
	tidEpoch       = 1682924400
)

type tidPair struct {
	animationKey string
	verification string
}

var tidPairs struct {
	sync.Mutex
	pairs   []tidPair
	fetched time.Time
}

func parseTidPairs(body string) ([]tidPair, error) {
	var pairs []tidPair
	if gjson.Valid(body) {
		for _, p := range items(gjson.Parse(body)) {
			key := p.Get("animationKey")
			if !key.Exists() {
				key = p.Get("animation_key")
			}
			// cached for an hour; don't pin the response body
			pairs = append(pairs, tidPair{
				animationKey: strings.Clone(key.String()),
				verification: strings.Clone(p.Get("verification").String()),
			})
		}
	}
	if len(pairs) == 0 {
		return nil, fmt.Errorf("parsing pairs failed: %s", body)
	}
	return pairs, nil
}

func getPair(ctx context.Context) (tidPair, error) {
	tidPairs.Lock()
	defer tidPairs.Unlock()
	if len(tidPairs.pairs) == 0 || time.Since(tidPairs.fetched) > pairsTTL {
		resp, err := doRequest(ctx, http.MethodGet, pairsURL, nil, "", apiTimeout)
		if err != nil {
			return tidPair{}, err
		}
		if resp.status == http.StatusOK {
			pairs, err := parseTidPairs(resp.body)
			if err != nil {
				return tidPair{}, err
			}
			tidPairs.pairs = pairs
			tidPairs.fetched = time.Now()
		}
	}
	if len(tidPairs.pairs) == 0 {
		return tidPair{}, errors.New("failed to fetch x-client-transaction-id pairs")
	}
	return tidPairs.pairs[rand.IntN(len(tidPairs.pairs))], nil
}

func decodeBase64(s string) ([]byte, error) {
	s = strings.TrimRight(s, "=")
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawURLEncoding.DecodeString(s)
}

func genTid(ctx context.Context, path string) (string, error) {
	pair, err := getPair(ctx)
	if err != nil {
		return "", err
	}
	return tidFromPair(pair, path, time.Now().Unix()-tidEpoch, byte(rand.IntN(256)))
}

func tidFromPair(pair tidPair, path string, timeNow int64, randomNum byte) (string, error) {
	key, err := decodeBase64(pair.verification)
	if err != nil {
		return "", fmt.Errorf("tid verification key: %w", err)
	}
	data := "GET!" + path + "!" + strconv.FormatInt(timeNow, 10) + defaultKeyword + pair.animationKey
	hash := sha256.Sum256([]byte(data))

	bytes := make([]byte, 0, len(key)+4+16+1)
	bytes = append(bytes, key...)
	bytes = append(bytes, byte(timeNow), byte(timeNow>>8), byte(timeNow>>16), byte(timeNow>>24))
	bytes = append(bytes, hash[:16]...)
	bytes = append(bytes, 3)

	tid := make([]byte, 0, len(bytes)+1)
	tid = append(tid, randomNum)
	for _, b := range bytes {
		tid = append(tid, b^randomNum)
	}
	return base64.RawStdEncoding.EncodeToString(tid), nil
}
