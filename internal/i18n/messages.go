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
