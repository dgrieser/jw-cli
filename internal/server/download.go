package server

import (
	"net/http"

	"github.com/dgrieser/jw-cli/internal/download"
	"github.com/dgrieser/jw-cli/internal/service"
)

// downloadMedia picks a rendition of a media item — the quality selection the
// CLI's download command does — and redirects to its public CDN URL. The
// server never proxies the bytes: the URLs are unauthenticated, and a redirect
// keeps range requests and resume with the CDN where they belong.
func (s *Server) downloadMedia(w http.ResponseWriter, r *http.Request) {
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	item, err := s.svc.Mediator.MediaItem(r.Context(), lng.Symbol, r.PathValue("lank"))
	if err != nil {
		failJSON(w, r, err)
		return
	}
	file, err := download.PickVideo(item.Files, valueOr(r, "quality", "best"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	target := file.URL
	if boolParam(r, "subtitles") {
		if file.SubtitlesURL == "" {
			writeError(w, http.StatusNotFound, "not_found", "no subtitles for "+item.LANK)
			return
		}
		target = file.SubtitlesURL
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// downloadPub resolves a publication query to its files: one match redirects
// to the file, several come back as a listing to pick a fileUrl from.
func (s *Server) downloadPub(w http.ResponseWriter, r *http.Request) {
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	q, err := pubQuery(r, lng)
	if err != nil {
		badRequest(w, "%s", err)
		return
	}
	pm, err := s.svc.PubMedia.Links(r.Context(), q)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	files := service.PubFilesToResults(pm)
	switch len(files) {
	case 0:
		writeError(w, http.StatusNotFound, "not_found", "no files matched")
	case 1:
		http.Redirect(w, r, files[0].FileURL, http.StatusFound)
	default:
		// several files match: not an error, but nothing to redirect to either
		writeJSON(w, http.StatusMultipleChoices, pubResponse{
			PubName: pm.PubName, ParentPubName: pm.ParentPubName,
			Pub: pm.Pub, Issue: pm.Issue, BookNum: pm.BookNum, Files: files,
		})
	}
}
