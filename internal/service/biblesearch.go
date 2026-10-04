package service

import (
	"context"
	"strings"

	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/bibleref"
	"github.com/dgrieser/jw-cli/internal/model"
)

// MaxBibleSearchPages bounds how many pages of results a bible search reads:
// every result is wanted, but a word every book uses would read the whole
// library page by page.
const MaxBibleSearchPages = 25

// BibleHit is one document of the bible a text search found: a book of an
// edition, or the study notes of a chapter, with the passages that matched.
type BibleHit struct {
	model.Result
	// Edition is the bible the hit belongs to, by its symbol.
	Edition string
	// Passages are the verses of each chapter the hit's passages hold, in the
	// order they read; none for a hit that is not bible text (study notes).
	Passages []bibleref.Ref
}

// BibleSearch is what a text search of the bible found.
type BibleSearch struct {
	Query string
	// Editions are the bibles searched: the one being read, then the others
	// of the language the way its translations section lists them.
	Editions []wol.BibleEdition
	Hits     []BibleHit
	// Total is what the search reported over every bible of the language,
	// before the hits of bibles not searched were left out.
	Total int
	// Truncated says there were more pages than were read.
	Truncated bool
}

// SearchBible searches the text of the bible being read and of the other
// bibles of the language — the editions a verse's translations section shows —
// for query, with wol's own syntax. Every page of results is read, up to
// MaxBibleSearchPages.
func (s *Service) SearchBible(ctx context.Context, lng model.Language, query, edition string) (BibleSearch, error) {
	if edition == "" {
		edition = studyEdition
	}
	out := BibleSearch{Query: query}
	all, err := s.ReadEditions(ctx, lng, "", true)
	if err == nil {
		for _, e := range all {
			if e.Symbol == edition {
				out.Editions = append(out.Editions, e)
			}
		}
		out.Editions = append(out.Editions, OtherEditions(all, edition)...)
	}
	if len(out.Editions) == 0 {
		out.Editions = []wol.BibleEdition{{Symbol: edition}}
	}
	searched := map[string]bool{}
	for _, e := range out.Editions {
		searched[strings.ToLower(e.Symbol)] = true
	}
	p := &SearchParams{Engine: "wol", Query: query, Categories: WOLCategories{List: []string{wol.CategoryBibles}}}
	for page := 1; ; page++ {
		if page > MaxBibleSearchPages {
			out.Truncated = true
			break
		}
		sp, err := s.searchWOL(ctx, lng, p, page)
		if err != nil {
			if page == 1 {
				return out, err
			}
			break
		}
		if page == 1 {
			out.Total = sp.Total
		}
		for _, r := range sp.Results {
			sym := editionOfResult(r)
			if !searched[strings.ToLower(sym)] {
				continue
			}
			out.Hits = append(out.Hits, BibleHit{Result: r, Edition: sym, Passages: passageRefs(r.Snippet)})
		}
		size := max(sp.Limit, len(sp.Results))
		if len(sp.Results) == 0 || size == 0 || page*size >= sp.Total {
			break
		}
	}
	return out, ctx.Err()
}

// editionOfResult is the bible a search result belongs to: its publication
// line opens with the edition's symbol ("nwtsty Markus 1:1-16:8 - Studienbibel",
// "Rbi8 Richter 1:1-21:25 - …").
func editionOfResult(r model.Result) string {
	f := strings.Fields(r.Context)
	if len(f) == 0 {
		return ""
	}
	return f[0]
}
