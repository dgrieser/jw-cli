package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/render"
)

func newLanguagesCmd(a *app.App) *cobra.Command {
	var search string
	cmd := &cobra.Command{
		Use:   "languages",
		Short: "List available content languages",
		Long: `List all languages with content on jw.org, showing the JW language
symbol (used by the APIs), the ISO locale, and the language names.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			langs, err := a.Resolver().All(cmd.Context())
			if err != nil {
				return err
			}
			if search != "" {
				q := strings.ToLower(search)
				filtered := langs[:0]
				for _, l := range langs {
					if strings.Contains(strings.ToLower(l.Name), q) ||
						strings.Contains(strings.ToLower(l.Vernacular), q) ||
						strings.EqualFold(l.Locale, q) ||
						strings.EqualFold(l.Symbol, q) {
						filtered = append(filtered, l)
					}
				}
				langs = filtered
			}
			format, err := a.Format()
			if err != nil {
				return err
			}
			if format == render.JSON {
				return a.WriteJSON(langs)
			}
			return a.Write(languagesTable(langs))
		},
	}
	cmd.Flags().StringVarP(&search, "search", "s", "", "filter by name, vernacular name, symbol, or locale")
	return cmd
}

func languagesTable(langs []model.Language) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "SYMBOL\tLOCALE\tNAME\tVERNACULAR")
	for _, l := range langs {
		name := l.Name
		if l.IsSignLanguage {
			name += " (sign language)"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", l.Symbol, l.Locale, name, l.Vernacular)
	}
	w.Flush()
	return b.String()
}
