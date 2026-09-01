package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/render"
	"github.com/dgrieser/jw-cli/internal/results"
	"github.com/dgrieser/jw-cli/internal/service"
)

func newMediaCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "media",
		Short: "Browse videos and audio (JW Broadcasting media library)",
	}
	cmd.AddCommand(newMediaBrowseCmd(a), newMediaInfoCmd(a))
	return cmd
}

func newMediaBrowseCmd(a *app.App) *cobra.Command {
	var (
		limit       int
		offset      int
		interactive bool
	)
	cmd := &cobra.Command{
		Use:   "browse [category-key]",
		Short: "Browse media categories and their items",
		Long: `Browse the media library category tree. Without an argument the
top-level categories are listed; pass a category key (e.g. VideoOnDemand,
LatestVideos, Audio) to drill in.

Examples:
  jw media browse
  jw media browse VideoOnDemand
  jw media browse LatestVideos -n 25`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			lng, err := a.Lang(cmd.Context())
			if err != nil {
				return err
			}
			if interactive {
				return runBrowseTUI(cmd.Context(), a, lng, argOrEmpty(args))
			}
			key := argOrEmpty(args)
			var items []model.Result
			header := ""
			if key == "" {
				cats, err := a.Mediator().RootCategories(cmd.Context(), lng.Symbol)
				if err != nil {
					return err
				}
				header = a.Text().MediaCategories
				items = service.CategoriesToResults(cats)
			} else {
				cat, err := a.Mediator().Category(cmd.Context(), lng.Symbol, key, limit, offset)
				if err != nil {
					return err
				}
				header = fmt.Sprintf("%s (%s)", cat.Name, cat.Key)
				items = append(service.CategoriesToResults(cat.Subcategories), service.MediaToResults(cat.Media)...)
			}
			rs := results.ResultSet{Kind: "media-browse", Query: key, Lang: lng.Symbol, Items: items}
			return writeListing(a, rs, header)
		},
	}
	fl := cmd.Flags()
	fl.IntVarP(&limit, "limit", "n", 0, "maximum number of media items")
	fl.IntVar(&offset, "offset", 0, "pagination offset")
	fl.BoolVarP(&interactive, "interactive", "i", false, "browse interactively (TUI)")
	return cmd
}

func argOrEmpty(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
}

func newMediaInfoCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info <LANK>",
		Short: "Show details and downloadable files of a media item",
		Long: `Show a media item's metadata and all downloadable renditions.
The LANK (language-agnostic natural key) looks like pub-jwb_202401_1_VIDEO
and is shown by 'jw media browse' and 'jw search -t videos'.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			lng, err := a.Lang(cmd.Context())
			if err != nil {
				return err
			}
			item, err := a.Mediator().MediaItem(cmd.Context(), lng.Symbol, args[0])
			if err != nil {
				return err
			}
			format, err := a.Format()
			if err != nil {
				return err
			}
			if format == render.JSON {
				return a.WriteJSON(item)
			}
			// also cache the renditions so `jw download <n>` works
			var items []model.Result
			for _, f := range item.Files {
				label := f.Label
				if label == "" {
					label = strings.TrimPrefix(f.MimeType, "audio/")
				}
				items = append(items, model.Result{
					Kind: item.Type, Title: item.Title, Context: label,
					LANK: item.LANK, FileURL: f.URL, Checksum: f.Checksum, Filesize: f.Filesize,
				})
			}
			_ = results.Save(a.Cache().Dir(), results.ResultSet{Kind: "media-info", Query: item.LANK, Lang: lng.Symbol, Items: items})
			return a.WriteMarkdown(mediaInfoText(item, a.Text(), a.Flags.NoURLs))
		},
	}
	return cmd
}

func mediaInfoText(m model.MediaItem, txt *i18n.Messages, noURLs bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", m.Title)
	if m.Description != "" {
		fmt.Fprintf(&b, "%s\n\n", m.Description)
	}
	fmt.Fprintf(&b, "- %s: %s\n- %s: %s\n", txt.LabelLANK, m.LANK, txt.LabelType, m.Type)
	if m.DurationFormatted != "" {
		fmt.Fprintf(&b, "- %s: %s\n", txt.LabelDuration, m.DurationFormatted)
	}
	if m.FirstPublished != "" {
		fmt.Fprintf(&b, "- %s: %s\n", txt.LabelPublished, m.FirstPublished)
	}
	if m.PrimaryCategory != "" {
		fmt.Fprintf(&b, "- %s: %s\n", txt.LabelCategory, m.PrimaryCategory)
	}
	if n := len(m.AvailableLanguages); n > 0 {
		sample := m.AvailableLanguages
		more := ""
		if n > 12 {
			sample, more = sample[:12], ", ..."
		}
		fmt.Fprintf(&b, "- "+txt.LabelLanguages+"\n", n, strings.Join(sample, ", ")+more)
	}
	if len(m.Files) > 0 {
		// an ordered list: the numbers are the indexes `jw download <n>` takes
		fmt.Fprintf(&b, "\n## %s\n\n", txt.FilesHeading)
		for i, f := range m.Files {
			label := f.Label
			if label == "" {
				label = f.MimeType
			}
			fmt.Fprintf(&b, "%d. **%s** (%s)", i+1, label, humanSize(f.Filesize))
			if f.SubtitlesURL != "" {
				b.WriteString(" — " + txt.Subtitles)
			}
			b.WriteString("\n")
			if !noURLs {
				fmt.Fprintf(&b, "   <%s>\n", f.URL)
			}
		}
		fmt.Fprintf(&b, "\n"+txt.DownloadHint+"\n", m.LANK)
	}
	return b.String()
}
