package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/dgrieser/jw-cli/internal/api/pubmedia"
	"github.com/dgrieser/jw-cli/internal/httpx"
	"github.com/dgrieser/jw-cli/internal/render"
	"github.com/dgrieser/jw-cli/internal/service"
)

// apiError is the envelope every non-2xx API response carries.
type apiError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	var e apiError
	e.Error.Code, e.Error.Message = code, message
	writeJSON(w, status, e)
}

func badRequest(w http.ResponseWriter, format string, args ...any) {
	writeError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf(format, args...))
}

// failJSON maps an operation error onto the API's status codes: what the
// upstream sites did not have is 404, an expansion past the request budget is
// 422, an upstream failure is 502, and everything else — bad references, bad
// parameters the handler could not catch up front — is 400.
func failJSON(w http.ResponseWriter, r *http.Request, err error) {
	status, code := classify(r.Context(), err)
	if status == 0 {
		return // the client went away; nobody is listening
	}
	writeError(w, status, code, err.Error())
}

// classify is the shared error mapping of the API and the UI. A zero status
// means the request was canceled by the client.
func classify(ctx context.Context, err error) (status int, code string) {
	var se *httpx.StatusError
	var te *tooExpensiveError
	switch {
	case errors.Is(err, context.Canceled) && ctx.Err() != nil:
		return 0, ""
	case errors.As(err, &te):
		return http.StatusUnprocessableEntity, "too_expensive"
	case errors.Is(err, pubmedia.ErrNotFound):
		return http.StatusNotFound, "not_found"
	case errors.As(err, &se):
		if se.StatusCode == http.StatusNotFound {
			return http.StatusNotFound, "not_found"
		}
		return http.StatusBadGateway, "upstream"
	}
	return http.StatusBadRequest, "bad_request"
}

// tooExpensiveError says an unfold level would cost more upstream requests
// than an unattended server is willing to spend.
type tooExpensiveError struct {
	level, requests int
}

func (e *tooExpensiveError) Error() string {
	return fmt.Sprintf("unfolding level %d needs %d requests to wol.jw.org; lower the unfold depth", e.level, e.requests)
}

// maxUnfoldDepth caps ?unfold= on the server: each level multiplies upstream
// traffic, and past this depth the CLI is the right tool.
const maxUnfoldDepth = 3

// unfoldConfig is how the server runs an expansion: capped in depth, never
// waiting on anybody, refusing a level that would cost too many requests.
func unfoldConfig(depth int) service.UnfoldConfig {
	return service.UnfoldConfig{
		Depth: min(depth, maxUnfoldDepth),
		Confirm: func(level, requests int) (bool, error) {
			return false, &tooExpensiveError{level: level, requests: requests}
		},
	}
}

// bodyFormat parses ?format= for endpoints that render a document body:
// html (default), markdown, or text. JSON is the endpoint's own shape and raw
// is a terminal affair, so neither is a body format here.
func bodyFormat(r *http.Request) (render.Format, error) {
	switch v := r.FormValue("format"); v {
	case "", "html":
		return render.HTML, nil
	case "markdown", "md":
		return render.Markdown, nil
	case "text", "txt", "plain":
		return render.Text, nil
	default:
		return render.HTML, fmt.Errorf("invalid format %q (want html, markdown, or text)", v)
	}
}

// intParam parses an optional integer query parameter.
func intParam(r *http.Request, name string, def int) (int, error) {
	v := r.FormValue(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q (want a number)", name, v)
	}
	return n, nil
}

// boolParam reads an optional boolean query parameter: absent and "false" are
// false, "" (bare ?x), "true" and "1" are true.
func boolParam(r *http.Request, name string) bool {
	if !r.URL.Query().Has(name) {
		return false
	}
	switch r.FormValue(name) {
	case "", "1", "true", "yes":
		return true
	}
	return false
}
