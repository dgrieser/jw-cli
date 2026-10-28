package wol

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"

	"github.com/dgrieser/jw-cli/internal/bibleref"
	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/model"
)

// The publications that go through every bible book part by part are read by
// their documents' ids, which are the same in every language the library has
// them in.
const (
	// inspiredDoc + book is the "Inspired" book's study of that book: "Bible
	// Book Number 1—Genesis" is 1101990062.
	inspiredDoc = 1101990061
	// handbookDoc + book is the Bible handbook's chapter on that book, for
	// the books it has come out with so far.
	handbookDoc = 1102026500
)

// highlightDocs are the Watchtower's "Jehovah's Word Is Alive" articles,
// 2004 to 2009, with the books each one goes through, in its order.
var highlightDocs = []struct {
	doc   int
	books []int
}{
	{2004004, []int{1}}, {2004046, []int{1}}, {2004206, []int{2}}, {2004365, []int{3}},
	{2004565, []int{4}}, {2004686, []int{5}}, {2004882, []int{6}}, {2005046, []int{7}},
	{2005168, []int{8}}, {2005205, []int{9}}, {2005365, []int{10}}, {2005487, []int{11}},
	{2005562, []int{12}}, {2005722, []int{13}}, {2005884, []int{14}}, {2006044, []int{15}},
	{2006082, []int{16}}, {2006162, []int{17}}, {2006205, []int{18}}, {2006365, []int{19}},
	{2006402, []int{19}}, {2006523, []int{19}}, {2006643, []int{19}}, {2006685, []int{20}},
	{2006803, []int{21}}, {2006843, []int{22}}, {2006882, []int{23}}, {2007042, []int{23}},
	{2007202, []int{24}}, {2007402, []int{25}}, {2007482, []int{26}}, {2007562, []int{26}},
	{2007644, []int{27}}, {2007684, []int{28}}, {2007722, []int{29, 30}}, {2007803, []int{31, 32, 33}},
	{2007842, []int{34, 35, 36}}, {2007882, []int{37, 38}}, {2007927, []int{39}}, {2008049, []int{40}},
	{2008127, []int{41}}, {2008208, []int{42}}, {2008287, []int{43}}, {2008368, []int{44}},
	{2008447, []int{45}}, {2008526, []int{46, 47}}, {2008607, []int{48, 49, 50, 51}},
	{2008689, []int{52, 53, 54, 55}}, {2008768, []int{56, 57, 58}}, {2008849, []int{59, 60, 61}},
	{2008926, []int{62, 63, 64, 65}}, {2009046, []int{66}}, {2009124, []int{66}},
}

// insightDocs is Insight's entry on each book, by book. The books written as
// one work — Samuel, Kings, Chronicles, the letters to the Corinthians, ... —
// share an entry, which has a box of highlights for each of them in turn.
var insightDocs = [67]int{0,
	1200001650, 1200001459, 1200002732, 1200003279, 1200001163, 1200002524, 1200002546, 1200003777,
	1200003825, 1200003825, 1200002616, 1200002616, 1200000969, 1200000969, 1200001476, 1200003213,
	1200001431, 1200002476, 1200003572, 1200003569, 1200001255, 1200004181, 1200002205, 1200002422,
	1200002675, 1200001470, 1200001118, 1200002087, 1200002482, 1200000247, 1200003288, 1200002504,
	1200003033, 1200003173, 1200001790, 1200004697, 1200001813, 1200004682, 1200002861, 1200002939,
	1200002909, 1200002791, 1200002490, 1200000084, 1200003762, 1200001039, 1200001039, 1200001595,
	1200001386, 1200003468, 1200001011, 1200004378, 1200004378, 1200004417, 1200004417, 1200004432,
	1200003464, 1200001954, 1200002314, 1200003452, 1200003452, 1200002491, 1200002491, 1200002491,
	1200002543, 1200003717,
}

// studyDoc is one document a publication has on a book: its id, and the books
// it goes through in its order, the book asked for among them.
type studyDoc struct {
	doc   int
	books []int
}

// studyDocs are the documents pub has on book, in reading order.
func studyDocs(pub string, book int) []studyDoc {
	if book < 1 || book > 66 {
		return nil
	}
	switch pub {
	case model.StudyInspired:
		return []studyDoc{{inspiredDoc + book, []int{book}}}
	case model.StudyHandbook:
		return []studyDoc{{handbookDoc + book, []int{book}}}
	case model.StudyHighlights:
		var out []studyDoc
		for _, h := range highlightDocs {
			if slices.Contains(h.books, book) {
				out = append(out, studyDoc{h.doc, h.books})
			}
		}
		return out
	case model.StudyInsight:
		var books []int
		for b, doc := range insightDocs {
			if doc == insightDocs[book] {
				books = append(books, b)
			}
		}
		return []studyDoc{{insightDocs[book], books}}
	}
	return nil
}

// StudyPubs are the publications BookStudies reads, in the order a chapter
// lists them.
var StudyPubs = []string{model.StudyHandbook, model.StudyHighlights, model.StudyInsight, model.StudyInspired}

// BookStudies reads what one of the publications that go through every bible
// book (StudyPubs) says about book, part by part: one study for each of its
// documents on the book. A document the language does not have — or the
// handbook has not come out with yet — is left out without an error. Kept
// until the documents change upstream.
func (c *Client) BookStudies(ctx context.Context, cfg Config, pub string, book int) ([]model.BookStudy, error) {
	docs := studyDocs(pub, book)
	if docs == nil {
		return nil, fmt.Errorf("no %q study of book %d", pub, book)
	}
	var out []model.BookStudy
	for _, d := range docs {
		u := c.url(cfg, "d", fmt.Sprintf("/%d", d.doc))
		st, err := httpx.Memo(ctx, c.cache, fmt.Sprintf("study3-%s-%d-%s", pub, book, u), func(ctx context.Context) (model.BookStudy, error) {
			return c.bookStudy(ctx, u, pub, book, d.books)
		})
		if err != nil {
			return out, err
		}
		if len(st.Parts) > 0 {
			out = append(out, st)
		}
	}
	return out, nil
}

func (c *Client) bookStudy(ctx context.Context, u, pub string, book int, books []int) (model.BookStudy, error) {
	st := model.BookStudy{Pub: pub, Book: book, URL: u}
	doc, err := c.hc.GetHTML(ctx, u)
	if err != nil {
		if httpx.IsGone(err) {
			// not in this language, or not out yet: nothing to say, which
			// is kept like anything else
			return st, nil
		}
		return st, err
	}
	content := doc.Find("#article").First()
	if content.Length() == 0 {
		content = doc.Find("article").First()
	}
	st.Title = documentTitle(doc, content)
	blocks := studyBlocks(content)
	switch pub {
	case model.StudyInspired, model.StudyHandbook:
		st.Parts = parseSummary(blocks, u, book)
	case model.StudyHighlights:
		st.Parts = parseHighlights(blocks, u, book, books)
	case model.StudyInsight:
		st.Parts = parseInsightBox(blocks, u, book, books)
	}
	return st, nil
}

// studyBlock is one paragraph or heading of a document, as the parsers below
// read it.
type studyBlock struct {
	pid     int
	class   string
	heading bool // h1-h3, which close a section of a document as p.ss does
	text    string
	html    string // as the document has it
	sel     *goquery.Selection
	// box is the box the block is in, as the library lays boxes out now,
	// and lead says the block opens with words in bold: a heading of it.
	box  *html.Node
	lead bool
}

func (b studyBlock) is(class string) bool {
	return slices.Contains(strings.Fields(b.class), class)
}

// selBox is a box as the library lays it out now, its title and its
// content within: Insight's "Highlights of Isaiah".
const selBox = "div.boxSupplement"

func studyBlocks(content *goquery.Selection) []studyBlock {
	var out []studyBlock
	content.Find("h1, h2, h3, p").Each(func(_ int, s *goquery.Selection) {
		pid, _ := strconv.Atoi(s.AttrOr("data-pid", ""))
		b := studyBlock{
			pid:     pid,
			class:   s.AttrOr("class", ""),
			heading: !s.Is("p"),
			text:    studyText(s.Text()),
			sel:     s,
		}
		b.html, _ = goquery.OuterHtml(s)
		if box := s.Closest(selBox); box.Length() > 0 {
			b.box = box.Get(0)
		}
		if strong := s.ChildrenFiltered("strong").First(); strong.Length() > 0 {
			b.lead = strings.HasPrefix(b.text, studyText(strong.Text()))
		}
		out = append(out, b)
	})
	return out
}

// studyText is a block's text without the invisible marks wol sets around
// dashes and in long words.
func studyText(s string) string {
	return cleanSpace(strings.NewReplacer("\u2060", "", "\u00ad", "", "\u200b", "").Replace(s))
}

// passageURL links the paragraphs from to to of a document.
func passageURL(u string, from, to int) string {
	if from == 0 {
		return u
	}
	return fmt.Sprintf("%s#h=%d:0-%d:0", u, from, max(from, to))
}

var (
	// summaryPart is a paragraph of a summary that opens a part: its title,
	// the verses it covers in parentheses, then the sentence ends — "Jesus
	// and the Kingdom message introduced (1:1–4:25). Matthew logically
	// begins ...".
	// The title may hold words in parentheses of its own: "Prophetic
	// judgments against Ephraim (and Judah) (4:1–14:9)."
	// English sets the verses after the title's full stop: "Jehovah
	// commissions Ezekiel as a prophet. (1:1–3:27) In 613 B.C.E., ...".
	summaryPart = regexp.MustCompile(`^((?:[^()]|\([^()\d]*\)){3,200}?)\.?\s*\(([^()]*\d[^()]*)\)\s*(?:\.|\s|$)`)
	// paragraphNumber is the number a study's paragraph opens with.
	paragraphNumber = regexp.MustCompile(`^\d+\s+`)
	// refSpan tells the verses of a part from a verse a sentence quotes: a
	// part covers a stretch, "1:1–2:25", "11:10 to 25:26", "Psalm 1–41",
	// "vss. 1-16".
	refSpan = regexp.MustCompile(`\d\s*(?:-|\p{L}{1,4})\s*\d`)
	// titledRef is a heading followed by the verses it is about:
	// "The serpent deceives the woman (3:1-7)".
	titledRef = regexp.MustCompile(`^(.*?)\s*\(([^()]*\d[^()]*)\)$`)
)

// isSummarySpan tells the title and verses of a part of a summary from a
// sentence that happens to end in verses: the title is no sentence of its
// own, and the verses are one stretch of the book, not several places.
func isSummarySpan(title, ref string) bool {
	return !strings.Contains(title, ". ") && !strings.Contains(ref, ";") && refSpan.MatchString(normDashes(ref))
}

// parseSummary reads the part of a study that goes through the book from
// beginning to end — the "Inspired" book's "Contents of Genesis", the
// handbook's "Summary" — as parts, one for each paragraph that names the
// verses it covers, together with the paragraphs after it. The summary is the
// longest run of such paragraphs going on through the book: a paragraph
// elsewhere that happens to quote a stretch of verses is not one of it.
func parseSummary(blocks []studyBlock, u string, book int) []model.StudyPart {
	type part struct {
		model.StudyPart
		from, to int
		section  int
		html     []string
	}
	var parts []part
	section, open := 0, false
	for _, b := range blocks {
		if b.heading || b.is("ss") {
			section++
			open = false
			continue
		}
		if b.is("qu") || b.pid == 0 {
			continue
		}
		t := paragraphNumber.ReplaceAllString(b.text, "")
		if m := summaryPart.FindStringSubmatch(t); m != nil && isSummarySpan(m[1], m[2]) {
			parts = append(parts, part{
				StudyPart: model.StudyPart{Title: strings.TrimSpace(m[1]), Label: m[2], Chapters: refChapters(m[2], book)},
				from:      b.pid, to: b.pid, section: section, html: []string{b.html},
			})
			open = true
			continue
		}
		// a summary's paragraphs are body text; a box's lines close a part
		if open && (b.class == "" || b.is("sb")) {
			parts[len(parts)-1].to = b.pid
			parts[len(parts)-1].html = append(parts[len(parts)-1].html, b.html)
		} else {
			open = false
		}
	}
	// the summary goes on through the book where a box breaks into it: a
	// part under the next heading carries on from the chapter the last one
	// ended in
	follows := func(p, prev part) bool {
		first, last := p.Chapters[0], prev.Chapters[len(prev.Chapters)-1]
		return p.section == prev.section && first >= prev.Chapters[0] || first == last || first == last+1
	}
	var runs [][]part
	for _, p := range parts {
		if len(p.Chapters) == 0 {
			continue
		}
		if n := len(runs); n > 0 {
			run := runs[n-1]
			if follows(p, run[len(run)-1]) {
				runs[n-1] = append(run, p)
				continue
			}
			// the last one was another book's verses the summary quotes
			if len(run) > 1 && follows(p, run[len(run)-2]) {
				run[len(run)-1] = p
				continue
			}
		}
		runs = append(runs, []part{p})
	}
	var best []part
	for _, run := range runs {
		if len(run) > len(best) {
			best = run
		}
	}
	if len(best) == 0 {
		return chapterByChapter(blocks, u, book)
	}
	out := make([]model.StudyPart, 0, len(best))
	for _, p := range best {
		p.URL = passageURL(u, p.from, p.to)
		p.HTML = strings.Join(p.html, "")
		out = append(out, p.StudyPart)
	}
	return out
}

// chapterByChapter reads a summary that names no verses but goes through
// the book a paragraph a chapter — the "Inspired" book's on Lamentations, a
// paragraph for each of its five poems: the first section of the document
// with as many paragraphs as the book has chapters, each the part about its
// chapter, titled by its first sentence.
func chapterByChapter(blocks []studyBlock, u string, book int) []model.StudyPart {
	n := bibleref.Chapters(book)
	var sections [][]studyBlock
	var cur []studyBlock
	for _, b := range blocks {
		switch {
		case b.heading || b.is("ss"):
			sections, cur = append(sections, cur), nil
		case b.pid != 0 && b.is("sb"):
			cur = append(cur, b)
		}
	}
	sections = append(sections, cur)
	for _, sec := range sections[1:] { // not the introduction before the first heading
		if len(sec) != n || n < 2 {
			continue
		}
		out := make([]model.StudyPart, 0, n)
		for i, b := range sec {
			title, _ := splitSentence(paragraphNumber.ReplaceAllString(b.text, ""))
			out = append(out, model.StudyPart{Title: title, Chapters: []int{i + 1}, URL: passageURL(u, b.pid, b.pid), HTML: b.html})
		}
		return out
	}
	return nil
}

// parseHighlights reads a "Jehovah's Word Is Alive" article: each section
// headed by the verses it goes through, "(Genesis 1:1–7:24)", with the
// questions answered and the lessons drawn on its verses as its items, under
// their subheadings; the section's overview of what it goes through ("Chapters
// 46 to 51 contain Jeremiah's pronouncements against the nations") comes with
// its heading, about every chapter it covers. An article on several books goes through them in turn,
// each section of the next book starting over at its beginning; only those
// of book are kept.
func parseHighlights(blocks []studyBlock, u string, book int, books []int) []model.StudyPart {
	var out []model.StudyPart
	var cur *model.StudyPart
	title, titlePid, from, to := "", 0, 0, 0
	titleHTML := ""
	// overview says the paragraphs read are the section's overview of what
	// it goes through, which comes before its questions and lessons
	overview := false
	group := ""
	at, lastStart := 0, 0
	closeSection := func() {
		if cur != nil {
			cur.URL = passageURL(u, from, to)
			out = append(out, *cur)
			cur = nil
		}
	}
	for _, b := range blocks {
		if b.pid == 0 {
			continue
		}
		switch {
		case b.is("ss"):
			m := titledRef.FindStringSubmatch(b.text)
			if m == nil || !strings.Contains(m[2], ":") && !refSpan.MatchString(normDashes(m[2])) {
				// the next section's title, or a subheading of this one
				title, titlePid, group, titleHTML = b.text, b.pid, b.text, b.html
				overview = false
				continue
			}
			closeSection()
			if m[1] != "" {
				title, titlePid, titleHTML = m[1], b.pid, ""
			}
			start := firstChapter(m[2])
			if lastStart > 0 && start <= lastStart && at < len(books)-1 {
				at++
			}
			lastStart = start
			group = ""
			if books[at] != book {
				continue
			}
			cur = &model.StudyPart{Title: title, Label: m[2], Chapters: refChapters(m[2], book), HTML: titleHTML + b.html}
			from, to = firstNonZero(titlePid, b.pid), b.pid
			overview = true
		case cur == nil:
		case b.is("sn"):
			group, to, overview = b.text, b.pid, false
		default:
			to = b.pid
			if item, ok := verseItem(b, book); ok {
				item.Group = group
				item.URL = passageURL(u, b.pid, b.pid)
				item.HTML = b.html
				cur.Items = append(cur.Items, item)
				overview = false
			} else if overview {
				cur.HTML += b.html
			}
		}
	}
	closeSection()
	return out
}

// parseInsightBox reads the boxes of an Insight entry that go through a book
// — "Highlights of Genesis" —, each line naming the verses it is about as its
// heading or its items do: "Sin and death enter; a "seed" promised as
// deliverer (3:1–5:5)". A box opens with its marker ("[Box on page 864]"),
// its title and what it says the book is about, then its headings with their
// lines — or, as the library lays boxes out now, a box whose headings open in
// bold. An entry on books written as one work has a box for each of them in
// turn.
func parseInsightBox(blocks []studyBlock, u string, book int, books []int) []model.StudyPart {
	var boxes [][]model.StudyPart
	var box []model.StudyPart
	var from []int // the paragraph each heading of the box is at
	inBox := false
	closeBox := func() {
		if inBox && slices.ContainsFunc(box, func(p model.StudyPart) bool {
			return len(p.Chapters) > 0 || slices.ContainsFunc(p.Items, func(it model.StudyPart) bool { return len(it.Chapters) > 0 })
		}) {
			boxes = append(boxes, box)
		}
		box, from, inBox = nil, nil, false
	}
	titled := func(b studyBlock) model.StudyPart {
		p := model.StudyPart{Title: b.text, URL: passageURL(u, b.pid, b.pid), HTML: b.html}
		if m := titledRef.FindStringSubmatch(b.text); m != nil && m[1] != "" {
			p.Title, p.Label, p.Chapters = m[1], m[2], refChapters(m[2], book)
		}
		return p
	}
	var at *html.Node // the box of the layout of now the blocks are in
	for _, b := range blocks {
		if b.pid == 0 || b.heading {
			continue
		}
		var heading bool
		switch {
		case b.box != nil:
			if b.box != at {
				closeBox()
				inBox, at = true, b.box
			}
			heading = b.lead
		case at != nil:
			// past a box of the layout of now
			closeBox()
			at = nil
			continue
		case b.is("se") && strings.HasPrefix(b.text, "[") && strings.HasSuffix(b.text, "]"):
			closeBox()
			inBox = true
			continue
		case !inBox:
			continue
		case b.is("se"):
			heading = true
		case b.is("sc"):
		default:
			closeBox()
			continue
		}
		if heading {
			box = append(box, titled(b))
			from = append(from, b.pid)
			continue
		}
		// the box's title and what it says first, before any heading
		if len(box) == 0 {
			continue
		}
		p := &box[len(box)-1]
		p.Items = append(p.Items, titled(b))
		p.URL = passageURL(u, from[len(from)-1], b.pid)
	}
	closeBox()
	if len(books) == 1 {
		return slices.Concat(boxes...)
	}
	i := slices.Index(books, book)
	if i < 0 || i >= len(boxes) {
		return nil
	}
	return boxes[i]
}

// itemLabel is the verses a paragraph opens with, up to the dash before a
// question or the full stop before a lesson: "1:16 — How could ...",
// "3:18, 19; 5:5. What Jehovah says ...".
var itemLabel = regexp.MustCompile(`^([^—.]{1,80}?)\s*(?:—|\.(?:\s|$))\s*(.*)$`)

// verseItem reads a paragraph that opens with a link to the verses it is on
// as an item of its section: the verses, its question and the answer, or its
// lesson.
func verseItem(b studyBlock, book int) (model.StudyPart, bool) {
	a := b.sel.Find("a.b").First()
	lead := studyText(a.Text())
	if lead == "" || !strings.HasPrefix(b.text, lead) || !unicode.IsDigit(rune(lead[0])) {
		return model.StudyPart{}, false
	}
	m := itemLabel.FindStringSubmatch(b.text)
	if m == nil {
		return model.StudyPart{}, false
	}
	title, text := splitSentence(m[2])
	return model.StudyPart{Title: title, Text: text, Label: m[1], Chapters: refChapters(m[1], book)}, true
}

// splitSentence parts a paragraph into the question it asks or the sentence
// it opens with, and what it goes on to say: the answer, the rest of the
// lesson. A first sentence too long for a line is cut short, and what is cut
// off goes on in the rest. A full stop after a word of one or two letters is
// an abbreviation's ("B.C.E.", "v. u. Z.").
func splitSentence(s string) (first, rest string) {
	const most = 220
	end := func(i int) (string, string) {
		// a quote the sentence closes with stays with it
		for i < len(s) {
			r, n := utf8.DecodeRuneInString(s[i:])
			if !strings.ContainsRune("”“\"’‘»«", r) {
				break
			}
			i += n
		}
		return s[:i], strings.TrimSpace(s[i:])
	}
	word, prev := 0, ' '
	for i, r := range s {
		switch {
		case r == '?' || r == '!':
			return end(i + 1)
		case r == '.' && (i+1 == len(s) || s[i+1] == ' '):
			// after a word, a closing parenthesis or quote; not an
			// ellipsis (". . .")
			if word > 2 || !unicode.IsLetter(prev) && strings.ContainsRune(")]”“\"’»«", prev) {
				return end(i + 1)
			}
		}
		if unicode.IsLetter(r) {
			word++
		} else {
			word = 0
		}
		prev = r
		if utf8.RuneCountInString(s[:i]) >= most {
			return strings.TrimSpace(s[:i]) + "…", "…" + s[i:]
		}
	}
	return s, ""
}

// normDashes writes the dashes of a reference as hyphens.
func normDashes(s string) string {
	return strings.NewReplacer("—", "-", "–", "-", "\u2011", "-", "\u2060", "").Replace(s)
}

var (
	// refVerse is one verse or stretch of verses a reference names,
	// "11:10 to 25:26", "1:1-17": the chapter it starts in and, when it runs
	// on into another, the one it ends in.
	refVerse = regexp.MustCompile(`(\d+):\d+[a-z]?(?:\s*(?:-|\p{L}{1,4}\.?)\s*(?:(\d+):)?\d+)?`)
	// refChapter is a chapter or stretch of chapters a reference names
	// without verses: "Psalm 1–41".
	refChapter = regexp.MustCompile(`(\d+)(?:\s*(?:-|\p{L}{1,4})\s*(\d+))?`)
	// bookNumber is the number a book's name starts with: "1. Mose", "2 Sam."
	bookNumber = regexp.MustCompile(`(^|[\s(;])[1-3]\.?\s+(\p{L})`)
)

// refChapters are the chapters of book a reference names, in order: "3:1-5,
// 16-23" is chapter 3, "1:1–2:25" chapters 1 and 2, "Psalm 1–41" the
// first 41 psalms. Whatever a book of one chapter names is in that one.
func refChapters(ref string, book int) []int {
	n := bibleref.Chapters(book)
	if n == 0 || !strings.ContainsFunc(ref, unicode.IsDigit) {
		return nil
	}
	if n == 1 {
		return []int{1}
	}
	ref = bookNumber.ReplaceAllString(normDashes(ref), "$1$2")
	var out []int
	add := func(from, to int) {
		if to < from {
			to = from
		}
		for c := max(from, 1); c <= min(to, n); c++ {
			if !slices.Contains(out, c) {
				out = append(out, c)
			}
		}
	}
	if strings.Contains(ref, ":") {
		for _, m := range refVerse.FindAllStringSubmatch(ref, -1) {
			from, _ := strconv.Atoi(m[1])
			to := from
			if m[2] != "" {
				to, _ = strconv.Atoi(m[2])
			}
			add(from, to)
		}
	} else {
		for _, m := range refChapter.FindAllStringSubmatch(ref, -1) {
			from, _ := strconv.Atoi(m[1])
			to := from
			if m[2] != "" {
				to, _ = strconv.Atoi(m[2])
			}
			add(from, to)
		}
	}
	slices.Sort(out)
	return out
}

// firstChapter is the chapter a section's verses start in: "(Joel 1:1 to
// 3:21)" in 1, "(Obadiah 1-21)", which names verses of a book of one chapter,
// in 1 too.
func firstChapter(ref string) int {
	if m := refVerse.FindStringSubmatch(bookNumber.ReplaceAllString(normDashes(ref), "$1$2")); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 1
}

func firstNonZero(vals ...int) int {
	for _, v := range vals {
		if v != 0 {
			return v
		}
	}
	return 0
}
