package service

import (
	"fmt"
	"html"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/dgrieser/jw-cli/internal/bibleref"
	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/unfold"
)

// The sections a passage of the bible unfolds to — its study notes, its
// footnotes, the indexes, its marginal references, the other translations and
// who quotes it — are made here, once, whichever way they go out: handed to a
// web page one at a time as they are ready (StreamPassageUnfold), or written
// in one piece under the verse (writeSections) for the CLI and a page the
// server unfolds itself. level is the heading level the section itself stands
// at; what it holds is headed from level+1 down.

// sectionList gathers the sections of a passage written in one piece.
type sectionList []UnfoldSection

// add keeps a section that has something to show.
func (l *sectionList) add(sec UnfoldSection, ok bool) {
	if ok {
		*l = append(*l, sec)
	}
}

// notesSection is the study pane's notes of a passage, or what kept the pane
// from being read.
func notesSection(notes []model.StudyNote, err error, level int, txt *i18n.Messages) (UnfoldSection, bool) {
	var b strings.Builder
	if err != nil {
		fmt.Fprintf(&b, "<p><em>%s</em></p>", html.EscapeString(fmt.Sprintf(txt.StudyFailed, err)))
	}
	for _, note := range notes {
		// a note is the inside of its paragraph, so it needs one of its own:
		// without it two notes run together into one
		b.WriteString("<p>" + note.HTML + "</p>")
	}
	if b.Len() == 0 {
		return UnfoldSection{}, false
	}
	return UnfoldSection{
		Title: html.EscapeString(txt.StudyNotesHeading), Body: b.String(), Order: orderNotes, Ref: NotesRef,
	}, true
}

// footnotesSection holds the footnotes of a passage, as footnotesHTML read them.
func footnotesSection(body string, txt *i18n.Messages) (UnfoldSection, bool) {
	if body == "" {
		return UnfoldSection{}, false
	}
	return UnfoldSection{
		Title: html.EscapeString(txt.FootnotesHeading), Body: body, Ref: FootnotesRef, Order: orderFootnotes,
	}, true
}

// indexSections are the study bible's indexes of a passage, one section each:
// the whole articles they name, and the passages they point at, unfolded.
func indexSections(links []model.ResearchItem, research []unfold.Node, level int,
	txt *i18n.Messages) []UnfoldSection {
	var out []UnfoldSection
	for i, g := range indexGroups(links, research, txt) {
		var b strings.Builder
		writeIndexGroup(&b, g, level, txt)
		out = append(out, UnfoldSection{
			Title: html.EscapeString(g.name), Body: b.String(), Order: orderIndexes + i, Ref: IndexRef(i),
		})
	}
	return out
}

// researchLinksSection lists the research guide's whole articles for a verse
// reached inside an expansion, whose indexes are not told apart.
func researchLinksSection(links []model.ResearchItem, txt *i18n.Messages) (UnfoldSection, bool) {
	if len(links) == 0 {
		return UnfoldSection{}, false
	}
	var b strings.Builder
	writeResearchLinks(&b, links, "")
	return UnfoldSection{
		Title: html.EscapeString(txt.ResearchHeading), Body: b.String(), Order: orderIndexes,
	}, true
}

// marginalKey is the group a passage's marginal references go into.
const marginalKey = "marginal"

// marginalSection heads the marginal references of a passage, which
// marginalEntry makes one by one.
func marginalSection(passage string, txt *i18n.Messages) UnfoldSection {
	return UnfoldSection{
		Title: html.EscapeString(fmt.Sprintf(txt.MarginalReferencesOf, passage)),
		Key:   marginalKey, Order: orderMarginal, Ref: MarginalRef,
	}
}

// marginalEntry is one marginal reference of a passage, unfolded. source names
// the passage in its heading, for a reader that does not see it nested under
// the passage's own; empty leaves it to the nesting.
func marginalEntry(n unfold.Node, source string, level int, txt *i18n.Messages) UnfoldSection {
	label := marginalLabel(n, txt)
	if source != "" {
		label = unfoldHeading(n, source, txt)
	}
	var b strings.Builder
	writeUnfoldNode(&b, n, level, label, txt)
	return UnfoldSection{
		Title: nodeTitle(n, label), Body: b.String(), In: marginalKey, Ref: RefPath(n.Ref.Path),
	}
}

// translationsSection is a passage as the other bibles of the language render
// it: the verses read, or — with none read and lazy set — where they load from
// once the section is opened.
func translationsSection(list []EditionVerse, lazy string, level int, txt *i18n.Messages) (UnfoldSection, bool) {
	sec := UnfoldSection{Title: html.EscapeString(txt.TranslationsHeading), Ref: TranslationsRef, Order: orderTranslations}
	switch {
	case len(list) > 0:
		var b strings.Builder
		for _, v := range list {
			b.WriteString(headingHTML(level+1, html.EscapeString(v.Label())))
			b.WriteString(paragraph(v.HTML))
		}
		sec.Body = b.String()
	case lazy != "":
		sec.Lazy = lazy
	default:
		return UnfoldSection{}, false
	}
	return sec, true
}

// citedSection is who quotes a passage: the publications the search found, or
// — when the search was left to the page — where they load from.
func citedSection(c unfold.Cited, level int, txt *i18n.Messages) (UnfoldSection, bool) {
	if c.Ref == "" || (len(c.Results) == 0 && c.Lazy == "") {
		return UnfoldSection{}, false
	}
	sec := UnfoldSection{
		Title: html.EscapeString(fmt.Sprintf(txt.CitedInHeading, c.Ref)), Lazy: c.Lazy, Order: orderCited,
		Ref: CitedRef,
	}
	if c.Lazy == "" {
		var b strings.Builder
		writeCitedItems(&b, c.Results, level)
		sec.Body = b.String()
	}
	return sec, true
}

// nodeCited is what the engine found quoting the verse a node resolved to.
func nodeCited(n unfold.Node) unfold.Cited {
	return unfold.Cited{Results: n.Cited, Total: n.CitedTotal, Ref: n.CitedRef, Lazy: n.CitedLazy}
}

// LazyClass marks the placeholder a lazy section written in one piece carries
// under its heading: data-lazy says where its body loads from, which a page
// folding the heading into a section moves onto it.
const LazyClass = "unfold-lazy"

// writeSections writes sections under the heading of what they belong to, in
// the order they read: each at level, the ones streamed into it (In) one
// below, in the order given. A lazy section leaves a placeholder saying where
// its body loads from.
func writeSections(b *strings.Builder, secs []UnfoldSection, level int) {
	top := slices.DeleteFunc(slices.Clone(secs), func(s UnfoldSection) bool { return s.In != "" })
	slices.SortStableFunc(top, func(a, b UnfoldSection) int { return a.Order - b.Order })
	for _, sec := range top {
		b.WriteString(headingHTML(level, sec.Title))
		if sec.Lazy != "" {
			fmt.Fprintf(b, `<p class="%s" data-lazy="%s">…</p>`, LazyClass, html.EscapeString(sec.Lazy))
		}
		b.WriteString(sec.Body)
		if sec.Key == "" {
			continue
		}
		for _, in := range secs {
			if in.In == sec.Key {
				b.WriteString(headingHTML(level+1, in.Title))
				b.WriteString(in.Body)
			}
		}
	}
}

// translationsURL is where a page loads a passage in the other bibles of the
// language from, once its translations are opened.
func translationsURL(ref bibleref.Ref, edition string) string {
	q := passageQuery([]bibleref.Ref{ref})
	q.Set("bible", edition)
	return "/unfold/translations?" + q.Encode()
}

// citedURL is where a page loads the publications quoting refs from, once
// their citations are opened; self are the documents to leave out of them.
func citedURL(refs []bibleref.Ref, self ...int) string {
	q := passageQuery(refs)
	for _, id := range self {
		q.Add("self", strconv.Itoa(id))
	}
	return "/unfold/cited?" + q.Encode()
}

// passageQuery names passages the way /unfold/translations and /unfold/cited
// read them: the wol id of the first verse of each, and the number of its
// last (zero for a single verse).
func passageQuery(refs []bibleref.Ref) url.Values {
	q := url.Values{}
	for _, ref := range refs {
		from := max(ref.VerseStart, 1)
		q.Add("vid", strconv.Itoa(ref.Book*1_000_000+ref.Chapter*1_000+from))
		to := 0
		switch {
		case ref.VerseStart == 0:
			to = bibleref.LastVerse // a whole chapter
		case ref.VerseEnd > from:
			to = min(ref.VerseEnd, bibleref.LastVerse)
		}
		q.Add("to", strconv.Itoa(to))
	}
	return q
}
