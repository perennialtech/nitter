// SPDX-License-Identifier: AGPL-3.0-only
package parser

import (
	"strings"

	"github.com/perennialtech/nitter/internal/types"
	"github.com/tidwall/gjson"
)

// ParseGraphCommunitySearch reads community cards and the next cursor from
// the nested discovery response.
func ParseGraphCommunitySearch(js gjson.Result, after string) types.Result[types.Community] {
	result := types.Result[types.Community]{Beginning: after == ""}
	seen := make(map[string]bool)
	var walk func(gjson.Result)
	walk = func(node gjson.Result) {
		if typeName(node) == "Community" {
			community := parseCommunity(node)
			if community.ID != "" && strings.Trim(community.ID, "0123456789") == "" &&
				community.Name != "" && !seen[community.ID] {
				seen[community.ID] = true
				result.Content = append(result.Content, community)
			}
			return
		}
		if !node.IsObject() && !node.IsArray() {
			return
		}
		node.ForEach(func(key, child gjson.Result) bool {
			if key.Str == "next_cursor" && result.Bottom == "" {
				result.Bottom = str(child)
			}
			walk(child)
			return true
		})
	}
	walk(get(js, "data"))
	if result.Bottom == after {
		result.Bottom = ""
	}
	return result
}
