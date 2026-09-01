package server

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/render"
	"github.com/dgrieser/jw-cli/internal/service"
)

// basePage is what every UI page carries: the title, the language round-trip,
// and where the language form posts back to.
type basePage struct {
	Title string
	// Lang is the ?lang= of the request, kept on every internal link so the
	// chosen language follows the reader around.
	Lang string
	// Path is the current page, the action of the language form.
	Path string
	// Hidden are the current query parameters (minus lang), so switching the
	// language re-runs the same request.
	Hidden []hiddenField
	// Error is the banner an upstream failure renders above the page.
	Error string
}

type hiddenField struct{ Name, Value string }

// WithLang appends the page's language to an internal link.
func (p basePage) WithLang(path string) string {
	if p.Lang == "" {
		return path
	}
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "lang=" + url.QueryEscape(p.Lang)
}

func (s *Server) base(r *http.Request, title string) basePage {
	q := r.URL.Query()
	lang := q.Get("lang")
	q.Del("lang")
	var hidden []hiddenField
	for name, vals := range q {
		for _, v := range vals {
			hidden = append(hidden, hiddenField{Name: name, Value: v})
		}
	}
	sort.Slice(hidden, func(i, j int) bool {
		if hidden[i].Name != hidden[j].Name {
			return hidden[i].Name < hidden[j].Name
		}
		return hidden[i].Value < hidden[j].Value
	})
	return basePage{Title: title, Lang: lang, Path: r.URL.Path, Hidden: hidden}
}

// render executes one page template into a buffer first, so a template error
// becomes a clean 500 instead of a half-written page.
func (s *Server) render(w http.ResponseWriter, status int, page string, data any) {
	var buf bytes.Buffer
	if err := s.templates[page].ExecuteTemplate(&buf, "base.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

// errorPage is the UI's failure surface, mapped through the same status codes
// as the API.
type errorPage struct {
	basePage
	Status  int
	Message string
}

func (s *Server) failUI(w http.ResponseWriter, r *http.Request, err error) {
	status, _ := classify(r.Context(), err)
	if status == 0 {
		return
	}
	s.render(w, status, "error", errorPage{
		basePage: s.base(r, "Error"),
		Status:   status,
		Message:  err.Error(),
	})
}

// sanitized runs a site HTML fragment through the same sanitizer the CLI
// renders with, and only then marks it safe for the page.
func (s *Server) sanitized(fragment, baseURL string) template.HTML {
	out, err := render.Render(fragment, render.HTML, render.Options{BaseURL: baseURL})
	if err != nil {
		return ""
	}
	return template.HTML(out) //nolint:gosec // bluemonday-sanitized above
}

// inlineText flattens a fragment with inline markup ("<strong>Jesus</strong>")
// to its plain text, for places the page escapes itself: titles, labels, links.
func inlineText(fragment string) string {
	return render.Inline(fragment, render.InlineOptions{})
}

// resultView is one listing row as the UI shows it: where it leads inside the
// site, where it points outside, and what it says about itself.
type resultView struct {
	Index    int
	Kind     string
	Title    string
	Context  string
	Snippet  template.HTML
	Excerpt  template.HTML
	Href     string // internal page, when the row has one
	External string // the row's own link on jw.org / wol.jw.org
	FileURL  string
	ImageURL string
	Duration string
	Size     string
	Meta     []string // image metadata lines
}

// resultViews prepares listing rows for a page. lang keeps the reader's
// language on the internal links.
func (s *Server) resultViews(items []model.Result, lang string) []resultView {
	base := basePage{Lang: lang}
	out := make([]resultView, 0, len(items))
	for i, r := range items {
		v := resultView{
			Index:    i + 1,
			Kind:     r.Kind,
			Title:    inlineText(r.Title),
			Context:  inlineText(r.Context),
			Snippet:  s.sanitized(r.Snippet, s.svc.HTTP.Base.WOL),
			Excerpt:  s.sanitized(r.Excerpt, s.svc.HTTP.Base.WOL),
			FileURL:  r.FileURL,
			ImageURL: r.ImageURL,
			Duration: r.Duration,
		}
		if r.Filesize > 0 {
			v.Size = humanSize(r.Filesize)
		}
		if r.Image != nil {
			for _, line := range []string{r.Image.Description, r.Image.Alt, r.Image.Credit} {
				if line != "" && line != r.Title {
					v.Meta = append(v.Meta, line)
				}
			}
		}
		switch {
		case r.Kind == "category" && r.CategoryKey != "":
			v.Href = base.WithLang("/media/category/" + url.PathEscape(r.CategoryKey))
		case (r.Kind == "video" || r.Kind == "audio") && r.LANK != "":
			v.Href = base.WithLang("/media/item/" + url.PathEscape(r.LANK))
		default:
			if target := firstNonEmpty(r.WOLLink, r.JWLink, docidTarget(r.DocID)); target != "" {
				v.Href = articleHref(target, lang)
			}
		}
		switch {
		case r.JWLink != "":
			v.External = r.JWLink
		case r.WOLLink != "":
			v.External = r.WOLLink
		}
		out = append(out, v)
	}
	return out
}

func docidTarget(docid int) string {
	if docid == 0 {
		return ""
	}
	return fmt.Sprint(docid)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// articleHref is the internal reader page for a wol/jw.org target.
func articleHref(target, lang string) string {
	q := url.Values{"target": {target}}
	if lang != "" {
		q.Set("lang", lang)
	}
	return "/article?" + q.Encode()
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// --- pages ---------------------------------------------------------------

func (s *Server) uiIndex(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, "index", struct{ basePage }{s.base(r, "jw")})
}

type searchPage struct {
	basePage
	Query    string
	Engine   string
	Type     string
	Sort     string
	Scope    string
	Excerpts bool
	Page     int
	Header   string
	Items    []resultView
	PrevURL  string
	NextURL  string
}

func (s *Server) uiSearch(w http.ResponseWriter, r *http.Request) {
	page := searchPage{
		basePage: s.base(r, "Search"),
		Query:    strings.TrimSpace(r.FormValue("q")),
		Engine:   valueOr(r, "engine", "jworg"),
		Type:     valueOr(r, "type", "all"),
		Sort:     valueOr(r, "sort", "rel"),
		Scope:    valueOr(r, "scope", "par"),
		Excerpts: boolParam(r, "excerpts"),
	}
	if page.Query == "" {
		s.render(w, http.StatusOK, "search", page)
		return
	}
	lng, err := s.language(r)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	p, pageNum, err := s.searchParams(r, lng)
	if err != nil {
		page.Error = err.Error()
		s.render(w, http.StatusBadRequest, "search", page)
		return
	}
	out, err := s.svc.RunSearch(r.Context(), lng, p, pageNum, nil)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	page.Page = pageNum
	txt := text(lng)
	if out.Kind == "wol-search" {
		page.Header = txt.WolResults(out.Total, out.Query, out.Page)
	} else {
		page.Header = txt.Results(out.Total, out.Query)
	}
	page.Items = s.resultViews(out.Items, page.Lang)
	if pageNum > 1 {
		page.PrevURL = pagedURL(r, pageNum-1)
	}
	if len(out.Items) > 0 {
		page.NextURL = pagedURL(r, pageNum+1)
	}
	s.render(w, http.StatusOK, "search", page)
}

// pagedURL is the current request with another page number.
func pagedURL(r *http.Request, page int) string {
	q := r.URL.Query()
	q.Set("page", fmt.Sprint(page))
	return r.URL.Path + "?" + q.Encode()
}

type articlePage struct {
	basePage
	Target  string
	Heading string
	URL     string
	Unfold  int
	Body    template.HTML
	Refs    []model.ScriptureAnchor
	Images  []model.MediaAsset
}

func (s *Server) uiArticle(w http.ResponseWriter, r *http.Request) {
	page := articlePage{basePage: s.base(r, "Article"), Target: strings.TrimSpace(r.FormValue("target"))}
	if page.Target == "" {
		s.render(w, http.StatusOK, "article", page)
		return
	}
	lng, err := s.language(r)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	depth, err := intParam(r, "unfold", 0)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	art, err := s.svc.Article(r.Context(), lng, page.Target)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	if depth > 0 {
		body, err := s.svc.UnfoldArticle(r.Context(), lng, art, unfoldConfig(depth), text(lng))
		if err != nil {
			s.failUI(w, r, err)
			return
		}
		art.HTML = body
	}
	page.Unfold = depth
	page.Title = firstNonEmpty(art.Title, "Article")
	page.Heading = art.Title
	page.URL = art.URL
	page.Body = s.sanitized(art.HTML, s.svc.ArticleBase(art))
	page.Refs = art.ScriptureRefs
	page.Images = art.Images
	s.render(w, http.StatusOK, "article", page)
}

// documentPage renders the daily text and the meeting pages: an article with a
// date picker above it.
type documentPage struct {
	basePage
	Heading string
	URL     string
	Date    string
	Part    string // meetings: "", "midweek" or "weekend"
	Body    template.HTML
}

// PartURL addresses one of the meeting tabs, keeping the chosen week and
// language.
func (p documentPage) PartURL(part string) string {
	path := "/meetings"
	if part != "" {
		path += "/" + part
	}
	q := url.Values{}
	if p.Date != "" {
		q.Set("date", p.Date)
	}
	if p.Lang != "" {
		q.Set("lang", p.Lang)
	}
	if enc := q.Encode(); enc != "" {
		return path + "?" + enc
	}
	return path
}

func (s *Server) uiDailyText(w http.ResponseWriter, r *http.Request) {
	lng, err := s.language(r)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	date, err := dateParam(r)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	art, err := s.svc.DailyText(r.Context(), lng, date)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	s.render(w, http.StatusOK, "dailytext", documentPage{
		basePage: s.base(r, firstNonEmpty(art.Title, "Daily text")),
		Heading:  art.Title,
		URL:      art.URL,
		Date:     r.FormValue("date"),
		Body:     s.sanitized(art.HTML, s.svc.ArticleBase(art)),
	})
}

func (s *Server) uiMeetings(w http.ResponseWriter, r *http.Request) {
	part := r.PathValue("part")
	if part != "" && part != "midweek" && part != "weekend" {
		s.failUI(w, r, fmt.Errorf("unknown meeting %q (want midweek or weekend)", part))
		return
	}
	lng, err := s.language(r)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	date, err := dateParam(r)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	var art model.Article
	if part == "" {
		art, err = s.svc.Meetings(r.Context(), lng, date)
	} else {
		art, err = s.svc.MeetingPart(r.Context(), lng, date, part)
	}
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	s.render(w, http.StatusOK, "meetings", documentPage{
		basePage: s.base(r, firstNonEmpty(art.Title, "Meetings")),
		Heading:  art.Title,
		URL:      art.URL,
		Date:     r.FormValue("date"),
		Part:     part,
		Body:     s.sanitized(art.HTML, s.svc.ArticleBase(art)),
	})
}

type mediaPage struct {
	basePage
	Heading string
	Items   []resultView
}

func (s *Server) uiMedia(w http.ResponseWriter, r *http.Request) {
	lng, err := s.language(r)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	cats, err := s.svc.Mediator.RootCategories(r.Context(), lng.Symbol)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	page := mediaPage{basePage: s.base(r, "Media"), Heading: text(lng).MediaCategories}
	page.Items = s.resultViews(service.CategoriesToResults(cats), page.Lang)
	s.render(w, http.StatusOK, "media", page)
}

func (s *Server) uiMediaCategory(w http.ResponseWriter, r *http.Request) {
	lng, err := s.language(r)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	limit, err := intParam(r, "limit", 0)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	offset, err := intParam(r, "offset", 0)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	cat, err := s.svc.Mediator.Category(r.Context(), lng.Symbol, r.PathValue("key"), limit, offset)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	page := mediaPage{
		basePage: s.base(r, cat.Name),
		Heading:  fmt.Sprintf("%s (%s)", cat.Name, cat.Key),
	}
	items := append(service.CategoriesToResults(cat.Subcategories), service.MediaToResults(cat.Media)...)
	page.Items = s.resultViews(items, page.Lang)
	s.render(w, http.StatusOK, "media_category", page)
}

type mediaItemPage struct {
	basePage
	Item  model.MediaItem
	Image string
	Files []mediaFileView
}

type mediaFileView struct {
	Label     string
	Size      string
	URL       string
	Subtitles string
}

func (s *Server) uiMediaItem(w http.ResponseWriter, r *http.Request) {
	lng, err := s.language(r)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	item, err := s.svc.Mediator.MediaItem(r.Context(), lng.Symbol, r.PathValue("lank"))
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	page := mediaItemPage{
		basePage: s.base(r, item.Title),
		Item:     item,
		Image:    service.BestImage(item.Images),
	}
	for _, f := range item.Files {
		label := f.Label
		if label == "" {
			label = f.MimeType
		}
		page.Files = append(page.Files, mediaFileView{
			Label: label, Size: humanSize(f.Filesize), URL: f.URL, Subtitles: f.SubtitlesURL,
		})
	}
	s.render(w, http.StatusOK, "media_item", page)
}

type pubPage struct {
	basePage
	Pub, DocID, Issue, BookNum, Track, Formats string
	AllLangs                                   bool
	Heading                                    string
	Items                                      []resultView
}

func (s *Server) uiPub(w http.ResponseWriter, r *http.Request) {
	page := pubPage{
		basePage: s.base(r, "Publications"),
		Pub:      r.FormValue("pub"),
		DocID:    r.FormValue("docid"),
		Issue:    r.FormValue("issue"),
		BookNum:  r.FormValue("booknum"),
		Track:    r.FormValue("track"),
		Formats:  r.FormValue("fileformat"),
		AllLangs: boolParam(r, "allLangs"),
	}
	if page.Pub == "" && page.DocID == "" {
		s.render(w, http.StatusOK, "pub", page)
		return
	}
	lng, err := s.language(r)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	q, err := pubQuery(r, lng)
	if err != nil {
		page.Error = err.Error()
		s.render(w, http.StatusBadRequest, "pub", page)
		return
	}
	pm, err := s.svc.PubMedia.Links(r.Context(), q)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	page.Heading = pm.PubName
	if pm.ParentPubName != "" && pm.ParentPubName != pm.PubName {
		page.Heading = pm.ParentPubName + " — " + pm.PubName
	}
	page.Items = s.resultViews(service.PubFilesToResults(pm), page.Lang)
	s.render(w, http.StatusOK, "pub", page)
}

type languagesPage struct {
	basePage
	Query     string
	Languages []model.Language
}

func (s *Server) uiLanguages(w http.ResponseWriter, r *http.Request) {
	langs, err := s.svc.Languages(r.Context())
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	q := r.FormValue("q")
	s.render(w, http.StatusOK, "languages", languagesPage{
		basePage:  s.base(r, "Languages"),
		Query:     q,
		Languages: service.FilterLanguages(langs, q),
	})
}

type biblePage struct {
	basePage
	Ref     string
	View    string
	Edition string
	Unfold  int
	// Editions is the picker's option list.
	Editions []string
	// Read
	Body template.HTML
	// Notes / Research
	Notes    []notesEntryView
	Research []researchEntryView
	// XRefs
	XRefs []service.XRefsEntry
	// Cited / Media listings
	Header string
	Items  []resultView
}

type notesEntryView struct {
	Ref   string
	Verse template.HTML
	Notes []template.HTML
}

type researchEntryView struct {
	Ref   string
	Verse template.HTML
	Items []researchItemView
}

type researchItemView struct {
	Title   string
	URL     string
	Source  string
	Excerpt template.HTML
}

// bibleViews are the tabs of the bible page.
var bibleViews = []string{"read", "notes", "xrefs", "research", "cited", "media"}

// TabURL is the address of one tab with the page's current reference.
func (p biblePage) TabURL(view string) string {
	q := url.Values{}
	if p.Ref != "" {
		q.Set("ref", p.Ref)
	}
	if view != "read" {
		q.Set("view", view)
	}
	if p.Lang != "" {
		q.Set("lang", p.Lang)
	}
	if enc := q.Encode(); enc != "" {
		return "/bible?" + enc
	}
	return "/bible"
}

// Views lists the tabs, for the template.
func (p biblePage) Views() []string { return bibleViews }

func (s *Server) uiBible(w http.ResponseWriter, r *http.Request) {
	page := biblePage{
		basePage: s.base(r, "Bible"),
		Ref:      strings.TrimSpace(r.FormValue("ref")),
		View:     valueOr(r, "view", "read"),
		Edition:  valueOr(r, "bible", "nwtsty"),
		Editions: wol.BibleEditions,
	}
	if !slices.Contains(bibleViews, page.View) {
		s.failUI(w, r, fmt.Errorf("unknown view %q", page.View))
		return
	}
	if page.Ref == "" {
		s.render(w, http.StatusOK, "bible", page)
		return
	}
	lng, err := s.language(r)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	depth, err := intParam(r, "unfold", 0)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	page.Unfold = depth
	wolBase := s.svc.HTTP.Base.WOL
	switch page.View {
	case "read":
		res, err := s.svc.ReadPassages(r.Context(), lng, service.ReadRequest{
			Refs: page.Ref, Edition: page.Edition, AllBibles: boolParam(r, "all"),
			Unfold: unfoldConfig(depth),
		}, text(lng))
		if err != nil {
			s.failUI(w, r, err)
			return
		}
		body, err := service.FormatPassages(res, render.HTML, render.Options{BaseURL: wolBase})
		if err != nil {
			s.failUI(w, r, err)
			return
		}
		// FormatPassages already sanitizes each passage through render.Render;
		// the headings around them are escaped there too.
		page.Body = template.HTML(body) //nolint:gosec // sanitized per passage above
	case "notes":
		entries, err := s.svc.Notes(r.Context(), lng, page.Ref)
		if err != nil {
			s.failUI(w, r, err)
			return
		}
		for _, e := range entries {
			view := notesEntryView{Ref: e.Ref, Verse: s.sanitized(e.Verse.HTML, wolBase)}
			for _, n := range e.Notes {
				view.Notes = append(view.Notes, s.sanitized(n.HTML, wolBase))
			}
			page.Notes = append(page.Notes, view)
		}
	case "xrefs":
		page.XRefs, err = s.svc.XRefs(r.Context(), lng, page.Ref, false)
		if err != nil {
			s.failUI(w, r, err)
			return
		}
	case "research":
		entries, err := s.svc.Research(r.Context(), lng, page.Ref, boolParam(r, "excerpts"))
		if err != nil {
			s.failUI(w, r, err)
			return
		}
		for _, e := range entries {
			view := researchEntryView{Ref: e.Ref, Verse: s.sanitized(e.Verse.HTML, wolBase)}
			for _, it := range e.Items {
				view.Items = append(view.Items, researchItemView{
					Title:   it.Title,
					URL:     absoluteWOL(firstNonEmpty(it.ArticleURL, it.PCPath), wolBase),
					Source:  it.Source,
					Excerpt: s.sanitized(it.ExcerptHTML, wolBase),
				})
			}
			page.Research = append(page.Research, view)
		}
	case "cited":
		query, label, err := s.svc.CitationQuery(r.Context(), lng, page.Ref)
		if err != nil {
			s.failUI(w, r, err)
			return
		}
		p := service.SearchParams{
			Engine: "wol", Query: query, Sort: valueOr(r, "sort", "newest"),
			Scope: "par", Excerpts: boolParam(r, "excerpts"),
		}
		if p.Categories, err = s.categoriesParam(r, lng, []string{wol.CategoryBibles, wol.CategoryIndex}); err != nil {
			s.failUI(w, r, err)
			return
		}
		out, err := s.svc.CitedListing(r.Context(), lng, &p, nil)
		if err != nil {
			s.failUI(w, r, err)
			return
		}
		page.Header = text(lng).CitedResults(out.Total, label)
		page.Items = s.resultViews(out.Items, page.Lang)
	case "media":
		items, err := s.svc.BibleMedia(r.Context(), lng, page.Ref, true)
		if err != nil {
			s.failUI(w, r, err)
			return
		}
		page.Header = fmt.Sprintf(text(lng).MediaOn, page.Ref)
		page.Items = s.resultViews(items, page.Lang)
	}
	s.render(w, http.StatusOK, "bible", page)
}

// absoluteWOL roots a wol path at the library, so links in listings leave the
// server for the real page.
func absoluteWOL(path, base string) string {
	if path == "" || strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return base + path
}
