package service

import (
	"context"
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/unfold"
)

// glossarySections reads the same entries a page loads when a marked word is
// opened. The index must be ready even on the CLI's first run; MarkGlossary
// alone deliberately returns unmarked text while it loads in the background.
func (s *Service) glossarySections(ctx context.Context, lng model.Language, fragment string, level int, txt *i18n.Messages) []UnfoldSection {
	failed := func(err error) string {
		return "<p><em>" + html.EscapeString(fmt.Sprintf(txt.UnfoldFailed, failureCause(err))) + "</em></p>"
	}
	if _, err := s.Glossary(ctx, lng); err != nil {
		return []UnfoldSection{{Title: html.EscapeString(txt.GlossaryShort), Body: failed(err), Order: 90}}
	}
	marked := s.MarkGlossary(lng, `<div class="item-text">`+fragment+`</div>`)
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(marked))
	if err != nil {
		return nil
	}
	var sections []UnfoldSection
	seen := map[string]bool{}
	doc.Find("a.gloss[data-gloss]").Each(func(_ int, a *goquery.Selection) {
		key, _ := a.Attr("data-gloss")
		if seen[key] {
			return
		}
		seen[key] = true
		var ids []int
		for id := range strings.FieldsSeq(key) {
			if n, err := strconv.Atoi(id); err == nil {
				ids = append(ids, n)
			}
		}
		var entries []UnfoldSection
		_, err := s.StreamGlossary(ctx, lng, ids, UnfoldStream{Section: func(sec UnfoldSection) {
			sec.Body = demoteHeadings(sec.Body, level+1)
			entries = append(entries, sec)
		}})
		var body strings.Builder
		writeSections(&body, entries, level+1)
		if err != nil {
			body.WriteString(failed(err))
		}
		sections = append(sections, UnfoldSection{
			Title: html.EscapeString(collapseSpace(a.Text())), Body: body.String(), Order: 90,
		})
	})
	return sections
}

// loadUnfoldGlossary attaches entries after the citation tree is complete, so
// the entries are included at depth one without unfolding their own citations.
func loadUnfoldGlossary(ctx context.Context, resolver unfold.Resolver, groups [][]unfold.Node, cfg UnfoldConfig, txt *i18n.Messages) {
	r, ok := resolver.(*tooltipResolver)
	if !ok || !cfg.Glossary || cfg.Hops || cfg.Depth <= 0 {
		return
	}
	var visit func([]unfold.Node)
	visit = func(nodes []unfold.Node) {
		for i := range nodes {
			n := &nodes[i]
			if n.Err == nil && n.Ref.IsVerse() {
				var body strings.Builder
				writeSections(&body, r.s.glossarySections(ctx, r.lng, n.HTML, 1, txt), 1)
				n.Glossary = body.String()
			}
			visit(n.Children)
		}
	}
	for _, nodes := range groups {
		visit(nodes)
	}
}
