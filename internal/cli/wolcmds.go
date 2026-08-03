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
	header := a.Text().WolResults(sp.Total, query, sp.Page)
	rs := results.ResultSet{Kind: "wol-search", Query: query, Lang: lng.Symbol, Page: sp.Page, Items: sp.Results}
	return rs, header, nil
}

func newDailyTextCmd(a *app.App) *cobra.Command {
	var view articleView
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
			return view.write(cmd.Context(), a, art)
		},
	}
	view.bind(cmd)
	return cmd
}

func newMeetingsCmd(a *app.App) *cobra.Command {
	var (
		dateStr string
		view    articleView
	)
	cmd := &cobra.Command{
		Use:   "meetings",
		Short: "Show this week's meeting material (workbook and Watchtower study)",
		Long: `Without a subcommand, list the material for the week: what each
meeting covers and which publications it uses. The subcommands read the material
itself instead of linking to it.

Examples:
  jw meetings                          this week's overview
  jw meetings midweek                  the Life and Ministry workbook part
  jw meetings weekend                  the Watchtower study article
  jw meetings weekend --date 2026-08-10
  jw meetings midweek --refs           the verses that week's part cites
  jw meetings weekend --images         the study article's illustrations`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			date, err := meetingDate(dateStr)
			if err != nil {
				return err
			}
			cfg, err := a.WOLConfig(cmd.Context())
			if err != nil {
				return err
			}
			art, err := a.WOL().Meetings(cmd.Context(), cfg, date)
			if err != nil {
				return err
			}
			return view.write(cmd.Context(), a, art)
		},
	}
	view.bind(cmd)
	// persistent so the subcommands take --date too; the view flags stay local,
	// so each subcommand documents its own
	cmd.PersistentFlags().StringVar(&dateStr, "date", "", "any date inside the wanted week (YYYY-MM-DD, default today)")
	cmd.AddCommand(
		newMeetingPartCmd(a, &dateStr, meetingPart{
			use:     "midweek",
			aliases: []string{"mid", "mw"},
			short:   "Read the midweek meeting's workbook part (Life and Ministry)",
			pick:    func(p wol.MeetingParts) string { return p.Midweek },
		}),
		newMeetingPartCmd(a, &dateStr, meetingPart{
			use:     "weekend",
			aliases: []string{"we", "wt"},
			short:   "Read the weekend meeting's Watchtower study article",
			pick:    func(p wol.MeetingParts) string { return p.Weekend },
		}),
	)
	return cmd
}

// meetingPart describes one of the two meetings a subcommand can read.
type meetingPart struct {
	use     string
	aliases []string
	short   string
	// pick returns the document URL for this meeting, or "" when the week does
	// not list it.
	pick func(wol.MeetingParts) string
}

// newMeetingPartCmd builds a subcommand that resolves one meeting's document on
// the material page and then renders that document.
func newMeetingPartCmd(a *app.App, dateStr *string, part meetingPart) *cobra.Command {
	var view articleView
	cmd := &cobra.Command{
		Use:     part.use,
		Aliases: part.aliases,
		Short:   part.short,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			date, err := meetingDate(*dateStr)
			if err != nil {
				return err
			}
			cfg, err := a.WOLConfig(cmd.Context())
			if err != nil {
				return err
			}
			parts, err := a.WOL().MeetingParts(cmd.Context(), cfg, date)
			if err != nil {
				return err
			}
			target := part.pick(parts)
			if target == "" {
				return fmt.Errorf("no %s material listed for week %d/%d at %s",
					part.use, parts.Week, parts.Year, parts.URL)
			}
			art, err := a.WOL().DocumentByURL(cmd.Context(), target)
			if err != nil {
				return err
			}
			return view.write(cmd.Context(), a, art)
		},
	}
	view.bind(cmd)
	return cmd
}

// meetingDate parses the --date flag, defaulting to today.
func meetingDate(dateStr string) (time.Time, error) {
	if dateStr == "" {
		return time.Now(), nil
	}
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q (want YYYY-MM-DD)", dateStr)
	}
	return date, nil
}
