// Package model holds the shared domain types produced by the API clients and
// consumed by the CLI/TUI layers. It must not depend on any api package.
package model

// Language describes one content language, bridging the two code systems the
// sites use: JW/MEPS symbols ("E", "X") in API parameters and BCP-47 locales
// ("en", "de") in URL paths.
type Language struct {
	Symbol         string `json:"symbol"`
	Locale         string `json:"locale"`
	Name           string `json:"name"`
	Vernacular     string `json:"vernacular"`
	Script         string `json:"script,omitempty"`
	RTL            bool   `json:"rtl,omitempty"`
	IsSignLanguage bool   `json:"isSignLanguage,omitempty"`
}

// PubKey identifies a publication (or a part of it) for the pub-media API.
type PubKey struct {
	Pub     string `json:"pub,omitempty"`
	DocID   int    `json:"docid,omitempty"`
	Issue   string `json:"issue,omitempty"`
	BookNum int    `json:"booknum,omitempty"`
	Track   int    `json:"track,omitempty"`
}

// Result is one row in any listing (search results, category browse, file
// lists). It is self-contained so follow-up commands (show/open/download) can
// act on it from the results cache without re-querying.
type Result struct {
	Index   int    `json:"index"`
	Kind    string `json:"kind"` // article|video|audio|publication|bible|category|file|image
	Title   string `json:"title"`
	Snippet string `json:"snippet,omitempty"`
	// Excerpt is the passage of the document a search hit sits in, as HTML:
	// what Snippet only teases. Several blocks are joined, in document order.
	Excerpt     string `json:"excerpt,omitempty"`
	Context     string `json:"context,omitempty"`
	LANK        string `json:"lank,omitempty"`
	CategoryKey string `json:"categoryKey,omitempty"` // mediator category to browse into
	DocID       int    `json:"docid,omitempty"`
	JWLink      string `json:"jwLink,omitempty"`
	WOLLink     string `json:"wolLink,omitempty"`
	FileURL     string `json:"fileUrl,omitempty"` // direct download URL when known
	Checksum    string `json:"checksum,omitempty"`
	Filesize    int64  `json:"filesize,omitempty"`
	Duration    string `json:"duration,omitempty"`
	ImageURL    string `json:"imageUrl,omitempty"`
	// Image is the metadata of an image row (kind "image"): caption, alt text,
	// rights line, pixel size. Printed even under --no-urls.
	Image *ImageMeta `json:"image,omitempty"`
	Pub   *PubKey    `json:"pub,omitempty"`
}

// SearchPage is one page of search or browse results.
type SearchPage struct {
	Query   string   `json:"query"`
	Results []Result `json:"results"`
	Total   int      `json:"total,omitempty"`
	Page    int      `json:"page"`
	Limit   int      `json:"limit,omitempty"`
	Filters []string `json:"filters,omitempty"`
	Sorts   []string `json:"sorts,omitempty"`
}

// MediaFile is one downloadable rendition of a mediator media item.
type MediaFile struct {
	URL          string  `json:"url"`
	Label        string  `json:"label,omitempty"` // "720p" etc, empty for audio
	MimeType     string  `json:"mimetype"`
	Checksum     string  `json:"checksum,omitempty"`
	SubtitlesURL string  `json:"subtitlesUrl,omitempty"`
	FrameHeight  int     `json:"frameHeight,omitempty"`
	FrameWidth   int     `json:"frameWidth,omitempty"`
	FrameRate    float64 `json:"frameRate,omitempty"`
	BitRate      float64 `json:"bitRate,omitempty"` // kbit/s
	Filesize     int64   `json:"filesize"`
	Duration     float64 `json:"duration,omitempty"`
	// Subtitled says the subtitles are burned into the picture.
	Subtitled bool `json:"subtitled,omitempty"`
}

// MediaItem is one video/audio item from the mediator API.
type MediaItem struct {
	LANK               string                       `json:"lank"`
	Type               string                       `json:"type"` // video|audio
	Title              string                       `json:"title"`
	Description        string                       `json:"description,omitempty"`
	DurationSec        float64                      `json:"durationSec,omitempty"`
	DurationFormatted  string                       `json:"duration,omitempty"`
	FirstPublished     string                       `json:"firstPublished,omitempty"`
	PrimaryCategory    string                       `json:"primaryCategory,omitempty"`
	Files              []MediaFile                  `json:"files,omitempty"`
	Images             map[string]map[string]string `json:"images,omitempty"` // type -> size -> url
	AvailableLanguages []string                     `json:"availableLanguages,omitempty"`
}

// MediaText is what a media item says in words: the document it sings or
// reads — a song's lyrics in the songbook, an article read aloud — and the
// transcript its subtitles carry. Either can be missing.
type MediaText struct {
	LANK       string         `json:"lank"`
	Document   *MediaDocument `json:"document,omitempty"`
	Transcript []Cue          `json:"transcript,omitempty"`
	// SubtitlesURL is the WebVTT file the transcript is read from.
	SubtitlesURL string `json:"subtitlesUrl,omitempty"`
}

// Empty says there is no text at all.
func (t MediaText) Empty() bool { return t.Document == nil && len(t.Transcript) == 0 }

// MediaDocument is the jw.org document a recording sings or reads.
type MediaDocument struct {
	DocID int    `json:"docid"`
	URL   string `json:"url"`
	// Pub and PubName are the publication of the recording the document was
	// found by: "sjjm", "“Sing Out Joyfully” to Jehovah—Meetings".
	Pub     string `json:"pub,omitempty"`
	PubName string `json:"pubName,omitempty"`
	// Context is the line over the title: "SONG 1".
	Context string `json:"context,omitempty"`
	Title   string `json:"title"`
	// Theme is the scripture a song is written on: "(Revelation 4:11)";
	// Closing what follows the text: "(See also Ps. 36:9; ...)".
	Theme   string      `json:"theme,omitempty"`
	Blocks  []TextBlock `json:"blocks"`
	Closing string      `json:"closing,omitempty"`
	// PrintedEdition is the picture of the page as printed, with the notes of
	// a song.
	PrintedEdition string `json:"printedEdition,omitempty"`
	// Downloads are the files the page offers with the text: a lead sheet.
	Downloads []Link `json:"downloads,omitempty"`
	// Synced says the lines carry their times in the recording the item
	// plays.
	Synced bool `json:"synced,omitempty"`
}

// Lyrics says the document is a song's: it is made of stanzas.
func (d MediaDocument) Lyrics() bool {
	for _, b := range d.Blocks {
		if b.Kind == BlockStanza {
			return true
		}
	}
	return false
}

// The kinds of a TextBlock.
const (
	BlockStanza    = "stanza"
	BlockChorus    = "chorus"
	BlockHeading   = "heading"
	BlockParagraph = "paragraph"
)

// TextBlock is a stanza or chorus of a song, or a heading or paragraph of
// any other document.
type TextBlock struct {
	Kind string `json:"kind"`
	// Label is a stanza's number ("1.") or a chorus's name ("(CHORUS)").
	Label string     `json:"label,omitempty"`
	Lines []TextLine `json:"lines"`
}

// TextLine is one paragraph of the page: a line of a song. Start and End are
// where the recording sings it, in seconds, when it is known.
type TextLine struct {
	PID   int     `json:"pid,omitempty"`
	Text  string  `json:"text"`
	Start float64 `json:"start,omitempty"`
	End   float64 `json:"end,omitempty"`
}

// Cue is one caption of a transcript, Start and End in seconds.
type Cue struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

// Link is a labelled URL.
type Link struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// Category is a mediator category tree node.
type Category struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Type        string `json:"type"` // container|ondemand
	// Images are the category's own pictures, type -> size -> url, as a
	// media item's.
	Images        map[string]map[string]string `json:"images,omitempty"`
	Subcategories []Category                   `json:"subcategories,omitempty"`
	Media         []MediaItem                  `json:"media,omitempty"`
	Total         int                          `json:"total,omitempty"`
	Parent        *CategoryRef                 `json:"parent,omitempty"`
}

// CategoryRef names a category without its contents: the parent of another.
type CategoryRef struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// PubFile is one downloadable publication file (PDF, EPUB, MP3 track, ...).
type PubFile struct {
	Title    string `json:"title"`
	URL      string `json:"url"`
	Checksum string `json:"checksum,omitempty"`
	Label    string `json:"label,omitempty"`
	MimeType string `json:"mimetype,omitempty"`
	Format   string `json:"format"` // PDF, EPUB, MP3, ...
	Track    int    `json:"track,omitempty"`
	DocID    int    `json:"docid,omitempty"`
	BookNum  int    `json:"booknum,omitempty"`
	Filesize int64  `json:"filesize"`
	// Duration is the length of an audio or video file in seconds, Modified
	// when the file last changed ("2026-10-01 20:46:48"), ImageURL a track's
	// own picture when it has one.
	Duration float64 `json:"duration,omitempty"`
	Modified string  `json:"modified,omitempty"`
	ImageURL string  `json:"imageUrl,omitempty"`
	// Markers time a recording against the document it reads or sings.
	Markers *Markers `json:"markers,omitempty"`
}

// Markers say where in a recording each paragraph of the document it reads or
// sings begins, in seconds.
type Markers struct {
	DocID      int          `json:"docid"`
	Paragraphs []ParaMarker `json:"paragraphs"`
}

// ParaMarker is one paragraph of a document, by its id on the page, as a
// recording reads it.
type ParaMarker struct {
	PID      int     `json:"pid"`
	Start    float64 `json:"start"`
	Duration float64 `json:"duration"`
}

// PubMedia is the pub-media API response for one publication.
type PubMedia struct {
	PubName       string                          `json:"pubName"`
	ParentPubName string                          `json:"parentPubName,omitempty"`
	Pub           string                          `json:"pub"`
	Issue         string                          `json:"issue,omitempty"`
	BookNum       int                             `json:"booknum,omitempty"`
	Files         map[string]map[string][]PubFile `json:"files"` // lang symbol -> format -> files
	Languages     map[string]Language             `json:"languages,omitempty"`
}

// Verse is a single bible verse with its rendered HTML content.
type Verse struct {
	ID       int    `json:"id"` // BBCCCVVV
	Citation string `json:"citation,omitempty"`
	HTML     string `json:"html"`
}

// StudyNote is one study note attached to a verse (nwtsty).
type StudyNote struct {
	Lemma string `json:"lemma,omitempty"` // the bolded phrase the note explains
	HTML  string `json:"html"`
}

// CrossRef is one marginal-reference group of a verse.
type CrossRef struct {
	Citation     string `json:"citation"`             // e.g. "Ge 22:2, 16; Joh 1:14"
	SourcePath   string `json:"sourcePath,omitempty"` // wol marginalreference path for lazy resolve
	ResolvedHTML string `json:"resolvedHtml,omitempty"`
}

// MediaAsset is an image (or linked clip) with caption, e.g. verse media or
// article figures.
//
// Everything past URL is metadata the picture carries in the page that
// references it: the sites strip EXIF/IPTC from the image files themselves, so
// the caption, the alt text, the rights line and the pixel size are only ever
// readable from the HTML (or, for study-bible media, from the gallery page the
// thumbnail links to). It is collected whether or not the URLs are printed —
// --no-urls hides targets, not the picture's description.
type MediaAsset struct {
	URL          string `json:"url"`
	ThumbnailURL string `json:"thumbnailUrl,omitempty"` // small rendition, when URL is the large one
	Caption      string `json:"caption,omitempty"`
	Alt          string `json:"alt,omitempty"`
	Credit       string `json:"credit,omitempty"`      // rights line printed beside the picture
	Description  string `json:"description,omitempty"` // the long caption of a gallery item
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
	SourceURL    string `json:"sourceUrl,omitempty"`  // page carrying the metadata (wol gallery item)
	FinderLink   string `json:"finderLink,omitempty"` // jw.org finder deep link (videos w/ timestamps)
}

// Meta condenses the asset's metadata into the record a listing row carries.
func (m MediaAsset) Meta() *ImageMeta {
	im := ImageMeta{
		Caption: m.Caption, Alt: m.Alt, Credit: m.Credit,
		Description: m.Description, Width: m.Width, Height: m.Height,
	}
	if im == (ImageMeta{}) {
		return nil
	}
	return &im
}

// ImageMeta is what is known about an image besides where it is: the words
// printed with it and its pixel size. Carried by an image result so a listing,
// jw show and -o json can all report it, with or without --no-urls.
type ImageMeta struct {
	Caption     string `json:"caption,omitempty"`
	Alt         string `json:"alt,omitempty"`
	Credit      string `json:"credit,omitempty"`
	Description string `json:"description,omitempty"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
}

// Label is the best one-line name for the image: the caption, else the alt
// text, else the rights line. Empty when the image says nothing about itself,
// in which case a listing falls back to its index.
func (m ImageMeta) Label() string {
	for _, s := range []string{m.Caption, m.Alt, m.Credit} {
		if s != "" {
			return s
		}
	}
	return ""
}

// ResearchItem is one research-guide reference on a verse: a link to a
// publication passage discussing it.
type ResearchItem struct {
	Title  string `json:"title"`
	Source string `json:"source,omitempty"` // publication line, e.g. "it-2 528"
	// Kind is the index the entry was listed under: ResearchGuideItem or
	// PublicationIndexItem. wol marks the group with a class of its own, which
	// is the same in every language, while Source is the localized heading the
	// group carries.
	Kind        string `json:"kind,omitempty"`
	PCPath      string `json:"pcPath,omitempty"` // wol /pc/ path for the excerpt
	ExcerptHTML string `json:"excerptHtml,omitempty"`
	ArticleURL  string `json:"articleUrl,omitempty"`
}

// SameTarget reports whether href points at what this entry already points at.
// An index spells one entry over several links — the publication name, then
// the issue, then the page — all with the same target, and they are one entry.
func (r ResearchItem) SameTarget(href string) bool {
	if href == "" {
		return false
	}
	return href == r.PCPath || href == r.ArticleURL
}

// The indexes a research entry can come from. Both point at publications
// discussing a verse, and the same passage is regularly listed in each: the
// research guide spells the publication out ("Insight, Volume 1, page 1044"),
// the publications index cites it by symbol ("it-1 1044").
const (
	ResearchGuideItem    = "researchGuide"
	PublicationIndexItem = "publicationIndex"
)

// StudySection groups everything the study bible attaches to one verse.
type StudySection struct {
	Verse     string         `json:"verse"` // citation, e.g. "John 3:16"
	Notes     []StudyNote    `json:"notes,omitempty"`
	XRefs     []CrossRef     `json:"xrefs,omitempty"`
	Media     []MediaAsset   `json:"media,omitempty"`
	Research  []ResearchItem `json:"research,omitempty"`
	Footnotes []string       `json:"footnotes,omitempty"`
}

// ScriptureAnchor is a bible reference found inside an article.
type ScriptureAnchor struct {
	Text   string `json:"text"`             // link text, e.g. "Matt 24:14"
	BCPath string `json:"bcPath,omitempty"` // wol /bc/ tooltip path
	BID    string `json:"bid,omitempty"`
}

// Article is a parsed document (wol /d/ page or www.jw.org article).
type Article struct {
	DocID         int               `json:"docid,omitempty"`
	Title         string            `json:"title"`
	URL           string            `json:"url,omitempty"`
	HTML          string            `json:"html"`
	Images        []MediaAsset      `json:"images,omitempty"`
	ScriptureRefs []ScriptureAnchor `json:"scriptureRefs,omitempty"`
}

// Tooltip is the JSON payload of wol's bc/pc citation endpoints.
type Tooltip struct {
	Title            string `json:"title"`
	Caption          string `json:"caption,omitempty"`
	ContentHTML      string `json:"contentHtml"`
	URL              string `json:"url,omitempty"`
	ImageURL         string `json:"imageUrl,omitempty"`
	PublicationTitle string `json:"publicationTitle,omitempty"`
}
