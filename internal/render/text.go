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
// paragraphs, list items get a dash, whitespace is collapsed.
func toText(fragment string) (string, error) {
	node, err := html.Parse(strings.NewReader(fragment))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	walkText(node, &b)
	return tidyText(b.String()), nil
}

func walkText(n *html.Node, b *strings.Builder) {
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
				b.WriteString("[image: " + alt + "]")
			}
		}
		if blockTags[n.Data] {
			b.WriteString("\n\n")
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkText(c, b)
	}
	if n.Type == html.ElementNode && blockTags[n.Data] {
		b.WriteString("\n\n")
	}
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
