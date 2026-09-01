package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/dgrieser/jw-cli/internal/api/search"
	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/model"
)

// SearchParams is one search request, minus the page: what the user asked for,
// as both a one-shot listing and a paging fetcher need it.
type SearchParams struct {
	Engine string // jworg (default) or wol
	Query  string
	Facet  string // jworg content type
	Sort   string
	Scope  string // wol match unit: par or sen
	Limit  int    // jworg page size; wol pages are server-sized
	// Categories is the wol publication-category filter.
	Categories WOLCategories
	// Excerpts reads each result's document to replace its teaser with the
	// passage it was cut from. wol engine only.
	Excerpts bool
}

// SearchOutcome is one page of results together with what a caller needs to
// head or page it.
type SearchOutcome struct {
	Kind  string `json:"kind"` // search (jworg) or wol-search
	Query string `json:"query"`
	Lang  string `json:"lang,omitempty"`
	Page  int    `json:"page"`
	// Total is what the engine reports; zero when it reports none.
	Total int            `json:"total,omitempty"`
	Items []model.Result `json:"items"`
}

// RunSearch executes one page of a search on the chosen engine. Excerpts, when
// asked for, are filled in before returning; progress is a nilable callback.
func (s *Service) RunSearch(ctx context.Context, lng model.Language, p *SearchParams, page int,
	progress func(done, total int)) (SearchOutcome, error) {
	switch p.Engine {
	case "jworg", "jw", "":
		sp, err := s.Search.Search(ctx, lng.Symbol, search.Params{
			Query: p.Query, Facet: p.Facet, Sort: p.Sort,
			Offset: (page - 1) * p.Limit, Limit: p.Limit,
		})
		if err != nil {
			return SearchOutcome{}, err
		}
		return SearchOutcome{Kind: "search", Query: p.Query, Lang: lng.Symbol,
			Page: page, Total: sp.Total, Items: sp.Results}, nil
	case "wol":
		sp, err := s.searchWOL(ctx, lng, p, page)
		if err != nil {
			return SearchOutcome{}, err
		}
		if p.Excerpts {
			s.FillExcerpts(ctx, sp.Results, progress)
		}
		return SearchOutcome{Kind: "wol-search", Query: p.Query, Lang: lng.Symbol,
			Page: sp.Page, Total: sp.Total, Items: sp.Results}, nil
	}
	return SearchOutcome{}, fmt.Errorf("invalid engine %q (want jworg or wol)", p.Engine)
}

// searchWOL runs the wol search engine for jw search -e wol and jw bible cited.
func (s *Service) searchWOL(ctx context.Context, lng model.Language, p *SearchParams, page int) (model.SearchPage, error) {
	cfg, err := s.WOL.ConfigFor(ctx, lng.Locale)
	if err != nil {
		return model.SearchPage{}, err
	}
	sortBy := p.Sort
	if sortBy == "rel" {
		sortBy = "occ" // wol's default ranking
	}
	opts := wol.SearchOpts{Scope: p.Scope, Sort: sortBy, Page: page, Categories: p.Categories.List}
	sp, err := s.WOL.Search(ctx, cfg, p.Query, opts)
	if err != nil {
		return model.SearchPage{}, err
	}
	// the category list differs per language; if the page names one the sent
	// whitelist did not know, its documents were just dropped — ask again with
	// the corrected list. The list is cached, so this happens once per language.
	if fixed, ok := p.Categories.corrected(sp.Filters); ok {
		// the corrected list needs no second correction, so further pages of
		// the same search go out as one request each
		p.Categories = WOLCategories{List: fixed}
		opts.Categories = fixed
		if sp, err = s.WOL.Search(ctx, cfg, p.Query, opts); err != nil {
			return model.SearchPage{}, err
		}
	}
	return sp, nil
}

// KnownCategories is the fc[] category list of the language, as last seen on a
// search page. Best effort: an empty list falls back to the
// language-independent superset.
func (s *Service) KnownCategories(ctx context.Context, lng model.Language) []string {
	cfg, err := s.WOL.ConfigFor(ctx, lng.Locale)
	if err != nil {
		return nil
	}
	return s.WOL.Categories(cfg)
}

// WOLCategories is the resolved fc[] publication-category filter of one search.
type WOLCategories struct {
	// List is the whitelist to send; nil sends no filter at all.
	List []string
	// Exclude is what must stay out of the whitelist. Nil when the filter was
	// spelled out by the user (--all, --include), which switches the
	// correction pass off.
	Exclude []string
}

// corrected returns the whitelist to retry with when the search page offers a
// category this language has but the sent list did not name.
func (c WOLCategories) corrected(available []string) ([]string, bool) {
	if c.Exclude == nil || len(available) == 0 {
		return nil, false
	}
	var fixed []string
	missing := false
	for _, cat := range available {
		if slices.Contains(c.Exclude, cat) {
			continue
		}
		if !slices.Contains(c.List, cat) {
			missing = true
		}
		fixed = append(fixed, cat)
	}
	if !missing {
		return nil, false
	}
	return fixed, true
}

// ResolveCategories turns an all/include/exclude choice into the whitelist to
// send. known is the category list of the active language, as last seen on a
// search page; exclude is nil when the caller did not choose one, in which
// case defaultExclude applies. An empty outcome means "no filter at all",
// which is what the site itself does.
func ResolveCategories(all bool, include, exclude, defaultExclude, known []string) (WOLCategories, error) {
	if len(known) == 0 {
		known = wol.AllCategories
	}
	if err := validCategories(include, known); err != nil {
		return WOLCategories{}, err
	}
	if err := validCategories(exclude, known); err != nil {
		return WOLCategories{}, err
	}
	switch {
	case all:
		return WOLCategories{}, nil
	case len(include) > 0:
		return WOLCategories{List: include}, nil
	}
	if exclude == nil {
		exclude = defaultExclude
	}
	if len(exclude) == 0 {
		return WOLCategories{}, nil
	}
	var list []string
	for _, cat := range known {
		if !slices.Contains(exclude, cat) {
			list = append(list, cat)
		}
	}
	return WOLCategories{List: list, Exclude: exclude}, nil
}

func validCategories(cats, known []string) error {
	for _, cat := range cats {
		if !slices.Contains(known, cat) && !slices.Contains(wol.AllCategories, cat) {
			return fmt.Errorf("unknown publication category %q (known: %s)", cat, strings.Join(known, ", "))
		}
	}
	return nil
}

// excerptWorkers is how many documents are read at once. The per-host limiter
// in internal/httpx caps the real rate; this only keeps a listing of several
// dozen results from taking one round trip after another.
const excerptWorkers = 8

// FillExcerpts replaces each result's teaser with the passage it was cut from.
// Best effort: a result whose document cannot be read, or whose fragment cannot
// be placed in it, keeps the teaser. Only wol results carry a document to read.
// progress, when non-nil, reports each finished document.
func (s *Service) FillExcerpts(ctx context.Context, items []model.Result, progress func(done, total int)) {
	todo := make([]int, 0, len(items))
	for i, r := range items {
		if r.WOLLink != "" && r.Snippet != "" {
			todo = append(todo, i)
		}
	}
	if len(todo) == 0 {
		return
	}
	var (
		wg   sync.WaitGroup
		sem  = make(chan struct{}, excerptWorkers)
		mu   sync.Mutex
		done int
	)
	for _, i := range todo {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			blocks, err := s.WOL.Excerpts(ctx, items[i].WOLLink, items[i].Snippet)
			mu.Lock()
			defer mu.Unlock()
			if err == nil && len(blocks) > 0 {
				items[i].Excerpt = strings.Join(blocks, "\n")
			}
			done++
			if progress != nil {
				progress(done, len(todo))
			}
		}()
	}
	wg.Wait()
	if progress != nil {
		progress(len(todo), len(todo))
	}
}
