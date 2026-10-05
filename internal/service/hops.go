package service

import (
	"context"
	"fmt"
	"html"
	"strings"

	"github.com/dgrieser/jw-cli/internal/api/wol"
	"github.com/dgrieser/jw-cli/internal/bibleref"
	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/unfold"
)

// An expansion counted in bible references (UnfoldConfig.Hops) is how jw serve
// unfolds. At depth N a verse brings, at once, its study notes, its footnotes,
// the passages its research guide and publications index point at, and its
// marginal references — and every bible verse any of them cites, as a verse at
// depth N-1. A verse at depth 0 is its text, with all of its sections headings
// that load once opened. The other translations and who quotes a verse load
// once opened at any depth. A passage of another publication is no step of its
// own: it brings the verses it cites at the depth of what cites it, and nothing
// else it cites. A verse is shown once, where it is first reached.

// The kinds of index a passage's indexes part can load alone
// (PassageParts.Kind).
const (
	KindGuide    = "guide"
	KindPubIndex = "pubindex"
)

// IsIndexKind reports whether kind names one of the study bible's indexes.
func IsIndexKind(kind string) bool { return kind == KindGuide || kind == KindPubIndex }

// indexKind is the index a research entry of the given rank was listed under:
// the publications index, or the research guide for everything else.
func indexKind(rank int) string {
	if rank == researchRank(model.ResearchItem{Kind: model.PublicationIndexItem}) {
		return KindPubIndex
	}
	return KindGuide
}

// hopPartURL is where a page loads one part of a verse's expansion from once
// its heading is opened, one reference deep. kind narrows the indexes to one.
func hopPartURL(ref bibleref.Ref, part, kind string) string {
	q := passageQuery([]bibleref.Ref{ref})
	q.Set("bible", studyEdition)
	q.Set("part", part)
	if kind != "" {
		q.Set("kind", kind)
	}
	q.Set("depth", "1")
	return "/unfold/verse?" + q.Encode()
}

// lazyVerseSections are the sections of a verse whose expansion is left to the
// page: every one of them a heading that loads once opened, and a page drops
// the ones that turn out to have nothing. title names the verse in the
// headings that say whose they are; text is the verse itself, which tells
// whether it has footnotes.
func lazyVerseSections(refs []bibleref.Ref, title, text string, cited unfold.Cited, txt *i18n.Messages) []UnfoldSection {
	if len(refs) == 0 {
		return nil
	}
	ref := refs[0]
	if cited.Lazy == "" {
		cited = unfold.Cited{Ref: title, Lazy: citedURL(refs)}
	}
	secs := []UnfoldSection{
		{Title: html.EscapeString(txt.StudyNotesHeading), Order: orderNotes, Lazy: hopPartURL(ref, PartNotes, "")},
	}
	if len(footnoteLinks(text)) > 0 {
		secs = append(secs, UnfoldSection{
			Title: html.EscapeString(txt.FootnotesHeading), Order: orderFootnotes,
			Lazy: hopPartURL(ref, PartFootnotes, ""),
		})
	}
	return append(secs,
		UnfoldSection{
			Title: html.EscapeString(txt.ResearchGuideHeading), Order: orderIndexes,
			Lazy: hopPartURL(ref, PartIndexes, KindGuide),
		},
		UnfoldSection{
			Title: html.EscapeString(txt.PublicationsIndexHeading), Order: orderIndexes + 1,
			Lazy: hopPartURL(ref, PartIndexes, KindPubIndex),
		},
		UnfoldSection{
			Title: html.EscapeString(fmt.Sprintf(txt.MarginalReferencesOf, title)), Order: orderMarginal,
			Lazy: hopPartURL(ref, PartMarginal, ""),
		},
		UnfoldSection{
			Title: html.EscapeString(txt.TranslationsHeading), Order: orderTranslations,
			Lazy: translationsURL(ref, studyEdition),
		},
		UnfoldSection{
			Title: html.EscapeString(fmt.Sprintf(txt.CitedInHeading, cited.Ref)), Order: orderCited,
			Lazy: cited.Lazy,
		},
	)
}

// hopResolver is the resolver of an expansion counted in bible references:
// who quotes a verse it reaches is where the page loads that from, with the
// documents self left out of it.
func hopResolver(s *Service, lng model.Language, docs map[string]*wol.ChapterDoc, self ...int) *tooltipResolver {
	r := newTooltipResolver(s, lng, docs).excluding(self...)
	r.cited, r.lazyCited = true, true
	return r
}

// verseTitle is how a verse node names itself in its headings: wol's own title
// for it, without the punctuation a citation trails.
func verseTitle(n unfold.Node) string {
	return strings.TrimRight(firstNonBlank(n.Title, n.Ref.Text), ",;. ")
}

// writeHopVerse renders what follows the heading of a verse of an expansion
// counted in bible references, written at level: its text, then its sections
// one level below.
func writeHopVerse(b *strings.Builder, n unfold.Node, level int, txt *i18n.Messages) {
	b.WriteString(demoteHeadings(n.HTML, level))
	writeSections(b, hopVerseSections(n, level+1, txt), level+1)
}

// hopVerseSections are the sections of a verse node at level: all of them to
// load once opened when it has no references left, its material otherwise.
func hopVerseSections(n unfold.Node, level int, txt *i18n.Messages) []UnfoldSection {
	refs := passageRefs(n.HTML)
	title := verseTitle(n)
	// who quotes it, as the resolver says it loads — with the documents the
	// expansion is read from left out — or else as the verse names itself
	cited := nodeCited(n)
	if cited.Lazy == "" && len(refs) > 0 {
		cited = unfold.Cited{Ref: title, Lazy: citedURL(refs)}
	}
	if n.Parts.Lazy {
		return lazyVerseSections(refs, title, n.HTML, cited, txt)
	}
	var secs sectionList
	secs.add(hopNotesSection(n.Notes, n.StudyErr, n.Parts.NoteRefs, level, txt))
	secs.add(hopFootnotesSection(n.Parts.Footnotes, level, txt))
	secs = append(secs, indexSections(n.Links, n.Parts.Research, level, txt)...)
	if len(n.Children) > 0 {
		secs = append(secs, marginalSection(title, txt))
		for _, c := range n.Children {
			secs = append(secs, marginalEntry(c, "", level+1, txt))
		}
	}
	if len(refs) > 0 {
		secs.add(translationsSection(nil, translationsURL(refs[0], studyEdition), level, txt))
	}
	secs.add(citedSection(cited, level, txt))
	return secs
}

// withCited puts the verses a block of a section cites under that block, and
// the ones it could not place after it all; level is the heading level of
// each of them.
func withCited(content string, nodes []unfold.Node, level int, txt *i18n.Messages) string {
	content, rest := inlineChildren(content, nodes, level, "", txt)
	var b strings.Builder
	b.WriteString(content)
	writeUnfoldNodes(&b, rest, level, "", txt)
	return b.String()
}

// notesBody is the study notes of a passage, each a paragraph of its own.
func notesBody(notes []model.StudyNote) string {
	var b strings.Builder
	for _, note := range notes {
		// a note is the inside of its paragraph, so it needs one of its own:
		// without it two notes run together into one
		b.WriteString("<p>" + note.HTML + "</p>")
	}
	return b.String()
}

// hopNotesSection is notesSection with the verses the notes cite under the
// note citing them; the section stands at level.
func hopNotesSection(notes []model.StudyNote, err error, cited []unfold.Node, level int,
	txt *i18n.Messages) (UnfoldSection, bool) {
	sec, ok := notesSection(notes, err, level, txt)
	if !ok || len(cited) == 0 {
		return sec, ok
	}
	var b strings.Builder
	if err != nil {
		fmt.Fprintf(&b, "<p><em>%s</em></p>", html.EscapeString(fmt.Sprintf(txt.StudyFailed, err)))
	}
	b.WriteString(withCited(notesBody(notes), cited, level+1, txt))
	sec.Body = b.String()
	return sec, true
}

// hopFootnotesSection holds the footnotes of a verse node, each with the verses
// it cites under it; the section stands at level.
func hopFootnotesSection(footnotes []unfold.Node, level int, txt *i18n.Messages) (UnfoldSection, bool) {
	var b strings.Builder
	for _, f := range footnotes {
		if f.Err != nil || strings.TrimSpace(f.HTML) == "" {
			continue
		}
		fmt.Fprintf(&b, `<div class="footnote" data-ref="%s">%s</div>`,
			html.EscapeString(RefPath(f.Ref.Path)), withCited(paragraph(f.HTML), f.Children, level+1, txt))
	}
	return footnotesSection(b.String(), txt)
}

// streamPassageHops is StreamPassageParts for an expansion counted in bible
// references, with its sections standing at level: what a web page streams
// (SectionLevel), or what is written in one piece under a verse.
func (s *Service) streamPassageHops(ctx context.Context, lng model.Language, edition string, ref bibleref.Ref,
	parts PassageParts, cfg UnfoldConfig, level int, txt *i18n.Messages, out UnfoldStream) (string, int, error) {
	if edition == "" {
		edition = studyEdition
	}
	if cfg.Verses == nil {
		cfg.Verses = unfold.NewVerses()
	}
	depth := max(cfg.Depth, 1)
	// a part asked for alone is the body of a section on the page already
	alone := parts.Only != "" && !parts.Lazy
	want := func(part string) bool { return parts.Only == "" || parts.Only == part || parts.Lazy }
	lazy := func(part string) bool { return parts.Lazy && parts.Only != part }

	out.stage(StageStudy)
	doc, err := s.Chapter(ctx, lng, edition, ref)
	if err != nil {
		return "", 1, err
	}
	verses, err := doc.Verses(ref.VerseStart, ref.VerseEnd)
	if err != nil {
		return "", 1, err
	}
	var text strings.Builder
	for _, v := range verses {
		text.WriteString(v.HTML)
	}
	chapters := map[string]*wol.ChapterDoc{
		fmt.Sprintf("%s-%d-%d", edition, ref.Book, ref.Chapter): doc,
	}
	r := hopResolver(s, lng, chapters)
	sess := unfold.NewSession(r, unfoldOptions(cfg))
	sess.Spend(cfg.Spent + 1)
	spent := func() int { return sess.Requests() - cfg.Spent }
	// the passage is on the page already, wherever else it is cited
	for _, v := range verses {
		sess.ShowVerses(v.ID)
	}
	passageRef := RefString(ref, s.BookTable(ctx, lng))

	study, studyErr := r.studyOf(ctx, ref)
	sess.Spend(study.Requests)
	if studyErr != nil && ctx.Err() != nil {
		return "", spent(), ctx.Err()
	}
	// the indexes, narrowed to one when one is asked for alone
	indexRefs, indexLinks := study.Research, study.Links
	var otherRefs []unfold.Ref
	if parts.Kind != "" {
		indexRefs, indexLinks, otherRefs = nil, nil, nil
		for _, ref := range study.Research {
			if indexKind(ref.Rank) == parts.Kind {
				indexRefs = append(indexRefs, ref)
			} else {
				otherRefs = append(otherRefs, ref)
			}
		}
		for _, item := range study.Links {
			if indexKind(researchRank(item)) == parts.Kind {
				indexLinks = append(indexLinks, item)
			}
		}
	}
	research := map[string]bool{}
	for _, ref := range study.Research {
		research[ref.Path] = true
	}
	var marginal []unfold.Ref
	for _, ref := range unfold.Refs(text.String()) {
		if !research[ref.Path] {
			marginal = append(marginal, ref)
		}
	}
	noteRefs := verseRefsOf(unfold.Refs(notesBody(study.Notes)))
	footnotes := footnoteLinks(text.String())

	// the first step at once — what each part cites — so an expensive passage
	// is asked about before any of it is spent
	var planned, plannedIndexes []unfold.Ref
	if want(PartNotes) && !lazy(PartNotes) {
		planned = append(planned, noteRefs...)
	}
	if want(PartMarginal) && !lazy(PartMarginal) {
		planned = append(planned, marginal...)
	}
	if want(PartIndexes) && !lazy(PartIndexes) {
		plannedIndexes = indexRefs
	}
	cost := sess.CostHops(planned, depth-1) + sess.CostHops(plannedIndexes, depth)
	if want(PartFootnotes) && !lazy(PartFootnotes) {
		cost += len(footnotes)
	}
	if ok, err := sess.Check(1, cost); err != nil || !ok {
		return stoppedNote(!ok && err == nil, len(planned)+len(plannedIndexes), txt), spent(), err
	}
	var notes []string
	run := func(refs []unfold.Ref, hops int) ([]unfold.Node, error) {
		if len(refs) == 0 {
			return nil, nil
		}
		expanded, note, err := runSession(ctx, sess, []unfold.Group{{RootRefs: refs, Hops: hops}}, txt)
		if err != nil {
			return nil, err
		}
		notes = appendNote(notes, note)
		return expanded[0], nil
	}

	if want(PartNotes) {
		if lazy(PartNotes) {
			sec, ok := notesSection(study.Notes, studyErr, level, txt)
			sec.Body, sec.Lazy = "", hopPartURL(ref, PartNotes, "")
			out.add(sec, ok)
		} else {
			cited, err := run(noteRefs, depth-1)
			if err != nil {
				return joinNotes(notes), spent(), err
			}
			sec, ok := hopNotesSection(study.Notes, studyErr, cited, level, txt)
			sec.Unwrap = alone
			out.add(sec, ok)
		}
	}

	if want(PartFootnotes) && len(footnotes) > 0 {
		if lazy(PartFootnotes) {
			out.Section(UnfoldSection{
				Title: html.EscapeString(txt.FootnotesHeading), Ref: FootnotesRef, Order: orderFootnotes,
				Lazy: hopPartURL(ref, PartFootnotes, ""),
			})
		} else {
			body, n := footnotesHTML(ctx, r, footnotes)
			sess.Spend(n)
			cited, err := run(verseRefsOf(unfold.Refs(body)), depth-1)
			if err != nil {
				return joinNotes(notes), spent(), err
			}
			sec, ok := footnotesSection(withCited(body, cited, level+1, txt), txt)
			sec.Unwrap = alone
			out.add(sec, ok)
		}
	}

	if !alone {
		// the other bibles of the language and who quotes the passage, both
		// loaded only once they are opened
		if len(s.otherEditionsFor(ctx, lng, edition)) > 0 {
			out.add(translationsSection(nil, translationsURL(ref, edition), level, txt))
		}
		out.add(citedSection(unfold.Cited{
			Ref: passageRef, Lazy: citedURL([]bibleref.Ref{ref}),
		}, level, txt))
	}

	out.stage(StageReferences)
	if want(PartMarginal) && len(marginal) > 0 {
		if lazy(PartMarginal) {
			sec := marginalSection(passageRef, txt)
			sec.Key, sec.Lazy = "", hopPartURL(ref, PartMarginal, "")
			out.Section(sec)
		} else {
			if !alone {
				out.Section(marginalSection(passageRef, txt))
			}
			// one at a time, each as soon as it is ready
			for _, m := range marginal {
				expanded, err := run([]unfold.Ref{m}, depth-1)
				if err != nil {
					return joinNotes(notes), spent(), err
				}
				for _, n := range expanded {
					sec := marginalEntry(n, "", level+1, txt)
					if alone {
						// straight into the section on the page
						sec.In = ""
					}
					out.Section(sec)
				}
			}
		}
	}

	if want(PartIndexes) && (len(indexRefs) > 0 || len(indexLinks) > 0) {
		if lazy(PartIndexes) {
			roots := make([]unfold.Node, len(indexRefs))
			for i, ref := range indexRefs {
				roots[i] = unfold.Node{Ref: ref}
			}
			for i, g := range indexGroups(indexLinks, roots, txt) {
				out.Section(UnfoldSection{
					Title: html.EscapeString(g.name), Order: orderIndexes + i, Ref: IndexRef(i),
					Lazy: hopPartURL(ref, PartIndexes, indexKind(g.rank)),
				})
			}
		} else {
			if parts.Kind == KindPubIndex {
				// what the research guide lists already is not listed again
				if err := sess.Show(ctx, otherRefs); err != nil {
					return joinNotes(notes), spent(), err
				}
			}
			// one run for both indexes, which is what drops a passage the two
			// of them point at alike
			nodes, err := run(indexRefs, depth)
			if err != nil {
				return joinNotes(notes), spent(), err
			}
			for _, sec := range indexSections(indexLinks, nodes, level, txt) {
				sec.Unwrap = alone
				out.Section(sec)
			}
		}
	}
	return joinNotes(notes), spent(), ctx.Err()
}

// unfoldVersesHops is unfoldBibleVerses for an expansion counted in bible
// references: every verse of a reading as a web page streams it, written in one
// piece under the verse, with one budget and one set of verses shown across
// the reading; cfg.Spent counts what it spent. A verse of the reading is shown
// again under another only when it is reached through it.
func (s *Service) unfoldVersesHops(ctx context.Context, lng model.Language, edition string, ref bibleref.Ref,
	verses []model.Verse, level int, cfg *UnfoldConfig, txt *i18n.Messages) ([]string, string, error) {
	if cfg.Verses == nil {
		cfg.Verses = unfold.NewVerses()
	}
	out := make([]string, len(verses))
	var notes []string
	for i, v := range verses {
		num := v.ID % 1000
		var secs sectionList
		note, requests, err := s.streamPassageHops(ctx, lng, edition, bibleref.Ref{
			Book: ref.Book, Chapter: ref.Chapter, VerseStart: num, VerseEnd: num,
		}, PassageParts{}, *cfg, level, txt, UnfoldStream{Section: func(sec UnfoldSection) {
			secs = append(secs, sec)
		}})
		cfg.Spent += requests
		notes = appendNote(notes, note)
		if err != nil {
			return nil, "", err
		}
		var b strings.Builder
		writeSections(&b, secs, level)
		if b.Len() > 0 && i < len(verses)-1 {
			b.WriteString("<hr/>")
		}
		out[i] = b.String()
	}
	return out, joinNotes(notes), nil
}
