package render

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var blockTags = map[string]bool{
	"p": true, "div": true, "section": true, "article": true, "aside": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"ul": true, "ol": true, "table": true, "tr": true, "blockquote": true,
	"figure": true, "figcaption": true, "header": true, "footer": true,
}

// toText renders HTML as readable plain text: block elements become
// paragraphs, list items get a dash, whitespace is collapsed. With a style
// that is on, the inline emphasis and the search highlight survive as ANSI —
// the structure is the same either way, so a pipe reads what a terminal reads.
func toText(fragment string, style TextStyle) (string, error) {
	node, err := html.Parse(strings.NewReader(fragment))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	walkText(node, &b, style, "")
	return tidyText(b.String()), nil
}

// walkText writes n as text. enclosing is the escape sequence in force around
// it: a nested style ends with a reset, which clears everything, so the
// enclosing one is written again after it.
func walkText(n *html.Node, b *strings.Builder, style TextStyle, enclosing string) {
	switch n.Type {
	case html.TextNode:
		b.WriteString(n.Data)
		return
	case html.ElementNode:
		switch n.Data {
		case "script", "style", "noscript":
			return
		case "br":
			b.WriteString("\n")
		case "li":
			b.WriteString("\n- ")
		case "img":
			if alt := attrOf(n, "alt"); alt != "" {
				b.WriteString(imagePlaceholder(alt))
			}
		}
		if blockTags[n.Data] {
			b.WriteString("\n\n")
		}
		if seq := escapeFor(n, style); seq != "" {
			b.WriteString(seq)
			walkTextChildren(n, b, style, enclosing+seq)
			b.WriteString(ansiReset + enclosing)
			if blockTags[n.Data] {
				b.WriteString("\n\n")
			}
			return
		}
	}
	walkTextChildren(n, b, style, enclosing)
	if n.Type == html.ElementNode && blockTags[n.Data] {
		b.WriteString("\n\n")
	}
}

func walkTextChildren(n *html.Node, b *strings.Builder, style TextStyle, enclosing string) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkText(c, b, style, enclosing)
	}
}

// escapeFor is how an element is marked up, or "" for the ones that are not.
// wol wraps what a search matched in span.mk, both in a result's teaser and in
// the document it serves through that result's link.
func escapeFor(n *html.Node, style TextStyle) string {
	switch n.Data {
	case "strong", "b":
		return style.strongSeq()
	case "em", "i":
		return style.italicSeq()
	case "mark":
		return style.markSeq()
	}
	if hasClass(n, "mk") {
		return style.markSeq()
	}
	return ""
}

// hasClass reports whether the element carries the class.
func hasClass(n *html.Node, class string) bool {
	for c := range strings.FieldsSeq(attrOf(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
}

func attrOf(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

var (
	spaceRun   = regexp.MustCompile(`[ \t\r\f]+`)
	newlineRun = regexp.MustCompile(`\n{3,}`)
	lineSpace  = regexp.MustCompile(`(?m)^[ \t]+|[ \t]+$`)
)

func tidyText(s string) string {
	s = spaceRun.ReplaceAllString(s, " ")
	s = lineSpace.ReplaceAllString(s, "")
	s = newlineRun.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
