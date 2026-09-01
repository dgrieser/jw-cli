package server

import (
	"net/http"
	"strings"

	"github.com/dgrieser/jw-cli/internal/api/pubmedia"
	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/render"
	"github.com/dgrieser/jw-cli/internal/service"
)

// maxSearchLimit caps ?limit=: the API serves pages, not dumps.
const maxSearchLimit = 50

func (s *Server) apiLanguages(w http.ResponseWriter, r *http.Request) {
	langs, err := s.svc.Languages(r.Context())
	if err != nil {
		failJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, service.FilterLanguages(langs, r.FormValue("q")))
}

func (s *Server) apiSearch(w http.ResponseWriter, r *http.Request) {
	// cheap validation before the language list is fetched
	if strings.TrimSpace(r.FormValue("q")) == "" {
		badRequest(w, "%s", errMissing("q"))
		return
	}
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	p, page, err := s.searchParams(r, lng)
	if err != nil {
		badRequest(w, "%s", err)
		return
	}
	out, err := s.svc.RunSearch(r.Context(), lng, p, page, nil)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// searchParams reads the query parameters shared by search and cited.
func (s *Server) searchParams(r *http.Request, lng model.Language) (*service.SearchParams, int, error) {
	q := strings.TrimSpace(r.FormValue("q"))
	if q == "" {
		return nil, 0, errMissing("q")
	}
	limit, err := intParam(r, "limit", 10)
	if err != nil {
		return nil, 0, err
	}
	limit = max(1, min(limit, maxSearchLimit))
	page, err := intParam(r, "page", 1)
	if err != nil {
		return nil, 0, err
	}
	page = max(1, page)
	p := &service.SearchParams{
		Engine:   valueOr(r, "engine", "jworg"),
		Query:    q,
		Facet:    valueOr(r, "type", "all"),
		Sort:     valueOr(r, "sort", "rel"),
		Scope:    valueOr(r, "scope", "par"),
		Limit:    limit,
		Excerpts: boolParam(r, "excerpts"),
	}
	if p.Engine == "wol" {
		if p.Categories, err = s.categoriesParam(r, lng, nil); err != nil {
			return nil, 0, err
		}
	}
	return p, page, nil
}

// categoriesParam reads the wol category filter (?all, ?include=, ?exclude=)
// the way the CLI flags resolve it.
func (s *Server) categoriesParam(r *http.Request, lng model.Language, defaultExclude []string) (service.WOLCategories, error) {
	q := r.URL.Query()
	all := boolParam(r, "all")
	include := splitCSV(q.Get("include"))
	var exclude []string
	if q.Has("exclude") {
		exclude = splitCSV(q.Get("exclude"))
		if exclude == nil {
			exclude = []string{}
		}
	}
	if all && (len(include) > 0 || exclude != nil) {
		return service.WOLCategories{}, errExclusive("all", "include/exclude")
	}
	if len(include) > 0 && exclude != nil {
		return service.WOLCategories{}, errExclusive("include", "exclude")
	}
	return service.ResolveCategories(all, include, exclude, defaultExclude,
		s.svc.KnownCategories(r.Context(), lng))
}

func (s *Server) apiBibleCited(w http.ResponseWriter, r *http.Request) {
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	ref := strings.TrimSpace(r.FormValue("ref"))
	if ref == "" {
		badRequest(w, "%s", errMissing("ref"))
		return
	}
	query, _, err := s.svc.CitationQuery(r.Context(), lng, ref)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	p := service.SearchParams{
		Engine:   "wol",
		Query:    query,
		Sort:     valueOr(r, "sort", "newest"),
		Scope:    valueOr(r, "scope", "par"),
		Excerpts: boolParam(r, "excerpts"),
	}
	if p.Categories, err = s.categoriesParam(r, lng, []string{wol.CategoryBibles, wol.CategoryIndex}); err != nil {
		badRequest(w, "%s", err)
		return
	}
	out, err := s.svc.CitedListing(r.Context(), lng, &p, nil)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// articleResponse is the article shape shared by /article, /dailytext and
// /meetings: the parsed document plus its body rendered in the asked-for
// format. Images and scripture references cover the CLI's --images and --refs.
type articleResponse struct {
	DocID         int                     `json:"docid,omitempty"`
	Title         string                  `json:"title"`
	URL           string                  `json:"url,omitempty"`
	Format        string                  `json:"format"`
	Body          string                  `json:"body"`
	Images        []model.MediaAsset      `json:"images,omitempty"`
	ScriptureRefs []model.ScriptureAnchor `json:"scriptureRefs,omitempty"`
}

// articleParams validates the rendering parameters of an article-shaped
// endpoint up front, before anything is fetched upstream.
func articleParams(r *http.Request) (render.Format, int, error) {
	format, err := bodyFormat(r)
	if err != nil {
		return format, 0, err
	}
	depth, err := intParam(r, "unfold", 0)
	if err != nil {
		return format, 0, err
	}
	return format, depth, nil
}

// writeArticleJSON optionally unfolds an article, renders its body, and writes
// the article shape.
func (s *Server) writeArticleJSON(w http.ResponseWriter, r *http.Request, lng model.Language,
	art model.Article, format render.Format, depth int) {
	if depth > 0 {
		body, err := s.svc.UnfoldArticle(r.Context(), lng, art, unfoldConfig(depth), text(lng))
		if err != nil {
			failJSON(w, r, err)
			return
		}
		art.HTML = body
	}
	body, err := render.Render(art.HTML, format, render.Options{BaseURL: s.svc.ArticleBase(art)})
	if err != nil {
		failJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, articleResponse{
		DocID: art.DocID, Title: art.Title, URL: art.URL,
		Format: format.String(), Body: body,
		Images: art.Images, ScriptureRefs: art.ScriptureRefs,
	})
}

func (s *Server) apiArticle(w http.ResponseWriter, r *http.Request) {
	target := strings.TrimSpace(r.FormValue("target"))
	if target == "" {
		badRequest(w, "%s", errMissing("target"))
		return
	}
	format, depth, err := articleParams(r)
	if err != nil {
		badRequest(w, "%s", err)
		return
	}
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	art, err := s.svc.Article(r.Context(), lng, target)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	s.writeArticleJSON(w, r, lng, art, format, depth)
}

func (s *Server) apiDailyText(w http.ResponseWriter, r *http.Request) {
	format, depth, err := articleParams(r)
	if err != nil {
		badRequest(w, "%s", err)
		return
	}
	date, err := dateParam(r)
	if err != nil {
		badRequest(w, "%s", err)
		return
	}
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	art, err := s.svc.DailyText(r.Context(), lng, date)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	s.writeArticleJSON(w, r, lng, art, format, depth)
}

func (s *Server) apiMeetings(w http.ResponseWriter, r *http.Request) {
	format, depth, err := articleParams(r)
	if err != nil {
		badRequest(w, "%s", err)
		return
	}
	date, err := dateParam(r)
	if err != nil {
		badRequest(w, "%s", err)
		return
	}
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	art, err := s.svc.Meetings(r.Context(), lng, date)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	s.writeArticleJSON(w, r, lng, art, format, depth)
}

func (s *Server) apiMeetingPart(w http.ResponseWriter, r *http.Request) {
	part := r.PathValue("part")
	if part != "midweek" && part != "weekend" {
		badRequest(w, "unknown meeting %q (want midweek or weekend)", part)
		return
	}
	format, depth, err := articleParams(r)
	if err != nil {
		badRequest(w, "%s", err)
		return
	}
	date, err := dateParam(r)
	if err != nil {
		badRequest(w, "%s", err)
		return
	}
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	art, err := s.svc.MeetingPart(r.Context(), lng, date, part)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	s.writeArticleJSON(w, r, lng, art, format, depth)
}

func (s *Server) apiMediaCategories(w http.ResponseWriter, r *http.Request) {
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	cats, err := s.svc.Mediator.RootCategories(r.Context(), lng.Symbol)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, cats)
}

func (s *Server) apiMediaCategory(w http.ResponseWriter, r *http.Request) {
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	limit, err := intParam(r, "limit", 0)
	if err != nil {
		badRequest(w, "%s", err)
		return
	}
	offset, err := intParam(r, "offset", 0)
	if err != nil {
		badRequest(w, "%s", err)
		return
	}
	cat, err := s.svc.Mediator.Category(r.Context(), lng.Symbol, r.PathValue("key"), limit, offset)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, cat)
}

func (s *Server) apiMediaItem(w http.ResponseWriter, r *http.Request) {
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	item, err := s.svc.Mediator.MediaItem(r.Context(), lng.Symbol, r.PathValue("lank"))
	if err != nil {
		failJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// pubResponse is /api/v1/pub: the publication's names plus its files as
// listing rows, each with its direct download URL.
type pubResponse struct {
	PubName       string         `json:"pubName"`
	ParentPubName string         `json:"parentPubName,omitempty"`
	Pub           string         `json:"pub"`
	Issue         string         `json:"issue,omitempty"`
	BookNum       int            `json:"booknum,omitempty"`
	Files         []model.Result `json:"files"`
}

// pubQuery reads the publication parameters shared by /api/v1/pub and
// /download/pub.
func pubQuery(r *http.Request, lng model.Language) (pubmedia.Query, error) {
	docid, err := intParam(r, "docid", 0)
	if err != nil {
		return pubmedia.Query{}, err
	}
	booknum, err := intParam(r, "booknum", 0)
	if err != nil {
		return pubmedia.Query{}, err
	}
	track, err := intParam(r, "track", 0)
	if err != nil {
		return pubmedia.Query{}, err
	}
	q := pubmedia.Query{
		Pub:      r.FormValue("pub"),
		DocID:    docid,
		Issue:    r.FormValue("issue"),
		BookNum:  booknum,
		Track:    track,
		Formats:  service.SplitFormats(splitCSV(r.FormValue("fileformat"))),
		Lang:     lng.Symbol,
		AllLangs: boolParam(r, "allLangs"),
	}
	if q.Pub == "" && q.DocID == 0 {
		return pubmedia.Query{}, errMissing("pub (or docid)")
	}
	return q, nil
}

func (s *Server) apiPub(w http.ResponseWriter, r *http.Request) {
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	q, err := pubQuery(r, lng)
	if err != nil {
		badRequest(w, "%s", err)
		return
	}
	pm, err := s.svc.PubMedia.Links(r.Context(), q)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pubResponse{
		PubName: pm.PubName, ParentPubName: pm.ParentPubName,
		Pub: pm.Pub, Issue: pm.Issue, BookNum: pm.BookNum,
		Files: service.PubFilesToResults(pm),
	})
}
