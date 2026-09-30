package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dgrieser/jw-cli/internal/i18n"
	"github.com/dgrieser/jw-cli/internal/render"
	"github.com/dgrieser/jw-cli/internal/service"
	"github.com/dgrieser/jw-cli/internal/unfold"
)

// The unfold streams are what the web UI loads an expansion through once the
// page itself is on screen: one verse, or the citations of one paragraph, at a
// time. Each answers with newline-delimited JSON events, flushed as they
// happen, so a section shows up the moment it is ready and the page can say
// how far a long expansion has got.
//
//	{"type":"stage","stage":"references"}
//	{"type":"progress","level":1,"done":3,"total":7}
//	{"type":"section","html":"<details class=\"section\">…","in":"marginal"}
//	{"type":"expensive","level":2,"requests":2400,"text":"…"}
//	{"type":"error","text":"…"}
//	{"type":"done","count":4,"requests":37,"text":"note about an expansion cut short"}
//
// A page that unfolds item after item — every verse of a reading — passes what
// the items before cost as ?spent=, so the request budget an expensive
// expansion is asked about covers the whole run, not each item on its own.

// streamEvent is one line of a stream.
type streamEvent struct {
	Type     string `json:"type"`
	HTML     string `json:"html,omitempty"`
	Key      string `json:"key,omitempty"`
	In       string `json:"in,omitempty"`
	Stage    string `json:"stage,omitempty"`
	Level    int    `json:"level,omitempty"`
	Done     int    `json:"done,omitempty"`
	Total    int    `json:"total,omitempty"`
	Requests int    `json:"requests,omitempty"`
	Count    int    `json:"count,omitempty"`
	Order    int    `json:"order,omitempty"`
	Text     string `json:"text,omitempty"`
}

// progressEvery throttles the progress events of a level: a request answered
// from the cache takes no time at all, and a line per request would be most of
// the stream.
const progressEvery = 150 * time.Millisecond

// eventStream writes the events of one response.
type eventStream struct {
	mu       sync.Mutex
	w        http.ResponseWriter
	rc       *http.ResponseController
	enc      *json.Encoder
	sections int
	last     time.Time
}

func startStream(w http.ResponseWriter) *eventStream {
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	// a proxy in front of jw serve must not hold the lines back
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	return &eventStream{w: w, rc: http.NewResponseController(w), enc: json.NewEncoder(w)}
}

func (e *eventStream) send(ev streamEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if ev.Type == "section" {
		e.sections++
	}
	_ = e.enc.Encode(ev)
	_ = e.rc.Flush()
}

func (e *eventStream) stage(name string) { e.send(streamEvent{Type: "stage", Stage: name}) }

func (e *eventStream) progress(level, done, total int) {
	e.mu.Lock()
	now := time.Now()
	skip := done < total && done > 1 && now.Sub(e.last) < progressEvery
	if !skip {
		e.last = now
	}
	e.mu.Unlock()
	if !skip {
		e.send(streamEvent{Type: "progress", Level: level, Done: done, Total: total})
	}
}

// finish closes a stream: what went wrong, if anything, then the count of
// sections sent. A client that went away is not written to again.
func (e *eventStream) finish(r *http.Request, note string, requests int, err error, txt *i18n.Messages) {
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		var te *tooExpensiveError
		if errors.As(err, &te) {
			e.send(streamEvent{
				Type: "expensive", Level: te.level, Requests: te.requests,
				Text: fmt.Sprintf(txt.UIExpensiveBody, te.level, te.requests),
			})
		} else {
			e.send(streamEvent{Type: "error", Text: err.Error()})
		}
	}
	e.mu.Lock()
	count := e.sections
	e.mu.Unlock()
	e.send(streamEvent{Type: "done", Count: count, Requests: requests, Text: note})
}

// sectionEvent renders a streamed section into the disclosure the page shows:
// the title as the summary, the body sanitized like every other fragment and
// folded, so each reference inside it opens on its own. A section others are
// streamed into carries the empty list they go into.
func (s *Server) sectionEvent(sec service.UnfoldSection) streamEvent {
	body, err := render.Render(sec.Body, render.HTML, render.Options{BaseURL: s.svc.HTTP.Base.WOL})
	if err != nil {
		body = ""
	}
	var b strings.Builder
	b.WriteString(`<details class="section"><summary>`)
	b.WriteString(sec.Title)
	b.WriteString(`</summary><div class="section-body">`)
	b.WriteString(foldFragment(body))
	if sec.Key != "" {
		fmt.Fprintf(&b, `<div class="sections" data-key="%s"></div>`, html.EscapeString(sec.Key))
	}
	b.WriteString(`</div></details>`)
	return streamEvent{Type: "section", HTML: b.String(), Key: sec.Key, In: sec.In, Order: sec.Order}
}

// streamDepth reads ?depth= for a stream: at least one level, since a stream
// that unfolds nothing has nothing to say, and no deeper than the server goes.
func streamDepth(r *http.Request) (int, error) {
	depth, err := intParam(r, "depth", 1)
	if err != nil {
		return 0, err
	}
	return min(max(depth, 1), maxUnfoldDepth), nil
}

// streamConfig is how a stream runs its expansion: as unfoldConfig says, on
// top of what the page says it already spent (?spent=).
func streamConfig(r *http.Request, depth int) (service.UnfoldConfig, error) {
	spent, err := intParam(r, "spent", 0)
	if err != nil {
		return service.UnfoldConfig{}, err
	}
	cfg := unfoldConfig(depth, forceParam(r))
	cfg.Spent = max(spent, 0)
	return cfg, nil
}

// editionSymbol is what a bible edition symbol looks like, which is all a
// stream checks before handing it on to wol.
var editionSymbol = regexp.MustCompile(`^[a-z0-9]{1,16}$`)

// unfoldVerse streams the expansion of one verse: GET /unfold/verse?vid=
// 43003016&depth=1&bible=nwtsty.
func (s *Server) unfoldVerse(w http.ResponseWriter, r *http.Request) {
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	vid, err := intParam(r, "vid", 0)
	if err != nil || vid <= 0 {
		badRequest(w, "missing or invalid parameter %q", "vid")
		return
	}
	depth, err := streamDepth(r)
	if err != nil {
		badRequest(w, "%v", err)
		return
	}
	edition := valueOr(r, "bible", "nwtsty")
	if !editionSymbol.MatchString(edition) {
		badRequest(w, "invalid bible edition %q", edition)
		return
	}
	cfg, err := streamConfig(r, depth)
	if err != nil {
		badRequest(w, "%v", err)
		return
	}
	txt := text(lng)
	ev := startStream(w)
	cfg.Progress = ev.progress
	note, requests, err := s.svc.StreamVerseUnfold(r.Context(), lng, edition, vid, cfg, txt, service.UnfoldStream{
		Section: func(sec service.UnfoldSection) { ev.send(s.sectionEvent(sec)) },
		Stage:   ev.stage,
	})
	ev.finish(r, note, requests, err, txt)
}

// maxStreamRefs bounds the citations one request may ask about: a paragraph
// cites a handful, and anything past this is not a paragraph.
const maxStreamRefs = 64

// unfoldRefs streams the expansion of the citations of one paragraph:
// GET /unfold/refs?path=…&text=…&path=…&text=…&depth=1. Each path is the link
// the page shows, absolutized or not; only its path is kept, and only a
// citation is followed, so nothing but wol's own citation endpoints is ever
// asked.
func (s *Server) unfoldRefs(w http.ResponseWriter, r *http.Request) {
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	if err := r.ParseForm(); err != nil {
		badRequest(w, "%v", err)
		return
	}
	paths, texts := r.Form["path"], r.Form["text"]
	var refs []service.CitationRef
	for i, raw := range paths {
		path, ok := citationPath(raw)
		if !ok {
			continue
		}
		ref := service.CitationRef{Path: path}
		if i < len(texts) {
			ref.Text = texts[i]
		}
		refs = append(refs, ref)
	}
	if len(refs) == 0 {
		badRequest(w, "no citation among the %q parameters", "path")
		return
	}
	if len(refs) > maxStreamRefs {
		badRequest(w, "too many citations (%d, at most %d)", len(refs), maxStreamRefs)
		return
	}
	depth, err := streamDepth(r)
	if err != nil {
		badRequest(w, "%v", err)
		return
	}
	cfg, err := streamConfig(r, depth)
	if err != nil {
		badRequest(w, "%v", err)
		return
	}
	txt := text(lng)
	ev := startStream(w)
	cfg.Progress = ev.progress
	note, requests, err := s.svc.StreamRefsUnfold(r.Context(), lng, refs, cfg, txt, service.UnfoldStream{
		Section: func(sec service.UnfoldSection) { ev.send(s.sectionEvent(sec)) },
		Stage:   ev.stage,
	})
	ev.finish(r, note, requests, err, txt)
}

// citationPath reduces a link to the wol path it names, and reports whether
// that is a citation. The host is dropped, whatever it was: the path is always
// asked of wol itself.
func citationPath(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	path := u.EscapedPath()
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return "", false
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	return path, unfold.IsCitation(path)
}
