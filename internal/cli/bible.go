package cli

import (
	"context"
	"fmt"
	htmlpkg "html"
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
	var (
		edition   string
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

Examples:
  jw bible read Matthew 24:14
  jw bible read "mt 24:3-14"
  jw bible read "Pr 8-9"
  jw bible read "Pr 8:30-9:6"
  jw bible read "Joh 3:16; Ro 5:8" -o text
  jw bible read "Psalm 83" --bible nwt
  jw bible read -l de "Matthäus 24:14"
  jw bible read John 3:16 --unfold 1`,
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
			var passages []readPassage
			chapters := map[string]*wol.ChapterDoc{}
			// one expander for every passage read, so they share the chapter
			// pages their study panes come from
			var expander *tooltipResolver
			if depth > 0 {
				expander = newTooltipResolver(a, chapters)
			}
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
				ref = resolveChapterEnd(ref, verses)
				p := readPassage{Ref: refString(ref, table)}
				var unfolded []string
				if depth > 0 {
					// several verses are headed one by one, so the expansion of a
					// verse reads as belonging to that verse rather than to the
					// passage; a single verse is already the passage heading
					level := passageUnfoldLevel
					if len(verses) > 1 {
						level = verseUnfoldLevel
					}
					unfolded, p.UnfoldNote, err = unfoldBibleVerses(ctx, a, expander, ref, verses, depth, level, assumeYes)
					if err != nil {
						return err
					}
				}
				for i, v := range verses {
					out := readVerse{Verse: v}
					if i < len(unfolded) {
						out.Unfold = unfolded[i]
						if len(verses) > 1 {
							num := v.ID % 1000
							out.Citation = refString(bibleref.Ref{
								Book: ref.Book, Chapter: ref.Chapter, VerseStart: num, VerseEnd: num,
							}, table)
						}
					}
					p.Verses = append(p.Verses, out)
				}
				passages = append(passages, p)
			}
			if format == render.JSON {
				return a.WriteJSON(passages)
			}
			var b strings.Builder
			for i, p := range passages {
				if i > 0 {
					b.WriteString("\n")
				}
				var html strings.Builder
				for _, run := range verseRuns(p.Verses) {
					if cite := runCitation(run, table); cite != "" {
						html.WriteString(headingHTML(passageUnfoldLevel, htmlpkg.EscapeString(cite)))
					}
					for _, v := range run {
						html.WriteString(v.HTML)
						html.WriteString(" ")
						html.WriteString(v.Unfold)
					}
				}
				html.WriteString(unfoldNoteHTML(p.UnfoldNote))
				body, err := render.Render(collapseRules(html.String()), format,
					a.RenderOptions(a.HTTP().Base.WOL))
				if err != nil {
					return err
				}
				switch format {
				case render.Markdown, render.Raw:
					fmt.Fprintf(&b, "## %s\n\n%s\n", p.Ref, body)
				case render.HTML:
					fmt.Fprintf(&b, "<h2>%s</h2>\n%s\n", p.Ref, body)
				default:
					fmt.Fprintf(&b, "%s\n\n%s\n", p.Ref, body)
				}
			}
			return a.WriteMarkdown(b.String())
		},
	}
	cmd.Flags().StringVarP(&edition, "bible", "b", "nwtsty", "bible edition: "+strings.Join(wol.BibleEditions, ", "))
	cmd.Flags().IntVar(&depth, "unfold", 0, "print the study notes and the text behind every reference, following references this many levels deep")
	cmd.Flags().BoolVarP(&assumeYes, "yes", "y", false, "do not ask before an unfold that needs many requests")
	return cmd
}

// readVerse is a verse as jw bible read prints it: the text, and with --unfold
// what the verse references expanded under it, so the study material of a verse
// is read where the verse is.
type readVerse struct {
	model.Verse
	// Unfold is the expansion of everything this verse references, as an HTML
	// appendix to its text.
	Unfold string `json:"unfold,omitempty"`
}

// readPassage is one reference read.
type readPassage struct {
	Ref    string      `json:"ref"`
	Verses []readVerse `json:"verses"`
	// UnfoldNote says an expansion was cut short, which is about the passage
	// rather than about one of its verses.
	UnfoldNote string `json:"unfoldNote,omitempty"`
}

// verseRuns groups the verses of a passage into the spans printed under one
// heading. A verse that brought something of its own stands alone, so what
// follows the verse reads as belonging to it; the verses around it that brought
// nothing are one span of plain text and are headed as the span they are —
// "Jeremiah 30:1, 2" rather than a heading over every verse of it.
func verseRuns(verses []readVerse) [][]readVerse {
	var runs [][]readVerse
	for i := 0; i < len(verses); {
		j := i + 1
		if verses[i].Unfold == "" {
			for j < len(verses) && verses[j].Unfold == "" {
				j++
			}
		}
		runs = append(runs, verses[i:j])
		i = j
	}
	return runs
}

// runCitation heads a span of verses. It is empty when the verses of the passage
// are not headed one by one at all: without an expansion under them there is
// nothing to tell apart, and the passage heading already says what they are.
func runCitation(run []readVerse, t *bibleref.Table) string {
	if len(run) == 0 || run[0].Citation == "" {
		return ""
	}
	if len(run) == 1 {
		return run[0].Citation
	}
	first, last := run[0].ID, run[len(run)-1].ID
	return refString(bibleref.Ref{
		Book:       first / 1_000_000,
		Chapter:    first / 1_000 % 1_000,
		VerseStart: first % 1_000,
		VerseEnd:   last % 1_000,
	}, t)
}

// refString renders a ref with the (possibly localized) book table.
func refString(r bibleref.Ref, t *bibleref.Table) string {
	name := t.Name(r.Book)
	switch {
	case r.VerseStart == 0:
		return fmt.Sprintf("%s %d", name, r.Chapter)
	case r.RunsToChapterEnd():
		// the chapter was not read here, so its last verse cannot be named
		return fmt.Sprintf("%s %d:%dff.", name, r.Chapter, r.VerseStart)
	case r.VerseEnd > r.VerseStart:
		return fmt.Sprintf("%s %d:%d-%d", name, r.Chapter, r.VerseStart, r.VerseEnd)
	default:
		return fmt.Sprintf("%s %d:%d", name, r.Chapter, r.VerseStart)
	}
}

// resolveChapterEnd names the last verse of a reference that was written as
// running past its chapter — "Pr 8:5-9:10" takes chapter 8 to its end — now that
// the chapter has been read and where it ends is known.
func resolveChapterEnd(ref bibleref.Ref, verses []model.Verse) bibleref.Ref {
	if !ref.RunsToChapterEnd() || len(verses) == 0 {
		return ref
	}
	ref.VerseEnd = verses[len(verses)-1].ID % 1000
	return ref
}

// forEachStudySection iterates the study sections of every verse in refs,
// together with the verse itself (zero value when its text is not on the page).
func forEachStudySection(ctx context.Context, a *app.App, args []string, fn func(ref string, verse model.Verse, sec model.StudySection)) error {
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
		// the verse text of the whole span, so every study section can be
		// printed with the verse it belongs to
		texts := map[int]model.Verse{}
		verses, err := doc.Verses(ref.VerseStart, ref.VerseEnd)
		if err != nil {
			return fmt.Errorf("%s: %w", refString(ref, table), err)
		}
		ref = resolveChapterEnd(ref, verses)
		from, to := ref.VerseStart, ref.VerseEnd
		for _, v := range verses {
			texts[v.ID%1000] = v
		}
		if from == 0 {
			// bound the scan to the chapter's real last verse
			from, to = 1, verses[len(verses)-1].ID%1000
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
			fn(label, texts[v], sec)
		}
	}
	return nil
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
			type entry struct {
				Ref   string            `json:"ref"`
				Verse model.Verse       `json:"verse"`
				Notes []model.StudyNote `json:"notes"`
			}
			var entries []entry
			err = forEachStudySection(cmd.Context(), a, args, func(ref string, verse model.Verse, sec model.StudySection) {
				if len(sec.Notes) > 0 {
					entries = append(entries, entry{Ref: ref, Verse: verse, Notes: sec.Notes})
				}
			})
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
			type entry struct {
				Ref   string           `json:"ref"`
				XRefs []model.CrossRef `json:"xrefs"`
			}
			var entries []entry
			err = forEachStudySection(ctx, a, args, func(ref string, _ model.Verse, sec model.StudySection) {
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
			var items []model.Result
			err := forEachStudySection(cmd.Context(), a, args, func(ref string, _ model.Verse, sec model.StudySection) {
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
			type entry struct {
				Ref   string               `json:"ref"`
				Verse model.Verse          `json:"verse"`
				Items []model.ResearchItem `json:"items"`
			}
			var entries []entry
			err = forEachStudySection(ctx, a, args, func(ref string, verse model.Verse, sec model.StudySection) {
				if len(sec.Research) > 0 {
					entries = append(entries, entry{Ref: ref, Verse: verse, Items: sec.Research})
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
