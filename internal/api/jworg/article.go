// Package jworg parses article pages of www.jw.org (magazine articles,
// news, library items reached from search results).
package jworg

import (
	"context"
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/dgrieser/jw-cli/internal/htmlx"
	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/model"
)

type Client struct {
	hc *httpx.Client
}

func New(hc *httpx.Client) *Client { return &Client{hc: hc} }

// ArticleByURL fetches and parses a www.jw.org article page.
func (c *Client) ArticleByURL(ctx context.Context, pageURL string) (model.Article, error) {
	doc, err := c.hc.GetHTML(ctx, pageURL)
	if err != nil {
		return model.Article{}, err
	}
	art := parse(doc, c.hc.Base.JWOrg)
	art.URL = pageURL
	if art.HTML == "" {
		return art, fmt.Errorf("no article content found at %s", pageURL)
	}
	return art, nil
}

// contentSelectors: jw.org keeps article text in <article>, body copy in
// .bodyTxt (missing on some pubs like Insight, where the whole article is
// the body).
var contentSelectors = []string{"main article", "article", "#article", ".docSubContent", "main"}

func parse(doc *goquery.Document, base string) model.Article {
	var art model.Article
	var container *goquery.Selection
	for _, sel := range contentSelectors {
		if s := doc.Find(sel).First(); s.Length() > 0 {
			container = s
			break
		}
	}
	if container == nil {
		return art
	}
	art.Title = clean(container.Find("h1").First().Text())
	if art.Title == "" {
		art.Title = clean(doc.Find("head title").Text())
	}
	// docid from the article element class list: "... docId-1102025912 ..."
	if cls, ok := container.Attr("class"); ok {
		for _, tok := range strings.Fields(cls) {
			if strings.HasPrefix(tok, "docId-") {
				_, _ = fmt.Sscanf(strings.TrimPrefix(tok, "docId-"), "%d", &art.DocID)
			}
		}
	}
	content := container.Find(".bodyTxt").First()
	if content.Length() == 0 {
		content = container
	}
	if html, err := goquery.OuterHtml(content); err == nil {
		art.HTML = html
	}
	art.Images = htmlx.Images(container, base)
	art.ScriptureRefs = htmlx.ScriptureRefs(container, base)
	return art
}

func clean(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
