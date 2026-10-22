# SPDX-License-Identifier: AGPL-3.0-only
import std/unittest
import ../src/formatters

suite "HTML text formatting":
  test "plain text and nested tags":
    check stripHtml("") == ""
    check stripHtml("Nim") == "Nim"
    check stripHtml("<p>Hello <strong>Nim</strong>!</p>") == "Hello Nim!"

  test "HTML entities":
    check stripHtml("Nim &amp; C &lt;3 &#x2764;") == "Nim & C <3 ❤"

  test "external links use their full destination":
    check stripHtml("<a href=\"https://example.com/path\">short</a>") ==
      "https://example.com/path"
    check stripHtml("<p><a href=\"https://example.com/path\">short</a></p>") ==
      "https://example.com/path"

  test "external links can be shortened":
    check stripHtml("<a href=\"https://example.com/abcdefghijklmnopqrstuvwxyz\">short</a>",
                   shorten=true) == "example.com/abcdefghijklmnop…"

  test "relative links keep their label":
    check stripHtml("<a href=\"/nim_lang\">@nim_lang</a>") == "@nim_lang"

  test "empty links are ignored":
    check stripHtml("<a href=\"https://example.com\"></a>") == ""
