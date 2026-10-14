package server

import (
	"net/http"
	"strings"

	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/service"
)

// citedPage is who quotes a verse, as jw bible cited lists it: every
// publication of the library citing it, and the videos quoting it, with the
// command's options as a form.
type citedPage struct {
	basePage
	Ref   string
	Sort  string
	Scope string
	// AllCats, Include and Exclude are the publications covered: every one,
	// only those named, or all but those named — by default all but the
	// bibles and the indexes, which cite every verse.
	AllCats  bool
	Include  string
	Exclude  string
	Videos   bool
	Excerpts bool
	Header   string
	Items    []resultView
}

// citedDefaultExclude is what a listing of who quotes a verse leaves out
// unless asked otherwise, as on the command line.
var citedDefaultExclude = []string{wol.CategoryBibles, wol.CategoryIndex}

// uiCited lists who quotes a verse: GET /cited?ref=Jer+31:15, or the passage
// a section of an unfolded page names (?vid=, ?to=, as /unfold/cited takes
// it), with sort, scope, all/include/exclude, videos=0 and excerpts=0 as the
// API's /api/v1/bible/cited takes them. A page without scripts reads the
// quotations of a verse here; with scripts they load in place.
func (s *Server) uiCited(w http.ResponseWriter, r *http.Request) {
	dropBlank(r, "include", "exclude")
	page := citedPage{
		basePage: s.base(r, "Quotations"),
		Ref:      strings.TrimSpace(r.FormValue("ref")),
		Sort:     valueOr(r, "sort", "newest"),
		Scope:    valueOr(r, "scope", "par"),
		AllCats:  boolParam(r, "all"),
		Include:  r.FormValue("include"),
		Exclude:  r.FormValue("exclude"),
		Videos:   boolParamOr(r, "videos", true),
		Excerpts: boolParamOr(r, "excerpts", true),
	}
	page.Title = page.T.UICitedTitle
	lng, err := s.language(r)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	ctx := r.Context()
	if page.Ref == "" && r.URL.Query().Has("vid") {
		refs, err := passagesParam(r)
		if err != nil {
			s.failUI(w, r, err)
			return
		}
		table := s.svc.BookTable(ctx, lng)
		names := make([]string, len(refs))
		for i, ref := range refs {
			names[i] = service.RefString(ref, table)
		}
		page.Ref = strings.Join(names, "; ")
	}
	if page.Ref == "" {
		s.render(w, http.StatusOK, "cited", page)
		return
	}
	query, label, err := s.svc.CitationQuery(ctx, lng, page.Ref)
	if err != nil {
		status, _ := classify(ctx, err)
		page.Error = readable(err, page.T)
		s.render(w, max(status, http.StatusBadRequest), "cited", page)
		return
	}
	p := service.SearchParams{
		Engine: "wol", Query: query, Sort: page.Sort, Scope: page.Scope, Excerpts: page.Excerpts,
		// include names the publications to cover, which no video is
		Videos: page.Videos && page.Include == "",
	}
	if p.Categories, err = categoriesParam(r, citedDefaultExclude, s.svc.KnownCategories(ctx, lng)); err != nil {
		page.Error = err.Error()
		s.render(w, http.StatusBadRequest, "cited", page)
		return
	}
	out, err := s.svc.CitedListing(ctx, lng, &p, nil)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	page.Header = text(lng).CitedResults(out.Total, label)
	page.Items = s.resultViews(out.Items, page.Lang)
	s.render(w, http.StatusOK, "cited", page)
}

// dropBlank takes the parameters a form sent empty out of the request: a
// field left blank asks for nothing, where the API reads ?exclude= as
// excluding nothing.
func dropBlank(r *http.Request, names ...string) {
	q := r.URL.Query()
	changed := false
	for _, name := range names {
		if q.Has(name) && strings.TrimSpace(q.Get(name)) == "" {
			q.Del(name)
			changed = true
		}
	}
	if changed {
		r.URL.RawQuery = q.Encode()
		r.Form = nil
	}
}
