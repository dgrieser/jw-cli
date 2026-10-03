package service

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"strings"

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
// link was absolutized for the page or is still relative as wol wrote it.
func RefPath(href string) string {
	u, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return href
	}
	return u.Path
}

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
		fmt.Fprintf(&b, `<div class="footnote" data-ref="%s">%s</div>`,
			html.EscapeString(RefPath(p)), paragraph(tip.ContentHTML))
	}
	return b.String(), requests
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
func (s *Service) ArticleSection(ctx context.Context, lng model.Language, target string) (UnfoldSection, error) {
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
	return UnfoldSection{
		Title: html.EscapeString(firstNonBlank(art.Title, target)),
		Body:  dropFirstH1(body),
		Ref:   RefPath(target),
		Base:  s.ArticleBase(art),
		Open:  true,
	}, nil
}

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
