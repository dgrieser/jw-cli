package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dgrieser/jw-cli/internal/bibleref"
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
	// Unwrap says the section's body goes into the section the page already
	// shows for it, without the section around it.
	Unwrap bool `json:"unwrap,omitempty"`
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
//
// A section says what it holds (data-ref) so a link to it opens it rather than
// leaving the page, and a lazy one where its body loads from (data-lazy) once
// it is opened.
func (s *Server) sectionEvent(sec service.UnfoldSection) streamEvent {
	base := firstNonEmpty(sec.Base, s.svc.HTTP.Base.WOL)
	body, err := render.Render(sec.Body, render.HTML, render.Options{BaseURL: base})
	if err != nil {
		body = ""
	}
	var b strings.Builder
	b.WriteString(`<details class="section"`)
	if sec.Ref != "" {
		fmt.Fprintf(&b, ` data-ref="%s"`, html.EscapeString(sec.Ref))
	}
	if sec.Lazy != "" {
		fmt.Fprintf(&b, ` data-lazy="%s"`, html.EscapeString(sec.Lazy))
	}
	if sec.Doc > 0 {
		fmt.Fprintf(&b, ` data-doc="%d"`, sec.Doc)
	}
	if sec.Open {
		b.WriteString(` open`)
	}
	b.WriteString(`><summary>`)
	b.WriteString(sec.Title)
	b.WriteString(`</summary><div class="section-body">`)
	b.WriteString(foldFragment(body))
	if sec.Key != "" {
		fmt.Fprintf(&b, `<div class="sections" data-key="%s"></div>`, html.EscapeString(sec.Key))
	}
	b.WriteString(`</div></details>`)
	return streamEvent{Type: "section", HTML: b.String(), Key: sec.Key, In: sec.In, Order: sec.Order, Unwrap: sec.Unwrap}
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
	// a passage the last level reaches reads with the scriptures it quotes
	cfg.Tail = true
	return cfg, nil
}

// editionSymbol is what a bible edition symbol looks like, which is all a
// stream checks before handing it on to wol.
var editionSymbol = regexp.MustCompile(`^[a-z0-9]{1,16}$`)

// unfoldVerse streams the expansion of one verse: GET /unfold/verse?vid=
// 43003016&depth=1&bible=nwtsty, a range of verses with &to=. &part= narrows
// it to one of its sections (notes, footnotes, indexes with &group=, marginal):
// alone, as the body of a section the page shows already, or with &lazy=1
// loaded while every other section comes as a heading loaded once opened.
func (s *Server) unfoldVerse(w http.ResponseWriter, r *http.Request) {
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	ref, err := passageParam(r)
	if err != nil {
		badRequest(w, "missing or invalid parameter %q", "vid")
		return
	}
	parts := service.PassageParts{Only: r.FormValue("part"), Lazy: boolParam(r, "lazy")}
	if parts.Only != "" && !service.IsPart(parts.Only) {
		badRequest(w, "invalid parameter %q: %q", "part", parts.Only)
		return
	}
	if parts.Group, err = intParam(r, "group", 0); err != nil {
		badRequest(w, "%v", err)
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
	note, requests, err := s.svc.StreamPassageParts(r.Context(), lng, edition, ref, parts, cfg, txt, service.UnfoldStream{
		Section: func(sec service.UnfoldSection) { ev.send(s.sectionEvent(sec)) },
		Stage:   ev.stage,
	})
	ev.finish(r, note, requests, err, txt)
}

// passageParam reads the passage a lazy section loads for: ?vid= the wol id of
// its first verse, ?to= the number of its last when it has more than one.
func passageParam(r *http.Request) (bibleref.Ref, error) {
	refs, err := passagesParam(r)
	if err != nil {
		return bibleref.Ref{}, err
	}
	return refs[0], nil
}

// maxCitedPassages bounds the passages one citations request asks about: a
// reference names a handful at most.
const maxCitedPassages = 16

// passagesParam reads the passages a citations section loads for: each ?vid=
// with the ?to= at the same position, zero or missing for a single verse.
func passagesParam(r *http.Request) ([]bibleref.Ref, error) {
	if err := r.ParseForm(); err != nil {
		return nil, err
	}
	vids, tos := r.Form["vid"], r.Form["to"]
	if len(vids) == 0 {
		return nil, fmt.Errorf("missing parameter %q", "vid")
	}
	if len(vids) > maxCitedPassages {
		return nil, fmt.Errorf("too many passages (%d, at most %d)", len(vids), maxCitedPassages)
	}
	refs := make([]bibleref.Ref, 0, len(vids))
	for i, v := range vids {
		vid, err := strconv.Atoi(v)
		if err != nil || vid <= 0 {
			return nil, fmt.Errorf("invalid parameter %q: %q", "vid", v)
		}
		to := 0
		if i < len(tos) && tos[i] != "" {
			if to, err = strconv.Atoi(tos[i]); err != nil {
				return nil, fmt.Errorf("invalid parameter %q: %q", "to", tos[i])
			}
		}
		ref, err := service.PassageRef(vid, to)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

// unfoldCited streams the publications quoting a passage: GET
// /unfold/cited?vid=43014031&to=33. What a page loads when the citations of a
// verse are opened.
func (s *Server) unfoldCited(w http.ResponseWriter, r *http.Request) {
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	refs, err := passagesParam(r)
	if err != nil {
		badRequest(w, "%v", err)
		return
	}
	// the documents the page read the passage from, left out of the answer
	var self []int
	for _, v := range r.Form["self"] {
		if id, err := strconv.Atoi(v); err == nil && id > 0 && len(self) < maxCitedPassages {
			self = append(self, id)
		}
	}
	txt := text(lng)
	ev := startStream(w)
	requests, err := s.svc.StreamCited(r.Context(), lng, refs, self, service.UnfoldStream{
		Section: func(sec service.UnfoldSection) { ev.send(s.sectionEvent(sec)) },
		Stage:   ev.stage,
	})
	ev.finish(r, "", requests, err, txt)
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

// unfoldTranslations streams one verse in the other bibles of the language:
// GET /unfold/translations?vid=24039014&bible=nwtsty. What a page loads when
// the translations section of a verse is opened.
func (s *Server) unfoldTranslations(w http.ResponseWriter, r *http.Request) {
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	ref, err := passageParam(r)
	if err != nil {
		badRequest(w, "%v", err)
		return
	}
	edition := valueOr(r, "bible", "nwtsty")
	if !editionSymbol.MatchString(edition) {
		badRequest(w, "invalid bible edition %q", edition)
		return
	}
	txt := text(lng)
	ev := startStream(w)
	requests, err := s.svc.StreamTranslations(r.Context(), lng, edition, ref, service.UnfoldStream{
		Section: func(sec service.UnfoldSection) { ev.send(s.sectionEvent(sec)) },
		Stage:   ev.stage,
	})
	ev.finish(r, "", requests, err, txt)
}

// unfoldFootnote streams one footnote: GET /unfold/footnote?path=… What a page
// loads when a "*" is followed before its verse was unfolded. Only a footnote
// path is followed, and only its path: it is always asked of wol itself.
func (s *Server) unfoldFootnote(w http.ResponseWriter, r *http.Request) {
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	path, ok := libraryPath(r.FormValue("path"))
	if !ok || !service.IsFootnote(path) {
		badRequest(w, "parameter %q is not a footnote", "path")
		return
	}
	txt := text(lng)
	ev := startStream(w)
	requests, err := s.svc.StreamFootnote(r.Context(), lng, path, txt, service.UnfoldStream{
		Section: func(sec service.UnfoldSection) { ev.send(s.sectionEvent(sec)) },
		Stage:   ev.stage,
	})
	ev.finish(r, "", requests, err, txt)
}

// unfoldArticle streams a linked document as one section: GET
// /unfold/article?url=… A document of the library (/wol/d/…) is read from the
// library, a jw.org page from jw.org; either way only the path of the link is
// kept and put on the site's own base, so nothing else is ever asked.
func (s *Server) unfoldArticle(w http.ResponseWriter, r *http.Request) {
	lng, err := s.language(r)
	if err != nil {
		failJSON(w, r, err)
		return
	}
	target, ok := s.articleTarget(r.FormValue("url"))
	if !ok {
		badRequest(w, "parameter %q is not a document of wol.jw.org or jw.org", "url")
		return
	}
	// no depth reads the document as it is
	depth, err := intParam(r, "depth", 0)
	if err != nil {
		badRequest(w, "%v", err)
		return
	}
	cfg, err := streamConfig(r, min(max(depth, 0), maxUnfoldDepth))
	if err != nil {
		badRequest(w, "%v", err)
		return
	}
	cfg.LazyCited = true
	txt := text(lng)
	ev := startStream(w)
	cfg.Progress = ev.progress
	sec, err := s.svc.ArticleSection(r.Context(), lng, target, cfg, txt)
	if err == nil {
		ev.send(s.sectionEvent(sec))
	}
	ev.finish(r, "", 1, err, txt)
}

// libraryPath is the path (and query) of a link, whatever host it named.
func libraryPath(raw string) (string, bool) {
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
	return path, true
}

// articleTarget puts a link to a document back on the base it belongs to: a
// library document (/wol/d/) on wol.jw.org, anything else of jw.org on
// www.jw.org. The fragment is kept, since it names the passage a link means.
// Anything else is refused.
func (s *Server) articleTarget(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	path := u.EscapedPath()
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return "", false
	}
	var base string
	host := strings.ToLower(u.Hostname())
	switch {
	case strings.Contains(path, "/wol/d/") || strings.Contains(path, "/wol/tc/"):
		// a table-of-contents link ("App. C") answers with the document it
		// names, redirected
		base = s.svc.HTTP.Base.WOL
	case strings.Contains(path, "/wol/") || host == "wol.jw.org":
		// the library's other endpoints are citations, not documents
		return "", false
	case host == "jw.org" || strings.HasSuffix(host, ".jw.org"):
		base = s.svc.HTTP.Base.JWOrg
	default:
		return "", false
	}
	out := base + path
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	if u.Fragment != "" {
		out += "#" + u.EscapedFragment()
	}
	return out, true
}
