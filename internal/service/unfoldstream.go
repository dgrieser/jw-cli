package service

import (
	"context"
	"fmt"
	"html"
	"slices"
	"strings"

	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/bibleref"
	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/unfold"
)

// SectionLevel is the heading level a streamed section stands at. Its Body
// starts one below, so whatever it holds nests under it.
const SectionLevel = 2

// UnfoldSection is one piece of an expansion, handed out as soon as it is
// ready rather than once the whole expansion is: a web page shows the study
// notes of a verse while its references are still being resolved.
type UnfoldSection struct {
	// Title heads the section, already HTML.
	Title string
	// Body is what the section holds, HTML with its headings from
	// SectionLevel+1 down.
	Body string
	// Key names a section that others are streamed into; In names the section
	// this one goes into, empty for one of the top level.
	Key string
	In  string
	// Order is where the section belongs among the others of its level,
	// lowest first. Sections are handed out as they are ready, which is not
	// always the order they read in: a verse's handful of marginal references
	// is out long before its indexes, which can run to a hundred entries.
	Order int
}

// The order the sections of a verse read in, whenever they arrive: what it
// says about itself, what the indexes point at, what its own margin points at,
// and only then who else quotes it.
const (
	orderNotes    = 10
	orderIndexes  = 20
	orderMarginal = 50
	orderCited    = 60
)

// UnfoldStream is where a streamed expansion reports to. Section is required;
// Stage, told which part of the expansion is being worked on ("study",
// "references", "cited"), may be nil.
type UnfoldStream struct {
	Section func(UnfoldSection)
	Stage   func(stage string)
}

func (o UnfoldStream) stage(name string) {
	if o.Stage != nil {
		o.Stage(name)
	}
}

// The stages a stream reports.
const (
	StageStudy      = "study"
	StageReferences = "references"
	StageCited      = "cited"
)

// StreamVerseUnfold expands one verse the way jw bible read expands every verse
// it prints — study notes, the indexes, the marginal references, who quotes it —
// and hands out each of those as soon as it is ready. The marginal references
// come one at a time, into a section holding them all. verseID is the id wol
// gives the verse (book*1e6 + chapter*1e3 + verse), and edition the bible its
// marginal references are read from. The note it returns closes an expansion
// that was cut short, as ReadPassages' UnfoldNote does, and requests is what
// the expansion spent.
//
// Everything the verse brings is one expansion: one budget, weighed together
// with cfg.Spent, and one set of passages already expanded, however many
// pieces it is handed out in. What the first level will cost is asked about
// before any of it is spent.
func (s *Service) StreamVerseUnfold(ctx context.Context, lng model.Language, edition string, verseID int,
	cfg UnfoldConfig, txt *i18n.Messages, out UnfoldStream) (note string, requests int, err error) {
	ref := bibleref.Ref{
		Book: verseID / 1_000_000, Chapter: verseID / 1_000 % 1_000,
		VerseStart: verseID % 1_000, VerseEnd: verseID % 1_000,
	}
	if ref.Book < 1 || ref.Book > 66 || ref.Chapter < 1 || ref.VerseStart < 1 {
		return "", 0, fmt.Errorf("invalid verse id %d", verseID)
	}
	if edition == "" {
		edition = studyEdition
	}
	out.stage(StageStudy)
	doc, err := s.Chapter(ctx, lng, edition, ref)
	if err != nil {
		return "", 1, err
	}
	verses, err := doc.Verses(ref.VerseStart, ref.VerseEnd)
	if err != nil {
		return "", 1, err
	}
	// the chapter just read is the study pane as well when it is the study
	// edition, so the pane costs nothing more
	chapters := map[string]*wol.ChapterDoc{
		fmt.Sprintf("%s-%d-%d", edition, ref.Book, ref.Chapter): doc,
	}
	r := newTooltipResolver(s, lng, chapters).withCited(ctx, cfg.Cited)
	// the verse is what was asked about; the references reached through it are
	// not looked up, as jw bible read does not
	cfg.CitedDepth = 0
	sess := unfold.NewSession(r, unfoldOptions(cfg))
	// the chapter page, and whatever came before this verse
	sess.Spend(cfg.Spent + 1)
	spent := func() int { return sess.Requests() - cfg.Spent }
	table := s.BookTable(ctx, lng)
	verseRef := RefString(ref, table)

	var notes []string
	study, err := r.studyOf(ctx, ref)
	sess.Spend(study.Requests)
	if err != nil {
		if ctx.Err() != nil {
			return "", spent(), ctx.Err()
		}
		out.Section(UnfoldSection{
			Title: html.EscapeString(txt.StudyNotesHeading),
			Body: fmt.Sprintf("<p><em>%s</em></p>",
				html.EscapeString(fmt.Sprintf(txt.StudyFailed, err))),
			Order: orderNotes,
		})
	}
	if len(study.Notes) > 0 {
		var b strings.Builder
		writeStudyNotes(&b, unfold.Node{Notes: study.Notes}, SectionLevel, txt)
		out.Section(UnfoldSection{
			Title: html.EscapeString(txt.StudyNotesHeading), Body: afterHeading(b.String()), Order: orderNotes,
		})
	}

	// the marginal references first, one at a time, under a section naming the
	// verse: a handful of requests, where the indexes can take a hundred
	out.stage(StageReferences)
	research := map[string]bool{}
	for _, ref := range study.Research {
		research[ref.Path] = true
	}
	var marginal []unfold.Ref
	for _, ref := range unfold.Refs(verses[0].HTML) {
		if !research[ref.Path] {
			marginal = append(marginal, ref)
		}
	}
	// the whole first level at once — the margin, the indexes and the
	// quotations — so an expensive verse is asked about before any of it
	planned := sess.Cost(append(slices.Clone(marginal), study.Research...))
	if cfg.Cited {
		planned += citedCostEstimate
	}
	if ok, err := sess.Check(1, planned); err != nil || !ok {
		return joinNotes(notes), spent(), err
	}
	if len(marginal) > 0 {
		const key = "marginal"
		out.Section(UnfoldSection{
			Title: html.EscapeString(fmt.Sprintf(txt.MarginalReferencesOf, verseRef)),
			Key:   key, Order: orderMarginal,
		})
		for _, ref := range marginal {
			expanded, note, err := runSession(ctx, sess, []unfold.Group{{RootRefs: []unfold.Ref{ref}}}, txt)
			if err != nil {
				return joinNotes(notes), spent(), err
			}
			notes = appendNote(notes, note)
			for _, n := range expanded[0] {
				label := marginalLabel(n, txt)
				var b strings.Builder
				writeUnfoldNode(&b, n, SectionLevel, label, txt)
				out.Section(UnfoldSection{Title: html.EscapeString(label), Body: b.String(), In: key})
			}
		}
	}

	if len(study.Research) > 0 || len(study.Links) > 0 {
		var nodes []unfold.Node
		if len(study.Research) > 0 {
			// one run for both indexes, which is what drops a passage the two
			// of them point at alike
			expanded, note, err := runSession(ctx, sess, []unfold.Group{{RootRefs: study.Research}}, txt)
			if err != nil {
				return joinNotes(notes), spent(), err
			}
			nodes, notes = expanded[0], appendNote(notes, note)
		}
		for i, g := range indexGroups(study.Links, nodes, txt) {
			var b strings.Builder
			writeIndexGroup(&b, g, SectionLevel, txt)
			out.Section(UnfoldSection{Title: html.EscapeString(g.name), Body: b.String(), Order: orderIndexes + i})
		}
	}

	if cfg.Cited {
		// priced up front with the rest, and asked about again should the
		// references have spent more than they were priced at
		if ok, err := sess.Check(1, citedCostEstimate); err != nil || !ok {
			return joinNotes(notes), spent(), err
		}
		out.stage(StageCited)
		c := r.citedFor(ctx, []bibleref.Ref{ref}, "")
		sess.Spend(c.Requests)
		if len(c.Results) > 0 && c.Ref != "" {
			var b strings.Builder
			writeCitedItems(&b, c.Results, SectionLevel)
			out.Section(UnfoldSection{
				Title: html.EscapeString(fmt.Sprintf(txt.CitedInHeading, c.Ref)),
				Body:  b.String(), Order: orderCited,
			})
		}
	}
	return joinNotes(notes), spent(), ctx.Err()
}

// runSession runs one piece of a streamed expansion as part of its session, and
// words the note closing a piece that was cut short.
func runSession(ctx context.Context, sess *unfold.Session, groups []unfold.Group,
	txt *i18n.Messages) ([][]unfold.Node, string, error) {
	res, err := sess.Run(ctx, groups)
	if err != nil {
		return nil, "", err
	}
	return res.Nodes, stoppedNote(res.Stopped, res.Pending, txt), nil
}

// marginalLabel heads a marginal reference inside the section naming the verse
// it belongs to: the verse it points at is all there is left to say.
func marginalLabel(n unfold.Node, txt *i18n.Messages) string {
	if n.Ref.IsVerse() && n.Title != "" && isMarker(strings.TrimSpace(n.Ref.Text)) {
		return n.Title
	}
	return unfoldHeading(n, "", txt)
}

// CitationRef is one citation a web page asks to be unfolded: the wol path
// behind it and the text the document wrote it as.
type CitationRef struct {
	Path string
	Text string
}

// StreamRefsUnfold expands the citations of one block of a document — a
// paragraph of an article, a line of the meeting workbook — and hands out each
// one as soon as it is resolved, in the order given. The references a document
// writes are looked up for quotations as UnfoldArticle does. As for a verse,
// the citations are one expansion — one budget weighed together with
// cfg.Spent, one set of passages already expanded — and the first level of all
// of them is asked about before any is spent.
func (s *Service) StreamRefsUnfold(ctx context.Context, lng model.Language, refs []CitationRef,
	cfg UnfoldConfig, txt *i18n.Messages, out UnfoldStream) (note string, requests int, err error) {
	cfg.CitedDepth = 1
	r := newTooltipResolver(s, lng, nil).withCited(ctx, cfg.Cited)
	sess := unfold.NewSession(r, unfoldOptions(cfg))
	sess.Spend(cfg.Spent)
	spent := func() int { return sess.Requests() - cfg.Spent }
	var plan []unfold.Ref
	for _, c := range refs {
		if unfold.IsCitation(c.Path) {
			plan = append(plan, unfold.Ref{Path: c.Path, Text: collapseSpace(c.Text)})
		}
	}
	if ok, err := sess.Check(1, sess.Cost(plan)); err != nil || !ok {
		return "", 0, err
	}
	out.stage(StageReferences)
	var notes []string
	for i, ref := range plan {
		expanded, note, err := runSession(ctx, sess, []unfold.Group{{RootRefs: []unfold.Ref{ref}}}, txt)
		if err != nil {
			return joinNotes(notes), spent(), err
		}
		notes = appendNote(notes, note)
		for _, n := range expanded[0] {
			label := unfoldHeading(n, "", txt)
			var b strings.Builder
			writeUnfoldNode(&b, n, SectionLevel, label, txt)
			out.Section(UnfoldSection{Title: html.EscapeString(label), Body: b.String(), Order: i})
		}
	}
	return joinNotes(notes), spent(), ctx.Err()
}

// afterHeading drops the heading a writer opened its output with, for a
// section whose title is carried separately.
func afterHeading(fragment string) string {
	if !strings.HasPrefix(fragment, "<h") {
		return fragment
	}
	if i := strings.Index(fragment, "</h"); i >= 0 && len(fragment) >= i+5 {
		return fragment[i+5:]
	}
	return fragment
}

func appendNote(notes []string, note string) []string {
	if note == "" || slices.Contains(notes, note) {
		return notes
	}
	return append(notes, note)
}

func joinNotes(notes []string) string { return strings.Join(notes, " ") }
