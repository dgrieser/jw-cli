package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/results"
)

// searchWOL runs the wol search engine for jw search -e wol.
func searchWOL(ctx context.Context, a *app.App, lng model.Language, query, scope, sortBy string, page int) (results.ResultSet, string, error) {
	cfg, err := a.WOL().ConfigFor(ctx, lng.Locale)
	if err != nil {
		return results.ResultSet{}, "", err
	}
	if sortBy == "rel" {
		sortBy = "occ" // wol's default ranking
	}
	sp, err := a.WOL().Search(ctx, cfg, query, wol.SearchOpts{Scope: scope, Sort: sortBy, Page: page})
	if err != nil {
		return results.ResultSet{}, "", err
	}
	header := fmt.Sprintf("wol results for %q (page %d)", query, sp.Page)
	if sp.Total > 0 {
		header = fmt.Sprintf("%d wol results for %q (page %d)", sp.Total, query, sp.Page)
	}
	rs := results.ResultSet{Kind: "wol-search", Query: query, Lang: lng.Symbol, Page: sp.Page, Items: sp.Results}
	return rs, header, nil
}

func newDailyTextCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dailytext [YYYY-MM-DD]",
		Short: "Show the daily text (Examining the Scriptures Daily)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			date := time.Now()
			if len(args) == 1 {
				var err error
				date, err = time.Parse("2006-01-02", args[0])
				if err != nil {
					return fmt.Errorf("invalid date %q (want YYYY-MM-DD)", args[0])
				}
			}
			cfg, err := a.WOLConfig(cmd.Context())
			if err != nil {
				return err
			}
			art, err := a.WOL().DailyText(cmd.Context(), cfg, date)
			if err != nil {
				return err
			}
			return writeArticle(a, art)
		},
	}
	return cmd
}

func newMeetingsCmd(a *app.App) *cobra.Command {
	var dateStr string
	cmd := &cobra.Command{
		Use:   "meetings",
		Short: "Show this week's meeting material (workbook and Watchtower study)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			date := time.Now()
			if dateStr != "" {
				var err error
				date, err = time.Parse("2006-01-02", dateStr)
				if err != nil {
					return fmt.Errorf("invalid date %q (want YYYY-MM-DD)", dateStr)
				}
			}
			cfg, err := a.WOLConfig(cmd.Context())
			if err != nil {
				return err
			}
			art, err := a.WOL().Meetings(cmd.Context(), cfg, date)
			if err != nil {
				return err
			}
			return writeArticle(a, art)
		},
	}
	cmd.Flags().StringVar(&dateStr, "date", "", "any date inside the wanted week (YYYY-MM-DD, default today)")
	return cmd
}
