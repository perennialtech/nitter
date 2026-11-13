# SPDX-License-Identifier: AGPL-3.0-only
## Counts upstream API calls by backend, endpoint and outcome, and logs the
## totals periodically, so the logs show which calls FxTwitter serves and
## which still reach the session pool.
import std/[asyncdispatch, strutils, tables]

const statsIntervalMinutes = 5

var
  calls: CountTable[string]
  fxPeakPerMinute*: int

proc recordCall*(backend, endpoint, outcome: string) =
  calls.inc(backend & " " & endpoint & " " & outcome)

proc formatApiStats*(): string =
  calls.sort()
  var parts: seq[string]
  for key, count in calls:
    parts.add key & "=" & $count
  result = parts.join(", ")
  if fxPeakPerMinute > 0:
    result.add "; fx peak/min=" & $fxPeakPerMinute

proc resetApiStats*() =
  calls.clear()
  fxPeakPerMinute = 0

proc logApiStats*() {.async.} =
  while true:
    await sleepAsync(statsIntervalMinutes * 60_000)
    if calls.len > 0:
      echo "[api] last ", statsIntervalMinutes, "m: ", formatApiStats()
      resetApiStats()
