package server

import (
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/dgrieser/jw-cli/internal/httpx"
)

// maxImage bounds what one picture may weigh: the largest renditions wol
// and jw.org serve are well below it.
const maxImage = 25 << 20

// imageHost reports whether a picture may be fetched through this server:
// only jw.org's own sites and CDN, so the route opens nothing else.
func imageHost(h string) bool {
	h = strings.ToLower(h)
	for _, site := range []string{"jw.org", "jw-cdn.org"} {
		if h == site || strings.HasSuffix(h, "."+site) {
			return true
		}
	}
	return false
}

// imageSource reports whether the server reads pictures off u: one of jw.org's
// sites over https, or a site the server is set to read jw.org from.
func (s *Server) imageSource(u *url.URL) bool {
	if u.Scheme == "https" && imageHost(u.Hostname()) {
		return true
	}
	b := s.svc.HTTP.Base
	for _, base := range []string{b.WOL, b.JWOrg, b.CDN} {
		if bu, err := url.Parse(base); err == nil && bu.Host != "" && bu.Scheme == u.Scheme && strings.EqualFold(bu.Host, u.Host) {
			return true
		}
	}
	return false
}

// image passes a picture of jw.org on to the page that shows it. The pages
// link pictures where jw.org keeps them, and the browser loads them from
// there; wol.jw.org sits behind bot management that may refuse a browser that
// never opened one of its pages, and then the page's script (app.js) asks for
// the picture here instead, where the server reads it the way it reads wol's
// pages.
func (s *Server) image(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("u")
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || !s.imageSource(u) {
		http.Error(w, "not a picture of jw.org", http.StatusBadRequest)
		return
	}
	hdr := http.Header{}
	hdr.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	hdr.Set("Sec-Fetch-Dest", "image")
	hdr.Set("Sec-Fetch-Mode", "no-cors")
	hdr.Set("Sec-Fetch-Site", "same-origin")
	hdr.Set("Referer", u.Scheme+"://"+u.Host+"/")
	resp, err := s.svc.HTTP.Get(r.Context(), u.String(), hdr)
	if err != nil {
		status := http.StatusBadGateway
		if se, ok := err.(*httpx.StatusError); ok && se.StatusCode == http.StatusNotFound {
			status = http.StatusNotFound
		}
		s.logf("image %s: %v", u, err)
		http.Error(w, "the picture could not be read from jw.org", status)
		return
	}
	defer resp.Body.Close()
	if resp.ContentLength > maxImage {
		http.Error(w, "the picture is too large", http.StatusBadGateway)
		return
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") {
		// a challenge page or an error dressed as success: not a picture
		s.logf("image %s: answered with %q", u, ct)
		http.Error(w, "jw.org did not answer with a picture", http.StatusBadGateway)
		return
	}
	h := w.Header()
	h.Set("Content-Type", ct)
	h.Set("X-Content-Type-Options", "nosniff")
	// a picture under its address does not change: the browser keeps it,
	// for this reader only, as the pages are
	h.Set("Cache-Control", "private, max-age=604800, immutable")
	if n := resp.Header.Get("Content-Length"); n != "" {
		h.Set("Content-Length", n)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, io.LimitReader(resp.Body, maxImage))
}
