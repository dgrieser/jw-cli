package render

import (
	"fmt"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
)

// Options controls rendering.
type Options struct {
	// BaseURL absolutizes relative links and image sources (e.g.
	// "https://wol.jw.org" for wol documents).
	BaseURL string
}

// Render converts a site HTML fragment into the requested output format.
// JSON is not a rendering of HTML; callers handle it before calling Render.
func Render(fragment string, f Format, o Options) (string, error) {
	clean, err := sanitize(fragment, o.BaseURL)
	if err != nil {
		return "", fmt.Errorf("sanitize HTML: %w", err)
	}
	switch f {
	case HTML:
		return clean, nil
	case Markdown:
		md, err := htmltomarkdown.ConvertString(clean)
		if err != nil {
			return "", fmt.Errorf("convert to markdown: %w", err)
		}
		return md, nil
	case Text:
		return toText(clean)
	case JSON:
		return "", fmt.Errorf("render: JSON output must be handled by the caller")
	}
	return "", fmt.Errorf("unknown format %v", f)
}
