package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dgrieser/jw-cli/internal/api/search"
	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/results"
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
  jw search -l de Königreich`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.Join(args, " ")
			lng, err := a.Lang(cmd.Context())
			if err != nil {
				return err
			}
			if page < 1 {
				page = 1
			}
			var rs results.ResultSet
			var header string
			switch engine {
			case "jworg", "jw", "":
				sp, err := a.Search().Search(cmd.Context(), lng.Symbol, search.Params{
					Query: query, Facet: facet, Sort: sortBy,
					Offset: (page - 1) * limit, Limit: limit,
				})
				if err != nil {
					return err
				}
				header = fmt.Sprintf("%d results for %q", sp.Total, query)
				if sp.Total > limit {
					header += fmt.Sprintf(" (page %d, %d per page)", page, limit)
				}
				rs = results.ResultSet{Kind: "search", Query: query, Lang: lng.Symbol, Page: page, Items: sp.Results}
			case "wol":
				var err error
				rs, header, err = searchWOL(cmd.Context(), a, lng, query, scope, sortBy, page)
				if err != nil {
					return err
				}
			default:
				return fmt.Errorf("invalid engine %q (want jworg or wol)", engine)
			}
			if interactive {
				return runSearchTUI(a, rs, header)
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
