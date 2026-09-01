package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/service"
)

// wolKnownCategories is the fc[] category list of the active language, as last
// seen on a search page. Best effort: an empty list falls back to the
// language-independent superset.
func wolKnownCategories(ctx context.Context, a *app.App) []string {
	lng, err := a.Lang(ctx)
	if err != nil {
		return nil
	}
	return a.Service().KnownCategories(ctx, lng)
}

// categoryFilter is the --all/--include/--exclude flag set of the wol engine:
// which publication categories a search covers.
type categoryFilter struct {
	all     bool
	include []string
	exclude []string
	// defaultExclude is left out when the user names none of the flags. Empty
	// means "no filter at all", which is what the site itself does.
	defaultExclude []string
}

func (f *categoryFilter) bind(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.BoolVar(&f.all, "all", false, "cover every publication category, bibles and indexes included")
	fl.StringSliceVar(&f.include, "include", nil, "cover only these publication categories (see jw search --help)")
	fl.StringSliceVar(&f.exclude, "exclude", nil, "cover every publication category except these")
	cmd.MarkFlagsMutuallyExclusive("all", "include", "exclude")
}

// resolve turns the flags into the whitelist to send. known is the category
// list of the active language, as last seen on a search page.
func (f *categoryFilter) resolve(cmd *cobra.Command, known []string) (service.WOLCategories, error) {
	exclude := f.exclude
	if !cmd.Flags().Changed("exclude") {
		exclude = nil
	}
	return service.ResolveCategories(f.all, f.include, exclude, f.defaultExclude, known)
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
			lng, err := a.Lang(cmd.Context())
			if err != nil {
				return err
			}
			art, err := a.Service().DailyText(cmd.Context(), lng, date)
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
			lng, err := a.Lang(cmd.Context())
			if err != nil {
				return err
			}
			art, err := a.Service().Meetings(cmd.Context(), lng, date)
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
		}),
		newMeetingPartCmd(a, &dateStr, meetingPart{
			use:     "weekend",
			aliases: []string{"we", "wt"},
			short:   "Read the weekend meeting's Watchtower study article",
		}),
	)
	return cmd
}

// meetingPart describes one of the two meetings a subcommand can read.
type meetingPart struct {
	use     string
	aliases []string
	short   string
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
			lng, err := a.Lang(cmd.Context())
			if err != nil {
				return err
			}
			art, err := a.Service().MeetingPart(cmd.Context(), lng, date, part.use)
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
