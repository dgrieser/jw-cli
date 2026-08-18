package cli

import (
	"bufio"
	"context"
	"fmt"
	"html"
	"os"
	"strconv"
	"strings"
	"unicode"

	"github.com/PuerkitoBio/goquery"

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
	if from == 0 {
		// a whole chapter: every verse that has a pane at all
		from, to = 1, maxVerse(sections)
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
			out.Research = append(out.Research, unfold.Ref{Text: researchLabel(item), Path: item.PCPath})
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
// the expansion appended as HTML, so the whole thing goes through the same
// renderer — and gains the same hyperlinks, wrapping and styling — as the
// article itself.
func unfoldArticle(ctx context.Context, a *app.App, art model.Article, depth int, assumeYes bool) (string, error) {
	appendix, err := unfoldFragment(ctx, a, newTooltipResolver(a, nil), art.HTML, unfoldRequest{
		depth: depth, assumeYes: assumeYes, level: documentUnfoldLevel,
	})
	if err != nil {
		return "", err
	}
	return art.HTML + appendix, nil
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
		out[i] = b.String()
	}
	return out, note, nil
}

// unfoldRequest is what a command asks an expansion for.
type unfoldRequest struct {
	depth     int
	assumeYes bool
	// level is the heading level the appendix is written at.
	level int
	// rootRefs are references of the fragment itself rather than of a citation
	// inside it, for a single fragment. Grouped expansions carry them per group
	// instead.
	rootRefs []unfold.Ref
}

// unfoldFragment expands the citations in an HTML fragment and returns the
// expansion as an HTML appendix, to be appended to the fragment before it is
// rendered.
func unfoldFragment(ctx context.Context, a *app.App, r unfold.Resolver, fragment string, req unfoldRequest) (string, error) {
	o := unfoldOptions(a, req)
	o.RootRefs = req.rootRefs
	res, err := unfold.Run(ctx, r, fragment, o)
	if err != nil {
		return "", err
	}
	return unfoldHTMLAt(res, a.Text(), req.level), nil
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
		Progress: func(level, done, total int) {
			// stderr: the document itself belongs to stdout
			fmt.Fprintf(a.Stderr, "\r%s", fmt.Sprintf(txt.UnfoldProgress, level, done, total))
			if done == total {
				fmt.Fprintln(a.Stderr)
			}
		},
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

// unfoldHTML renders an expansion as an HTML appendix under a document.
func unfoldHTML(res unfold.Result, txt *i18n.Messages) string {
	return unfoldHTMLAt(res, txt, documentUnfoldLevel)
}

// unfoldHTMLAt renders an expansion as an HTML appendix at the given heading
// level: one heading per reference carrying its own content, nested one level
// deeper for what that content cited.
func unfoldHTMLAt(res unfold.Result, txt *i18n.Messages, level int) string {
	body := unfoldNodesHTML(res.Nodes, txt, level)
	if body == "" {
		return ""
	}
	return body + unfoldNoteHTML(stoppedNote(res.Stopped, res.Pending, txt))
}

// unfoldNodesHTML renders one tier of expanded references under its own heading.
func unfoldNodesHTML(nodes []unfold.Node, txt *i18n.Messages, level int) string {
	if len(nodes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<hr/>" + headingHTML(level, html.EscapeString(txt.UnfoldHeading)))
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
		switch {
		case n.Err != nil:
			fmt.Fprintf(b, "<p><em>%s</em></p>",
				html.EscapeString(fmt.Sprintf(txt.UnfoldFailed, n.Err)))
		case strings.TrimSpace(n.HTML) != "":
			b.WriteString(demoteHeadings(n.HTML, level))
		}
		writeStudy(b, n, level+1, txt)
		writeUnfoldNodes(b, n.Children, level+1, unfoldSource(n, label), txt)
	}
}
