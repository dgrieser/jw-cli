package cli

import (
	"bufio"
	"context"
	"fmt"
	"html"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	nethtml "golang.org/x/net/html"

	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/bibleref"
	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/render"
	"github.com/dgrieser/jw-cli/internal/unfold"
)

// unfoldThreshold is how many requests one level may need before the user is
// asked. At the rate the client paces wol.jw.org this is a couple of minutes of
// waiting — long enough to be worth confirming, while everything that finishes
// inside a minute goes through unremarked.
const unfoldThreshold = 2000

// studyEdition is the only edition that carries a study pane, matching what
// jw bible notes, xrefs and research read.
const studyEdition = "nwtsty"

// tooltipResolver adapts the wol client to unfold.Resolver, and reads the study
// pane of every verse an expansion touches.
type tooltipResolver struct {
	a     *app.App
	table *bibleref.Table
	// sections holds the study pane of a chapter, keyed edition-book-chapter.
	// Only the extracted sections are kept, not the chapter document they came
	// from: an expansion can touch dozens of chapters, and their parsed pages
	// would sit in memory for the whole run.
	sections map[string]map[int]model.StudySection
	// docs are chapter pages the caller already holds, borrowed rather than
	// fetched again. Same key as sections.
	docs map[string]*wol.ChapterDoc
}

func newTooltipResolver(a *app.App, docs map[string]*wol.ChapterDoc) *tooltipResolver {
	return &tooltipResolver{a: a, sections: map[string]map[int]model.StudySection{}, docs: docs}
}

func (r *tooltipResolver) Resolve(ctx context.Context, path string) (model.Tooltip, error) {
	return r.a.WOL().Tooltip(ctx, path)
}

// Study reads the study pane of the verse wol titled a citation with. wol titles
// a verse citation with the reference spelled out ("Acts 24:15"), which is what
// locates the chapter page the pane lives on; a title that is not a reference
// belongs to something other than a verse and has no pane to look for.
func (r *tooltipResolver) Study(ctx context.Context, title string) (unfold.Study, error) {
	if r.table == nil {
		r.table = bookTable(ctx, r.a)
	}
	refs, err := bibleref.Parse(title, r.table)
	if err != nil {
		return unfold.Study{}, nil
	}
	var out unfold.Study
	for _, ref := range refs {
		s, err := r.studyOf(ctx, ref)
		out.Requests += s.Requests
		if err != nil {
			return out, err
		}
		out.Notes = append(out.Notes, s.Notes...)
		out.Research = append(out.Research, s.Research...)
		out.Links = append(out.Links, s.Links...)
	}
	return out, nil
}

// studyOf collects the study material of every verse ref covers.
func (r *tooltipResolver) studyOf(ctx context.Context, ref bibleref.Ref) (unfold.Study, error) {
	key := fmt.Sprintf("%s-%d-%d", studyEdition, ref.Book, ref.Chapter)
	sections, ok := r.sections[key]
	var out unfold.Study
	if !ok {
		doc, borrowed := r.docs[key]
		if !borrowed {
			var err error
			doc, err = chapterFor(ctx, r.a, studyEdition, ref)
			out.Requests++
			if err != nil {
				return out, err
			}
		}
		sections = studySections(doc)
		r.sections[key] = sections
	}
	from, to := ref.VerseStart, ref.VerseEnd
	switch {
	case from == 0:
		// a whole chapter: every verse that has a pane at all
		from, to = 1, maxVerse(sections)
	case ref.RunsToChapterEnd():
		// the reference runs past this chapter, so it takes the pane of every
		// verse from its start on
		to = maxVerse(sections)
	}
	to = max(to, from)
	for v := from; v <= to; v++ {
		sec, ok := sections[v]
		if !ok {
			continue
		}
		out.Notes = append(out.Notes, sec.Notes...)
		for _, item := range sec.Research {
			if item.PCPath == "" {
				// a whole article rather than a passage: nothing to resolve
				out.Links = append(out.Links, item)
				continue
			}
			out.Research = append(out.Research, unfold.Ref{
				Text: researchLabel(item), Path: item.PCPath, Rank: researchRank(item),
			})
		}
	}
	return out, nil
}

// researchLabel is how a research-guide passage is cited. The entry's own line
// names the publication and where in it the passage sits, which is what a
// citation inside a document would say; the group it came from ("Research
// Guide") only stands in when there is no such line.
func researchLabel(item model.ResearchItem) string {
	if item.Title != "" {
		return item.Title
	}
	return item.Source
}

// researchRank is which of two index entries for the same passage is kept when
// the expansion finds they say the same thing: the research guide, which names
// the publication as a reader would cite it ("Einsichten, Band 1, S. 1044"),
// over the publications index citing it by symbol ("it-1 1044"). An entry from
// neither index sits between the two: it is nothing to prefer the symbol over.
func researchRank(item model.ResearchItem) int {
	switch item.Kind {
	case model.ResearchGuideItem:
		return 0
	case model.PublicationIndexItem:
		return 2
	}
	return 1
}

// longestChapter is how far to look for study sections when the chapter's own
// verses cannot be read: Psalm 119, the longest chapter there is.
const longestChapter = 176

// studySections extracts the study pane of every verse of a chapter at once, so
// the parsed page can be dropped afterwards.
func studySections(doc *wol.ChapterDoc) map[int]model.StudySection {
	out := map[int]model.StudySection{}
	last := longestChapter
	if verses, err := doc.Verses(0, 0); err == nil && len(verses) > 0 {
		last = verses[len(verses)-1].ID % 1000
	}
	for v := 1; v <= last; v++ {
		if sec, ok := doc.StudySection(v); ok {
			out[v] = sec
		}
	}
	return out
}

// maxVerse is the highest verse a chapter's study pane covers.
func maxVerse(sections map[int]model.StudySection) int {
	high := 0
	for v := range sections {
		high = max(high, v)
	}
	return high
}

// The heading level an expansion is appended at. A document carries its title as
// the only h1, so its references are an h2 section; a bible passage is itself an
// h2, and its references belong under it. A passage of several verses heads every
// verse at that level instead, so what a verse references sits under the verse
// and not under the passage.
const (
	documentUnfoldLevel = 2
	passageUnfoldLevel  = 3
	verseUnfoldLevel    = 4
)

// unfoldArticle expands the citations in art and returns the document body with
// the text behind every citation inlined under the block that cites it, the way
// jw bible read prints it under a verse. It comes back as HTML, so the whole
// thing goes through the same renderer — and gains the same hyperlinks, wrapping
// and styling — as the article itself.
func unfoldArticle(ctx context.Context, a *app.App, art model.Article, depth int, assumeYes bool) (string, error) {
	return unfoldInline(ctx, a, newTooltipResolver(a, nil), art.HTML, unfoldRequest{
		depth: depth, assumeYes: assumeYes,
	})
}

// blockTags are the elements an expansion is inlined under: the smallest piece of
// a document that reads as a unit of its own, so what a citation resolves to
// lands under the sentence that cites it rather than at the end of the page.
var blockTags = map[string]bool{
	"p": true, "li": true, "blockquote": true, "figcaption": true,
	"dd": true, "dt": true, "td": true, "th": true, "caption": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
}

// citingBlock is one such block: what it cites, and the heading level its
// expansion is written at — one below the section the block sits in, so the
// expansion reads as part of that section rather than taking it over.
type citingBlock struct {
	sel   *goquery.Selection
	level int
	refs  []citation
	// block says the citation sits in a block of its own. Without one there is
	// no place inside the text that an expansion could go without landing in
	// the middle of a sentence.
	block bool
}

// citation is one reference found in a block, together with the verse of that
// block it sits in when the block is bible text. A marginal reference is written
// as a bare "+", so the verse around it is the only thing that says where the
// reference came from.
type citation struct {
	ref     unfold.Ref
	chapter int
	verse   int
}

// verseSpan matches the id wol wraps a verse of a passage in, "v24-29-2-1":
// book, chapter, verse, segment.
var verseSpan = regexp.MustCompile(`^v(\d+)-(\d+)-(\d+)`)

// verseAt is the verse of the passage a citation sits in, zero when the text
// around it is not bible text.
func verseAt(s *goquery.Selection) (int, int) {
	id, ok := s.Closest("span.v").Attr("id")
	if !ok {
		return 0, 0
	}
	m := verseSpan.FindStringSubmatch(id)
	if m == nil {
		return 0, 0
	}
	chapter, _ := strconv.Atoi(m[2])
	verse, _ := strconv.Atoi(m[3])
	return chapter, verse
}

// citingBlocks finds the blocks of a document that cite something, in document
// order, with the citations of each. A citation belongs to the smallest block
// around it, and is listed once per document: a reference repeated further down
// is expanded where it first appears.
func citingBlocks(doc *goquery.Document) []citingBlock {
	var out []citingBlock
	at := map[*nethtml.Node]int{}
	seen := map[string]bool{}
	// nothing seen yet: the document's own title is the heading above it
	heading := documentUnfoldLevel - 1
	doc.Find("body *").Each(func(_ int, s *goquery.Selection) {
		name := goquery.NodeName(s)
		if level, ok := headingLevel(name); ok {
			heading = level
		}
		href, _ := s.Attr("href")
		if name != "a" || !unfold.IsCitation(href) || seen[href] {
			return
		}
		seen[href] = true
		block, isBlock := blockOf(s)
		chapter, verse := verseAt(s)
		cite := citation{
			ref:     unfold.Ref{Text: collapseSpace(s.Text()), Path: href},
			chapter: chapter,
			verse:   verse,
		}
		if i, ok := at[block.Nodes[0]]; ok {
			out[i].refs = append(out[i].refs, cite)
			return
		}
		at[block.Nodes[0]] = len(out)
		out = append(out, citingBlock{
			sel: block, level: heading + 1, refs: []citation{cite}, block: isBlock,
		})
	})
	return out
}

// headingLevel reads the level of a heading element, and reports whether the
// element is one at all.
func headingLevel(name string) (int, bool) {
	if len(name) != 2 || name[0] != 'h' || name[1] < '1' || name[1] > '6' {
		return 0, false
	}
	return int(name[1] - '0'), true
}

// blockOf is the smallest block a citation sits in. A citation in none — a
// passage that came back as bare text rather than as paragraphs — reports the
// citation itself, and that it is no block: a document still has to carry the
// expansion somewhere, while a passage can simply put it after itself.
func blockOf(s *goquery.Selection) (*goquery.Selection, bool) {
	for p := s.Parent(); p.Length() > 0 && goquery.NodeName(p) != "body"; p = p.Parent() {
		if blockTags[goquery.NodeName(p)] {
			return p, true
		}
	}
	return s, false
}

// inlineUnder puts an expansion where it reads as belonging to the block: inside
// a list item, since a heading between two items would land outside the list,
// and after any other block.
func inlineUnder(block *goquery.Selection, fragment string) {
	if goquery.NodeName(block) == "li" {
		block.AppendHtml(fragment)
		return
	}
	block.AfterHtml(fragment)
}

// collapseSpace is the citation text as a heading takes it, without the line
// breaks the document wrapped it in.
func collapseSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

// unfoldInline expands the citations of a document and returns its HTML with
// every expansion inlined under the block that cites it. What an expanded
// passage cites in turn keeps nesting under it, so every level is read where it
// belongs. One run for the whole document: the request budget is confirmed once,
// and a passage cited twice is expanded once, under the citation that came first.
func unfoldInline(ctx context.Context, a *app.App, r unfold.Resolver, fragment string,
	req unfoldRequest) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(fragment))
	if err != nil {
		return "", err
	}
	blocks := citingBlocks(doc)
	if len(blocks) == 0 {
		return fragment, nil
	}
	groups := make([]unfold.Group, len(blocks))
	for i, b := range blocks {
		groups[i] = unfold.Group{RootRefs: refsOf(b.refs)}
	}
	res, err := unfold.RunGroups(ctx, r, groups, unfoldOptions(a, req))
	if err != nil {
		return "", err
	}
	txt := a.Text()
	for i, b := range blocks {
		part := unfoldNodesHTML(res.Nodes[i], txt, b.level)
		if part == "" {
			continue
		}
		// the rule parts what the block brought from the document going on
		// after it
		inlineUnder(b.sel, part+"<hr/>")
	}
	body := doc.Find("body")
	if note := unfoldNoteHTML(stoppedNote(res.Stopped, res.Pending, txt)); note != "" {
		body.AppendHtml(note)
	}
	out, err := body.Html()
	if err != nil {
		return "", err
	}
	return dropTrailingRule(out), nil
}

// refsOf is what a block cites, for an expansion that only needs the references.
func refsOf(cites []citation) []unfold.Ref {
	out := make([]unfold.Ref, len(cites))
	for i, c := range cites {
		out[i] = c.ref
	}
	return out
}

// dropTrailingRule takes back the rule of the last expansion when the document
// ends there: it parts an expansion from what follows it, and nothing does.
func dropTrailingRule(s string) string {
	trimmed := strings.TrimRight(s, " \t\n")
	if rest, ok := strings.CutSuffix(trimmed, "<hr/>"); ok {
		return rest
	}
	return s
}

// unfoldBibleVerses expands what every verse of a passage references and returns
// the expansion of each, in the order of verses, so it can be printed under the
// verse it belongs to rather than after the whole passage: the verse's study
// notes, then the text behind the references it carries. A verse is nobody's
// citation, so its own study material is gathered up front: the notes are printed
// with it, and its research-guide passages go into the expansion as references of
// the verse itself, alongside the marginal references in its text. Chapter pages
// the caller already read are borrowed, so the study pane costs no second request
// for them.
//
// The note closing an expansion that was cut short is returned separately: it is
// about the run as a whole, not about one verse.
// level is the heading level the expansion of a verse is written at, which
// depends on whether the verses are headed one by one.
func unfoldBibleVerses(ctx context.Context, a *app.App, r *tooltipResolver, ref bibleref.Ref,
	verses []model.Verse, depth, level int, assumeYes bool) ([]string, string, error) {
	txt := a.Text()
	studies := make([]unfold.Study, len(verses))
	groups := make([]unfold.Group, len(verses))
	for i, v := range verses {
		num := v.ID % 1000
		study, err := r.studyOf(ctx, bibleref.Ref{
			Book: ref.Book, Chapter: ref.Chapter, VerseStart: num, VerseEnd: num,
		})
		if err != nil {
			return nil, "", err
		}
		studies[i] = study
		groups[i] = unfold.Group{Fragment: v.HTML, RootRefs: study.Research}
	}
	parts, note, err := unfoldFragments(ctx, a, r, groups, unfoldRequest{
		depth: depth, assumeYes: assumeYes, level: level,
	})
	if err != nil {
		return nil, "", err
	}
	out := make([]string, len(verses))
	for i := range verses {
		var b strings.Builder
		writeStudy(&b, unfold.Node{Notes: studies[i].Notes, Links: studies[i].Links}, level, txt)
		b.WriteString(parts[i])
		if b.Len() > 0 && i < len(verses)-1 {
			// the rule closes what the verse brought rather than opening it,
			// parting it from the verse that follows. Nothing follows the last
			// verse, so nothing needs parting from it either
			b.WriteString("<hr/>")
		}
		out[i] = b.String()
	}
	return out, note, nil
}

// unfoldRequest is what a command asks an expansion for.
type unfoldRequest struct {
	depth     int
	assumeYes bool
	// level is the heading level the expansion is written at. Inlined
	// expansions take it from the document instead.
	level int
}

// unfoldFragments expands several fragments in one run and returns the expansion
// of each, to be appended to the fragment it belongs to. One run rather than one
// per fragment: the request budget is confirmed once, and a passage cited by two
// fragments is expanded once.
func unfoldFragments(ctx context.Context, a *app.App, r unfold.Resolver, groups []unfold.Group,
	req unfoldRequest) ([]string, string, error) {
	txt := a.Text()
	res, err := unfold.RunGroups(ctx, r, groups, unfoldOptions(a, req))
	if err != nil {
		return nil, "", err
	}
	out := make([]string, len(groups))
	for i, nodes := range res.Nodes {
		out[i] = unfoldNodesHTML(nodes, txt, req.level)
	}
	return out, stoppedNote(res.Stopped, res.Pending, txt), nil
}

// unfoldOptions is what every expansion is run with: the confirmation of a level
// that costs a lot of requests, and the progress of the level being spent.
func unfoldOptions(a *app.App, req unfoldRequest) unfold.Options {
	txt := a.Text()
	return unfold.Options{
		Depth:     req.depth,
		Threshold: unfoldThreshold,
		Confirm:   unfoldConfirmer(a, req.assumeYes),
		Progress:  unfoldProgress(a, txt),
	}
}

// unfoldProgress reports a level being spent on one line of stderr — the
// document itself belongs to stdout — where every count overwrites the one
// before it and the last one is erased again, so what is left above the rendered
// output is nothing. Erased with spaces rather than an escape sequence, so a
// terminal that takes no escapes, or was asked for none, is left as clean as any
// other.
//
// A stderr that is not a terminal has nothing to overwrite, where the line would
// pile up once per request, so progress goes unreported there.
func unfoldProgress(a *app.App, txt *i18n.Messages) func(level, done, total int) {
	if !render.IsTerminal(a.Stderr) {
		return nil
	}
	return statusLine(a.Stderr, txt)
}

// statusLine is that line, on any writer.
func statusLine(w io.Writer, txt *i18n.Messages) func(level, done, total int) {
	written := 0
	return func(level, done, total int) {
		if done == total {
			// the level is through: take its line back
			fmt.Fprintf(w, "\r%s\r", strings.Repeat(" ", written))
			written = 0
			return
		}
		line := fmt.Sprintf(txt.UnfoldProgress, level, done, total)
		width := render.StringWidth(line)
		// a shorter count than the one before it would leave the tail of the
		// longer line standing
		fmt.Fprintf(w, "\r%s%s", line, strings.Repeat(" ", max(written-width, 0)))
		written = max(written, width)
	}
}

// unfoldConfirmer asks before a level that needs a lot of requests. There is
// nobody to ask when stdin is not a terminal, so a script has to say up front
// with --yes that the traffic is wanted.
func unfoldConfirmer(a *app.App, assumeYes bool) func(level, requests int) (bool, error) {
	if assumeYes {
		return nil
	}
	if !render.IsTerminal(os.Stdin) {
		return func(level, requests int) (bool, error) {
			return false, fmt.Errorf("unfolding level %d needs %d requests to wol.jw.org; "+
				"pass --yes to allow that without being asked", level, requests)
		}
	}
	// one reader for the whole run: a fresh one per level would throw away
	// anything already buffered
	in := bufio.NewReader(os.Stdin)
	return func(level, requests int) (bool, error) {
		fmt.Fprintf(a.Stderr, a.Text().UnfoldConfirm, level, requests)
		line, err := in.ReadString('\n')
		if err != nil {
			return false, nil // no answer: stop rather than spend the requests
		}
		return affirmative(line), nil
	}
}

// affirmative reports whether an answer to the unfold prompt means yes. Both
// catalog languages are accepted either way round, so a German prompt still
// takes "y" and an English one "j".
func affirmative(line string) bool {
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "j", "ja":
		return true
	}
	return false
}

// maxHeading is the deepest heading level markdown has. Past it, headingHTML
// falls back to bold text.
const maxHeading = 6

// headingHTML renders a heading at the given level. inner is already HTML.
// Beyond h6 there is no deeper heading, and clamping everything onto h6 would
// make separate nesting levels look identical — bold text keeps them apart while
// staying visibly subordinate to the last real heading.
func headingHTML(level int, inner string) string {
	if level > maxHeading {
		return "<p><strong>" + inner + "</strong></p>"
	}
	return fmt.Sprintf("<h%d>%s</h%d>", level, inner, level)
}

// unfoldNodesHTML renders one tier of expanded references under its own heading.
func unfoldNodesHTML(nodes []unfold.Node, txt *i18n.Messages, level int) string {
	if len(nodes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(headingHTML(level, html.EscapeString(txt.UnfoldHeading)))
	writeUnfoldNodes(&b, nodes, level+1, "", txt)
	return b.String()
}

// stoppedNote closes an expansion that did not do what was asked. Reaching the
// requested depth leaves references behind as well, but that is the expansion
// working as asked, so it says nothing.
func stoppedNote(stopped bool, pending int, txt *i18n.Messages) string {
	if stopped {
		return fmt.Sprintf(txt.UnfoldStopped, pending)
	}
	return ""
}

// unfoldNoteHTML is how such a note is printed: an aside under the expansion it
// belongs to. An empty note prints nothing.
func unfoldNoteHTML(note string) string {
	if note == "" {
		return ""
	}
	return fmt.Sprintf("<p><em>%s</em></p>", html.EscapeString(note))
}

// demoteHeadings pushes the headings inside an expanded passage below the
// reference that introduced it. A passage lifted out of an article brings that
// article's own headings along — a sidebar <h2>, say — and those would otherwise
// outrank the reference's heading and take the outline over from there down.
// Relative order is kept, so a passage with structure still reads as structured.
func demoteHeadings(fragment string, below int) string {
	if !strings.Contains(fragment, "<h") {
		return fragment
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(fragment))
	if err != nil {
		return fragment
	}
	body := doc.Find("body")
	body.Find("h1, h2, h3, h4, h5, h6").Each(func(_ int, s *goquery.Selection) {
		level, err := strconv.Atoi(strings.TrimPrefix(goquery.NodeName(s), "h"))
		if err != nil {
			return
		}
		inner, err := s.Html()
		if err != nil {
			return
		}
		s.ReplaceWithHtml(headingHTML(below+level, inner))
	})
	out, err := body.Html()
	if err != nil {
		return fragment
	}
	return out
}

// refSeparator joins the two halves of a reference heading. An arrow rather than
// a colon: every one of these headings is a pointer from one thing to another,
// and a colon reads as "label: value" instead.
const refSeparator = " → "

// unfoldHeading is the one-line label for an expanded reference: the citation as
// the document wrote it, then what the passage turned out to be —
// "6 Abs. 15 → Vertraue dem barmherzigen „Richter der ganzen Erde“".
//
// A verse is the exception. wol titles a verse citation with the same reference
// spelled out, so "Apg. 24:15: Apostelgeschichte 24:15" says one thing twice and
// only the citation is kept.
//
// A cross reference inside a verse is the other way round: the document writes it
// as a bare marker ("+") that names nothing at all. Neither end of it is in the
// text, so both are named — the passage the marker sits in and the one it points
// at: "Querverweis Apostelgeschichte 24:15 → Jesaja 26:19". At the top level
// there is no enclosing passage to name, and the label carries the target alone.
func unfoldHeading(n unfold.Node, source string, txt *i18n.Messages) string {
	// citation text carries the punctuation that joined it to its sentence
	// ("Joh. 5:29;")
	ref := strings.TrimRight(n.Ref.Text, ",;. ")
	if ref == "" || isMarker(ref) {
		switch {
		case n.Title == "":
			return ref
		case !n.Ref.IsVerse():
			return n.Title
		case source == "":
			return txt.MarginalReference + " " + n.Title
		}
		return fmt.Sprintf(txt.MarginalReferenceWithSource, source, n.Title)
	}
	if n.Ref.IsVerse() || n.Title == "" || saysIt(ref, n.Title) {
		return ref
	}
	return ref + refSeparator + n.Title
}

// saysIt reports whether the citation already names what the passage turned out
// to be, so the heading would say it twice. A research-guide entry cites an
// article by its own headline — “God So Loved the World”, The Watchtower,
// 7/1/2014 — which wol then answers with that headline again, in straight
// quotes or none.
func saysIt(citation, title string) bool {
	return strings.Contains(unquote(citation), unquote(title))
}

// unquote strips the typographic quotes and case a citation and a title can
// spell differently.
func unquote(s string) string {
	return strings.ToLower(strings.Map(func(r rune) rune {
		switch r {
		case '"', '\'', '“', '”', '„', '‘', '’', '«', '»':
			return -1
		}
		return r
	}, s))
}

// verseSource names the verse of a passage a reference was found in. The passage
// is named as a whole — "Jeremia 29:1-30:24" — while a marginal reference in it
// belongs to one of its verses, and saying the passage instead puts the same
// name on every reference the passage carries. The book comes from the passage's
// own name, so it stays in the language the passage is read in; a passage that
// is not bible text has no verse to name and keeps its name.
func verseSource(passage string, chapter, verse int) string {
	book := bookNameOf(passage)
	if book == "" || chapter == 0 || verse == 0 {
		return passage
	}
	return fmt.Sprintf("%s %d:%d", book, chapter, verse)
}

// bookNameOf is the book a bible citation names: everything before the chapter
// it goes on to name. Empty when the citation names no chapter, and so is not a
// bible citation at all.
func bookNameOf(citation string) string {
	fields := strings.Fields(citation)
	for i, f := range fields {
		if i > 0 && f != "" && f[0] >= '0' && f[0] <= '9' {
			return strings.Join(fields[:i], " ")
		}
	}
	return ""
}

// unfoldSource names a passage well enough to be cited as the origin of a cross
// reference found inside it. The resolved title is preferred over the citation
// text, so both ends of the reference are spelled out the same way rather than
// pairing an abbreviation with a full name.
func unfoldSource(n unfold.Node, label string) string {
	if n.Title != "" {
		return n.Title
	}
	return label
}

// isMarker reports whether s is only punctuation, and so says nothing about
// what it points at.
func isMarker(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// writeStudy renders the study material of an unfolded verse: the study notes,
// then the research-guide entries that name a whole article and so have no
// passage to unfold. Both belong to the verse above them and sit at the same
// level as what the verse cites, which follows them.
func writeStudy(b *strings.Builder, n unfold.Node, level int, txt *i18n.Messages) {
	if n.StudyErr != nil {
		fmt.Fprintf(b, "<p><em>%s</em></p>",
			html.EscapeString(fmt.Sprintf(txt.StudyFailed, n.StudyErr)))
	}
	if len(n.Notes) > 0 {
		b.WriteString(headingHTML(level, html.EscapeString(txt.StudyNotesHeading)))
		for _, note := range n.Notes {
			// a note is the inside of its paragraph, so it needs one of its own:
			// without it two notes run together into one
			b.WriteString("<p>" + note.HTML + "</p>")
		}
	}
	if len(n.Links) == 0 {
		return
	}
	b.WriteString(headingHTML(level, html.EscapeString(txt.ResearchHeading)))
	b.WriteString("<ul>")
	for _, item := range n.Links {
		// the same shape jw bible research prints: the title carries the link,
		// the publication line follows in parentheses
		fmt.Fprintf(b, `<li><a href="%s">%s</a>`,
			html.EscapeString(item.ArticleURL), html.EscapeString(item.Title))
		if item.Source != "" {
			fmt.Fprintf(b, " (%s)", html.EscapeString(item.Source))
		}
		b.WriteString("</li>")
	}
	b.WriteString("</ul>")
}

// writeUnfoldNodes renders one tier of an expansion. source names the passage
// these references were found in, so a bare marker can say where it came from;
// it is empty for the references of the document itself.
func writeUnfoldNodes(b *strings.Builder, nodes []unfold.Node, level int, source string, txt *i18n.Messages) {
	for _, n := range nodes {
		label := unfoldHeading(n, source, txt)
		b.WriteString(headingHTML(level, html.EscapeString(label)))
		// what the passage cites is read inside the passage, at the block citing
		// it; what it does not cite itself follows the passage
		rest := n.Children
		switch {
		case n.Err != nil:
			fmt.Fprintf(b, "<p><em>%s</em></p>",
				html.EscapeString(fmt.Sprintf(txt.UnfoldFailed, n.Err)))
		case strings.TrimSpace(n.HTML) != "":
			var content string
			content, rest = inlineChildren(demoteHeadings(n.HTML, level), rest,
				level+1, unfoldSource(n, label), txt)
			b.WriteString(content)
		}
		writeStudy(b, n, level+1, txt)
		writeUnfoldNodes(b, rest, level+1, unfoldSource(n, label), txt)
	}
}

// inlineChildren puts the expansion of every citation of a passage under the
// block of that passage which cites it, the way a document carries the
// expansions of its own citations, and returns the passage with them in place.
// The children it could not place come back to be read after the passage: a
// verse's research-guide passages are references of the verse rather than of
// anything its text says, so its text has nowhere to hold them.
func inlineChildren(content string, children []unfold.Node, level int, source string,
	txt *i18n.Messages) (string, []unfold.Node) {
	if len(children) == 0 {
		return content, nil
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(content))
	if err != nil {
		return content, children
	}
	blocks := citingBlocks(doc)
	if len(blocks) == 0 {
		return content, children
	}
	at := map[string]int{}
	for i, c := range children {
		at[c.Ref.Path] = i
	}
	placed := map[string]bool{}
	for _, block := range blocks {
		if !block.block {
			// nowhere inside the passage to put it: it goes after the passage,
			// as it did before
			continue
		}
		var sub strings.Builder
		for _, cite := range block.refs {
			i, ok := at[cite.ref.Path]
			if !ok || placed[cite.ref.Path] {
				// cited here but expanded elsewhere: at another block of this
				// passage, or not at all because the depth ran out
				continue
			}
			placed[cite.ref.Path] = true
			// one at a time: each names the verse it was found in, which is
			// what a marginal reference has instead of a citation text
			writeUnfoldNodes(&sub, []unfold.Node{children[i]}, level,
				verseSource(source, cite.chapter, cite.verse), txt)
		}
		if sub.Len() == 0 {
			continue
		}
		inlineUnder(block.sel, sub.String()+"<hr/>")
	}
	out, err := doc.Find("body").Html()
	if err != nil {
		return content, children
	}
	var rest []unfold.Node
	for _, c := range children {
		if !placed[c.Ref.Path] {
			rest = append(rest, c)
		}
	}
	return dropTrailingRule(out), rest
}
