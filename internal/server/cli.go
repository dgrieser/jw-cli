package server

import (
	"encoding/json"
	"mime"
	"net/http"

	"github.com/dgrieser/jw-cli/internal/remote"
)

// maxCLIRequest bounds the body of POST /api/v1/cli: a command line and the
// client's last listing, which with excerpts runs to a few hundred kilobytes.
const maxCLIRequest = 8 << 20

// apiCLI is POST /api/v1/cli: a command of the jw CLI, run for a client in
// remote mode (jw --server) against this server's service and caches. It
// answers with what the command did (see package remote); a command that
// failed is still a 200, its failure in the answer after what it wrote.
func (s *Server) apiCLI(w http.ResponseWriter, r *http.Request) {
	if s.runCLI == nil {
		writeError(w, http.StatusNotFound, "not_found", "this server runs no commands")
		return
	}
	// JSON only: a form posted from another site can carry neither this
	// type nor a preflight-free request with it
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "bad_request", "want a JSON body (Content-Type: application/json)")
		return
	}
	var req remote.Request
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCLIRequest))
	if err := dec.Decode(&req); err != nil {
		badRequest(w, "unreadable command: %v", err)
		return
	}
	if len(req.Args) == 0 {
		badRequest(w, "%s", errMissing("args"))
		return
	}
	resp := s.runCLI(r.Context(), req)
	if r.Context().Err() != nil {
		failJSON(w, r, r.Context().Err())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
