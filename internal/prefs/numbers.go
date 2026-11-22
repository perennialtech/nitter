// SPDX-License-Identifier: AGPL-3.0-only

package prefs

import (
	"strconv"
	"strings"

	"golang.org/x/text/language"
)

// NumberFormat formats counts and decimal numbers for one locale.
type NumberFormat struct {
	digits                       [10]string
	decimal, group, minus        string
	primaryGroup, secondaryGroup int
	minimumGroupingDigits        int
}

var englishNumbers = lookupNumberFormat("en")

// SetNumberLanguage applies the browser preference without storing it in a cookie.
func (p *Prefs) SetNumberLanguage(header string) {
	p.numberFormat = nil
	tags, _, _ := language.ParseAcceptLanguage(header)
	for _, tag := range tags {
		system := tag.TypeForKey("nu")
		if len(tag.Extensions()) > 0 {
			tag = tag.Parent()
		}
		for ; tag != language.Und; tag = tag.Parent() {
			name := tag.String()
			if system != "" {
				if f := lookupNumberFormat(name + "-u-nu-" + system); f != nil {
					p.numberFormat = f
					return
				}
			}
			if f := lookupNumberFormat(name); f != nil {
				p.numberFormat = f
				return
			}
		}
	}
}

// Numbers returns the visitor's number format, defaulting to English.
func (p *Prefs) Numbers() *NumberFormat {
	if p.numberFormat != nil {
		return p.numberFormat
	}
	return englishNumbers
}

func (f *NumberFormat) Int(n int) string {
	return f.Int64(int64(n))
}

func (f *NumberFormat) Int64(n int64) string {
	return f.format(strconv.FormatInt(n, 10))
}

func (f *NumberFormat) OneDecimal(n float64) string {
	return f.format(strconv.FormatFloat(n, 'f', 1, 64))
}

func (f *NumberFormat) format(ascii string) string {
	var result strings.Builder
	if unsigned, negative := strings.CutPrefix(ascii, "-"); negative {
		result.WriteString(f.minus)
		ascii = unsigned
	}
	integer, fraction, hasFraction := strings.Cut(ascii, ".")
	group := f.primaryGroup > 0 && len(integer) >= f.primaryGroup+f.minimumGroupingDigits
	for i := range len(integer) {
		remaining := len(integer) - i
		if group && i > 0 && remaining >= f.primaryGroup &&
			(remaining-f.primaryGroup)%f.secondaryGroup == 0 {
			result.WriteString(f.group)
		}
		result.WriteString(f.digits[integer[i]-'0'])
	}
	if hasFraction {
		result.WriteString(f.decimal)
		for i := range len(fraction) {
			result.WriteString(f.digits[fraction[i]-'0'])
		}
	}
	return result.String()
}
