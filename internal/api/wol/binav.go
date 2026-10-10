package wol

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/dgrieser/jw-cli/internal/httpx"
)

// NavBook is one book of a bible's navigation grid, as the library's own
// /binav/ page lists it.
type NavBook struct {
	Number       int    `json:"number"`       // 1-66
	Name         string `json:"name"`         // "Matthäus"
	Abbreviation string `json:"abbreviation"` // "Mat."
	Official     string `json:"official"`     // "Mat"
	// Group is the kind of book wol colours it by: pentateuch, historical,
	// poetic, prophetic, gospels, acts, letters, revelation.
	Group string `json:"group"`
	// Study marks a book the edition carries study notes for.
	Study bool `json:"study,omitempty"`
}

// NavSection is one part of the grid: the Hebrew-Aramaic or the Christian
// Greek Scriptures, under the heading the page gives it.
type NavSection struct {
	Key     string    `json:"key"`     // "hebrew", "greek"
	Heading string    `json:"heading"` // "HEBRÄISCH-ARAMÄISCHE SCHRIFTEN"
	Books   []NavBook `json:"books"`
}

// BibleNav is a bible's book grid.
type BibleNav struct {
	Edition  string       `json:"edition"`
	Title    string       `json:"title,omitempty"`
	Sections []NavSection `json:"sections"`
}

// BookNav is one book's chapter grid.
type BookNav struct {
	Edition  string `json:"edition"`
	Book     int    `json:"book"`
	Title    string `json:"title"` // "Das Evangelium nach Matthäus"
	Chapters []int  `json:"chapters"`
	// Links are the documents the library lists under the chapter grid: the
	// book's introduction, its outline or overview, its media gallery.
	Links []BookLink `json:"links,omitempty"`
}

// The kinds of document a book's page links to below its chapters.
const (
	BookIntroduction = "introduction"
	BookOutline      = "outline"
	BookOverview     = "overview"
	BookGallery      = "gallery"
)

// BookLink is one document listed below a book's chapters.
type BookLink struct {
	Kind  string `json:"kind"`  // BookIntroduction, BookOutline, BookOverview or BookGallery
	Title string `json:"title"` // as the library names it: "Introduction to Matthew"
	URL   string `json:"url"`
}

const (
	selNavBooks     = "ul.books"       // one section of the book grid
	selNavHeading   = "div.group.grid" // the heading printed before it
	selNavBook      = "li.book"        // one book, its class the group
	selNavBookLink  = "a.bookLink"     // carrying data-bookid
	selNavBookName  = "h1.navBook"     // the book's title, on the chapter grid
	selNavChapter   = "li.chapter a"   // one chapter link
	selNavPubTitle  = "header h1"
	selNavBookLinks = "ul.bibleAdditionalLinks li a" // introduction, outline, gallery      // the bible's title, on the book grid
)

var chapterHref = regexp.MustCompile(`/b/[^/]+/[^/]+/[^/]+/(\d+)/(\d+)$`)

// BibleNav reads the book grid of an edition (/binav/{edition}), kept until
// the page changes upstream.
func (c *Client) BibleNav(ctx context.Context, cfg Config, edition string) (BibleNav, error) {
	if edition == "" {
		edition = "nwtsty"
	}
	u := c.url(cfg, "binav", "/"+edition)
	return httpx.Memo(ctx, c.cache, "binav2-"+u, func(ctx context.Context) (BibleNav, error) {
		return c.bibleNav(ctx, u, edition)
	})
}

func (c *Client) bibleNav(ctx context.Context, u, edition string) (BibleNav, error) {
	doc, err := c.hc.GetHTML(ctx, u)
	if err != nil {
		return BibleNav{}, err
	}
	nav := BibleNav{Edition: edition, Title: cleanSpace(doc.Find(selNavPubTitle).First().Text())}
	doc.Find(selNavBooks).Each(func(_ int, ul *goquery.Selection) {
		sec := NavSection{Heading: cleanSpace(ul.PrevFiltered(selNavHeading).Text())}
		switch {
		case ul.HasClass("hebrew"):
			sec.Key = "hebrew"
		case ul.HasClass("greek"):
			sec.Key = "greek"
		}
		ul.Find(selNavBook).Each(func(_ int, li *goquery.Selection) {
			a := li.Find(selNavBookLink).First()
			num, err := strconv.Atoi(a.AttrOr("data-bookid", ""))
			if err != nil || num < 1 || num > 66 {
				return
			}
			b := NavBook{
				Number:       num,
				Name:         cleanSpace(a.Find(".name").First().Text()),
				Abbreviation: cleanSpace(a.Find(".abbreviation").First().Text()),
				Official:     cleanSpace(a.Find(".official").First().Text()),
			}
			for class := range strings.FieldsSeq(li.AttrOr("class", "")) {
				switch class {
				case "book":
				case "study":
					b.Study = true
				default:
					if b.Group == "" {
						b.Group = class
					}
				}
			}
			if b.Name == "" {
				b.Name = cleanSpace(a.Text())
			}
			sec.Books = append(sec.Books, b)
		})
		if len(sec.Books) > 0 {
			nav.Sections = append(nav.Sections, sec)
		}
	})
	if len(nav.Sections) == 0 {
		return BibleNav{}, fmt.Errorf("no bible books found at %s (edition missing, or page layout changed)", u)
	}
	return nav, nil
}

// BookNav reads the chapter grid of one book (/binav/{edition}/{book}), kept
// until the page changes upstream.
func (c *Client) BookNav(ctx context.Context, cfg Config, edition string, book int) (BookNav, error) {
	if edition == "" {
		edition = "nwtsty"
	}
	u := c.url(cfg, "binav", fmt.Sprintf("/%s/%d", edition, book))
	return httpx.Memo(ctx, c.cache, "binav3-"+u, func(ctx context.Context) (BookNav, error) {
		return c.bookNav(ctx, u, edition, book)
	})
}

func (c *Client) bookNav(ctx context.Context, u, edition string, book int) (BookNav, error) {
	doc, err := c.hc.GetHTML(ctx, u)
	if err != nil {
		return BookNav{}, err
	}
	nav := BookNav{Edition: edition, Book: book, Title: cleanSpace(doc.Find(selNavBookName).First().Text())}
	seen := map[int]bool{}
	doc.Find(selNavChapter).Each(func(_ int, a *goquery.Selection) {
		m := chapterHref.FindStringSubmatch(a.AttrOr("href", ""))
		if m == nil {
			return
		}
		if b, _ := strconv.Atoi(m[1]); b != book {
			return
		}
		ch, _ := strconv.Atoi(m[2])
		if ch > 0 && !seen[ch] {
			seen[ch] = true
			nav.Chapters = append(nav.Chapters, ch)
		}
	})
	doc.Find(selNavBookLinks).Each(func(_ int, a *goquery.Selection) {
		href := a.AttrOr("href", "")
		kind := bookLinkKind(href)
		if kind == "" {
			return
		}
		title := cleanSpace(a.Find(".title").First().Text())
		if title == "" {
			title = cleanSpace(a.Text())
		}
		nav.Links = append(nav.Links, BookLink{Kind: kind, Title: title, URL: absURL(c.hc.Base.WOL, href)})
	})
	if len(nav.Chapters) == 0 {
		return BookNav{}, fmt.Errorf("no chapters found at %s (book missing in %s, or page layout changed)", u, edition)
	}
	return nav, nil
}

// bookLinkKind tells the documents below a book's chapters apart by their
// address, which is the same in every language: /bibledocument/.../introduction,
// .../outline, .../overview, and /gallery/... for the media gallery.
func bookLinkKind(href string) string {
	if strings.Contains(href, "/gallery/") {
		return BookGallery
	}
	if !strings.Contains(href, "/bibledocument/") {
		return ""
	}
	switch kind := href[strings.LastIndexByte(href, '/')+1:]; kind {
	case BookIntroduction, BookOutline, BookOverview:
		return kind
	}
	return ""
}
