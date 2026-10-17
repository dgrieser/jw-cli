package service

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/dgrieser/jw-cli/internal/model"
)

// MediaCode names a video or a recording in short, the way a citation names a
// publication by its symbol and year ("w26"): "vi-" for a video or "au-" for
// a recording, the series it is part of, and the year it came out —
// "vi-jwb23", "au-sjjm17", "vi-co-r22".
//
// A publication's media (pub-jwb-136_5_VIDEO) are named by the publication's
// symbol, without the issue it carries (jwb-136, jwb_201801, co-r22: jwb,
// jwb, co-r). A document's (docid-502100035_1_VIDEO) carry no symbol: they
// are named by their category instead (mediaCategoryCodes), "jw" where it is
// not one of them. The year is left out where the item gives none.
func MediaCode(item model.MediaItem) string {
	kind := "vi-"
	if strings.HasSuffix(item.LANK, "_AUDIO") || item.Type == "audio" {
		kind = "au-"
	}
	code := "jw"
	if sym, ok := strings.CutPrefix(item.LANK, "pub-"); ok {
		if s := mediaSymbol(sym); s != "" {
			code = s
		}
	} else if c, ok := mediaCategoryCodes[strings.TrimSuffix(item.PrimaryCategory, "AD")]; ok {
		code = c
	}
	if t, err := time.Parse(time.RFC3339, item.FirstPublished); err == nil {
		code += fmt.Sprintf("%02d", t.Year()%100)
	}
	return kind + code
}

// mediaTrack is what follows a publication's symbol in a media key: the
// track, and whether it is a video or a recording ("_5_VIDEO", "_x_VIDEO").
var mediaTrack = regexp.MustCompile(`(_[0-9a-z]+)?_(VIDEO|AUDIO)$`)

// mediaIssue is the issue a symbol names after the series: a year and month
// ("_201801"), a running number ("-136"), or a year ("22" of "co-r22").
var mediaIssue = regexp.MustCompile(`([_-]\d+|\d+)$`)

// mediaSymbol is the series of a publication's media key, without "pub-":
// "jwb" of "jwb-136_5_VIDEO", "t-famv" of "t-famv_101_VIDEO".
func mediaSymbol(key string) string {
	sym := mediaTrack.ReplaceAllString(key, "")
	return strings.ToLower(mediaIssue.ReplaceAllString(sym, ""))
}

// mediaCategoryCodes name the categories of the media a document of jw.org
// carries, by the category's key without the "AD" of its audio-described
// copy. None of them is a publication's symbol, which a citation would take
// it for.
var mediaCategoryCodes = map[string]string{
	"StudioNewsReports":             "lb",
	"VODStudio":                     "lb", // VODStudioAD: the reports, described
	"":                              "lb", // a described report with no category
	"SeriesWebsiteAppHelp":          "jwh",
	"SeriesWhiteboard":              "wba",
	"VODBibleMedia":                 "bmv",
	"SeriesWasItDesigned":           "wid",
	"VODActivitiesConstruction":     "con",
	"SeriesBibleTeachings":          "btv",
	"VODSeriesBibleTeachings":       "btv",
	"VODOrgLegal":                   "leg",
	"SeriesWhatPeersSay":            "wps",
	"VODBibleTranslations":          "btr",
	"VODOrgBloodlessMedicine":       "med",
	"SeriesBibleChangesLives":       "bcl",
	"VODActivitiesSpecialEvents":    "sev",
	"VODIntExpEndurance":            "end",
	"VODActivitiesReliefWork":       "rel",
	"VODPgmEvtGilead":               "gil",
	"VODActivitiesPrintingShipping": "prt",
	"VODMinistryMethods":            "mim",
	"VODOrgHistory":                 "his",
	"VODOrgBethel":                  "bet",
	"AccomplishMinistry":            "acm",
	"FamilyWorship":                 "fam",
	"VODPgmEvtAnnMtg":               "amt",
	"MakingMusic":                   "mus",
	"VODMinistryTools":              "mit",
	"VODMinistryExpandMinistry":     "exm",
	"2018Convention":                "co",
	"2019Convention":                "co",
	"Reports":                       "rep",
	"VODActivitiesTranslation":      "trn",
	"VODLFFVideos":                  "lffv",
}
