package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/bibleref"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/render"
	"github.com/dgrieser/jw-cli/internal/results"
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
		newBibleBooksCmd(a),
	)
	return cmd
}

// bookTable returns the reference table, merged with localized book names
// for the active language when they can be fetched (best effort).
func bookTable(ctx context.Context, a *app.App) *bibleref.Table {
	t := bibleref.English()
	lng, err := a.Lang(ctx)
	if err != nil || lng.Locale == "en" {
		return t
	}
	cfg, err := a.WOL().ConfigFor(ctx, lng.Locale)
	if err != nil {
		return t
	}
	if names, err := a.WOL().LocalizedBookNames(ctx, cfg); err == nil {
		t.Merge(names)
	}
	return t
}

// parseRefsArg joins args into one reference string and parses it.
func parseRefsArg(ctx context.Context, a *app.App, args []string) ([]bibleref.Ref, *bibleref.Table, error) {
	t := bookTable(ctx, a)
	refs, err := bibleref.Parse(strings.Join(args, " "), t)
	if err != nil {
		return nil, nil, err
	}
	return refs, t, nil
}

// chapterFor fetches the wol chapter page containing ref.
func chapterFor(ctx context.Context, a *app.App, edition string, ref bibleref.Ref) (*wol.ChapterDoc, error) {
	cfg, err := a.WOLConfig(ctx)
	if err != nil {
		return nil, err
	}
	return a.WOL().Chapter(ctx, cfg, edition, ref.Book, ref.Chapter)
}

func newBibleReadCmd(a *app.App) *cobra.Command {
	var edition string
	cmd := &cobra.Command{
		Use:   "read <reference...>",
		Short: "Read verses, verse ranges, or whole chapters",
		Long: `Read bible text in any edition available in the Watchtower Online
Library. References accept full names, abbreviations, and book numbers, in
English or in the selected content language.

Examples:
  jw bible read Matthew 24:14
  jw bible read "mt 24:3-14"
  jw bible read "Joh 3:16; Ro 5:8" -o text
  jw bible read "Psalm 83" --bible nwt
  jw bible read -l de "Matthäus 24:14"`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			refs, table, err := parseRefsArg(ctx, a, args)
			if err != nil {
				return err
			}
			format, err := a.Format()
			if err != nil {
				return err
			}
			type passage struct {
				Ref    string        `json:"ref"`
				Verses []model.Verse `json:"verses"`
			}
			var passages []passage
			chapters := map[string]*wol.ChapterDoc{}
			for _, ref := range refs {
				key := fmt.Sprintf("%s-%d-%d", edition, ref.Book, ref.Chapter)
				doc, ok := chapters[key]
				if !ok {
					doc, err = chapterFor(ctx, a, edition, ref)
					if err != nil {
						return err
					}
					chapters[key] = doc
				}
				verses, err := doc.Verses(ref.VerseStart, ref.VerseEnd)
				if err != nil {
					return fmt.Errorf("%s: %w", refString(ref, table), err)
				}
				passages = append(passages, passage{Ref: refString(ref, table), Verses: verses})
			}
			if format == render.JSON {
				return a.WriteJSON(passages)
			}
			var b strings.Builder
			for i, p := range passages {
				if i > 0 {
					b.WriteString("\n")
				}
				html := ""
				for _, v := range p.Verses {
					html += v.HTML + " "
				}
				body, err := render.Render(html, format, render.Options{BaseURL: a.HTTP().Base.WOL})
				if err != nil {
					return err
				}
				switch format {
				case render.Markdown:
					fmt.Fprintf(&b, "## %s\n\n%s\n", p.Ref, body)
				case render.HTML:
					fmt.Fprintf(&b, "<h2>%s</h2>\n%s\n", p.Ref, body)
				default:
					fmt.Fprintf(&b, "%s\n\n%s\n", p.Ref, body)
				}
			}
			return a.Write(b.String())
		},
	}
	cmd.Flags().StringVarP(&edition, "bible", "b", "nwtsty", "bible edition: "+strings.Join(wol.BibleEditions, ", "))
	return cmd
}

// refString renders a ref with the (possibly localized) book table.
func refString(r bibleref.Ref, t *bibleref.Table) string {
	name := t.Name(r.Book)
	switch {
	case r.VerseStart == 0:
		return fmt.Sprintf("%s %d", name, r.Chapter)
	case r.VerseEnd > r.VerseStart:
		return fmt.Sprintf("%s %d:%d-%d", name, r.Chapter, r.VerseStart, r.VerseEnd)
	default:
		return fmt.Sprintf("%s %d:%d", name, r.Chapter, r.VerseStart)
	}
}

// forEachStudySection iterates the study sections of every verse in refs.
func forEachStudySection(ctx context.Context, a *app.App, args []string, fn func(ref string, sec model.StudySection)) error {
	refs, table, err := parseRefsArg(ctx, a, args)
	if err != nil {
		return err
	}
	chapters := map[string]*wol.ChapterDoc{}
	for _, ref := range refs {
		key := fmt.Sprintf("%d-%d", ref.Book, ref.Chapter)
		doc, ok := chapters[key]
		if !ok {
			doc, err = chapterFor(ctx, a, "nwtsty", ref)
			if err != nil {
				return err
			}
			chapters[key] = doc
		}
		from, to := ref.VerseStart, ref.VerseEnd
		if from == 0 {
			from, to = 1, 999
			// bound the scan to the chapter's real last verse
			if verses, err := doc.Verses(0, 0); err == nil && len(verses) > 0 {
				to = verses[len(verses)-1].ID % 1000
			}
		}
		for v := from; v <= to; v++ {
			sec, ok := doc.StudySection(v)
			if !ok {
				continue
			}
			label := sec.Verse
			if label == "" {
				label = refString(bibleref.Ref{Book: ref.Book, Chapter: ref.Chapter, VerseStart: v, VerseEnd: v}, table)
			}
			fn(label, sec)
		}
	}
	return nil
}

func newBibleNotesCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "notes <reference...>",
		Short: "Show the study notes on a verse or verse range",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := a.Format()
			if err != nil {
				return err
			}
			type entry struct {
				Ref   string            `json:"ref"`
				Notes []model.StudyNote `json:"notes"`
			}
			var entries []entry
			err = forEachStudySection(cmd.Context(), a, args, func(ref string, sec model.StudySection) {
				if len(sec.Notes) > 0 {
					entries = append(entries, entry{Ref: ref, Notes: sec.Notes})
				}
			})
			if err != nil {
				return err
			}
			if format == render.JSON {
				return a.WriteJSON(entries)
			}
			if len(entries) == 0 {
				return a.Write("No study notes found (study notes are available in the study edition, nwtsty).")
			}
			var b strings.Builder
			for _, e := range entries {
				writeHeading(&b, format, e.Ref)
				for _, n := range e.Notes {
					body, err := render.Render(n.HTML, format, render.Options{BaseURL: a.HTTP().Base.WOL})
					if err != nil {
						return err
					}
					fmt.Fprintf(&b, "%s\n\n", strings.TrimSpace(body))
				}
			}
			return a.Write(b.String())
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
			type entry struct {
				Ref   string           `json:"ref"`
				XRefs []model.CrossRef `json:"xrefs"`
			}
			var entries []entry
			err = forEachStudySection(ctx, a, args, func(ref string, sec model.StudySection) {
				if len(sec.XRefs) > 0 {
					entries = append(entries, entry{Ref: ref, XRefs: sec.XRefs})
				}
			})
			if err != nil {
				return err
			}
			if resolve {
				for _, e := range entries {
					for i := range e.XRefs {
						if e.XRefs[i].SourcePath == "" {
							continue
						}
						html, err := a.WOL().MarginalReference(ctx, e.XRefs[i].SourcePath)
						if err != nil {
							return fmt.Errorf("resolve %s: %w", e.XRefs[i].Citation, err)
						}
						e.XRefs[i].ResolvedHTML = html
					}
				}
			}
			if format == render.JSON {
				return a.WriteJSON(entries)
			}
			if len(entries) == 0 {
				return a.Write("No cross references found.")
			}
			var b strings.Builder
			for _, e := range entries {
				writeHeading(&b, format, e.Ref)
				for _, x := range e.XRefs {
					fmt.Fprintf(&b, "- %s\n", x.Citation)
					if x.ResolvedHTML != "" {
						body, err := render.Render(x.ResolvedHTML, format, render.Options{BaseURL: a.HTTP().Base.WOL})
						if err != nil {
							return err
						}
						fmt.Fprintf(&b, "\n%s\n\n", indent(strings.TrimSpace(body), "  "))
					}
				}
				b.WriteString("\n")
			}
			return a.Write(b.String())
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
			var items []model.Result
			err := forEachStudySection(cmd.Context(), a, args, func(ref string, sec model.StudySection) {
				for _, m := range sec.Media {
					title := m.Caption
					if title == "" {
						title = m.Alt
					}
					if title == "" {
						title = m.URL
					}
					items = append(items, model.Result{
						Kind: "image", Title: title, Context: ref,
						FileURL: m.URL, ImageURL: m.URL, JWLink: m.FinderLink,
					})
				}
			})
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
			return writeListing(a, rs, "Media on "+strings.Join(args, " "))
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
that discuss it. With -x|--excerpts the referenced passage of each
publication is fetched and shown, including a link to the full article.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			format, err := a.Format()
			if err != nil {
				return err
			}
			type entry struct {
				Ref   string               `json:"ref"`
				Items []model.ResearchItem `json:"items"`
			}
			var entries []entry
			err = forEachStudySection(ctx, a, args, func(ref string, sec model.StudySection) {
				if len(sec.Research) > 0 {
					entries = append(entries, entry{Ref: ref, Items: sec.Research})
				}
			})
			if err != nil {
				return err
			}
			if excerpts {
				for _, e := range entries {
					for i := range e.Items {
						if e.Items[i].PCPath == "" {
							continue
						}
						tip, err := a.WOL().Tooltip(ctx, e.Items[i].PCPath)
						if err != nil {
							continue // excerpts are best effort
						}
						e.Items[i].ExcerptHTML = tip.ContentHTML
						if e.Items[i].ArticleURL == "" {
							e.Items[i].ArticleURL = tip.URL
						}
						if e.Items[i].Title == "" {
							e.Items[i].Title = tip.Title
						}
					}
				}
			}
			if format == render.JSON {
				return a.WriteJSON(entries)
			}
			if len(entries) == 0 {
				return a.Write("No research guide entries found.")
			}
			var b strings.Builder
			for _, e := range entries {
				writeHeading(&b, format, e.Ref)
				for _, it := range e.Items {
					fmt.Fprintf(&b, "- %s", it.Title)
					if it.Source != "" {
						fmt.Fprintf(&b, " (%s)", it.Source)
					}
					b.WriteString("\n")
					if it.ExcerptHTML != "" {
						body, err := render.Render(it.ExcerptHTML, format, render.Options{BaseURL: a.HTTP().Base.WOL})
						if err == nil {
							fmt.Fprintf(&b, "\n%s\n", indent(strings.TrimSpace(body), "  "))
						}
					}
					if link := firstNonEmpty(it.ArticleURL, it.PCPath); link != "" {
						fmt.Fprintf(&b, "  <%s>\n", link)
					}
					b.WriteString("\n")
				}
			}
			return a.Write(b.String())
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
			t := bookTable(cmd.Context(), a)
			format, err := a.Format()
			if err != nil {
				return err
			}
			if format == render.JSON {
				type book struct {
					Number int    `json:"number"`
					Name   string `json:"name"`
				}
				var books []book
				for i := 1; i <= 66; i++ {
					books = append(books, book{i, t.Name(i)})
				}
				return a.WriteJSON(books)
			}
			var b strings.Builder
			for i := 1; i <= 66; i++ {
				fmt.Fprintf(&b, "%2d. %s\n", i, t.Name(i))
			}
			return a.Write(b.String())
		},
	}
	return cmd
}

func writeHeading(b *strings.Builder, f render.Format, text string) {
	switch f {
	case render.Markdown:
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
