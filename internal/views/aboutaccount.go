// SPDX-License-Identifier: AGPL-3.0-only

package views

import (
	"html/template"
	"strconv"

	"github.com/perennialtech/nitter/internal/formatters"
	"github.com/perennialtech/nitter/internal/types"
)

// RenderAboutAccount renders a profile's "About this account" page.
func RenderAboutAccount(info types.AccountInfo) template.HTML {
	u := types.User{
		Username: info.Username, Fullname: info.Fullname, UserPic: info.UserPic,
		VerifiedType: info.VerifiedType,
	}
	d := map[string]any{
		"Info":   info,
		"U":      u,
		"Avatar": formatters.UserPic(info.UserPic, "_200x200"),
		"Joined": info.JoinDate.Format("January 2006"),
	}
	if isVerified(u) {
		switch {
		case info.OverrideVerifiedYear != 0:
			year := info.OverrideVerifiedYear
			era := ""
			if year < 0 {
				year, era = -year, " BCE"
			}
			d["VerifiedSince"] = "Since " + strconv.Itoa(year) + era
		case !info.VerifiedSince.IsZero():
			d["VerifiedSince"] = "Since " + info.VerifiedSince.Format("January 2006")
		}
		d["IDVerified"] = info.IsIdentityVerified
	}
	if info.AffiliateUsername != "" {
		text := "@" + info.AffiliateUsername
		if info.AffiliateLabel != "" {
			text = info.AffiliateLabel + " (@" + info.AffiliateUsername + ")"
		}
		d["Affiliate"] = text
	}
	if info.UsernameChanges > 0 {
		label := strconv.Itoa(info.UsernameChanges) + " username change"
		if info.UsernameChanges > 1 {
			label += "s"
		}
		d["Changes"] = label
		if !info.LastUsernameChange.IsZero() {
			d["LastChange"] = "Last on " + info.LastUsernameChange.Format("January 2006")
		}
	}
	return exec("aboutAccount", d)
}
