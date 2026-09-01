package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/bibleref"
)

func newBibleCitedCmd(a *app.App) *cobra.Command {
	var (
		sortBy      string
		page        int
		scope       string
		interactive bool
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
semicolons, are searched together as one OR query.

Examples:
  jw bible cited "Jer 31:15"
  jw bible cited "Mt 24:14" --include w,g      only Watchtower and Awake!
  jw bible cited "Ps 83" --all                 bibles and indexes included
  jw bible cited "Jer 31:15; Mt 2:18"          either of the two
  jw bible cited "Mt 24:14" -s oldest -p 2`,
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
			if page < 1 {
				page = 1
			}
			p := searchParams{Engine: "wol", Query: query, Sort: sortBy, Scope: scope}
			if p.Categories, err = cats.resolve(cmd, wolKnownCategories(ctx, a)); err != nil {
				return err
			}
			p.Header = func(total, pg int) string {
				return a.Text().CitedResults(total, label, pg)
			}
			if interactive {
				return runSearchTUI(ctx, a, lng, searchFetcher(ctx, a, lng, p), label)
			}
			rs, header, err := runSearch(ctx, a, lng, p, page)
			if err != nil {
				return err
			}
			return writeListing(a, rs, header)
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&sortBy, "sort", "s", "newest", "sort order: newest, oldest, occ (occurrences)")
	fl.IntVarP(&page, "page", "p", 1, "page number")
	fl.StringVar(&scope, "scope", "par", "match unit: par (paragraph) or sen (sentence)")
	fl.BoolVarP(&interactive, "interactive", "i", false, "browse results interactively (TUI)")
	cats.bind(cmd)
	return cmd
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
