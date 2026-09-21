package service

import (
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/dgrieser/jw-cli/internal/model"
)

// contentWords is how many words a passage must have left, once the reference
// it was found by is taken out of it, to be worth printing.
//
// A citation search matches wherever a verse is named, and a great deal of the
// library names verses without saying anything about them: reading schedules
// ("14 Jeremia 34-35"), scripture indexes ("34:7 1:179, 214; 2:150"), school
// programmes ("18. Juli Bibellesen:"), the headline over a workbook section.
// Every word those contribute sits inside the citation itself, so removing the
// citation empties them, while a paragraph that discusses the verse keeps its
// prose.
//
// Measured over 114 passages of two verses: what says nothing tops out at five
// words left, what says something starts at twelve, and nothing at all lands in
// between. Eight is the middle of that gap.
const contentWords = 8

// citationMarkup is the reference a passage was found by: the scripture link
// and the mark wol puts on what a search matched.
const citationMarkup = "a.b, .mk"

// wordRun is a word of the length it takes to carry meaning. Two letters are
// page markers and ordinals; digits are verse and page numbers.
var wordRun = regexp.MustCompile(`\p{L}{3,}`)

// keepTelling drops from a citation listing what only names the verse without
// saying anything about it. Passage by passage, because one document does both:
// a workbook heads its section with the reference and then asks a question
// about it, and only the heading is worth dropping. A result left with nothing
// to say drops out of the listing.
//
// Only citation listings are filtered. A plain search is asked for matches and
// should report the matches it found.
func keepTelling(items []model.Result) []model.Result {
	out := items[:0]
	for _, item := range items {
		if item.Excerpt != "" {
			item.Excerpt = tellingBlocks(item.Excerpt)
		}
		if item.Excerpt == "" && !tells(item.Snippet) {
			continue
		}
		out = append(out, item)
	}
	return out
}

// tellingBlocks keeps the passages of one result that say something.
func tellingBlocks(fragment string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(fragment))
	if err != nil {
		return fragment
	}
	var kept []string
	doc.Find("body").Children().Each(func(_ int, block *goquery.Selection) {
		html, err := goquery.OuterHtml(block)
		if err != nil || !tells(html) {
			return
		}
		kept = append(kept, html)
	})
	return strings.Join(kept, "\n")
}

// tells reports whether a fragment says anything once the reference it was
// found by is taken out of it.
func tells(fragment string) bool {
	if strings.TrimSpace(fragment) == "" {
		return false
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(fragment))
	if err != nil {
		return true // unreadable markup is not evidence of emptiness
	}
	doc.Find(citationMarkup).Remove()
	return len(wordRun.FindAllString(doc.Find("body").Text(), contentWords)) >= contentWords
}
