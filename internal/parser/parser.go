// SPDX-License-Identifier: AGPL-3.0-only

// Package parser turns Twitter GraphQL responses into domain types.
package parser

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"

	"github.com/perennialtech/nitter/internal/types"
	"github.com/perennialtech/nitter/internal/utils"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// newTweet is Nim's Tweet(): the zero tweet, whose user has Nim's default
// verified type.
func newTweet() *types.Tweet {
	return &types.Tweet{User: emptyUser()}
}

func emptyUser() types.User { return types.User{VerifiedType: types.VerifiedNone} }

func parseVerifiedType(s string, current types.VerifiedType) types.VerifiedType {
	return parseEnum(s, verifiedTypes, current)
}

func parseCommunityNote(js gjson.Result) string {
	subtitle := get(js, "subtitle")
	return expandBirdwatchEntities(str(get(subtitle, "text")), get(subtitle, "entities"))
}

type noteTrans struct {
	translatable, flagged bool
	text, lang            string
}

func parseNoteTrans(note gjson.Result) noteTrans {
	var result noteTrans
	result.flagged = getBool(get(note, "is_community_note_translatable"))
	trans := get(note, "grok_translated_community_note_with_availability")
	if notNull(trans) {
		if !boolOr(get(trans, "is_available"), true) {
			return result
		}
		result.translatable = true
		data := get(trans, "data")
		if boolOr(get(data, "translation_available"), true) {
			if text := str(get(data, "translation")); text != "" {
				result.text = expandNoteTranslation(text, get(data, "rich_text_entities"))
				result.lang = str(get(data, "source_language"))
			}
		}
	} else if getBool(get(note, "is_community_note_translatable")) {
		result.translatable = true
	}
	return result
}

func parseNoteTranslation(note gjson.Result, tweet *types.Tweet) {
	trans := parseNoteTrans(note)
	tweet.NoteID = str(get(note, "rest_id"))
	tweet.NoteTranslatable = trans.translatable
	tweet.NoteIsTranslatable = trans.flagged
	tweet.NoteTranslation = trans.text
	tweet.NoteTransLang = trans.lang
}

func parseUser(js gjson.Result, id string) types.User {
	if isNull(js) {
		return emptyUser()
	}
	if id == "" {
		id = str(get(js, "id_str"))
	}
	result := types.User{
		ID:           id,
		Username:     str(get(js, "screen_name")),
		Fullname:     str(get(js, "name")),
		Location:     str(get(js, "location")),
		Bio:          str(get(js, "description")),
		UserPic:      strings.ReplaceAll(getImageStr(get(js, "profile_image_url_https")), "_normal", ""),
		Banner:       getBanner(js),
		Following:    getInt(get(js, "friends_count")),
		Followers:    getInt(get(js, "followers_count")),
		Tweets:       getInt(get(js, "statuses_count")),
		Likes:        getInt(get(js, "favourites_count")),
		Media:        getInt(get(js, "media_count")),
		Protected:    boolOr(get(js, "protected"), getBool(get(js, "privacy.protected"))),
		JoinDate:     GetTime(get(js, "created_at")),
		VerifiedType: types.VerifiedNone,
	}

	if boolOr(get(js, "is_blue_verified"), false) {
		result.VerifiedType = types.VerifiedBlue
	}

	if vt := get(js, "verified_type"); notNull(vt) {
		result.VerifiedType = parseVerifiedType(str(vt), result.VerifiedType)
	}

	expandUserEntities(&result, js)
	return result
}

// parseGraphUserNode is parser.nim's internal parseGraphUser, for user
// objects embedded in other responses.
func parseGraphUserNode(js gjson.Result) types.User {
	user := get(js, "user_result.result")
	if isNull(user) {
		user = get(js, "user_results.result")
	}

	if isNull(user) {
		if notNull(get(js, "core")) {
			user = js
		} else {
			return emptyUser()
		}
	}

	result := parseUser(get(user, "legacy"), str(get(user, "rest_id")))

	if result.VerifiedType == types.VerifiedNone && boolOr(get(user, "is_blue_verified"), false) {
		result.VerifiedType = types.VerifiedBlue
	}

	// fallback to support UserMedia/recent GraphQL updates
	if result.Username == "" {
		result.ID = str(get(user, "rest_id"))
		result.Username = str(get(user, "core.screen_name"))
		result.Fullname = str(get(user, "core.name"))
		result.UserPic = strings.ReplaceAll(getImageStr(get(user, "avatar.image_url")), "_normal", "")

		if boolOr(get(user, "is_blue_verified"),
			boolOr(get(user, "verification.is_blue_verified"), false)) {
			result.VerifiedType = types.VerifiedBlue
		}

		if vt := get(user, "verification.verified_type"); notNull(vt) {
			result.VerifiedType = parseVerifiedType(str(vt), result.VerifiedType)
		}
	}

	// "Parody", "Commentary" or "Fan"; "None" (or missing) when not flagged
	if label := str(get(user, "parody_commentary_fan_label")); label != "" && strings.ToLower(label) != "none" {
		result.ParodyLabel = label
	}
	return result
}

func ParseAboutAccount(js gjson.Result) types.AccountInfo {
	result := types.AccountInfo{VerifiedType: types.VerifiedNone}
	user := get(js, "data.user_result_by_screen_name.result")
	if isNull(user) {
		return result
	}

	if str(get(user, "unavailable_reason")) == "Suspended" {
		result.Suspended = true
		return result
	}

	result = types.AccountInfo{
		Username:       str(get(user, "core.screen_name")),
		Fullname:       str(get(user, "core.name")),
		JoinDate:       GetTime(get(user, "core.created_at")),
		UserPic:        strings.ReplaceAll(getImageStr(get(user, "avatar.image_url")), "_normal", ""),
		AffiliateLabel: str(get(user, "identity_profile_labels_highlighted_label.label.description")),
		VerifiedType:   types.VerifiedNone,
	}

	if boolOr(get(user, "is_blue_verified"), false) {
		result.VerifiedType = types.VerifiedBlue
	}
	if vt := get(user, "verification.verified_type"); notNull(vt) {
		result.VerifiedType = parseVerifiedType(str(vt), result.VerifiedType)
	}

	if about := get(user, "about_profile"); notNull(about) {
		result.BasedIn = str(get(about, "account_based_in"))
		result.Source = str(get(about, "source"))
		result.AffiliateUsername = str(get(about, "affiliate_username"))

		if n, ok := nimParseInt(strOr(get(about, "username_changes.count"), "0")); ok {
			result.UsernameChanges = int(n)
		}

		if lastChange := get(about, "username_changes.last_changed_at_msec"); notNull(lastChange) {
			result.LastUsernameChange = getTimeFromMsStr(lastChange)
		}
	}

	if info := get(user, "verification_info"); notNull(info) {
		result.IsIdentityVerified = getBool(get(info, "is_identity_verified"))
		if reason := get(info, "reason"); notNull(reason) {
			result.OverrideVerifiedYear = getInt(get(reason, "override_verified_year"))
			if since := get(reason, "verified_since_msec"); notNull(since) {
				result.VerifiedSince = getTimeFromMsStr(since)
			}
		}
	}
	return result
}

func ParseBroadcastInfo(js gjson.Result) types.Broadcast {
	bc := get(js, "data.broadcast")
	if isNull(bc) {
		return types.Broadcast{User: emptyUser()}
	}
	return types.Broadcast{
		ID:                 str(get(bc, "broadcast_id")),
		Title:              str(get(bc, "status")),
		State:              strings.ToUpper(str(get(bc, "state"))),
		Thumb:              str(get(bc, "image_url")),
		MediaKey:           str(get(bc, "media_key")),
		TotalWatched:       getInt(get(bc, "total_watched")),
		StartTime:          getTimeFromMs(get(bc, "start_time")),
		EndTime:            getTimeFromMs(get(bc, "end_time")),
		ReplayStart:        getInt(get(bc, "edited_replay.start_time")),
		AvailableForReplay: getBool(get(bc, "available_for_replay")),
		User:               parseGraphUserNode(bc),
	}
}

func parseSpaceParticipant(js gjson.Result) types.SpaceParticipant {
	return types.SpaceParticipant{
		UserID:      str(get(js, "user_results.rest_id")),
		Username:    str(get(js, "twitter_screen_name")),
		DisplayName: str(get(js, "display_name")),
		AvatarURL:   str(get(js, "avatar_url")),
		IsVerified: getBool(get(js, "is_verified")) ||
			getBool(get(js, "user_results.result.is_blue_verified")),
	}
}

func ParseAudioSpace(js gjson.Result) types.AudioSpace {
	space := get(js, "data.audioSpace")
	if isNull(space) {
		return types.AudioSpace{Creator: emptyUser()}
	}
	meta := get(space, "metadata")

	result := types.AudioSpace{
		ID:                 str(get(meta, "rest_id")),
		Title:              str(get(meta, "title")),
		State:              strings.ToUpper(str(get(meta, "state"))),
		MediaKey:           str(get(meta, "media_key")),
		TotalLiveListeners: getInt(get(meta, "total_live_listeners")),
		TotalReplayWatched: getInt(get(meta, "total_replay_watched")),
		AvailableForReplay: getBool(get(meta, "is_space_available_for_replay")),
	}

	if startedAt := intOr(get(meta, "started_at"), 0); startedAt > 0 {
		result.StartTime = fromUnixMs(int64(startedAt))
	}

	if endedAtStr := str(get(meta, "ended_at")); endedAtStr != "" {
		if endedAt, ok := nimParseInt(endedAtStr); ok && endedAt > 0 {
			result.EndTime = fromUnixMs(endedAt)
		}
	}

	result.Creator = parseGraphUserNode(get(meta, "creator_results.result"))

	for _, admin := range arr(get(space, "participants.admins")) {
		result.Admins = append(result.Admins, parseSpaceParticipant(admin))
	}

	for _, speaker := range arr(get(space, "participants.speakers")) {
		result.Speakers = append(result.Speakers, parseSpaceParticipant(speaker))
	}
	return result
}

func ParseGraphCommunity(js gjson.Result) types.Community {
	c := get(js, "data.communityResults.result")
	if isNull(c) {
		return types.Community{Creator: emptyUser()}
	}

	result := types.Community{
		ID:          strOr(get(c, "rest_id"), str(get(c, "id_str"))),
		Name:        str(get(c, "name")),
		Description: str(get(c, "description")),
		MemberCount: getInt(get(c, "member_count")),
		JoinPolicy:  str(get(c, "join_policy")),
		Category:    str(get(c, "primary_community_topic.topic_name")),
		Banner:      getImageStr(get(c, "custom_banner_media.media_info.original_img_url")),
		Creator:     parseGraphUserNode(get(c, "creator_results.result")),
	}

	if createdMs := intOr(get(c, "created_at"), 0); createdMs > 0 {
		result.CreatedAt = fromUnixMs(int64(createdMs))
	}

	for _, rule := range arr(get(c, "rules")) {
		result.Rules = append(result.Rules, types.CommunityRule{
			Name:        str(get(rule, "name")),
			Description: str(get(rule, "description")),
		})
	}

	for _, item := range arr(get(c, "trending_hashtags_slice.items")) {
		if tag := str(get(item, "hashtag")); tag != "" {
			result.Hashtags = append(result.Hashtags, tag)
		}
	}
	return result
}

func parseListObject(js gjson.Result, owner types.User) types.List {
	return types.List{
		ID:          str(get(js, "id_str")),
		Name:        str(get(js, "name")),
		Username:    owner.Username,
		UserID:      owner.ID,
		Description: str(get(js, "description")),
		Members:     getInt(get(js, "member_count")),
		Banner: getImageStr(sel(
			get(js, "custom_banner_media.media_info.original_img_url"),
			get(js, "default_banner_media.media_info.original_img_url"),
		)),
	}
}

func ParseGraphList(js gjson.Result) types.List {
	list := get(js, "data.user_by_screen_name.list")
	if isNull(list) {
		list = get(js, "data.list")
	}
	if isNull(list) {
		return types.List{}
	}
	return parseListObject(list, parseGraphUserNode(list))
}

func parseGraphSearchList(js gjson.Result) types.ListSearchResult {
	owner := parseGraphUserNode(js)
	result := types.ListSearchResult{
		List:             parseListObject(js, owner),
		Owner:            owner,
		FollowersContext: str(get(js, "followers_context")),
	}
	for _, url := range arr(get(js, "facepile_urls")) {
		result.Facepiles = append(result.Facepiles, str(url))
	}
	return result
}

func parsePoll(js gjson.Result) *types.Poll {
	result := &types.Poll{}
	vals := get(js, "binding_values")
	// name format is pollNchoice_*
	if name := str(get(js, "name")); len(name) > 4 {
		for i := int('1'); i <= int(name[4]); i++ {
			choice := "choice" + string([]byte{byte(i)})
			n, _ := nimParseInt(getStrVal(get(vals, choice+"_count"), "0"))
			result.Values = append(result.Values, int(n))
			result.Options = append(result.Options, getStrVal(get(vals, choice+"_label"), ""))
		}
	}

	if end := getDateTime(get(vals, "end_datetime_utc.string_value")); end.After(time.Now()) {
		result.Status = durationLead(time.Until(end))
	} else {
		result.Status = "Final results"
	}

	if len(result.Values) > 0 {
		best := result.Values[0]
		for i, v := range result.Values {
			result.Votes += v
			if v > best {
				best, result.Leader = v, i
			}
		}
	}
	return result
}

// durationLead is the part of Nim's $Duration before the first comma: the
// largest non-zero unit, such as "2 days" or "1 hour".
func durationLead(d time.Duration) string {
	units := []struct {
		name string
		size time.Duration
	}{
		{"week", 7 * 24 * time.Hour}, {"day", 24 * time.Hour}, {"hour", time.Hour},
		{"minute", time.Minute}, {"second", time.Second}, {"millisecond", time.Millisecond},
		{"microsecond", time.Microsecond}, {"nanosecond", time.Nanosecond},
	}
	for _, u := range units {
		if n := int64(d / u.size); n != 0 {
			if n == 1 || n == -1 {
				return itoa(n) + " " + u.name
			}
			return itoa(n) + " " + u.name + "s"
		}
	}
	return "0 nanoseconds"
}

func parseVideoVariants(variants gjson.Result) []types.VideoVariant {
	result := []types.VideoVariant{}
	for _, v := range arr(variants) {
		url := str(get(v, "url"))
		contentType := parseVideoType(strOr(get(v, "content_type"), "video/mp4"))
		variant := types.VideoVariant{
			ContentType: contentType,
			Bitrate:     intOr(get(v, "bit_rate"), intOr(get(v, "bitrate"), 0)),
			URL:         url,
		}
		if contentType == types.VideoMp4 {
			variant.Resolution = GetMp4Resolution(url)
		}
		result = append(result, variant)
	}
	return result
}

func parseVideo(js gjson.Result) types.Video {
	result := types.Video{
		Thumb:        getImageStr(get(js, "media_url_https")),
		Available:    true,
		Title:        str(get(js, "ext_alt_text")),
		DurationMs:   getInt(get(js, "video_info.duration_millis")),
		PlaybackType: types.VideoM3u8,
	}

	if status := get(js, "ext_media_availability.status"); notNull(status) {
		if s := str(status); s != "" && strings.ToLower(s) != "available" {
			result.Available = false
		}
	}

	if title := get(js, "additional_media_info.title"); notNull(title) {
		result.Title = str(title)
	}

	if description := get(js, "additional_media_info.description"); notNull(description) {
		result.Description = str(description)
	}

	result.Variants = parseVideoVariants(get(js, "video_info.variants"))
	return result
}

func photoMedia(p types.Photo) types.Media { return types.Media{Kind: types.PhotoMedia, Photo: p} }
func videoMedia(v types.Video) types.Media { return types.Media{Kind: types.VideoMedia, Video: v} }
func gifMedia(g types.Gif) types.Media     { return types.Media{Kind: types.GifMedia, Gif: g} }

// attributionPath is the path of a video's expanded URL without its
// /video/1 suffix, which links to the tweet the video came from.
func attributionPath(expanded string) string {
	return strings.ReplaceAll(parseNimURI(expanded).path, "/video/1", "")
}

func parseLegacyMediaEntities(js gjson.Result, result *types.Tweet) {
	for _, m := range arr(get(js, "extended_entities.media")) {
		switch typeName(m) {
		case "photo":
			result.Media = append(result.Media, photoMedia(types.Photo{
				URL:     getImageStr(get(m, "media_url_https")),
				AltText: str(get(m, "ext_alt_text")),
			}))
		case "video":
			result.Media = append(result.Media, videoMedia(parseVideo(m)))
			if user := get(m, "additional_media_info.source_user"); notNull(user) {
				var u types.User
				if getInt(get(user, "id")) > 0 {
					u = parseUser(user, "")
				} else {
					u = parseGraphUserNode(user)
				}
				result.Attribution = &u
				// Set attribution link from expanded_url (strip /video/N suffix)
				if expanded := str(get(m, "expanded_url")); expanded != "" {
					result.AttributionLink = attributionPath(expanded)
				}
			}
		case "animated_gif":
			result.Media = append(result.Media, gifMedia(types.Gif{
				URL:     getImageStr(get(idx(get(m, "video_info.variants"), 0), "url")),
				Thumb:   getImageStr(get(m, "media_url_https")),
				AltText: str(get(m, "ext_alt_text")),
			}))
		}
	}
}

func parseMediaEntities(js gjson.Result, result *types.Tweet) {
	mediaEntities := get(js, "media_entities")
	if isNull(mediaEntities) {
		return
	}
	var parsedMedia []types.Media
	for _, mediaEntity := range arr(mediaEntities) {
		mediaInfo := get(mediaEntity, "media_results.result.media_info")
		if isNull(mediaInfo) {
			continue
		}
		switch typeName(mediaInfo) {
		case "ApiImage":
			parsedMedia = append(parsedMedia, photoMedia(types.Photo{
				URL:     getImageStr(get(mediaInfo, "original_img_url")),
				AltText: str(get(mediaInfo, "alt_text")),
			}))
		case "ApiVideo":
			status := get(mediaEntity, "media_results.result.media_availability_v2.status")
			parsedMedia = append(parsedMedia, videoMedia(types.Video{
				Available:    str(status) == "Available",
				Thumb:        getImageStr(get(mediaInfo, "preview_image.original_img_url")),
				Title:        str(get(mediaInfo, "alt_text")),
				DurationMs:   getInt(get(mediaInfo, "duration_millis")),
				Variants:     parseVideoVariants(get(mediaInfo, "variants")),
				PlaybackType: types.VideoM3u8,
			}))

			// Parse source user for video attribution
			if sourceUser := get(mediaEntity, "source_user_results.result"); notNull(sourceUser) && result.Attribution == nil {
				if expanded := str(get(mediaEntity, "expanded_url")); expanded != "" {
					result.AttributionLink = attributionPath(expanded)
				}
				result.Attribution = &types.User{
					ID:           str(get(sourceUser, "rest_id")),
					Fullname:     str(get(sourceUser, "core.name")),
					UserPic:      strings.ReplaceAll(getImageStr(get(sourceUser, "avatar.image_url")), "_normal", ""),
					VerifiedType: types.VerifiedNone,
				}
			}
		case "ApiGif":
			parsedMedia = append(parsedMedia, gifMedia(types.Gif{
				URL:     getImageStr(get(idx(get(mediaInfo, "variants"), 0), "url")),
				Thumb:   getImageStr(get(mediaInfo, "preview_image.original_img_url")),
				AltText: str(get(mediaInfo, "alt_text")),
			}))
		}
	}

	if n := length(mediaEntities); n > 0 && len(parsedMedia) == n {
		result.Media = parsedMedia
	}
}

func parsePromoVideo(js gjson.Result) types.Video {
	secs, _ := nimParseInt(getStrVal(get(js, "content_duration_seconds"), "0"))
	result := types.Video{
		Thumb:        getImageVal(get(js, "player_image_large")),
		Available:    true,
		DurationMs:   int(secs) * 1000,
		PlaybackType: types.VideoVmap,
	}

	variant := types.VideoVariant{
		ContentType: types.VideoVmap,
		URL: getStrVal(get(js, "player_hls_url"), getStrVal(get(js, "player_stream_url"),
			getStrVal(get(js, "amplify_url_vmap"), ""))),
	}

	if strings.Contains(variant.URL, "m3u8") {
		variant.ContentType = types.VideoM3u8
		result.PlaybackType = types.VideoM3u8
	}

	result.Variants = append(result.Variants, variant)
	return result
}

func parseBroadcast(js gjson.Result) types.Card {
	image := getImageVal(get(js, "broadcast_thumbnail_large"))
	broadcastURL := getStrVal(get(js, "broadcast_url"), "")
	broadcastID := broadcastURL[strings.LastIndexByte(broadcastURL, '/')+1:]
	streamURL := "/i/broadcasts/" + broadcastID + "/stream"
	return types.Card{
		Kind:  types.CardBroadcast,
		URL:   "/i/broadcasts/" + broadcastID,
		Title: getStrVal(get(js, "broadcaster_display_name"), ""),
		Text:  getStrVal(get(js, "broadcast_title"), ""),
		Image: image,
		Video: &types.Video{
			Thumb:        image,
			Available:    true,
			PlaybackType: types.VideoM3u8,
			Variants:     []types.VideoVariant{{ContentType: types.VideoM3u8, URL: streamURL}},
		},
	}
}

var cardImageTypes = []string{"summary_photo_image", "player_image", "promo_image",
	"photo_image_full_size", "thumbnail_image", "thumbnail", "event_thumbnail", "image"}

func parseCard(js, urls gjson.Result) types.Card {
	vals := get(js, "binding_values")
	if isNull(vals) {
		return types.Card{Kind: types.CardAmplify}
	}
	name := str(get(js, "name"))
	kind := parseEnum(name[strings.IndexByte(name, ':')+1:], cardKinds, types.CardUnknown)

	if kind == types.CardUnified {
		return ParseUnifiedCard(gjson.Parse(str(get(vals, "unified_card.string_value"))))
	}

	result := types.Card{
		Kind:  kind,
		URL:   getCardURL(vals, kind),
		Dest:  getCardDomain(vals, kind),
		Title: getCardTitle(vals, kind),
		Text:  getStrVal(get(vals, "description"), ""),
	}

	if result.URL == "" {
		result.URL = str(get(js, "url"))
	}

	switch kind {
	case types.CardPromoVideo, types.CardPromoVideoConvo, types.CardAppPlayer, types.CardVideoDirectMessage:
		v := parsePromoVideo(vals)
		result.Video = &v
		if kind == types.CardAppPlayer {
			result.Text = getStrVal(get(vals, "app_category"), result.Text)
		}
	case types.CardBroadcast:
		result = parseBroadcast(vals)
	case types.CardLiveEvent:
		result.Text = getStrVal(get(vals, "event_title"), "")
	case types.CardPlayer:
		result.URL = getStrVal(get(vals, "player_url"), "")
		if strings.Contains(result.URL, "youtube.com") {
			result.URL = strings.ReplaceAll(result.URL, "/embed/", "/watch?v=")
		}
	case types.CardAudiospace:
		if spaceID := getStrVal(get(vals, "id"), ""); spaceID != "" {
			result.URL = "/i/spaces/" + spaceID
			result.Title = "Twitter Space"
			result.Text = "Click to view Space"
		}
	case types.CardUnknown:
		result.Title = "This card type is not supported."
	}

	for _, typ := range cardImageTypes {
		if img := get(vals, typ+"_large"); notNull(img) {
			result.Image = getImageVal(img)
			break
		}
	}

	// Nim's `for u in ? urls` returns the card as it is when urls is null.
	if isNull(urls) {
		return result
	}
	for _, u := range arr(urls) {
		if str(get(u, "url")) == result.URL {
			result.URL = getExpandedURL(u, result.URL)
			break
		}
	}

	if kind == types.CardVideoDirectMessage || kind == types.CardImageDirectMessage {
		result.URL = ""
	}

	if (kind == types.CardPromoImageConvo || kind == types.CardPromoImageApp ||
		kind == types.CardImageDirectMessage) && result.URL == "" ||
		strings.HasPrefix(result.URL, "card://") {
		result.URL = utils.PicURL(result.Image)
	}
	return result
}

// parseTweetCard handles a tweet's card, which may be a poll, an amplify
// video or an ordinary card.
func parseTweetCard(result *types.Tweet, jsCard, urls gjson.Result) {
	if isNull(jsCard) {
		return
	}
	name := str(get(jsCard, "name"))
	switch {
	case strings.Contains(name, "poll"):
		if strings.Contains(name, "image") {
			result.Media = append(result.Media, photoMedia(types.Photo{
				URL: getImageVal(get(jsCard, "binding_values.image_large")),
			}))
		}
		result.Poll = parsePoll(jsCard)
	case name == "amplify":
		result.Media = append(result.Media, videoMedia(parsePromoVideo(get(jsCard, "binding_values"))))
	case name != "" && notNull(get(jsCard, "binding_values")):
		card := parseCard(jsCard, urls)
		result.Card = &card
	}
}

func parseTweet(js, jsCard gjson.Result, replyID int64, hasArticle bool) *types.Tweet {
	if isNull(js) {
		return newTweet()
	}

	var t time.Time
	if notNull(get(js, "created_at")) {
		t = GetTime(get(js, "created_at"))
	} else {
		t = getTimeFromMs(get(js, "created_at_ms"))
	}

	result := &types.Tweet{
		ID:        GetID(get(js, "id_str")),
		ThreadID:  GetID(get(js, "conversation_id_str")),
		ReplyID:   GetID(get(js, "in_reply_to_status_id_str")),
		Text:      str(get(js, "full_text")),
		Time:      t,
		HasThread: notNull(get(js, "self_thread")),
		Available: true,
		User:      types.User{ID: str(get(js, "user_id_str")), VerifiedType: types.VerifiedNone},
		ExpURL:    str(get(js, "expanded")),
		Stats: types.TweetStats{
			Replies:  getInt(get(js, "reply_count")),
			Retweets: getInt(get(js, "retweet_count")),
			Quotes:   getInt(get(js, "quote_count")),
			Likes:    getInt(get(js, "favorite_count")),
			Views:    getInt(get(js, "views_count")),
			Source:   str(get(js, "source")),
		},
	}

	if result.ReplyID == 0 {
		result.ReplyID = replyID
	}

	// fix for pinned threads
	if result.HasThread && result.ThreadID == 0 {
		result.ThreadID = GetID(get(js, "self_thread.id_str"))
	}

	if has(js, "retweeted_status") {
		result.Retweet = newTweet()
	} else if getBool(get(js, "is_quote_status")) {
		q := newTweet()
		q.ID = GetID(get(js, "quoted_status_id_str"))
		result.Quote = q
	}

	// legacy
	if rt := get(js, "retweeted_status_id_str"); notNull(rt) {
		r := newTweet()
		r.ID = GetID(rt)
		result.Retweet = r
		return result
	}

	// graphql
	if rt := get(js, "retweeted_status_result.result"); notNull(rt) {
		// needed due to weird edgecase where the actual tweet data isn't included
		if has(rt, "legacy") || has(rt, "rest_id") {
			result.Retweet = ParseGraphTweet(rt)
			return result
		}
	}

	if reposts := get(js, "repostedStatusResults"); notNull(reposts) {
		if rt := get(reposts, "result"); notNull(rt) {
			if has(rt, "legacy") || has(rt, "rest_id") {
				result.Retweet = ParseGraphTweet(rt)
				return result
			}
		}
	}

	parseTweetCard(result, jsCard, get(js, "entities.urls"))

	expandTweetEntities(result, js, hasArticle)
	parseLegacyMediaEntities(js, result)

	if jsWithheld := get(js, "withheld_in_countries"); notNull(jsWithheld) {
		var withheldInCountries []string
		for _, c := range arr(jsWithheld) {
			withheldInCountries = append(withheldInCountries, str(c))
		}

		// XX - Content is withheld in all countries
		// XY - Content is withheld due to a DMCA request.
		if getBool(get(js, "withheld_copyright")) ||
			len(withheldInCountries) > 0 && (indexOf(withheldInCountries, "XX") >= 0 ||
				indexOf(withheldInCountries, "XY") >= 0 ||
				strings.Contains(result.Text, "withheld")) {
			result.Text = strings.TrimSuffix(result.Text, " Learn more.")
			result.Available = false
		}
	}
	return result
}

// flattenLegacyCard turns a GraphQL card's {"legacy": {"binding_values":
// [{"key", "value"}]}} into the legacy {"binding_values": {key: value}} shape.
func flattenLegacyCard(jsCard gjson.Result) gjson.Result {
	legacyCard := get(jsCard, "legacy")
	if isNull(legacyCard) {
		return jsCard
	}
	bindingArray := get(legacyCard, "binding_values")
	if !bindingArray.IsArray() {
		return jsCard
	}
	raw := func(r gjson.Result) string {
		if r.Raw == "" {
			return "null"
		}
		return r.Raw
	}
	var b strings.Builder
	b.WriteString(`{"name":` + raw(get(legacyCard, "name")) + `,"url":` + raw(get(legacyCard, "url")) +
		`,"binding_values":{`)
	for i, item := range arr(bindingArray) {
		if i > 0 {
			b.WriteByte(',')
		}
		key, _ := json.Marshal(str(get(item, "key")))
		b.Write(key)
		b.WriteByte(':')
		b.WriteString(raw(get(item, "value")))
	}
	b.WriteString("}}")
	return gjson.Parse(b.String())
}

func ParseGraphTweet(js gjson.Result) *types.Tweet {
	if isNull(js) {
		return newTweet()
	}

	switch typeName(js) {
	case "TweetUnavailable":
		return newTweet()
	case "TweetTombstone":
		if text := sel(get(js, "tombstone.richText"), get(js, "tombstone.text")); notNull(text) {
			t := newTweet()
			t.Text = getTombstone(text)
			return t
		}
		return newTweet()
	case "TweetPreviewDisplay":
		t := newTweet()
		t.Text = "You're unable to view this Tweet because it's only available to the Subscribers of the account owner."
		return t
	case "TweetWithVisibilityResults":
		return ParseGraphTweet(get(js, "tweet"))
	}

	if !has(js, "legacy") && !has(js, "rest_id") {
		return newTweet()
	}

	jsCard := flattenLegacyCard(sel(get(js, "card"), get(js, "tweet_card"), get(js, "legacy.tweet_card")))

	var replyID int64
	if restID := get(js, "reply_to_results.rest_id"); notNull(restID) {
		replyID = GetID(restID)
	}

	hasArticle := str(get(js, "article.article_results.result.title")) != ""

	var result *types.Tweet
	if has(js, "details") {
		result = &types.Tweet{
			ID:        GetID(get(js, "rest_id")),
			Available: true,
			Text:      str(get(js, "details.full_text")),
			Time:      getTimeFromMs(get(js, "details.created_at_ms")),
			ReplyID:   GetID(get(js, "reply_to_results.rest_id")),
			IsAd:      getBool(get(js, "content_disclosure.advertising_disclosure.is_paid_promotion")),
			IsAI:      getBool(get(js, "content_disclosure.ai_generated_disclosure.has_ai_generated_media")),
			User:      emptyUser(),
			Stats: types.TweetStats{
				Replies:  getInt(get(js, "counts.reply_count")),
				Retweets: getInt(get(js, "counts.retweet_count")),
				Quotes:   getInt(get(js, "counts.quote_count")),
				Likes:    getInt(get(js, "counts.favorite_count")),
			},
		}

		parseTweetCard(result, jsCard, get(js, "url_entities"))

		parseMediaEntities(js, result)
		if result.Attribution == nil {
			parseLegacyMediaEntities(get(js, "legacy"), result)
		}

		expandTweetEntitiesV2(result, js, hasArticle)

		// Strip video source URL from text (for videos from other tweets)
		for _, m := range arr(get(js, "media_entities")) {
			if has(m, "source_status_id_str") {
				if mediaURL := str(get(m, "url")); mediaURL != "" {
					if i := strings.LastIndex(result.Text, mediaURL); i >= 0 {
						result.Text = strings.Trim(result.Text[:i], " \t\v\r\n\f")
					}
					break
				}
			}
		}
	} else {
		result = parseTweet(get(js, "legacy"), jsCard, replyID, hasArticle)
		result.ID = GetID(get(js, "rest_id"))
	}

	if artNode := get(js, "article.article_results.result"); notNull(artNode) {
		if artTitle := str(get(artNode, "title")); artTitle != "" {
			result.ArticlePreview = &types.ArticlePreview{
				Title:       artTitle,
				PreviewText: str(get(artNode, "preview_text")),
				CoverImage: getImageStr(sel(
					get(artNode, "cover_media_results.result.media_info.original_img_url"),
					get(artNode, "cover_media.media_info.original_img_url"),
				)),
				TweetID: result.ID,
			}
		}
	}

	result.User = parseGraphUserNode(get(js, "core"))

	if len(result.Reply) == 0 {
		if replyTo := get(js, "reply_to_user_results.result.core.screen_name"); notNull(replyTo) {
			result.Reply = []string{str(replyTo)}
		}
	}

	if count := get(js, "views.count"); notNull(count) {
		n, _ := nimParseInt(strOr(count, "0"))
		result.Stats.Views = int(n)
	}

	if source := get(js, "source"); notNull(source) {
		result.Stats.Source = str(source)
	}

	if noteTweet := get(js, "note_tweet.note_tweet_results.result"); notNull(noteTweet) {
		expandNoteTweetEntities(result, noteTweet)
	}

	hasAiDisclosure := getBool(get(js, "content_disclosure.ai_generated_disclosure.has_ai_generated_media"))

	hasGrokPost := false
	for _, media := range arr(get(js, "legacy.entities.media")) {
		if str(get(media, "grok_post_id")) != "" {
			hasGrokPost = true
			break
		}
	}

	result.IsAI = hasAiDisclosure || hasGrokPost

	parseMediaEntities(js, result)

	// Hide card if it's redundant with attribution (same video shown via embed)
	if result.Attribution != nil && result.Card != nil {
		if utils.IsTwitterURL(result.Card.URL) {
			cardPath := attributionPath(result.Card.URL)
			if cardPath != "" && cardPath == result.AttributionLink {
				result.Card.Kind = types.CardHidden
			}
		}
	}

	if reposts := get(js, "legacy.repostedStatusResults"); notNull(reposts) {
		if rt := get(reposts, "result"); notNull(rt) {
			if has(rt, "legacy") || has(rt, "rest_id") {
				result.Retweet = ParseGraphTweet(rt)
			}
		}
	}

	if quoted := get(js, "quoted_status_result.result"); notNull(quoted) {
		result.Quote = ParseGraphTweet(quoted)
	}

	if quoted := get(js, "quotedPostResults"); notNull(quoted) {
		if has(quoted, "result") {
			result.Quote = ParseGraphTweet(get(quoted, "result"))
		} else {
			q := newTweet()
			q.ID = GetID(get(js, "legacy.quoted_status_id_str"))
			result.Quote = q
		}
	}

	if result.Quote != nil && !result.Quote.Available {
		if expanded := str(get(js, "legacy.quoted_status_permalink.expanded")); expanded != "" {
			if utils.IsTwitterURL(expanded) {
				result.Quote.ExpURL = utils.LocalizeTwitterLink(expanded)
			}
			if result.Quote.Text == "" {
				if username := utils.ExtractUsername(expanded); username != "" {
					result.Quote.Text = "This post from @" + username + " is unavailable."
				}
			}
		}
	}

	for _, id := range arr(get(js, "edit_control.edit_control_initial.edit_tweet_ids")) {
		n, _ := nimParseInt(str(id))
		result.History = append(result.History, n)
	}

	if birdwatch := get(js, "birdwatch_pivot"); notNull(birdwatch) {
		result.Note = parseCommunityNote(birdwatch)
		parseNoteTranslation(get(birdwatch, "note"), result)
	}

	if trans := get(js, "grok_translated_post_with_availability"); notNull(trans) {
		if boolOr(get(trans, "is_available"), true) {
			result.Translatable = true
			if data := get(trans, "data"); notNull(data) {
				if text := str(get(data, "translation")); text != "" {
					result.Translation = expandTranslation(text, get(data, "entities"))
					result.TransLang = str(get(data, "source_language"))
				}
			}
		}
	}

	result.IsTranslatable = getBool(get(js, "is_translatable"))
	result.HasBirdwatch = getBool(get(js, "has_birdwatch_notes")) || notNull(get(js, "birdwatch_pivot"))
	return result
}

func getConvSection(js gjson.Result) string {
	details := sel(
		get(js, "item.client_event_info.details"),
		get(js, "item.clientEventInfo.details"),
	)
	return str(sel(
		get(details, "conversation_details.conversation_section"),
		get(details, "conversationDetails.conversationSection"),
	))
}

func parseGraphThread(js gjson.Result) (thread types.Chain, self bool) {
	checkedSection := false
	for _, t := range arr(get(js, "content.items")) {
		entryID := entryID(t)
		switch {
		case strings.Contains(entryID, "tweet-") && !strings.Contains(entryID, "promoted"):
			if !checkedSection {
				checkedSection = true
				if getConvSection(t) == "RelatedTweet" {
					thread.Related = true
				}
			}

			if tweet := getTweetResult(t, "item"); notNull(tweet) {
				thread.Content = append(thread.Content, ParseGraphTweet(tweet))

				tweetDisplayType := sel(
					get(t, "item.content.tweet_display_type"),
					get(t, "item.itemContent.tweetDisplayType"),
				)
				if str(tweetDisplayType) == "SelfThread" {
					self = true
				}
			} else {
				placeholder := newTweet()
				placeholder.ID = ParseID(entryID)
				thread.Content = append(thread.Content, placeholder)
			}
		case strings.Contains(entryID, "cursor-showmore"):
			thread.Cursor = str(get(t, "item.content.value"))
			thread.HasMore = true
		}
	}
	return thread, self
}

func ParseGraphTweetResult(js gjson.Result) *types.Tweet {
	if tweet := get(js, "data.tweet_result.result"); notNull(tweet) {
		return ParseGraphTweet(tweet)
	}
	return nil
}

func ParseTweetByRestID(js gjson.Result) *types.Tweet {
	if tweet := get(js, "data.tweetResult.result"); notNull(tweet) {
		return ParseGraphTweet(tweet)
	}
	return nil
}

func ParseGraphTweetResults(js gjson.Result) []*types.Tweet {
	var result []*types.Tweet
	for _, item := range arr(get(js, "data.tweetResult")) {
		tweet := get(item, "result")
		if isNull(tweet) {
			continue
		}
		if t := ParseGraphTweet(tweet); t != nil {
			result = append(result, t)
		}
	}
	return result
}

func ParseGraphConversation(js gjson.Result, tweetID string) *types.Conversation {
	result := &types.Conversation{Replies: types.Result[types.Chain]{Beginning: true}}

	instructions := sel(
		get(js, "data.timelineResponse.instructions"),
		get(js, "data.timeline_response.instructions"),
		get(js, "data.threaded_conversation_with_injections_v2.instructions"),
	)
	if length(instructions) == 0 {
		return result
	}

	for _, i := range arr(instructions) {
		if typeName(i) != "TimelineAddEntries" {
			continue
		}
		for _, e := range arr(get(i, "entries")) {
			entryID := entryID(e)
			switch {
			case strings.HasPrefix(entryID, "tweet-"):
				if tweetResult := getTweetResult(e, "content"); notNull(tweetResult) {
					tweet := ParseGraphTweet(tweetResult)

					if !tweet.Available {
						tweet.ID = ParseID(entryID)
					}

					if strings.HasSuffix(entryID, tweetID) {
						result.Tweet = tweet
					} else {
						result.Before.Content = append(result.Before.Content, tweet)
					}
				} else if !strings.HasSuffix(entryID, tweetID) {
					placeholder := newTweet()
					placeholder.ID = ParseID(entryID)
					result.Before.Content = append(result.Before.Content, placeholder)
				}
			case strings.HasPrefix(entryID, "conversationthread"),
				strings.HasPrefix(entryID, "tweetdetailrelatedtweets"):
				thread, self := parseGraphThread(e)
				if self {
					result.After = thread
				} else if len(thread.Content) > 0 {
					result.Replies.Content = append(result.Replies.Content, thread)
				}
			case strings.HasPrefix(entryID, "tombstone"):
				content := sel(get(e, "content.content"), get(e, "content.itemContent"))
				tweet := newTweet()
				tweet.ID = ParseID(entryID)
				tweet.Text = getTombstone(get(content, "tombstoneInfo.richText"))

				if itoa(tweet.ID) == tweetID {
					result.Tweet = tweet
				} else {
					result.Before.Content = append(result.Before.Content, tweet)
				}
			case strings.HasPrefix(entryID, "cursor-bottom"):
				result.Replies.Bottom = str(sel(
					get(e, "content.value"),
					get(e, "content.content.value"),
					get(e, "content.itemContent.value"),
				))
			}
		}
	}
	return result
}

func ParseGraphEditHistory(js gjson.Result, tweetID string) types.EditHistory {
	var result types.EditHistory
	instructions := get(js, "data.tweet_result_by_rest_id.result.edit_history_timeline.timeline.instructions")
	if length(instructions) == 0 {
		return result
	}

	for _, i := range arr(instructions) {
		if typeName(i) != "TimelineAddEntries" {
			continue
		}
		for _, e := range arr(get(i, "entries")) {
			switch entryID(e) {
			case "latestTweet":
				if item := idx(get(e, "content.items"), 0); notNull(item) {
					if tweetResult := getTweetResult(item, "item"); notNull(tweetResult) {
						result.Latest = ParseGraphTweet(tweetResult)
					}
				}
			case "staleTweets":
				for _, item := range arr(get(e, "content.items")) {
					if tweetResult := getTweetResult(item, "item"); notNull(tweetResult) {
						result.History = append(result.History, ParseGraphTweet(tweetResult))
					}
				}
			}
		}
	}
	return result
}

func extractTweetsFromModuleItems(items gjson.Result) []*types.Tweet {
	var result []*types.Tweet
	for _, item := range arr(items) {
		if tweetResult := getTweetResult(item, "item"); notNull(tweetResult) {
			tweet := ParseGraphTweet(tweetResult)
			if !tweet.Available {
				tweet.ID = ParseID(entryID(item))
			}
			result = append(result, tweet)
		}
	}
	return result
}

func extractListsFromItems(items gjson.Result) []types.ListSearchResult {
	var result []types.ListSearchResult
	for _, item := range arr(items) {
		if listJs := get(item, "item.itemContent.list"); notNull(listJs) {
			if r := parseGraphSearchList(listJs); r.List.ID != "" {
				result = append(result, r)
			}
		}
	}
	return result
}

func extractTweetsFromEntry(e gjson.Result) []*types.Tweet {
	if tweetResult := getTweetResult(e, "content"); notNull(tweetResult) {
		tweet := ParseGraphTweet(tweetResult)
		if !tweet.Available {
			tweet.ID = ParseID(entryID(e))
		}
		return []*types.Tweet{tweet}
	}
	return extractTweetsFromModuleItems(get(e, "content.items"))
}

// addSingles appends each tweet as its own timeline entry.
func addSingles(content []types.Tweets, tweets []*types.Tweet) []types.Tweets {
	for _, t := range tweets {
		content = append(content, types.Tweets{t})
	}
	return content
}

func ParseGraphTimeline(js gjson.Result, after string) types.Profile {
	result := types.Profile{
		User:        emptyUser(),
		Tweets:      types.Timeline{Beginning: after == ""},
		AccountInfo: types.AccountInfo{VerifiedType: types.VerifiedNone},
	}

	instructions := sel(
		get(js, "data.list.timeline_response.timeline.instructions"),
		get(js, "data.user.result.timeline.timeline.instructions"),
		get(js, "data.user_result.result.timeline_response.timeline.instructions"),
	)
	if length(instructions) == 0 {
		return result
	}

	for _, i := range arr(instructions) {
		if moduleItems := get(i, "moduleItems"); notNull(moduleItems) {
			result.Tweets.Content = addSingles(result.Tweets.Content, extractTweetsFromModuleItems(moduleItems))
			continue
		}

		if entries := get(i, "entries"); notNull(entries) {
			for _, e := range arr(entries) {
				entryID := entryID(e)
				switch {
				case strings.HasPrefix(entryID, "tweet"), strings.HasPrefix(entryID, "profile-grid"):
					result.Tweets.Content = addSingles(result.Tweets.Content, extractTweetsFromEntry(e))
				case strings.Contains(entryID, "-conversation-"), strings.HasPrefix(entryID, "homeConversation"):
					thread, _ := parseGraphThread(e)
					result.Tweets.Content = append(result.Tweets.Content, thread.Content)
				case strings.HasPrefix(entryID, "cursor-bottom"):
					result.Tweets.Bottom = str(get(e, "content.value"))
				}
			}
		}

		if after == "" && typeName(i) == "TimelinePinEntry" {
			if tweets := extractTweetsFromEntry(get(i, "entry")); len(tweets) > 0 {
				tweet := tweets[0]
				tweet.Pinned = true
				result.Pinned = tweet
			}
		}
	}
	return result
}

func ParseGraphPhotoRail(js gjson.Result) types.PhotoRail {
	result := types.PhotoRail{}

	instructions := sel(
		get(js, "data.user.result.timeline.timeline.instructions"),
		get(js, "data.user_result.result.timeline_response.timeline.instructions"),
	)
	if length(instructions) == 0 {
		return result
	}

	for _, i := range arr(instructions) {
		if moduleItems := get(i, "moduleItems"); notNull(moduleItems) {
			for _, t := range extractTweetsFromModuleItems(moduleItems) {
				if photo := ExtractGalleryPhoto(t); photo.URL != "" {
					result = append(result, photo)
				}
				if len(result) == 16 {
					return result
				}
			}
			continue
		}

		if typeName(i) != "TimelineAddEntries" {
			continue
		}

		for _, e := range arr(get(i, "entries")) {
			entryID := entryID(e)
			if strings.HasPrefix(entryID, "tweet") || strings.HasPrefix(entryID, "profile-grid") {
				for _, t := range extractTweetsFromEntry(e) {
					if photo := ExtractGalleryPhoto(t); photo.URL != "" {
						result = append(result, photo)
					}
					if len(result) == 16 {
						return result
					}
				}
			}
		}
	}
	return result
}

func ParseGraphRetweetersTimeline(js gjson.Result, after string) types.UsersTimeline {
	result := types.UsersTimeline{Beginning: after == ""}

	instructions := get(js, "data.retweeters_timeline.timeline.instructions")
	if length(instructions) == 0 {
		return result
	}

	for _, instruction := range arr(instructions) {
		if typeName(instruction) != "TimelineAddEntries" {
			continue
		}
		for _, e := range arr(get(instruction, "entries")) {
			switch str(get(e, "content.entryType")) {
			case "TimelineTimelineItem":
				if userRes := get(e, "content.itemContent.user_results.result"); notNull(userRes) {
					result.Content = append(result.Content, parseGraphUserNode(userRes))
				}
			case "TimelineTimelineCursor":
				if str(get(e, "content.cursorType")) == "Bottom" {
					result.Bottom = str(get(e, "content.value"))
				}
			}
		}
	}
	return result
}

func stringList(js gjson.Result) []string {
	var out []string
	for _, t := range arr(js) {
		out = append(out, str(t))
	}
	return out
}

func parseBirdwatchNote(note gjson.Result, classification string, misleading bool) types.BirdwatchNote {
	trans := parseNoteTrans(note)
	return types.BirdwatchNote{
		ID:             str(get(note, "rest_id")),
		Text:           str(get(note, "data_v1.summary.text")),
		Helpful:        str(get(note, "rating_status")) == "CurrentlyRatedHelpful",
		Misleading:     misleading,
		CreatedAt:      getTimeFromMs(get(note, "created_at")),
		AuthorAlias:    str(get(note, "birdwatch_profile.alias")),
		Classification: classification,
		MisleadingTags: stringList(get(note, "data_v1.misleading_tags")),
		HelpfulTags:    stringList(get(note, "helpful_tags")),
		DecidedBy:      str(get(note, "decided_by")),
		Translatable:   trans.translatable,
		IsTranslatable: trans.flagged,
		Translation:    trans.text,
		TransLang:      trans.lang,
	}
}

func ParseBirdwatchNotes(js gjson.Result) types.BirdwatchNotes {
	var result types.BirdwatchNotes
	tweetResult := get(js, "data.tweet_result_by_rest_id.result")
	if isNull(tweetResult) {
		return result
	}

	extractNotes := func(bucket gjson.Result, misleading bool) {
		for _, note := range arr(get(bucket, "notes")) {
			result.Notes = append(result.Notes,
				parseBirdwatchNote(note, str(get(note, "data_v1.classification")), misleading))
		}
	}

	// misleading notes first, then not-misleading, per each bucket's own order
	extractNotes(get(tweetResult, "misleading_birdwatch_notes"), true)
	extractNotes(get(tweetResult, "not_misleading_birdwatch_notes"), false)
	return result
}

func ParseBirdwatchHistory(js gjson.Result) types.BirdwatchHistory {
	var result types.BirdwatchHistory
	profile := get(js, "data.birdwatch_profile_by_alias")
	if isNull(profile) {
		return result
	}

	result.Alias = str(get(profile, "alias"))
	result.Bottom = str(get(profile, "notes_slice.slice_info.next_cursor"))

	for _, note := range arr(get(profile, "notes_slice.notes")) {
		tweetResult := get(note, "tweet_results.result")
		result.Notes = append(result.Notes, types.BirdwatchHistoryNote{
			ID:             str(get(note, "rest_id")),
			Text:           str(get(note, "data_v1.summary.text")),
			Helpful:        str(get(note, "rating_status")) == "CurrentlyRatedHelpful",
			Classification: str(get(note, "data_v1.classification")),
			CreatedAt:      getTimeFromMs(get(note, "created_at")),
			TweetID:        GetID(get(tweetResult, "rest_id")),
			TweetUsername:  str(get(tweetResult, "core.user_results.result.core.screen_name")),
		})
	}
	return result
}

func ParseBirdwatchOneNote(js gjson.Result) types.BirdwatchSingleNote {
	var result types.BirdwatchSingleNote
	note := get(js, "data.birdwatch_note_by_rest_id")
	if isNull(note) {
		return result
	}

	classification := str(get(note, "data_v1.classification"))
	result.Note = parseBirdwatchNote(note, classification,
		classification == "MisinformedOrPotentiallyMisleading")
	result.TweetID = GetID(get(note, "tweet_results.result.rest_id"))
	return result
}

func sortNewestFirst(tl *types.Timeline) {
	newest := func(thread types.Tweets) int64 {
		var n int64
		for _, t := range thread {
			n = max(n, t.ID)
		}
		return n
	}
	nimSort(tl.Content, func(a, b types.Tweets) int {
		x, y := newest(b), newest(a)
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
		return 0
	})
}

// parseNoteTimeline reads the tweet entries of a birdwatch timeline.
func parseNoteTimeline(instructions gjson.Result) types.Timeline {
	result := types.Timeline{Beginning: true}
	if length(instructions) == 0 {
		return result
	}
	for _, i := range arr(instructions) {
		if entries := get(i, "entries"); notNull(entries) {
			for _, e := range arr(entries) {
				if strings.HasPrefix(entryID(e), "tweet") {
					for _, tweet := range extractTweetsFromEntry(e) {
						tweet.HasBirdwatch = true
						result.Content = append(result.Content, types.Tweets{tweet})
					}
				}
			}
		}
	}
	sortNewestFirst(&result)
	return result
}

func ParseBirdwatchGlobalTimeline(js gjson.Result) types.Timeline {
	return parseNoteTimeline(get(js,
		"data.viewer.birdwatch_home_page.body.initialTimeline.timeline.timeline.instructions"))
}

func ParseGenericTweetTimeline(js gjson.Result) types.Timeline {
	return parseNoteTimeline(get(js, "data.timeline.timeline.instructions"))
}

// parseGraphSearch is Nim's generic parseGraphSearch[T]: entry handles one
// TimelineAddEntries entry and module one TimelineAddToModule's items.
func parseGraphSearch[T any](js gjson.Result, after string,
	entry func(e gjson.Result, entryID string) []T, module func(items gjson.Result) []T) types.Result[T] {
	result := types.Result[T]{Beginning: after == ""}

	instructions := sel(
		get(js, "data.search.timeline_response.timeline.instructions"),
		get(js, "data.search_by_raw_query.search_timeline.timeline.instructions"),
	)
	if length(instructions) == 0 {
		return result
	}

	for _, instruction := range arr(instructions) {
		switch typeName(instruction) {
		case "TimelineAddEntries":
			for _, e := range arr(get(instruction, "entries")) {
				entryID := entryID(e)
				result.Content = append(result.Content, entry(e, entryID)...)

				if strings.HasPrefix(entryID, "cursor-bottom") {
					result.Bottom = str(get(e, "content.value"))
				}
			}
		case "TimelineAddToModule":
			if module != nil {
				result.Content = append(result.Content, module(get(instruction, "moduleItems"))...)
			}
		case "TimelineReplaceEntry":
			if strings.HasPrefix(str(get(instruction, "entry_id_to_replace")), "cursor-bottom") {
				result.Bottom = str(get(instruction, "entry.content.value"))
			}
		}
	}
	return result
}

func singles(tweets []*types.Tweet) []types.Tweets { return addSingles(nil, tweets) }

func ParseGraphSearchTweets(js gjson.Result, after string) types.Timeline {
	return parseGraphSearch(js, after,
		func(e gjson.Result, entryID string) []types.Tweets {
			if strings.HasPrefix(entryID, "tweet") || strings.HasPrefix(entryID, "search-grid") {
				return singles(extractTweetsFromEntry(e))
			}
			return nil
		},
		func(items gjson.Result) []types.Tweets {
			return singles(extractTweetsFromModuleItems(items))
		})
}

func ParseGraphSearchUsers(js gjson.Result, after string) types.Result[types.User] {
	return parseGraphSearch(js, after,
		func(e gjson.Result, entryID string) []types.User {
			if strings.HasPrefix(entryID, "user") {
				if userRes := get(e, "content.itemContent"); notNull(userRes) {
					return []types.User{parseGraphUserNode(userRes)}
				}
			}
			return nil
		}, nil)
}

func ParseGraphSearchLists(js gjson.Result, after string) types.Result[types.ListSearchResult] {
	return parseGraphSearch(js, after,
		func(e gjson.Result, entryID string) []types.ListSearchResult {
			if strings.HasPrefix(entryID, "list-search") {
				return extractListsFromItems(get(e, "content.items"))
			}
			return nil
		},
		extractListsFromItems)
}

func ParseGraphCommunityTimeline(js gjson.Result, after string) types.Timeline {
	result := types.Timeline{Beginning: after == ""}

	communityResult := get(js, "data.communityResults.result")
	instructions := sel(
		get(communityResult, "ranked_community_timeline.timeline.instructions"),
		get(communityResult, "community_media_timeline.timeline.instructions"),
		get(communityResult, "community_filtered_timeline.timeline.instructions"),
	)
	if length(instructions) == 0 {
		return result
	}

	for _, i := range arr(instructions) {
		if entries := get(i, "entries"); notNull(entries) {
			for _, e := range arr(entries) {
				entryID := entryID(e)
				switch {
				case strings.HasPrefix(entryID, "tweet"), strings.HasPrefix(entryID, "profile-grid"),
					strings.HasPrefix(entryID, "communities-grid"):
					result.Content = addSingles(result.Content, extractTweetsFromEntry(e))
				case strings.HasPrefix(entryID, "cursor-bottom"):
					result.Bottom = str(get(e, "content.value"))
				}
			}
		}

		if after == "" && typeName(i) == "TimelinePinEntry" {
			tweets := extractTweetsFromEntry(get(i, "entry"))
			for _, tweet := range tweets {
				tweet.Pinned = true
			}
			// Nim's insert puts all pinned tweets in one entry at the top.
			if len(tweets) > 0 {
				result.Content = append([]types.Tweets{tweets}, result.Content...)
			}
		}
	}
	return result
}

func ParseGraphCommunityMembers(js gjson.Result, after string) types.Result[types.User] {
	result := types.Result[types.User]{Beginning: after == ""}

	r := get(js, "data.communityResults.result")
	slice := get(r, "moderators_slice")
	if notNull(get(r, "members_slice")) {
		slice = get(r, "members_slice")
	}
	for _, item := range arr(get(slice, "items_results")) {
		if user := parseGraphUserNode(get(item, "result")); user.Username != "" {
			result.Content = append(result.Content, user)
		}
	}

	if cursor := str(get(slice, "slice_info.next_cursor")); cursor != "" {
		result.Bottom = cursor
	}
	return result
}
