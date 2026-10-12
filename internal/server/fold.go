package server

import (
	"strings"

	"github.com/dgrieser/jw-cli/internal/service"

	"github.com/PuerkitoBio/goquery"
	nethtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// foldSections turns every section an expansion brought with it — the study
// bible's indexes, each marginal reference, the publications quoting a verse —
// into a disclosure of its own, and every reference inside one into a
// disclosure again. A reading then reads as a reading, and what hangs off a
// verse is opened when it is wanted rather than pushing the next verse off the
// screen. Only what sits inside a div.expansion is folded: the document's own
// headings stay as they are.
func foldSections(fragment string) string {
	if !strings.Contains(fragment, "expansion") {
		return fragment
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(fragment))
	if err != nil {
		return fragment
	}
	doc.Find("div.expansion").Each(func(_ int, div *goquery.Selection) {
		foldHeadings(div.Nodes[0])
	})
	out, err := doc.Find("body").Html()
	if err != nil {
		return fragment
	}
	return out
}

// foldFragment folds every heading of a fragment, as foldSections does inside
// an expansion. A streamed section is expansion from top to bottom.
func foldFragment(fragment string) string {
	if !strings.Contains(fragment, "<h") {
		return fragment
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(fragment))
	if err != nil {
		return fragment
	}
	body := doc.Find("body")
	foldHeadings(body.Nodes[0])
	out, err := body.Html()
	if err != nil {
		return fragment
	}
	return out
}

// foldHeadings wraps each run of n's children under its own heading into a
// disclosure: the heading becomes the summary, what follows it up to the next
// heading of its level the body. The sections are the shallowest headings n
// holds; the deeper ones are folded the same way inside the section they belong
// to, so every level of an expansion opens on its own. Consecutive sections
// share one div.sections, which is what lays closed ones out side by side.
func foldHeadings(n *nethtml.Node) {
	level := 0
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if l := nodeHeading(c); l > 0 && (level == 0 || l < level) {
			level = l
		}
	}
	if level == 0 {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == nethtml.ElementNode && c.DataAtom != atom.Summary {
				foldHeadings(c)
			}
		}
		return
	}
	var kids []*nethtml.Node
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		n.RemoveChild(c)
		kids = append(kids, c)
		c = next
	}
	i := 0
	for ; i < len(kids) && nodeHeading(kids[i]) != level; i++ {
		if kids[i].Type == nethtml.ElementNode {
			foldHeadings(kids[i])
		}
		n.AppendChild(kids[i])
	}
	var wrap *nethtml.Node
	for i < len(kids) {
		heading := kids[i]
		i++
		details := element(atom.Details, "section")
		body := element(atom.Div, "section-body")
		for ; i < len(kids) && nodeHeading(kids[i]) != level; i++ {
			body.AppendChild(kids[i])
		}
		foldHeadings(body)
		lazySection(details, body)
		// a heading with nothing under it — a passage's title, say — stays a
		// heading: a section of it would open on nothing
		if attr(details, "data-lazy") == "" && blank(body) {
			n.AppendChild(heading)
			wrap = nil
			continue
		}
		summary := element(atom.Summary, "")
		for c := heading.FirstChild; c != nil; {
			next := c.NextSibling
			heading.RemoveChild(c)
			summary.AppendChild(c)
			c = next
		}
		details.AppendChild(summary)
		details.AppendChild(body)
		if wrap == nil {
			wrap = element(atom.Div, "sections")
			n.AppendChild(wrap)
		}
		wrap.AppendChild(details)
	}
}

// blank reports whether n holds nothing but white space.
func blank(n *nethtml.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == nethtml.ElementNode || (c.Type == nethtml.TextNode && strings.TrimSpace(c.Data) != "") {
			return false
		}
	}
	return true
}

// element is a fresh element with a class, or none when class is empty.
func element(a atom.Atom, class string) *nethtml.Node {
	n := &nethtml.Node{Type: nethtml.ElementNode, DataAtom: a, Data: a.String()}
	if class != "" {
		n.Attr = []nethtml.Attribute{{Key: "class", Val: class}}
	}
	return n
}

// nodeHeading is the level of a heading element, or zero for anything else.
func nodeHeading(n *nethtml.Node) int {
	if n.Type != nethtml.ElementNode {
		return 0
	}
	return headingLevel(n.Data)
}

// headingLevel is the depth of a heading element, or zero for anything else.
func headingLevel(name string) int {
	if len(name) == 2 && name[0] == 'h' && name[1] >= '1' && name[1] <= '6' {
		return int(name[1] - '0')
	}
	return 0
}

// lazySection makes a section written in one piece whose body was left to
// the page — who quotes a verse — a section that loads it once it is opened:
// the placeholder its body holds says from where.
func lazySection(details, body *nethtml.Node) {
	p := body.FirstChild
	for p != nil && p.Type != nethtml.ElementNode {
		p = p.NextSibling
	}
	if p == nil || p.DataAtom != atom.P || attr(p, "class") != service.LazyClass {
		return
	}
	lazy := attr(p, "data-lazy")
	if lazy == "" {
		return
	}
	body.RemoveChild(p)
	details.Attr = append(details.Attr, nethtml.Attribute{Key: "data-lazy", Val: lazy})
}

func attr(n *nethtml.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// foldPrintEditions puts the pictures of a document's printed pages — wol's
// "Printed edition" thumbnails, one block per spread, the later ones hidden
// behind a click on wol — into one disclosure under their common title,
// collapsed: they repeat what the text says, and push it far down the page.
func foldPrintEditions(fragment string) string {
	if !strings.Contains(fragment, "alternatePresentation") {
		return fragment
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(fragment))
	if err != nil {
		return fragment
	}
	doc.Find("div.alternatePresentation").Each(func(_ int, div *goquery.Selection) {
		// a run of them is folded from its first
		if prev := div.Prev(); prev.Length() > 0 && prev.HasClass("alternatePresentation") {
			return
		}
		run := div
		for next := div.Next(); next.Length() > 0 && next.HasClass("alternatePresentation"); next = next.Next() {
			run = run.AddSelection(next)
		}
		title := strings.TrimSpace(div.Find(".title").First().Text())
		var body strings.Builder
		run.Find(".thumbnail img").Each(func(_ int, img *goquery.Selection) {
			if h, err := goquery.OuterHtml(img); err == nil {
				body.WriteString(h)
			}
		})
		if body.Len() == 0 {
			return
		}
		div.BeforeHtml(`<details class="section print-edition"><summary>` + nethtml.EscapeString(title) +
			`</summary><div class="section-body">` + body.String() + `</div></details>`)
		run.Remove()
	})
	out, err := doc.Find("body").Html()
	if err != nil {
		return fragment
	}
	return out
}
