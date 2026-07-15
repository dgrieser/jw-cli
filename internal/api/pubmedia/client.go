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
	Filesize int64       `json:"filesize"`
	Label    string      `json:"label"`
	Track    json.Number `json:"track"`
	DocID    json.Number `json:"docid"`
	BookNum  json.Number `json:"booknum"`
	MimeType string      `json:"mimetype"`
}

// Links queries GETPUBMEDIALINKS and returns the available files grouped by
// language symbol and format.
func (c *Client) Links(ctx context.Context, q Query) (model.PubMedia, error) {
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
		PubName       string      `json:"pubName"`
		ParentPubName string      `json:"parentPubName"`
		Pub           string      `json:"pub"`
		Issue         json.Number `json:"issue"`
		BookNum       json.Number `json:"booknum"`
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
		if errors.As(err, &se) && se.StatusCode == 404 {
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

func atoiNum(n json.Number) (int, error) {
	if n == "" {
		return 0, nil
	}
	i, err := n.Int64()
	return int(i), err
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
