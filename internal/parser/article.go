// SPDX-License-Identifier: AGPL-3.0-only

package parser

import (
	"time"

	"github.com/tidwall/gjson"

	"github.com/perennialtech/nitter/internal/types"
)

// ParseGraphArticle parses the article of a TweetResultByRestId response,
// returning nil when the tweet has no article.
func ParseGraphArticle(js gjson.Result) *types.Article {
	if !js.IsObject() {
		return nil
	}

	tweet := jpath(js, "data", "tweetResult", "result")
	article := jpath(tweet, "article", "articleResults", "result")
	legacy := jfield(tweet, "legacy")

	title := str(jfield(article, "title"))
	if title == "" {
		return nil
	}

	var articleTime time.Time
	if publishedAt := getInt(jpath(article, "metadata", "firstPublishedAtSecs")); publishedAt > 0 {
		articleTime = time.Unix(int64(publishedAt), 0).UTC()
	} else if createdAt := str(jfield(legacy, "createdAt")); createdAt != "" {
		articleTime = parseTwitterDate(createdAt)
	}

	result := &types.Article{
		Title:      title,
		CoverImage: expImageURL(str(jpath(article, "coverMedia", "mediaInfo", "originalImgUrl"))),
		Time:       articleTime,
		User:       parseUserResult(jpath(tweet, "core", "userResults", "result")),
		Stats: types.TweetStats{
			Replies:  getInt(jfield(legacy, "replyCount")),
			Retweets: getInt(jfield(legacy, "retweetCount")),
			Quotes:   getInt(jfield(legacy, "quoteCount")),
			Likes:    getInt(jfield(legacy, "favoriteCount")),
		},
		Entities: map[int]types.ArticleEntity{},
		Media:    map[string]types.ArticleMedia{},
	}

	if count := str(jpath(tweet, "views", "count")); count != "" {
		if n, ok := nimParseInt(count); ok {
			result.Stats.Views = int(n)
		}
	}

	contentState := jfield(article, "contentState")
	for _, blk := range arr(jfield(contentState, "blocks")) {
		p := types.ArticleParagraph{
			Text: str(jfield(blk, "text")),
			Kind: str(jfield(blk, "blockKind", "type")),
		}
		for _, s := range arr(jfield(blk, "inlineStyleRanges")) {
			p.InlineStyles = append(p.InlineStyles, types.ArticleStyle{
				Offset: getInt(jfield(s, "offset")),
				Length: getInt(jfield(s, "length")),
				Style:  str(jfield(s, "style")),
			})
		}
		for _, r := range arr(jfield(blk, "entityRanges")) {
			p.EntityRanges = append(p.EntityRanges, types.ArticleEntityRange{
				Offset: getInt(jfield(r, "offset")),
				Length: getInt(jfield(r, "length")),
				Key:    getInt(jfield(r, "key")),
			})
		}
		result.Paragraphs = append(result.Paragraphs, p)
	}

	for _, entry := range arr(jfield(contentState, "entityMap")) {
		key, ok := nimParseInt(str(jfield(entry, "key")))
		if !ok {
			continue
		}
		value := jfield(entry, "value")
		data := jfield(value, "data")
		entity := types.ArticleEntity{Kind: str(jfield(value, "entityKind", "type"))}
		switch entity.Kind {
		case "LINK":
			entity.URL = str(jfield(data, "url"))
		case "MEDIA":
			for _, mi := range arr(jfield(data, "mediaItems")) {
				entity.MediaIDs = append(entity.MediaIDs, str(jfield(mi, "mediaId")))
			}
			entity.Caption = str(jfield(data, "caption"))
		case "TWEET":
			entity.TweetID = str(jfield(data, "tweetId"))
		case "MARKDOWN":
			entity.Markdown = str(jfield(data, "markdown"))
		}
		result.Entities[int(key)] = entity
	}

	for _, me := range arr(jfield(article, "mediaEntities")) {
		mediaInfo := jfield(me, "mediaInfo")
		typeName := str(jfield(mediaInfo, "typeName", "__typename"))
		media := types.ArticleMedia{Kind: typeName}
		if videoInfo := jfield(mediaInfo, "videoInfo"); notNull(videoInfo) {
			variants := arr(jfield(videoInfo, "variants"))
			switch typeName {
			case "ApiGif":
				if len(variants) > 0 {
					media.URL = str(jfield(variants[0], "url"))
				}
			case "ApiVideo":
				bestBitrate := -1
				for _, v := range variants {
					if bitrate := getInt(jfield(v, "bitrate")); bitrate > bestBitrate {
						bestBitrate = bitrate
						media.URL = str(jfield(v, "url"))
					}
				}
			}
		} else if typeName == "ApiImage" {
			media.URL = expImageURL(str(jfield(mediaInfo, "originalImgUrl")))
		}
		result.Media[str(jfield(me, "mediaId"))] = media
	}
	return result
}
