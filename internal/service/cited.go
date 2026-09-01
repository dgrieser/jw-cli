package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/dgrieser/jw-cli/internal/bibleref"
	"github.com/dgrieser/jw-cli/internal/model"
)

// maxCitedPages bounds the page walk. wol serves 40 documents a page, so this
// is 4000 of them — far past any single verse, and a stop that cannot spin
// should the site ever keep answering full pages.
const maxCitedPages = 100

// CitedListing reads every page of the citation search into one listing: the
// publications quoting a verse are a finite set worth seeing whole, not
// something to page through by hand. Excerpts, when asked for, are filled in
// after the walk so one progress counter covers the whole listing.
func (s *Service) CitedListing(ctx context.Context, lng model.Language, p *SearchParams,
	progress func(done, total int)) (SearchOutcome, error) {
	out := SearchOutcome{Kind: "wol-search", Query: p.Query, Lang: lng.Symbol, Page: 1}
	seen := map[string]bool{}
	for page := 1; page <= maxCitedPages; page++ {
		sp, err := s.searchWOL(ctx, lng, p, page)
		if err != nil {
			return SearchOutcome{}, err
		}
		if page == 1 {
			out.Total = sp.Total
		}
		added := 0
		for _, r := range sp.Results {
			if seen[r.WOLLink] {
				continue
			}
			seen[r.WOLLink] = true
			out.Items = append(out.Items, r)
			added++
		}
		// the last page is the one the site does not fill
		if added == 0 || len(sp.Results) < pageSize(sp.Limit) {
			break
		}
	}
	if p.Excerpts {
		s.FillExcerpts(ctx, out.Items, progress)
	}
	return out, nil
}

// pageSize is how many documents a full search page holds. The site states it;
// 40 is what it has always answered when it does not.
func pageSize(reported int) int {
	if reported > 0 {
		return reported
	}
	return 40
}

// CitationQuery turns a reference string into wol's scripture-citation search
// syntax — "(Jeremia 31:15) | (Matthäus 2:18)" — and the label to print above
// the results. wol only understands the book names of its own language, which
// is what the merged book table renders.
func (s *Service) CitationQuery(ctx context.Context, lng model.Language, input string) (query, label string, err error) {
	refs, table, err := s.ParseRefs(ctx, lng, input)
	if err != nil {
		return "", "", err
	}
	terms := make([]string, 0, len(refs))
	for _, ref := range refs {
		if ref, err = s.closeOpenEnd(ctx, lng, ref); err != nil {
			return "", "", err
		}
		terms = append(terms, RefString(ref, table))
	}
	label = strings.Join(terms, "; ")
	return "(" + strings.Join(terms, ") | (") + ")", label, nil
}

// closeOpenEnd names the last verse of a reference written as running past its
// chapter — the first half of a span like "Pr 8:5-9:10". wol rejects a verse
// number the chapter does not have, so the chapter is read to find its end.
func (s *Service) closeOpenEnd(ctx context.Context, lng model.Language, ref bibleref.Ref) (bibleref.Ref, error) {
	if !ref.RunsToChapterEnd() {
		return ref, nil
	}
	doc, err := s.Chapter(ctx, lng, studyEdition, ref)
	if err != nil {
		return ref, err
	}
	verses, err := doc.Verses(ref.VerseStart, ref.VerseEnd)
	if err != nil {
		return ref, fmt.Errorf("%s: %w", ref.String(), err)
	}
	return ResolveChapterEnd(ref, verses), nil
}
