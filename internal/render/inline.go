package render

import (
	"strings"

	"golang.org/x/net/html"
)

const (
	ansiBold   = "\x1b[1m"
	ansiItalic = "\x1b[3m"
	ansiReset  = "\x1b[0m"
)

// InlineOptions controls inline HTML rendering.
type InlineOptions struct {
	// Emphasis turns <strong>/<b> into ANSI bold and <em>/<i> into italics.
	// NO_COLOR turns it back off.
	Emphasis bool
}

// Inline renders an HTML fragment that carries only inline markup as one line of
// terminal text. Search results arrive that way — the matched words wrapped in
// <strong>, references spelled "Daniel&nbsp;7:27" — and listings would otherwise
// print the tags and entities verbatim. Tags are dropped and entities decoded;
// with Emphasis the search highlight survives as bold. Whitespace is collapsed,
// so a snippet with a newline in it cannot break a listing's layout.
func Inline(fragment string, o InlineOptions) string {
	if !strings.ContainsAny(fragment, "<&") {
		return collapseSpace(fragment)
	}
	node, err := html.Parse(strings.NewReader(fragment))
	if err != nil {
		return collapseSpace(fragment)
	}
	var b strings.Builder
	walkInline(node, &b, o.Emphasis && !noColorEnv())
	return collapseSpace(b.String())
}

func walkInline(n *html.Node, b *strings.Builder, emphasis bool) {
	switch n.Type {
	case html.TextNode:
		b.WriteString(n.Data)
		return
	case html.ElementNode:
		switch n.Data {
		case "script", "style", "noscript":
			return
		case "br":
			b.WriteString(" ")
		case "img":
			if alt := attrOf(n, "alt"); alt != "" {
				b.WriteString(imagePlaceholder(alt))
			}
		}
		if emphasis {
			if seq := emphasisSeq(n.Data); seq != "" {
				b.WriteString(seq)
				walkInlineChildren(n, b, emphasis)
				b.WriteString(ansiReset)
				return
			}
		}
	}
	walkInlineChildren(n, b, emphasis)
}

func walkInlineChildren(n *html.Node, b *strings.Builder, emphasis bool) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkInline(c, b, emphasis)
	}
}

func emphasisSeq(tag string) string {
	switch tag {
	case "strong", "b":
		return ansiBold
	case "em", "i":
		return ansiItalic
	}
	return ""
}

// collapseSpace reduces every run of whitespace — including the non-breaking
// spaces decoded out of &nbsp; — to a single blank.
func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
