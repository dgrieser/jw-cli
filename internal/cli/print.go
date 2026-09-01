package cli

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/render"
	"github.com/dgrieser/jw-cli/internal/results"
)

// writeListing prints a numbered result list (or JSON with -o json) and saves
// it to the results cache so `jw show|open|download <index>` can act on it.
func writeListing(a *app.App, rs results.ResultSet, header string) error {
	// number before rendering; don't rely on Save's numbering reaching this
	// copy (it is skipped entirely when the cache dir is unavailable)
	for i := range rs.Items {
		rs.Items[i].Index = i + 1
	}
	if err := results.Save(a.Cache().Dir(), rs); err != nil {
		return err
	}
	format, err := a.Format()
	if err != nil {
		return err
	}
	if format == render.JSON {
		return a.WriteJSON(rs)
	}
	var b strings.Builder
	if header != "" {
		b.WriteString(header + "\n\n")
	}
	style := listStyleFor(a)
	for _, r := range rs.Items {
		b.WriteString(formatResult(r, style))
	}
	if len(rs.Items) == 0 {
		b.WriteString(a.Text().NoResults + "\n")
	}
	return a.Write(b.String())
}

// listIndent aligns a wrapped continuation line under a result's text.
const listIndent = "     "

// snippetMax caps the teaser line of a result, in runes. The APIs hand out
// whole passages; a listing row is not the place to print them.
const snippetMax = 300

// listStyle is how a listing renders the text the APIs hand it.
type listStyle struct {
	// inline controls the HTML fragments in titles, contexts and snippets.
	inline render.InlineOptions
	// width wraps long lines to the terminal; 0 leaves them on one line, which
	// keeps piped and redirected listings greppable.
	width int
	// noURLs leaves the link line off every entry (--no-urls). The index still
	// identifies the entry for jw show|open|download.
	noURLs bool
	// txt labels the metadata lines of an image row.
	txt *i18n.Messages
	// block renders a result's excerpt — the passage of the document it was
	// found in — as plain paragraphs. Nil leaves excerpts out.
	block func(html string) string
}

func listStyleFor(a *app.App) listStyle {
	s := listStyle{
		inline: render.InlineOptions{Emphasis: a.Styled()},
		noURLs: a.Flags.NoURLs,
		txt:    a.Text(),
	}
	if a.Styled() {
		s.width = a.Width()
	}
	// text, not markdown: a listing is plain text throughout, and writeListing
	// writes it with Write, so markdown would never be styled. --no-urls comes
	// in through RenderOptions.
	opts := a.RenderOptions(a.HTTP().Base.WOL)
	s.block = func(fragment string) string {
		out, err := render.Render(fragment, render.Text, opts)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(out)
	}
	return s
}

// wrap lays out one line of a result under the listing's indent.
func (s listStyle) wrap(text string) string {
	return render.WrapIndent(text, listIndent, s.width)
}

func formatResult(r model.Result, style listStyle) string {
	var b strings.Builder
	meta := []string{}
	if r.Duration != "" {
		meta = append(meta, r.Duration)
	}
	if r.Filesize > 0 {
		meta = append(meta, humanSize(r.Filesize))
	}
	title := render.Inline(r.Title, style.inline)
	if title == "" && r.Kind == "image" {
		title = style.imageFallbackTitle(r.Index)
	}
	if r.Context != "" {
		title += " — " + render.Inline(r.Context, style.inline)
	}
	if len(meta) > 0 {
		title += " (" + strings.Join(meta, ", ") + ")"
	}
	// the index prefix is wrapped along with the title, so a long title breaks
	// onto the listing's indent instead of the terminal's left edge
	fmt.Fprintf(&b, "%s\n", style.wrap(fmt.Sprintf("%3d. [%s] %s", r.Index, r.Kind, title)))
	// the excerpt is the passage the snippet was cut from, so it replaces it
	if excerpt := style.excerpt(r); excerpt != "" {
		b.WriteString(excerpt)
	} else if snippet := truncate(render.Inline(r.Snippet, style.inline), snippetMax); snippet != "" {
		fmt.Fprintf(&b, "%s%s\n", listIndent, style.wrap(snippet))
	}
	// what the image itself says: printed with or without --no-urls, since the
	// flag drops the target, not the picture's description
	for _, line := range style.imageMetaLines(r) {
		fmt.Fprintf(&b, "%s%s\n", listIndent, style.wrap(line))
	}
	// links stay on one line: a wrapped URL cannot be clicked or copied
	if link := preferredLink(r); link != "" && !style.noURLs {
		fmt.Fprintf(&b, "%s%s\n", listIndent, link)
	}
	return b.String()
}

// excerpt lays the passage a result was found in out under the listing indent,
// one wrapped paragraph at a time, framed by blank lines so the rows stay
// apart. Empty when the result carries none.
func (s listStyle) excerpt(r model.Result) string {
	if r.Excerpt == "" || s.block == nil {
		return ""
	}
	body := s.block(r.Excerpt)
	if body == "" {
		return ""
	}
	// line by line, so a table keeps its rows and a list its items; the
	// renderer already separates paragraphs with a blank line
	var b strings.Builder
	b.WriteString("\n")
	for line := range strings.SplitSeq(body, "\n") {
		if line = strings.TrimSpace(line); line == "" {
			b.WriteString("\n")
			continue
		}
		if !saysSomething(line) {
			continue // a list marker whose item was a picture, and the like
		}
		fmt.Fprintf(&b, "%s%s\n", listIndent, s.wrap(line))
	}
	b.WriteString("\n")
	return b.String()
}

// imageMetaLines labels the metadata of an image row, leaving out whatever
// already stands in the title.
func (s listStyle) imageMetaLines(r model.Result) []string {
	if r.Image == nil || s.txt == nil {
		return nil
	}
	im := *r.Image
	var out []string
	add := func(label, value string) {
		if value == "" || value == r.Title {
			return
		}
		out = append(out, label+": "+render.Inline(value, s.inline))
	}
	add(s.txt.LabelDescription, im.Description)
	add(s.txt.LabelAltText, im.Alt)
	add(s.txt.LabelCredit, im.Credit)
	if size := imageSize(im); size != "" {
		out = append(out, s.txt.LabelImageSize+": "+size)
	}
	return out
}

// imageDetailLines spells one image out for `jw show <n>` and the TUI detail
// pane: the title the listing showed, then every metadata line it printed
// underneath. Independent of --no-urls — the caller adds the URL, or does not.
func imageDetailLines(r model.Result, txt *i18n.Messages) []string {
	style := listStyle{txt: txt}
	title := r.Title
	if title == "" && r.Kind == "image" {
		title = style.imageFallbackTitle(r.Index)
	}
	return append([]string{title}, style.imageMetaLines(r)...)
}

// imageFallbackTitle names an image row that carries no words of its own.
func (s listStyle) imageFallbackTitle(index int) string {
	if s.txt == nil {
		return ""
	}
	return fmt.Sprintf(s.txt.ImageFallbackTitle, index)
}

// imageSize spells the pixel size out, or just the one side that is known.
func imageSize(im model.ImageMeta) string {
	switch {
	case im.Width > 0 && im.Height > 0:
		return fmt.Sprintf("%d×%d px", im.Width, im.Height)
	case im.Width > 0:
		return fmt.Sprintf("%d px", im.Width)
	case im.Height > 0:
		return fmt.Sprintf("%d px", im.Height)
	}
	return ""
}

// saysSomething reports whether a line carries anything to read: bullets,
// dashes and stray punctuation left over from an empty element do not.
func saysSomething(line string) bool {
	return strings.ContainsFunc(line, func(r rune) bool {
		return unicode.IsLetter(r) || unicode.IsDigit(r)
	})
}

// truncate cuts a line to max runes, marking that it was cut.
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimRight(string(r[:max]), " ") + "…"
}

func preferredLink(r model.Result) string {
	switch {
	case r.JWLink != "":
		return r.JWLink
	case r.WOLLink != "":
		return r.WOLLink
	case r.FileURL != "":
		return r.FileURL
	}
	return ""
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
