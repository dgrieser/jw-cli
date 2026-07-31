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
	// titles and snippets arrive as HTML fragments from the search APIs
	inline := render.InlineOptions{Emphasis: a.Styled()}
	for _, r := range rs.Items {
		b.WriteString(formatResult(r, inline))
	}
	if len(rs.Items) == 0 {
		b.WriteString("No results.\n")
	}
	return a.Write(b.String())
}

func formatResult(r model.Result, inline render.InlineOptions) string {
	var b strings.Builder
	meta := []string{}
	if r.Duration != "" {
		meta = append(meta, r.Duration)
	}
	if r.Filesize > 0 {
		meta = append(meta, humanSize(r.Filesize))
	}
	title := render.Inline(r.Title, inline)
	if r.Context != "" {
		title += " — " + render.Inline(r.Context, inline)
	}
	if len(meta) > 0 {
		title += " (" + strings.Join(meta, ", ") + ")"
	}
	fmt.Fprintf(&b, "%3d. [%s] %s\n", r.Index, r.Kind, title)
	if snippet := render.Inline(r.Snippet, inline); snippet != "" {
		fmt.Fprintf(&b, "     %s\n", snippet)
	}
	if link := preferredLink(r); link != "" {
		fmt.Fprintf(&b, "     %s\n", link)
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
