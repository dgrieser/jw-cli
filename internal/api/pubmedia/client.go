// Package pubmedia is a client for the publication media links API
// (GETPUBMEDIALINKS), which returns download URLs for publications in all
// formats: PDF, EPUB, JWPUB, RTF, MP3, MP4, AAC, ZIP, ...
package pubmedia

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/model"
)

// ErrNotFound is returned when the API reports no media for the query.
var ErrNotFound = errors.New("publication not found")

type Client struct {
	hc *httpx.Client
}

func New(hc *httpx.Client) *Client { return &Client{hc: hc} }

// Query identifies the publication (or part) to fetch links for.
type Query struct {
	Pub      string   // publication symbol: w, g, nwt, sjj, ...
	DocID    int      // alternative to Pub: MEPS document id
	Issue    string   // YYYYMM for periodicals
	BookNum  int      // bible book 1-66
	Track    int      // audio track / chapter
	Formats  []string // PDF, EPUB, MP3, ... (empty = all common formats)
	Lang     string   // JW language symbol (required)
	AllLangs bool
}

// DefaultFormats requested when the user does not narrow the format down.
var DefaultFormats = []string{"PDF", "EPUB", "JWPUB", "RTF", "MP3", "MP4", "AAC", "ZIP"}

type wireFile struct {
	Title string `json:"title"`
	File  struct {
		URL      string `json:"url"`
		Checksum string `json:"checksum"`
	} `json:"file"`
	Filesize int64   `json:"filesize"`
	Label    string  `json:"label"`
	Track    flexNum `json:"track"`
	DocID    flexNum `json:"docid"`
	BookNum  flexNum `json:"booknum"`
	MimeType string  `json:"mimetype"`
}

// Links queries GETPUBMEDIALINKS and returns the available files grouped by
// language symbol and format.
//
// A symbol written the way the files are named, with the language attached
// ("wcg_X", "wcg-X"), is not one the API knows; when it is refused and ends in
// the language asked for, the bare symbol is tried instead.
func (c *Client) Links(ctx context.Context, q Query) (model.PubMedia, error) {
	pm, err := c.links(ctx, q)
	if err != nil && errors.Is(err, ErrNotFound) {
		if bare, ok := stripLangSuffix(q.Pub, q.Lang); ok {
			retry := q
			retry.Pub = bare
			if pm, rerr := c.links(ctx, retry); rerr == nil {
				return pm, nil
			}
		}
	}
	return pm, err
}

// stripLangSuffix drops a trailing "_X" or "-X" from a publication symbol when
// X is the language symbol lang.
func stripLangSuffix(pub, lang string) (string, bool) {
	if pub == "" || lang == "" {
		return "", false
	}
	for _, sep := range []string{"_", "-"} {
		if bare, ok := strings.CutSuffix(strings.ToLower(pub), strings.ToLower(sep+lang)); ok && bare != "" {
			return pub[:len(bare)], true
		}
	}
	return "", false
}

func (c *Client) links(ctx context.Context, q Query) (model.PubMedia, error) {
	if q.Lang == "" {
		return model.PubMedia{}, errors.New("pubmedia: language symbol required")
	}
	if q.Pub == "" && q.DocID == 0 {
		return model.PubMedia{}, errors.New("pubmedia: pub symbol or docid required")
	}
	formats := q.Formats
	if len(formats) == 0 {
		formats = DefaultFormats
	}
	v := url.Values{}
	v.Set("output", "json")
	v.Set("langwritten", q.Lang)
	v.Set("txtCMSLang", q.Lang)
	v.Set("fileformat", strings.Join(formats, ","))
	if q.AllLangs {
		v.Set("alllangs", "1")
	} else {
		v.Set("alllangs", "0")
	}
	if q.Pub != "" {
		v.Set("pub", q.Pub)
	}
	if q.DocID != 0 {
		v.Set("docid", strconv.Itoa(q.DocID))
	}
	if q.Issue != "" {
		v.Set("issue", q.Issue)
	}
	if q.BookNum != 0 {
		v.Set("booknum", strconv.Itoa(q.BookNum))
	}
	if q.Track != 0 {
		v.Set("track", strconv.Itoa(q.Track))
	}
	u := c.hc.Base.CDN + "/apis/pub-media/GETPUBMEDIALINKS?" + v.Encode()

	var resp struct {
		PubName       string  `json:"pubName"`
		ParentPubName string  `json:"parentPubName"`
		Pub           string  `json:"pub"`
		Issue         flexNum `json:"issue"`
		BookNum       flexNum `json:"booknum"`
		Languages     map[string]struct {
			Name   string `json:"name"`
			Locale string `json:"locale"`
		} `json:"languages"`
		Files map[string]map[string][]wireFile `json:"files"`
		// error envelope on bad requests
		Err *struct {
			Title  string `json:"title"`
			Status string `json:"status"`
		} `json:"0"`
	}
	if err := c.hc.GetJSON(ctx, u, nil, &resp); err != nil {
		var se *httpx.StatusError
		// an unknown symbol is answered with 400 rather than 404
		if errors.As(err, &se) && (se.StatusCode == 404 || se.StatusCode == 400) {
			return model.PubMedia{}, fmt.Errorf("%w: %s", ErrNotFound, describe(q))
		}
		return model.PubMedia{}, err
	}
	if len(resp.Files) == 0 {
		if resp.Err != nil && resp.Err.Title != "" {
			return model.PubMedia{}, fmt.Errorf("%w: %s (%s)", ErrNotFound, describe(q), resp.Err.Title)
		}
		return model.PubMedia{}, fmt.Errorf("%w: %s", ErrNotFound, describe(q))
	}

	pm := model.PubMedia{
		PubName:       resp.PubName,
		ParentPubName: resp.ParentPubName,
		Pub:           resp.Pub,
		Issue:         resp.Issue.String(),
		Files:         map[string]map[string][]model.PubFile{},
		Languages:     map[string]model.Language{},
	}
	if pm.Issue == "0" {
		pm.Issue = ""
	}
	pm.BookNum, _ = atoiNum(resp.BookNum)
	for sym, l := range resp.Languages {
		pm.Languages[sym] = model.Language{Symbol: sym, Name: l.Name, Locale: l.Locale}
	}
	for sym, byFormat := range resp.Files {
		pm.Files[sym] = map[string][]model.PubFile{}
		for format, files := range byFormat {
			out := make([]model.PubFile, 0, len(files))
			for _, f := range files {
				pf := model.PubFile{
					Title:    f.Title,
					URL:      f.File.URL,
					Checksum: f.File.Checksum,
					Label:    f.Label,
					MimeType: f.MimeType,
					Format:   format,
					Filesize: f.Filesize,
				}
				pf.Track, _ = atoiNum(f.Track)
				pf.DocID, _ = atoiNum(f.DocID)
				pf.BookNum, _ = atoiNum(f.BookNum)
				out = append(out, pf)
			}
			pm.Files[sym][format] = out
		}
	}
	return pm, nil
}

// flexNum is a numeric field the API sends in whatever shape it likes: a
// number (202405), a string ("202405"), an empty string or null when the
// publication has none. json.Number refuses the empty string, which made every
// non-periodical (wcg, lff, ...) fail to decode.
type flexNum string

func (n *flexNum) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" {
		*n = ""
		return nil
	}
	if strings.HasPrefix(s, `"`) {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		*n = flexNum(strings.TrimSpace(str))
		return nil
	}
	var num json.Number
	if err := json.Unmarshal(b, &num); err != nil {
		return err
	}
	*n = flexNum(num)
	return nil
}

func (n flexNum) String() string { return string(n) }

func atoiNum(n flexNum) (int, error) {
	if n == "" {
		return 0, nil
	}
	i, err := strconv.Atoi(string(n))
	return i, err
}

func describe(q Query) string {
	var parts []string
	if q.Pub != "" {
		parts = append(parts, "pub "+q.Pub)
	}
	if q.DocID != 0 {
		parts = append(parts, fmt.Sprintf("docid %d", q.DocID))
	}
	if q.Issue != "" {
		parts = append(parts, "issue "+q.Issue)
	}
	if q.BookNum != 0 {
		parts = append(parts, fmt.Sprintf("book %d", q.BookNum))
	}
	parts = append(parts, "language "+q.Lang)
	return strings.Join(parts, ", ")
}
