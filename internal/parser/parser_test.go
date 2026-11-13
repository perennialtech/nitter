// SPDX-License-Identifier: AGPL-3.0-only

package parser

import (
	"slices"
	"testing"
	"time"

	"github.com/perennialtech/nitter/internal/types"
)

const legacyTweet = `{"__typename":"Tweet","rest_id":"100",
  "core":{"user_results":{"result":{"rest_id":"1","is_blue_verified":true,"legacy":{
    "screen_name":"bob","name":"Bob","description":"bio @carl","entities":{},
    "profile_image_url_https":"https://pbs.twimg.com/profile_images/1/a_normal.jpg"}}}},
  "card":{"legacy":{"name":"summary","url":"https://t.co/c","binding_values":[
    {"key":"title","value":{"string_value":"T","type":"STRING"}},
    {"key":"description","value":{"string_value":"D","type":"STRING"}},
    {"key":"vanity_url","value":{"string_value":"ex.com","type":"STRING"}},
    {"key":"thumbnail_image_large","value":{"image_value":{"url":"https://pbs.twimg.com/card_img/1"}}}]}},
  "views":{"count":"42"},
  "legacy":{"id_str":"100","full_text":"@alice hello https://t.co/x https://t.co/c",
    "display_text_range":[7,42],"in_reply_to_status_id_str":"99","in_reply_to_screen_name":"alice",
    "created_at":"Wed Dec 19 20:20:32 +0000 2007","favorite_count":5,
    "entities":{"user_mentions":[{"screen_name":"alice","name":"Alice","indices":[0,6]}],
      "urls":[{"url":"https://t.co/x","expanded_url":"https://example.com/page","indices":[13,27]},
              {"url":"https://t.co/c","expanded_url":"https://ex.com/a","indices":[28,42]}]}}}`

func TestParseGraphTweetLegacy(t *testing.T) {
	tw := ParseGraphTweet(pj(legacyTweet))
	if tw.ID != 100 || tw.ReplyID != 99 || !tw.Available {
		t.Errorf("ids: %+v", tw)
	}
	if !slices.Equal(tw.Reply, []string{"alice"}) {
		t.Errorf("reply = %v", tw.Reply)
	}
	wantText := `hello <a href="https://example.com/page">example.com/page</a> ` +
		`<a href="https://ex.com/a">ex.com/a</a>`
	if tw.Text != wantText {
		t.Errorf("text = %q", tw.Text)
	}
	if tw.Stats.Views != 42 || tw.Stats.Likes != 5 {
		t.Errorf("stats = %+v", tw.Stats)
	}
	if !tw.Time.Equal(time.Date(2007, 12, 19, 20, 20, 32, 0, time.UTC)) {
		t.Errorf("time = %v", tw.Time)
	}
	u := tw.User
	if u.ID != "1" || u.Username != "bob" || u.UserPic != "profile_images/1/a.jpg" ||
		u.VerifiedType != types.VerifiedBlue || u.Bio != `bio <a href="/carl">@carl</a>` {
		t.Errorf("user = %+v", u)
	}
	c := tw.Card
	if c == nil {
		t.Fatal("no card")
	}
	want := types.Card{Kind: types.CardSummary, URL: "https://ex.com/a", Title: "T", Dest: "ex.com",
		Text: "D", Image: "card_img/1"}
	if *c != want {
		t.Errorf("card = %+v", *c)
	}
}

func TestParseGraphTweetWrappersAndTombstones(t *testing.T) {
	tw := ParseGraphTweet(pj(`{"__typename":"TweetWithVisibilityResults","tweet":` + legacyTweet + `}`))
	if tw.ID != 100 {
		t.Errorf("visibility wrapper: id %d", tw.ID)
	}
	tw = ParseGraphTweet(pj(`{"__typename":"TweetTombstone","tombstone":{"text":{"text":"This Post was deleted. Learn more"}}}`))
	if tw.Text != "This Post was deleted." || tw.Available {
		t.Errorf("tombstone = %+v", tw)
	}
	tw = ParseGraphTweet(pj(`{"__typename":"TweetUnavailable"}`))
	if tw == nil || tw.Available || tw.User.VerifiedType != types.VerifiedNone {
		t.Errorf("unavailable = %+v", tw)
	}
	if ParseGraphTweetResult(pj(`{"data":{}}`)) != nil {
		t.Error("missing result should be nil")
	}
}

func TestParseGraphTweetUnavailableQuote(t *testing.T) {
	tw := ParseGraphTweet(pj(`{"__typename":"Tweet","rest_id":"5","legacy":{"full_text":"q",
		"display_text_range":[0,1],"entities":{},"is_quote_status":true,"quoted_status_id_str":"6",
		"quoted_status_permalink":{"expanded":"https://twitter.com/someone/status/6"}}}`))
	if tw.Quote == nil || tw.Quote.ID != 6 {
		t.Fatalf("quote = %+v", tw.Quote)
	}
	if tw.Quote.ExpURL != "/someone/status/6" || tw.Quote.Text != "This post from @someone is unavailable." {
		t.Errorf("quote = %+v", tw.Quote)
	}
}

func TestParseGraphTweetDetailsShape(t *testing.T) {
	tw := ParseGraphTweet(pj(`{"__typename":"Tweet","rest_id":"7",
		"details":{"full_text":"hi #go https://t.co/v","created_at_ms":1700000000123,"display_text_range":[0,21],
		  "hashtag_entities":[{"indices":[3,6]}]},
		"counts":{"favorite_count":3},
		"media_entities":[{"url":"https://t.co/v","source_status_id_str":"1",
		  "media_results":{"result":{"media_availability_v2":{"status":"Available"},
		  "media_info":{"__typename":"ApiVideo","duration_millis":1500,
		    "preview_image":{"original_img_url":"https://pbs.twimg.com/thumb.jpg"},
		    "variants":[{"content_type":"video/mp4","bit_rate":100,"url":"https://video.twimg.com/v/vid/1x2/a.mp4"}]}}}}]}`))
	if tw.Text != `hi <a href="/search?f=tweets&q=%23go">#go</a>` {
		t.Errorf("text = %q", tw.Text)
	}
	if !tw.Time.Equal(time.Unix(1700000000, 0)) || tw.Stats.Likes != 3 {
		t.Errorf("time %v stats %+v", tw.Time, tw.Stats)
	}
	if len(tw.Media) != 1 || tw.Media[0].Kind != types.VideoMedia {
		t.Fatalf("media = %+v", tw.Media)
	}
	v := tw.Media[0].Video
	if !v.Available || v.Thumb != "thumb.jpg" || v.PlaybackType != types.VideoM3u8 ||
		len(v.Variants) != 1 || v.Variants[0].Resolution != 2 || v.Variants[0].Bitrate != 100 {
		t.Errorf("video = %+v", v)
	}
}

func TestParsePoll(t *testing.T) {
	end := time.Now().Add(50 * time.Hour).UTC().Format(isoLayout)
	poll := parsePoll(pj(`{"name":"poll3choice_text_only","binding_values":{
		"choice1_label":{"string_value":"a"},"choice1_count":{"string_value":"2"},
		"choice2_label":{"string_value":"b"},"choice2_count":{"string_value":"7"},
		"choice3_label":{"string_value":"c"},"choice3_count":{"string_value":"7"},
		"end_datetime_utc":{"string_value":"` + end + `"}}}`))
	if !slices.Equal(poll.Options, []string{"a", "b", "c"}) || !slices.Equal(poll.Values, []int{2, 7, 7}) ||
		poll.Leader != 1 || poll.Votes != 16 || poll.Status != "2 days" {
		t.Errorf("poll = %+v", poll)
	}
	poll = parsePoll(pj(`{"name":"poll2choice_text_only","binding_values":{}}`))
	if poll.Status != "Final results" {
		t.Errorf("status = %q", poll.Status)
	}
}

func tweetEntry(id string) string {
	return `{"entryId":"tweet-` + id + `","content":{"itemContent":{"tweet_results":{"result":
		{"__typename":"Tweet","rest_id":"` + id + `","legacy":{"full_text":"t` + id +
		`","display_text_range":[0,9],"entities":{}}}}}}}`
}

func TestParseGraphConversationCursors(t *testing.T) {
	conv := ParseGraphConversation(pj(`{"data":{"threaded_conversation_with_injections_v2":{"instructions":[
		{"type":"TimelineAddEntries","entries":[
		  `+tweetEntry("4")+`,`+tweetEntry("5")+`,
		  {"entryId":"conversationthread-6","content":{"items":[
		    {"entryId":"conversationthread-6-tweet-6","item":{"itemContent":{"tweet_results":{"result":
		      {"__typename":"Tweet","rest_id":"6","legacy":{"full_text":"r","display_text_range":[0,1],"entities":{}}}}}}},
		    {"entryId":"conversationthread-6-tweet-60","item":{"itemContent":{}}},
		    {"entryId":"conversationthread-6-cursor-showmore-1","item":{"content":{"value":"MORE"}}}]}},
		  {"entryId":"tombstone-8","content":{"itemContent":{"tombstoneInfo":{"richText":{"text":"gone Learn more"}}}}},
		  {"entryId":"cursor-bottom-9","content":{"content":{"value":"BOT"}}}]}]}}}`), "5")
	if conv.Tweet == nil || conv.Tweet.ID != 5 {
		t.Fatalf("tweet = %+v", conv.Tweet)
	}
	if len(conv.Before.Content) != 2 || conv.Before.Content[0].ID != 4 ||
		conv.Before.Content[1].ID != 8 || conv.Before.Content[1].Text != "gone" {
		t.Errorf("before = %+v", conv.Before.Content)
	}
	if len(conv.Replies.Content) != 1 {
		t.Fatalf("replies = %+v", conv.Replies)
	}
	thread := conv.Replies.Content[0]
	if !thread.HasMore || thread.Cursor != "MORE" || len(thread.Content) != 2 || thread.Content[1].ID != 60 {
		t.Errorf("thread = %+v", thread)
	}
	if conv.Replies.Bottom != "BOT" || !conv.Replies.Beginning {
		t.Errorf("bottom = %q", conv.Replies.Bottom)
	}
	if conv := ParseGraphConversation(pj(`{}`), "1"); conv == nil || !conv.Replies.Beginning {
		t.Errorf("empty = %+v", conv)
	}
}

func TestParseGraphSearchCursors(t *testing.T) {
	js := pj(`{"data":{"search_by_raw_query":{"search_timeline":{"timeline":{"instructions":[
		{"type":"TimelineAddEntries","entries":[` + tweetEntry("1") + `,
		  {"entryId":"user-2","content":{"itemContent":{"user_results":{"result":{"rest_id":"2","legacy":{"screen_name":"u"}}}}}},
		  {"entryId":"cursor-bottom-0","content":{"value":"B1"}}]},
		{"type":"TimelineReplaceEntry","entry_id_to_replace":"cursor-bottom-0","entry":{"content":{"value":"B2"}}}]}}}}}`)
	tl := ParseGraphSearchTweets(js, "")
	if len(tl.Content) != 1 || tl.Content[0][0].ID != 1 || tl.Bottom != "B2" || !tl.Beginning {
		t.Errorf("tweets = %+v", tl)
	}
	users := ParseGraphSearchUsers(js, "x")
	if len(users.Content) != 1 || users.Content[0].Username != "u" || users.Beginning {
		t.Errorf("users = %+v", users)
	}
	if lists := ParseGraphSearchLists(js, ""); len(lists.Content) != 0 || lists.Bottom != "B2" {
		t.Errorf("lists = %+v", lists)
	}
}

func TestParseGraphTimelinePinned(t *testing.T) {
	js := pj(`{"data":{"user_result":{"result":{"timeline_response":{"timeline":{"instructions":[
		{"__typename":"TimelinePinEntry","entry":` + tweetEntry("9") + `},
		{"__typename":"TimelineAddEntries","entries":[` + tweetEntry("3") + `,
		  {"entryId":"cursor-bottom-1","content":{"value":"NEXT"}}]}]}}}}}}`)
	p := ParseGraphTimeline(js, "")
	if p.Pinned == nil || p.Pinned.ID != 9 || !p.Pinned.Pinned {
		t.Errorf("pinned = %+v", p.Pinned)
	}
	if len(p.Tweets.Content) != 1 || p.Tweets.Content[0][0].ID != 3 || p.Tweets.Bottom != "NEXT" {
		t.Errorf("tweets = %+v", p.Tweets)
	}
	if p := ParseGraphTimeline(js, "c"); p.Pinned != nil || p.Tweets.Beginning {
		t.Errorf("paged = %+v", p)
	}

	tl := ParseGraphCommunityTimeline(pj(`{"data":{"communityResults":{"result":{"ranked_community_timeline":{"timeline":{"instructions":[
		{"type":"TimelineAddEntries","entries":[`+tweetEntry("3")+`]},
		{"type":"TimelinePinEntry","entry":`+tweetEntry("9")+`}]}}}}}}`), "")
	if len(tl.Content) != 2 || tl.Content[0][0].ID != 9 || !tl.Content[0][0].Pinned {
		t.Errorf("community = %+v", tl.Content)
	}
}

func TestParseGraphUserLegacyShape(t *testing.T) {
	u := ParseGraphUser(pj(`{"data":{"user":{"result":{"rest_id":"7","is_blue_verified":true,"legacy":{
		"id_str":"7","screen_name":"jack","name":"Jack","description":"see https://t.co/a #go",
		"entities":{"url":{"urls":[{"expanded_url":"https://jack.example"}]},
		  "description":{"urls":[{"url":"https://t.co/a","expanded_url":"https://example.com/x","indices":[4,18]}]}},
		"profile_image_url_https":"https://pbs.twimg.com/profile_images/1/a_normal.jpg",
		"profile_banner_url":"https://pbs.twimg.com/profile_banners/7/1",
		"followers_count":10,"created_at":"Wed Dec 19 20:20:32 +0000 2007","pinned_tweet_ids_str":["123"]}}}}}`))
	want := types.User{
		ID: "7", Username: "jack", Fullname: "Jack", Website: "https://jack.example",
		Bio: `see <a href="https://example.com/x">example.com/x</a> <a href="/search?f=tweets&q=%23go">#go</a>`,
		// The experimental parser strips the host before the scheme, so only the scheme goes.
		UserPic:      "pbs.twimg.com/profile_images/1/a.jpg",
		Banner:       "https://pbs.twimg.com/profile_banners/7/1/1500x500",
		PinnedTweet:  123,
		Followers:    10,
		VerifiedType: types.VerifiedBlue,
		JoinDate:     time.Date(2007, 12, 19, 20, 20, 32, 0, time.UTC),
	}
	if u != want {
		t.Errorf("got  %+v\nwant %+v", u, want)
	}
}

func TestParseGraphUserModernShape(t *testing.T) {
	u := ParseGraphUser(pj(`{"data":{"user_result":{"result":{"rest_id":"8",
		"core":{"screen_name":"ann","name":"Ann","created_at":"Wed Dec 19 20:20:32 +0000 2007"},
		"avatar":{"image_url":"https://pbs.twimg.com/x_normal.jpg"},
		"relationship_counts":{"followers":3,"following":4},"tweet_counts":{"tweets":5,"media_tweets":6},
		"action_counts":{"favorites_count":7},"privacy":{"protected":true},"location":{"location":"Here"},
		"verification":{"verified_type":"Business"},
		"profile_bio":{"description":"hi @bob","entities":{"url":{"urls":[{"expanded_url":"https://ann.example"}]}}},
		"parody_commentary_fan_label":"Parody"}}}}`))
	want := types.User{
		ID: "8", Username: "ann", Fullname: "Ann", UserPic: "https://pbs.twimg.com/x.jpg",
		Followers: 3, Following: 4, Tweets: 5, Media: 6, Likes: 7, Protected: true, Location: "Here",
		VerifiedType: types.VerifiedBusiness, Bio: `hi <a href="/bob">@bob</a>`,
		Website: "https://ann.example", ParodyLabel: "Parody",
		JoinDate: time.Date(2007, 12, 19, 20, 20, 32, 0, time.UTC),
	}
	if u != want {
		t.Errorf("got  %+v\nwant %+v", u, want)
	}

	u = ParseGraphUser(pj(`{"data":{"user":{"result":{"__typename":"UserUnavailable","unavailable_reason":"Suspended"}}}}`))
	if !u.Suspended {
		t.Errorf("suspended = %+v", u)
	}
	if u := ParseGraphUser(pj(`[]`)); u.Username != "" || u.VerifiedType != types.VerifiedNone {
		t.Errorf("non-object = %+v", u)
	}
}

func TestParseGraphFollowersAndMembers(t *testing.T) {
	timeline := `{"timeline":{"instructions":[{"type":"TimelineAddEntries","entries":[
		{"content":{"entryType":"TimelineTimelineItem","itemContent":{"user_results":{"result":{"rest_id":"1","legacy":{"screen_name":"a"}}}}}},
		{"content":{"entryType":"TimelineTimelineItem","itemContent":{"user_results":{"result":{}}}}},
		{"content":{"entryType":"TimelineTimelineCursor","cursorType":"Top","value":"T"}},
		{"content":{"entryType":"TimelineTimelineCursor","cursorType":"Bottom","value":"B"}}]}]}}`
	f := ParseGraphFollowers(pj(`{"data":{"user":{"result":{"timeline":`+timeline+`}}}}`), "", types.QueryFollowing)
	if len(f.Content) != 1 || f.Content[0].Username != "a" || f.Bottom != "B" || !f.Beginning ||
		f.Query.Kind != types.QueryFollowing {
		t.Errorf("followers = %+v", f)
	}
	m := ParseGraphListMembers(pj(`{"data":{"list":{"members_timeline":`+timeline+`}}}`), "c")
	if len(m.Content) != 1 || m.Bottom != "B" || m.Beginning || m.Query.Kind != types.QueryUserList {
		t.Errorf("members = %+v", m)
	}
}

func TestParseUnifiedCard(t *testing.T) {
	c := ParseUnifiedCard(pj(`{"component_objects":{
		  "details_1":{"type":"details","data":{"title":{"content":"Title"},"destination":"d1"}},
		  "media_1":{"type":"media","data":{"id":"m1"}}},
		"destination_objects":{"d1":{"type":"browser","data":{"url_data":{"url":"https://example.com","vanity":"example.com"}}}},
		"media_entities":{"m1":{"type":"photo","media_url_https":"https://pbs.twimg.com/media/a.jpg"}}}`))
	want := types.Card{Kind: types.CardSummaryLarge, URL: "https://example.com", Dest: "example.com",
		Text: "Title", Image: "pbs.twimg.com/media/a.jpg"}
	if c != want {
		t.Errorf("got %+v", c)
	}

	c = ParseUnifiedCard(pj(`{"component_objects":{"swipeable_media_1":{"type":"swipeable_media","data":{"media_list":[{"id":"v"}]}}},
		"media_entities":{"v":{"type":"video","media_url_https":"https://pbs.twimg.com/t.jpg",
		  "video_info":{"duration_millis":1000,"variants":[{"content_type":"video/mp4","url":"u","bitrate":5}]}}}}`))
	if c.Kind != types.CardPromoVideo || c.Video == nil || c.Video.DurationMs != 1000 ||
		!slices.Equal(c.Video.Variants, []types.VideoVariant{{ContentType: types.VideoMp4, URL: "u", Bitrate: 5}}) {
		t.Errorf("video = %+v %+v", c, c.Video)
	}

	c = ParseUnifiedCard(pj(`{"component_objects":{"list":{"type":"twitter_list_details","data":{"name":"L","member_count":12,"destination":"d"}}},
		"destination_objects":{"d":{"data":{"url_data":{"url":"https://x.com/i/lists/1","vanity":"x.com"}}}}}`))
	if c.Kind != types.CardAmplify || c.Text != "L" || c.Dest != "List · 12 Members" || c.URL != "https://x.com/i/lists/1" {
		t.Errorf("list = %+v", c)
	}
}

func TestParseGraphArticle(t *testing.T) {
	a := ParseGraphArticle(pj(`{"data":{"tweetResult":{"result":{
		"core":{"user_results":{"result":{"rest_id":"1","legacy":{"screen_name":"w"}}}},
		"views":{"count":"9"},"legacy":{"reply_count":1,"favorite_count":2},
		"article":{"article_results":{"result":{"title":"Art","metadata":{"first_published_at_secs":1700000000},
		  "cover_media":{"media_info":{"original_img_url":"https://pbs.twimg.com/media/c.jpg"}},
		  "content_state":{"blocks":[{"text":"Hello","type":"unstyled",
		      "inlineStyleRanges":[{"offset":0,"length":5,"style":"Bold"}],"entityRanges":[{"offset":0,"length":5,"key":0}]}],
		    "entityMap":[{"key":"0","value":{"type":"LINK","data":{"url":"https://e.com"}}},
		                 {"key":"1","value":{"type":"MEDIA","data":{"mediaItems":[{"mediaId":"m"}],"caption":"cap"}}},
		                 {"key":"x","value":{"type":"LINK"}}]},
		  "media_entities":[{"media_id":"m","media_info":{"__typename":"ApiVideo","video_info":{"variants":[
		      {"url":"lo","bitrate":1},{"url":"hi","bitrate":9}]}}}]}}}}}}}`))
	if a == nil {
		t.Fatal("nil article")
	}
	if a.Title != "Art" || a.CoverImage != "pbs.twimg.com/media/c.jpg" || a.User.Username != "w" ||
		!a.Time.Equal(time.Unix(1700000000, 0)) || a.Stats.Views != 9 || a.Stats.Likes != 2 {
		t.Errorf("article = %+v", a)
	}
	if len(a.Paragraphs) != 1 || a.Paragraphs[0].Kind != "unstyled" ||
		a.Paragraphs[0].InlineStyles[0].Style != "Bold" || a.Paragraphs[0].EntityRanges[0].Length != 5 {
		t.Errorf("paragraphs = %+v", a.Paragraphs)
	}
	if len(a.Entities) != 2 || a.Entities[0].URL != "https://e.com" ||
		!slices.Equal(a.Entities[1].MediaIDs, []string{"m"}) || a.Entities[1].Caption != "cap" {
		t.Errorf("entities = %+v", a.Entities)
	}
	if a.Media["m"] != (types.ArticleMedia{Kind: "ApiVideo", URL: "hi"}) {
		t.Errorf("media = %+v", a.Media)
	}
	if ParseGraphArticle(pj(`{"data":{}}`)) != nil {
		t.Error("article without title should be nil")
	}
}

func TestParseBirdwatchNotesOrder(t *testing.T) {
	n := ParseBirdwatchNotes(pj(`{"data":{"tweet_result_by_rest_id":{"result":{
		"not_misleading_birdwatch_notes":{"notes":[{"rest_id":"2"}]},
		"misleading_birdwatch_notes":{"notes":[{"rest_id":"1","rating_status":"CurrentlyRatedHelpful",
		  "data_v1":{"summary":{"text":"s"},"misleading_tags":["a"]},"created_at":1700000000000,
		  "is_community_note_translatable":true}]}}}}}`))
	if len(n.Notes) != 2 || n.Notes[0].ID != "1" || !n.Notes[0].Misleading || n.Notes[1].Misleading {
		t.Fatalf("notes = %+v", n.Notes)
	}
	first := n.Notes[0]
	if !first.Helpful || first.Text != "s" || !slices.Equal(first.MisleadingTags, []string{"a"}) ||
		!first.Translatable || !first.IsTranslatable || !first.CreatedAt.Equal(time.Unix(1700000000, 0)) {
		t.Errorf("note = %+v", first)
	}
}

func TestParseGraphCommunityMembers(t *testing.T) {
	r := ParseGraphCommunityMembers(pj(`{"data":{"communityResults":{"result":{"members_slice":{
		"items_results":[{"result":{"rest_id":"1","core":{"screen_name":"m"}}},{"result":{}}],
		"slice_info":{"next_cursor":"N"}}}}}}`), "")
	if len(r.Content) != 1 || r.Content[0].Username != "m" || r.Bottom != "N" {
		t.Errorf("members = %+v", r)
	}
}
