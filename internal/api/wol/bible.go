package wol

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"

	"github.com/dgrieser/jw-cli/internal/httpx"
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
	selVerse            = "span.v"                     // verse text segments, id=v{b}-{c}-{v}-{seg}
	selVerseBlock       = "p, div, li, blockquote, td" // the paragraph or line a segment is laid out in
	selStudySection     = "#studyDiscover div.section" // per-verse study material, data-key={b}-{c}-{v}
	selStudyNote        = ".studyNoteGroup li.item p"  // one study note paragraph
	selMarginalItem     = ".group.marginal li.item"    // one cross-reference group
	selMarginalCite     = ".marginal.title"            // inline citation list
	selMediaItem        = ".group.media li.item"       // one media entry
	selMediaImg         = "img.studyItemMedia"         // its thumbnail
	selMediaLink        = "a.directLinkItem"           // finder deep link
	selMediaGallery     = "a.galleryItem"              // gallery page of a picture
	selResearchItem     = ".group.index li.item"       // one index's entries
	selResearchSubtitle = ".subtitle"                  // the index that listed them
	selFootnoteItem     = ".group.footnote li.item"    // footnotes (best effort)
	selSectionTitle     = "h3.title"

	selBibleCard  = "li.resultAlternatePubTitle a" // one bible on the bible list
	selCardSymbol = "[data-pub-symbol]"            // its publication symbol
	selCardTitle  = ".cardLine1"                   // its title
	selCardDetail = ".cardTitleDetail"             // the year beside the title
)

// The classes of a line of poetry: wol sets each line of a psalm or a proverb
// in a paragraph of its own, the line a verse begins on classed "sl" and every
// line after it "sz". Prose is set in paragraphs of several verses each.
var poetryLineClasses = []string{"sl", "sz"}

// The two indexes a research entry can be listed under, as wol classes them.
const (
	classResearchGuide    = "ref-rsg"
	classPublicationIndex = "ref-dx"
)

// ErrNoVerses is a chapter page without a verse on it: a chapter the edition
// does not have, served as an empty page rather than refused (or a page whose
// layout changed).
var ErrNoVerses = errors.New("no verses on the chapter page")

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
		return nil, fmt.Errorf("%w at %s (chapter missing in %s, or page layout changed)", ErrNoVerses, u, edition)
	}
	return cd, nil
}

var verseID = regexp.MustCompile(`^v(\d+)-(\d+)-(\d+)-\d+$`)

// VerseCount is the number of the last verse of a chapter in an edition, as
// its page numbers them: what a reference to the chapter can name. It is kept
// as a memo of its own, the page read once.
func (c *Client) VerseCount(ctx context.Context, cfg Config, edition string, book, chapter int) (int, error) {
	if edition == "" {
		edition = "nwtsty"
	}
	u := c.url(cfg, "b", fmt.Sprintf("/%s/%d/%d", edition, book, chapter))
	return httpx.Memo(ctx, c.cache, "verses1-"+u, func(ctx context.Context) (int, error) {
		d, err := c.Chapter(ctx, cfg, edition, book, chapter)
		if err != nil {
			return 0, err
		}
		return d.LastVerse(), nil
	})
}

// LastVerse is the highest verse number of the chapter on its page.
func (d *ChapterDoc) LastVerse() int {
	last := 0
	d.eachPart(func(v int, _ *goquery.Selection) { last = max(last, v) })
	return last
}

// lineBreak is what parts of one verse that wol sets in separate paragraphs
// are joined with: a line of poetry stays a line of its own in every format.
const lineBreak = "<br/>"

// Verses returns verses from..to (inclusive); 0,0 means the whole chapter.
// The segments of a verse are concatenated, and segments set in different
// paragraphs — the lines of a verse of poetry — are joined with a line break,
// so the lines are not run together.
func (d *ChapterDoc) Verses(from, to int) ([]model.Verse, error) {
	if from == 0 {
		from, to = 1, 999
	}
	if to == 0 {
		to = from
	}
	type verseText struct {
		b     strings.Builder
		block *html.Node // the paragraph the last segment was in
	}
	order := []int{}
	byVerse := map[int]*verseText{}
	d.eachPart(func(v int, s *goquery.Selection) {
		if v < from || v > to {
			return
		}
		fragment, err := s.Html()
		if err != nil {
			return
		}
		block := verseBlockOf(s)
		t, seen := byVerse[v]
		if !seen {
			t = &verseText{}
			byVerse[v] = t
			order = append(order, v)
		} else if block != t.block {
			t.b.WriteString(lineBreak)
		}
		if block != t.block {
			// a line of poetry is indented with a no-break space ahead of its
			// verse number, which only a page set line by line has use for
			fragment = strings.TrimLeftFunc(fragment, unicode.IsSpace)
		}
		t.block = block
		t.b.WriteString(fragment)
	})
	if len(order) == 0 {
		return nil, fmt.Errorf("verses %d-%d not found in chapter %d", from, to, d.Chapter)
	}
	verses := make([]model.Verse, 0, len(order))
	for _, v := range order {
		verses = append(verses, model.Verse{
			ID:   d.Book*1_000_000 + d.Chapter*1_000 + v,
			HTML: byVerse[v].b.String(),
		})
	}
	return verses, nil
}

// StartsLine reports whether a verse begins a line of its own when the verses
// of a passage are printed one after the other: a verse of poetry opens a new
// line, and so does the prose that follows poetry. A verse of prose that
// follows prose runs on in its paragraph, as the page prints it.
func (d *ChapterDoc) StartsLine(verse int) bool {
	var prev *html.Node // the paragraph the segment before the verse is in
	starts := false
	d.eachPartUntil(func(v int, s *goquery.Selection) bool {
		block := verseBlockOf(s)
		if v != verse {
			prev = block
			return true
		}
		// a verse that shares its paragraph with the one before it continues
		// that paragraph, whatever kind of paragraph it is
		starts = block != nil && block != prev && (isPoetryLine(block) || isPoetryLine(prev))
		return false
	})
	return starts
}

// eachPart calls fn with every verse segment of the chapter, in page order.
func (d *ChapterDoc) eachPart(fn func(verse int, s *goquery.Selection)) {
	d.eachPartUntil(func(v int, s *goquery.Selection) bool {
		fn(v, s)
		return true
	})
}

// eachPartUntil is eachPart that stops once fn returns false.
func (d *ChapterDoc) eachPartUntil(fn func(verse int, s *goquery.Selection) bool) {
	d.doc.Find(selVerse).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		m := verseID.FindStringSubmatch(s.AttrOr("id", ""))
		if m == nil {
			return true
		}
		book, _ := strconv.Atoi(m[1])
		ch, _ := strconv.Atoi(m[2])
		v, _ := strconv.Atoi(m[3])
		if book != d.Book || ch != d.Chapter {
			return true
		}
		return fn(v, s)
	})
}

// verseBlockOf is the paragraph a verse segment is set in, nil when it is in none.
func verseBlockOf(s *goquery.Selection) *html.Node {
	if b := s.Closest(selVerseBlock); b.Length() > 0 {
		return b.Get(0)
	}
	return nil
}

// isPoetryLine reports whether a paragraph is one line of poetry.
func isPoetryLine(n *html.Node) bool {
	if n == nil {
		return false
	}
	for _, a := range n.Attr {
		if a.Key == "class" {
			return slices.ContainsFunc(strings.Fields(a.Val), func(c string) bool {
				return slices.Contains(poetryLineClasses, c)
			})
		}
	}
	return false
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
		// one item per index, holding every entry that index lists — and a
		// single entry is spelled over several links, the publication and the
		// issue each carrying one, all pointing at the same passage
		source := cleanSpace(li.Find(selResearchSubtitle).First().Text())
		kind := researchKind(li)
		var last *model.ResearchItem
		li.Find("a").Each(func(_ int, a *goquery.Selection) {
			href, _ := a.Attr("href")
			href = absURL(d.base, href)
			text := cleanSpace(a.Text())
			if href == "" && text == "" {
				return
			}
			if last != nil && last.SameTarget(href) {
				last.Title = cleanSpace(last.Title + " " + text)
				return
			}
			item := model.ResearchItem{Title: text, Source: source, Kind: kind}
			if strings.Contains(href, "/pc/") {
				item.PCPath = href
			} else {
				item.ArticleURL = href
			}
			out.Research = append(out.Research, item)
			last = &out.Research[len(out.Research)-1]
		})
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
// lazy-loaded data-src of a marginal item). Like a citation, it answers with
// the passage only when asked without the locale segment — with it, the whole
// page comes back — and it answers with JSON: one entry per verse the group
// refers to.
func (c *Client) MarginalReference(ctx context.Context, srcURL string) (string, error) {
	var items []struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	u := absURL(c.hc.Base.WOL, citationAPI(srcURL))
	if err := c.hc.GetJSON(ctx, u, xhrHeaders(), &items); err != nil {
		return "", err
	}
	var b strings.Builder
	for _, it := range items {
		b.WriteString(it.Content)
	}
	if b.Len() == 0 {
		return "", fmt.Errorf("no marginal reference content at %s", u)
	}
	return relocalize(strings.TrimSpace(b.String()), citationLocaleOf(srcURL)), nil
}

// Tooltip fetches a wol bc/pc citation endpoint as JSON (publication
// excerpts, verse popups). Every passage the citation names is kept: the
// first as the tooltip, the others as its More.
func (c *Client) Tooltip(ctx context.Context, tcURL string) (model.Tooltip, error) {
	var resp struct {
		Items []tooltipItem `json:"items"`
	}
	// citation paths taken straight out of a document are relative, and carry a
	// locale segment that has to come off first
	locale := citationLocaleOf(tcURL)
	tcURL = absURL(c.hc.Base.WOL, citationAPI(tcURL))
	if err := c.hc.GetJSON(ctx, tcURL, xhrHeaders(), &resp); err != nil {
		return model.Tooltip{}, err
	}
	if len(resp.Items) == 0 {
		return model.Tooltip{}, fmt.Errorf("no citation content at %s", tcURL)
	}
	return c.tooltip(resp.Items, locale), nil
}

// LocalizedBookNames extracts the localized bible book names from the bible
// navigation page (best effort; kept until the page changes upstream).
func (c *Client) LocalizedBookNames(ctx context.Context, cfg Config) (map[int][]string, error) {
	u := c.url(cfg, "binav", "")
	return httpx.Memo(ctx, c.cache, "books3-"+u, func(ctx context.Context) (map[int][]string, error) {
		return c.localizedBookNames(ctx, cfg, u)
	})
}

func (c *Client) localizedBookNames(ctx context.Context, cfg Config, u string) (map[int][]string, error) {
	doc, err := c.hc.GetHTML(ctx, u)
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
	return names, nil
}

// Bibles lists the bible editions the library carries in a language, in the
// order its own bible list prints them (the current translation first, older
// ones after it). Which editions exist is language specific — English has eight,
// German three — and the list moves, so it is read rather than hardcoded.
// Kept until the page changes upstream.
func (c *Client) Bibles(ctx context.Context, cfg Config) ([]BibleEdition, error) {
	return httpx.Memo(ctx, c.cache, "bibles2-"+c.url(cfg, "bibles", ""), func(ctx context.Context) ([]BibleEdition, error) {
		return c.bibles(ctx, cfg)
	})
}

func (c *Client) bibles(ctx context.Context, cfg Config) ([]BibleEdition, error) {
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
var citationLocale = regexp.MustCompile(`/([\w-]+)/wol/`)

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

// citationLocaleOf is the locale a citation link carries, empty when it has
// none. It has to be put back into whatever the locale-less endpoint answers.
func citationLocaleOf(path string) string {
	if m := citationLocale.FindStringSubmatch(path); m != nil {
		return m[1]
	}
	return ""
}

// wolLink matches a link the content endpoints write: they answer the
// locale-less request with locale-less links, which lead nowhere.
var wolLink = regexp.MustCompile(`((?:href|src)=")/wol/`)

// relocalize puts the locale back into the links of a fetched passage, so a
// reader can follow them.
func relocalize(fragment, locale string) string {
	if locale == "" || fragment == "" {
		return fragment
	}
	return wolLink.ReplaceAllString(fragment, "${1}/"+locale+"/wol/")
}

// relocalizeURL is relocalize for a bare path rather than markup.
func relocalizeURL(path, locale string) string {
	if locale == "" || !strings.HasPrefix(path, "/wol/") {
		return path
	}
	return "/" + locale + path
}

func absURL(base, path string) string {
	if path == "" || strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return base + path
}
