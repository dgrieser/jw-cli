package wol

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/dgrieser/jw-cli/internal/model"
)

// BibleEditions are well-known bible publication symbols for the /b/ command.
// Which of them a language actually carries differs per language, and a language
// may carry others still, so this is a hint for the flag help only — Bibles
// reports what the library really offers.
var BibleEditions = []string{"nwtsty", "nwt", "Rbi8", "int", "by", "bi22", "rh", "bi10"}

// BibleEdition is one bible available in a language, as the library's own bible
// list names it.
type BibleEdition struct {
	Symbol string `json:"symbol"` // publication symbol used in /b/ URLs ("nwtsty")
	Title  string `json:"title"`  // "New World Translation ... (Study Edition)"
	Year   string `json:"year"`   // the year printed beside the title, may be empty
}

// Label names an edition the way a heading should: its title with the symbol
// (and year, when the list gave one) after it, so the symbol --bible takes is
// always visible next to the translation it stands for.
func (e BibleEdition) Label() string {
	switch {
	case e.Title == "":
		return e.Symbol
	case e.Year == "":
		return fmt.Sprintf("%s (%s)", e.Title, e.Symbol)
	default:
		return fmt.Sprintf("%s (%s, %s)", e.Title, e.Symbol, e.Year)
	}
}

// ChapterDoc wraps a fetched bible chapter page. All selectors that mirror
// the live wol markup are grouped in the sel* constants below so layout
// drift is cheap to fix.
type ChapterDoc struct {
	doc     *goquery.Document
	base    string
	Book    int
	Chapter int
	URL     string
}

const (
	selVerse        = "span.v"                     // verse text segments, id=v{b}-{c}-{v}-{seg}
	selStudySection = "#studyDiscover div.section" // per-verse study material, data-key={b}-{c}-{v}
	selStudyNote    = ".studyNoteGroup li.item p"  // one study note paragraph
	selMarginalItem = ".group.marginal li.item"    // one cross-reference group
	selMarginalCite = ".marginal.title"            // inline citation list
	selMediaItem    = ".group.media li.item"       // one media entry
	selMediaImg     = "img.studyItemMedia"         // its thumbnail
	selMediaLink    = "a.directLinkItem"           // finder deep link
	selMediaGallery = "a.galleryItem"              // gallery page of a picture
	selResearchItem = ".group.index li.item"       // research guide entry
	selFootnoteItem = ".group.footnote li.item"    // footnotes (best effort)
	selSectionTitle = "h3.title"

	selBibleCard  = "li.resultAlternatePubTitle a" // one bible on the bible list
	selCardSymbol = "[data-pub-symbol]"            // its publication symbol
	selCardTitle  = ".cardLine1"                   // its title
	selCardDetail = ".cardTitleDetail"             // the year beside the title
)

// The two indexes a research entry can be listed under, as wol classes them.
const (
	classResearchGuide    = "ref-rsg"
	classPublicationIndex = "ref-dx"
)

// Chapter fetches one bible chapter page (with the study pane inlined).
func (c *Client) Chapter(ctx context.Context, cfg Config, edition string, book, chapter int) (*ChapterDoc, error) {
	if edition == "" {
		edition = "nwtsty"
	}
	u := c.url(cfg, "b", fmt.Sprintf("/%s/%d/%d", edition, book, chapter))
	doc, err := c.hc.GetHTML(ctx, u)
	if err != nil {
		return nil, err
	}
	cd := &ChapterDoc{doc: doc, base: c.hc.Base.WOL, Book: book, Chapter: chapter, URL: u}
	if cd.doc.Find(selVerse).Length() == 0 {
		return nil, fmt.Errorf("no verses found at %s (chapter missing in %s, or page layout changed)", u, edition)
	}
	return cd, nil
}

var verseID = regexp.MustCompile(`^v(\d+)-(\d+)-(\d+)-\d+$`)

// Verses returns verses from..to (inclusive); 0,0 means the whole chapter.
// Multi-segment verses are concatenated.
func (d *ChapterDoc) Verses(from, to int) ([]model.Verse, error) {
	if from == 0 {
		from, to = 1, 999
	}
	if to == 0 {
		to = from
	}
	order := []int{}
	byVerse := map[int]*strings.Builder{}
	d.doc.Find(selVerse).Each(func(_ int, s *goquery.Selection) {
		id, ok := s.Attr("id")
		if !ok {
			return
		}
		m := verseID.FindStringSubmatch(id)
		if m == nil {
			return
		}
		book, _ := strconv.Atoi(m[1])
		ch, _ := strconv.Atoi(m[2])
		v, _ := strconv.Atoi(m[3])
		if book != d.Book || ch != d.Chapter || v < from || v > to {
			return
		}
		html, err := s.Html()
		if err != nil {
			return
		}
		b, seen := byVerse[v]
		if !seen {
			b = &strings.Builder{}
			byVerse[v] = b
			order = append(order, v)
		}
		b.WriteString(html)
	})
	if len(order) == 0 {
		return nil, fmt.Errorf("verses %d-%d not found in chapter %d", from, to, d.Chapter)
	}
	verses := make([]model.Verse, 0, len(order))
	for _, v := range order {
		verses = append(verses, model.Verse{
			ID:   d.Book*1_000_000 + d.Chapter*1_000 + v,
			HTML: byVerse[v].String(),
		})
	}
	return verses, nil
}

// StudySection extracts everything the study pane attaches to one verse.
func (d *ChapterDoc) StudySection(verse int) (model.StudySection, bool) {
	key := fmt.Sprintf("%d-%d-%d", d.Book, d.Chapter, verse)
	sec := d.doc.Find(selStudySection).FilterFunction(func(_ int, s *goquery.Selection) bool {
		k, _ := s.Attr("data-key")
		return k == key
	}).First()
	if sec.Length() == 0 {
		return model.StudySection{}, false
	}
	out := model.StudySection{
		Verse: cleanSpace(sec.Find(selSectionTitle).First().Text()),
	}

	sec.Find(selStudyNote).Each(func(_ int, p *goquery.Selection) {
		html, err := p.Html()
		if err != nil || strings.TrimSpace(p.Text()) == "" {
			return
		}
		out.Notes = append(out.Notes, model.StudyNote{
			Lemma: cleanSpace(p.Find("strong").First().Text()),
			HTML:  html,
		})
	})

	sec.Find(selMarginalItem).Each(func(_ int, li *goquery.Selection) {
		cite := cleanSpace(li.Find(selMarginalCite).First().Text())
		if cite == "" {
			cite = cleanSpace(li.Find(".header").First().Text())
		}
		src, _ := li.Attr("data-src")
		if cite == "" && src == "" {
			return
		}
		out.XRefs = append(out.XRefs, model.CrossRef{Citation: cite, SourcePath: absURL(d.base, src)})
	})

	sec.Find(selMediaItem).Each(func(_ int, li *goquery.Selection) {
		asset := model.MediaAsset{Caption: cleanSpace(li.Find(".caption").First().Text())}
		if img := li.Find(selMediaImg).First(); img.Length() > 0 {
			// the study pane shows a thumbnail and names the full-size
			// rendition beside it; the picture is the latter
			thumb := absURL(d.base, img.AttrOr("src", ""))
			asset.URL = absURL(d.base, img.AttrOr("data-img-large-src", ""))
			if asset.URL == "" {
				asset.URL = thumb
			} else if thumb != asset.URL {
				asset.ThumbnailURL = thumb
			}
			asset.Alt = cleanSpace(img.AttrOr("alt", ""))
		}
		if a := li.Find(selMediaLink).First(); a.Length() > 0 {
			href, _ := a.Attr("href")
			asset.FinderLink = absURL(d.base, href)
		}
		// a picture links to its gallery page, which is where its caption and
		// rights line are written down (see GalleryItem)
		if a := li.Find(selMediaGallery).First(); a.Length() > 0 {
			href, _ := a.Attr("href")
			asset.SourceURL = absURL(d.base, href)
		}
		if asset.URL == "" && asset.FinderLink == "" {
			return
		}
		out.Media = append(out.Media, asset)
	})

	sec.Find(selResearchItem).Each(func(_ int, li *goquery.Selection) {
		// media entries live in their own group; skip anything without a link
		a := li.Find("a").First()
		if a.Length() == 0 {
			return
		}
		href, _ := a.Attr("href")
		item := model.ResearchItem{
			Title:  cleanSpace(a.Text()),
			Source: cleanSpace(li.Find(".subtitle").First().Text()),
			Kind:   researchKind(li),
		}
		if strings.Contains(href, "/pc/") {
			item.PCPath = absURL(d.base, href)
		} else {
			item.ArticleURL = absURL(d.base, href)
		}
		if item.Title == "" && item.PCPath == "" && item.ArticleURL == "" {
			return
		}
		out.Research = append(out.Research, item)
	})

	sec.Find(selFootnoteItem).Each(func(_ int, li *goquery.Selection) {
		if txt := cleanSpace(li.Text()); txt != "" {
			out.Footnotes = append(out.Footnotes, txt)
		}
	})

	return out, true
}

// researchKind names the index a research entry was listed under. The class is
// the same in every language, so it says what the localized group heading
// cannot; an entry in neither group is left unnamed rather than guessed at.
func researchKind(li *goquery.Selection) string {
	switch {
	case li.HasClass(classResearchGuide):
		return model.ResearchGuideItem
	case li.HasClass(classPublicationIndex):
		return model.PublicationIndexItem
	}
	return ""
}

// MarginalReference fetches the full text of one cross-reference group (the
// lazy-loaded data-src of a marginal item).
func (c *Client) MarginalReference(ctx context.Context, srcURL string) (string, error) {
	doc, err := c.hc.GetHTML(ctx, srcURL)
	if err != nil {
		return "", err
	}
	html, err := doc.Find("body").Html()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(html), nil
}

// Tooltip fetches a wol bc/pc citation endpoint as JSON (publication
// excerpts, verse popups).
func (c *Client) Tooltip(ctx context.Context, tcURL string) (model.Tooltip, error) {
	var resp struct {
		Items []struct {
			Title            string `json:"title"`
			Caption          string `json:"caption"`
			Content          string `json:"content"`
			URL              string `json:"url"`
			ImageURL         string `json:"imageUrl"`
			PublicationTitle string `json:"publicationTitle"`
		} `json:"items"`
	}
	// citation paths taken straight out of a document are relative, and carry a
	// locale segment that has to come off first
	tcURL = absURL(c.hc.Base.WOL, citationAPI(tcURL))
	if err := c.hc.GetJSON(ctx, tcURL, xhrHeaders(), &resp); err != nil {
		return model.Tooltip{}, err
	}
	if len(resp.Items) == 0 {
		return model.Tooltip{}, fmt.Errorf("no citation content at %s", tcURL)
	}
	it := resp.Items[0]
	return model.Tooltip{
		Title:            it.Title,
		Caption:          it.Caption,
		ContentHTML:      it.Content,
		URL:              absURL(c.hc.Base.WOL, it.URL),
		ImageURL:         absURL(c.hc.Base.WOL, it.ImageURL),
		PublicationTitle: it.PublicationTitle,
	}, nil
}

// LocalizedBookNames extracts the localized bible book names from the bible
// navigation page (best effort; cached for 30 days).
func (c *Client) LocalizedBookNames(ctx context.Context, cfg Config) (map[int][]string, error) {
	// v2: v1 entries hold run-together names ("JohannesJoh.Joh").
	key := "books2-" + cfg.Locale
	var cached map[int][]string
	if c.cache.Get(key, 30*24*time.Hour, &cached) && len(cached) > 0 {
		return cached, nil
	}
	doc, err := c.hc.GetHTML(ctx, c.url(cfg, "binav", ""))
	if err != nil {
		return nil, err
	}
	names := map[int][]string{}
	bookHref := regexp.MustCompile(`/(\d{1,2})$`)
	doc.Find("a").Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		if !strings.Contains(href, "/wol/") {
			return
		}
		m := bookHref.FindStringSubmatch(href)
		if m == nil {
			return
		}
		num, _ := strconv.Atoi(m[1])
		if num < 1 || num > 66 {
			return
		}
		if len(names[num]) > 0 {
			return
		}
		if v := bookNameVariants(a); len(v) > 0 {
			names[num] = v
		}
	})
	if len(names) < 60 {
		return nil, fmt.Errorf("could not extract localized book names for %s (found %d)", cfg.Locale, len(names))
	}
	c.cache.Put(key, names)
	return names, nil
}

// Bibles lists the bible editions the library carries in a language, in the
// order its own bible list prints them (the current translation first, older
// ones after it). Which editions exist is language specific — English has eight,
// German three — and the list moves, so it is read rather than hardcoded.
// Cached for 30 days.
func (c *Client) Bibles(ctx context.Context, cfg Config) ([]BibleEdition, error) {
	key := "bibles-" + cfg.Locale
	var cached []BibleEdition
	if c.cache.Get(key, 30*24*time.Hour, &cached) && len(cached) > 0 {
		return cached, nil
	}
	doc, err := c.hc.GetHTML(ctx, c.url(cfg, "bibles", ""))
	if err != nil {
		return nil, err
	}
	var out []BibleEdition
	seen := map[string]bool{}
	doc.Find(selBibleCard).Each(func(_ int, a *goquery.Selection) {
		sym := cleanSpace(a.Find(selCardSymbol).AttrOr("data-pub-symbol", ""))
		if sym == "" || seen[sym] {
			return
		}
		seen[sym] = true
		out = append(out, BibleEdition{
			Symbol: sym,
			Title:  cleanSpace(a.Find(selCardTitle).Text()),
			Year:   cleanSpace(a.Find(selCardDetail).Text()),
		})
	})
	if len(out) == 0 {
		return nil, fmt.Errorf("could not find the bible editions of %s at %s (page layout changed?)", cfg.Locale, c.url(cfg, "bibles", ""))
	}
	c.cache.Put(key, out)
	return out, nil
}

// bookNameVariants pulls the name forms out of one bible-navigation link.
// Each link holds them in separate adjacent spans with no whitespace between
// the tags, so a.Text() would run them together ("JohannesJoh.Joh"). The
// full name comes first: callers use variants[0] as the display name and the
// rest as lookup aliases.
func bookNameVariants(a *goquery.Selection) []string {
	var variants []string
	add := func(s string) {
		s = cleanSpace(s)
		if s == "" || len(s) > 40 || slices.Contains(variants, s) {
			return
		}
		variants = append(variants, s)
	}
	for _, sel := range []string{".name", ".abbreviation", ".official"} {
		a.Find(sel).Each(func(_ int, s *goquery.Selection) { add(s.Text()) })
	}
	if len(variants) == 0 {
		// unstructured link: the whole text is the only name we get
		add(a.AttrOr("title", ""))
		add(a.Text())
	}
	return variants
}

func xhrHeaders() map[string][]string {
	return map[string][]string{
		"X-Requested-With": {"XMLHttpRequest"},
		"Accept":           {"application/json, text/javascript, */*; q=0.01"},
	}
}

// citationLocale matches the locale segment wol puts in the citation links of a
// document, as in "/de/wol/pc/r10/lp-x/1204408/577/0".
var citationLocale = regexp.MustCompile(`/[\w-]+/wol/`)

// citationAPI turns a citation link into its JSON endpoint by dropping the
// locale segment: /de/wol/pc/... answers with a 307 to the target page, while
// /wol/pc/... answers with the passage itself. This is what wol's own tooltip
// code does, and it is what makes a citation resolvable at all. Only the first
// segment is replaced, and a path that already lacks one is left alone.
func citationAPI(path string) string {
	if loc := citationLocale.FindStringIndex(path); loc != nil {
		return path[:loc[0]] + "/wol/" + path[loc[1]:]
	}
	return path
}

func absURL(base, path string) string {
	if path == "" || strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return base + path
}
