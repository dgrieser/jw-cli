package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/render"
	"github.com/dgrieser/jw-cli/internal/service"
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
			langs = service.FilterLanguages(langs, search)
			format, err := a.Format()
			if err != nil {
				return err
			}
			if format == render.JSON {
				return a.WriteJSON(langs)
			}
			return a.Write(languagesTable(langs, a.Text()))
		},
	}
	cmd.Flags().StringVarP(&search, "search", "s", "", "filter by name, vernacular name, symbol, or locale")
	return cmd
}

func languagesTable(langs []model.Language, txt *i18n.Messages) string {
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 2, 4, 2, ' ', 0)
	fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", txt.ColSymbol, txt.ColLocale, txt.ColName, txt.ColVernacular)
	for _, l := range langs {
		name := l.Name
		if l.IsSignLanguage {
			name += " (" + txt.SignLanguage + ")"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", l.Symbol, l.Locale, name, l.Vernacular)
	}
	w.Flush()
	return b.String()
}
