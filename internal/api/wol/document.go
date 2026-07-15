package wol

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

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
	doc, err := c.hc.GetHTML(ctx, pageURL)
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

	title := doc.Find("h1").First()
	art.Title = cleanSpace(title.Text())
	if art.Title == "" {
		art.Title = cleanSpace(doc.Find("title").Text())
	}

	// drop non-content chrome inside the article container
	pruned := content.Clone()
	pruned.Find("#regionMain nav, .resultsNavigationSelected, #docSubMedia, .groupTOC, #contentColumnControl").Remove()
	html, err := goquery.OuterHtml(pruned)
	if err == nil {
		art.HTML = html
	}
	art.Images = htmlx.Images(content, base)
	art.ScriptureRefs = htmlx.ScriptureRefs(content, base)
	return art
}

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
