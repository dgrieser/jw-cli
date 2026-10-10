package cli

import (
	"fmt"
	htmlpkg "html"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/download"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/render"
	"github.com/dgrieser/jw-cli/internal/results"
	"github.com/dgrieser/jw-cli/internal/service"
	"github.com/dgrieser/jw-cli/internal/subtitles"
)

// bookArg is the book a book-document command is about, named as a reference
// names it or by number, possibly over several words ("song of solomon").
func bookArg(cmd *cobra.Command, a *app.App, args []string) (model.Language, int, error) {
	lng, err := a.Lang(cmd.Context())
	if err != nil {
		return model.Language{}, 0, err
	}
	book, err := a.Service().ParseBook(cmd.Context(), lng, strings.Join(args, " "))
	return lng, book, err
}

func editionFlag(cmd *cobra.Command, edition *string) {
	cmd.Flags().StringVarP(edition, "bible", "b", "nwtsty", "bible edition, as available in the selected language: "+strings.Join(wol.BibleEditions, ", "))
}

func newBibleIntroCmd(a *app.App) *cobra.Command {
	var (
		edition    string
		timestamps bool
		doDL       bool
		quality    string
		dir        string
	)
	cmd := &cobra.Command{
		Use:   "intro <book>",
		Short: "Show a bible book's introduction: facts, noteworthy facts, video",
		Long: `Show the introduction of a bible book, as the study edition has it.

The facts of the book's writing come first — writer, place written, when it
was completed, the time it covers. Those the introduction lists under its video
are completed from the study edition's table of the books of the Bible, which
every book has a row in, so a book introduced by the video alone has them too.

Then come the noteworthy facts written under the video or, for a book whose
introduction is the video alone, the transcript of the video's subtitles
(--timestamps sets each line's time before it). The video itself is linked at
the end; --download saves it in the --quality asked for.

Examples:
  jw bible intro Genesis
  jw bible intro Mt
  jw bible intro -l de Matthäus
  jw bible intro 19 --timestamps
  jw bible intro Genesis --download -q 480p`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			format, err := a.Format()
			if err != nil {
				return err
			}
			lng, book, err := bookArg(cmd, a, args)
			if err != nil {
				return err
			}
			intro, err := a.Service().BookIntro(ctx, lng, edition, book)
			if err != nil {
				return err
			}
			if doDL {
				if intro.Video == nil {
					return fmt.Errorf("%s: no video", intro.Title)
				}
				f, err := download.PickVideo(intro.Video.Files, quality)
				if err != nil {
					return err
				}
				path, err := downloadURL(ctx, a, f.URL, f.Checksum, f.Filesize, dir, "")
				if err != nil {
					return err
				}
				return a.Write(path)
			}
			if format == render.JSON {
				return a.WriteJSON(intro)
			}
			body, err := render.Render(introHTML(intro, a, timestamps), format, a.RenderOptions(a.HTTP().Base.WOL))
			if err != nil {
				return err
			}
			return a.WriteMarkdown(body)
		},
	}
	editionFlag(cmd, &edition)
	fl := cmd.Flags()
	fl.BoolVar(&timestamps, "timestamps", false, "show each line's time in the video, for a transcript")
	fl.BoolVar(&doDL, "download", false, "download the video instead of printing the introduction")
	fl.StringVarP(&quality, "quality", "q", "best", "video quality: best, worst, 1080p, 720p, 480p, 360p, 240p")
	fl.StringVarP(&dir, "dir", "d", "", "download directory (default current directory)")
	return cmd
}

// listParagraph matches the paragraph a list item wraps its text in.
var listParagraph = regexp.MustCompile(`(?s)<li>\s*<p[^>]*>(.*?)</p>\s*</li>`)

// introHTML lays an introduction out as one document: the title, the facts,
// the noteworthy facts or the transcript, and the video.
func introHTML(intro model.BookIntro, a *app.App, timestamps bool) string {
	esc := htmlpkg.EscapeString
	var b strings.Builder
	b.WriteString("<h2>" + esc(intro.Title) + "</h2>")
	if len(intro.Facts) > 0 {
		b.WriteString("<ul>")
		for _, f := range intro.Facts {
			b.WriteString("<li><strong>" + esc(f.Label) + ":</strong> " + esc(f.Value) + "</li>")
		}
		b.WriteString("</ul>")
	}
	switch {
	case intro.NotesHTML != "":
		// the notes head themselves at the level of the title; they belong
		// under it
		notes := strings.NewReplacer("<h2", "<h3", "</h2>", "</h3>").Replace(intro.NotesHTML)
		// a list item's text sits in a paragraph of its own, which plain
		// text would set apart from its marker
		notes = listParagraph.ReplaceAllString(notes, "<li>$1</li>")
		b.WriteString(notes)
	case len(intro.Transcript) > 0:
		b.WriteString("<h3>" + esc(a.Text().TranscriptHeading) + "</h3>")
		for _, para := range subtitles.Paragraphs(intro.Transcript) {
			var parts []string
			for _, c := range para {
				if timestamps {
					parts = append(parts, "<code>"+subtitles.Clock(c.Start)+"</code> "+esc(c.Text))
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
	}
	if v := intro.Video; v != nil && len(v.Files) > 0 {
		f, err := download.PickVideo(v.Files, "720p")
		if err != nil {
			f = v.Files[0]
		}
		title := esc(firstNonEmpty(v.Title, intro.Title))
		line := "<strong>" + esc(a.Text().LabelVideo) + ":</strong> <a href=\"" + esc(f.URL) + "\">" + title + "</a>"
		if f.Duration > 0 {
			line += " (" + subtitles.Clock(f.Duration) + ")"
		}
		b.WriteString("<p>" + line + "</p>")
	}
	return b.String()
}

func newBibleOutlineCmd(a *app.App) *cobra.Command {
	var edition string
	cmd := &cobra.Command{
		Use:   "outline <book>",
		Short: "Show a bible book's outline of contents (the overview of the Gospels and Acts)",
		Long: `Show the outline of contents of a bible book: what each part of the
book is about, heading by heading, with the verses it covers — chapter by
chapter. The Gospels and Acts have an overview instead, whose headings run
across chapters and name them themselves.

jw bible read prints the same headings between the verses it reads.

Examples:
  jw bible outline Genesis
  jw bible outline Mt
  jw bible outline 65 -o text`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := a.Format()
			if err != nil {
				return err
			}
			lng, book, err := bookArg(cmd, a, args)
			if err != nil {
				return err
			}
			o, err := a.Service().BookOutlineDoc(cmd.Context(), lng, edition, book)
			if err != nil {
				return err
			}
			if format == render.JSON {
				return a.WriteJSON(o)
			}
			name := a.Service().BookTable(cmd.Context(), lng).Name(book)
			body, err := service.FormatBookOutline(o, name, format, a.RenderOptions(a.HTTP().Base.WOL))
			if err != nil {
				return err
			}
			return a.WriteMarkdown(body)
		},
	}
	editionFlag(cmd, &edition)
	return cmd
}

func newBibleGalleryCmd(a *app.App) *cobra.Command {
	var (
		edition string
		doDL    bool
		dir     string
	)
	cmd := &cobra.Command{
		Use:   "gallery <book>",
		Short: "List a bible book's media gallery: its pictures and videos by chapter",
		Long: `List the media gallery of a bible book, as the study edition has it:
the pictures and videos of each chapter, numbered like any other listing, so
jw show and jw download take a row's number. --download saves every picture.
Not every book has a gallery.

Examples:
  jw bible gallery Matthew
  jw bible gallery Mt --download -d pictures`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := a.Format()
			if err != nil {
				return err
			}
			lng, book, err := bookArg(cmd, a, args)
			if err != nil {
				return err
			}
			g, err := a.Service().BookGallery(cmd.Context(), lng, edition, book)
			if err != nil {
				return err
			}
			items := galleryResults(g)
			if doDL {
				var pictures []model.Result
				for _, it := range items {
					if it.Kind == "image" && it.FileURL != "" {
						pictures = append(pictures, it)
					}
				}
				return downloadAll(cmd.Context(), a, pictures, dir)
			}
			if format == render.JSON {
				return a.WriteJSON(g)
			}
			return writeListing(a, results.ResultSet{Kind: "bible-gallery", Query: strings.Join(args, " "), Items: items}, g.Title)
		},
	}
	editionFlag(cmd, &edition)
	cmd.Flags().BoolVar(&doDL, "download", false, "download the pictures")
	cmd.Flags().StringVarP(&dir, "dir", "d", "", "download directory")
	return cmd
}

// galleryResults lists a gallery's tiles as rows, each under its chapter. A
// picture downloads as its large rendition; a video's row leads to its page.
func galleryResults(g model.BookGallery) []model.Result {
	var out []model.Result
	for _, grp := range g.Groups {
		for _, t := range grp.Items {
			r := model.Result{Kind: "image", Title: t.Title, Context: grp.Heading, WOLLink: t.URL, ImageURL: t.Image}
			if t.Video {
				r.Kind = "video"
			} else {
				r.FileURL = t.Image
			}
			out = append(out, r)
		}
	}
	return out
}
