// SPDX-License-Identifier: AGPL-3.0-only

// Package api is the client for Twitter's GraphQL and REST APIs, made with
// the account or guest sessions in internal/auth, and for FxTwitter, which
// is tried first wherever it can answer without a session.
package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/perennialtech/nitter/internal/flags"
	"github.com/perennialtech/nitter/internal/formatters"
	"github.com/perennialtech/nitter/internal/parser"
	"github.com/perennialtech/nitter/internal/prefs"
	"github.com/perennialtech/nitter/internal/query"
	"github.com/perennialtech/nitter/internal/types"
	"github.com/perennialtech/nitter/internal/utils"
)

// genParams builds the parameters of a GraphQL request.
func genParams(variables, fieldToggles, features string) []param {
	ps := []param{{"variables", variables}, {"features", features}}
	if fieldToggles != "" {
		ps = append(ps, param{"fieldToggles", fieldToggles})
	}
	return ps
}

func gqlURL(endpoint, variables, fieldToggles string) apiURL {
	return apiURL{endpoint: endpoint, params: genParams(variables, fieldToggles, gqlFeatures)}
}

func gqlReq(endpoint, variables, fieldToggles string) apiReq {
	u := gqlURL(endpoint, variables, fieldToggles)
	return apiReq{cookie: u, oauth: u}
}

// cursorParam JSON-escapes the user-supplied cursor so it cannot break out
// of the GraphQL variables object (same input-validation class as the #1411
// media SSRF).
func cursorParam(after string) string {
	if after == "" {
		return ""
	}
	return `"cursor":` + quote(after) + ","
}

func mediaURL(id, cursor string) apiReq {
	return gqlReq(graphUserMedia, format(userMediaVars, id, cursor, "100"), "")
}

func userTweetsURL(id, cursor string, rss bool) apiReq {
	// Existing /@user/rss subscribers expect the full tweet timeline, including
	// reposts; the newer UI's originals-only endpoint must not change the feed.
	endpoint := graphUserTweetsOriginals
	if rss || flags.GuestAuth {
		endpoint = graphUserTweets
	}
	return gqlReq(endpoint, format(userTweetsVars, id, cursor, "20"), userTweetsFieldToggles)
}

func userTweetsAndRepliesURL(id, cursor string) apiReq {
	return gqlReq(graphUserTweetsAndReplies, format(userTweetsAndRepliesVars, id, cursor), userTweetsFieldToggles)
}

func userRepliesURL(id, cursor string, rss bool) apiReq {
	if rss {
		return userTweetsAndRepliesURL(id, cursor)
	}
	return gqlReq(graphUserReplies, format(userRepliesVars, id, cursor), userRepliesFieldToggles)
}

func userArticlesURL(id, cursor string) apiReq {
	return gqlReq(graphUserArticles, format(userArticlesVars, id, cursor), userTweetsFieldToggles)
}

func userRepostsURL(id, cursor string) apiReq {
	return gqlReq(graphUserReposts, format(userRepostsVars, id, cursor), userTweetsFieldToggles)
}

func tweetDetailURL(id, cursor string, mode types.RankingMode) apiReq {
	return gqlReq(graphTweetDetail, format(tweetDetailVars, id, cursor, string(mode)), tweetDetailFieldToggles)
}

func userURL(username string) apiReq {
	cookieVars := obj{{"screen_name", username}, {"withGrokTranslatedBio", false}}.String()
	return apiReq{
		cookie: gqlURL(graphUser, cookieVars, tweetDetailFieldToggles),
		oauth:  gqlURL(graphUserV2, obj{{"screen_name", username}}.String(), ""),
	}
}

func restReq(endpoint string) apiReq {
	u := apiURL{endpoint: endpoint}
	return apiReq{cookie: u, oauth: u}
}

// GraphUser fetches a profile by username.
func GraphUser(ctx context.Context, username string) (types.User, error) {
	if username == "" {
		return types.User{}, nil
	}
	u, err := fxUser(ctx, username)
	if !fxFailed(err) {
		return u, err
	}
	js, err := fetchRawJSON(ctx, userURL(username))
	if err != nil {
		return types.User{}, err
	}
	return parser.ParseGraphUser(js), nil
}

func isDigits(s string) bool {
	for _, c := range []byte(s) {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// GraphUserByID fetches a profile by user ID.
func GraphUserByID(ctx context.Context, id string) (types.User, error) {
	if id == "" || !isDigits(id) {
		return types.User{}, nil
	}
	u, err := fxUser(ctx, "id:"+id)
	if !fxFailed(err) {
		return u, err
	}
	js, err := fetchRawJSON(ctx, gqlReq(graphUserById, format(userByRestIdVars, id), ""))
	if err != nil {
		return types.User{}, err
	}
	return parser.ParseGraphUser(js), nil
}

// AboutAccount fetches the "About this account" details of a profile.
func AboutAccount(ctx context.Context, username string) (types.AccountInfo, error) {
	if username == "" {
		return types.AccountInfo{}, nil
	}
	js, err := fetch(ctx, gqlReq(graphAboutAccount, obj{{"screenName", username}}.String(), ""))
	if err != nil {
		return types.AccountInfo{}, err
	}
	return parser.ParseAboutAccount(js), nil
}

// BroadcastInfo fetches a live broadcast.
func BroadcastInfo(ctx context.Context, id string) (types.Broadcast, error) {
	if id == "" {
		return types.Broadcast{}, nil
	}
	js, err := fetch(ctx, gqlReq(graphBroadcast, obj{{"id", id}}.String(), ""))
	if err != nil {
		return types.Broadcast{}, err
	}
	return parser.ParseBroadcastInfo(js), nil
}

// BroadcastStream fetches the playback URL of a broadcast or space's media
// key.
func BroadcastStream(ctx context.Context, mediaKey string) (string, error) {
	if mediaKey == "" {
		return "", nil
	}
	js, err := fetch(ctx, restReq(restLiveStream+mediaKey))
	if err != nil {
		return "", err
	}
	return getStrOr(js.Get("source.noRedirectPlaybackUrl"), getStr(js.Get("source.location"))), nil
}

// AudioSpace fetches a space.
func AudioSpace(ctx context.Context, id string) (types.AudioSpace, error) {
	if id == "" {
		return types.AudioSpace{}, nil
	}
	variables := obj{
		{"id", id},
		{"isMetatagsQuery", false},
		{"withReplays", true},
		{"withListeners", true},
	}.String()
	js, err := fetch(ctx, gqlReq(graphAudioSpace, variables, ""))
	if err != nil {
		return types.AudioSpace{}, err
	}
	return parser.ParseAudioSpace(js), nil
}

// GraphUserTweets fetches a page of a profile tab. rss selects the feeds
// RSS subscribers expect.
func GraphUserTweets(ctx context.Context, id string, kind types.TimelineKind, after string,
	rss bool) (types.Profile, error) {
	if id == "" {
		return types.Profile{}, nil
	}
	// FxTwitter's statuses tab includes reposts, like UserTweets, so it serves the
	// tabs that use UserTweets; the web tab's originals-only timeline and its
	// pinned post have no FxTwitter equivalent.
	fxTab, fxCount := "", 20
	var fxParams []param
	switch kind {
	case types.TimelineTweets:
		if rss || flags.GuestAuth {
			fxTab = "statuses"
		}
	case types.TimelineReplies:
		if rss {
			fxTab = "statuses"
			fxParams = append(fxParams, param{"with_replies", "true"})
		}
	case types.TimelineMedia:
		fxTab, fxCount = "media", 100
	}
	if fxTab != "" {
		tl, err := fxUserTimeline(ctx, id, fxTab, fxCount, after, fxParams...)
		if !fxFailed(err) {
			return types.Profile{Tweets: tl}, err
		}
	}

	cursor := cursorParam(after)
	var req apiReq
	switch kind {
	case types.TimelineReplies:
		req = userRepliesURL(id, cursor, rss)
	case types.TimelineMedia:
		req = mediaURL(id, cursor)
	case types.TimelineArticles:
		req = userArticlesURL(id, cursor)
	case types.TimelineReposts:
		req = userRepostsURL(id, cursor)
	default:
		req = userTweetsURL(id, cursor, rss)
	}
	js, err := fetch(ctx, req)
	if err != nil {
		return types.Profile{}, err
	}
	return parser.ParseGraphTimeline(js, after), nil
}

// GraphCommunity fetches a community.
func GraphCommunity(ctx context.Context, id string) (types.Community, error) {
	if id == "" {
		return types.Community{}, nil
	}
	js, err := fetch(ctx, gqlReq(graphCommunity, obj{{"communityId", id}}.String(), ""))
	if err != nil {
		return types.Community{}, err
	}
	return parser.ParseGraphCommunity(js), nil
}

func communityTimeline(ctx context.Context, req apiReq, after string) (types.Timeline, error) {
	js, err := fetch(ctx, req)
	if err != nil {
		return types.Timeline{}, err
	}
	return parser.ParseGraphCommunityTimeline(js, after), nil
}

// GraphCommunityTweets fetches a page of a community's posts, ranked by
// rankingMode.
func GraphCommunityTweets(ctx context.Context, id, rankingMode, after string) (types.Timeline, error) {
	if id == "" {
		return types.Timeline{}, nil
	}
	vars := format(communityTweetsVars, id, cursorParam(after), rankingMode)
	return communityTimeline(ctx, gqlReq(graphCommunityTweets, vars, ""), after)
}

// GraphCommunityMedia fetches a page of a community's media posts.
func GraphCommunityMedia(ctx context.Context, id, after string) (types.Timeline, error) {
	if id == "" {
		return types.Timeline{}, nil
	}
	vars := format(communityMediaVars, id, cursorParam(after))
	return communityTimeline(ctx, gqlReq(graphCommunityMedia, vars, ""), after)
}

// communitySliceReq is a community member request, which takes no features.
func communitySliceReq(endpoint, variables string) apiReq {
	u := apiURL{endpoint: endpoint, params: []param{{"variables", variables}}}
	return apiReq{cookie: u, oauth: u}
}

func communityMembers(ctx context.Context, endpoint, id, after string) (types.Result[types.User], error) {
	cursor := "null"
	if after != "" {
		cursor = quote(after)
	}
	js, err := fetch(ctx, communitySliceReq(endpoint, format(communityMembersVars, id, cursor)))
	if err != nil {
		return types.Result[types.User]{}, err
	}
	return parser.ParseGraphCommunityMembers(js, after), nil
}

// GraphCommunityMembers fetches a page of a community's members.
func GraphCommunityMembers(ctx context.Context, id, after string) (types.Result[types.User], error) {
	if id == "" {
		return types.Result[types.User]{}, nil
	}
	return communityMembers(ctx, graphCommunityMembers, id, after)
}

// GraphCommunityModerators fetches a community's moderators.
func GraphCommunityModerators(ctx context.Context, id string) (types.Result[types.User], error) {
	if id == "" {
		return types.Result[types.User]{}, nil
	}
	return communityMembers(ctx, graphCommunityModerators, id, "")
}

// GraphCommunityHashtags fetches a page of a community's posts with hashtag.
func GraphCommunityHashtags(ctx context.Context, id, hashtag, after string) (types.Timeline, error) {
	if id == "" || hashtag == "" {
		return types.Timeline{}, nil
	}
	safeTag := strings.NewReplacer(`"`, "", `\`, "").Replace(hashtag)
	vars := format(communityHashtagsVars, id, cursorParam(after), safeTag)
	return communityTimeline(ctx, gqlReq(graphCommunityHashtags, vars, ""), after)
}

// GraphListTweets fetches a page of a list's timeline.
func GraphListTweets(ctx context.Context, id, after string) (types.Timeline, error) {
	if id == "" {
		return types.Timeline{}, nil
	}
	js, err := fetch(ctx, gqlReq(graphListTweets, format(restIdVars, id, cursorParam(after), "20"), ""))
	if err != nil {
		return types.Timeline{}, err
	}
	return parser.ParseGraphTimeline(js, after).Tweets, nil
}

// GraphListBySlug fetches the list slug owned by the user name.
func GraphListBySlug(ctx context.Context, name, slug string) (types.List, error) {
	vars := obj{{"screenName", name}, {"listSlug", slug}}.String()
	js, err := fetch(ctx, gqlReq(graphListBySlug, vars, ""))
	if err != nil {
		return types.List{}, err
	}
	return parser.ParseGraphList(js), nil
}

// GraphList fetches a list by ID.
func GraphList(ctx context.Context, id string) (types.List, error) {
	js, err := fetch(ctx, gqlReq(graphListById, obj{{"listId", id}}.String(), ""))
	if err != nil {
		return types.List{}, err
	}
	return parser.ParseGraphList(js), nil
}

// GraphListMembers fetches a page of a list's members.
func GraphListMembers(ctx context.Context, list types.List, after string) (types.Result[types.User], error) {
	if list.ID == "" {
		return types.Result[types.User]{}, nil
	}
	vars := obj{
		{"listId", list.ID},
		{"withBirdwatchPivots", false},
		{"withDownvotePerspective", false},
		{"withReactionsMetadata", false},
		{"withReactionsPerspective", false},
	}
	if after != "" {
		vars = vars.with("cursor", after)
	}
	js, err := fetchRawJSON(ctx, gqlReq(graphListMembers, vars.String(), ""))
	if err != nil {
		return types.Result[types.User]{}, err
	}
	return parser.ParseGraphListMembers(js, after), nil
}

func graphUserConnections(ctx context.Context, userID, endpoint string, kind types.QueryKind,
	after string) (types.Result[types.User], error) {
	if userID == "" {
		return types.Result[types.User]{}, nil
	}
	res, err := fxConnections(ctx, userID, kind, 20, after)
	if !fxFailed(err) {
		return res, err
	}
	vars := obj{
		{"userId", userID},
		{"count", 20},
		{"includePromotedContent", false},
		{"withGrokTranslatedBio", true},
	}
	if after != "" {
		vars = vars.with("cursor", after)
	}
	js, err := fetchRawJSON(ctx, gqlReq(endpoint, vars.String(), ""))
	if err != nil {
		return types.Result[types.User]{}, err
	}
	return parser.ParseGraphFollowers(js, after, kind), nil
}

// GraphFollowers fetches a page of a user's followers.
func GraphFollowers(ctx context.Context, userID, after string) (types.Result[types.User], error) {
	return graphUserConnections(ctx, userID, graphFollowers, types.QueryFollowers, after)
}

// GraphFollowing fetches a page of the users a user follows.
func GraphFollowing(ctx context.Context, userID, after string) (types.Result[types.User], error) {
	return graphUserConnections(ctx, userID, graphFollowing, types.QueryFollowing, after)
}

// GraphTweetResult fetches a single tweet with TweetResultByIdQuery.
func GraphTweetResult(ctx context.Context, id string) (*types.Tweet, error) {
	if id == "" {
		return nil, nil
	}
	js, err := fetch(ctx, gqlReq(graphTweetResult, obj{{"rest_id", id}}.String(), ""))
	if err != nil {
		return nil, err
	}
	return parser.ParseGraphTweetResult(js), nil
}

// TweetByRestID fetches a single tweet.
func TweetByRestID(ctx context.Context, id string) (*types.Tweet, error) {
	if id == "" {
		return nil, nil
	}
	js, err := fetch(ctx, gqlReq(graphTweetResultByRestId, format(tweetByRestIdVars, id), articleFieldToggles))
	if err != nil {
		return nil, err
	}
	return parser.ParseTweetByRestID(js), nil
}

// TweetTranslation fetches a tweet with Twitter's translations of it.
func TweetTranslation(ctx context.Context, id string) (*types.Tweet, error) {
	if id == "" {
		return nil, nil
	}
	js, err := fetch(ctx, gqlReq(graphTweetResultByRestId, format(tweetTransVars, id), tweetTransFieldToggles))
	if err != nil {
		return nil, err
	}
	return parser.ParseTweetByRestID(js), nil
}

func conversation(ctx context.Context, id, after string, mode types.RankingMode) (*types.Conversation, error) {
	if id == "" {
		return nil, nil
	}
	conv, err := fxConversation(ctx, id, mode, after)
	if !fxFailed(err) {
		return conv, err
	}
	js, err := fetch(ctx, tweetDetailURL(id, cursorParam(after), mode))
	if err != nil {
		return nil, err
	}
	return parser.ParseGraphConversation(js, id), nil
}

// Replies fetches the page of replies to tweet id after the cursor.
func Replies(ctx context.Context, id, after string, mode types.RankingMode) (types.Result[types.Chain], error) {
	conv, err := conversation(ctx, id, after, mode)
	if err != nil {
		return types.Result[types.Chain]{}, err
	}
	var replies types.Result[types.Chain]
	if conv != nil {
		replies = conv.Replies
	}
	replies.Beginning = after == ""
	return replies, nil
}

// Tweet fetches a tweet's conversation; with a cursor, its replies are the
// page after it.
func Tweet(ctx context.Context, id, after string, mode types.RankingMode) (*types.Conversation, error) {
	conv, err := conversation(ctx, id, "", mode)
	if err != nil || conv == nil || after == "" {
		return conv, err
	}
	if conv.Replies, err = Replies(ctx, id, after, mode); err != nil {
		return nil, err
	}
	return conv, nil
}

// GraphEditHistory fetches a tweet's edits.
func GraphEditHistory(ctx context.Context, id string) (types.EditHistory, error) {
	if id == "" {
		return types.EditHistory{}, nil
	}
	js, err := fetch(ctx, gqlReq(graphTweetEditHistory, format(tweetEditHistoryVars, id), ""))
	if err != nil {
		return types.EditHistory{}, err
	}
	return parser.ParseGraphEditHistory(js, id), nil
}

// GraphRetweeters fetches a page of the users who reposted a tweet.
func GraphRetweeters(ctx context.Context, id, after string) (types.UsersTimeline, error) {
	if id == "" {
		return types.UsersTimeline{}, nil
	}
	res, err := fxReposters(ctx, id, 20, after)
	if !fxFailed(err) {
		return res, err
	}
	js, err := fetch(ctx, gqlReq(graphRetweeters, format(reactorsVars, id, cursorParam(after)), ""))
	if err != nil {
		return types.UsersTimeline{}, err
	}
	return parser.ParseGraphRetweetersTimeline(js, after), nil
}

// GraphBirdwatchNotes fetches the community notes on a tweet.
func GraphBirdwatchNotes(ctx context.Context, id string) (types.BirdwatchNotes, error) {
	if id == "" {
		return types.BirdwatchNotes{}, nil
	}
	js, err := fetch(ctx, gqlReq(graphBirdwatchNotes, format(birdwatchNotesVars, id), ""))
	if err != nil {
		return types.BirdwatchNotes{}, err
	}
	return parser.ParseBirdwatchNotes(js), nil
}

// GraphBirdwatchHistory fetches a page of the notes by a contributor alias.
func GraphBirdwatchHistory(ctx context.Context, alias, after string) (types.BirdwatchHistory, error) {
	if alias == "" {
		return types.BirdwatchHistory{}, nil
	}
	vars := obj{{"count", 10}, {"alias", alias}}
	if after != "" {
		vars = vars.with("cursor", after)
	}
	js, err := fetch(ctx, gqlReq(graphBirdwatchContributorNotes, vars.String(), ""))
	if err != nil {
		return types.BirdwatchHistory{}, err
	}
	return parser.ParseBirdwatchHistory(js), nil
}

// GraphBirdwatchOneNote fetches a community note.
func GraphBirdwatchOneNote(ctx context.Context, noteID string) (types.BirdwatchSingleNote, error) {
	if noteID == "" {
		return types.BirdwatchSingleNote{}, nil
	}
	js, err := fetch(ctx, gqlReq(graphBirdwatchOneNote, format(birdwatchOneNoteVars, noteID), ""))
	if err != nil {
		return types.BirdwatchSingleNote{}, err
	}
	return parser.ParseBirdwatchOneNote(js), nil
}

// GraphBirdwatchGlobalTimeline fetches the posts with recent community notes.
func GraphBirdwatchGlobalTimeline(ctx context.Context) (types.Timeline, error) {
	js, err := fetch(ctx, gqlReq(graphBirdwatchGlobalTimeline, birdwatchGlobalTimelineVars, ""))
	if err != nil {
		return types.Timeline{}, err
	}
	return parser.ParseBirdwatchGlobalTimeline(js), nil
}

// GraphCommunityNotesNew fetches the posts with new community notes.
func GraphCommunityNotesNew(ctx context.Context) (types.Timeline, error) {
	js, err := fetch(ctx, gqlReq(graphGenericTimelineById, format(genericTimelineVars, communityNotesNewTimelineId), ""))
	if err != nil {
		return types.Timeline{}, err
	}
	return parser.ParseGenericTweetTimeline(js), nil
}

func searchVars(rawQuery, product string) obj {
	return obj{
		{"rawQuery", rawQuery},
		{"count", 20},
		{"querySource", "typed_query"},
		{"product", product},
		{"withGrokTranslatedBio", true},
		{"withQuickPromoteEligibilityTweetFields", false},
	}
}

// GraphTweetSearch fetches a page of tweets matching q. A cursor of the form
// "maxid:<id>" pages back from that tweet instead.
func GraphTweetSearch(ctx context.Context, q types.Query, after string) (types.Timeline, error) {
	// workaround for #1372
	maxID := ""
	if rest, ok := strings.CutPrefix(after, "maxid:"); ok {
		maxID = utils.ValidateNumber(rest)
	}

	rawQuery := query.Param(q, maxID)
	if rawQuery == "" || rawQuery == query.EmptyQuery {
		return types.Timeline{Query: q, Beginning: true}, nil
	}

	var product string
	switch {
	case q.Kind == types.QueryTop:
		product = "Top"
	// profile media feeds (RSS, multi-user timelines) must stay chronological
	case q.Kind == types.QueryMedia && len(q.FromUser) == 0:
		product = "Media"
	default:
		product = "Latest"
	}

	cursor := ""
	if maxID == "" {
		cursor = after
	}

	var result types.Timeline
	fromFx := false
	// https://docs.fxembed.com/api/twitter/operations/2search/ caps q at 512
	if len(rawQuery) <= 512 {
		tl, err := fxSearch(ctx, rawQuery, strings.ToLower(product), 20, cursor)
		if !fxFailed(err) {
			if err != nil {
				return types.Timeline{}, err
			}
			result, fromFx = tl, true
		}
	}

	if !fromFx {
		vars := searchVars(rawQuery, product).with("withBirdwatchNotes", true)
		if cursor != "" {
			vars = vars.with("cursor", cursor)
		}
		js, err := fetch(ctx, gqlReq(graphSearchTimeline, vars.String(), ""))
		if err != nil {
			return types.Timeline{}, err
		}
		result = parser.ParseGraphSearchTweets(js, after)
	}
	result.Query = q

	// when no more items are available the API just returns the last page in
	// full. this detects that and clears the page instead.
	prefix := min(64, len(after), len(result.Bottom))
	if prefix > 0 && maxID == "" && after[:prefix] == result.Bottom[:prefix] {
		result.Content = nil
	}
	return result, nil
}

func productSearch(ctx context.Context, q types.Query, product, after string) (gjson.Result, error) {
	vars := searchVars(q.Text, product)
	if after != "" {
		vars = vars.with("cursor", after)
	}
	return fetch(ctx, gqlReq(graphSearchTimeline, vars.String(), ""))
}

// GraphUserSearch fetches a page of users matching q.Text.
func GraphUserSearch(ctx context.Context, q types.Query, after string) (types.Result[types.User], error) {
	if n := len(q.Text); n >= 1 && n <= 512 {
		res, err := fxUserSearch(ctx, q.Text, 20, after)
		if !fxFailed(err) {
			res.Query = q
			return res, err
		}
	}
	if q.Text == "" {
		return types.Result[types.User]{Query: q, Beginning: true}, nil
	}
	js, err := productSearch(ctx, q, "People", after)
	if err != nil {
		return types.Result[types.User]{}, err
	}
	res := parser.ParseGraphSearchUsers(js, after)
	res.Query = q
	return res, nil
}

// GraphListSearch fetches a page of lists matching q.Text.
func GraphListSearch(ctx context.Context, q types.Query, after string) (types.Result[types.ListSearchResult], error) {
	if q.Text == "" {
		return types.Result[types.ListSearchResult]{Query: q, Beginning: true}, nil
	}
	js, err := productSearch(ctx, q, "Lists", after)
	if err != nil {
		return types.Result[types.ListSearchResult]{}, err
	}
	res := parser.ParseGraphSearchLists(js, after)
	res.Query = q
	return res, nil
}

// PhotoRail fetches the recent photos shown on a profile.
func PhotoRail(ctx context.Context, id string) (types.PhotoRail, error) {
	if id == "" {
		return nil, nil
	}
	media, err := fxUserTimeline(ctx, id, "media", 16, "")
	if !fxFailed(err) {
		if err != nil {
			return nil, err
		}
		var rail types.PhotoRail
		for _, thread := range media.Content {
			if photo := parser.ExtractGalleryPhoto(thread[0]); photo.URL != "" {
				rail = append(rail, photo)
			}
		}
		return rail, nil
	}
	js, err := fetch(ctx, mediaURL(id, ""))
	if err != nil {
		return nil, err
	}
	return parser.ParseGraphPhotoRail(js), nil
}

// GraphArticle fetches the article attached to tweet id.
func GraphArticle(ctx context.Context, id string) (*types.Article, error) {
	if id == "" {
		return nil, nil
	}
	js, err := fetchRawJSON(ctx, gqlReq(graphTweetResultByRestId, format(articleVars, id), articleFieldToggles))
	if err != nil {
		return nil, err
	}
	return parser.ParseGraphArticle(js), nil
}

// GraphTweetResults fetches several tweets at once.
func GraphTweetResults(ctx context.Context, ids []string) ([]*types.Tweet, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = `"` + id + `"`
	}
	idsJSON := "[" + strings.Join(quoted, ",") + "]"
	js, err := fetch(ctx, gqlReq(graphTweetResultsByRestIds, format(articleBatchVars, idsJSON), articleFieldToggles))
	if err != nil {
		return nil, err
	}
	return parser.ParseGraphTweetResults(js), nil
}

// noRedirectClient shares httpClient's connections but returns redirects
// instead of following them.
func noRedirectClient() *http.Client {
	c := *httpClient
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &c
}

// Resolve returns where a short link such as t.co redirects to, rewritten
// for p, or "" if it doesn't redirect.
func Resolve(ctx context.Context, link string, p *prefs.Prefs) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodHead, link, nil)
	if err != nil {
		return "", err
	}
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		return "", requestError(ctx, reqCtx, err)
	}
	resp.Body.Close()
	location := resp.Header.Get("Location")
	if location == "" {
		return "", nil
	}
	return formatters.ReplaceURLs(location, p, ""), nil
}
