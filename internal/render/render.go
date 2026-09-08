package render

import (
	"fmt"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
)

// Options controls rendering.
type Options struct {
	// BaseURL absolutizes relative links and image sources (e.g.
	// "https://wol.jw.org" for wol documents).
	BaseURL string
	// NoURLs drops every link target and image source from the output: a link
	// keeps its text, an image its alt text. Set by the global --no-urls flag.
	NoURLs bool
}

// horizontalRule is the thematic break written for an <hr>. The converter's
// default is "* * *", which is valid but unusual; three hyphens is the form
// almost every markdown document uses.
const horizontalRule = "---"

// markdownConverter is the stock converter with the thematic break settled. It
// holds no per-conversion state, so one is enough for the whole process.
var markdownConverter = converter.NewConverter(
	converter.WithPlugins(
		base.NewBasePlugin(),
		commonmark.NewCommonmarkPlugin(
			commonmark.WithHorizontalRule(horizontalRule),
		),
	),
)

// StyledText renders a fragment as plain text with the inline emphasis and the
// search highlight kept as ANSI. It is Render(fragment, Text, o) exactly when
// the style is off, so the same call serves a terminal and a pipe.
func StyledText(fragment string, o Options, style TextStyle) (string, error) {
	clean, err := sanitize(fragment, o)
	if err != nil {
		return "", fmt.Errorf("sanitize HTML: %w", err)
	}
	return toText(clean, style)
}

// Render converts a site HTML fragment into the requested output format.
// JSON is not a rendering of HTML; callers handle it before calling Render.
func Render(fragment string, f Format, o Options) (string, error) {
	clean, err := sanitize(fragment, o)
	if err != nil {
		return "", fmt.Errorf("sanitize HTML: %w", err)
	}
	switch f {
	case HTML:
		return clean, nil
	case Markdown, Raw:
		md, err := markdownConverter.ConvertString(clean)
		if err != nil {
			return "", fmt.Errorf("convert to markdown: %w", err)
		}
		return md, nil
	case Text:
		return toText(clean, TextStyle{})
	case JSON:
		return "", fmt.Errorf("render: JSON output must be handled by the caller")
	}
	return "", fmt.Errorf("unknown format %v", f)
}
