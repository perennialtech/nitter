// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"strings"

	"github.com/perennialtech/nitter/internal/auth"
)

// compact strips the spaces and newlines the templates below are written
// with, as upstream does.
func compact(s string) string {
	return strings.NewReplacer(" ", "", "\n", "").Replace(s)
}

const (
	consumerKey                    = "3nVuSoBZnx6U4vzUxf5w"
	consumerSecret                 = "Bcs59EFbbsdF6Sl9Ng71smgStWEGwXXKSjYvPVt7qys"
	bearerToken                    = "Bearer AAAAAAAAAAAAAAAAAAAAANRILgAAAAAAnNwIzUejRCOuH5E6I8xnZz4puTs%3D1Zv7ttfk8LF81IUq16cHjhLTvJu4FA33AGWWjCpTnA"
	bearerToken2                   = "Bearer AAAAAAAAAAAAAAAAAAAAAFXzAwAAAAAAMHCxpeSDG1gLNLghVe8d74hl6k4%3DRUMF4xAQLsbeBhTSRrCiQpJtxoGWeyHrDb5te2jpGskWDFW82F"
	graphUser                      = "Gb-d6r0vxPOADdG62OEBpQ/UserByScreenName"
	graphUserV2                    = "-ZzAG_Bckx16LMbEvHC3lg/UserResultByScreenNameQuery"
	graphUserById                  = "xvmVfRLmnr1alc5f2dib0Q/UserByRestId"
	graphUserTweetsV2              = auth.GraphUserTweetsV2
	graphUserTweetsAndRepliesV2    = "AcYHjc_YAx-9_rKWdMsKvA/UserWithProfileTweetsAndRepliesQueryV2"
	graphUserTweets                = "SXVCYB8XHSS25nzIljNtZA/UserTweets"
	graphUserTweetsOriginals       = "Yr8749ieoUptxRqQv766Fw/UserOriginalsTimeline"
	graphUserTweetsAndReplies      = "qUpkZU6eN8MbtQb7rC_pYg/UserTweetsAndReplies"
	graphUserReplies               = "xz348nziCm96wndJ1S0MUQ/UserRepliesTimeline"
	graphUserMedia                 = "VyudDWQnr9vJNw7GasFz2g/UserMedia"
	graphUserMediaV2               = "WK111rbR0vM0ZX4lyZCYjw/MediaTimelineV2"
	graphUserArticles              = "ZmMjUyrTpwYfTGAdylEyMw/UserArticlesTweets"
	graphUserArticlesV2            = "PsGixN38UZz2RheyayNB5Q/UserProfileArticlesTimelineQuery"
	graphUserReposts               = "CnNg1YiKG7bRgi-RoIfKsw/UserRepostsTimeline"
	graphTweet                     = "OZMbEnEa96AN8Pq6HyTWdw/ConversationTimeline"
	graphTweetDetail               = "XMOz5h24KAZ86qKffKTLdQ/TweetDetail"
	graphTweetResult               = "xYOrBQoTlfKJJPsX76MZEw/TweetResultByIdQuery"
	graphTweetEditHistory          = "1izbuOcH_QpuMcyCxOXkAg/TweetEditHistory"
	graphSearchTimeline            = "hyPfJYJ_XAtDYoslQc-Rgg/SearchTimeline"
	graphRetweeters                = "53eDOg7W9h2J866mtRyaXA/Retweeters"
	graphBirdwatchNotes            = "gIsAL0K8Ceo7h4lY149Eog/BirdwatchFetchNotes"
	graphBirdwatchContributorNotes = "DM12wC9t64PNE_tB9syjRw/BirdwatchFetchContributorNotesSlice"
	graphBirdwatchOneNote          = "1lt6XSRik4s93WG0BrEvig/BirdwatchFetchOneNote"
	graphBirdwatchGlobalTimeline   = "8h4-Db-fcu8bNAA7mpNAVw/BirdwatchFetchGlobalTimeline"
	graphGenericTimelineById       = "yXgbfagsvlYI3tUiju1_tA/GenericTimelineById"
	communityNotesNewTimelineId    = "VGltZWxpbmU6CwA6AAAAEjcxMzgxNDUxOTEwNTk5ODg0OAA="
	graphListById                  = "niz0TtOxL2zIcbq6_NQiNw/ListByRestId"
	graphListBySlug                = "RqkWNDQpOntlxNtJa4RIoQ/ListBySlug"
	graphListMembers               = "8rYmkvWQe9jRRZdy_-vkGA/ListMembers"
	graphListTweets                = "0QJtcuMzVywHGAWD6Dtjlw/ListTimeline"
	graphAboutAccount              = "TzOG2twZEfhr9KmClvVVqA/AboutAccountQuery"
	graphCommunity                 = "-ElI1vg3dYbttVMhBhGdLw/CommunityQuery"
	graphCommunityTweets           = "EwftYyqQemkckQ0wzGM6uw/CommunityTweetsTimeline"
	graphCommunityMedia            = "ESJtwnI_apuGesbJncpc0Q/CommunityMediaTimeline"
	graphCommunityMembers          = "woAp_YdzAdqnWDrqLTNpAw/membersSliceTimeline_Query"
	graphCommunityModerators       = "0oYT9GRiWUhrz5xoqFE9uw/moderatorsSliceTimeline_Query"
	graphCommunityHashtags         = "D5EqomOIWeJnSkMhL-FLew/CommunityHashtagsTimeline"
	graphTweetResultByRestId       = "GZsN2Pc4knAoit6pXa4HSA/TweetResultByRestId"
	graphTweetResultsByRestIds     = "Pho4sg8jLcrVlMeclMayrg/TweetResultsByRestIds"
	graphBroadcast                 = "RG6wSogandh6WPIzxW9aag/BroadcastQuery"
	graphAudioSpace                = "Bh0L6azTQoMs9rJKeCF4wQ/AudioSpaceById"
	restLiveStream                 = "1.1/live_video_stream/status/"
	restTrends                     = "1.1/trends/place.json"
	graphFollowers                 = "JNyQdTISpzCkj_1fqxDvFg/Followers"
	graphFollowing                 = "qGZZDF3mp91q7X22s3HxpA/Following"
	articleFieldToggles            = `{"withArticleRichContentState":true,"withArticlePlainText":false,"withArticleSummaryText":true,"withArticleVoiceOver":true}`
	tweetTransFieldToggles         = `{"withArticleRichContentState":true,"withArticlePlainText":false,"withArticleSummaryText":true,"withArticleVoiceOver":true,"withPayments":false}`
	userFieldToggles               = `{"withPayments":false,"withAuxiliaryUserLabels":true}`
	userTweetsFieldToggles         = `{"withArticleRichContentState":true,"withArticlePlainText":false}`
	userRepliesFieldToggles        = `{"withPayments":false,"withArticlePlainText":false}`
	tweetDetailFieldToggles        = `{"withArticleRichContentState":true,"withArticlePlainText":false,"withGrokAnalyze":false,"withDisallowedReplyControls":false}`
)

var (
	gqlFeatures = compact(`{
  "rweb_video_screen_enabled": false,
  "rweb_cashtags_enabled": true,
  "profile_label_improvements_pcf_label_in_post_enabled": true,
  "responsive_web_profile_redirect_enabled": false,
  "rweb_tipjar_consumption_enabled": false,
  "verified_phone_label_enabled": false,
  "creator_subscriptions_tweet_preview_api_enabled": true,
  "responsive_web_graphql_timeline_navigation_enabled": true,
  "responsive_web_graphql_skip_user_profile_image_extensions_enabled": false,
  "premium_content_api_read_enabled": false,
  "communities_web_enable_tweet_community_results_fetch": true,
  "c9s_tweet_anatomy_moderator_badge_enabled": true,
  "c9s_list_members_action_api_enabled": false,
  "c9s_superc9s_indication_enabled": false,
  "responsive_web_grok_analyze_button_fetch_trends_enabled": false,
  "responsive_web_grok_analyze_post_followups_enabled": true,
  "rweb_cashtags_composer_attachment_enabled": true,
  "responsive_web_jetfuel_frame": true,
  "responsive_web_grok_share_attachment_enabled": true,
  "responsive_web_grok_annotations_enabled": true,
  "articles_preview_enabled": true,
  "responsive_web_edit_tweet_api_enabled": true,
  "rweb_conversational_replies_downvote_enabled": false,
  "graphql_is_translatable_rweb_tweet_is_translatable_enabled": true,
  "view_counts_everywhere_api_enabled": true,
  "longform_notetweets_consumption_enabled": true,
  "responsive_web_twitter_article_tweet_consumption_enabled": true,
  "content_disclosure_indicator_enabled": true,
  "content_disclosure_ai_generated_indicator_enabled": true,
  "responsive_web_grok_show_grok_translated_post": true,
  "responsive_web_grok_analysis_button_from_backend": true,
  "post_ctas_fetch_enabled": true,
  "freedom_of_speech_not_reach_fetch_enabled": true,
  "standardized_nudges_misinfo": true,
  "tweet_with_visibility_results_prefer_gql_limited_actions_policy_enabled": true,
  "longform_notetweets_rich_text_read_enabled": true,
  "longform_notetweets_inline_media_enabled": false,
  "responsive_web_grok_image_annotation_enabled": true,
  "responsive_web_grok_imagine_annotation_enabled": true,
  "responsive_web_grok_community_note_auto_translation_is_enabled": true,
  "responsive_web_grok_community_note_translation_is_enabled": true,
  "responsive_web_enhance_cards_enabled": false
}`)

	tweetVars = compact(`{
  "postId": "$1",
  $2
  "ranking_mode": "$3",
  "includeHasBirdwatchNotes": false,
  "includePromotedContent": false,
  "withBirdwatchNotes": true,
  "withVoice": false,
  "withV2Timeline": true
}`)

	tweetDetailVars = compact(`{
  "focalTweetId": "$1",
  $2
  "referrer": "profile",
  "with_rux_injections": false,
  "rankingMode": "$3",
  "includePromotedContent": true,
  "withCommunity": true,
  "withQuickPromoteEligibilityTweetFields": true,
  "withBirdwatchNotes": true,
  "withVoice": true
}`)

	tweetEditHistoryVars = compact(`{
  "tweetId": "$1",
  "withQuickPromoteEligibilityTweetFields": true
}`)

	restIdVars = compact(`{
  "rest_id": "$1", $2
  "count": $3
}`)

	userMediaVars = compact(`{
  "userId": "$1", $2
  "count": $3,
  "includePromotedContent": false,
  "withClientEventToken": false,
  "withBirdwatchNotes": false,
  "withVoice": true
}`)

	userTweetsVars = compact(`{
  "userId": "$1", $2
  "count": 20,
  "includePromotedContent": false,
  "withQuickPromoteEligibilityTweetFields": true,
  "withBirdwatchNotes": true,
  "withVoice": true
}`)

	userTweetsAndRepliesVars = compact(`{
  "userId": "$1", $2
  "count": 20,
  "includePromotedContent": false,
  "withCommunity": true,
  "withBirdwatchNotes": true,
  "withVoice": true
}`)

	userRepliesVars = compact(`{
  "userId": "$1", $2
  "count": 20,
  "includePromotedContent": false,
  "withCommunity": true,
  "withBirdwatchNotes": true,
  "withVoice": true
}`)

	userArticlesVars = compact(`{
  "userId": "$1", $2
  "count": 20,
  "includePromotedContent": false,
  "withVoice": true
}`)

	userRepostsVars = compact(`{
  "userId": "$1", $2
  "count": 20,
  "includePromotedContent": false,
  "withVoice": true
}`)

	reactorsVars = compact(`{
  "tweetId": "$1", $2
  "count": 20,
  "enableRanking": true,
  "includePromotedContent": true
}`)

	birdwatchNotesVars = compact(`{
  "tweet_id": "$1"
}`)

	birdwatchOneNoteVars = compact(`{
  "note_id": "$1"
}`)

	birdwatchGlobalTimelineVars = compact(`{
  "count": 200
}`)

	genericTimelineVars = compact(`{
  "timelineId": "$1",
  "count": 200,
  "withQuickPromoteEligibilityTweetFields": true
}`)

	articleVars = compact(`{
  "tweetId": "$1",
  "includePromotedContent": false,
  "withBirdwatchNotes": true,
  "withVoice": true,
  "withCommunity": true
}`)

	articleBatchVars = compact(`{
  "tweetIds": $1,
  "includePromotedContent": false,
  "withBirdwatchNotes": true,
  "withVoice": true,
  "withCommunity": true
}`)

	tweetByRestIdVars = compact(`{
  "tweetId": "$1",
  "includePromotedContent": false,
  "withBirdwatchNotes": false,
  "withVoice": false,
  "withCommunity": false
}`)

	tweetTransVars = compact(`{
  "tweetId": "$1",
  "includePromotedContent": true,
  "withBirdwatchNotes": true,
  "withVoice": true,
  "withCommunity": true
}`)

	userByRestIdVars = compact(`{
  "userId": "$1",
  "withSafetyModeUserFields": true
}`)

	communityTweetsVars = compact(`{
  "communityId": "$1", $2
  "count": 20,
  "displayLocation": "Community",
  "rankingMode": "$3",
  "withCommunity": true
}`)

	communityMediaVars = compact(`{
  "communityId": "$1", $2
  "count": 20,
  "withCommunity": true
}`)

	communityMembersVars = compact(`{
  "communityId": "$1",
  "cursor": $2
}`)

	communityHashtagsVars = compact(`{
  "communityId": "$1", $2
  "count": 20,
  "hashtags": ["$3"],
  "withCommunity": true
}`)
)
