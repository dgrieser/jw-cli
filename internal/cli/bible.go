package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/render"
	"github.com/dgrieser/jw-cli/internal/results"
	"github.com/dgrieser/jw-cli/internal/service"
)

func newBibleCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bible",
		Short: "Read the Bible with study notes, cross references, media, and research links",
	}
	cmd.AddCommand(
		newBibleReadCmd(a),
		newBibleNotesCmd(a),
		newBibleXrefsCmd(a),
		newBibleMediaCmd(a),
		newBibleResearchCmd(a),
		newBibleCitedCmd(a),
		newBibleBooksCmd(a),
	)
	return cmd
}

func newBibleReadCmd(a *app.App) *cobra.Command {
	var (
		edition   string
		allBibles bool
		depth     int
		assumeYes bool
	)
	cmd := &cobra.Command{
		Use:   "read <reference...>",
		Short: "Read verses, verse ranges, chapters, or spans of chapters",
		Long: `Read bible text in any edition available in the Watchtower Online
Library. References accept full names, abbreviations, and book numbers, in
English or in the selected content language.

A reference reads a verse ("Pr 8:8"), a list of them ("Pr 8:8, 9"), a range
("Pr 8:8-11"), a whole chapter ("Pr 8"), a range of chapters ("Pr 8-9"), or a
span running from one chapter into another ("Pr 8:30-9:6"). A span is printed
one chapter at a time, since that is how the library serves it.

With --unfold the study material of every verse is printed under that verse:
its study notes, and the text behind every reference it carries — the marginal
references and the research-guide passages — followed that many levels deep.

With --bible-all the passage is read in every bible the library carries in the
selected language, one after the other, so translations can be compared. Which
editions those are is looked up per language; an edition that does not carry the
passage — the Kingdom Interlinear has the Greek Scriptures only — is named at
the end instead of failing the run.

Examples:
  jw bible read Matthew 24:14
  jw bible read "mt 24:3-14"
  jw bible read "Pr 8-9"
  jw bible read "Pr 8:30-9:6"
  jw bible read "Joh 3:16; Ro 5:8" -o text
  jw bible read "Psalm 83" --bible nwt
  jw bible read "Joh 3:16" --bible-all
  jw bible read -l de "Matthäus 24:14"
  jw bible read John 3:16 --unfold 1`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if allBibles && cmd.Flags().Changed("bible") {
				return fmt.Errorf("--bible and --bible-all cannot be combined")
			}
			format, err := a.Format()
			if err != nil {
				return err
			}
			lng, err := a.Lang(ctx)
			if err != nil {
				return err
			}
			res, err := a.Service().ReadPassages(ctx, lng, service.ReadRequest{
				Refs: strings.Join(args, " "), Edition: edition, AllBibles: allBibles,
				Unfold: unfoldConfig(a, depth, assumeYes),
			}, a.Text())
			if err != nil {
				return err
			}
			if format == render.JSON {
				return a.WriteJSON(res.Passages)
			}
			body, err := service.FormatPassages(res, format, a.RenderOptions(a.HTTP().Base.WOL))
			if err != nil {
				return err
			}
			return a.WriteMarkdown(body)
		},
	}
	cmd.Flags().StringVarP(&edition, "bible", "b", "nwtsty", "bible edition, as available in the selected language: "+strings.Join(wol.BibleEditions, ", "))
	cmd.Flags().BoolVar(&allBibles, "bible-all", false, "read the passage in every bible available in the selected language")
	cmd.Flags().IntVar(&depth, "unfold", 0, "print the study notes and the text behind every reference, following references this many levels deep")
	cmd.Flags().BoolVarP(&assumeYes, "yes", "y", false, "do not ask before an unfold that needs many requests")
	return cmd
}

func newBibleNotesCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "notes <reference...>",
		Short: "Show the study notes on a verse or verse range",
		Long: `List the study notes attached to each verse, printed below the
verse text they explain.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := a.Format()
			if err != nil {
				return err
			}
			lng, err := a.Lang(cmd.Context())
			if err != nil {
				return err
			}
			entries, err := a.Service().Notes(cmd.Context(), lng, strings.Join(args, " "))
			if err != nil {
				return err
			}
			if format == render.JSON {
				return a.WriteJSON(entries)
			}
			if len(entries) == 0 {
				return a.Write(a.Text().NoStudyNotes)
			}
			var b strings.Builder
			for _, e := range entries {
				writeHeading(&b, format, e.Ref)
				if err := writeVerseText(&b, a, format, e.Verse); err != nil {
					return err
				}
				for _, n := range e.Notes {
					body, err := render.Render(n.HTML, format, a.RenderOptions(a.HTTP().Base.WOL))
					if err != nil {
						return err
					}
					fmt.Fprintf(&b, "%s\n\n", strings.TrimSpace(body))
				}
			}
			return a.WriteMarkdown(b.String())
		},
	}
	return cmd
}

func newBibleXrefsCmd(a *app.App) *cobra.Command {
	var resolve bool
	cmd := &cobra.Command{
		Use:   "xrefs <reference...>",
		Short: "Show the cross references (marginal references) of a verse",
		Long: `List the marginal cross references attached to each verse. With
-r|--resolve the full text of the referenced verses is fetched too.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			format, err := a.Format()
			if err != nil {
				return err
			}
			lng, err := a.Lang(ctx)
			if err != nil {
				return err
			}
			entries, err := a.Service().XRefs(ctx, lng, strings.Join(args, " "), resolve)
			if err != nil {
				return err
			}
			if format == render.JSON {
				return a.WriteJSON(entries)
			}
			if len(entries) == 0 {
				return a.Write(a.Text().NoCrossRefs)
			}
			var b strings.Builder
			for _, e := range entries {
				writeHeading(&b, format, e.Ref)
				for _, x := range e.XRefs {
					fmt.Fprintf(&b, "- %s\n", escapeListMarker(x.Citation))
					if x.ResolvedHTML != "" {
						body, err := render.Render(x.ResolvedHTML, format, a.RenderOptions(a.HTTP().Base.WOL))
						if err != nil {
							return err
						}
						fmt.Fprintf(&b, "\n%s\n\n", indent(strings.TrimSpace(body), "  "))
					}
				}
				b.WriteString("\n")
			}
			return a.WriteMarkdown(b.String())
		},
	}
	cmd.Flags().BoolVarP(&resolve, "resolve", "r", false, "fetch the full text of the referenced verses")
	return cmd
}

func newBibleMediaCmd(a *app.App) *cobra.Command {
	var (
		doDL bool
		dir  string
	)
	cmd := &cobra.Command{
		Use:   "media <reference...>",
		Short: "Show images and clips attached to a verse (with captions)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			lng, err := a.Lang(ctx)
			if err != nil {
				return err
			}
			items, err := a.Service().BibleMedia(ctx, lng, strings.Join(args, " "), !doDL)
			if err != nil {
				return err
			}
			if doDL {
				if len(items) == 0 {
					return fmt.Errorf("no media found")
				}
				return downloadAll(cmd.Context(), a, items, dir)
			}
			rs := results.ResultSet{Kind: "bible-media", Query: strings.Join(args, " "), Items: items}
			return writeListing(a, rs, fmt.Sprintf(a.Text().MediaOn, strings.Join(args, " ")))
		},
	}
	cmd.Flags().BoolVar(&doDL, "download", false, "download the media files")
	cmd.Flags().StringVarP(&dir, "dir", "d", "", "download directory")
	return cmd
}

func newBibleResearchCmd(a *app.App) *cobra.Command {
	var excerpts bool
	cmd := &cobra.Command{
		Use:   "research <reference...>",
		Short: "Show research-guide references on a verse (publications discussing it)",
		Long: `List the Research Guide entries attached to a verse: publications
that discuss it. Every listing is printed below the verse text it belongs to.
With -x|--excerpts the referenced passage of each publication is fetched and
shown, including a link to the full article.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			format, err := a.Format()
			if err != nil {
				return err
			}
			lng, err := a.Lang(ctx)
			if err != nil {
				return err
			}
			entries, err := a.Service().Research(ctx, lng, strings.Join(args, " "), excerpts)
			if err != nil {
				return err
			}
			if format == render.JSON {
				return a.WriteJSON(entries)
			}
			if len(entries) == 0 {
				return a.Write(a.Text().NoResearch)
			}
			var b strings.Builder
			for _, e := range entries {
				writeHeading(&b, format, e.Ref)
				if err := writeVerseText(&b, a, format, e.Verse); err != nil {
					return err
				}
				for _, it := range e.Items {
					fmt.Fprintf(&b, "- %s", mdLinked(it.Title, linkTarget(a, firstNonEmpty(it.ArticleURL, it.PCPath))))
					if it.Source != "" {
						fmt.Fprintf(&b, " (%s)", it.Source)
					}
					b.WriteString("\n")
					if it.ExcerptHTML != "" {
						body, err := render.Render(it.ExcerptHTML, format, a.RenderOptions(a.HTTP().Base.WOL))
						if err == nil {
							fmt.Fprintf(&b, "\n%s\n", indent(strings.TrimSpace(body), "  "))
						}
					}
					b.WriteString("\n")
				}
			}
			return a.WriteMarkdown(b.String())
		},
	}
	cmd.Flags().BoolVarP(&excerpts, "excerpts", "x", false, "fetch the referenced excerpt of each publication")
	return cmd
}

func newBibleBooksCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "books",
		Short: "List the 66 bible books with their numbers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := a.Format()
			if err != nil {
				return err
			}
			// best effort: without a resolvable language the English names stand
			lng, _ := a.Lang(cmd.Context())
			books := a.Service().Books(cmd.Context(), lng)
			if format == render.JSON {
				return a.WriteJSON(books)
			}
			var b strings.Builder
			for _, book := range books {
				fmt.Fprintf(&b, "%2d. %s\n", book.Number, book.Name)
			}
			return a.Write(b.String())
		},
	}
	return cmd
}

// writeVerseText renders the verse itself, printed above the study material
// listed for it. A verse without text is skipped.
func writeVerseText(b *strings.Builder, a *app.App, f render.Format, v model.Verse) error {
	if strings.TrimSpace(v.HTML) == "" {
		return nil
	}
	body, err := render.Render(v.HTML, f, a.RenderOptions(a.HTTP().Base.WOL))
	if err != nil {
		return err
	}
	if body = strings.TrimSpace(body); body != "" {
		fmt.Fprintf(b, "%s\n\n", body)
	}
	return nil
}

func writeHeading(b *strings.Builder, f render.Format, text string) {
	switch f {
	case render.Markdown, render.Raw:
		fmt.Fprintf(b, "## %s\n\n", text)
	case render.HTML:
		fmt.Fprintf(b, "<h2>%s</h2>\n", text)
	default:
		fmt.Fprintf(b, "%s\n\n", text)
	}
}

func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
