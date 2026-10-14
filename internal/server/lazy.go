package server

import (
	"fmt"
	"html"
	"regexp"
	"slices"
	"strings"

	"github.com/dgrieser/jw-cli/internal/render"
	"github.com/dgrieser/jw-cli/internal/service"
)

// An expansion the server writes in one piece leaves some sections to load
// later — who quotes a verse, the other translations, every section of a
// verse at depth 0 — as a heading over a placeholder naming the /unfold/…
// address its body loads from (service.LazyClass). An HTML body keeps the
// placeholder for whoever shows it; markdown and text keep only what is
// written, which would leave the heading over a bare "…". The placeholders
// are therefore swapped for a word no renderer touches before rendering,
// and the word for the address in the format's own terms afterwards.

// lazyPlaceholder is a placeholder as service.writeSections writes it.
var lazyPlaceholder = regexp.MustCompile(`<p class="` + regexp.QuoteMeta(service.LazyClass) + `" data-lazy="([^"]*)">…</p>`)

// lazyMarks are the addresses taken out of what is being rendered, in order.
type lazyMarks []string

// lazyToken is the word the i-th address stands in for while rendering. It
// ends in a letter, so no token is the start of another.
func lazyToken(i int) string { return fmt.Sprintf("jwlazy%dx", i) }

// mark takes the addresses out of fragment, each placeholder becoming a
// paragraph of its token.
func (m *lazyMarks) mark(fragment string) string {
	if !strings.Contains(fragment, service.LazyClass) {
		return fragment
	}
	return lazyPlaceholder.ReplaceAllStringFunc(fragment, func(p string) string {
		*m = append(*m, html.UnescapeString(lazyPlaceholder.FindStringSubmatch(p)[1]))
		return "<p>" + lazyToken(len(*m)-1) + "</p>"
	})
}

// restore puts the addresses back into a body rendered in format: a link
// under the heading in markdown, the address itself in text, and with the
// URLs left out (urls=0) the "…" alone.
func (m lazyMarks) restore(body string, format render.Format, noURLs bool) string {
	for i, addr := range m {
		line := "… " + addr
		switch {
		case noURLs:
			line = "…"
		case format == render.Markdown:
			line = "[…](" + addr + ")"
		}
		body = strings.ReplaceAll(body, lazyToken(i), line)
	}
	return body
}

// markPassages is res with the addresses of its expansions taken out, for a
// body rendered as markdown or text; res itself is left as it is, since the
// passages also go out as they are.
func (m *lazyMarks) markPassages(res service.ReadResult) service.ReadResult {
	res.Passages = slices.Clone(res.Passages)
	for i, p := range res.Passages {
		p.Verses = slices.Clone(p.Verses)
		for j := range p.Verses {
			p.Verses[j].Unfold = m.mark(p.Verses[j].Unfold)
		}
		res.Passages[i] = p
	}
	return res
}
