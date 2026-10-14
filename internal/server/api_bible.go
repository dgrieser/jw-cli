package server

import (
	"net/http"
	"strings"

	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/render"
	"github.com/dgrieser/jw-cli/internal/service"
)

// refParam is the bible reference every /api/v1/bible endpoint takes.
func refParam(r *http.Request) (string, error) {
	ref := strings.TrimSpace(r.FormValue("ref"))
	if ref == "" {
		return "", errMissing("ref")
	}
	return ref, nil
}

// readResponse is /api/v1/bible/read: the passages as the CLI's JSON output
// carries them, plus the whole read rendered as one body in the asked-for
// format.
type readResponse struct {
	Ref      string            `json:"ref"`
	Format   string            `json:"format"`
	Body     string            `json:"body"`
	Passages []service.Passage `json:"passages"`
	Missing  []string          `json:"missing,omitempty"`
}

func (s *Server) apiBibleRead(w http.ResponseWriter, r *http.Request) {
	ref, err := refParam(r)
	if err != nil {
		badRequest(w, "%s", err)
		return
	}
	format, err := bodyFormat(r)
	if err != nil {
		badRequest(w, "%s", err)
		return
	}
	depth, err := intParam(r, "unfold", 0)
	if err != nil {
		badRequest(w, "%s", err)
		return
	}
	// every bible is not the one named, as on the command line; the reader's
	// form sends its edition along with all=1, but that is the page's affair
	if boolParam(r, "all") && r.FormValue("bible") != "" {
		badRequest(w, "%s", errExclusive("bible", "all"))
		return
	}
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	res, err := s.svc.ReadPassages(r.Context(), lng, service.ReadRequest{
		Refs:      ref,
		Edition:   valueOr(r, "bible", "nwtsty"),
		AllBibles: boolParam(r, "all"),
		Unfold:    unfoldConfig(depth, forceParam(r)),
		// ?outlines=0 where the CLI takes --no-outlines
		NoOutlines: !boolParamOr(r, "outlines", true),
	}, text(lng))
	if err != nil {
		failJSON(w, r, err)
		return
	}
	body, err := service.FormatPassages(res, format, render.Options{BaseURL: s.svc.HTTP.Base.WOL})
	if err != nil {
		failJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, readResponse{
		Ref: ref, Format: format.String(), Body: body,
		Passages: res.Passages, Missing: res.Missing,
	})
}

func (s *Server) apiBibleNotes(w http.ResponseWriter, r *http.Request) {
	s.bibleEntries(w, r, func(r *http.Request, lng model.Language, ref string) (any, error) {
		return s.svc.Notes(r.Context(), lng, ref)
	})
}

func (s *Server) apiBibleXrefs(w http.ResponseWriter, r *http.Request) {
	s.bibleEntries(w, r, func(r *http.Request, lng model.Language, ref string) (any, error) {
		return s.svc.XRefs(r.Context(), lng, ref, boolParam(r, "resolve"))
	})
}

func (s *Server) apiBibleResearch(w http.ResponseWriter, r *http.Request) {
	s.bibleEntries(w, r, func(r *http.Request, lng model.Language, ref string) (any, error) {
		return s.svc.Research(r.Context(), lng, ref, boolParam(r, "excerpts"))
	})
}

func (s *Server) apiBibleMedia(w http.ResponseWriter, r *http.Request) {
	s.bibleEntries(w, r, func(r *http.Request, lng model.Language, ref string) (any, error) {
		return s.svc.BibleMedia(r.Context(), lng, ref, true)
	})
}

// bibleEntries is the shared shape of the per-verse study endpoints: a ref in,
// a JSON list out. HTML fields in the entries are the site's own fragments,
// sanitized only on rendering — API consumers get the model as the CLI's
// -o json does.
func (s *Server) bibleEntries(w http.ResponseWriter, r *http.Request,
	fetch func(r *http.Request, lng model.Language, ref string) (any, error)) {
	ref, err := refParam(r)
	if err != nil {
		badRequest(w, "%s", err)
		return
	}
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	entries, err := fetch(r, lng, ref)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) apiBibleBooks(w http.ResponseWriter, r *http.Request) {
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.svc.Books(r.Context(), lng))
}

// apiBibleNav is the book grid of an edition (?bible=), or with ?book= the
// chapter grid of one book — the library's own bible navigation.
func (s *Server) apiBibleNav(w http.ResponseWriter, r *http.Request) {
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	book, err := intParam(r, "book", 0)
	if err != nil {
		badRequest(w, "%s", err)
		return
	}
	edition := valueOr(r, "bible", "nwtsty")
	var out any
	if book == 0 {
		out, err = s.svc.BibleNav(r.Context(), lng, edition)
	} else {
		out, err = s.svc.BookNav(r.Context(), lng, edition, book)
	}
	if err != nil {
		failJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// apiBibleIntro is a book's introduction, as jw bible intro -o json prints
// it: GET /api/v1/bible/intro?book=Mt.
func (s *Server) apiBibleIntro(w http.ResponseWriter, r *http.Request) {
	s.bookDocument(w, r, func(r *http.Request, lng model.Language, edition string, book int) (any, error) {
		return s.svc.BookIntro(r.Context(), lng, edition, book)
	})
}

// apiBibleOutline is a book's outline of contents, or the overview of the
// Gospels and Acts, as jw bible outline -o json prints it.
func (s *Server) apiBibleOutline(w http.ResponseWriter, r *http.Request) {
	s.bookDocument(w, r, func(r *http.Request, lng model.Language, edition string, book int) (any, error) {
		return s.svc.BookOutlineDoc(r.Context(), lng, edition, book)
	})
}

// apiBibleGallery is a book's media gallery, as jw bible gallery -o json
// prints it.
func (s *Server) apiBibleGallery(w http.ResponseWriter, r *http.Request) {
	s.bookDocument(w, r, func(r *http.Request, lng model.Language, edition string, book int) (any, error) {
		return s.svc.BookGallery(r.Context(), lng, edition, book)
	})
}

// bookDocument is the shared shape of the book document endpoints: a book
// (?book=, named the way a reference names it or by number) of an edition
// (?bible=) in, the document as JSON out. A document the library does not
// list for the book is not found.
func (s *Server) bookDocument(w http.ResponseWriter, r *http.Request,
	fetch func(r *http.Request, lng model.Language, edition string, book int) (any, error)) {
	name := strings.TrimSpace(r.FormValue("book"))
	if name == "" {
		badRequest(w, "%s", errMissing("book"))
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
	book, err := s.svc.ParseBook(r.Context(), lng, name)
	if err != nil {
		badRequest(w, "%s", err)
		return
	}
	doc, err := fetch(r, lng, edition, book)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

// apiBibleVerses says how many verses a chapter has in an edition:
// GET /api/v1/bible/verses?bible=nwtsty&book=40&chapter=2.
func (s *Server) apiBibleVerses(w http.ResponseWriter, r *http.Request) {
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
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
	n, err := s.svc.VerseCount(r.Context(), lng, edition, book, chapter)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"book": book, "chapter": chapter, "verses": n})
}
