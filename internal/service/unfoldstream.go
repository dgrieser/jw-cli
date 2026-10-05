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
	// It is the address its body streams from, without the language, which
	// the page adds; empty for a section that comes with its body.
	Lazy string
	// Open shows the section opened rather than as a closed chip.
	Open bool
	// Base absolutizes the links of Body, when it came from somewhere else than
	// the library (an article on jw.org); empty is the library itself.
	Base string
	// Unwrap hands out what the section holds without the section: the body
	// of a lazy section that already stands on the page with its heading.
	Unwrap bool
	// Doc is the document the section's passage was lifted out of, by docid;
	// zero when it is not known. A page leaves it out of the publications
	// quoting the verses inside the section.
	Doc int
}

// The parts of a passage's expansion a page can load on its own
// (PassageParts.Only).
const (
	PartNotes     = "notes"
	PartFootnotes = "footnotes"
	PartIndexes   = "indexes"
	PartMarginal  = "marginal"
)

// PassageParts narrows what StreamPassageUnfold hands out. The zero value is
// everything, loaded.
type PassageParts struct {
	// Only is the part to load: one of the Part constants, empty for all.
	Only string
	// Lazy hands out every other part as its heading only, loaded once it
	// is opened — what a page shows for a verse whose marginal reference was
	// followed before the verse was unfolded. Without it, Only is the body of
	// a section already on the page, handed out unwrapped.
	Lazy bool
	// Kind narrows Only=PartIndexes to one index, KindGuide or KindPubIndex;
	// empty is both.
	Kind string
}

// IsPart reports whether name is a part a passage can load on its own.
func IsPart(name string) bool {
	switch name {
	case PartNotes, PartFootnotes, PartIndexes, PartMarginal:
		return true
	}
	return false
}

// The names a page finds a verse's sections by, beside the paths of its
// references.
const (
	NotesRef        = "notes"
	FootnotesRef    = "footnotes"
	MarginalRef     = "marginal"
	TranslationsRef = "translations"
	CitedRef        = "cited"
)

// IndexRef names the index of a passage at position i.
func IndexRef(i int) string { return "index-" + strconv.Itoa(i) }

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

// add hands out a section that has something to show.
func (o UnfoldStream) add(sec UnfoldSection, ok bool) {
	if ok {
		o.Section(sec)
	}
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
// range of them — the way jw serve unfolds a verse, counted in bible references
// (UnfoldConfig.Hops) cfg.Depth deep: study notes, footnotes, the indexes and
// the marginal references, each with the verses it cites, handed out as soon
// as it is ready, for all the verses together. The marginal references come
// one at a time, into a section holding them all. The other translations and
// the publications quoting the passage are handed out as headings only
// (Lazy), loaded once they are opened. edition is the bible the marginal
// references are read from. The note it returns closes an expansion that was
// cut short, as ReadPassages' UnfoldNote does, and requests is what the
// expansion spent.
//
// Everything the passage brings is one expansion: one budget, weighed together
// with cfg.Spent, and one set of passages already expanded, however many
// pieces it is handed out in; the verses it shows go into cfg.Verses. What the
// first step will cost is asked about before any of it is spent.
func (s *Service) StreamPassageUnfold(ctx context.Context, lng model.Language, edition string, ref bibleref.Ref,
	cfg UnfoldConfig, txt *i18n.Messages, out UnfoldStream) (note string, requests int, err error) {
	return s.StreamPassageParts(ctx, lng, edition, ref, PassageParts{}, cfg, txt, out)
}

// StreamPassageParts is StreamPassageUnfold narrowed to the parts parts asks
// for: one of them alone, unwrapped into the section a page already shows for
// it, or one loaded and the others as headings loaded once opened.
func (s *Service) StreamPassageParts(ctx context.Context, lng model.Language, edition string, ref bibleref.Ref,
	parts PassageParts, cfg UnfoldConfig, txt *i18n.Messages, out UnfoldStream) (note string, requests int, err error) {
	cfg.Hops = true
	return s.streamPassageHops(ctx, lng, edition, ref, parts, cfg, SectionLevel, txt, out)
}

// StreamCited lists the publications quoting a passage, each as a section of
// its own heading the passage it quotes the verses in: what a page loads when
// the citations of a passage are opened.
//
// self are the documents the page read the passage from — an article citing
// it, the passage of another article that did: a publication is never listed
// as quoting a verse in the very document the reader followed it from.
func (s *Service) StreamCited(ctx context.Context, lng model.Language, refs []bibleref.Ref, self []int,
	out UnfoldStream) (int, error) {
	out.stage(StageCited)
	r := newTooltipResolver(s, lng, nil).withCited(ctx, true).excluding(self...)
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
// one as soon as it is resolved, in the order given, counted in bible
// references cfg.Depth deep: a verse the block cites unfolds as
// StreamPassageUnfold unfolds it, a passage of another publication brings the
// verses it cites one reference further down. A verse shown already
// (cfg.Verses) is not shown again. As for a verse, the citations are one
// expansion — one budget weighed together with cfg.Spent, one set of passages
// already expanded — and the first step of all of them is asked about before
// any is spent.
func (s *Service) StreamRefsUnfold(ctx context.Context, lng model.Language, refs []CitationRef,
	cfg UnfoldConfig, txt *i18n.Messages, out UnfoldStream) (note string, requests int, err error) {
	cfg.Hops = true
	if cfg.Verses == nil {
		cfg.Verses = unfold.NewVerses()
	}
	depth := max(cfg.Depth, 1)
	r := hopResolver(s, lng, nil)
	sess := unfold.NewSession(r, unfoldOptions(cfg))
	sess.Spend(cfg.Spent)
	spent := func() int { return sess.Requests() - cfg.Spent }
	var plan []unfold.Ref
	for _, c := range refs {
		if unfold.IsCitation(c.Path) {
			plan = append(plan, unfold.Ref{Path: c.Path, Text: collapseSpace(c.Text)})
		}
	}
	if ok, err := sess.Check(1, sess.CostHops(plan, depth)); err != nil || !ok {
		return "", 0, err
	}
	out.stage(StageReferences)
	var notes []string
	for i, ref := range plan {
		// a verse, or a range of them, unfolds as it does in the bible
		if ref.IsVerse() {
			tip, err := r.Resolve(ctx, ref.Path)
			sess.Spend(1)
			if passages := passageRefs(tip.ContentHTML); err == nil && len(passages) > 0 {
				if cfg.Verses.Shown(unfold.VerseIDs(tip.ContentHTML)) {
					continue
				}
				note, err := s.streamCitedPassage(ctx, lng, i, ref, tip, passages, sess, cfg, txt, out)
				if err != nil {
					return joinNotes(notes), spent(), err
				}
				notes = appendNote(notes, note)
				continue
			}
		}
		expanded, note, err := runSession(ctx, sess, []unfold.Group{{RootRefs: []unfold.Ref{ref}, Hops: depth}}, txt)
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
				Doc: wol.DocIDFromURL(n.URL),
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

func appendNote(notes []string, note string) []string {
	if note == "" || slices.Contains(notes, note) {
		return notes
	}
	return append(notes, note)
}

func joinNotes(notes []string) string { return strings.Join(notes, " ") }
