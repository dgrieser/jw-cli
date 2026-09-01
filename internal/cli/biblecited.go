package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/bibleref"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/results"
)

func newBibleCitedCmd(a *app.App) *cobra.Command {
	var (
		sortBy      string
		scope       string
		interactive bool
		noExcerpts  bool
		cats        categoryFilter
	)
	cats.defaultExclude = []string{wol.CategoryBibles, wol.CategoryIndex}
	cmd := &cobra.Command{
		Use:     "cited <reference...>",
		Aliases: []string{"citations", "citedby"},
		Short:   "Find publications that cite a verse (reverse lookup)",
		Long: `Search the whole library for paragraphs citing a bible reference:
which articles, books, workbooks and magazines discuss it, newest first. This is
the reverse of jw bible read — and wider than jw bible research, which lists only
the curated Research Guide entries.

Bibles and the indexes (concordance, bible-word index) are left out by default,
since they cite every verse by construction. Several references, separated by
semicolons, are searched together as one OR query. Every result page is read,
so the listing is the complete answer rather than its first 40 rows.

Examples:
  jw bible cited "Jer 31:15"
  jw bible cited "Mt 24:14" --include w,g      only Watchtower and Awake!
  jw bible cited "Ps 83" --all                 bibles and indexes included
  jw bible cited "Jer 31:15; Mt 2:18"          either of the two
  jw bible cited "Mt 24:14" -s oldest`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			lng, err := a.Lang(ctx)
			if err != nil {
				return err
			}
			query, label, err := citationQuery(ctx, a, args)
			if err != nil {
				return err
			}
			p := searchParams{Engine: "wol", Query: query, Sort: sortBy, Scope: scope, Excerpts: !noExcerpts}
			if p.Categories, err = cats.resolve(cmd, wolKnownCategories(ctx, a)); err != nil {
				return err
			}
			fetch := func(page int) (results.ResultSet, string, error) {
				if page > 1 {
					return results.ResultSet{}, "", errors.New(a.Text().NoMoreResults)
				}
				rs, total, err := citedListing(ctx, a, lng, &p)
				if err != nil {
					return results.ResultSet{}, "", err
				}
				// after every page, so one counter covers the whole listing
				if p.Excerpts {
					fillExcerpts(ctx, a, rs.Items)
				}
				return rs, a.Text().CitedResults(total, label), nil
			}
			if interactive {
				return runSearchTUI(ctx, a, lng, func(page int) (results.ResultSet, string, error) {
					rs, header, err := fetch(page)
					if err != nil {
						return rs, header, err
					}
					_ = results.Save(a.Cache().Dir(), rs)
					return rs, header, nil
				}, label)
			}
			rs, header, err := fetch(1)
			if err != nil {
				return err
			}
			return writeListing(a, rs, header)
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&sortBy, "sort", "s", "newest", "sort order: newest, oldest, occ (occurrences)")
	fl.StringVar(&scope, "scope", "par", "match unit: par (paragraph) or sen (sentence)")
	fl.BoolVarP(&interactive, "interactive", "i", false, "browse results interactively (TUI)")
	fl.BoolVar(&noExcerpts, "no-excerpts", false, "keep wol's short teasers instead of reading each document")
	cats.bind(cmd)
	return cmd
}

// maxCitedPages bounds the page walk. wol serves 40 documents a page, so this
// is 4000 of them — far past any single verse, and a stop that cannot spin
// should the site ever keep answering full pages.
const maxCitedPages = 100

// citedListing reads every page of the citation search into one listing: the
// publications quoting a verse are a finite set worth seeing whole, not
// something to page through by hand. It returns the listing and the total the
// site reports.
func citedListing(ctx context.Context, a *app.App, lng model.Language, p *searchParams) (results.ResultSet, int, error) {
	rs := results.ResultSet{Kind: "wol-search", Query: p.Query, Lang: lng.Symbol, Page: 1}
	seen := map[string]bool{}
	total := 0
	for page := 1; page <= maxCitedPages; page++ {
		sp, err := searchWOL(ctx, a, lng, p, page)
		if err != nil {
			return results.ResultSet{}, 0, err
		}
		if page == 1 {
			total = sp.Total
		}
		added := 0
		for _, r := range sp.Results {
			if seen[r.WOLLink] {
				continue
			}
			seen[r.WOLLink] = true
			rs.Items = append(rs.Items, r)
			added++
		}
		// the last page is the one the site does not fill
		if added == 0 || len(sp.Results) < pageSize(sp.Limit) {
			break
		}
	}
	return rs, total, nil
}

// pageSize is how many documents a full search page holds. The site states it;
// 40 is what it has always answered when it does not.
func pageSize(reported int) int {
	if reported > 0 {
		return reported
	}
	return 40
}

// citationQuery turns the reference arguments into wol's scripture-citation
// search syntax — "(Jeremia 31:15) | (Matthäus 2:18)" — and the label to print
// above the results. wol only understands the book names of its own language,
// which is what the merged book table renders.
func citationQuery(ctx context.Context, a *app.App, args []string) (query, label string, err error) {
	refs, table, err := parseRefsArg(ctx, a, args)
	if err != nil {
		return "", "", err
	}
	terms := make([]string, 0, len(refs))
	for _, ref := range refs {
		if ref, err = closeOpenEnd(ctx, a, ref); err != nil {
			return "", "", err
		}
		terms = append(terms, refString(ref, table))
	}
	label = strings.Join(terms, "; ")
	return "(" + strings.Join(terms, ") | (") + ")", label, nil
}

// closeOpenEnd names the last verse of a reference written as running past its
// chapter — the first half of a span like "Pr 8:5-9:10". wol rejects a verse
// number the chapter does not have, so the chapter is read to find its end.
func closeOpenEnd(ctx context.Context, a *app.App, ref bibleref.Ref) (bibleref.Ref, error) {
	if !ref.RunsToChapterEnd() {
		return ref, nil
	}
	doc, err := chapterFor(ctx, a, "nwtsty", ref)
	if err != nil {
		return ref, err
	}
	verses, err := doc.Verses(ref.VerseStart, ref.VerseEnd)
	if err != nil {
		return ref, fmt.Errorf("%s: %w", ref.String(), err)
	}
	return resolveChapterEnd(ref, verses), nil
}
