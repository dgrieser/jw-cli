package wol

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/dgrieser/jw-cli/internal/model"
)

// SearchOpts controls a wol library search.
type SearchOpts struct {
	Scope string // par (paragraph, default) or sen (sentence)
	Sort  string // occ (occurrences, default), newest, oldest
	Page  int    // 1-based
}

// search result selectors, grouped for cheap fixing on layout drift.
const (
	selSearchResult  = "li.searchResult, .searchResult"
	selResultCaption = ".caption a, .cardTitleBlock a, h3 a"
	selResultSnippet = ".searchResultDocument, .docContent, .des"
	selResultsCount  = ".resultsCount, .searchesCount"
)

// Search runs the wol full-text search. wol supports special syntax passed
// through verbatim in the query: quoted phrases, * wildcards, & (AND),
// | (OR), and parenthesized scripture citations like (Matthew 24:14) to find
// documents citing that verse.
func (c *Client) Search(ctx context.Context, cfg Config, query string, opts SearchOpts) (model.SearchPage, error) {
	scope := opts.Scope
	switch scope {
	case "", "par":
		scope = "par"
	case "sen":
	default:
		return model.SearchPage{}, fmt.Errorf("invalid scope %q (want par or sen)", opts.Scope)
	}
	sort := opts.Sort
	switch sort {
	case "", "occ", "rel":
		sort = "occ"
	case "newest", "oldest":
	default:
		return model.SearchPage{}, fmt.Errorf("invalid wol sort %q (want occ, newest, or oldest)", opts.Sort)
	}
	v := url.Values{}
	v.Set("q", query)
	v.Set("p", scope)
	v.Set("r", sort)
	if opts.Page > 1 {
		v.Set("pg", strconv.Itoa(opts.Page))
	}
	u := c.url(cfg, "s", "") + "?" + v.Encode()
	doc, err := c.hc.GetHTML(ctx, u)
	if err != nil {
		return model.SearchPage{}, err
	}

	page := model.SearchPage{Query: query, Page: max(opts.Page, 1)}
	seen := map[string]bool{}
	add := func(title, href, snippet string) {
		href = absURL(c.hc.Base.WOL, href)
		if title == "" || href == "" || seen[href] {
			return
		}
		seen[href] = true
		page.Results = append(page.Results, model.Result{
			Kind:    "article",
			Title:   title,
			Snippet: snippet,
			WOLLink: href,
			DocID:   docIDFromURL(href),
		})
	}

	doc.Find(selSearchResult).Each(func(_ int, li *goquery.Selection) {
		link := li.Find(selResultCaption).First()
		if link.Length() == 0 {
			link = li.Find(`a[href*="/wol/d/"]`).First()
		}
		href, _ := link.Attr("href")
		snippet := cleanSpace(li.Find(selResultSnippet).First().Text())
		if snippet == "" {
			full := cleanSpace(li.Text())
			title := cleanSpace(link.Text())
			snippet = strings.TrimSpace(strings.TrimPrefix(full, title))
		}
		if len(snippet) > 300 {
			snippet = snippet[:300] + "…"
		}
		add(cleanSpace(link.Text()), href, snippet)
	})

	// fallback for layout drift: any document link in the main region
	if len(page.Results) == 0 {
		doc.Find(`main a[href*="/wol/d/"], #content a[href*="/wol/d/"]`).Each(func(_ int, a *goquery.Selection) {
			href, _ := a.Attr("href")
			add(cleanSpace(a.Text()), href, "")
		})
	}

	if txt := cleanSpace(doc.Find(selResultsCount).First().Text()); txt != "" {
		for _, f := range strings.Fields(strings.ReplaceAll(txt, ",", "")) {
			if n, err := strconv.Atoi(f); err == nil {
				page.Total = n
				break
			}
		}
	}
	return page, nil
}
