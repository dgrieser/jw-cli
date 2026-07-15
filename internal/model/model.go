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
	Index    int     `json:"index"`
	Kind     string  `json:"kind"` // article|video|audio|publication|bible|category|file|image
	Title    string  `json:"title"`
	Snippet  string  `json:"snippet,omitempty"`
	Context  string  `json:"context,omitempty"`
	LANK     string  `json:"lank,omitempty"`
	DocID    int     `json:"docid,omitempty"`
	JWLink   string  `json:"jwLink,omitempty"`
	WOLLink  string  `json:"wolLink,omitempty"`
	FileURL  string  `json:"fileUrl,omitempty"` // direct download URL when known
	Duration string  `json:"duration,omitempty"`
	ImageURL string  `json:"imageUrl,omitempty"`
	Pub      *PubKey `json:"pub,omitempty"`
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
	Filesize     int64   `json:"filesize"`
	Duration     float64 `json:"duration,omitempty"`
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

// Category is a mediator category tree node.
type Category struct {
	Key           string      `json:"key"`
	Name          string      `json:"name"`
	Description   string      `json:"description,omitempty"`
	Type          string      `json:"type"` // container|ondemand
	Subcategories []Category  `json:"subcategories,omitempty"`
	Media         []MediaItem `json:"media,omitempty"`
	Total         int         `json:"total,omitempty"`
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
type MediaAsset struct {
	URL        string `json:"url"`
	Caption    string `json:"caption,omitempty"`
	Alt        string `json:"alt,omitempty"`
	FinderLink string `json:"finderLink,omitempty"` // jw.org finder deep link (videos w/ timestamps)
}

// ResearchItem is one research-guide reference on a verse: a link to a
// publication passage discussing it.
type ResearchItem struct {
	Title       string `json:"title"`
	Source      string `json:"source,omitempty"` // publication line, e.g. "it-2 528"
	PCPath      string `json:"pcPath,omitempty"` // wol /pc/ path for the excerpt
	ExcerptHTML string `json:"excerptHtml,omitempty"`
	ArticleURL  string `json:"articleUrl,omitempty"`
}

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
