// SPDX-License-Identifier: AGPL-3.0-only

// Package query builds Twitter search queries from nitter's search form
// parameters, and nitter search URLs from queries.
package query

import (
	"net/url"
	"slices"
	"strings"

	"github.com/perennialtech/nitter/internal/types"
	"github.com/perennialtech/nitter/internal/utils"
)

var ValidFilters = []string{
	"media", "images", "twimg", "videos",
	"native_video", "consumer_video", "spaces",
	"links", "news", "quote", "mentions",
	"replies", "retweets", "nativeretweets",
}

const EmptyQuery = "include:nativeretweets"

// Init parses search parameters. name is the comma-separated list of users
// the search is restricted to, if any.
func Init(params url.Values, name string) types.Query {
	kind := types.QueryKind(params.Get("f"))
	if !slices.Contains(types.QueryKinds, kind) {
		kind = types.QueryTweets
	}
	// articles and reposts are internal tab kinds, not valid search filters
	if kind == types.QueryArticles || kind == types.QueryReposts {
		kind = types.QueryTweets
	}
	q := types.Query{
		Kind:     kind,
		View:     params.Get("view"),
		Text:     params.Get("q"),
		Since:    params.Get("since"),
		Until:    params.Get("until"),
		MinLikes: utils.ValidateNumber(params.Get("min_faves")),
	}
	for _, f := range ValidFilters {
		if params.Has("f-" + f) {
			q.Filters = append(q.Filters, f)
		}
		if params.Has("e-" + f) {
			q.Excludes = append(q.Excludes, f)
		}
	}
	if name != "" {
		q.FromUser = strings.Split(name, ",")
	}
	return q
}

func Media(name string) types.Query {
	return types.Query{
		Kind:     types.QueryMedia,
		Filters:  []string{"twimg", "native_video"},
		FromUser: []string{name},
		Sep:      "OR",
	}
}

func Articles(name string) types.Query {
	return types.Query{Kind: types.QueryArticles, FromUser: []string{name}}
}

func Replies(name string) types.Query {
	return types.Query{Kind: types.QueryReplies, FromUser: []string{name}}
}

func Reposts(name string) types.Query {
	return types.Query{Kind: types.QueryReposts, FromUser: []string{name}}
}

// Param renders q as a Twitter search string. maxID, if set, pages
// backwards from that tweet ID instead of using q.Until.
func Param(q types.Query, maxID string) string {
	if q.Kind == types.QueryUsers {
		return q.Text
	}

	var param string
	for i, user := range q.FromUser {
		if i == 0 {
			param = "("
		}
		param += "from:" + user
		if i < len(q.FromUser)-1 {
			param += " OR "
		} else {
			param += ")"
		}
	}

	if len(q.FromUser) > 0 && (q.Kind == types.QueryPosts || q.Kind == types.QueryMedia) {
		param += " (filter:self_threads OR -filter:replies)"
	}

	if !slices.Contains(q.Excludes, "nativeretweets") {
		param += " include:nativeretweets"
	}

	var filters []string
	for _, f := range q.Filters {
		filters = append(filters, "filter:"+f)
	}
	for _, e := range q.Excludes {
		if e == "nativeretweets" {
			continue
		}
		filters = append(filters, "-filter:"+e)
	}
	for _, i := range q.Includes {
		filters = append(filters, "include:"+i)
	}

	var result string
	if len(filters) > 0 {
		result = strings.TrimSpace(param + " (" + strings.Join(filters, " "+q.Sep+" ") + ")")
	} else {
		result = strings.TrimSpace(param)
	}

	if q.Since != "" {
		result += " since:" + q.Since
	}
	if q.Until != "" && maxID == "" {
		result += " until:" + q.Until
	}
	if q.MinLikes != "" {
		result += " min_faves:" + q.MinLikes
	}
	if q.Text != "" {
		if result != "" {
			result += " " + q.Text
		} else {
			result = q.Text
		}
	}
	if result != "" && maxID != "" {
		result += " max_id:" + maxID
	}
	return result
}

// URL renders q as the query string of a nitter search URL, without "?".
func URL(q types.Query) string {
	var params []string
	if q.View != "" {
		params = append(params, "view="+utils.EncodeURL(q.View))
	}

	// media doubles as the profile media tab, where f isn't part of the URL scheme
	switch {
	case q.Kind == types.QueryTweets, q.Kind == types.QueryUsers, q.Kind == types.QueryLists,
		q.Kind == types.QueryTop, q.Kind == types.QueryMedia && len(q.FromUser) == 0:
		params = append(params, "f="+string(q.Kind))
		if q.Text != "" {
			params = append(params, "q="+utils.EncodeURL(q.Text))
		}
		for _, f := range q.Filters {
			params = append(params, "f-"+f+"=on")
		}
		for _, e := range q.Excludes {
			params = append(params, "e-"+e+"=on")
		}
		for _, i := range q.Includes {
			if i != "nativeretweets" {
				params = append(params, "i-"+i+"=on")
			}
		}
		if q.Since != "" {
			params = append(params, "since="+q.Since)
		}
		if q.Until != "" {
			params = append(params, "until="+q.Until)
		}
		if q.MinLikes != "" {
			params = append(params, "min_faves="+q.MinLikes)
		}
	}
	return strings.Join(params, "&")
}
