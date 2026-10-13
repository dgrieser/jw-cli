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
	// Short is the label with the book's abbreviation, for a row too narrow
	// for its name.
	Short string
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
	abbrs := map[string]map[int]string{}
	abbr := func(edition string, book int) string {
		if _, ok := abbrs[edition]; !ok {
			abbrs[edition] = map[int]string{}
			if bn, err := s.svc.BibleNav(ctx, lng, edition); err == nil {
				for _, sec := range bn.Sections {
					for _, b := range sec.Books {
						abbrs[edition][b.Number] = b.Abbreviation
					}
				}
			}
		}
		return abbrs[edition][book]
	}
	link := func(p service.Passage, book, chapter int) chapterLink {
		name := res.Table.Name(book)
		edition := firstNonEmpty(p.Bible, page.Edition)
		l := chapterLink{
			Label: fmt.Sprintf("%s %d", name, chapter),
			Href:  page.chapterHref(edition, name, chapter),
		}
		if a := abbr(edition, book); a != "" && a != name {
			l.Short = fmt.Sprintf("%s %d", a, chapter)
		}
		return l
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
	// a reading of several passages is what was asked for: none of them
	// leads to its whole chapter
	if !last.Whole && len(res.Passages) == 1 {
		nav.Whole = link(last, last.Book, last.Chapter)
		nav.Whole.Label = page.T.UIWholeChapter
		nav.Whole.Short = page.T.UIWholeChapterShort
	}
	return nav
}

// chapterStudyView is one part of what the library has on a chapter, as the
// chapter's number in its heading unfolds it, section by section like a
// verse's: the headings of the book's outline that cover the chapter, the
// book's introduction — its facts, and the text under its video or what the
// video says, the video itself a link — or the chapter's part of the book's
// media gallery. A part the library does not have for the book is left out.
type chapterStudyView struct {
	biblePage

	OutlineTitle string
	Outline      []outlineRow

	Intro      *model.BookIntro
	IntroHref  string
	HasVideo   bool
	Notes      template.HTML
	Paragraphs [][]model.Cue

	GalleryTitle string
	Gallery      []model.GalleryTile
}

// The order the chapter's sections read in, whichever is ready first.
const (
	orderOutline = iota + 1
	orderIntro
	orderGallery
)

// unfoldChapter streams what the library has on a chapter, the way
// /unfold/verse streams a verse: GET /unfold/chapter?bible=…&book=…&chapter=….
func (s *Server) unfoldChapter(w http.ResponseWriter, r *http.Request) {
	book, _ := intParam(r, "book", 0)
	chapter, _ := intParam(r, "chapter", 0)
	if book < 1 || book > 66 || chapter < 1 {
		badRequest(w, "parameters %q (1-66) and %q are required", "book", "chapter")
		return
	}
	edition := valueOr(r, "bible", "nwtsty")
	if !editionSymbol.MatchString(edition) {
		badRequest(w, "invalid bible edition %q", edition)
		return
	}
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	txt := text(lng)
	ctx := r.Context()
	ev := startStream(w)
	page := biblePage{basePage: s.base(r, "Bible"), Edition: edition}
	nav, err := s.svc.BookNav(ctx, lng, edition, book)
	if err != nil {
		ev.finish(r, "", 0, err, txt)
		return
	}
	page.BookNav, page.BookName = &nav, s.svc.BookTable(ctx, lng).Name(book)

	var mu sync.Mutex
	var failed []string
	fail := func(err error) {
		if errors.Is(err, service.ErrNoBookDoc) {
			return
		}
		mu.Lock()
		failed = append(failed, err.Error())
		mu.Unlock()
	}
	send := func(name string, order int, v chapterStudyView) {
		var buf bytes.Buffer
		if err := s.templates["bible"].ExecuteTemplate(&buf, name, v); err != nil {
			fail(err)
			return
		}
		ev.send(streamEvent{Type: "section", HTML: buf.String(), Order: order})
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
			o, err := s.svc.BookOutlineDoc(ctx, lng, edition, book)
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
				send("chapteroutline", orderOutline,
					chapterStudyView{biblePage: page, OutlineTitle: chapterTitle(o.Title, chapter), Outline: outlineRows(items, false, &page)})
			}
		})
	}
	if has(wol.BookIntroduction) {
		wg.Go(func() {
			intro, err := s.svc.BookIntro(ctx, lng, edition, book)
			if err != nil {
				fail(err)
				return
			}
			v := chapterStudyView{biblePage: page, Intro: &intro, IntroHref: page.docLink(viewIntro, ""), HasVideo: intro.Video != nil}
			if intro.NotesHTML != "" {
				v.Notes = s.sanitized(intro.NotesHTML, s.svc.HTTP.Base.WOL)
			}
			v.Paragraphs = subtitles.Paragraphs(intro.Transcript)
			send("chapterintro", orderIntro, v)
		})
	}
	if has(wol.BookGallery) {
		wg.Go(func() {
			g, err := s.svc.BookGallery(ctx, lng, edition, book)
			if err != nil {
				fail(err)
				return
			}
			for _, grp := range g.Groups {
				if headingChapter(grp.Heading) == chapter && len(grp.Items) > 0 {
					send("chaptergallery", orderGallery, chapterStudyView{biblePage: page, GalleryTitle: chapterTitle(g.Title, chapter), Gallery: grp.Items})
				}
			}
		})
	}
	wg.Wait()

	// what could not be read fails the unfold when nothing else came, and is
	// a note under what did
	var note string
	if len(failed) > 0 {
		ev.mu.Lock()
		got := ev.sections
		ev.mu.Unlock()
		if got == 0 {
			ev.finish(r, "", 0, errors.New(strings.Join(failed, "; ")), txt)
			return
		}
		note = strings.Join(failed, "; ")
	}
	ev.finish(r, note, 0, nil, txt)
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

// chapterTitle is the title of a book's outline or gallery as the part of it
// one chapter shows: "Overview of Matthew" becomes "Overview of Matthew 19".
// The book's introduction stays the book's.
func chapterTitle(title string, chapter int) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return title
	}
	return fmt.Sprintf("%s %d", title, chapter)
}
