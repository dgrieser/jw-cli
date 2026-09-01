package cli

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/results"
	"github.com/dgrieser/jw-cli/internal/service"
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
			svc := a.Service()
			query, label, err := svc.CitationQuery(ctx, lng, strings.Join(args, " "))
			if err != nil {
				return err
			}
			p := service.SearchParams{Engine: "wol", Query: query, Sort: sortBy, Scope: scope, Excerpts: !noExcerpts}
			if p.Categories, err = cats.resolve(cmd, svc.KnownCategories(ctx, lng)); err != nil {
				return err
			}
			fetch := func(page int) (results.ResultSet, string, error) {
				if page > 1 {
					return results.ResultSet{}, "", errors.New(a.Text().NoMoreResults)
				}
				// excerpts fill in after every page, so one counter covers the
				// whole listing
				out, err := svc.CitedListing(ctx, lng, &p, excerptProgress(a))
				if err != nil {
					return results.ResultSet{}, "", err
				}
				rs := results.ResultSet{Kind: out.Kind, Query: out.Query, Lang: out.Lang, Page: out.Page, Items: out.Items}
				return rs, a.Text().CitedResults(out.Total, label), nil
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
