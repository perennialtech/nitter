# Changelog

## 2026-09-05
## Added

- Quote count in the tweet stats row, linked to a new Quoted Tweets page (`/user/status/id/quotes`)
- New Retweets page (`/user/status/id/retweets`)
- Infinite scroll on both new pages
- New Community Notes page (`/i/birdwatch/t/<id>`) — misleading notes first, each labeled Community Note / Proposed Community Note with a Misleading / Not Misleading tag
- Community-notes indicator icon in the tweet stats row, linking to the notes page
- Client/app source label (iPhone, Android, Web App, etc.) in the tweet stats row, including on Articles
- Abbreviated stat count display option (`1.2M` vs `1,200,000`), per-user or admin-forced via config

## Fixed

- Reply sorting (Relevance/Recency/Likes) — now uses `TweetDetail`, which also restores per-reply source labels
- Missing `withBirdwatchNotes` request parameter causing the community-notes icon to not appear on UserTweets/SearchTimeline results
- Several missing/incorrect GraphQL request parameters silently dropping data (quote counts, view counts, source labels) on specific endpoints

## 2026-09-13
## Added

 - Option for 2 customizable trending tabs on main page (enabled by default).

## 2026-09-19
## Added

 - Option to show retweet timestamp and permalink next to "user retweeted" (showRetweetTime)
 - Retweets-only tab, using `UserRepostsTimeline`
 - Replies-only tab, using `UserRepliesTimeline`
 - RSS feed for replies and retweets

## 2026-09-27
## Changed

 - Use tweets-only endpoint, as retweets are now isolated to a new tab.

## 2026-10-07
## Added

 - Tweet and Community Note translation (via Grok), using the translation-enabled `TweetResultByRestId` endpoint.
 - `autoTranslate` preference ("Auto translate tweets if available"), default off; can be set server-wide under `[Preferences]` in nitter.conf

## 2026-10-16
## Added

 - Full on-demand translation: Tweets and Community Notes that are in another language, but don't have a translation yet, now show a small "Translate to English" (or specified target lang) link.
 - After translation, the tweets/notes will show "Translated to English" with a "show original" link next to it. Adding `?tr=0` to any page also shows the original, even if auto-translate is on.

This feature is off by default. In `nitter.conf`, add:

```
onDemandTranslation = true
onDemandTranslationLang = "en"
```

Use a different lang code (for example `"fr"`) to translate into that language.

## Changed

 - Community notes tabs are now sorted by tweet dates, newest to oldest.

## 2026-10-19
## Added

 - `restoreFocalSource` preference ("Restore focal tweet source label"), default off: makes one extra `count=1` request to the legacy `/2/timeline/conversation/<id>.json` endpoint to fill in the focal tweet's source label when `TweetDetail` didn't return one (it's currently only being returned on retweets, random old tweets and deleted conversations). Conversations themselves still use `TweetDetail` so translation, birdwatch and verification features don't break.


## 2026-10-24
## Added

 - Parody, Fan and Commentary profile labels.

## 2026-10-26
## Added

 - RSS feed for birdwatch (community notes). The feed for /new is noisy, be warned.
