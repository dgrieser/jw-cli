package service

import (
	"context"
	"fmt"
	htmlpkg "html"
	"strings"

	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/bibleref"
	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/render"
)

// Verse is a verse as jw bible read prints it: the text, and with an unfold
// what the verse references expanded under it, so the study material of a verse
// is read where the verse is.
type Verse struct {
	model.Verse
	// Unfold is the expansion of everything this verse references, as an HTML
	// appendix to its text.
	Unfold string `json:"unfold,omitempty"`
}

// Passage is one reference read, in one bible edition.
type Passage struct {
	Ref string `json:"ref"`
	// Bible and BibleTitle name the edition the passage was read in. Both are
	// empty unless several editions were read (--bible-all), where saying which
	// one a passage came from is the point.
	Bible      string  `json:"bible,omitempty"`
	BibleTitle string  `json:"bibleTitle,omitempty"`
	Verses     []Verse `json:"verses"`
	// UnfoldNote says an expansion was cut short, which is about the passage
	// rather than about one of its verses.
	UnfoldNote string `json:"unfoldNote,omitempty"`
}

// Heading is the line a passage is printed under: the reference, and with
// several editions read the edition it was read in.
func (p Passage) Heading() string {
	if p.BibleTitle == "" {
		return p.Ref
	}
	return p.Ref + " — " + p.BibleTitle
}

// ReadRequest is one jw bible read: which references, in which editions, and
// how deep to unfold what the verses cite.
type ReadRequest struct {
	Refs      string
	Edition   string // one edition; ignored with AllBibles
	AllBibles bool
	Unfold    UnfoldConfig
}

// ReadResult is what a read produced: the passages, the references an edition
// did not carry (already worded for the reader), and the book table the
// references were parsed with.
type ReadResult struct {
	Passages []Passage
	Missing  []string
	Table    *bibleref.Table
}

// ReadPassages reads verses, verse ranges, chapters, or spans of chapters, in
// one or every bible edition. txt words the notes about editions that do not
// carry a passage.
func (s *Service) ReadPassages(ctx context.Context, lng model.Language, req ReadRequest, txt *i18n.Messages) (ReadResult, error) {
	refs, table, err := s.ParseRefs(ctx, lng, req.Refs)
	if err != nil {
		return ReadResult{}, err
	}
	editions, err := s.ReadEditions(ctx, lng, req.Edition, req.AllBibles)
	if err != nil {
		return ReadResult{}, err
	}
	out := ReadResult{Table: table}
	chapters := map[string]*wol.ChapterDoc{}
	// one expander for every passage read, so they share the chapter pages
	// their study panes come from
	var expander *tooltipResolver
	if req.Unfold.Depth > 0 {
		expander = newTooltipResolver(s, lng, chapters)
	}
	for _, ref := range refs {
		var skipped []string
		for _, ed := range editions {
			key := fmt.Sprintf("%s-%d-%d", ed.Symbol, ref.Book, ref.Chapter)
			doc, ok := chapters[key]
			if !ok {
				doc, err = s.Chapter(ctx, lng, ed.Symbol, ref)
				if err != nil {
					if !req.AllBibles {
						return ReadResult{}, err
					}
					// a translation of part of the Bible only, or one that
					// numbers its chapters differently
					skipped = append(skipped, ed.Symbol)
					continue
				}
				chapters[key] = doc
			}
			verses, err := doc.Verses(ref.VerseStart, ref.VerseEnd)
			if err != nil {
				if !req.AllBibles {
					return ReadResult{}, fmt.Errorf("%s: %w", RefString(ref, table), err)
				}
				skipped = append(skipped, ed.Symbol)
				continue
			}
			// where a reference running past its chapter ends is the edition's
			// own answer, so it is resolved per edition
			r := ResolveChapterEnd(ref, verses)
			p := Passage{Ref: RefString(r, table)}
			if req.AllBibles {
				p.Bible, p.BibleTitle = ed.Symbol, ed.Label()
			}
			var unfolded []string
			if req.Unfold.Depth > 0 {
				// several verses are headed one by one, so the expansion of a
				// verse reads as belonging to that verse rather than to the
				// passage; a single verse is already the passage heading
				level := passageUnfoldLevel
				if len(verses) > 1 {
					level = verseUnfoldLevel
				}
				unfolded, p.UnfoldNote, err = unfoldBibleVerses(ctx, expander, r, verses, level, req.Unfold, txt)
				if err != nil {
					return ReadResult{}, err
				}
			}
			for i, v := range verses {
				verse := Verse{Verse: v}
				if i < len(unfolded) {
					verse.Unfold = unfolded[i]
					if len(verses) > 1 {
						num := v.ID % 1000
						verse.Citation = RefString(bibleref.Ref{
							Book: r.Book, Chapter: r.Chapter, VerseStart: num, VerseEnd: num,
						}, table)
					}
				}
				p.Verses = append(p.Verses, verse)
			}
			out.Passages = append(out.Passages, p)
		}
		if len(skipped) > 0 {
			out.Missing = append(out.Missing, fmt.Sprintf(txt.NotInEditions,
				RefString(ref, table), strings.Join(skipped, ", ")))
		}
	}
	return out, nil
}

// FormatPassages renders a read into one document in the requested format,
// each passage under its heading and the missing-edition notes at the end.
// JSON is not a rendering; callers handle it themselves.
func FormatPassages(res ReadResult, format render.Format, opts render.Options) (string, error) {
	var b strings.Builder
	for i, p := range res.Passages {
		if i > 0 {
			b.WriteString("\n")
		}
		var html strings.Builder
		for _, run := range verseRuns(p.Verses) {
			if cite := runCitation(run, res.Table); cite != "" {
				html.WriteString(headingHTML(passageUnfoldLevel, htmlpkg.EscapeString(cite)))
			}
			for _, v := range run {
				html.WriteString(v.HTML)
				html.WriteString(" ")
				html.WriteString(v.Unfold)
			}
		}
		html.WriteString(unfoldNoteHTML(p.UnfoldNote))
		body, err := render.Render(collapseRules(html.String()), format, opts)
		if err != nil {
			return "", err
		}
		switch format {
		case render.Markdown, render.Raw:
			fmt.Fprintf(&b, "## %s\n\n%s\n", p.Heading(), body)
		case render.HTML:
			fmt.Fprintf(&b, "<h2>%s</h2>\n%s\n", htmlpkg.EscapeString(p.Heading()), body)
		default:
			fmt.Fprintf(&b, "%s\n\n%s\n", p.Heading(), body)
		}
	}
	for _, note := range res.Missing {
		switch format {
		case render.Markdown, render.Raw:
			fmt.Fprintf(&b, "\n_%s_\n", note)
		case render.HTML:
			fmt.Fprintf(&b, "%s\n", unfoldNoteHTML(note))
		default:
			fmt.Fprintf(&b, "\n%s\n", note)
		}
	}
	return b.String(), nil
}

// ReadEditions is the set of bibles one read covers: the single edition named,
// or every bible the library carries in the selected language.
func (s *Service) ReadEditions(ctx context.Context, lng model.Language, edition string, all bool) ([]wol.BibleEdition, error) {
	if !all {
		return []wol.BibleEdition{{Symbol: edition}}, nil
	}
	cfg, err := s.WOLConfig(ctx, lng)
	if err != nil {
		return nil, err
	}
	return s.WOL.Bibles(ctx, cfg)
}

// verseRuns groups the verses of a passage into the spans printed under one
// heading. A verse that brought something of its own stands alone, so what
// follows the verse reads as belonging to it; the verses around it that brought
// nothing are one span of plain text and are headed as the span they are —
// "Jeremiah 30:1, 2" rather than a heading over every verse of it.
func verseRuns(verses []Verse) [][]Verse {
	var runs [][]Verse
	for i := 0; i < len(verses); {
		j := i + 1
		if verses[i].Unfold == "" {
			for j < len(verses) && verses[j].Unfold == "" {
				j++
			}
		}
		runs = append(runs, verses[i:j])
		i = j
	}
	return runs
}

// runCitation heads a span of verses. It is empty when the verses of the passage
// are not headed one by one at all: without an expansion under them there is
// nothing to tell apart, and the passage heading already says what they are.
func runCitation(run []Verse, t *bibleref.Table) string {
	if len(run) == 0 || run[0].Citation == "" {
		return ""
	}
	if len(run) == 1 {
		return run[0].Citation
	}
	first, last := run[0].ID, run[len(run)-1].ID
	return RefString(bibleref.Ref{
		Book:       first / 1_000_000,
		Chapter:    first / 1_000 % 1_000,
		VerseStart: first % 1_000,
		VerseEnd:   last % 1_000,
	}, t)
}

// RefString renders a ref with the (possibly localized) book table.
func RefString(r bibleref.Ref, t *bibleref.Table) string {
	name := t.Name(r.Book)
	switch {
	case r.VerseStart == 0:
		return fmt.Sprintf("%s %d", name, r.Chapter)
	case r.RunsToChapterEnd():
		// the chapter was not read here, so its last verse cannot be named
		return fmt.Sprintf("%s %d:%dff.", name, r.Chapter, r.VerseStart)
	case r.VerseEnd > r.VerseStart:
		return fmt.Sprintf("%s %d:%d-%d", name, r.Chapter, r.VerseStart, r.VerseEnd)
	default:
		return fmt.Sprintf("%s %d:%d", name, r.Chapter, r.VerseStart)
	}
}

// ResolveChapterEnd names the last verse of a reference that was written as
// running past its chapter — "Pr 8:5-9:10" takes chapter 8 to its end — now that
// the chapter has been read and where it ends is known.
func ResolveChapterEnd(ref bibleref.Ref, verses []model.Verse) bibleref.Ref {
	if !ref.RunsToChapterEnd() || len(verses) == 0 {
		return ref
	}
	ref.VerseEnd = verses[len(verses)-1].ID % 1000
	return ref
}

// ForEachStudySection iterates the study sections of every verse in the parsed
// references, together with the verse itself (zero value when its text is not
// on the page).
func (s *Service) ForEachStudySection(ctx context.Context, lng model.Language, input string,
	fn func(ref string, verse model.Verse, sec model.StudySection)) error {
	refs, table, err := s.ParseRefs(ctx, lng, input)
	if err != nil {
		return err
	}
	chapters := map[string]*wol.ChapterDoc{}
	for _, ref := range refs {
		key := fmt.Sprintf("%d-%d", ref.Book, ref.Chapter)
		doc, ok := chapters[key]
		if !ok {
			doc, err = s.Chapter(ctx, lng, studyEdition, ref)
			if err != nil {
				return err
			}
			chapters[key] = doc
		}
		// the verse text of the whole span, so every study section can be
		// printed with the verse it belongs to
		texts := map[int]model.Verse{}
		verses, err := doc.Verses(ref.VerseStart, ref.VerseEnd)
		if err != nil {
			return fmt.Errorf("%s: %w", RefString(ref, table), err)
		}
		ref = ResolveChapterEnd(ref, verses)
		from, to := ref.VerseStart, ref.VerseEnd
		for _, v := range verses {
			texts[v.ID%1000] = v
		}
		if from == 0 {
			// bound the scan to the chapter's real last verse
			from, to = 1, verses[len(verses)-1].ID%1000
		}
		for v := from; v <= to; v++ {
			sec, ok := doc.StudySection(v)
			if !ok {
				continue
			}
			label := sec.Verse
			if label == "" {
				label = RefString(bibleref.Ref{Book: ref.Book, Chapter: ref.Chapter, VerseStart: v, VerseEnd: v}, table)
			}
			fn(label, texts[v], sec)
		}
	}
	return nil
}

// NotesEntry is the study notes of one verse, printed below the verse text.
type NotesEntry struct {
	Ref   string            `json:"ref"`
	Verse model.Verse       `json:"verse"`
	Notes []model.StudyNote `json:"notes"`
}

// Notes lists the study notes attached to each verse of the references.
func (s *Service) Notes(ctx context.Context, lng model.Language, input string) ([]NotesEntry, error) {
	var entries []NotesEntry
	err := s.ForEachStudySection(ctx, lng, input, func(ref string, verse model.Verse, sec model.StudySection) {
		if len(sec.Notes) > 0 {
			entries = append(entries, NotesEntry{Ref: ref, Verse: verse, Notes: sec.Notes})
		}
	})
	return entries, err
}

// XRefsEntry is the marginal cross references of one verse.
type XRefsEntry struct {
	Ref   string           `json:"ref"`
	XRefs []model.CrossRef `json:"xrefs"`
}

// XRefs lists the cross references of each verse; with resolve the full text
// of the referenced verses is fetched too.
func (s *Service) XRefs(ctx context.Context, lng model.Language, input string, resolve bool) ([]XRefsEntry, error) {
	var entries []XRefsEntry
	err := s.ForEachStudySection(ctx, lng, input, func(ref string, _ model.Verse, sec model.StudySection) {
		if len(sec.XRefs) > 0 {
			entries = append(entries, XRefsEntry{Ref: ref, XRefs: sec.XRefs})
		}
	})
	if err != nil {
		return nil, err
	}
	if resolve {
		for _, e := range entries {
			for i := range e.XRefs {
				if e.XRefs[i].SourcePath == "" {
					continue
				}
				html, err := s.WOL.MarginalReference(ctx, e.XRefs[i].SourcePath)
				if err != nil {
					return nil, fmt.Errorf("resolve %s: %w", e.XRefs[i].Citation, err)
				}
				e.XRefs[i].ResolvedHTML = html
			}
		}
	}
	return entries, nil
}

// ResearchEntry is the research-guide references of one verse.
type ResearchEntry struct {
	Ref   string               `json:"ref"`
	Verse model.Verse          `json:"verse"`
	Items []model.ResearchItem `json:"items"`
}

// Research lists the Research Guide entries attached to each verse; with
// excerpts the referenced passage of each publication is fetched too (best
// effort).
func (s *Service) Research(ctx context.Context, lng model.Language, input string, excerpts bool) ([]ResearchEntry, error) {
	var entries []ResearchEntry
	err := s.ForEachStudySection(ctx, lng, input, func(ref string, verse model.Verse, sec model.StudySection) {
		if len(sec.Research) > 0 {
			entries = append(entries, ResearchEntry{Ref: ref, Verse: verse, Items: sec.Research})
		}
	})
	if err != nil {
		return nil, err
	}
	if excerpts {
		for _, e := range entries {
			for i := range e.Items {
				if e.Items[i].PCPath == "" {
					continue
				}
				tip, err := s.WOL.Tooltip(ctx, e.Items[i].PCPath)
				if err != nil {
					continue // excerpts are best effort
				}
				e.Items[i].ExcerptHTML = tip.ContentHTML
				if e.Items[i].ArticleURL == "" {
					e.Items[i].ArticleURL = tip.URL
				}
				if e.Items[i].Title == "" {
					e.Items[i].Title = tip.Title
				}
			}
		}
	}
	return entries, nil
}

// BibleMedia lists images and clips attached to the verses of the references.
// With meta, what the study pane leaves out is read from each item's gallery
// page (best effort); a download-bound listing skips that.
func (s *Service) BibleMedia(ctx context.Context, lng model.Language, input string, meta bool) ([]model.Result, error) {
	var items []model.Result
	err := s.ForEachStudySection(ctx, lng, input, func(ref string, _ model.Verse, sec model.StudySection) {
		for _, m := range sec.Media {
			if meta {
				m = s.WithGalleryMeta(ctx, m)
			}
			r := ImageResult(m, ref)
			r.JWLink = m.FinderLink
			items = append(items, r)
		}
	})
	return items, err
}

// WithGalleryMeta fills in what the study pane leaves out. A verse's picture is
// listed there as a thumbnail with a one-line title; its explanatory caption
// and its rights line are written down only on the gallery page it links to, so
// that page is read (cached for a month) whenever a picture names one. Failing
// to read it costs the extra words, not the entry: the thumbnail's own title
// stays.
func (s *Service) WithGalleryMeta(ctx context.Context, m model.MediaAsset) model.MediaAsset {
	if m.SourceURL == "" {
		return m
	}
	g, err := s.WOL.GalleryItem(ctx, m.SourceURL)
	if err != nil {
		return m
	}
	if g.URL != "" && g.URL != m.URL {
		if m.ThumbnailURL == "" {
			m.ThumbnailURL = m.URL // what the study pane pointed at
		}
		m.URL = g.URL
	}
	for _, f := range []struct {
		dst *string
		src string
	}{
		{&m.Caption, g.Caption},
		{&m.Alt, g.Alt},
		{&m.Credit, g.Credit},
		{&m.Description, g.Description},
	} {
		if *f.dst == "" {
			*f.dst = f.src
		}
	}
	return m
}

// Book is one bible book, numbered as the APIs number them.
type Book struct {
	Number int    `json:"number"`
	Name   string `json:"name"`
}

// Books lists the 66 bible books with their (localized) names.
func (s *Service) Books(ctx context.Context, lng model.Language) []Book {
	t := s.BookTable(ctx, lng)
	books := make([]Book, 0, 66)
	for i := 1; i <= 66; i++ {
		books = append(books, Book{Number: i, Name: t.Name(i)})
	}
	return books
}
