package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/service"
	"github.com/dgrieser/jw-cli/internal/subtitles"
)

// chapterHref reads one whole chapter, named the way a typed reference names it.
func (p biblePage) chapterHref(edition, bookName string, chapter int) string {
	q := url.Values{"bible": {edition}, "ref": {fmt.Sprintf("%s %d", bookName, chapter)}}
	return p.WithLang("/bible?" + q.Encode())
}

// chapterNavView leads on from a reading: the chapter before its first
// passage, the one after its last, and all of the last passage's chapter
// when it shows part of it. A link not there is empty.
type chapterNavView struct {
	Prev, Whole, Next chapterLink
}

type chapterLink struct {
	Label string
	Href  string
}

// chapterNav is the way on from a reading. Across a book's end it goes to the
// neighbouring book, whose chapters its nav knows; when that cannot be read,
// the link is left out rather than guessed.
func (s *Server) chapterNav(ctx context.Context, lng model.Language, res service.ReadResult, page *biblePage) *chapterNavView {
	if len(res.Passages) == 0 || res.Table == nil {
		return nil
	}
	first, last := res.Passages[0], res.Passages[len(res.Passages)-1]
	if first.Book == 0 || last.Book == 0 {
		return nil
	}
	nav := &chapterNavView{}
	link := func(p service.Passage, book, chapter int) chapterLink {
		name := res.Table.Name(book)
		return chapterLink{
			Label: fmt.Sprintf("%s %d", name, chapter),
			Href:  page.chapterHref(firstNonEmpty(p.Bible, page.Edition), name, chapter),
		}
	}
	chapters := func(p service.Passage, book int) []int {
		bn, err := s.svc.BookNav(ctx, lng, firstNonEmpty(p.Bible, page.Edition), book)
		if err != nil {
			return nil
		}
		return bn.Chapters
	}
	switch {
	case first.Chapter > 1:
		nav.Prev = link(first, first.Book, first.Chapter-1)
	case first.Book > 1:
		if cs := chapters(first, first.Book-1); len(cs) > 0 {
			nav.Prev = link(first, first.Book-1, cs[len(cs)-1])
		}
	}
	if cs := chapters(last, last.Book); len(cs) > 0 {
		switch {
		case last.Chapter < cs[len(cs)-1]:
			nav.Next = link(last, last.Book, last.Chapter+1)
		case last.Book < 66:
			nav.Next = link(last, last.Book+1, 1)
		}
	}
	if !last.Whole {
		nav.Whole = link(last, last.Book, last.Chapter)
		nav.Whole.Label = page.T.UIWholeChapter
	}
	return nav
}

// chapterStudyView is what the library has on one chapter, as the diamond at
// a chapter's heading opens it: the headings of the book's outline that cover
// the chapter, the book's introduction — its facts, and the text under its
// video or what the video says, the video itself a link — and the chapter's
// part of the book's media gallery. A part the library does not have for the
// book is left out.
type chapterStudyView struct {
	biblePage
	Chapter int

	OutlineTitle string
	Outline      []outlineRow

	Intro      *model.BookIntro
	IntroHref  string
	HasVideo   bool
	Notes      template.HTML
	Paragraphs [][]model.Cue

	GalleryTitle string
	Gallery      []model.GalleryTile

	// Errors are the parts that could not be read.
	Errors []string
}

// Empty says the library has nothing on the chapter.
func (v chapterStudyView) Empty() bool {
	return v.Outline == nil && v.Intro == nil && v.Gallery == nil && v.Errors == nil
}

// uiChapterStudy answers the diamond at a chapter's heading with what the
// library has on the chapter, as a fragment of the reader:
// GET /bible/chapter?bible=…&book=…&chapter=….
func (s *Server) uiChapterStudy(w http.ResponseWriter, r *http.Request) {
	book, _ := intParam(r, "book", 0)
	chapter, _ := intParam(r, "chapter", 0)
	if book < 1 || book > 66 || chapter < 1 {
		http.Error(w, "parameters book (1-66) and chapter are required", http.StatusBadRequest)
		return
	}
	lng, err := s.language(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	page := biblePage{basePage: s.base(r, "Bible"), Edition: valueOr(r, "bible", "nwtsty")}
	nav, err := s.svc.BookNav(ctx, lng, page.Edition, book)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	page.BookNav, page.BookName = &nav, s.svc.BookTable(ctx, lng).Name(book)
	v := chapterStudyView{biblePage: page, Chapter: chapter}

	var mu sync.Mutex
	fail := func(err error) {
		if errors.Is(err, service.ErrNoBookDoc) {
			return
		}
		mu.Lock()
		v.Errors = append(v.Errors, err.Error())
		mu.Unlock()
	}
	has := func(kinds ...string) bool {
		for _, l := range nav.Links {
			if slices.Contains(kinds, l.Kind) {
				return true
			}
		}
		return false
	}
	var wg sync.WaitGroup
	if has(wol.BookOutline, wol.BookOverview) {
		wg.Go(func() {
			o, err := s.svc.BookOutlineDoc(ctx, lng, page.Edition, book)
			if err != nil {
				fail(err)
				return
			}
			var items []model.OutlineItem
			for _, it := range o.Items {
				if it.Start/1_000%1_000 <= chapter && chapter <= it.End/1_000%1_000 {
					items = append(items, it)
				}
			}
			if len(items) > 0 {
				v.OutlineTitle, v.Outline = o.Title, outlineRows(items, false, &page)
			}
		})
	}
	if has(wol.BookIntroduction) {
		wg.Go(func() {
			intro, err := s.svc.BookIntro(ctx, lng, page.Edition, book)
			if err != nil {
				fail(err)
				return
			}
			v.Intro, v.IntroHref, v.HasVideo = &intro, page.docLink(viewIntro, ""), intro.Video != nil
			if intro.NotesHTML != "" {
				v.Notes = s.sanitized(intro.NotesHTML, s.svc.HTTP.Base.WOL)
			}
			v.Paragraphs = subtitles.Paragraphs(intro.Transcript)
		})
	}
	if has(wol.BookGallery) {
		wg.Go(func() {
			g, err := s.svc.BookGallery(ctx, lng, page.Edition, book)
			if err != nil {
				fail(err)
				return
			}
			for _, grp := range g.Groups {
				if headingChapter(grp.Heading) == chapter && len(grp.Items) > 0 {
					v.GalleryTitle, v.Gallery = g.Title, grp.Items
				}
			}
		})
	}
	wg.Wait()

	var buf bytes.Buffer
	if err := s.templates["bible"].ExecuteTemplate(&buf, "chapterstudy", v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

// headingChapter is the chapter a gallery's heading names last: "Matthew 2".
func headingChapter(h string) int {
	f := strings.Fields(h)
	if len(f) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimRight(f[len(f)-1], ".:"))
	return n
}
