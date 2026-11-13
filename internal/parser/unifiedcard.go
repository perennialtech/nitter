// SPDX-License-Identifier: AGPL-3.0-only

package parser

import (
	"encoding/binary"
	"log"
	"math/bits"
	"strconv"

	"github.com/tidwall/gjson"

	"github.com/perennialtech/nitter/internal/formatters"
	"github.com/perennialtech/nitter/internal/types"
)

type componentType int

const (
	ctDetails componentType = iota
	ctMedia
	ctSwipeableMedia
	ctButtonGroup
	ctJobDetails
	ctAppStoreDetails
	ctTwitterListDetails
	ctCommunityDetails
	ctMediaWithDetailsHorizontal
	ctHidden
	ctGrokShare
	ctUnknown
)

var componentTypes = map[string]componentType{
	"details":                       ctDetails,
	"media":                         ctMedia,
	"swipeable_media":               ctSwipeableMedia,
	"button_group":                  ctButtonGroup,
	"job_details":                   ctJobDetails,
	"app_store_details":             ctAppStoreDetails,
	"twitter_list_details":          ctTwitterListDetails,
	"community_details":             ctCommunityDetails,
	"media_with_details_horizontal": ctMediaWithDetailsHorizontal,
	"commerce_drop_details":         ctHidden,
	"grok_share":                    ctGrokShare,
}

// jsonyEnum decodes an enum field like jsony with an enumHook: absent
// values are the first variant, unknown ones are logged and mapped to def.
func jsonyEnum[T any](r gjson.Result, values map[string]T, first, def T, name string) T {
	if r.Type != gjson.String {
		return first
	}
	if v, ok := values[r.Str]; ok {
		return v
	}
	log.Printf("ERROR: Unknown enum value (%s): %s", name, r.Str)
	return def
}

type mediaType int

const (
	mtPhoto mediaType = iota
	mtVideo
	mtModel3d
)

var mediaTypes = map[string]mediaType{"photo": mtPhoto, "video": mtVideo, "model3d": mtModel3d}

type appType int

const (
	atAndroidApp appType = iota
	atIPhoneApp
	atIPadApp
)

var appTypes = map[string]appType{"android_app": atAndroidApp, "iphone_app": atIPhoneApp, "ipad_app": atIPadApp}

// textVal decodes the experimental Text type: a string, or {"content": ...}.
func textVal(r gjson.Result) string {
	if r.Type == gjson.String {
		return r.Str
	}
	return str(jfield(r, "content"))
}

// table decodes an object as a Nim Table looked up by key (the last
// duplicate key wins).
func table(r gjson.Result) map[string]gjson.Result {
	out := map[string]gjson.Result{}
	r.ForEach(func(k, v gjson.Result) bool {
		out[k.String()] = v
		return true
	})
	return out
}

// nimHash is Nim's hash(string): MurmurHash3 x86_32 with seed 0, with 0
// replaced the way Nim's tables do.
func nimHash(key string) uint32 {
	const (
		c1 = 0xcc9e2d51
		c2 = 0x1b873593
	)
	data := []byte(key)
	var h1 uint32
	n := len(data) / 4
	for i := 0; i < n; i++ {
		k1 := binary.LittleEndian.Uint32(data[i*4:])
		k1 *= c1
		k1 = bits.RotateLeft32(k1, 15)
		k1 *= c2
		h1 ^= k1
		h1 = bits.RotateLeft32(h1, 13)
		h1 = h1*5 + 0xe6546b64
	}
	var k1 uint32
	tail := data[n*4:]
	for i := len(tail) - 1; i >= 0; i-- {
		k1 = k1<<8 | uint32(tail[i])
	}
	k1 *= c1
	k1 = bits.RotateLeft32(k1, 15)
	k1 *= c2
	h1 ^= k1
	h1 ^= uint32(len(data))
	h1 ^= h1 >> 16
	h1 *= 0x85ebca6b
	h1 ^= h1 >> 13
	h1 *= 0xc2b2ae35
	h1 ^= h1 >> 16
	if h1 == 0 {
		return 314159265
	}
	return h1
}

// nimTableValues returns an object's values in the order Nim iterates the
// Table[string, T] jsony decodes it into: by hash slot, with linear probing,
// in a table that starts at 32 slots and doubles when two-thirds full. When
// several components set the same card field, the last one in this order
// wins, so JSON order isn't enough.
func nimTableValues(r gjson.Result) []gjson.Result {
	type slot struct {
		used bool
		hc   uint32
		key  string
		val  gjson.Result
	}
	data := make([]slot, 32)
	counter := 0
	rawGet := func(key string, hc uint32) int {
		mask := len(data) - 1
		h := int(hc) & mask
		for data[h].used {
			if data[h].hc == hc && data[h].key == key {
				return h
			}
			h = (h + 1) & mask
		}
		return -1 - h
	}
	enlarge := func() {
		old := data
		data = make([]slot, len(old)*2)
		mask := len(data) - 1
		for _, s := range old {
			if !s.used {
				continue
			}
			j := int(s.hc) & mask
			for data[j].used {
				j = (j + 1) & mask
			}
			data[j] = s
		}
	}

	r.ForEach(func(k, v gjson.Result) bool {
		key := k.String()
		hc := nimHash(key)
		i := rawGet(key, hc)
		if i >= 0 {
			data[i].val = v
			return true
		}
		if len(data)*2 < counter*3 || len(data)-counter < 4 {
			enlarge()
			i = rawGet(key, hc)
		}
		data[-1-i] = slot{used: true, hc: hc, key: key, val: v}
		counter++
		return true
	})

	var out []gjson.Result
	for _, s := range data {
		if s.used {
			out = append(out, s.val)
		}
	}
	return out
}

type unifiedCard struct {
	destinations  map[string]gjson.Result
	mediaEntities map[string]gjson.Result
	appStoreData  map[string]gjson.Result
}

// Nim raises KeyError for a destination, media entity or app that isn't in
// the card; missing ones are read as empty here.

func (c *unifiedCard) parseDestination(id string, result *types.Card) {
	urlData := jpath(c.destinations[id], "data", "urlData")
	result.Dest = str(jfield(urlData, "vanity"))
	result.URL = str(jfield(urlData, "url"))
}

func (c *unifiedCard) parseDetails(data gjson.Result, result *types.Card) {
	c.parseDestination(str(jfield(data, "destination")), result)

	result.Text = textVal(jfield(data, "title"))
	if result.Text == "" {
		result.Text = textVal(jfield(data, "name"))
	}
}

func (c *unifiedCard) parseMediaDetails(data gjson.Result, result *types.Card) {
	c.parseDestination(str(jfield(data, "destination")), result)

	result.Kind = types.CardSummary
	result.Image = expImageURL(str(jfield(c.mediaEntities[str(jfield(data, "mediaId"))], "mediaUrlHttps")))
	result.Text = textVal(jpath(data, "topicDetail", "title"))
	result.Dest = "Topic"
}

func (c *unifiedCard) parseJobDetails(data gjson.Result, result *types.Card) {
	c.parseDestination(str(jfield(data, "destination")), result)

	result.Kind = types.CardJobDetails
	result.Title = textVal(jfield(data, "title"))
	result.Text = str(jfield(data, "shortDescriptionText"))
	result.Dest = "@" + toUser(jfield(data, "profileUser")).Username + " · " + str(jfield(data, "location"))
}

func (c *unifiedCard) parseAppDetails(data gjson.Result, result *types.Card) {
	app := idx(c.appStoreData[str(jfield(data, "appId"))], 0)

	id := str(jfield(app, "id"))
	switch jsonyEnum(jfield(app, "kind", "type"), appTypes, atAndroidApp, atAndroidApp, "AppType") {
	case atAndroidApp:
		result.URL = "http://play.google.com/store/apps/details?id=" + id
	case atIPhoneApp, atIPadApp:
		result.URL = "https://itunes.apple.com/app/id" + id
	}

	result.Text = textVal(jfield(app, "title"))
	result.Dest = textVal(jfield(app, "category"))
}

// jsonyVariants decodes video variants into types.VideoVariant fields
// directly, as jsony does: a missing content type is the enum's first value
// and the resolution isn't computed.
func jsonyVariants(r gjson.Result) []types.VideoVariant {
	var out []types.VideoVariant
	for _, v := range arr(r) {
		contentType := types.VideoM3u8
		if ct := jfield(v, "contentType"); ct.Type == gjson.String {
			contentType = parseVideoType(ct.Str)
		}
		out = append(out, types.VideoVariant{
			ContentType: contentType,
			URL:         str(jfield(v, "url")),
			Bitrate:     getInt(jfield(v, "bitrate")),
			Resolution:  getInt(jfield(v, "resolution")),
		})
	}
	return out
}

func (c *unifiedCard) parseMedia(kind componentType, data gjson.Result, result *types.Card) {
	mediaID := str(jfield(data, "id"))
	if kind == ctSwipeableMedia {
		mediaID = str(jfield(idx(jfield(data, "mediaList"), 0), "id"))
	}

	rMedia := c.mediaEntities[mediaID]
	switch jsonyEnum(jfield(rMedia, "kind", "type"), mediaTypes, mtPhoto, mtPhoto, "MediaType") {
	case mtPhoto:
		result.Kind = types.CardSummaryLarge
		result.Image = expImageURL(str(jfield(rMedia, "mediaUrlHttps")))
	case mtVideo:
		videoInfo := jfield(rMedia, "videoInfo")
		result.Kind = types.CardPromoVideo
		result.Video = &types.Video{
			Available:    true,
			Thumb:        expImageURL(str(jfield(rMedia, "mediaUrlHttps"))),
			DurationMs:   getInt(jfield(videoInfo, "durationMillis")),
			Variants:     jsonyVariants(jfield(videoInfo, "variants")),
			PlaybackType: types.VideoM3u8,
		}
	case mtModel3d:
		result.Title = "Unsupported 3D model ad"
	}
}

func (c *unifiedCard) parseGrokShare(data gjson.Result, result *types.Card) {
	result.Kind = types.CardSummaryLarge

	c.parseDestination(str(jfield(data, "destination")), result)
	result.Dest = "Answer by Grok"

	for _, msg := range arr(jfield(data, "conversationPreview")) {
		switch str(jfield(msg, "sender")) {
		case "USER":
			result.Title = formatters.Shorten(str(jfield(msg, "message")), 70)
		case "AGENT":
			result.Text = formatters.Shorten(str(jfield(msg, "message")), 500)
		}
	}
}

// ParseUnifiedCard parses the JSON in a unified_card binding value.
func ParseUnifiedCard(js gjson.Result) types.Card {
	result := types.Card{Kind: types.CardAmplify}
	card := unifiedCard{
		destinations:  table(jfield(js, "destinationObjects")),
		mediaEntities: table(jfield(js, "mediaEntities")),
		appStoreData:  table(jfield(js, "appStoreData")),
	}

	for _, component := range nimTableValues(jfield(js, "componentObjects")) {
		kind := jsonyEnum(jfield(component, "kind", "type"), componentTypes, ctDetails, ctUnknown, "ComponentType")
		data := jfield(component, "data")
		switch kind {
		case ctDetails, ctCommunityDetails, ctTwitterListDetails:
			card.parseDetails(data, &result)
		case ctAppStoreDetails:
			card.parseAppDetails(data, &result)
		case ctMediaWithDetailsHorizontal:
			card.parseMediaDetails(data, &result)
		case ctMedia, ctSwipeableMedia:
			card.parseMedia(kind, data, &result)
		case ctGrokShare:
			card.parseGrokShare(data, &result)
		case ctJobDetails:
			card.parseJobDetails(data, &result)
		case ctHidden:
			result.Kind = types.CardHidden
		case ctUnknown:
			log.Printf("ERROR: Unknown component type: %s", js.Raw)
		}

		switch kind {
		case ctTwitterListDetails:
			result.Dest = "List · " + strconv.Itoa(getInt(jfield(data, "memberCount"))) + " Members"
		case ctCommunityDetails:
			result.Dest = "Community · " + strconv.Itoa(getInt(jfield(data, "memberCount"))) + " Members"
		}
	}
	return result
}
