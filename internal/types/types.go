// SPDX-License-Identifier: AGPL-3.0-only

// Package types holds the domain model shared by the API client, parser,
// cache, views and routes.
package types

import (
	"errors"
	"time"
)

// Errors that routes map to specific error pages.
var (
	ErrRateLimited = errors.New("rate limited")
	ErrNoSessions  = errors.New("no sessions available")
	ErrInternal    = errors.New("internal error")
	ErrBadClient   = errors.New("bad client")
)

type TimelineKind int

const (
	TimelineTweets TimelineKind = iota
	TimelineReplies
	TimelineMedia
	TimelineArticles
	TimelineReposts
)

// ErrorCode is a Twitter API error code.
type ErrorCode int

const (
	ErrNull                ErrorCode = 0
	ErrNoUserMatches       ErrorCode = 17
	ErrProtectedUser       ErrorCode = 22
	ErrMissingParams       ErrorCode = 25
	ErrTimeout             ErrorCode = 29
	ErrCouldntAuth         ErrorCode = 32
	ErrDoesntExist         ErrorCode = 34
	ErrUnauthorized        ErrorCode = 37
	ErrInvalidParam        ErrorCode = 47
	ErrUserNotFound        ErrorCode = 50
	ErrSuspended           ErrorCode = 63
	ErrRateLimitedCode     ErrorCode = 88
	ErrExpiredToken        ErrorCode = 89
	ErrListIdOrSlug        ErrorCode = 112
	ErrTimelineUnavailable ErrorCode = 131
	ErrTweetNotFound       ErrorCode = 144
	ErrTweetNotAuthorized  ErrorCode = 179
	ErrForbidden           ErrorCode = 200
	ErrBadRequest          ErrorCode = 214
	ErrBadToken            ErrorCode = 239
	ErrLocked              ErrorCode = 326
	ErrNoCsrf              ErrorCode = 353
	ErrTweetUnavailable    ErrorCode = 421
	ErrTweetCensored       ErrorCode = 422
)

type VerifiedType string

const (
	VerifiedNone       VerifiedType = "None"
	VerifiedBlue       VerifiedType = "Blue"
	VerifiedBusiness   VerifiedType = "Business"
	VerifiedGovernment VerifiedType = "Government"
)

type User struct {
	ID           string
	Username     string
	Fullname     string
	Location     string
	Website      string
	Bio          string
	UserPic      string
	Banner       string
	PinnedTweet  int64
	Following    int
	Followers    int
	Tweets       int
	Likes        int
	Media        int
	VerifiedType VerifiedType
	ParodyLabel  string
	Protected    bool
	Suspended    bool
	JoinDate     time.Time
}

type AccountInfo struct {
	Username             string
	Fullname             string
	UserPic              string
	JoinDate             time.Time
	VerifiedType         VerifiedType
	Suspended            bool
	BasedIn              string
	Source               string
	UsernameChanges      int
	LastUsernameChange   time.Time
	AffiliateUsername    string
	AffiliateLabel       string
	IsIdentityVerified   bool
	VerifiedSince        time.Time
	OverrideVerifiedYear int
}

type Broadcast struct {
	ID                 string
	Title              string
	State              string
	Thumb              string
	MediaKey           string
	M3u8URL            string
	TotalWatched       int
	StartTime          time.Time
	EndTime            time.Time
	ReplayStart        int
	AvailableForReplay bool
	User               User
}

type SpaceParticipant struct {
	UserID      string
	Username    string
	DisplayName string
	AvatarURL   string
	IsVerified  bool
}

type AudioSpace struct {
	ID                 string
	Title              string
	State              string
	MediaKey           string
	M3u8URL            string
	TotalLiveListeners int
	TotalReplayWatched int
	StartTime          time.Time
	EndTime            time.Time
	AvailableForReplay bool
	Creator            User
	Admins             []SpaceParticipant
	Speakers           []SpaceParticipant
}

type VideoType string

const (
	VideoM3u8 VideoType = "application/x-mpegURL"
	VideoMp4  VideoType = "video/mp4"
	VideoVmap VideoType = "video/vmap"
)

type VideoVariant struct {
	ContentType VideoType
	URL         string
	Bitrate     int
	Resolution  int
}

type Video struct {
	DurationMs   int
	URL          string
	Thumb        string
	Available    bool
	Reason       string
	Title        string
	Description  string
	PlaybackType VideoType
	Variants     []VideoVariant
}

// QueryKind values match the Nim enum names, which appear in URLs (f=...).
type QueryKind string

const (
	QueryPosts     QueryKind = "posts"
	QueryReplies   QueryKind = "replies"
	QueryMedia     QueryKind = "media"
	QueryUsers     QueryKind = "users"
	QueryTweets    QueryKind = "tweets"
	QueryUserList  QueryKind = "userList"
	QueryFollowers QueryKind = "followers"
	QueryFollowing QueryKind = "following"
	QueryLists     QueryKind = "lists"
	QueryTop       QueryKind = "top"
	QueryArticles  QueryKind = "articles"
	QueryReposts   QueryKind = "reposts"
)

var QueryKinds = []QueryKind{
	QueryPosts, QueryReplies, QueryMedia, QueryUsers, QueryTweets, QueryUserList,
	QueryFollowers, QueryFollowing, QueryLists, QueryTop, QueryArticles, QueryReposts,
}

type RankingMode string

const (
	RankingRelevance RankingMode = "Relevance"
	RankingRecency   RankingMode = "Recency"
	RankingLikes     RankingMode = "Likes"
)

type Query struct {
	Kind     QueryKind
	View     string
	Text     string
	Filters  []string
	Includes []string
	Excludes []string
	FromUser []string
	Since    string
	Until    string
	MinLikes string
	Sep      string
}

type Gif struct {
	URL     string
	Thumb   string
	AltText string
}

type Photo struct {
	URL     string
	AltText string
}

type MediaKind int

const (
	PhotoMedia MediaKind = iota
	VideoMedia
	GifMedia
)

// Media is a tagged union: only the field matching Kind is set.
type Media struct {
	Kind  MediaKind
	Photo Photo
	Video Video
	Gif   Gif
}

func (m Media) Thumb() string {
	switch m.Kind {
	case PhotoMedia:
		return m.Photo.URL
	case VideoMedia:
		return m.Video.Thumb
	default:
		return m.Gif.Thumb
	}
}

type GalleryPhoto struct {
	URL     string
	TweetID string
	Color   string
}

type PhotoRail []GalleryPhoto

type Article struct {
	Title      string
	CoverImage string
	User       User
	Time       time.Time
	Stats      TweetStats
	Paragraphs []ArticleParagraph
	Entities   map[int]ArticleEntity
	Media      map[string]ArticleMedia
}

type ArticleParagraph struct {
	Text         string
	Kind         string
	InlineStyles []ArticleStyle
	EntityRanges []ArticleEntityRange
}

type ArticleStyle struct {
	Offset int
	Length int
	Style  string
}

type ArticleEntityRange struct {
	Offset int
	Length int
	Key    int
}

type ArticleEntity struct {
	Kind     string
	URL      string
	MediaIDs []string
	TweetID  string
	Markdown string
	Caption  string
}

type ArticleMedia struct {
	Kind string
	URL  string
}

type Poll struct {
	Options []string
	Values  []int
	Votes   int
	Leader  int
	Status  string
}

type CardKind string

const (
	CardAmplify               CardKind = "amplify"
	CardApp                   CardKind = "app"
	CardAppPlayer             CardKind = "appplayer"
	CardPlayer                CardKind = "player"
	CardSummary               CardKind = "summary"
	CardSummaryLarge          CardKind = "summary_large_image"
	CardPromoWebsite          CardKind = "promo_website"
	CardPromoVideo            CardKind = "promo_video_website"
	CardPromoVideoConvo       CardKind = "promo_video_convo"
	CardPromoImageConvo       CardKind = "promo_image_convo"
	CardPromoImageApp         CardKind = "promo_image_app"
	CardStoreLink             CardKind = "direct_store_link_app"
	CardLiveEvent             CardKind = "live_event"
	CardBroadcast             CardKind = "broadcast"
	CardPeriscope             CardKind = "periscope_broadcast"
	CardUnified               CardKind = "unified_card"
	CardMoment                CardKind = "moment"
	CardMessageMe             CardKind = "message_me"
	CardVideoDirectMessage    CardKind = "video_direct_message"
	CardImageDirectMessage    CardKind = "image_direct_message"
	CardAudiospace            CardKind = "audiospace"
	CardNewsletterPublication CardKind = "newsletter_publication"
	CardJobDetails            CardKind = "job_details"
	CardHidden                CardKind = "hidden"
	CardUnknown               CardKind = "unknown"
)

type Card struct {
	Kind  CardKind
	URL   string
	Title string
	Dest  string
	Text  string
	Image string
	Video *Video
}

type TweetStats struct {
	Replies  int
	Retweets int
	Quotes   int
	Likes    int
	Views    int
	Source   string
}

type ArticlePreview struct {
	Title       string
	PreviewText string
	CoverImage  string
	TweetID     int64
}

type BirdwatchNote struct {
	ID             string
	Text           string
	Helpful        bool
	Misleading     bool
	CreatedAt      time.Time
	AuthorAlias    string
	Classification string
	MisleadingTags []string
	HelpfulTags    []string
	DecidedBy      string
	Translatable   bool
	IsTranslatable bool
	Translation    string
	TransLang      string
	TransTarget    string
}

type BirdwatchNotes struct {
	Notes []BirdwatchNote
}

type BirdwatchHistoryNote struct {
	ID             string
	Text           string
	Helpful        bool
	Classification string
	CreatedAt      time.Time
	TweetID        int64
	TweetUsername  string
}

type BirdwatchSingleNote struct {
	Note    BirdwatchNote
	TweetID int64
}

type BirdwatchHistory struct {
	Alias  string
	Bottom string
	Notes  []BirdwatchHistoryNote
}

type Tweet struct {
	ID                 int64
	ThreadID           int64
	ReplyID            int64
	User               User
	Text               string
	Time               time.Time
	Reply              []string
	Pinned             bool
	HasThread          bool
	Available          bool
	ExpURL             string
	Tombstone          string
	Location           string
	Source             string
	Stats              TweetStats
	Retweet            *Tweet
	Attribution        *User
	AttributionLink    string
	MediaTags          []User
	Quote              *Tweet
	Card               *Card
	Poll               *Poll
	Media              []Media
	History            []int64
	Note               string
	HasBirdwatch       bool
	IsAd               bool
	IsAI               bool
	ArticlePreview     *ArticlePreview
	Translatable       bool
	IsTranslatable     bool
	Translation        string
	TransLang          string
	TransTarget        string
	ShowTrans          bool
	NoteID             string
	NoteTranslatable   bool
	NoteIsTranslatable bool
	NoteTranslation    string
	NoteTransLang      string
	NoteTransTarget    string
}

// Tweets is one timeline entry: a single tweet, or a self-thread shown together.
type Tweets []*Tweet

type Result[T any] struct {
	Content   []T
	Top       string
	Bottom    string
	Beginning bool
	Query     Query
}

type Chain struct {
	Content Tweets
	HasMore bool
	Cursor  string
	Related bool
}

func (c Chain) Contains(t *Tweet) bool {
	for _, it := range c.Content {
		if it.ID == t.ID {
			return true
		}
	}
	return false
}

type Conversation struct {
	Tweet   *Tweet
	Before  Chain
	After   Chain
	Replies Result[Chain]
}

type EditHistory struct {
	Latest  *Tweet
	History Tweets
}

type Timeline = Result[Tweets]
type UsersTimeline = Result[User]

type Profile struct {
	User        User
	PhotoRail   PhotoRail
	Pinned      *Tweet
	Tweets      Timeline
	AccountInfo AccountInfo
}

type List struct {
	ID          string
	Name        string
	UserID      string
	Username    string
	Description string
	Members     int
	Banner      string
}

type ListSearchResult struct {
	List             List
	Owner            User
	FollowersContext string
	Facepiles        []string
}

type CommunityRule struct {
	Name        string
	Description string
}

type Community struct {
	ID          string
	Name        string
	Description string
	MemberCount int
	Banner      string
	Creator     User
	Category    string
	JoinPolicy  string
	CreatedAt   time.Time
	Rules       []CommunityRule
	Hashtags    []string
}

type GlobalObjects struct {
	Tweets map[string]*Tweet
	Users  map[string]User
}

type Rss struct {
	Feed   string
	Cursor string
}

type Trend struct {
	Name        string
	SearchQuery string
	TweetVolume int
}

func (t *Tweet) Photos() []Photo {
	var out []Photo
	for _, m := range t.Media {
		if m.Kind == PhotoMedia {
			out = append(out, m.Photo)
		}
	}
	return out
}

func (t *Tweet) Videos() []Video {
	var out []Video
	for _, m := range t.Media {
		if m.Kind == VideoMedia {
			out = append(out, m.Video)
		}
	}
	return out
}

func (t *Tweet) hasKind(k MediaKind) bool {
	for _, m := range t.Media {
		if m.Kind == k {
			return true
		}
	}
	return false
}

func (t *Tweet) HasPhotos() bool { return t.hasKind(PhotoMedia) }
func (t *Tweet) HasVideos() bool { return t.hasKind(VideoMedia) }
func (t *Tweet) HasGifs() bool   { return t.hasKind(GifMedia) }
