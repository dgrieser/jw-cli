package i18n

import (
	"fmt"
	"time"
)

// Messages is every piece of text jw-cli prints itself. Fields holding a "%"
// are fmt format strings; the argument order is fixed by the field comment, so a
// translation may reorder them only with explicit indexes ("%[2]s").
//
// A struct rather than a keyed map on purpose: adding a message is a compile
// error until every language carries it, and a call site cannot mistype a key.
type Messages struct {
	// --- listings ---------------------------------------------------------
	NoResults string // "No results."
	// ResultsOne and ResultsMany take the count and the query.
	ResultsOne  string
	ResultsMany string
	// WolResultsOne and WolResultsMany take the count, the query and the page.
	WolResultsOne  string
	WolResultsMany string
	// WolResultsUnknown takes the query and the page, for a page with no total.
	WolResultsUnknown string
	// PageSuffix takes the page number and the results per page.
	PageSuffix string
	// PageSuffixShort takes the page number.
	PageSuffixShort string
	// SearchHeader takes the query.
	SearchHeader string
	// ImagesIn takes the article title.
	ImagesIn string
	// MediaOn takes the bible reference.
	MediaOn         string
	MediaCategories string

	// --- bible ------------------------------------------------------------
	// BibleRefsIn and NoBibleRefs take the article title.
	BibleRefsIn  string
	NoBibleRefs  string
	ReadOneHint  string
	NoStudyNotes string
	NoCrossRefs  string
	NoResearch   string

	// --- image metadata ---------------------------------------------------
	// The words an image carries in the page that references it. Printed with
	// every image row and by `jw show <n>` on one, --no-urls or not: the flag
	// hides where the picture is, not what it shows.
	LabelAltText     string
	LabelCredit      string
	LabelImageSize   string
	LabelDescription string
	// ImageFallbackTitle names an image that says nothing about itself and
	// takes the result index, which is what jw show|download takes.
	ImageFallbackTitle string

	// --- media details ----------------------------------------------------
	FilesHeading   string
	LabelLANK      string
	LabelType      string
	LabelDuration  string
	LabelPublished string
	LabelCategory  string
	// LabelLanguages takes the count and a sample of the language symbols.
	LabelLanguages string
	Subtitles      string
	// DownloadHint takes the media item's LANK.
	DownloadHint string
	// DownloadHintIndex takes the result index.
	DownloadHintIndex string
	// PressToDownload is the TUI hint for a directly downloadable item.
	PressToDownload string

	// --- languages table --------------------------------------------------
	ColSymbol     string
	ColLocale     string
	ColName       string
	ColVernacular string
	SignLanguage  string // appended to a sign language's name

	// --- downloads --------------------------------------------------------
	// Downloaded takes the saved path.
	Downloaded string
	// Downloading takes the item title (TUI status).
	Downloading string
	// DownloadingLabel is the progress bar's own caption.
	DownloadingLabel string
	// DownloadFailed takes the reason (TUI status).
	DownloadFailed string

	// --- wol pages --------------------------------------------------------
	// DailyTextTitle takes the formatted date.
	DailyTextTitle string
	// MeetingsTitle takes the ISO week and year.
	MeetingsTitle string

	// --- interactive browser ----------------------------------------------
	Loading string
	// ErrorStatus takes the reason.
	ErrorStatus   string
	NoMoreItems   string
	NoMoreResults string
	KeyView       string
	KeyDownload   string
	KeyOpenLink   string
	KeyNextPage   string
	KeyPrevPage   string
	KeyBack       string
	KeyQuit       string
	DetailHelp    string

	// MarginalReference labels a cross reference inside a verse, which the
	// document writes as a bare "+". wol's own name for the feature.
	MarginalReference string
	// MarginalReferenceWithSource labels the same thing when the passage it sits
	// in is known, and takes that passage and the one it points at. The two are
	// joined by an arrow, matching the separator the unfold headings use.
	MarginalReferenceWithSource string

	// --- unfolding citations ------------------------------------------------
	UnfoldHeading string
	// UnfoldConfirm takes the level and the number of requests it needs. That
	// number is an upper bound: verses of the same chapter share the chapter
	// page their study pane comes from, and it is fetched once.
	UnfoldConfirm string
	// StudyNotesHeading labels the study notes printed with an unfolded verse.
	StudyNotesHeading string
	// ResearchHeading labels the research-guide entries of an unfolded verse
	// that point at a whole article, which has no passage to unfold.
	ResearchHeading string
	// StudyFailed takes the reason the study pane of a verse could not be read.
	// The verse itself is still printed.
	StudyFailed string
	// UnfoldProgress takes the level, the requests done and the total.
	UnfoldProgress string
	// UnfoldStopped takes the number of references left unexpanded after the
	// expansion was cut short. Reaching the requested depth is not that: it is
	// the normal end of an expansion and goes unremarked.
	UnfoldStopped string
	// UnfoldFailed takes the reason a reference could not be resolved.
	UnfoldFailed string

	// --- dates ------------------------------------------------------------
	// Weekdays is indexed by time.Weekday, Months by time.Month minus one.
	Weekdays [7]string
	Months   [12]string
	// FullDate assembles a weekday, day, month name and year.
	FullDate func(weekday string, day int, month string, year int) string
}

// Date renders t as a full date in the language's own word order.
func (m *Messages) Date(t time.Time) string {
	return m.FullDate(m.Weekdays[t.Weekday()], t.Day(), m.Months[t.Month()-1], t.Year())
}

// Results renders the result-count header for a search.
func (m *Messages) Results(total int, query string) string {
	return fmt.Sprintf(m.plural(total, m.ResultsOne, m.ResultsMany), total, query)
}

// WolResults renders the result-count header for a wol search. A total of zero
// means the page did not report one.
func (m *Messages) WolResults(total int, query string, page int) string {
	if total <= 0 {
		return fmt.Sprintf(m.WolResultsUnknown, query, page)
	}
	return fmt.Sprintf(m.plural(total, m.WolResultsOne, m.WolResultsMany), total, query, page)
}

func (m *Messages) plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
