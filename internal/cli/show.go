package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/render"
)

func newShowCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <index>",
		Short: "Display the content of a result from the last listing",
		Long: `Fetch and render the item behind a numbered result of the last
listing command (jw search, jw media browse, jw pub, ...): articles are
rendered in the selected output format, videos/audio show their details.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			item, err := resolveIndexArg(a, args[0])
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			switch item.Kind {
			case "video", "audio":
				lng, err := a.Lang(ctx)
				if err != nil {
					return err
				}
				mi, err := a.Mediator().MediaItem(ctx, lng.Symbol, item.LANK)
				if err != nil {
					return err
				}
				format, err := a.Format()
				if err != nil {
					return err
				}
				if format == render.JSON {
					return a.WriteJSON(mi)
				}
				return a.Write(mediaInfoText(mi))
			case "category":
				return fmt.Errorf("result %d is a category; browse it with: jw media browse %s", item.Index, item.CategoryKey)
			case "file", "image":
				format, err := a.Format()
				if err != nil {
					return err
				}
				if format == render.JSON {
					return a.WriteJSON(item)
				}
				return a.Write(fmt.Sprintf("%s\n%s\nDownload with: jw download %d", item.Title, item.FileURL, item.Index))
			}
			// articles, publications, bible hits, indexes
			target := item.WOLLink
			if target == "" {
				target = item.JWLink
			}
			if target == "" && item.DocID != 0 {
				target = fmt.Sprint(item.DocID)
			}
			if target == "" {
				return fmt.Errorf("result %d (%s) has no readable content", item.Index, item.Title)
			}
			art, err := fetchArticle(ctx, a, target)
			if err != nil {
				return err
			}
			return writeArticle(a, art)
		},
	}
	return cmd
}
