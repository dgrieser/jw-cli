package server

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/download"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/service"
	"github.com/dgrieser/jw-cli/internal/subtitles"
)

// The documents of a book the bible page opens from its chapter grid (?view=).
const (
	viewIntro   = "intro"
	viewOutline = "outline"
	viewGallery = "gallery"
)

// bookDocView is one document of a book as the bible page shows it. Which of
// its parts are set says which document it is.
type bookDocView struct {
	View  string
	Title string
	// the introduction: its video, facts, and the text under the video or
	// what the video says
	Intro      *model.BookIntro
	Stream     *model.MediaFile
	Notes      template.HTML
	Paragraphs [][]model.Cue
	// the outline or overview, heading by heading
	Outline []outlineRow
	// the media gallery, or one item of it (?item=)
	Gallery *model.BookGallery
	Item    *service.GalleryEntry
}

// outlineRow is one heading of a book's outline, leading into the reader at
// the verses it covers. Chapter is set on the first heading of a chapter in an
// outline that counts its verses by chapter.
type outlineRow struct {
	Chapter int
	Depth   int
	Title   string
	Label   string
	Href    string
}

// bookDocLinkView is one document listed below the chapter grid.
type bookDocLinkView struct {
	Kind  string
	Title string
	Href  string
}

// DocLinks are the documents the library lists below the book's chapters, as
// links into this page.
func (p biblePage) DocLinks() []bookDocLinkView {
	if p.BookNav == nil {
		return nil
	}
	var out []bookDocLinkView
	for _, l := range p.BookNav.Links {
		view := viewOutline
		switch l.Kind {
		case wol.BookIntroduction:
			view = viewIntro
		case wol.BookGallery:
			view = viewGallery
		}
		out = append(out, bookDocLinkView{Kind: l.Kind, Title: l.Title, Href: p.docLink(view, "")})
	}
	return out
}

// docLink opens one of the book's documents, or one item of its gallery.
func (p biblePage) docLink(view, item string) string {
	q := url.Values{"bible": {p.Edition}, "book": {fmt.Sprint(p.BookNav.Book)}, "view": {view}}
	if item != "" {
		q.Set("item", item)
	}
	return p.WithLang("/bible?" + q.Encode())
}

// GalleryItemLink opens one tile of the book's gallery on this page: its id is
// the last segment of its gallery address.
func (p biblePage) GalleryItemLink(tileURL string) string {
	if i := strings.IndexByte(tileURL, '#'); i >= 0 {
		tileURL = tileURL[:i]
	}
	return p.docLink(viewGallery, tileURL[strings.LastIndexByte(tileURL, '/')+1:])
}

// GalleryTitle names the gallery as the book's page lists it.
func (p biblePage) GalleryTitle() string {
	for _, l := range p.BookNav.Links {
		if l.Kind == wol.BookGallery {
			return l.Title
		}
	}
	return p.BookNav.Title
}

// GalleryLink leads from a gallery item back to the gallery.
func (p biblePage) GalleryLink() string { return p.docLink(viewGallery, "") }

// BookPageLink leads from a document back to the book's chapters.
func (p biblePage) BookPageLink() string { return p.BookLink(p.BookNav.Book) }

// Clock writes a transcript's time the way the media page does.
func (biblePage) Clock(sec float64) string { return subtitles.Clock(sec) }

// bookDoc fills in the document of the book asked for with ?view=. A document
// the library does not list for the book is said so on the chapter grid.
func (s *Server) bookDoc(r *http.Request, lng model.Language, view string, page *biblePage) {
	ctx := r.Context()
	book := page.BookNav.Book
	doc := &bookDocView{View: view}
	var err error
	switch view {
	case viewIntro:
		var intro model.BookIntro
		intro, err = s.svc.BookIntro(ctx, lng, page.Edition, book)
		if err != nil {
			break
		}
		doc.Intro, doc.Title = &intro, intro.Title
		if intro.Video != nil {
			if f, err := download.PickVideo(intro.Video.Files, streamQuality); err == nil {
				doc.Stream = &f
			}
		}
		if intro.NotesHTML != "" {
			doc.Notes = s.sanitized(intro.NotesHTML, s.svc.HTTP.Base.WOL)
		}
		doc.Paragraphs = subtitles.Paragraphs(intro.Transcript)
	case viewOutline:
		var o service.BookOutline
		o, err = s.svc.BookOutlineDoc(ctx, lng, page.Edition, book)
		if err != nil {
			break
		}
		doc.Title = o.Title
		doc.Outline = outlineRows(o.Items, o.ByChapter(), page)
	case viewGallery:
		if item := r.FormValue("item"); item != "" {
			var e service.GalleryEntry
			e, err = s.svc.BookGalleryItem(ctx, lng, page.Edition, book, item)
			if err != nil {
				break
			}
			doc.Item, doc.Title = &e, e.Caption
			if e.Video != nil {
				if f, err := download.PickVideo(e.Video.Files, streamQuality); err == nil {
					doc.Stream = &f
				}
			}
			break
		}
		var g model.BookGallery
		g, err = s.svc.BookGallery(ctx, lng, page.Edition, book)
		if err != nil {
			break
		}
		doc.Gallery, doc.Title = &g, g.Title
	default:
		err = fmt.Errorf("unknown view %q (want %s, %s or %s)", view, viewIntro, viewOutline, viewGallery)
	}
	if err != nil {
		page.Error = err.Error()
		return
	}
	page.Doc = doc
}

// outlineRows lays an outline out for the page, every heading a link to the
// verses it covers. An outline of contents counts its verses by chapter, so
// each chapter's headings are put under its number; an overview's headings
// name their chapters themselves.
func outlineRows(items []model.OutlineItem, byChapter bool, page *biblePage) []outlineRow {
	rows := make([]outlineRow, 0, len(items))
	last := 0
	for _, it := range items {
		row := outlineRow{Depth: min(it.Depth, 3), Title: it.Title, Label: it.Label}
		sc, sv := it.Start/1_000%1_000, it.Start%1_000
		ec, ev := it.End/1_000%1_000, it.End%1_000
		if byChapter && sc != last {
			row.Chapter, last = sc, sc
		}
		var ref string
		switch {
		case sv == 0 || (sv == 1 && ev == 999):
			ref = fmt.Sprintf("%s %d", page.BookName, sc) // a whole chapter
		case sc != ec:
			ref = fmt.Sprintf("%s %d:%d-%d:%d", page.BookName, sc, sv, ec, ev)
		case ev > sv:
			ref = fmt.Sprintf("%s %d:%d-%d", page.BookName, sc, sv, ev)
		default:
			ref = fmt.Sprintf("%s %d:%d", page.BookName, sc, sv)
		}
		q := url.Values{"bible": {page.Edition}, "ref": {ref}}
		row.Href = page.WithLang("/bible?" + q.Encode())
		rows = append(rows, row)
	}
	return rows
}
