// Package render converts site HTML fragments into the CLI's output formats.
package render

import "fmt"

// Format is the CLI-wide output format selected with -o|--output.
type Format int

const (
	Markdown Format = iota // default; styled for the terminal when stdout is one
	Raw                    // markdown as-is, never styled
	HTML
	Text
	JSON // machine-readable model output, bypasses rendering
)

// ParseFormat parses the -o|--output flag value.
func ParseFormat(s string) (Format, error) {
	switch s {
	case "", "markdown", "md":
		return Markdown, nil
	case "raw":
		return Raw, nil
	case "html":
		return HTML, nil
	case "text", "txt", "plain":
		return Text, nil
	case "json":
		return JSON, nil
	}
	return Markdown, fmt.Errorf("invalid output format %q (want markdown, raw, html, text, or json)", s)
}

// IsMarkdown reports whether the format's body is markdown, so callers can add
// markdown headings and lists for both the styled and the raw variant.
func (f Format) IsMarkdown() bool {
	return f == Markdown || f == Raw
}

func (f Format) String() string {
	switch f {
	case Raw:
		return "raw"
	case HTML:
		return "html"
	case Text:
		return "text"
	case JSON:
		return "json"
	default:
		return "markdown"
	}
}
