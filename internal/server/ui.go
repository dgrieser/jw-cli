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
	"sort"
	"strings"
	"sync"

	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/download"
	"github.com/dgrieser/jw-cli/internal/i18n"
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
	// Locale is the resolved content language, for the document's lang
	// attribute; T is the catalog the chrome is written in, which follows the
	// same language wherever one exists.
	Locale string
	T      *i18n.Messages
}

type hiddenField struct{ Name, Value string }

// Active marks the navigation entry of the page being shown: "active" when
// the current path is the entry's path or lies under it, otherwise empty.
func (p basePage) Active(path string) string {
	if p.Path == path || (path != "/" && strings.HasPrefix(p.Path, path+"/")) {
		return "active"
	}
	return ""
}

// UIText is what the page's script says, in the language of the page, and the
// language it asks the server in.
func (p basePage) UIText() map[string]string {
	t := p.T
	return map[string]string{
		"lang":           p.Lang,
		"unfold":         t.UIUnfold,
		"unfoldItem":     t.UIUnfoldItem,
		"unfoldAll":      t.UIUnfoldAll,
		"depth":          t.UIUnfoldDepth,
		"depthN":         t.UIUnfoldDepthN,
		"foldAway":       t.UIFoldAway,
		"openAll":        t.UIOpenAll,
		"closeAll":       t.UICloseAll,
		"stop":           t.UIStop,
		"nothing":        t.UINothingToUnfold,
		"error":          t.UIUnfoldError,
		"retry":          t.UIRetry,
		"stageStudy":     t.UIStageStudy,
		"stageRefs":      t.UIStageReferences,
		"stageCited":     t.UIStageCited,
		"progressLevel":  t.UIProgressLevel,
		"progressItems":  t.UIProgressItems,
		"unfolding":      t.UIUnfolding,
		"loading":        t.UILoading,
		"expensiveTitle": t.UIExpensiveTitle,
		"unfoldAnyway":   t.UIUnfoldAnyway,
		"translations":   t.TranslationsHeading,
		"footnotes":      t.FootnotesHeading,
	}
}

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
	page := basePage{Title: title, Lang: lang, Path: r.URL.Path, Hidden: hidden, Locale: "en", T: i18n.EN.Text()}
	if lng, err := s.language(r); err == nil {
		page.Locale, page.T = lng.Locale, text(lng)
	}
	return page
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

// confirmPage is the web's answer to the terminal prompt: an expansion that
// costs more than the server spends unasked is offered rather than refused.
type confirmPage struct {
	basePage
	Message  string
	Level    int
	Requests int
	ForceURL string
	BackURL  string
}

func (s *Server) failUI(w http.ResponseWriter, r *http.Request, err error) {
	status, _ := classify(r.Context(), err)
	if status == 0 {
		return
	}
	var te *tooExpensiveError
	if errors.As(err, &te) {
		s.render(w, status, "confirm", confirmPage{
			basePage: s.base(r, "Unfold?"),
			Message:  err.Error(),
			Level:    te.level,
			Requests: te.requests,
			ForceURL: urlWith(r, "force", "1"),
			BackURL:  urlWith(r, "unfold", ""),
		})
		return
	}
	s.render(w, status, "error", errorPage{
		basePage: s.base(r, "Error"),
		Status:   status,
		Message:  err.Error(),
	})
}

// urlWith is this request's own URL with one parameter set, or dropped when
// the value is empty.
func urlWith(r *http.Request, name, value string) string {
	u := *r.URL
	q := u.Query()
	if value == "" {
		q.Del(name)
	} else {
		q.Set(name, value)
	}
	u.RawQuery = q.Encode()
	return u.RequestURI()
}

// unfoldLevel is one entry of the unfold switcher: the level, and this page
// again at that level.
type unfoldLevel struct {
	Level  int
	URL    string
	Active bool
}

// unfoldLevels lists every level the server unfolds to as a link to the page
// being shown, so a result already on screen can be unfolded further or folded
// back without filling in the form again. def is the level the page takes
// without ?unfold=, left out of its link; a switch never carries a force=
// along, so a costly level is asked about again.
func unfoldLevels(r *http.Request, current, def int) []unfoldLevel {
	out := make([]unfoldLevel, 0, maxUnfoldDepth+1)
	for n := 0; n <= maxUnfoldDepth; n++ {
		q := r.URL.Query()
		q.Del("force")
		// the page with JavaScript asks for its levels lazily on its own; a
		// link has to work without it
		q.Del("lazy")
		if n == def {
			q.Del("unfold")
		} else {
			q.Set("unfold", fmt.Sprint(n))
		}
		u := r.URL.Path
		if enc := q.Encode(); enc != "" {
			u += "?" + enc
		}
		out = append(out, unfoldLevel{Level: n, URL: u, Active: n == min(current, maxUnfoldDepth)})
	}
	return out
}

// unfoldDocument expands the citations of a document page when a level is
// asked for, and returns its body sanitized for the page.
func (s *Server) unfoldDocument(r *http.Request, lng model.Language, art model.Article, depth int) (template.HTML, error) {
	if depth > 0 {
		body, err := s.svc.UnfoldArticle(r.Context(), lng, art, unfoldConfig(depth, forceParam(r)), text(lng))
		if err != nil {
			return "", err
		}
		art.HTML = body
	}
	out := s.sanitized(art.HTML, s.svc.ArticleBase(art))
	if depth > 0 {
		out = template.HTML(foldSections(string(out))) //nolint:gosec // sanitized above
	}
	return out, nil
}

// unfoldRequest reads the level a page is asked to unfold to. With ?lazy=1 the
// page is rendered without it and the browser loads the expansion afterwards,
// piece by piece, once the document itself is on screen: depth is then zero and
// auto the level asked for.
func unfoldRequest(r *http.Request, def int) (depth, auto int, err error) {
	depth, err = intParam(r, "unfold", def)
	if err != nil {
		return 0, 0, err
	}
	if boolParam(r, "lazy") {
		return 0, min(max(depth, 0), maxUnfoldDepth), nil
	}
	return depth, 0, nil
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
		if r.Kind == "category" {
			v.Context = "" // the category key: an internal name
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
	s.render(w, http.StatusOK, "index", struct{ basePage }{s.base(r, "JW")})
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
		Excerpts: boolParamOr(r, "excerpts", true),
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
	// AutoUnfold is the level the browser unfolds the page to once it is
	// shown, zero for none.
	AutoUnfold int
	// UnfoldLevels is the level switcher above the document.
	UnfoldLevels []unfoldLevel
	Body         template.HTML
	Refs         []model.ScriptureAnchor
	Images       []model.MediaAsset
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
	depth, auto, err := unfoldRequest(r, 0)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	art, err := s.svc.Article(r.Context(), lng, page.Target)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	page.Body, err = s.unfoldDocument(r, lng, art, depth)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	page.Unfold, page.AutoUnfold = depth, auto
	page.UnfoldLevels = unfoldLevels(r, max(depth, auto), 0)
	page.Title = firstNonEmpty(art.Title, "Article")
	page.Heading = art.Title
	page.URL = art.URL
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
	Unfold  int
	// AutoUnfold is the level the browser unfolds the page to once it is
	// shown, zero for none.
	AutoUnfold int
	// UnfoldLevels is the level switcher above the document.
	UnfoldLevels []unfoldLevel
	Body         template.HTML
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
	if level := max(p.Unfold, p.AutoUnfold); level > 0 {
		q.Set("unfold", fmt.Sprint(level))
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
	depth, auto, err := unfoldRequest(r, 0)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	art, err := s.svc.DailyText(r.Context(), lng, date)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	body, err := s.unfoldDocument(r, lng, art, depth)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	s.render(w, http.StatusOK, "dailytext", documentPage{
		basePage:     s.base(r, firstNonEmpty(art.Title, "Daily text")),
		Heading:      art.Title,
		URL:          art.URL,
		Date:         r.FormValue("date"),
		Unfold:       depth,
		AutoUnfold:   auto,
		UnfoldLevels: unfoldLevels(r, max(depth, auto), 0),
		Body:         body,
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
	depth, auto, err := unfoldRequest(r, 0)
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
	body, err := s.unfoldDocument(r, lng, art, depth)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	s.render(w, http.StatusOK, "meetings", documentPage{
		basePage:     s.base(r, firstNonEmpty(art.Title, "Meetings")),
		Heading:      art.Title,
		URL:          art.URL,
		Date:         r.FormValue("date"),
		Part:         part,
		Unfold:       depth,
		AutoUnfold:   auto,
		UnfoldLevels: unfoldLevels(r, max(depth, auto), 0),
		Body:         body,
	})
}

type mediaPage struct {
	basePage
	Crumbs  []crumb // the categories above this one, top first
	Heading string
	Items   []resultView
	// the start page leads with a featured video and rows of videos to
	// browse, before the categories
	Sections []mediaSection
}

// crumb is one step of a breadcrumb trail.
type crumb struct {
	Label, Href string
}

// maxCrumbs bounds the walk up the category tree, which is a few levels deep.
const maxCrumbs = 6

// categoryCrumbs is the trail from the media start page down to a category's
// parent. The mediator names only the direct parent, so each level above
// takes a small lookup; one that fails cuts the trail short there.
func (s *Server) categoryCrumbs(ctx context.Context, lng model.Language, cat model.Category, page basePage) []crumb {
	var trail []crumb
	for p := cat.Parent; p != nil && len(trail) < maxCrumbs; {
		trail = append(trail, crumb{Label: p.Name, Href: page.WithLang("/media/category/" + url.PathEscape(p.Key))})
		up, err := s.svc.Mediator.CategoryInfo(ctx, lng.Symbol, p.Key)
		if err != nil {
			break
		}
		p = up.Parent
	}
	trail = append(trail, crumb{Label: text(lng).UINavMedia, Href: page.WithLang("/media")})
	slices.Reverse(trail)
	return trail
}

// mediaSection is one block of the media start page: a featured video shown
// large, carousel rows, or both under one heading.
type mediaSection struct {
	Heading string
	Href    string // the section's own category page, when it has one
	More    bool   // the row shows only the first videos of that category
	Hero    *resultView
	Rows    [][]resultView
}

// The categories the media start page shows as a featured video and as
// carousels rather than as links in the category list. The mediator hands out
// FeaturedLibraryVideos one item at a time, whatever the limit: it is the
// single big video, as on jw.org.
const (
	mediaHeroCategory = "FeaturedLibraryVideos"
	mediaShelfLimit   = 24
)

var mediaShelfCategories = []string{"FeaturedLibraryLanding", "LatestVideos"}

// featuredCategory reports whether a root category is one of the curated
// rows (and their variants) rather than a library to browse.
func featuredCategory(key string) bool {
	return strings.HasPrefix(key, "Featured") || key == "LatestVideos"
}

func (s *Server) uiMedia(w http.ResponseWriter, r *http.Request) {
	lng, err := s.language(r)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	// the featured rows come along with the category tree; one that fails
	// leaves its row out rather than the whole page
	keys := append([]string{mediaHeroCategory}, mediaShelfCategories...)
	featured := make([]model.Category, len(keys))
	var wg sync.WaitGroup
	for i, key := range keys {
		limit := mediaShelfLimit
		if key == mediaHeroCategory {
			limit = 1
		}
		wg.Go(func() {
			if cat, err := s.svc.Mediator.Category(r.Context(), lng.Symbol, key, limit, 0); err == nil {
				featured[i] = cat
			}
		})
	}
	cats, err := s.svc.Mediator.RootCategories(r.Context(), lng.Symbol)
	wg.Wait()
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	page := mediaPage{basePage: s.base(r, text(lng).UINavMedia), Heading: text(lng).MediaCategories}
	if hero := featured[0]; len(hero.Media) > 0 {
		v := s.resultViews(service.MediaToResults(hero.Media[:1]), page.Lang)[0]
		page.Sections = append(page.Sections, mediaSection{Heading: hero.Name, Hero: &v})
	}
	for _, cat := range featured[1:] {
		if len(cat.Media) == 0 {
			continue
		}
		row := s.resultViews(service.MediaToResults(cat.Media), page.Lang)
		// a row named like the section before it carries that section on
		if n := len(page.Sections); n > 0 && page.Sections[n-1].Heading == cat.Name {
			page.Sections[n-1].Rows = append(page.Sections[n-1].Rows, row)
			continue
		}
		page.Sections = append(page.Sections, mediaSection{Heading: cat.Name, Rows: [][]resultView{row}})
	}
	cats = slices.DeleteFunc(cats, func(c model.Category) bool { return featuredCategory(c.Key) })
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
		Heading:  cat.Name,
	}
	page.Crumbs = s.categoryCrumbs(r.Context(), lng, cat, page.basePage)
	// a subcategory that holds videos itself — the lowest level of the tree —
	// comes with them, and shows as a carousel rather than as a link
	var subs []model.Category
	for _, sub := range cat.Subcategories {
		if len(sub.Media) == 0 {
			subs = append(subs, sub)
			continue
		}
		media := sub.Media[:min(len(sub.Media), mediaShelfLimit)]
		page.Sections = append(page.Sections, mediaSection{
			Heading: sub.Name,
			Href:    page.WithLang("/media/category/" + url.PathEscape(sub.Key)),
			More:    len(sub.Media) > len(media),
			Rows:    [][]resultView{s.resultViews(service.MediaToResults(media), page.Lang)},
		})
	}
	items := append(service.CategoriesToResults(subs), service.MediaToResults(cat.Media)...)
	page.Items = s.resultViews(items, page.Lang)
	s.render(w, http.StatusOK, "media_category", page)
}

type mediaItemPage struct {
	basePage
	Item  model.MediaItem
	Image string
	Files []mediaFileView
	// Stream is the rendition the page plays: up to 720p, which is plenty
	// for a phone and spares its data plan
	Stream *model.MediaFile
}

const streamQuality = "720p"

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
	if f, err := download.PickVideo(item.Files, streamQuality); err == nil {
		page.Stream = &f
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
	Edition string
	Unfold  int
	// AutoUnfold is the level the browser unfolds the reading to once it is
	// shown, zero for none.
	AutoUnfold int
	// UnfoldLevels is the level switcher above the reading.
	UnfoldLevels []unfoldLevel
	// Editions is the picker's option list.
	Editions []editionOption
	Body     template.HTML
	// Nav is the book grid shown before anything is read, BookNav the chapter
	// grid of the book picked from it (?book=); BookName is that book as a
	// reference names it, so a chapter link reads like one typed.
	Nav      *wol.BibleNav
	BookNav  *wol.BookNav
	BookName string
}

// BookLink leads from the book grid to the chapter grid of one book.
func (p biblePage) BookLink(book int) string {
	return p.WithLang(fmt.Sprintf("/bible?bible=%s&book=%d", url.QueryEscape(p.Edition), book))
}

// ChapterLink reads one chapter of the book whose chapter grid is shown.
func (p biblePage) ChapterLink(chapter int) string {
	q := url.Values{"bible": {p.Edition}, "ref": {fmt.Sprintf("%s %d", p.BookName, chapter)}}
	return p.WithLang("/bible?" + q.Encode())
}

// BooksLink leads from a chapter grid back to the book grid.
func (p biblePage) BooksLink() string {
	return p.WithLang("/bible?bible=" + url.QueryEscape(p.Edition))
}

// editionOption is one entry of the edition picker: the symbol the form sends,
// and the name the reader knows the translation by.
type editionOption struct {
	Symbol string
	Label  string
}

// editionOptions lists the bibles the library carries in the page's language,
// each by its own title with the symbol after it — "Neue-Welt-Übersetzung der
// Heiligen Schrift (Studienausgabe) (nwtsty)". When the list cannot be read
// the well-known symbols stand in. The edition asked for is always among them,
// so the picker never shows another one than the page read.
func (s *Server) editionOptions(r *http.Request, current string) []editionOption {
	var out []editionOption
	if lng, err := s.language(r); err == nil {
		if eds, err := s.svc.ReadEditions(r.Context(), lng, "", true); err == nil {
			for _, e := range eds {
				label := e.Symbol
				if e.Title != "" {
					label = e.Title + " (" + e.Symbol + ")"
				}
				out = append(out, editionOption{Symbol: e.Symbol, Label: label})
			}
		}
	}
	if len(out) == 0 {
		for _, sym := range wol.BibleEditions {
			out = append(out, editionOption{Symbol: sym, Label: sym})
		}
	}
	if !slices.ContainsFunc(out, func(o editionOption) bool { return o.Symbol == current }) {
		out = append(out, editionOption{Symbol: current, Label: current})
	}
	return out
}

// uiBible is the bible reader: the verses first, what they reference unfolded
// verse by verse or all at once. The study material of a verse — its notes,
// cross references, research guide, quotations and media — is what an unfold
// brings, so the reader is the only view; the JSON API keeps the others.
func (s *Server) uiBible(w http.ResponseWriter, r *http.Request) {
	page := biblePage{
		basePage: s.base(r, "Bible"),
		Ref:      strings.TrimSpace(r.FormValue("ref")),
		Edition:  valueOr(r, "bible", "nwtsty"),
	}
	page.Editions = s.editionOptions(r, page.Edition)
	if page.Ref == "" {
		s.bibleNav(r, &page)
		s.render(w, http.StatusOK, "bible", page)
		return
	}
	lng, err := s.language(r)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	depth, auto, err := unfoldRequest(r, 0)
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	page.Unfold, page.AutoUnfold = depth, auto
	res, err := s.svc.ReadPassages(r.Context(), lng, service.ReadRequest{
		Refs: page.Ref, Edition: page.Edition, AllBibles: boolParam(r, "all"),
		Unfold: unfoldConfig(depth, forceParam(r)),
	}, text(lng))
	if err != nil {
		s.failUI(w, r, err)
		return
	}
	page.UnfoldLevels = unfoldLevels(r, max(depth, auto), 0)
	page.Body = s.passagesHTML(res, page.Edition, depth)
	s.render(w, http.StatusOK, "bible", page)
}

// bibleNav fills in the grid the page opens with: the books of the edition, or
// with ?book= the chapters of one of them, as the library's own bible
// navigation lays them out. The grid is a way in, not the page: when it cannot
// be read the reference field still works, so a failure is only a banner.
func (s *Server) bibleNav(r *http.Request, page *biblePage) {
	lng, err := s.language(r)
	if err != nil {
		page.Error = err.Error()
		return
	}
	if book, _ := intParam(r, "book", 0); book != 0 {
		nav, err := s.svc.BookNav(r.Context(), lng, page.Edition, book)
		if err == nil {
			page.BookNav, page.BookName = &nav, s.svc.BookTable(r.Context(), lng).Name(book)
			return
		}
		page.Error = err.Error()
	}
	nav, err := s.svc.BibleNav(r.Context(), lng, page.Edition)
	if err != nil {
		if page.Error == "" {
			page.Error = err.Error()
		}
		return
	}
	page.Nav = &nav
}

// passagesHTML lays a reading out for the page: every passage under its
// heading, and every verse an item of its own, which is what the page unfolds
// one at a time. A verse the server already unfolded carries its expansion
// folded under it, and says to what level.
func (s *Server) passagesHTML(res service.ReadResult, edition string, depth int) template.HTML {
	wolBase := s.svc.HTTP.Base.WOL
	var b strings.Builder
	for _, p := range res.Passages {
		fmt.Fprintf(&b, `<section class="passage" data-bible="%s"><h2>%s</h2><div class="items">`,
			template.HTMLEscapeString(firstNonEmpty(p.Bible, edition)), template.HTMLEscapeString(p.Heading()))
		for _, v := range p.Verses {
			level := ""
			if depth > 0 {
				level = fmt.Sprintf(` data-level="%d"`, min(depth, maxUnfoldDepth))
			}
			fmt.Fprintf(&b, `<div class="item verse" data-vid="%d"%s><div class="item-text">%s</div>`,
				v.ID, level, s.sanitized(v.HTML, wolBase))
			if strings.TrimSpace(v.Unfold) != "" {
				exp := strings.TrimSpace(string(s.sanitized(v.Unfold, wolBase)))
				exp = strings.TrimSuffix(strings.TrimSuffix(exp, "<hr/>"), "<hr>")
				b.WriteString(foldSections(`<div class="expansion">` + exp + `</div>`))
			}
			b.WriteString(`</div>`)
		}
		b.WriteString(`</div>`)
		if p.UnfoldNote != "" {
			fmt.Fprintf(&b, `<p class="note">%s</p>`, template.HTMLEscapeString(p.UnfoldNote))
		}
		b.WriteString(`</section>`)
	}
	for _, note := range res.Missing {
		fmt.Fprintf(&b, `<p class="note">%s</p>`, template.HTMLEscapeString(note))
	}
	return template.HTML(b.String()) //nolint:gosec // every fragment sanitized above, the rest escaped
}
