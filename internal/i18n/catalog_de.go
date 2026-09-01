package i18n

import "fmt"

var de = Messages{
	NoResults:           "Keine Ergebnisse.",
	ResultsOne:          "%d Ergebnis für %q",
	ResultsMany:         "%d Ergebnisse für %q",
	WolResultsOne:       "%d wol-Ergebnis für %q (Seite %d)",
	WolResultsMany:      "%d wol-Ergebnisse für %q (Seite %d)",
	WolResultsUnknown:   "wol-Ergebnisse für %q (Seite %d)",
	CitedResultsOne:     "%d Publikation, die %s zitiert",
	CitedResultsMany:    "%d Publikationen, die %s zitieren",
	CitedResultsUnknown: "Publikationen, die %s zitieren",
	ExcerptProgress:     "Textstellen werden gelesen: %d/%d",
	PageSuffix:          " (Seite %d, %d pro Seite)",
	PageSuffixShort:     " — Seite %d",
	SearchHeader:        "Suche: %s",
	ImagesIn:            "Bilder in %q",
	MediaOn:             "Medien zu %s",
	MediaCategories:     "Medienkategorien",

	BibleRefsIn:  "Bibelstellen in %q:",
	NoBibleRefs:  "Keine Bibelstellen in %q gefunden.",
	ReadOneHint:  `Eine davon lesen mit: jw bible read "<Bibelstelle>"`,
	NoStudyNotes: "Keine Studienanmerkungen gefunden (Studienanmerkungen gibt es in der Studienausgabe, nwtsty).",
	NoCrossRefs:  "Keine Querverweise gefunden.",
	NoResearch:   "Keine Einträge im Forschungsverzeichnis gefunden.",
	// NotInEditions nimmt die Bibelstelle und die Ausgaben.
	NotInEditions: "%s ist nicht enthalten in: %s",

	LabelAltText:       "Bildbeschreibung",
	LabelCredit:        "Bildnachweis",
	LabelImageSize:     "Größe",
	LabelDescription:   "Beschreibung",
	ImageFallbackTitle: "Bild %d",

	FilesHeading:      "Dateien",
	LabelLANK:         "LANK",
	LabelType:         "Typ",
	LabelDuration:     "Dauer",
	LabelPublished:    "Veröffentlicht",
	LabelCategory:     "Kategorie",
	LabelLanguages:    "Sprachen: %d (%s)",
	Subtitles:         "Untertitel",
	DownloadHint:      "Herunterladen mit: `jw download <n>`  oder  `jw download %s -q 720p`",
	DownloadHintIndex: "Herunterladen mit: jw download %d",
	PressToDownload:   "d drücken, um herunterzuladen.",

	ColSymbol:     "SYMBOL",
	ColLocale:     "LOCALE",
	ColName:       "NAME",
	ColVernacular: "EIGENNAME",
	SignLanguage:  "Gebärdensprache",

	Downloaded:       "Heruntergeladen: %s",
	Downloading:      "lade %s herunter…",
	DownloadingLabel: "lade herunter",
	DownloadFailed:   "Herunterladen fehlgeschlagen: %s",

	MarginalReference:           "Querverweis",
	MarginalReferenceWithSource: "Querverweis %s → %s",

	UnfoldHeading:  "Referenzen",
	UnfoldConfirm:  "Ebene %d braucht bis zu %d weitere Abfragen an wol.jw.org. Fortfahren? [j/N] ",
	UnfoldProgress: "Ebene %d wird aufgeklappt: %d/%d",
	UnfoldStopped:  "Hier abgebrochen: %d weitere Referenzen wurden nicht aufgeklappt.",
	UnfoldFailed:   "konnte nicht aufgeklappt werden: %s",

	StudyNotesHeading: "Studienanmerkungen",
	ResearchHeading:   "Forschungsverzeichnis",
	StudyFailed:       "Die Studienanmerkungen zu diesem Vers konnten nicht gelesen werden: %s",

	DailyTextTitle: "Tagestext, %s",
	MeetingsTitle:  "Zusammenkünfte, Woche %d/%d",

	Loading:       "lade…",
	ErrorStatus:   "Fehler: %s",
	NoMoreItems:   "keine weiteren Einträge",
	NoMoreResults: "keine weiteren Ergebnisse",
	KeyView:       "ansehen",
	KeyDownload:   "herunterladen",
	KeyOpenLink:   "Link öffnen",
	KeyNextPage:   "nächste Seite",
	KeyPrevPage:   "vorige Seite",
	KeyBack:       "zurück",
	KeyQuit:       "beenden",
	DetailHelp:    "↑/↓ blättern · esc zurück · q beenden",

	Weekdays: [7]string{"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"},
	Months: [12]string{
		"Januar", "Februar", "März", "April", "Mai", "Juni",
		"Juli", "August", "September", "Oktober", "November", "Dezember",
	},
	FullDate: func(weekday string, day int, month string, year int) string {
		return fmt.Sprintf("%s, %d. %s %d", weekday, day, month, year)
	},
}
