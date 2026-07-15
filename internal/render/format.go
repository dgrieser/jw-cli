// Package render converts site HTML fragments into the CLI's output formats.
package render

import "fmt"

// Format is the CLI-wide output format selected with -o|--output.
type Format int

const (
	Markdown Format = iota // default
	HTML
	Text
	JSON // machine-readable model output, bypasses rendering
)

// ParseFormat parses the -o|--output flag value.
func ParseFormat(s string) (Format, error) {
	switch s {
	case "", "markdown", "md":
		return Markdown, nil
	case "html":
		return HTML, nil
	case "text", "txt", "plain":
		return Text, nil
	case "json":
		return JSON, nil
	}
	return Markdown, fmt.Errorf("invalid output format %q (want html, markdown, text, or json)", s)
}

func (f Format) String() string {
	switch f {
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
