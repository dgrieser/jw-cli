package service

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strings"
	"sync"

	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/bibleref"
	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
)

// BookContent is what the publications that go through every bible book part
// by part say about a book (chapter 0) or one chapter of it: the Bible
// handbook's summary, the Watchtower's highlights, Insight's box of
// highlights and the "Inspired" book's study, in that order, each with the
// parts about the chapter. A publication that has nothing on it is left out;
// what could be read comes back with the error of what could not.
func (s *Service) BookContent(ctx context.Context, lng model.Language, book, chapter int) ([]model.BookStudy, error) {
	if book < 1 || book > 66 {
		return nil, fmt.Errorf("invalid book %d (want 1-66)", book)
	}
	if n := bibleref.Chapters(book); chapter < 0 || chapter > n {
		return nil, fmt.Errorf("%s has no chapter %d (it has %d)", s.BookTable(ctx, lng).Name(book), chapter, n)
	}
	cfg, err := s.WOLConfig(ctx, lng)
	if err != nil {
		return nil, err
	}
	found := make([][]model.BookStudy, len(wol.StudyPubs))
	errs := make([]error, len(wol.StudyPubs))
	var wg sync.WaitGroup
	for i, pub := range wol.StudyPubs {
		wg.Go(func() {
			found[i], errs[i] = s.WOL.BookStudies(ctx, cfg, pub, book)
		})
	}
	wg.Wait()
	var out []model.BookStudy
	for _, studies := range found {
		for _, st := range studies {
			if chapter > 0 {
				st.Parts = ChapterParts(st.Parts, chapter)
			}
			if len(st.Parts) > 0 {
				out = append(out, st)
			}
		}
	}
	return out, errors.Join(errs...)
}

// ChapterParts are the parts of a study about chapter, each with those of its
// items that are about it: all of them for a part whose items name no verses
// of their own.
func ChapterParts(parts []model.StudyPart, chapter int) []model.StudyPart {
	var out []model.StudyPart
	for _, p := range parts {
		var items []model.StudyPart
		for _, it := range p.Items {
			if it.About(chapter, p.Chapters) {
				items = append(items, it)
			}
		}
		if p.About(chapter, nil) || len(items) > 0 {
			p.Items = items
			out = append(out, p)
		}
	}
	return out
}

// StudyHTML lays out what a study says as one document: each part's
// paragraphs as the publication has them, then its items, each under its
// subheading where it starts one. A part or item kept from before the
// paragraphs were kept with it is written from its title and verses.
func StudyHTML(st model.BookStudy) string {
	esc := html.EscapeString
	written := func(p model.StudyPart) string {
		if p.HTML != "" {
			return p.HTML
		}
		s := "<p><strong>" + esc(p.Title) + "</strong>"
		if p.Label != "" {
			s += " (" + esc(p.Label) + ")"
		}
		if p.Text != "" {
			s += " " + esc(p.Text)
		}
		return s + "</p>"
	}
	var b strings.Builder
	for _, p := range st.Parts {
		b.WriteString(written(p))
		group := ""
		for _, it := range p.Items {
			if it.Group != group {
				group = it.Group
				if group != "" {
					b.WriteString("<p><strong>" + esc(group) + "</strong></p>")
				}
			}
			b.WriteString(written(it))
		}
	}
	return b.String()
}

// StudyTitle heads a study of a chapter, or of a whole book (chapter 0), by
// the publication's short name and what it is about: "Highlights – Lamentations
// 1", with the dash a glossary's entry is headed with.
func StudyTitle(txt *i18n.Messages, st model.BookStudy, bookName string, chapter int) string {
	about := bookName
	if chapter > 0 {
		about = fmt.Sprintf("%s %d", bookName, chapter)
	}
	return strings.TrimPrefix(txt.StudyName(st.Pub)+" – "+about, " – ")
}
