package wol

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
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
}

const (
	selNavBooks    = "ul.books"       // one section of the book grid
	selNavHeading  = "div.group.grid" // the heading printed before it
	selNavBook     = "li.book"        // one book, its class the group
	selNavBookLink = "a.bookLink"     // carrying data-bookid
	selNavBookName = "h1.navBook"     // the book's title, on the chapter grid
	selNavChapter  = "li.chapter a"   // one chapter link
	selNavPubTitle = "header h1"      // the bible's title, on the book grid
)

var chapterHref = regexp.MustCompile(`/b/[^/]+/[^/]+/[^/]+/(\d+)/(\d+)$`)

// BibleNav reads the book grid of an edition (/binav/{edition}). Cached for 30
// days.
func (c *Client) BibleNav(ctx context.Context, cfg Config, edition string) (BibleNav, error) {
	if edition == "" {
		edition = "nwtsty"
	}
	key := "binav-" + cfg.Locale + "-" + edition
	var cached BibleNav
	if c.cache.Get(key, 30*24*time.Hour, &cached) && len(cached.Sections) > 0 {
		return cached, nil
	}
	u := c.url(cfg, "binav", "/"+edition)
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
			for _, class := range strings.Fields(li.AttrOr("class", "")) {
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
	c.cache.Put(key, nav)
	return nav, nil
}

// BookNav reads the chapter grid of one book (/binav/{edition}/{book}).
// Cached for 30 days.
func (c *Client) BookNav(ctx context.Context, cfg Config, edition string, book int) (BookNav, error) {
	if edition == "" {
		edition = "nwtsty"
	}
	key := fmt.Sprintf("binav-%s-%s-%d", cfg.Locale, edition, book)
	var cached BookNav
	if c.cache.Get(key, 30*24*time.Hour, &cached) && len(cached.Chapters) > 0 {
		return cached, nil
	}
	u := c.url(cfg, "binav", fmt.Sprintf("/%s/%d", edition, book))
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
	if len(nav.Chapters) == 0 {
		return BookNav{}, fmt.Errorf("no chapters found at %s (book missing in %s, or page layout changed)", u, edition)
	}
	c.cache.Put(key, nav)
	return nav, nil
}
