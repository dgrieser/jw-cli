package cli

import (
	"cmp"
	"context"
	"fmt"
	htmlpkg "html"
	"net/url"
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
				items = rootCategoryRows(cats, lng.Symbol)
			} else {
				cat, err := a.Service().Category(cmd.Context(), lng.Symbol, key, limit, offset)
				if err != nil {
					return err
				}
				header = fmt.Sprintf("%s (%s)", cat.Name, cat.Key)
				items = categoryRows(cat, lng.Symbol)
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

// The jw.org documents of the video library and of the audio library. jw.org's
// own search links a media category as one of them opened on the category,
// and that is the only address of a category the site answers: its finder
// knows media items, not categories.
const (
	videoLibraryDocID = "1011214"
	audioLibraryDocID = "1011994"
)

// describedSuffix ends the key of a category of tracks with audio
// descriptions: the service makes those up after the category whose tracks
// they describe, which is where jw.org has them.
const describedSuffix = "-AudioDescriptions"

// categoryLink is the jw.org page of a media category in language symbol.
func categoryLink(key, symbol string, audio bool) string {
	docid := videoLibraryDocID
	if audio {
		docid = audioLibraryDocID
	}
	key = strings.TrimSuffix(key, describedSuffix)
	return "https://www.jw.org/open?" + url.Values{"docid": {docid}, "category": {key}, "wtlocale": {symbol}}.Encode()
}

// rootCategoryRows lists the top of the category tree, each row linked to its
// page on jw.org so jw open has one to print.
func rootCategoryRows(cats []model.Category, symbol string) []model.Result {
	rows := service.CategoriesToResults(cats)
	for i := range rows {
		rows[i].JWLink = categoryLink(rows[i].CategoryKey, symbol, inAudioLibrary(cats[i]))
	}
	return rows
}

// categoryRows lists a category's subcategories, then its media, each row
// linked to its page on jw.org.
func categoryRows(cat model.Category, symbol string) []model.Result {
	subs := service.CategoriesToResults(cat.Subcategories)
	for i := range subs {
		audio := inAudioLibrary(cat) || inAudioLibrary(cat.Subcategories[i])
		subs[i].JWLink = categoryLink(subs[i].CategoryKey, symbol, audio)
	}
	media := service.MediaToResults(cat.Media)
	for i := range media {
		media[i].JWLink = service.VideoLink(media[i].LANK, symbol)
	}
	return append(subs, media...)
}

// inAudioLibrary reports whether a category belongs to the audio library: it
// is the Audio tree or right under it, or its media — or its subcategories'
// — are audio. The mediator names a category's parent, not its whole line,
// so a container deeper down that holds no media of its own goes to the
// video library, which is where every other category is.
func inAudioLibrary(cat model.Category) bool {
	if cat.Key == "Audio" || cat.Parent != nil && cat.Parent.Key == "Audio" {
		return true
	}
	if strings.HasSuffix(cat.Key, describedSuffix) {
		return true
	}
	for _, c := range append([]model.Category{cat}, cat.Subcategories...) {
		if len(c.Media) > 0 {
			return c.Media[0].Type == "audio"
		}
	}
	return false
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
		Long: `Show a media item's metadata and all downloadable renditions,
numbered for jw download. The LANK (language-agnostic natural key) looks like
pub-jwbcov_201505_1_VIDEO and is shown with every video and audio row of
'jw media browse' and 'jw search -t videos'.

Examples:
  jw media info pub-jwbcov_201505_1_VIDEO
  jw media info pub-jwbcov_201505_1_VIDEO && jw download 1`,
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
			return writeMediaInfo(a, item)
		},
	}
	return cmd
}

// categoryKeyOf is the mediator category a row stands for, or "" for a row
// that is none: a category of jw media browse, or one the jw.org search found,
// which it keys "mc-<category>" (kind videoCategory or audioCategory).
func categoryKeyOf(r model.Result) string {
	if r.CategoryKey != "" {
		return r.CategoryKey
	}
	if key, ok := strings.CutPrefix(r.LANK, "mc-"); ok {
		return key
	}
	return ""
}

// isGalleryVideo reports whether a row is a video of a bible book's media
// gallery. The gallery names no LANK for it, only its gallery page, which
// names the video the library plays (see galleryVideo).
func isGalleryVideo(r model.Result) bool {
	return r.Kind == "video" && r.LANK == "" && r.WOLLink != ""
}

// mediaOf resolves a listing row to the media item behind it: a video or an
// audio track by its LANK, from the mediator, or a gallery video from its
// gallery page. ok is false for a row that is neither, which has no media item
// to ask for — not even one by an empty key.
func mediaOf(ctx context.Context, a *app.App, r model.Result) (item model.MediaItem, ok bool, err error) {
	switch {
	case isMediaLANK(r.LANK):
		lng, err := a.Lang(ctx)
		if err != nil {
			return model.MediaItem{}, true, err
		}
		item, err = a.Service().MediaItem(ctx, lng.Symbol, r.LANK)
		return item, true, err
	case isGalleryVideo(r):
		item, err = galleryVideo(ctx, a, r.WOLLink)
		return item, true, err
	}
	return model.MediaItem{}, false, nil
}

// galleryVideo reads a gallery video as a media item: its gallery page has the
// title and the caption, and names the address its player reads the
// renditions from.
func galleryVideo(ctx context.Context, a *app.App, page string) (model.MediaItem, error) {
	asset, err := a.WOL().GalleryItem(ctx, page)
	if err != nil {
		return model.MediaItem{}, err
	}
	if asset.VideoSource == "" {
		return model.MediaItem{}, fmt.Errorf("%s: the gallery page names no video", page)
	}
	v, err := a.WOL().Video(ctx, asset.VideoSource)
	if err != nil {
		return model.MediaItem{}, err
	}
	item := model.MediaItem{
		Type: "video", Title: cmp.Or(asset.Caption, v.Title),
		Description: asset.Description, Files: v.Files,
	}
	if d := v.Files[0].Duration; d > 0 {
		item.DurationSec, item.DurationFormatted = d, subtitles.Clock(d)
	}
	return item, nil
}

// writeMediaInfo prints a media item's details and saves its renditions as
// the last listing, numbered as the details number them, so `jw download <n>`
// downloads file n. jw media info and jw show on a video or audio row print
// the same thing and leave the same listing behind.
func writeMediaInfo(a *app.App, item model.MediaItem) error {
	rs := results.ResultSet{Kind: "media-info", Query: item.LANK, Items: mediaFileRows(item)}
	if err := results.Save(a.Cache().Dir(), &rs); err != nil {
		return err
	}
	format, err := a.Format()
	if err != nil {
		return err
	}
	if format == render.JSON {
		return a.WriteJSON(item)
	}
	return writeComposed(a, format, mediaInfoHTML(item, a.Text(), a.Flags.NoURLs, format))
}

// mediaFileRows are a media item's renditions as listing rows. Each carries
// the item's LANK as well as its file: the row downloads that file, unless a
// quality is asked for, which the LANK then picks from all of them.
func mediaFileRows(item model.MediaItem) []model.Result {
	var rows []model.Result
	for _, f := range item.Files {
		label := f.Label
		if label == "" {
			label = strings.TrimPrefix(f.MimeType, "audio/")
		}
		rows = append(rows, model.Result{
			Kind: item.Type, Title: item.Title, Context: label,
			LANK: item.LANK, FileURL: f.URL, Checksum: f.Checksum, Filesize: f.Filesize,
		})
	}
	return rows
}

// mediaInfoHTML lays a media item's details out as a document for the
// selected format: its metadata, then its files, numbered as the listing
// writeMediaInfo saves numbers them.
func mediaInfoHTML(m model.MediaItem, txt *i18n.Messages, noURLs bool, format render.Format) string {
	esc := htmlpkg.EscapeString
	var b strings.Builder
	b.WriteString("<h1>" + esc(m.Title) + "</h1>")
	if m.Description != "" {
		b.WriteString("<p>" + esc(m.Description) + "</p>")
	}
	var facts []string
	add := func(label, value string) {
		if value != "" {
			facts = append(facts, esc(label)+": "+value)
		}
	}
	// the LANK is what jw media info and jw download take: a code span keeps
	// its underscores as they are in the markdown
	if m.LANK != "" {
		add(txt.LabelLANK, "<code>"+esc(m.LANK)+"</code>")
	}
	add(txt.LabelType, esc(m.Type))
	add(txt.LabelDuration, esc(m.DurationFormatted))
	add(txt.LabelPublished, esc(m.FirstPublished))
	add(txt.LabelCategory, esc(m.PrimaryCategory))
	if n := len(m.AvailableLanguages); n > 0 {
		sample := m.AvailableLanguages
		more := ""
		if n > 12 {
			sample, more = sample[:12], ", ..."
		}
		facts = append(facts, esc(fmt.Sprintf(txt.LabelLanguages, n, strings.Join(sample, ", ")+more)))
	}
	b.WriteString("<ul><li>" + strings.Join(facts, "</li><li>") + "</li></ul>")
	if len(m.Files) > 0 {
		b.WriteString("<h2>" + esc(txt.FilesHeading) + "</h2>")
		// numbered: the numbers are the indexes `jw download <n>` takes. Plain
		// text writes any list with dashes, so there each file is a paragraph
		// with its number spelled out.
		var files []string
		for i, f := range m.Files {
			line := "<strong>" + esc(cmp.Or(f.Label, f.MimeType)) + "</strong> (" + esc(humanSize(f.Filesize)) + ")"
			if f.SubtitlesURL != "" {
				line += " — " + esc(txt.Subtitles)
			}
			if !noURLs {
				line += `<br><a href="` + esc(f.URL) + `">` + esc(f.URL) + "</a>"
			}
			if format == render.Text {
				line = fmt.Sprintf("<p>%d. %s</p>", i+1, line)
			}
			files = append(files, line)
		}
		if format == render.Text {
			b.WriteString(strings.Join(files, ""))
		} else {
			b.WriteString("<ol><li>" + strings.Join(files, "</li><li>") + "</li></ol>")
		}
		if m.LANK != "" {
			b.WriteString("<p>" + inlineHTML(fmt.Sprintf(txt.DownloadHint, m.LANK)) + "</p>")
		} else {
			b.WriteString("<p>" + inlineHTML(txt.DownloadHintFiles) + "</p>")
		}
	}
	// jw media text looks an item up by its LANK; a gallery video has none
	if m.LANK != "" {
		b.WriteString("<p>" + inlineHTML(fmt.Sprintf(txt.MediaTextHint, m.LANK)) + "</p>")
	}
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
			return writeComposed(a, format, mediaTextHTML(item, text, a.Text(), transcript, timestamps, a.Flags.NoURLs))
		},
	}
	fl := cmd.Flags()
	fl.BoolVar(&transcript, "transcript", false, "also show the subtitles' transcript when lyrics or a document were found")
	fl.BoolVar(&timestamps, "timestamps", false, "show each line's time in the recording")
	return cmd
}

// mediaTextHTML lays out the words of item as a document for the selected
// format: the document it sings or reads, stanza by stanza, and the
// transcript of its subtitles, paragraph by paragraph.
func mediaTextHTML(item model.MediaItem, text model.MediaText, txt *i18n.Messages, transcript, timestamps, noURLs bool) string {
	esc := htmlpkg.EscapeString
	clock := func(sec float64) string { return "<code>" + subtitles.Clock(sec) + "</code> " }
	link := func(label, url string) string {
		return esc(label) + `: <a href="` + esc(url) + `">` + esc(url) + "</a>"
	}
	var b strings.Builder
	if d := text.Document; d != nil {
		b.WriteString("<h1>" + esc(cmp.Or(d.Title, item.Title)) + "</h1>")
		if d.Context != "" {
			b.WriteString("<p><em>" + esc(d.Context) + "</em></p>")
		}
		if d.Theme != "" {
			b.WriteString("<p>" + esc(d.Theme) + "</p>")
		}
		for _, blk := range d.Blocks {
			var lines []string
			for _, l := range blk.Lines {
				line := esc(l.Text)
				if timestamps && d.Synced && l.End > 0 {
					line = clock(l.Start) + line
				}
				lines = append(lines, line)
			}
			switch blk.Kind {
			case model.BlockHeading:
				b.WriteString("<h2>" + strings.Join(lines, " ") + "</h2>")
			case model.BlockChorus:
				if blk.Label != "" {
					lines = append([]string{"<strong>" + esc(blk.Label) + "</strong>"}, lines...)
				}
				b.WriteString("<blockquote><p>" + strings.Join(lines, "<br>") + "</p></blockquote>")
			default:
				if blk.Label != "" && len(lines) > 0 {
					lines[0] = "<strong>" + esc(blk.Label) + "</strong> " + lines[0]
				}
				b.WriteString("<p>" + strings.Join(lines, "<br>") + "</p>")
			}
		}
		if d.Closing != "" {
			b.WriteString("<p>" + esc(d.Closing) + "</p>")
		}
		var facts []string
		if d.PubName != "" {
			facts = append(facts, esc(txt.LabelPublication)+": "+esc(d.PubName))
		}
		if !noURLs {
			facts = append(facts, link(txt.LabelSource, d.URL))
			if d.PrintedEdition != "" {
				facts = append(facts, link(txt.PrintedEdition, d.PrintedEdition))
			}
			for _, l := range d.Downloads {
				facts = append(facts, link(l.Label, l.URL))
			}
		}
		if len(facts) > 0 {
			b.WriteString("<ul><li>" + strings.Join(facts, "</li><li>") + "</li></ul>")
		}
	}
	if len(text.Transcript) > 0 && (text.Document == nil || transcript) {
		if text.Document == nil {
			b.WriteString("<h1>" + esc(item.Title) + "</h1>")
		}
		heading := txt.TranscriptHeading
		if text.AITranscript {
			heading = txt.TranscriptAIHeading
		}
		b.WriteString("<h2>" + esc(heading) + "</h2>")
		// a paragraph of captions runs on as prose; with their times, each
		// caption stands on a line of its own behind its time
		for _, para := range subtitles.Paragraphs(text.Transcript) {
			var parts []string
			for _, c := range para {
				if timestamps {
					parts = append(parts, clock(c.Start)+esc(c.Text))
				} else {
					parts = append(parts, esc(c.Text))
				}
			}
			sep := " "
			if timestamps {
				sep = "<br>"
			}
			b.WriteString("<p>" + strings.Join(parts, sep) + "</p>")
		}
		if !noURLs && text.SubtitlesURL != "" {
			b.WriteString("<ul><li>" + link(txt.LabelSource, text.SubtitlesURL) + "</li></ul>")
		}
	}
	return b.String()
}
