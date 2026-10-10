package service

import (
	"context"
	"fmt"
	htmlpkg "html"
	"strings"

	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/render"
)

// Outline is the outline of contents of a bible book in an edition. It is an
// aid to the reading, not the reading, so it is best effort: an edition without
// one, or a page that cannot be read, gives no headings.
func (s *Service) Outline(ctx context.Context, lng model.Language, edition string, book int) []model.OutlineItem {
	cfg, err := s.WOLConfig(ctx, lng)
	if err != nil {
		return nil
	}
	items, err := s.WOL.Outline(ctx, cfg, edition, book)
	if err != nil {
		return nil
	}
	return items
}

// placeOutline hands every heading to the first verse read that it covers. A
// heading that begins inside the passage stands where it begins; one that began
// before it and is still running stands ahead of the first verse, so a passage
// opened in the middle says what it is in the middle of. Headings covering none
// of the verses are left out.
func placeOutline(items []model.OutlineItem, verses []Verse) {
	for _, it := range items {
		for i := range verses {
			if verses[i].ID >= it.Start && verses[i].ID <= it.End {
				verses[i].Outline = append(verses[i].Outline, it)
				break
			}
		}
	}
}

// outlineHTML is the headings ahead of one verse, as a block that ends the
// paragraph of verses before it. Markdown gets a nested list, the one way of
// indenting a terminal renderer keeps; the plain text gets one line per heading,
// indented by depth; both mark the heading off from the verses around it.
func outlineHTML(items []model.OutlineItem, format render.Format) string {
	if len(items) == 0 {
		return ""
	}
	if format == render.Text {
		lines := make([]string, len(items))
		for i, it := range items {
			lines[i] = strings.Repeat("  ", it.Depth) + outlineItemHTML(it, false)
		}
		return `<p class="outline">` + strings.Join(lines, "<br>") + `</p>`
	}
	var b strings.Builder
	b.WriteString(`<ul class="outline">`)
	// nested relative to the shallowest heading here: a list cannot open one
	// level down without an item to hang under
	base := items[0].Depth
	for _, it := range items {
		base = min(base, it.Depth)
	}
	cur := 0
	for i, it := range items {
		level := 0
		if i > 0 {
			level = min(it.Depth-base, cur+1)
			if level > cur {
				b.WriteString("<ul>")
			} else {
				b.WriteString("</li>")
				for ; cur > level; cur-- {
					b.WriteString("</ul></li>")
				}
			}
		}
		cur = level
		b.WriteString("<li>" + outlineItemHTML(it, true))
	}
	b.WriteString("</li>")
	for ; cur > 0; cur-- {
		b.WriteString("</ul></li>")
	}
	b.WriteString("</ul>")
	return b.String()
}

// outlineItemHTML is one heading with the verses it covers after it.
func outlineItemHTML(it model.OutlineItem, emphasis bool) string {
	title := htmlpkg.EscapeString(it.Title)
	if emphasis {
		title = "<em>" + title + "</em>"
	}
	if it.Label == "" {
		return title
	}
	return title + " (" + htmlpkg.EscapeString(it.Label) + ")"
}

// FormatBookOutline renders a book's outline in the requested format: under
// its title, an outline of contents chapter by chapter ("Genesis 1"), an
// overview as the one list it is. JSON is not a rendering; callers handle it.
func FormatBookOutline(o BookOutline, bookName string, format render.Format, opts render.Options) (string, error) {
	var html strings.Builder
	html.WriteString("<h2>" + htmlpkg.EscapeString(o.Title) + "</h2>")
	group := func(items []model.OutlineItem) {
		if len(items) > 0 {
			html.WriteString(outlineHTML(items, format))
		}
	}
	if !o.ByChapter() {
		group(o.Items)
	} else {
		var run []model.OutlineItem
		chapter := 0
		for _, it := range o.Items {
			if ch := it.Start / 1_000 % 1_000; ch != chapter {
				group(run)
				run, chapter = nil, ch
				html.WriteString("<h3>" + htmlpkg.EscapeString(fmt.Sprintf("%s %d", bookName, ch)) + "</h3>")
			}
			run = append(run, it)
		}
		group(run)
	}
	return render.Render(html.String(), format, opts)
}
