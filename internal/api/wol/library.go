package wol

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/dgrieser/jw-cli/internal/httpx"
)

// The two kinds of page the library's publication tree is made of. A library
// page is a shelf — the categories, a category's years, a year's editions and
// months — and a publication page is one publication's table of contents. Both
// list their entries as the same cards; an issue at the bottom of the tree is
// a library page whose cards are its articles.
const (
	LibraryKind     = "library"
	PublicationKind = "publication"
	DocumentKind    = "document"
)

// LibraryCard is one entry of a library or publication page.
type LibraryCard struct {
	// Kind is what the card leads to: LibraryKind, PublicationKind or
	// DocumentKind; anything else (a bible chapter, ...) is "link".
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	// Path is the target below the language part of a wol URL
	// ("all-publications/watchtower" for a library page, "lff" or "lff/21" for
	// a publication), unescaped.
	Path      string `json:"path,omitempty"`
	DocID     int    `json:"docid,omitempty"`
	URL       string `json:"url"`
	Thumbnail string `json:"thumbnail,omitempty"`
	// Icon is the kind of publication wol draws the card with ("w", "bk",
	// "mwb"), the same in every language.
	Icon string `json:"icon,omitempty"`
}

// LibraryGroup is a run of cards under one heading ("LESSONS"). The cards
// before the first heading come in a group without one.
type LibraryGroup struct {
	Title string        `json:"title,omitempty"`
	Cards []LibraryCard `json:"cards"`
}

// LibraryTab is one part of a publication whose table of contents is split
// over several pages ("SECTION 2").
type LibraryTab struct {
	Title    string `json:"title"`
	Path     string `json:"path"`
	Selected bool   `json:"selected,omitempty"`
}

// LibraryPage is one page of the publication tree.
type LibraryPage struct {
	Kind  string `json:"kind"` // LibraryKind or PublicationKind
	Path  string `json:"path"`
	Title string `json:"title,omitempty"`
	URL   string `json:"url"`
	// Parent is the page one level up, as the page's back link names it; nil
	// at the top of the tree.
	Parent    *LibraryCard   `json:"parent,omitempty"`
	Thumbnail string         `json:"thumbnail,omitempty"`
	Tabs      []LibraryTab   `json:"tabs,omitempty"`
	Groups    []LibraryGroup `json:"groups"`
	// Symbol and Issue are what the publication media API knows the page's
	// publication by ("w", "202405"), when the page is one publication.
	Symbol string `json:"symbol,omitempty"`
	Issue  string `json:"issue,omitempty"`
	Year   string `json:"year,omitempty"`
	// Bible marks a bible's page, which wol lays out as a book grid.
	Bible bool `json:"bible,omitempty"`
}

// Cards is every card of the page, groups flattened.
func (p LibraryPage) Cards() []LibraryCard {
	var out []LibraryCard
	for _, g := range p.Groups {
		out = append(out, g.Cards...)
	}
	return out
}

const (
	selLibTitle    = "#article h1"
	selLibBack     = "#article h1 a.backNav"
	selLibTabs     = "ul.pageList li"
	selLibDir      = "ul.directory:not(.pageList)"
	selLibCardLink = "a.cardContainer"
	selLibLine1    = ".cardLine1"
	selLibLine2    = ".cardLine2"
	selLibThumb    = "img.cardThumbnailImage"
)

// libraryHref splits a wol href into its command and the rest of its path:
// /en/wol/library/r1/lp-e/all-publications/books -> library,
// all-publications/books.
var libraryHref = regexp.MustCompile(`^(?:https?://[^/]+)?/[^/]+/wol/([a-z]+)/r\d+/lp-[^/]+/?([^?#]*)`)

// Library reads one page of the publication tree. kind is LibraryKind or
// PublicationKind; an empty library path is the top of the tree, the list of
// categories. Kept until the page changes upstream.
func (c *Client) Library(ctx context.Context, cfg Config, kind, path string) (LibraryPage, error) {
	if kind != LibraryKind && kind != PublicationKind {
		return LibraryPage{}, errors.New("wol: library kind must be library or publication")
	}
	path = strings.Trim(path, "/")
	if kind == PublicationKind && path == "" {
		return LibraryPage{}, errors.New("wol: publication path required")
	}
	rest := ""
	if path != "" {
		rest = "/" + escapePath(path)
	}
	u := c.url(cfg, kind, rest)
	return httpx.Memo(ctx, c.cache, "library3-"+u, func(ctx context.Context) (LibraryPage, error) {
		doc, err := c.hc.GetHTML(ctx, u)
		if err != nil {
			return LibraryPage{}, err
		}
		page := parseLibrary(doc.Selection, c.hc.Base.WOL)
		page.Kind, page.Path, page.URL = kind, path, u
		return page, nil
	})
}

// escapePath escapes each segment of an unescaped path: library paths carry
// the words of their language ("worterklärungen").
func escapePath(path string) string {
	segs := strings.Split(path, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

func parseLibrary(sel *goquery.Selection, base string) LibraryPage {
	page := LibraryPage{Groups: []LibraryGroup{}}
	hidden := func(id string) string {
		return strings.TrimSpace(sel.Find("input#"+id).First().AttrOr("value", ""))
	}
	page.Title = cleanSpace(sel.Find(selLibTitle).First().Text())
	if page.Title == "" {
		page.Title = hidden("contentTitle")
	}
	if back := sel.Find(selLibBack).First(); back.Length() > 0 {
		if parent := cardLink(back.AttrOr("href", ""), base); parent.Kind != "link" {
			page.Parent = &parent
		}
	}
	page.Thumbnail = absURL(base, hidden("contentThumbnail"))
	// a publication names itself by its English symbol, an issue by the
	// periodical's symbol and its date
	if sym := hidden("issueSymbol"); sym != "" {
		page.Symbol, page.Issue = sym, hidden("issueDate")
	} else {
		page.Symbol = hidden("englishSym")
	}
	page.Year = hidden("pubYear")
	page.Bible = hidden("shareType") == "bible-nav"

	sel.Find(selLibTabs).Each(func(_ int, li *goquery.Selection) {
		a := li.Find("a").First()
		link := cardLink(a.AttrOr("href", ""), base)
		if link.Path == "" {
			return
		}
		page.Tabs = append(page.Tabs, LibraryTab{
			Title:    cleanSpace(a.Text()),
			Path:     link.Path,
			Selected: li.HasClass("selected"),
		})
	})

	sel.Find(selLibDir).Each(func(_ int, ul *goquery.Selection) {
		loose := LibraryGroup{}
		ul.Children().Each(func(_ int, li *goquery.Selection) {
			if li.HasClass("group") {
				g := LibraryGroup{Title: cleanSpace(li.ChildrenFiltered("div.title").First().Text())}
				li.Find("li.card").Each(func(_ int, card *goquery.Selection) {
					if c, ok := parseCard(card, base); ok {
						g.Cards = append(g.Cards, c)
					}
				})
				if len(loose.Cards) > 0 {
					page.Groups = append(page.Groups, loose)
					loose = LibraryGroup{}
				}
				if len(g.Cards) > 0 {
					page.Groups = append(page.Groups, g)
				}
				return
			}
			if !li.HasClass("card") {
				return
			}
			if c, ok := parseCard(li, base); ok {
				loose.Cards = append(loose.Cards, c)
			}
		})
		if len(loose.Cards) > 0 {
			page.Groups = append(page.Groups, loose)
		}
	})
	return page
}

func parseCard(li *goquery.Selection, base string) (LibraryCard, bool) {
	a := li.Find(selLibCardLink).First()
	if a.Length() == 0 {
		a = li.Find("a").First()
	}
	href := a.AttrOr("href", "")
	if href == "" {
		return LibraryCard{}, false
	}
	card := cardLink(href, base)
	card.Title = cleanSpace(a.Find(selLibLine1).First().Text())
	card.Subtitle = cleanSpace(a.Find(selLibLine2).First().Text())
	if card.Title == "" {
		card.Title, card.Subtitle = card.Subtitle, ""
	}
	if img := a.Find(selLibThumb).First(); img.Length() > 0 {
		card.Thumbnail = absURL(base, img.AttrOr("src", ""))
	}
	for class := range strings.FieldsSeq(a.Find(".cardThumbnailImage").First().AttrOr("class", "")) {
		if icon, ok := strings.CutPrefix(class, "icon-"); ok && icon != "" {
			card.Icon = icon
			break
		}
	}
	return card, card.Title != ""
}

// cardLink reads what a wol href leads to.
func cardLink(href, base string) LibraryCard {
	card := LibraryCard{Kind: "link", URL: absURL(base, href)}
	m := libraryHref.FindStringSubmatch(href)
	if m == nil {
		return card
	}
	rest, err := url.PathUnescape(strings.Trim(m[2], "/"))
	if err != nil {
		rest = strings.Trim(m[2], "/")
	}
	switch m[1] {
	case "library":
		card.Kind, card.Path = LibraryKind, rest
	case "publication":
		card.Kind, card.Path = PublicationKind, rest
	case "d":
		first, _, _ := strings.Cut(rest, "/")
		if id, err := strconv.Atoi(first); err == nil {
			card.Kind, card.DocID = DocumentKind, id
		}
	}
	return card
}
