package service

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/dgrieser/jw-cli/internal/api/search"
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
//
// With p.Videos, the videos quoting the verses follow the publications: wol
// has none, so jw.org is asked for them alongside the walk. They carry no
// date to sort them in by, and a talk quoting a verse is a different kind of
// answer than an article, so they close the listing rather than mix into it.
func (s *Service) CitedListing(ctx context.Context, lng model.Language, p *SearchParams,
	progress func(done, total int)) (SearchOutcome, error) {
	out := SearchOutcome{Kind: "wol-search", Query: p.Query, Lang: lng.Symbol, Page: 1}
	var videos chan []model.Result
	if p.Videos {
		videos = make(chan []model.Result, 1)
		go func() { videos <- s.citedVideos(ctx, lng, citationTerms(p.Query), p.Excerpts) }()
	}
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
		// a citation search matches wherever the verse is named; what only
		// names it is not an answer to "who quotes this?". Judged on the
		// passages just read: wol's own teaser is cut short, so without them
		// there is nothing fair to judge, and --no-excerpts shows everything.
		// The total follows the listing, so its head never counts rows nobody
		// is shown.
		out.Items = keepTelling(out.Items)
		out.Total = len(out.Items)
	}
	// the videos were judged as they were found
	if videos != nil {
		found := <-videos
		out.Items = append(out.Items, found...)
		out.Total += len(found)
	}
	return out, nil
}

// videoPageSize is how many videos one jw.org search page is asked for: the
// most the API serves, as it answers a larger limit with HTTP 400.
const videoPageSize = 30

// maxVideoPages bounds the walk through the video results of one reference.
// Even the most quoted verses are a page or two of videos.
const maxVideoPages = 10

// refInTitle is a verse named in a video's title — "(Jak. 5:19, 20)", the
// way a talk or a demonstration announces its text.
var refInTitle = regexp.MustCompile(`\d+:\d+`)

// citedVideos asks jw.org for the videos quoting any of terms: one search per
// reference, as the API reads a citation the way wol does but has no OR. Best
// effort: the publications are the answer, the videos an addition to it, so a
// search that fails leaves out what it would have found.
//
// The snippet is the passage of the transcript the verse is quoted in; with
// excerpts on it stands as the excerpt, the way a publication's passage does,
// and a video that shows no such passage and names no verse in its title — a
// song matched by its theme text — is left out, as keepTelling leaves out the
// publications that only name a verse.
func (s *Service) citedVideos(ctx context.Context, lng model.Language, terms []string, excerpts bool) []model.Result {
	if s.Search == nil {
		return nil
	}
	var out []model.Result
	seen := map[string]bool{}
	for _, term := range terms {
		for page := 0; page < maxVideoPages; page++ {
			sp, err := s.Search.Search(ctx, lng.Symbol, search.Params{
				Query: "(" + term + ")", Facet: "videos",
				Offset: page * videoPageSize, Limit: videoPageSize,
			})
			if err != nil {
				break
			}
			for _, r := range sp.Results {
				key := firstNonEmptyString(r.LANK, r.JWLink, r.Title)
				if r.Kind != "video" || seen[key] {
					continue
				}
				seen[key] = true
				if excerpts && strings.TrimSpace(r.Snippet) == "" && !refInTitle.MatchString(r.Title) {
					continue
				}
				out = append(out, videoResult(r, lng, excerpts))
			}
			if len(sp.Results) < videoPageSize {
				break
			}
		}
	}
	return out
}

// videoResult prepares a video hit for a citation listing: a link that names
// the video itself, and the transcript passage as its excerpt.
func videoResult(r model.Result, lng model.Language, excerpts bool) model.Result {
	if r.LANK != "" {
		// the search's own link opens some videos only by the document they
		// belong to; the finder link names the video, in the reader's language
		r.JWLink = VideoLink(r.LANK, lng.Symbol)
	}
	// the snippet is markup already — the search marks what it matched — and
	// one passage, whose line breaks are the transcript's captions
	if excerpts && strings.TrimSpace(r.Snippet) != "" {
		r.Excerpt = "<p>" + strings.Join(strings.Fields(r.Snippet), " ") + "</p>"
	}
	return r
}

// VideoLink is the jw.org page of the video lank names, in language symbol.
func VideoLink(lank, symbol string) string {
	return "https://www.jw.org/finder?" + url.Values{"lank": {lank}, "wtlocale": {symbol}}.Encode()
}

// citationTerms takes a citation query apart into the references it is made
// of: "(Jeremia 31:15) | (Matthäus 2:18)" is Jeremia 31:15 and Matthäus 2:18.
func citationTerms(query string) []string {
	var terms []string
	for _, t := range strings.Split(query, "|") {
		t = strings.TrimSpace(t)
		t = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(t, "("), ")"))
		if t != "" {
			terms = append(terms, t)
		}
	}
	return terms
}

func firstNonEmptyString(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
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
	return s.CitationQueryFor(ctx, lng, refs, table)
}

// CitationQueryFor is CitationQuery for references that are already parsed:
// what the unfolder has when it looks a citation up as written, or a bible
// reading up one verse at a time.
func (s *Service) CitationQueryFor(ctx context.Context, lng model.Language,
	refs []bibleref.Ref, table *bibleref.Table) (query, label string, err error) {
	terms := make([]string, 0, len(refs))
	for _, ref := range refs {
		if ref, err = s.closeOpenEnd(ctx, lng, ref); err != nil {
			return "", "", err
		}
		terms = append(terms, RefString(ref, table))
	}
	if len(terms) == 0 {
		return "", "", nil
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
