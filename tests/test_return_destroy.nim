# SPDX-License-Identifier: AGPL-3.0-only
# Checks that the compiler from tools/patch-nim.sh destroys an earlier
# `result` value on `return x`, and that packedjson lookups of missing keys
# therefore no longer retain the parsed tree.
import std/strutils
import packedjson

type
  Payload = object
  Holder = object
    payload: ref Payload

var destroyed = 0
proc `=destroy`(p: Payload) = inc destroyed

proc replaceOnReturn(h: Holder; replace: bool): Holder =
  result = h
  if replace: return Holder()

for replace in [false, true]:
  destroyed = 0
  for _ in 0 ..< 100:
    discard replaceOnReturn(Holder(payload: new Payload), replace)
  doAssert destroyed == 100, "return leaked " & $(100 - destroyed) & " earlier results"

let body = "{\"data\":{\"items\":[" &
  repeat("{\"text\":\"" & repeat('x', 200) & "\"},", 200) & "1]}}"
let before = getOccupiedMem()
for _ in 0 ..< 200:
  let js = parseJson(body)
  doAssert js{"data", "missing"}.kind == JNull
  doAssert js{"missing"}.kind == JNull
  doAssert js["data"]{"items"}{1000}.kind == JNull
let grown = getOccupiedMem() - before
doAssert grown < body.len * 4, "missing-key lookups retained " & $grown & " bytes"
echo "ok"
