package wol

import (
	"bytes"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Editions tells apart the editions of the library wol.jw.org serves. Every
// page names the library it is from in a hidden field — "Deutsche
// Publikationen (1950-2027)" — and not all of wol's servers have the newest:
// for weeks after a new library year, some answer with last year's, where a
// new publication is missing or an old one stands in its place. The library's
// name is the scope, its last year the edition.
type Editions struct {
	// Host is wol's host, wol.jw.org; only its pages are told apart.
	Host string
}

var (
	libTitleField = regexp.MustCompile(`id="libTitle"[^>]*?value="([^"]*)"`)
	libTitleYears = regexp.MustCompile(`^(.*?)\s*\(\s*(?:\d{4}\s*[-–]\s*)?(\d{4})\s*\)\s*$`)
)

// Edition implements httpx.Editions.
func (e Editions) Edition(rawURL string, body []byte) (string, int, bool) {
	if !e.ours(rawURL) || !bytes.Contains(body, []byte(`id="libTitle"`)) {
		return "", 0, false
	}
	m := libTitleField.FindSubmatch(body)
	if m == nil {
		return "", 0, false
	}
	return libraryEdition(html.UnescapeString(string(m[1])))
}

// libraryEdition reads a library's name and last year out of its title.
func libraryEdition(title string) (string, int, bool) {
	m := libTitleYears.FindStringSubmatch(strings.TrimSpace(title))
	if m == nil || m[1] == "" {
		return "", 0, false
	}
	year, err := strconv.Atoi(m[2])
	// a year far ahead is no library year; taking it would have every page
	// seem out of date
	if err != nil || year < 1900 || year > time.Now().Year()+3 {
		return "", 0, false
	}
	return m[1], year, true
}

// bustSeq tells apart the addresses Bust makes within one process.
var bustSeq atomic.Uint64

// bustParam is the query parameter that keeps a request from the CDN's
// store; wol ignores it.
const bustParam = "_"

// Bust implements httpx.Editions: the address with a query parameter the CDN
// has never seen.
func (e Editions) Bust(rawURL string, try int) string {
	if !e.ours(rawURL) {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	q := u.Query()
	q.Set(bustParam, fmt.Sprintf("%d-%d-%d", time.Now().UnixNano()%1e9, bustSeq.Add(1), try))
	u.RawQuery = q.Encode()
	return u.String()
}

func (e Editions) ours(rawURL string) bool {
	if e.Host == "" {
		return false
	}
	u, err := url.Parse(rawURL)
	return err == nil && strings.EqualFold(u.Host, e.Host)
}
