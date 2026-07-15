// Package search is a client for the jw.org unified search API
// (b.jw-cdn.org/apis/search), which covers articles, publications, videos,
// audio, and bible hits. It is protected by an anonymously issued JWT.
package search

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/model"
)

// Facets accepted by the API.
var Facets = []string{"all", "publications", "videos", "audio", "bible", "indexes"}

type Client struct {
	hc     *httpx.Client
	tokens *TokenSource
}

func New(hc *httpx.Client) *Client {
	c := &Client{hc: hc}
	c.tokens = NewTokenSource(func(ctx context.Context) (string, error) {
		return hc.GetText(ctx, hc.Base.CDN+"/tokens/jworg.jwt", nil)
	})
	return c
}

type Params struct {
	Query  string
	Facet  string // one of Facets; "" = all
	Sort   string // ""/rel, newest, oldest
	Offset int
	Limit  int
}

type wireResult struct {
	Type     string            `json:"type"`
	Subtype  string            `json:"subtype"`
	Title    string            `json:"title"`
	Snippet  string            `json:"snippet"`
	Context  string            `json:"context"`
	LANK     string            `json:"lank"`
	Duration string            `json:"duration"`
	Links    map[string]string `json:"links"`
	Image    *struct {
		URL     string `json:"url"`
		AltText string `json:"altText"`
	} `json:"image"`
	Results []wireResult `json:"results"` // for type=group
}

// Search performs one search request, refreshing the token once on 401.
func (c *Client) Search(ctx context.Context, jwLang string, p Params) (model.SearchPage, error) {
	facet := p.Facet
	if facet == "" {
		facet = "all"
	}
	valid := false
	for _, f := range Facets {
		if f == facet {
			valid = true
		}
	}
	if !valid {
		return model.SearchPage{}, fmt.Errorf("invalid search type %q (want one of %s)", facet, strings.Join(Facets, ", "))
	}
	sort := p.Sort
	switch sort {
	case "", "rel", "relevance":
		sort = ""
	case "newest", "oldest":
	default:
		return model.SearchPage{}, fmt.Errorf("invalid sort %q (want rel, newest, or oldest)", p.Sort)
	}

	v := url.Values{}
	v.Set("q", p.Query)
	v.Set("sort", sort)
	if p.Offset > 0 {
		v.Set("offset", strconv.Itoa(p.Offset))
	}
	if p.Limit > 0 {
		v.Set("limit", strconv.Itoa(p.Limit))
	}
	u := fmt.Sprintf("%s/apis/search/results/%s/%s?%s", c.hc.Base.CDN, url.PathEscape(jwLang), facet, v.Encode())

	var resp struct {
		Results []wireResult `json:"results"`
		Insight struct {
			Total struct {
				Value int `json:"value"`
			} `json:"total"`
		} `json:"insight"`
		Filters []struct {
			Label string `json:"label"`
		} `json:"filters"`
		Sorts []struct {
			Label string `json:"label"`
		} `json:"sorts"`
	}
	err := c.getAuthed(ctx, u, &resp)
	if err != nil {
		return model.SearchPage{}, err
	}

	page := model.SearchPage{
		Query: p.Query,
		Total: resp.Insight.Total.Value,
		Page:  p.Offset/max(p.Limit, 1) + 1,
		Limit: p.Limit,
	}
	for _, f := range resp.Filters {
		page.Filters = append(page.Filters, f.Label)
	}
	for _, s := range resp.Sorts {
		page.Sorts = append(page.Sorts, s.Label)
	}
	for _, w := range flatten(resp.Results) {
		page.Results = append(page.Results, w.toModel())
	}
	return page, nil
}

// getAuthed performs an authorized GET, retrying once with a fresh token on 401.
func (c *Client) getAuthed(ctx context.Context, u string, out any) error {
	for attempt := 0; ; attempt++ {
		tok, err := c.tokens.Token(ctx)
		if err != nil {
			return fmt.Errorf("fetch search token: %w", err)
		}
		hdr := http.Header{}
		hdr.Set("Authorization", "Bearer "+tok)
		hdr.Set("Accept", "application/json; charset=utf-8")
		hdr.Set("Referer", "https://www.jw.org/")
		err = c.hc.GetJSON(ctx, u, hdr, out)
		var se *httpx.StatusError
		if errors.As(err, &se) && se.StatusCode == http.StatusUnauthorized && attempt == 0 {
			c.tokens.Invalidate()
			continue
		}
		return err
	}
}

func flatten(in []wireResult) []wireResult {
	var out []wireResult
	for _, r := range in {
		if r.Type == "group" {
			out = append(out, flatten(r.Results)...)
			continue
		}
		out = append(out, r)
	}
	return out
}

var lankDocID = regexp.MustCompile(`(\d+)$`)

func (w wireResult) toModel() model.Result {
	r := model.Result{
		Kind:     w.Subtype,
		Title:    w.Title,
		Snippet:  w.Snippet,
		Context:  w.Context,
		LANK:     w.LANK,
		Duration: w.Duration,
		JWLink:   w.Links["jw.org"],
		WOLLink:  w.Links["wol"],
	}
	if r.Kind == "" {
		r.Kind = "article"
	}
	if w.Image != nil {
		r.ImageURL = w.Image.URL
	}
	// lank like "pa-1102025912": numeric tail is the MEPS docId
	if m := lankDocID.FindString(w.LANK); m != "" {
		r.DocID, _ = strconv.Atoi(m)
	}
	return r
}
