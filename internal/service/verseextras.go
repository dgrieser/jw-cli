package service

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"

	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/bibleref"
	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/unfold"
)

// footnotePath is the endpoint wol answers a footnote marker ("*") through.
// It is the same tooltip endpoint as a citation's, and every bible edition
// writes its footnotes that way, study pane or not.
const footnotePath = "/fn/"

// IsFootnote reports whether a link is a footnote marker.
func IsFootnote(href string) bool { return strings.Contains(href, footnotePath) }

// footnoteLinks are the footnote markers of a fragment, in document order and
// without repeats.
func footnoteLinks(fragment string) []string {
	if !strings.Contains(fragment, footnotePath) {
		return nil
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(fragment))
	if err != nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href := a.AttrOr("href", "")
		if IsFootnote(href) && !seen[href] {
			seen[href] = true
			out = append(out, href)
		}
	})
	return out
}

// RefPath is the path a link names, without scheme, host, query or fragment:
// the key a page finds the section of a reference by, the same whether the
// link was absolutized for the page or is still relative as wol wrote it. The
// passages after the first of a citation answered with several keep the mark
// naming which they are (unfold.ItemPath): each is a section of its own, and
// a page shows a section once by its key.
func RefPath(href string) string {
	if base, item := unfold.SplitItemPath(href); item > 1 {
		return unfold.ItemPath(RefPath(base), item)
	}
	u, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return href
	}
	return u.Path
}

// refSeparator joins a footnote's pointer to what it points at ("Glossary →
// Soul"): an arrow, as it is a pointer from one thing to another.
const refSeparator = " → "

// footnotesHTML resolves footnote markers and returns one block per footnote,
// marked with the path of the marker it answers, so a page can go to it. A
// footnote that cannot be read is left out: the verse reads without it.
func footnotesHTML(ctx context.Context, r unfold.Resolver, paths []string) (string, int) {
	var b strings.Builder
	requests := 0
	for _, p := range paths {
		tip, err := r.Resolve(ctx, p)
		requests++
		if err != nil || strings.TrimSpace(tip.ContentHTML) == "" {
			continue
		}
		body := paragraph(tip.ContentHTML)
		// a footnote that only says where to read on — "See Glossary." —
		// says what is read there instead
		if path, label, ok := pointerFootnote(tip.ContentHTML); ok {
			entry, err := r.Resolve(ctx, path)
			requests++
			if err == nil && strings.TrimSpace(entry.ContentHTML) != "" {
				if entry.Title != "" && !saysIt(label, entry.Title) {
					label += refSeparator + entry.Title
				}
				body = fmt.Sprintf(`<p class="fn-title"><strong>%s</strong></p>%s`,
					html.EscapeString(label), withoutHeader(entry.ContentHTML))
			}
		}
		fmt.Fprintf(&b, `<div class="footnote" data-ref="%s">%s</div>`,
			html.EscapeString(RefPath(p)), body)
	}
	return b.String(), requests
}

// pointerFootnote reports whether a footnote does no more than point at one
// passage of a publication ("Siehe Worterklärungen."): the passage's path,
// and the link's text, which names it.
func pointerFootnote(fragment string) (path, label string, ok bool) {
	if !strings.Contains(fragment, "/pc/") {
		return "", "", false
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(fragment))
	if err != nil {
		return "", "", false
	}
	links := doc.Find("a[href]")
	if links.Length() != 1 {
		return "", "", false
	}
	href := links.AttrOr("href", "")
	if !strings.Contains(href, "/pc/") {
		return "", "", false
	}
	label = strings.Join(strings.Fields(links.Text()), " ")
	// besides the link, a word or two: "Siehe", "See", "Voir"
	rest := strings.Replace(doc.Text(), links.Text(), "", 1)
	rest = strings.TrimFunc(rest, func(r rune) bool { return r == '.' || r == ',' || r == ':' || unicode.IsSpace(r) })
	if label == "" || len(strings.Fields(rest)) > 2 {
		return "", "", false
	}
	u, err := url.Parse(href)
	if err != nil {
		return "", "", false
	}
	return u.Path, label, true
}

// withoutHeader is a passage without the header it opens with, the title its
// footnote's heading already says.
func withoutHeader(fragment string) string {
	if !strings.Contains(fragment, "<header") {
		return fragment
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(fragment))
	if err != nil {
		return fragment
	}
	body := doc.Find("body")
	body.Find("header").Remove()
	out, err := body.Html()
	if err != nil {
		return fragment
	}
	return strings.TrimSpace(out)
}

// EditionVerse is a verse as one bible edition renders it.
type EditionVerse struct {
	Edition wol.BibleEdition
	HTML    string
}

// Label names the edition a verse was read in, by its own title with the
// symbol after it.
func (v EditionVerse) Label() string {
	if v.Edition.Title == "" {
		return v.Edition.Symbol
	}
	return v.Edition.Title + " (" + v.Edition.Symbol + ")"
}

// isNWT tells the two editions of the New World Translation, which render one
// text: the study edition only adds its study pane.
func isNWT(symbol string) bool { return symbol == "nwt" || symbol == studyEdition }

// OtherEditions are the bibles a verse read in current is shown in beside it:
// every other bible of the language, the New World Translation once — as the
// study edition where the library has it — and not at all when it is what
// is being read.
func OtherEditions(all []wol.BibleEdition, current string) []wol.BibleEdition {
	hasStudy := false
	for _, e := range all {
		if e.Symbol == studyEdition {
			hasStudy = true
		}
	}
	var out []wol.BibleEdition
	for _, e := range all {
		switch {
		case e.Symbol == current:
		case isNWT(current) && isNWT(e.Symbol):
		case e.Symbol == "nwt" && hasStudy:
		default:
			out = append(out, e)
		}
	}
	return out
}

// otherEditionsFor lists the other bibles of the language, empty when the
// library's list cannot be read.
func (s *Service) otherEditionsFor(ctx context.Context, lng model.Language, current string) []wol.BibleEdition {
	all, err := s.ReadEditions(ctx, lng, "", true)
	if err != nil {
		return nil
	}
	return OtherEditions(all, current)
}

// translationsOf reads the verses of ref in every other bible of the language,
// keyed by verse number. Chapter pages go through chapters, so a passage asks
// for each edition's chapter once. An edition without the chapter or a verse —
// one that numbers them differently, or has only part of the Bible — is left
// out of that verse.
func (s *Service) translationsOf(ctx context.Context, lng model.Language, current string, ref bibleref.Ref,
	chapters map[string]*wol.ChapterDoc) map[int][]EditionVerse {
	out := map[int][]EditionVerse{}
	for _, e := range s.otherEditionsFor(ctx, lng, current) {
		key := fmt.Sprintf("%s-%d-%d", e.Symbol, ref.Book, ref.Chapter)
		doc, ok := chapters[key]
		if !ok {
			var err error
			if doc, err = s.Chapter(ctx, lng, e.Symbol, ref); err != nil {
				continue
			}
			chapters[key] = doc
		}
		verses, err := doc.Verses(ref.VerseStart, ref.VerseEnd)
		if err != nil {
			continue
		}
		for _, v := range verses {
			num := v.ID % 1000
			out[num] = append(out[num], EditionVerse{Edition: e, HTML: v.HTML})
		}
	}
	return out
}

// StreamTranslations reads a passage — a verse, or a range of verses of one
// chapter — in every other bible of the language and hands out each as a
// section of its own as soon as it is read: what a page loads when the reader
// opens the translations of a verse.
func (s *Service) StreamTranslations(ctx context.Context, lng model.Language, current string, ref bibleref.Ref,
	out UnfoldStream) (int, error) {
	requests := 0
	for i, e := range s.otherEditionsFor(ctx, lng, current) {
		if err := ctx.Err(); err != nil {
			return requests, err
		}
		doc, err := s.Chapter(ctx, lng, e.Symbol, ref)
		requests++
		if err != nil {
			continue
		}
		verses, err := doc.Verses(ref.VerseStart, ref.VerseEnd)
		if err != nil || len(verses) == 0 {
			continue
		}
		var body strings.Builder
		for _, v := range verses {
			body.WriteString(paragraph(v.HTML))
		}
		v := EditionVerse{Edition: e}
		out.Section(UnfoldSection{
			Title: html.EscapeString(v.Label()), Body: body.String(),
			Order: i, Open: true, Ref: "edition:" + e.Symbol,
		})
	}
	return requests, nil
}

// PassageRef is the passage a page names by the wol id of its first verse
// (book*1e6 + chapter*1e3 + verse) and the number of its last; to is zero for
// a single verse.
func PassageRef(verseID, to int) (bibleref.Ref, error) {
	ref, err := verseRef(verseID)
	if err != nil {
		return ref, err
	}
	if to > 0 {
		if to < ref.VerseStart || to > bibleref.LastVerse {
			return ref, fmt.Errorf("invalid last verse %d", to)
		}
		ref.VerseEnd = to
	}
	return ref, nil
}

// StreamFootnote resolves one footnote marker into a footnotes section holding
// it: what a page loads when a "*" is followed before the verse was unfolded.
func (s *Service) StreamFootnote(ctx context.Context, lng model.Language, path string,
	txt *i18n.Messages, out UnfoldStream) (int, error) {
	if !IsFootnote(path) {
		return 0, fmt.Errorf("not a footnote: %s", path)
	}
	r := newTooltipResolver(s, lng, nil)
	body, requests := footnotesHTML(ctx, r, []string{path})
	if body != "" {
		out.Section(UnfoldSection{
			Title: html.EscapeString(txt.FootnotesHeading), Body: body,
			Ref: FootnotesRef, Order: orderFootnotes,
		})
	}
	return requests, ctx.Err()
}

// ArticleSection reads a linked document into one section: the passage the
// link names when it names one ("#h=12:0-14:0"), the whole document otherwise.
// What a page loads when a link to an article is followed.
//
// The document comes as it is: nothing it cites is unfolded with it, so no
// paragraph of it brings its references before the reader asks — each one
// that cites something unfolds on its own in the page. cfg.Depth above zero
// reads a page of jw.org in its library copy, whose citations the page can
// unfold, where the library has one.
func (s *Service) ArticleSection(ctx context.Context, lng model.Language, target string,
	cfg UnfoldConfig) (UnfoldSection, error) {
	page := target
	if i := strings.IndexByte(page, '#'); i >= 0 {
		page = page[:i]
	}
	art, err := s.Article(ctx, lng, page)
	if err != nil {
		return UnfoldSection{}, err
	}
	body := art.HTML
	if passage, err := s.WOL.Passage(ctx, target); err == nil && strings.TrimSpace(passage) != "" {
		body = passage
	}
	if cfg.Depth > 0 {
		if lib := s.citable(ctx, lng, art); lib.URL != art.URL {
			art, body = lib, lib.HTML
		}
	}
	// an entry that only sends on to another ("Siehe SCHAF.") shows that one
	// under the line, once
	if href, ok := seeOnly(body); ok {
		if !strings.HasPrefix(href, "http") {
			href = s.HTTP.Base.WOL + href
		}
		if to, err := s.Article(ctx, lng, href); err == nil && strings.TrimSpace(to.HTML) != "" {
			body = dropFirstH1(body) + dropFirstH1(to.HTML)
		}
	}
	return UnfoldSection{
		Title: html.EscapeString(firstNonBlank(art.Title, target)),
		Body:  dropFirstH1(body),
		Ref:   RefPath(target),
		Doc:   firstDocID(art.DocID, wol.DocIDFromURL(art.URL), wol.DocIDFromURL(page)),
		Base:  s.ArticleBase(art),
		Open:  true,
	}, nil
}

// seeOnly is where a document leads that says nothing but where to look
// instead — an entry of Insight such as "LAMM: Siehe SCHAF." — as the one link
// to a document it holds besides its title; ok is false for any document
// that says more.
func seeOnly(fragment string) (href string, ok bool) {
	if len(fragment) > 4000 || !strings.Contains(fragment, "<a") {
		return "", false
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(fragment))
	if err != nil {
		return "", false
	}
	body := doc.Find("body")
	body.Find("h1, header, script, style, .pswp, input").Remove()
	links := body.Find("a[href]")
	if links.Length() != 1 {
		return "", false
	}
	href = links.AttrOr("href", "")
	if !strings.Contains(href, "/wol/tc/") && !strings.Contains(href, "/wol/d/") {
		return "", false
	}
	text := strings.Join(strings.Fields(body.Text()), " ")
	return href, utf8.RuneCountInString(text) <= seeOnlyMax
}

// seeOnlyMax is how long a document saying only where to look instead runs
// at most: "Siehe GOTTES KÖNIGREICH." and its like.
const seeOnlyMax = 60

// dropFirstH1 takes a document's own title off its body: the section showing
// it is already headed with it.
func dropFirstH1(fragment string) string {
	if !strings.Contains(fragment, "<h1") {
		return fragment
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(fragment))
	if err != nil {
		return fragment
	}
	doc.Find("h1").First().Remove()
	out, err := doc.Find("body").Html()
	if err != nil {
		return fragment
	}
	return out
}

func firstDocID(ids ...int) int {
	for _, id := range ids {
		if id > 0 {
			return id
		}
	}
	return 0
}

func firstNonBlank(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// verseRef is the single verse a wol verse id names (book*1e6 + chapter*1e3 +
// verse).
func verseRef(verseID int) (bibleref.Ref, error) {
	ref := bibleref.Ref{
		Book: verseID / 1_000_000, Chapter: verseID / 1_000 % 1_000,
		VerseStart: verseID % 1_000, VerseEnd: verseID % 1_000,
	}
	if ref.Book < 1 || ref.Book > 66 || ref.Chapter < 1 || ref.VerseStart < 1 {
		return ref, fmt.Errorf("invalid verse id %d", verseID)
	}
	return ref, nil
}
