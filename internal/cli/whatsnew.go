package cli

import (
	"fmt"
	"slices"
	"time"

	"github.com/spf13/cobra"

	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/results"
	"github.com/dgrieser/jw-cli/internal/service"
)

// newKinds are what jw new --kind takes.
var newKinds = []string{"media", "pub", "news"}

func newNewCmd(a *app.App) *cobra.Command {
	var (
		kind  string
		days  int
		limit int
	)
	cmd := &cobra.Command{
		Use:     "new",
		Aliases: []string{"latest", "whatsnew"},
		Short:   "List what is new on jw.org: media, publications and news",
		Long: `List what the site published lately, newest first: jw.org's page of
what is new ("What's New": articles, publications, news) together with the
media library's latest media — jw.org's pick of the latest videos and
everything else published in the last --days days. Each row starts with the
day it came out.

Finding the latest media walks the media library's whole category tree, a few
dozen requests the first time (cached after); --kind pub or --kind news
reads jw.org's page alone.

The rows are numbered for jw show, jw open and jw download: a medium by its
key, anything else by its page.

Examples:
  jw new                     everything new
  jw new --kind pub          the latest publications
  jw new --kind media -n 10  the ten latest media
  jw show 3                  read the third entry`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if kind != "" && !slices.Contains(newKinds, kind) {
				return fmt.Errorf("unknown --kind %q (want media, pub or news)", kind)
			}
			if days < 1 {
				return fmt.Errorf("--days must be at least 1")
			}
			lng, err := a.Lang(cmd.Context())
			if err != nil {
				return err
			}
			items, err := a.Service().Latest(cmd.Context(), lng.Symbol, service.LatestFilter{
				Since:   time.Now().AddDate(0, 0, -days),
				NoMedia: kind == "pub" || kind == "news",
			})
			if err != nil && len(items) == 0 {
				return err
			}
			items = filterNew(items, kind)
			if limit > 0 && len(items) > limit {
				items = items[:limit]
			}
			rs := results.ResultSet{Kind: "new", Query: kind, Lang: lng.Symbol, Items: service.NewItemsToResults(items)}
			return writeListing(a, rs, a.Text().UINavLatest)
		},
	}
	fl := cmd.Flags()
	fl.StringVarP(&kind, "kind", "k", "", "only one kind: media, pub (articles and publications) or news")
	fl.IntVar(&days, "days", 30, "how many days back the media are looked for")
	fl.IntVarP(&limit, "limit", "n", 0, "maximum number of entries")
	return cmd
}

// filterNew keeps the entries of one kind, all for none.
func filterNew(items []model.NewItem, kind string) []model.NewItem {
	if kind == "" {
		return items
	}
	return slices.DeleteFunc(items, func(it model.NewItem) bool { return service.LatestGroup(it) != kind })
}
