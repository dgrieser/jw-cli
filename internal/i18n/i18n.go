// Package i18n localizes the text jw-cli prints itself. Articles and verses
// arrive from jw.org already in the requested language; these are the headings,
// labels, hints and dates the CLI puts around them, so a German run does not
// come out framed in English.
//
// English is the fallback: an unsupported language gets English text next to
// its own content.
package i18n

import (
	"strings"

	"golang.org/x/text/language"
)

// Lang is a language jw-cli can print its own text in.
type Lang int

const (
	EN Lang = iota // the fallback
	DE
)

// tags are the supported languages in Lang order. The first is the fallback.
var tags = []language.Tag{language.English, language.German}

var matcher = language.NewMatcher(tags)

// Match picks the closest supported language for a locale or BCP-47 tag ("de",
// "de-AT", "de_DE"). Anything unparseable or unsupported — including the JW
// language symbols, which are not language tags — falls back to English.
func Match(tag string) Lang {
	tag = strings.TrimSpace(strings.ReplaceAll(tag, "_", "-"))
	if tag == "" {
		return EN
	}
	t, err := language.Parse(tag)
	if err != nil {
		return EN
	}
	_, index, conf := matcher.Match(t)
	if conf == language.No || index < 0 || index >= len(tags) {
		return EN
	}
	return Lang(index)
}

// String is the language's BCP-47 tag.
func (l Lang) String() string {
	if l < 0 || int(l) >= len(tags) {
		return tags[EN].String()
	}
	return tags[l].String()
}

// Text returns the message catalog for the language.
func (l Lang) Text() *Messages {
	if m, ok := catalog[l]; ok {
		return m
	}
	return catalog[EN]
}

// TextFor returns the message catalog for a locale or BCP-47 tag.
func TextFor(tag string) *Messages { return Match(tag).Text() }

var catalog = map[Lang]*Messages{EN: &en, DE: &de}
