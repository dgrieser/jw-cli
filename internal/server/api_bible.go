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
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	res, err := s.svc.ReadPassages(r.Context(), lng, service.ReadRequest{
		Refs:      ref,
		Edition:   valueOr(r, "bible", "nwtsty"),
		AllBibles: boolParam(r, "all"),
		Unfold:    unfoldConfig(depth),
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
