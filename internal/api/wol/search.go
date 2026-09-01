package wol

import (
	"context"
	"fmt"
	htmlpkg "html"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/dgrieser/jw-cli/internal/model"
)

// SearchOpts controls a wol library search.
type SearchOpts struct {
	Scope string // par (paragraph, default) or sen (sentence)
	Sort  string // occ (occurrences, default), newest, oldest
	Page  int    // 1-based
	// Categories is the fc[] publication-category whitelist. Nil sends no
	// filter at all, which is the site default: everything, bibles and the
	// indexes included.
	Categories []string
}

// AllCategories is every fc[] filter value wol offers. The set is
// language-dependent — German has "mwbr" and no "vern", English the other way
// round — but unknown values are ignored, so this one list serves as the
// starting point for every language until a search page reports the real one.
var AllCategories = []string{
	"bi", "gloss", "it", "dx", "w", "g", "km", "mwb", "mwbr", "vern",
	"bk", "yb", "syr", "sgbk", "brch", "es", "bklt", "trct", "kn", "pgm",
	"ca-copgm", "ca-brpgm", "co-pgm", "manual", "-none",
	"bibleteachings", "teenagers", "children", "publications",
	"newsroom", "aboutus", "web",
}

// The two categories a citation search is normally not interested in: the
// bible text itself and the indexes (concordance, bible-word index), which
// cite every verse by construction.
const (
	CategoryBibles = "bi"
	CategoryIndex  = "dx"
)

const (
	categoriesKey    = "wolfc1-"
	categoriesMaxAge = 30 * 24 * time.Hour
)

// search result selectors, grouped for cheap fixing on layout drift.
const (
	// one document, holding its caption, its matching passages and the
	// publication line they come from
	selResultDoc     = "ul.results.resultContentDocument"
	selResultCaption = "li.caption a"
	selResultPassage = "li.searchResult article .document"
	selResultRef     = "li.ref"
	selResultThumb   = "li.documentThumbnailItem img"
	selResultTotal   = "input#searchResultsTotal"
	selResultPageLen = "input#searchResultsPageSize"
	selResultFilters = `#searchFilterContainer a[id^="fc-"]`

	// older layout, kept as a fallback
	selSearchResult    = "li.searchResult, .searchResult"
	selOldCaption      = ".caption a, .cardTitleBlock a, h3 a"
	selOldSnippet      = ".searchResultDocument, .docContent, .des"
	selOldResultsCount = ".resultsCount, .searchesCount"
)

// snippetMax caps a result's excerpt, counted in runes of its text.
const snippetMax = 300

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
	for _, cat := range opts.Categories {
		v.Add("fc[]", cat)
	}
	u := c.url(cfg, "s", "") + "?" + v.Encode()
	doc, err := c.hc.GetHTML(ctx, u)
	if err != nil {
		return model.SearchPage{}, err
	}

	page := model.SearchPage{Query: query, Page: max(opts.Page, 1)}
	seen := map[string]bool{}
	add := func(title, href, snippet, ref, thumb string) {
		href = absURL(c.hc.Base.WOL, href)
		if title == "" || href == "" || seen[href] {
			return
		}
		seen[href] = true
		page.Results = append(page.Results, model.Result{
			Kind:     "article",
			Title:    title,
			Snippet:  snippet,
			Context:  ref,
			ImageURL: absURL(c.hc.Base.WOL, thumb),
			WOLLink:  href,
			DocID:    docIDFromURL(href),
		})
	}

	// current layout: caption, passages and publication line are siblings
	// under one results list per document
	doc.Find(selResultDoc).Each(func(_ int, ul *goquery.Selection) {
		link := ul.Find(selResultCaption).First()
		href, _ := link.Attr("href")
		thumb, _ := ul.Find(selResultThumb).First().Attr("src")
		add(cleanSpace(link.Text()), href,
			snippetOf(ul.Find(selResultPassage)),
			cleanSpace(ul.Find(selResultRef).First().Text()),
			strings.TrimSpace(thumb))
	})

	// older layout: everything inside one result element
	if len(page.Results) == 0 {
		doc.Find(selSearchResult).Each(func(_ int, li *goquery.Selection) {
			link := li.Find(selOldCaption).First()
			if link.Length() == 0 {
				link = li.Find(`a[href*="/wol/d/"]`).First()
			}
			href, _ := link.Attr("href")
			snippet := cleanSpace(li.Find(selOldSnippet).First().Text())
			if snippet == "" {
				full := cleanSpace(li.Text())
				title := cleanSpace(link.Text())
				snippet = strings.TrimSpace(strings.TrimPrefix(full, title))
			}
			add(cleanSpace(link.Text()), href, truncText(snippet), "", "")
		})
	}

	// fallback for layout drift: any document link in the main region
	if len(page.Results) == 0 {
		doc.Find(`main a[href*="/wol/d/"], #content a[href*="/wol/d/"]`).Each(func(_ int, a *goquery.Selection) {
			href, _ := a.Attr("href")
			add(cleanSpace(a.Text()), href, "", "", "")
		})
	}

	page.Total = intAttr(doc.Find(selResultTotal), "value")
	if page.Total == 0 {
		page.Total = firstNumber(cleanSpace(doc.Find(selOldResultsCount).First().Text()))
	}
	page.Limit = intAttr(doc.Find(selResultPageLen), "value")
	page.Filters = filterValues(doc)
	c.rememberCategories(cfg, page.Filters)
	return page, nil
}

// Categories returns the fc[] filter values wol offers in this language, as
// last seen on a search page. Nil when no search has been run for the language
// yet (AllCategories is the starting point in that case).
func (c *Client) Categories(cfg Config) []string {
	var out []string
	if c.cache.Get(categoriesKey+cfg.Locale, categoriesMaxAge, &out) {
		return out
	}
	return nil
}

func (c *Client) rememberCategories(cfg Config, fcs []string) {
	if len(fcs) == 0 {
		return
	}
	c.cache.Put(categoriesKey+cfg.Locale, fcs)
}

// filterValues reads the category codes off the "refine search" sidebar, which
// lists every fc[] value the language offers whether or not it is checked.
func filterValues(doc *goquery.Document) []string {
	var out []string
	doc.Find(selResultFilters).Each(func(_ int, a *goquery.Selection) {
		id, _ := a.Attr("id")
		code := strings.TrimPrefix(id, "fc-")
		if code != "" && !slices.Contains(out, code) {
			out = append(out, code)
		}
	})
	return out
}

// snippetOf joins the matching passages of one document, keeping their HTML so
// the listing can show the search highlights. Passages are taken until the cap
// is reached; a single passage longer than the cap is cut down to plain text.
func snippetOf(passages *goquery.Selection) string {
	var frags []string
	runes := 0
	passages.EachWithBreak(func(_ int, s *goquery.Selection) bool {
		frag, err := s.Html()
		if err != nil {
			return false
		}
		frag = cleanSpace(frag)
		if frag == "" {
			return true
		}
		n := len([]rune(cleanSpace(s.Text())))
		if runes+n > snippetMax {
			if runes == 0 {
				frags = append(frags, htmlpkg.EscapeString(truncText(cleanSpace(s.Text()))))
			}
			return false
		}
		frags = append(frags, frag)
		runes += n
		return true
	})
	return strings.Join(frags, " ")
}

// truncText cuts plain text to the snippet cap.
func truncText(s string) string {
	r := []rune(s)
	if len(r) <= snippetMax {
		return s
	}
	return string(r[:snippetMax]) + "…"
}

func intAttr(sel *goquery.Selection, attr string) int {
	v, ok := sel.First().Attr(attr)
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 0
	}
	return n
}

// firstNumber picks the count out of a localized "42 results" line.
func firstNumber(s string) int {
	for f := range strings.FieldsSeq(strings.ReplaceAll(s, ",", "")) {
		if n, err := strconv.Atoi(f); err == nil {
			return n
		}
	}
	return 0
}
