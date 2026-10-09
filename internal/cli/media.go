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
	"github.com/dgrieser/jw-cli/internal/subtitles"
)

func newMediaCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "media",
		Short: "Browse videos and audio (JW Broadcasting media library)",
	}
	cmd.AddCommand(newMediaBrowseCmd(a), newMediaInfoCmd(a), newMediaTextCmd(a))
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
				cat, err := a.Service().Category(cmd.Context(), lng.Symbol, key, limit, offset)
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
			item, err := a.Service().MediaItem(cmd.Context(), lng.Symbol, args[0])
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
	fmt.Fprintf(&b, "\n"+txt.MediaTextHint+"\n", m.LANK)
	return b.String()
}

func newMediaTextCmd(a *app.App) *cobra.Command {
	var transcript, timestamps bool
	cmd := &cobra.Command{
		Use:   "text <LANK>",
		Short: "Show the lyrics, text or transcript of a video or audio item",
		Long: `Show the words of a media item.

A song's lyrics come from its page on jw.org: the songs of the songbook —
for the meetings, sung by a choir, instrumental, sung by children — are the
songbook's song of that number in the content language, as are the original
songs and the children's songs. A recording read from a publication shows the
document it reads. Any other video shows the transcript of its subtitles, or —
without subtitles — of the machine-made subtitles (AIVTT) pub-media lists for
it. Those are often listed before they are written, as an empty file; the
file is kept by its checksum and read again once pub-media lists a new one.

When a song's lyrics are found, its subtitles are not shown; --transcript
shows them as well. --timestamps sets each line's time in the recording
before it, where it is known.

Examples:
  jw media text pub-sjjm_1_VIDEO
  jw media text pub-osg_118_AUDIO -l de
  jw media text pub-mwbv_202705_1_VIDEO --timestamps`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			lng, err := a.Lang(cmd.Context())
			if err != nil {
				return err
			}
			item, err := a.Service().MediaItem(cmd.Context(), lng.Symbol, args[0])
			if err != nil {
				return err
			}
			text, err := a.Service().MediaText(cmd.Context(), lng.Symbol, item)
			if err != nil {
				return err
			}
			format, err := a.Format()
			if err != nil {
				return err
			}
			if format == render.JSON {
				return a.WriteJSON(text)
			}
			if text.Empty() {
				return fmt.Errorf(a.Text().NoMediaText, item.LANK)
			}
			return a.WriteMarkdown(mediaTextMarkdown(item, text, a.Text(), transcript, timestamps, a.Flags.NoURLs))
		},
	}
	fl := cmd.Flags()
	fl.BoolVar(&transcript, "transcript", false, "also show the subtitles' transcript when lyrics or a document were found")
	fl.BoolVar(&timestamps, "timestamps", false, "show each line's time in the recording")
	return cmd
}

// mediaTextMarkdown writes the words of item: the document it sings or reads,
// stanza by stanza, and the transcript of its subtitles, paragraph by
// paragraph.
func mediaTextMarkdown(item model.MediaItem, text model.MediaText, txt *i18n.Messages, transcript, timestamps, noURLs bool) string {
	var b strings.Builder
	if d := text.Document; d != nil {
		title := d.Title
		if title == "" {
			title = item.Title
		}
		fmt.Fprintf(&b, "# %s\n\n", title)
		if d.Context != "" {
			fmt.Fprintf(&b, "*%s*\n\n", d.Context)
		}
		if d.Theme != "" {
			fmt.Fprintf(&b, "%s\n\n", d.Theme)
		}
		for _, blk := range d.Blocks {
			var lines []string
			for _, l := range blk.Lines {
				line := l.Text
				if timestamps && d.Synced && l.End > 0 {
					line = "`" + subtitles.Clock(l.Start) + "` " + line
				}
				lines = append(lines, line)
			}
			switch blk.Kind {
			case model.BlockHeading:
				fmt.Fprintf(&b, "## %s\n\n", strings.Join(lines, " "))
			case model.BlockChorus:
				if blk.Label != "" {
					lines = append([]string{"**" + blk.Label + "**"}, lines...)
				}
				fmt.Fprintf(&b, "> %s\n\n", strings.Join(lines, "  \n> "))
			default:
				if len(lines) > 0 && !timestamps {
					lines[0] = escapeListMarker(lines[0])
				}
				if blk.Label != "" {
					lines[0] = "**" + blk.Label + "** " + lines[0]
				}
				fmt.Fprintf(&b, "%s\n\n", strings.Join(lines, "  \n"))
			}
		}
		if d.Closing != "" {
			fmt.Fprintf(&b, "%s\n\n", d.Closing)
		}
		var facts []string
		if d.PubName != "" {
			facts = append(facts, fmt.Sprintf("- %s: %s", txt.LabelPublication, d.PubName))
		}
		if !noURLs {
			facts = append(facts, fmt.Sprintf("- %s: <%s>", txt.LabelSource, d.URL))
			if d.PrintedEdition != "" {
				facts = append(facts, fmt.Sprintf("- %s: <%s>", txt.PrintedEdition, d.PrintedEdition))
			}
			for _, l := range d.Downloads {
				facts = append(facts, fmt.Sprintf("- %s: <%s>", l.Label, l.URL))
			}
		}
		if len(facts) > 0 {
			b.WriteString(strings.Join(facts, "\n") + "\n\n")
		}
	}
	if len(text.Transcript) > 0 && (text.Document == nil || transcript) {
		if text.Document == nil {
			fmt.Fprintf(&b, "# %s\n\n", item.Title)
		}
		heading := txt.TranscriptHeading
		if text.AITranscript {
			heading = txt.TranscriptAIHeading
		}
		fmt.Fprintf(&b, "## %s\n\n", heading)
		for _, para := range subtitles.Paragraphs(text.Transcript) {
			var parts []string
			for _, c := range para {
				if timestamps {
					parts = append(parts, "`"+subtitles.Clock(c.Start)+"` "+c.Text)
				} else {
					parts = append(parts, c.Text)
				}
			}
			sep := " "
			if timestamps {
				sep = "  \n"
			} else {
				parts[0] = escapeListMarker(parts[0])
			}
			fmt.Fprintf(&b, "%s\n\n", strings.Join(parts, sep))
		}
		if !noURLs && text.SubtitlesURL != "" {
			fmt.Fprintf(&b, "- %s: <%s>\n", txt.LabelSource, text.SubtitlesURL)
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}
