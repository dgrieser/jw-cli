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

	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/render"
	"github.com/dgrieser/jw-cli/internal/unfold"
)

// unfoldThreshold is how many requests one level may need before the user is
// asked. wol is rate limited to about two requests a second, so fifty is a few
// seconds — past that a level is worth confirming.
const unfoldThreshold = 50

// tooltipResolver adapts the wol client to unfold.Resolver.
type tooltipResolver struct{ a *app.App }

func (r tooltipResolver) Resolve(ctx context.Context, path string) (model.Tooltip, error) {
	return r.a.WOL().Tooltip(ctx, path)
}

// unfoldArticle expands the citations in art and returns the document body with
// the expansion appended as HTML, so the whole thing goes through the same
// renderer — and gains the same hyperlinks, wrapping and styling — as the
// article itself.
func unfoldArticle(ctx context.Context, a *app.App, art model.Article, depth int, assumeYes bool) (string, error) {
	txt := a.Text()
	res, err := unfold.Run(ctx, tooltipResolver{a}, art.HTML, unfold.Options{
		Depth:     depth,
		Threshold: unfoldThreshold,
		Confirm:   unfoldConfirmer(a, assumeYes),
		Progress: func(level, done, total int) {
			// stderr: the document itself belongs to stdout
			fmt.Fprintf(a.Stderr, "\r%s", fmt.Sprintf(txt.UnfoldProgress, level, done, total))
			if done == total {
				fmt.Fprintln(a.Stderr)
			}
		},
	})
	if err != nil {
		return "", err
	}
	return art.HTML + unfoldHTML(res, txt), nil
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

// unfoldHTML renders an expansion as an HTML appendix: one heading per reference
// carrying its own content, nested one level deeper for what that content cited.
func unfoldHTML(res unfold.Result, txt *i18n.Messages) string {
	if len(res.Nodes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<hr/><h2>" + html.EscapeString(txt.UnfoldHeading) + "</h2>")
	writeUnfoldNodes(&b, res.Nodes, 3, txt)
	if note := unfoldNote(res, txt); note != "" {
		fmt.Fprintf(&b, "<p><em>%s</em></p>", html.EscapeString(note))
	}
	return b.String()
}

// unfoldNote closes an expansion that did not do what was asked. Reaching the
// requested depth leaves references behind as well, but that is the expansion
// working as asked, so it says nothing.
func unfoldNote(res unfold.Result, txt *i18n.Messages) string {
	if res.Stopped {
		return fmt.Sprintf(txt.UnfoldStopped, res.Pending)
	}
	return ""
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

// unfoldHeading is the one-line label for an expanded reference: the citation as
// the document wrote it, then what the passage turned out to be —
// "6 Abs. 15: Vertraue dem barmherzigen „Richter der ganzen Erde“".
//
// A verse is the exception. wol titles a verse citation with the same reference
// spelled out, so "Apg. 24:15: Apostelgeschichte 24:15" says one thing twice and
// only the citation is kept. A cross reference inside a verse is the other way
// round: it is written as a bare marker ("+", "*") that names nothing, so there
// the title has to stand in for it.
func unfoldHeading(n unfold.Node) string {
	// citation text carries the punctuation that joined it to its sentence
	// ("Joh. 5:29;")
	ref := strings.TrimRight(n.Ref.Text, ",;. ")
	if ref == "" || isMarker(ref) {
		if n.Title != "" {
			return n.Title
		}
		return ref
	}
	if n.Ref.IsVerse() || n.Title == "" || n.Title == ref {
		return ref
	}
	return ref + ": " + n.Title
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

func writeUnfoldNodes(b *strings.Builder, nodes []unfold.Node, level int, txt *i18n.Messages) {
	for _, n := range nodes {
		b.WriteString(headingHTML(level, html.EscapeString(unfoldHeading(n))))
		switch {
		case n.Err != nil:
			fmt.Fprintf(b, "<p><em>%s</em></p>",
				html.EscapeString(fmt.Sprintf(txt.UnfoldFailed, n.Err)))
		case strings.TrimSpace(n.HTML) != "":
			b.WriteString(demoteHeadings(n.HTML, level))
		}
		writeUnfoldNodes(b, n.Children, level+1, txt)
	}
}
