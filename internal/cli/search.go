package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dgrieser/jw-cli/internal/app"
)

func newSearchCmd(a *app.App) *cobra.Command {
	var (
		facet       string
		sortBy      string
		limit       int
		page        int
		engine      string
		scope       string
		interactive bool
		cats        categoryFilter
	)
	cmd := &cobra.Command{
		Use:   "search <query...>",
		Short: "Search jw.org and wol.jw.org",
		Long: `Search articles, publications, videos, audio, and bible content.

Two engines are available:
  jworg (default)  the jw.org unified search: all content types, sort options
  wol              the Watchtower Online Library search: supports special
                   syntax like scripture-citation search "(Matthew 24:14)"
                   (finds articles citing that verse), * wildcards, quoted
                   phrases, & (AND), | (OR)

Examples:
  jw search kingdom of god
  jw search -t videos -s newest creation
  jw search -e wol '(Matthew 24:14)'
  jw search -e wol 'faith & works' --scope sen
  jw search -e wol '(Matthew 24:14)' --exclude bi,dx
  jw search -l de Königreich

The wol engine covers every publication category by default. --exclude leaves
categories out, --include restricts to them; the codes are the ones wol's own
"refine search" sidebar uses (bi bibles, dx indexes, w Watchtower, g Awake!,
it Insight, bk books, mwb workbooks, es daily texts, ...).`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.Join(args, " ")
			ctx := cmd.Context()
			lng, err := a.Lang(ctx)
			if err != nil {
				return err
			}
			if page < 1 {
				page = 1
			}
			p := searchParams{Engine: engine, Query: query, Facet: facet, Sort: sortBy, Scope: scope, Limit: limit}
			var known []string
			if engine == "wol" {
				known = wolKnownCategories(ctx, a)
			}
			if p.Categories, err = cats.resolve(cmd, known); err != nil {
				return err
			}
			if interactive {
				fetch := searchFetcher(ctx, a, lng, p)
				return runSearchTUI(ctx, a, lng, fetch, fmt.Sprintf(a.Text().SearchHeader, query))
			}
			rs, header, err := runSearch(ctx, a, lng, p, page)
			if err != nil {
				return err
			}
			return writeListing(a, rs, header)
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&facet, "type", "t", "all", "content type: all, publications, videos, audio, bible, indexes (jworg engine)")
	fl.StringVarP(&sortBy, "sort", "s", "rel", "sort order: rel, newest, oldest (wol engine: occ, newest, oldest)")
	fl.IntVarP(&limit, "limit", "n", 10, "results per page")
	fl.IntVarP(&page, "page", "p", 1, "page number")
	fl.StringVarP(&engine, "engine", "e", "jworg", "search engine: jworg or wol")
	fl.StringVar(&scope, "scope", "par", "wol match unit: par (paragraph) or sen (sentence)")
	fl.BoolVarP(&interactive, "interactive", "i", false, "browse results interactively (TUI)")
	cats.bind(cmd)
	return cmd
}

func newOpenCmd(a *app.App) *cobra.Command {
	var browser bool
	cmd := &cobra.Command{
		Use:   "open <index>",
		Short: "Print (or open in a browser) the link of a result from the last listing",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			item, err := resolveIndexArg(a, args[0])
			if err != nil {
				return err
			}
			link := preferredLink(item)
			if link == "" {
				return fmt.Errorf("result %d (%s) has no link", item.Index, item.Title)
			}
			if browser {
				if err := openInBrowser(link); err != nil {
					return err
				}
			}
			return a.Write(link)
		},
	}
	cmd.Flags().BoolVarP(&browser, "browser", "b", false, "open the link with xdg-open/open")
	return cmd
}
