package i18n

import "fmt"

var en = Messages{
	NoResults:           "No results.",
	ResultsOne:          "%d result for %q",
	ResultsMany:         "%d results for %q",
	WolResultsOne:       "%d wol result for %q (page %d)",
	WolResultsMany:      "%d wol results for %q (page %d)",
	WolResultsUnknown:   "wol results for %q (page %d)",
	CitedResultsOne:     "%d publication citing %s (page %d)",
	CitedResultsMany:    "%d publications citing %s (page %d)",
	CitedResultsUnknown: "publications citing %s (page %d)",
	PageSuffix:          " (page %d, %d per page)",
	PageSuffixShort:     " — page %d",
	SearchHeader:        "Search: %s",
	ImagesIn:            "Images in %q",
	MediaOn:             "Media on %s",
	MediaCategories:     "Media categories",

	BibleRefsIn:  "Bible references in %q:",
	NoBibleRefs:  "No bible references found in %q.",
	ReadOneHint:  `Read one with: jw bible read "<reference>"`,
	NoStudyNotes: "No study notes found (study notes are available in the study edition, nwtsty).",
	NoCrossRefs:  "No marginal references found.",
	NoResearch:   "No research guide entries found.",
	// NotInEditions takes the reference and the editions.
	NotInEditions: "%s is not available in: %s",

	LabelAltText:       "Alt text",
	LabelCredit:        "Credit",
	LabelImageSize:     "Size",
	LabelDescription:   "Description",
	ImageFallbackTitle: "Image %d",

	FilesHeading:      "Files",
	LabelLANK:         "LANK",
	LabelType:         "Type",
	LabelDuration:     "Duration",
	LabelPublished:    "Published",
	LabelCategory:     "Category",
	LabelLanguages:    "Languages: %d (%s)",
	Subtitles:         "subtitles",
	DownloadHint:      "Download with: `jw download <n>`  or  `jw download %s -q 720p`",
	DownloadHintIndex: "Download with: jw download %d",
	PressToDownload:   "Press d to download.",

	ColSymbol:     "SYMBOL",
	ColLocale:     "LOCALE",
	ColName:       "NAME",
	ColVernacular: "VERNACULAR",
	SignLanguage:  "sign language",

	Downloaded:       "Downloaded %s",
	Downloading:      "downloading %s…",
	DownloadingLabel: "downloading",
	DownloadFailed:   "download failed: %s",

	MarginalReference:           "Marginal reference",
	MarginalReferenceWithSource: "Marginal reference %s → %s",

	UnfoldHeading:  "References",
	UnfoldConfirm:  "Unfolding level %d needs up to %d more requests to wol.jw.org. Continue? [y/N] ",
	UnfoldProgress: "unfolding level %d: %d/%d",
	UnfoldStopped:  "Stopped here: %d more references were not unfolded.",
	UnfoldFailed:   "could not be unfolded: %s",

	StudyNotesHeading: "Study notes",
	ResearchHeading:   "Research guide",
	StudyFailed:       "the study notes on this verse could not be read: %s",

	DailyTextTitle: "Daily text, %s",
	MeetingsTitle:  "Meetings, week %d/%d",

	Loading:       "loading…",
	ErrorStatus:   "error: %s",
	NoMoreItems:   "no more items",
	NoMoreResults: "no more results",
	KeyView:       "view",
	KeyDownload:   "download",
	KeyOpenLink:   "open link",
	KeyNextPage:   "next page",
	KeyPrevPage:   "prev page",
	KeyBack:       "back",
	KeyQuit:       "quit",
	DetailHelp:    "↑/↓ scroll · esc back · q quit",

	Weekdays: [7]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"},
	Months: [12]string{
		"January", "February", "March", "April", "May", "June",
		"July", "August", "September", "October", "November", "December",
	},
	FullDate: func(weekday string, day int, month string, year int) string {
		return fmt.Sprintf("%s, %s %d, %d", weekday, month, day, year)
	},
}
