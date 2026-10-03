package service

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"slices"
	"strconv"
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
	// Ref is what the page finds the section by when a link to what it holds
	// is followed: the path of the reference (RefPath), or a name of its own
	// (FootnotesRef, TranslationsRef).
	Ref string
	// Lazy names what the section loads once it is opened rather than now —
	// LazyTranslations, LazyCited — empty for a section that comes with its
	// body. Passage and Edition say which verses, in which bible, it loads for.
	Lazy    string
	Passage bibleref.Ref
	Edition string
	// Open shows the section opened rather than as a closed chip.
	Open bool
	// Base absolutizes the links of Body, when it came from somewhere else than
	// the library (an article on jw.org); empty is the library itself.
	Base string
}

// The names a page finds a verse's sections by, beside the paths of its
// references.
const (
	FootnotesRef    = "footnotes"
	TranslationsRef = "translations"
	// LazyTranslations is the Lazy of a verse's translations section.
	LazyTranslations = "translations"
	// LazyCited is the Lazy of a verse's citations: the search for the
	// publications quoting it is most of what a verse costs, so it runs only
	// once the section is opened.
	LazyCited = "cited"
)

// The order the sections of a verse read in, whenever they arrive: what it
// says about itself, what the indexes point at, what its own margin points at,
// and only then who else quotes it.
const (
	orderNotes        = 10
	orderFootnotes    = 15
	orderIndexes      = 20
	orderMarginal     = 50
	orderTranslations = 55
	orderCited        = 60
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
// it prints: StreamPassageUnfold of that verse. verseID is the id wol gives the
// verse (book*1e6 + chapter*1e3 + verse).
func (s *Service) StreamVerseUnfold(ctx context.Context, lng model.Language, edition string, verseID int,
	cfg UnfoldConfig, txt *i18n.Messages, out UnfoldStream) (note string, requests int, err error) {
	ref, err := verseRef(verseID)
	if err != nil {
		return "", 0, err
	}
	return s.StreamPassageUnfold(ctx, lng, edition, ref, cfg, txt, out)
}

// StreamPassageUnfold expands the verses of one chapter — a single verse, or a
// range of them — as jw bible read expands a verse: study notes, footnotes,
// the indexes, the marginal references, the other translations and who quotes
// it, each handed out as soon as it is ready, for all the verses together.
// The marginal references come one at a time, into a section holding them
// all. The other translations and the publications quoting the passage are
// handed out as headings only (Lazy), loaded once they are opened. edition is
// the bible the marginal references are read from. The note it returns closes
// an expansion that was cut short, as ReadPassages' UnfoldNote does, and
// requests is what the expansion spent.
//
// Everything the passage brings is one expansion: one budget, weighed together
// with cfg.Spent, and one set of passages already expanded, however many
// pieces it is handed out in. What the first level will cost is asked about
// before any of it is spent.
func (s *Service) StreamPassageUnfold(ctx context.Context, lng model.Language, edition string, ref bibleref.Ref,
	cfg UnfoldConfig, txt *i18n.Messages, out UnfoldStream) (note string, requests int, err error) {
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
	var text strings.Builder
	for _, v := range verses {
		text.WriteString(v.HTML)
	}
	// the chapter just read is the study pane as well when it is the study
	// edition, so the pane costs nothing more
	chapters := map[string]*wol.ChapterDoc{
		fmt.Sprintf("%s-%d-%d", edition, ref.Book, ref.Chapter): doc,
	}
	r := newTooltipResolver(s, lng, chapters)
	// the passage is what was asked about; who quotes the references reached
	// through it is not looked up, as jw bible read does not
	cfg.Cited, cfg.CitedDepth = false, 0
	sess := unfold.NewSession(r, unfoldOptions(cfg))
	// the chapter page, and whatever came before this passage
	sess.Spend(cfg.Spent + 1)
	spent := func() int { return sess.Requests() - cfg.Spent }
	table := s.BookTable(ctx, lng)
	passageRef := RefString(ref, table)

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

	// the footnotes: a request each, and a verse has one or two
	body, n := footnotesHTML(ctx, r, footnoteLinks(text.String()))
	sess.Spend(n)
	if body != "" {
		out.Section(UnfoldSection{
			Title: html.EscapeString(txt.FootnotesHeading), Body: body,
			Ref: FootnotesRef, Order: orderFootnotes,
		})
	}

	// the other bibles of the language, loaded only once the section is opened
	if len(s.otherEditionsFor(ctx, lng, edition)) > 0 {
		out.Section(UnfoldSection{
			Title: html.EscapeString(txt.TranslationsHeading),
			Ref:   TranslationsRef, Lazy: LazyTranslations, Order: orderTranslations,
			Passage: ref, Edition: edition,
		})
	}
	// who quotes the passage, likewise: the search is most of what a verse
	// would cost otherwise
	out.Section(UnfoldSection{
		Title: html.EscapeString(fmt.Sprintf(txt.CitedInHeading, passageRef)),
		Lazy:  LazyCited, Passage: ref, Order: orderCited,
	})

	// the marginal references first, one at a time, under a section naming the
	// passage: a handful of requests, where the indexes can take a hundred
	out.stage(StageReferences)
	research := map[string]bool{}
	for _, ref := range study.Research {
		research[ref.Path] = true
	}
	var marginal []unfold.Ref
	for _, ref := range unfold.Refs(text.String()) {
		if !research[ref.Path] {
			marginal = append(marginal, ref)
		}
	}
	// the whole first level at once — the margin and the indexes — so an
	// expensive passage is asked about before any of it
	planned := sess.Cost(append(slices.Clone(marginal), study.Research...))
	if ok, err := sess.Check(1, planned); err != nil || !ok {
		return joinNotes(notes), spent(), err
	}
	if len(marginal) > 0 {
		const key = "marginal"
		out.Section(UnfoldSection{
			Title: html.EscapeString(fmt.Sprintf(txt.MarginalReferencesOf, passageRef)),
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
				out.Section(UnfoldSection{
					Title: html.EscapeString(label), Body: b.String(), In: key, Ref: RefPath(n.Ref.Path),
				})
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
	return joinNotes(notes), spent(), ctx.Err()
}

// StreamCited lists the publications quoting a passage, each as a section of
// its own heading the passage it quotes the verses in: what a page loads when
// the citations of a passage are opened.
func (s *Service) StreamCited(ctx context.Context, lng model.Language, refs []bibleref.Ref,
	out UnfoldStream) (int, error) {
	out.stage(StageCited)
	r := newTooltipResolver(s, lng, nil).withCited(ctx, true)
	c := r.citedFor(ctx, refs, "")
	for i, item := range c.Results {
		var b strings.Builder
		writeCitedItems(&b, []model.Result{item}, SectionLevel)
		title, body := splitHeading(b.String())
		out.Section(UnfoldSection{Title: title, Body: body, Order: i})
	}
	return c.Requests, ctx.Err()
}

// splitHeading takes the heading a writer opened its output with apart from
// what follows it: the inside of the heading, already HTML, and the rest.
func splitHeading(fragment string) (string, string) {
	if !strings.HasPrefix(fragment, "<h") {
		return "", fragment
	}
	open := strings.Index(fragment, ">")
	end := strings.Index(fragment, "</h")
	if open < 0 || end < open || len(fragment) < end+5 {
		return "", fragment
	}
	return fragment[open+1 : end], fragment[end+5:]
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
	// who quotes a cited verse is a section of its own, loaded once opened
	rcfg := cfg
	rcfg.Cited, rcfg.CitedDepth = false, 0
	r := newTooltipResolver(s, lng, nil)
	sess := unfold.NewSession(r, unfoldOptions(rcfg))
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
		// a verse, or a range of them, unfolds as it does in the bible: its
		// notes, the indexes, its marginal references, the other bibles and
		// who quotes it
		if ref.IsVerse() {
			tip, err := r.Resolve(ctx, ref.Path)
			sess.Spend(1)
			if passages := passageRefs(tip.ContentHTML); err == nil && len(passages) > 0 {
				note, err := s.streamCitedPassage(ctx, lng, i, ref, tip, passages, sess, cfg, txt, out)
				if err != nil {
					return joinNotes(notes), spent(), err
				}
				notes = appendNote(notes, note)
				continue
			}
		}
		expanded, note, err := runSession(ctx, sess, []unfold.Group{{RootRefs: []unfold.Ref{ref}}}, txt)
		if err != nil {
			return joinNotes(notes), spent(), err
		}
		notes = appendNote(notes, note)
		for _, n := range expanded[0] {
			label := unfoldHeading(n, "", txt)
			var b strings.Builder
			writeUnfoldNode(&b, n, SectionLevel, label, txt)
			out.Section(UnfoldSection{
				Title: html.EscapeString(label), Body: b.String(), Order: i, Ref: RefPath(n.Ref.Path),
			})
		}
	}
	return joinNotes(notes), spent(), ctx.Err()
}

// streamCitedPassage hands out a citation of bible text as a section holding
// the text, with what the bible unfolds its verses to streamed into it — the
// verses of each chapter it covers together, under a heading of their own
// when it covers more than one. The expansion runs on the budget of the
// citations' session.
func (s *Service) streamCitedPassage(ctx context.Context, lng model.Language, i int, ref unfold.Ref,
	tip model.Tooltip, passages []bibleref.Ref, sess *unfold.Session, cfg UnfoldConfig, txt *i18n.Messages,
	out UnfoldStream) (string, error) {
	n := unfold.Node{Ref: ref, Title: tip.Title, HTML: tip.ContentHTML, URL: tip.URL}
	label := unfoldHeading(n, "", txt)
	var b strings.Builder
	writeUnfoldNode(&b, n, SectionLevel, label, txt)
	key := fmt.Sprintf("verse%d", i)
	out.Section(UnfoldSection{
		Title: html.EscapeString(label), Body: b.String(), Order: i, Ref: RefPath(ref.Path), Key: key,
	})
	table := s.BookTable(ctx, lng)
	var notes []string
	for j, passage := range passages {
		into := key
		if len(passages) > 1 {
			into = fmt.Sprintf("%s-%d", key, j)
			out.Section(UnfoldSection{
				Title: html.EscapeString(RefString(passage, table)), In: key, Key: into, Order: j,
			})
		}
		pcfg := cfg
		pcfg.Spent = sess.Requests()
		note, requests, err := s.StreamPassageUnfold(ctx, lng, "", passage, pcfg, txt, UnfoldStream{
			Section: func(sec UnfoldSection) {
				// the passage's sections go into the citation's, and its own
				// groups are named apart from those of any other passage
				if sec.Key != "" {
					sec.Key = into + "-" + sec.Key
				}
				if sec.In != "" {
					sec.In = into + "-" + sec.In
				} else {
					sec.In = into
				}
				out.Section(sec)
			},
			Stage: out.Stage,
		})
		sess.Spend(requests)
		notes = appendNote(notes, note)
		if err != nil {
			return joinNotes(notes), err
		}
	}
	return joinNotes(notes), nil
}

// verseSpanID finds the ids verseSpan reads in a passage's markup.
var verseSpanID = regexp.MustCompile(`\bid="v(\d+)-(\d+)-(\d+)-\d+"`)

// passageRefs is the bible text a passage holds, a reference for the verses
// of each chapter it covers, in the order it reads; none for a passage that
// is not bible text.
func passageRefs(passage string) []bibleref.Ref {
	var out []bibleref.Ref
	for _, m := range verseSpanID.FindAllStringSubmatch(passage, -1) {
		book, _ := strconv.Atoi(m[1])
		chapter, _ := strconv.Atoi(m[2])
		verse, _ := strconv.Atoi(m[3])
		if n := len(out); n > 0 && out[n-1].Book == book && out[n-1].Chapter == chapter {
			out[n-1].VerseStart = min(out[n-1].VerseStart, verse)
			out[n-1].VerseEnd = max(out[n-1].VerseEnd, verse)
			continue
		}
		out = append(out, bibleref.Ref{Book: book, Chapter: chapter, VerseStart: verse, VerseEnd: verse})
	}
	return out
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
