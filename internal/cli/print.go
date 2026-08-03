package cli

import (
	"fmt"
	"strings"

	"github.com/dgrieser/jw-cli/internal/app"
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

// listStyle is how a listing renders the text the APIs hand it.
type listStyle struct {
	// inline controls the HTML fragments in titles, contexts and snippets.
	inline render.InlineOptions
	// width wraps long lines to the terminal; 0 leaves them on one line, which
	// keeps piped and redirected listings greppable.
	width int
}

func listStyleFor(a *app.App) listStyle {
	s := listStyle{inline: render.InlineOptions{Emphasis: a.Styled()}}
	if a.Styled() {
		s.width = a.Width()
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
	if r.Context != "" {
		title += " — " + render.Inline(r.Context, style.inline)
	}
	if len(meta) > 0 {
		title += " (" + strings.Join(meta, ", ") + ")"
	}
	// the index prefix is wrapped along with the title, so a long title breaks
	// onto the listing's indent instead of the terminal's left edge
	fmt.Fprintf(&b, "%s\n", style.wrap(fmt.Sprintf("%3d. [%s] %s", r.Index, r.Kind, title)))
	if snippet := render.Inline(r.Snippet, style.inline); snippet != "" {
		fmt.Fprintf(&b, "%s%s\n", listIndent, style.wrap(snippet))
	}
	// links stay on one line: a wrapped URL cannot be clicked or copied
	if link := preferredLink(r); link != "" {
		fmt.Fprintf(&b, "%s%s\n", listIndent, link)
	}
	return b.String()
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
