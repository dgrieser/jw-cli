package cli

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/model"
)

// searchWOL runs the wol search engine for jw search -e wol and jw bible cited.
func searchWOL(ctx context.Context, a *app.App, lng model.Language, p *searchParams, page int) (model.SearchPage, error) {
	cfg, err := a.WOL().ConfigFor(ctx, lng.Locale)
	if err != nil {
		return model.SearchPage{}, err
	}
	sortBy := p.Sort
	if sortBy == "rel" {
		sortBy = "occ" // wol's default ranking
	}
	opts := wol.SearchOpts{Scope: p.Scope, Sort: sortBy, Page: page, Categories: p.Categories.list}
	sp, err := a.WOL().Search(ctx, cfg, p.Query, opts)
	if err != nil {
		return model.SearchPage{}, err
	}
	// the category list differs per language; if the page names one the sent
	// whitelist did not know, its documents were just dropped — ask again with
	// the corrected list. The list is cached, so this happens once per language.
	if fixed, ok := p.Categories.corrected(sp.Filters); ok {
		// the corrected list needs no second correction, so further pages of
		// the same search go out as one request each
		p.Categories = wolCategories{list: fixed}
		opts.Categories = fixed
		if sp, err = a.WOL().Search(ctx, cfg, p.Query, opts); err != nil {
			return model.SearchPage{}, err
		}
	}
	return sp, nil
}

// wolKnownCategories is the fc[] category list of the active language, as last
// seen on a search page. Best effort: an empty list falls back to the
// language-independent superset.
func wolKnownCategories(ctx context.Context, a *app.App) []string {
	lng, err := a.Lang(ctx)
	if err != nil {
		return nil
	}
	cfg, err := a.WOL().ConfigFor(ctx, lng.Locale)
	if err != nil {
		return nil
	}
	return a.WOL().Categories(cfg)
}

// wolCategories is the resolved fc[] publication-category filter of one search.
type wolCategories struct {
	// list is the whitelist to send; nil sends no filter at all.
	list []string
	// exclude is what must stay out of the whitelist. Nil when the filter was
	// spelled out by the user (--all, --include), which switches the
	// correction pass off.
	exclude []string
}

// corrected returns the whitelist to retry with when the search page offers a
// category this language has but the sent list did not name.
func (c wolCategories) corrected(available []string) ([]string, bool) {
	if c.exclude == nil || len(available) == 0 {
		return nil, false
	}
	var fixed []string
	missing := false
	for _, cat := range available {
		if slices.Contains(c.exclude, cat) {
			continue
		}
		if !slices.Contains(c.list, cat) {
			missing = true
		}
		fixed = append(fixed, cat)
	}
	if !missing {
		return nil, false
	}
	return fixed, true
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
func (f *categoryFilter) resolve(cmd *cobra.Command, known []string) (wolCategories, error) {
	if len(known) == 0 {
		known = wol.AllCategories
	}
	if err := validCategories(f.include, known); err != nil {
		return wolCategories{}, err
	}
	if err := validCategories(f.exclude, known); err != nil {
		return wolCategories{}, err
	}
	switch {
	case f.all:
		return wolCategories{}, nil
	case len(f.include) > 0:
		return wolCategories{list: f.include}, nil
	}
	exclude := f.exclude
	if !cmd.Flags().Changed("exclude") {
		exclude = f.defaultExclude
	}
	if len(exclude) == 0 {
		return wolCategories{}, nil
	}
	var list []string
	for _, cat := range known {
		if !slices.Contains(exclude, cat) {
			list = append(list, cat)
		}
	}
	return wolCategories{list: list, exclude: exclude}, nil
}

func validCategories(cats, known []string) error {
	for _, cat := range cats {
		if !slices.Contains(known, cat) && !slices.Contains(wol.AllCategories, cat) {
			return fmt.Errorf("unknown publication category %q (known: %s)", cat, strings.Join(known, ", "))
		}
	}
	return nil
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
