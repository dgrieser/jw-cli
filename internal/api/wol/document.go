package wol

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"

	"github.com/dgrieser/jw-cli/internal/htmlx"
	"github.com/dgrieser/jw-cli/internal/model"
)

// Document fetches article docid from the library.
func (c *Client) Document(ctx context.Context, cfg Config, docid int) (model.Article, error) {
	return c.DocumentByURL(ctx, c.url(cfg, "d", fmt.Sprintf("/%d", docid)))
}

// DocumentByURL fetches and parses any wol document page (including finder
// redirect URLs from search results).
func (c *Client) DocumentByURL(ctx context.Context, pageURL string) (model.Article, error) {
	doc, err := c.documentPage(ctx, pageURL)
	if err != nil {
		return model.Article{}, err
	}
	art := parseDocument(doc, c.hc.Base.WOL)
	art.URL = pageURL
	if art.DocID == 0 {
		art.DocID = docIDFromURL(pageURL)
	}
	if art.HTML == "" {
		return art, fmt.Errorf("no article content found at %s", pageURL)
	}
	return art, nil
}

// documentPage fetches a document page. The response cache keeps it, so the
// second reading of the same publication — a listing that quotes it, then jw
// show on one of its rows — costs no request.
func (c *Client) documentPage(ctx context.Context, pageURL string) (*goquery.Document, error) {
	return c.hc.GetHTML(ctx, pageURL)
}

// selDocLocation is the link to the library page a document is filed under.
const selDocLocation = "li.documentLocation a[href]"

// contentSelectors are tried in order to find the document body; kept
// together so live layout drift is cheap to fix.
var contentSelectors = []string{"#article", "article", ".bodyTxt", "main"}

func parseDocument(doc *goquery.Document, base string) model.Article {
	var art model.Article

	var content *goquery.Selection
	for _, sel := range contentSelectors {
		if s := doc.Find(sel).First(); s.Length() > 0 {
			content = s
			break
		}
	}
	if content == nil {
		return art
	}

	art.Title = documentTitle(doc, content)

	// drop non-content chrome inside the article container: navigation, and
	// what the page's scripts work with — the image viewer's lightbox, the
	// hidden fields they read their settings from, comments
	pruned := content.Clone()
	pruned.Find("#regionMain nav, .resultsNavigationSelected, #docSubMedia, .groupTOC, #contentColumnControl, " +
		todayChrome + ", " + scriptChrome).Remove()
	for _, n := range pruned.Nodes {
		dropComments(n)
	}
	if out, err := goquery.OuterHtml(pruned); err == nil {
		art.HTML = out
	}
	art.Images = htmlx.Images(content, base)
	art.ScriptureRefs = htmlx.ScriptureRefs(content, base)
	// where the library files it: the navigation's entry for the document
	if a := doc.Find(selDocLocation).First(); a.Length() > 0 {
		if shelf := cardLink(a.AttrOr("href", ""), base); shelf.Kind == LibraryKind || shelf.Kind == PublicationKind {
			art.ShelfKind, art.ShelfPath = shelf.Kind, shelf.Path
		}
	}
	return art
}

// scriptChrome is what a document page carries for its scripts inside the
// article: the lightbox wol builds its image viewer from, and the form
// controls holding the page's settings (the share link, the pub-media
// endpoint) and the viewer's buttons. A study article's answer fields
// (textarea) belong to the document and stay.
const scriptChrome = ".pswp, input, button"

// dropComments removes the HTML comments under n.
func dropComments(n *html.Node) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		if c.Type == html.CommentNode {
			n.RemoveChild(c)
		} else {
			dropComments(c)
		}
		c = next
	}
}

// selSiteName is the library's name in the banner of every page, in the page's
// language: what its <title> ends in.
const selSiteName = ".titleBig"

// documentTitle is the document's own heading, or failing one — the meetings
// page has none, nor have some search hits — the page's <title> without the
// library's name the site appends to it in every language ("He Fought Loyally
// for His God — Watchtower ONLINE LIBRARY", "Johannes 3 — Wachtturm
// ONLINE-BIBLIOTHEK"). A page titled with that name alone has no title of its
// own and gets none.
func documentTitle(doc *goquery.Document, content *goquery.Selection) string {
	for _, h := range []*goquery.Selection{content.Find("h1").First(), doc.Find("h1").First()} {
		if t := cleanSpace(h.Text()); t != "" {
			return t
		}
	}
	return stripSiteName(cleanSpace(doc.Find("title").Text()), cleanSpace(doc.Find(selSiteName).First().Text()))
}

// stripSiteName takes the library's name off a page title. Without the banner
// to say what the name is, it is what follows the last dash.
func stripSiteName(title, site string) string {
	if site != "" {
		if t, ok := strings.CutSuffix(title, site); ok {
			return strings.TrimRight(t, " —–-|")
		}
		return title
	}
	if i := strings.LastIndex(title, " — "); i >= 0 {
		return strings.TrimSpace(title[:i])
	}
	return title
}

// DocIDFromURL is the document id a wol link points at, or zero when it points
// at something else. Callers outside this package match a search result against
// a citation with it.
func DocIDFromURL(u string) int { return docIDFromURL(u) }

var docIDPattern = regexp.MustCompile(`(?:/d/[^/]+/[^/]+/|docid=)(\d+)`)

func docIDFromURL(u string) int {
	if m := docIDPattern.FindStringSubmatch(u); m != nil {
		id, _ := strconv.Atoi(m[1])
		return id
	}
	// /{loc}/wol/d/r1/lp-e/1102025912 -> last path segment
	parts := strings.Split(strings.TrimRight(u, "/"), "/")
	if len(parts) > 0 {
		if id, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
			return id
		}
	}
	return 0
}

func cleanSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// passageRange matches the extent a citation's link names on its document:
// "#h=43:0-48:0", from paragraph 43 to paragraph 48.
var passageRange = regexp.MustCompile(`[#&]h=(\d+):\d+-(\d+):\d+`)

// Passage reads the paragraphs a citation link names out of the document it
// points at. wol answers some citations — a box of a book, a sidebar — with a
// title and a link but no content; the paragraphs are on the document all the
// same. Empty when the link names no extent or the document has none of it.
func (c *Client) Passage(ctx context.Context, passageURL string) (string, error) {
	m := passageRange.FindStringSubmatch(passageURL)
	if m == nil {
		return "", nil
	}
	from, _ := strconv.Atoi(m[1])
	to, _ := strconv.Atoi(m[2])
	if to < from {
		return "", nil
	}
	page := passageURL
	if i := strings.IndexByte(page, '#'); i >= 0 {
		page = page[:i]
	}
	doc, err := c.documentPage(ctx, page)
	if err != nil {
		return "", err
	}
	inRange := func(s *goquery.Selection) bool {
		pid, err := strconv.Atoi(s.AttrOr("data-pid", ""))
		return err == nil && pid >= from && pid <= to
	}
	var b strings.Builder
	doc.Find("[data-pid]").Each(func(_ int, s *goquery.Selection) {
		// the outermost element of the extent only: a paragraph inside a box
		// that is itself in the extent comes with the box
		if !inRange(s) || s.ParentsFiltered("[data-pid]").FilterFunction(func(_ int, p *goquery.Selection) bool {
			return inRange(p)
		}).Length() > 0 {
			return
		}
		if html, err := goquery.OuterHtml(s); err == nil {
			b.WriteString(html)
		}
	})
	return b.String(), nil
}
