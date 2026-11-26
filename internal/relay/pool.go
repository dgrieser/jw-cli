package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/dgrieser/jw-cli/internal/httpx"
)

// The server's side: the proxies registered with it and the enrollments an
// admin made for new ones, kept in one file of its data folder; the jobs it
// hands out; and what it knows of each proxy's health — when it last asked
// for work, how its jobs went — which it keeps in memory.
//
// A job goes to whichever proxy asks first: the polls of all proxies wait on
// one queue, which spreads the work over the proxies that are free. Picking
// among them — or between them and the server itself — by other rules is a
// matter of Fetch alone.

const (
	// offlineAfter is how long a proxy that stopped asking for work counts
	// as online: a poll and the work it brings take less.
	offlineAfter = 75 * time.Second
	// pickupWait is how long a job waits for a proxy to take it.
	pickupWait = 8 * time.Second
	// answerWait is how long a proxy has to answer a job it took.
	answerWait = 45 * time.Second
	// maxPolls bounds the polls one proxy may hold open at once.
	maxPolls = 16
	// maxName bounds a proxy's name.
	maxName = 64
	// registerFailures within registerWindow from one address are all it
	// gets before it must wait.
	registerFailures = 10
	registerWindow   = 15 * time.Minute
)

// Pool is the proxies of a server.
type Pool struct {
	path string
	logf func(format string, args ...any)
	// version is the server's own, which a proxy is told
	version string
	// clientIP is the address a request came from, as the server tells it
	// behind its reverse proxies; nil takes the connection's
	clientIP func(*http.Request) string

	mu       sync.Mutex
	proxies  map[string]*proxy
	enrolls  []Enrollment
	jobs     map[string]*job
	failures map[string][]time.Time // failed registrations and credentials, by address

	queue chan *job
}

// record is what is kept of a proxy on disk.
type record struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	SecretHash string    `json:"secretHash"`
	Enabled    bool      `json:"enabled"`
	Created    time.Time `json:"created"`
	CreatedBy  string    `json:"createdBy,omitempty"`
}

// proxy is a proxy and what is known of its health.
type proxy struct {
	record
	lastSeen, lastJob, lastErrorAt time.Time
	polls, ok, failed              int
	version, addr, lastError       string
	avgMillis                      float64
}

// Enrollment is a proxy an admin made room for, until it registers.
type Enrollment struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	TokenHash string    `json:"tokenHash"`
	Expires   time.Time `json:"expires"`
	CreatedBy string    `json:"createdBy,omitempty"`
}

type job struct {
	Job
	proxy string
	done  chan jobResult
}

type jobResult struct {
	resp *http.Response
	err  error
}

// file is the pool's file.
type file struct {
	Proxies     []record     `json:"proxies"`
	Enrollments []Enrollment `json:"enrollments,omitempty"`
}

// OpenPool reads the proxies registered with a server from path (none when
// the file does not exist yet).
func OpenPool(path, version string, logf func(format string, args ...any)) (*Pool, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	p := &Pool{
		path: path, logf: logf, version: version,
		proxies: map[string]*proxy{}, jobs: map[string]*job{}, failures: map[string][]time.Time{},
		queue: make(chan *job),
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return nil, fmt.Errorf("proxies: %w", err)
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("proxies: %s: %w", path, err)
	}
	for _, r := range f.Proxies {
		p.proxies[r.ID] = &proxy{record: r}
	}
	p.enrolls = f.Enrollments
	return p, nil
}

// save writes the pool's file: aside first, then over the old one.
func (p *Pool) save() error {
	f := file{Enrollments: p.enrolls}
	for _, px := range p.proxies {
		f.Proxies = append(f.Proxies, px.record)
	}
	slices.SortFunc(f.Proxies, func(a, b record) int { return a.Created.Compare(b.Created) })
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p.path), 0o700); err != nil {
		return fmt.Errorf("proxies: %w", err)
	}
	tmp := p.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("proxies: %w", err)
	}
	if err := os.Rename(tmp, p.path); err != nil {
		return fmt.Errorf("proxies: %w", err)
	}
	return nil
}

// --- what the admin does --------------------------------------------------------

// ErrNotFound is a proxy or an enrollment that is not there.
var ErrNotFound = errors.New("no such proxy")

// ErrBadName is a name a proxy cannot have.
var ErrBadName = errors.New("a proxy's name is 1 to 64 printable characters")

func cleanName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" || utf8.RuneCountInString(name) > maxName || strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return "", ErrBadName
	}
	return name, nil
}

// Enroll makes room for a proxy named name: the one-time token its setup
// registers it with, valid for EnrollTTL.
func (p *Pool) Enroll(name, by string, now time.Time) (string, Enrollment, error) {
	name, err := cleanName(name)
	if err != nil {
		return "", Enrollment{}, err
	}
	token, tokenHash := newEnrollToken()
	e := Enrollment{ID: secret(9), Name: name, TokenHash: tokenHash, Expires: now.Add(EnrollTTL).UTC(), CreatedBy: by}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dropExpired(now)
	p.enrolls = append(p.enrolls, e)
	if err := p.save(); err != nil {
		p.enrolls = p.enrolls[:len(p.enrolls)-1]
		return "", Enrollment{}, err
	}
	p.logf("proxies: %s enrolled %q", by, name)
	return token, e, nil
}

// dropExpired forgets enrollments past their time; the caller holds mu and
// saves.
func (p *Pool) dropExpired(now time.Time) {
	p.enrolls = slices.DeleteFunc(p.enrolls, func(e Enrollment) bool { return !now.Before(e.Expires) })
}

// Enrollments are the enrollments not used yet.
func (p *Pool) Enrollments(now time.Time) []Enrollment {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []Enrollment
	for _, e := range p.enrolls {
		if now.Before(e.Expires) {
			out = append(out, e)
		}
	}
	return out
}

// Revoke withdraws an enrollment not used yet.
func (p *Pool) Revoke(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	i := slices.IndexFunc(p.enrolls, func(e Enrollment) bool { return e.ID == id })
	if i < 0 {
		return ErrNotFound
	}
	p.enrolls = slices.Delete(p.enrolls, i, i+1)
	return p.save()
}

// SetEnabled switches a proxy on or off: a proxy switched off gets no work
// and is told so when it asks.
func (p *Pool) SetEnabled(id string, on bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	px, ok := p.proxies[id]
	if !ok {
		return ErrNotFound
	}
	if px.Enabled == on {
		return nil
	}
	px.Enabled = on
	if err := p.save(); err != nil {
		px.Enabled = !on
		return err
	}
	p.logf("proxies: %q %s", px.Name, map[bool]string{true: "enabled", false: "disabled"}[on])
	return nil
}

// Delete forgets a proxy: its credential opens nothing anymore.
func (p *Pool) Delete(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	px, ok := p.proxies[id]
	if !ok {
		return ErrNotFound
	}
	delete(p.proxies, id)
	if err := p.save(); err != nil {
		p.proxies[id] = px
		return err
	}
	p.logf("proxies: %q deleted", px.Name)
	return nil
}

// Status is a proxy as the admin sees it.
type Status struct {
	ID, Name, Version, Addr, LastError string
	Enabled, Online                    bool
	Created, LastSeen, LastJob         time.Time
	LastErrorAt                        time.Time
	Polls, OK, Failed                  int
	AvgMillis                          int64
	CreatedBy                          string
}

// State is how the proxy is doing in a word: disabled, online, offline or
// never (registered, never seen).
func (s Status) State() string {
	switch {
	case !s.Enabled:
		return "disabled"
	case s.Online:
		return "online"
	case s.LastSeen.IsZero():
		return "never"
	}
	return "offline"
}

// Proxies are the proxies registered, oldest first.
func (p *Pool) Proxies(now time.Time) []Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Status, 0, len(p.proxies))
	for _, px := range p.proxies {
		out = append(out, Status{
			ID: px.ID, Name: px.Name, Version: px.version, Addr: px.addr, LastError: px.lastError,
			Enabled: px.Enabled, Online: px.online(now),
			Created: px.Created, LastSeen: px.lastSeen, LastJob: px.lastJob, LastErrorAt: px.lastErrorAt,
			Polls: px.polls, OK: px.ok, Failed: px.failed, AvgMillis: int64(px.avgMillis), CreatedBy: px.CreatedBy,
		})
	}
	slices.SortFunc(out, func(a, b Status) int { return a.Created.Compare(b.Created) })
	return out
}

func (px *proxy) online(now time.Time) bool {
	return px.Enabled && (px.polls > 0 || (!px.lastSeen.IsZero() && now.Sub(px.lastSeen) < offlineAfter))
}

// --- what the server asks for ---------------------------------------------------

// Allows reports whether a request may go through a proxy (httpx.Relay).
func (p *Pool) Allows(method, rawURL string) bool { return Allowed(method, rawURL) }

// available reports whether a proxy is there to take a job.
func (p *Pool) available(now time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, px := range p.proxies {
		if px.online(now) {
			return true
		}
	}
	return false
}

// Fetch has a proxy make a request (httpx.Relay): it waits for a proxy to
// take it, then for its answer. httpx.ErrNoRelay when no proxy is there, or
// none took it or answered in time.
func (p *Pool) Fetch(ctx context.Context, method, rawURL string, hdr http.Header) (*http.Response, error) {
	if !Allowed(method, rawURL) {
		return nil, fmt.Errorf("%w: %s %s is not for a proxy", httpx.ErrNoRelay, method, rawURL)
	}
	if !p.available(time.Now()) {
		return nil, httpx.ErrNoRelay
	}
	j := &job{Job: Job{ID: secret(12), Method: method, URL: rawURL, Header: RequestHeader(hdr)}, done: make(chan jobResult, 1)}
	p.mu.Lock()
	p.jobs[j.ID] = j
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.jobs, j.ID)
		p.mu.Unlock()
	}()

	pickup := time.NewTimer(pickupWait)
	defer pickup.Stop()
	select {
	case p.queue <- j:
	case <-pickup.C:
		return nil, fmt.Errorf("%w: no proxy took it within %s", httpx.ErrNoRelay, pickupWait)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	answer := time.NewTimer(answerWait)
	defer answer.Stop()
	select {
	case r := <-j.done:
		return r.resp, r.err
	case <-answer.C:
		p.mu.Lock()
		taker := j.proxy
		p.mu.Unlock()
		p.noted(taker, false, 0, "no answer within "+answerWait.String())
		return nil, fmt.Errorf("%w: the proxy did not answer within %s", httpx.ErrNoRelay, answerWait)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// noted records how a proxy's job went.
func (p *Pool) noted(id string, ok bool, millis int64, failure string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	px, found := p.proxies[id]
	if !found {
		return
	}
	now := time.Now()
	px.lastJob = now
	if ok {
		px.ok++
		if px.avgMillis == 0 {
			px.avgMillis = float64(millis)
		} else {
			px.avgMillis = 0.8*px.avgMillis + 0.2*float64(millis)
		}
		return
	}
	px.failed++
	px.lastError, px.lastErrorAt = failure, now
}

// --- what the proxies ask for ---------------------------------------------------

// Handler answers the protocol under PathPrefix. It stands outside the
// server's sign-in: a proxy shows its own credential, which nothing else
// takes.
func (p *Pool) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+PathPrefix+"register", p.register)
	mux.HandleFunc("GET "+PathPrefix+"poll", p.poll)
	mux.HandleFunc("POST "+PathPrefix+"jobs/{id}", p.result)
	mux.HandleFunc("GET "+PathPrefix+"self", p.self)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if p.version != "" {
			w.Header().Set(ServerVersionHeader, p.version)
		}
		mux.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// SetClientIP has the pool tell where a request came from the way the
// server does, behind its reverse proxies.
func (p *Pool) SetClientIP(f func(*http.Request) string) { p.clientIP = f }

// addr is the address a request came from: for the limits and the admin's
// view alone.
func (p *Pool) addr(r *http.Request) string {
	if p.clientIP != nil {
		return p.clientIP(r)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// limited reports an address that failed too often lately; failed counts
// one more failure of it.
func (p *Pool) limited(a string, now time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	recent := slices.DeleteFunc(p.failures[a], func(t time.Time) bool { return now.Sub(t) > registerWindow })
	p.failures[a] = recent
	return len(recent) >= registerFailures
}

func (p *Pool) failed(a string, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failures[a] = append(p.failures[a], now)
}

// register turns an enrollment into a proxy.
func (p *Pool) register(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	from := p.addr(r)
	if p.limited(from, now) {
		w.Header().Set("Retry-After", strconv.Itoa(int(registerWindow.Seconds())))
		fail(w, http.StatusTooManyRequests, "too many failed registrations; try again later")
		return
	}
	var reg Registration
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&reg); err != nil {
		fail(w, http.StatusBadRequest, "unreadable registration")
		return
	}
	tokenHash := hash(strings.TrimSpace(reg.Token))
	p.mu.Lock()
	p.dropExpired(now)
	i := slices.IndexFunc(p.enrolls, func(e Enrollment) bool { return sameHash(e.TokenHash, tokenHash) })
	if i < 0 {
		p.mu.Unlock()
		p.failed(from, now)
		fail(w, http.StatusUnauthorized, "the enrollment token is unknown, used or expired: make a new one on the server's proxies page")
		return
	}
	e := p.enrolls[i]
	id, credential, secretHash := newCredential()
	px := &proxy{record: record{ID: id, Name: e.Name, SecretHash: secretHash, Enabled: true, Created: now.UTC(), CreatedBy: e.CreatedBy}}
	px.version, px.addr = reg.Version, from
	p.enrolls = slices.Delete(p.enrolls, i, i+1)
	p.proxies[id] = px
	if err := p.save(); err != nil {
		delete(p.proxies, id)
		p.enrolls = append(p.enrolls, e)
		p.mu.Unlock()
		fail(w, http.StatusInternalServerError, "the server could not keep the proxy")
		return
	}
	p.mu.Unlock()
	p.logf("proxies: %q registered from %s", e.Name, from)
	writeJSON(w, http.StatusCreated, Registered{ID: id, Name: e.Name, Credential: credential})
}

// authenticate is the proxy a request's credential names, or nil after
// answering 401.
func (p *Pool) authenticate(w http.ResponseWriter, r *http.Request) *proxy {
	now := time.Now()
	from := p.addr(r)
	refuse := func() *proxy {
		p.failed(from, now)
		w.Header().Set("WWW-Authenticate", AuthScheme)
		fail(w, http.StatusUnauthorized, "credential refused: the proxy was deleted, or never registered")
		return nil
	}
	if p.limited(from, now) {
		w.Header().Set("Retry-After", strconv.Itoa(int(registerWindow.Seconds())))
		fail(w, http.StatusTooManyRequests, "too many refused credentials; try again later")
		return nil
	}
	id, s, ok := splitCredential(credentialOf(r))
	if !ok {
		return refuse()
	}
	p.mu.Lock()
	px, found := p.proxies[id]
	p.mu.Unlock()
	if !found || !sameHash(hash(s), px.SecretHash) {
		return refuse()
	}
	return px
}

// seen notes a proxy asking, and whether it came online with it.
func (p *Pool) seen(px *proxy, r *http.Request, polling int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	if polling > 0 && !px.online(now) {
		defer p.logf("proxies: %q online from %s", px.Name, p.addr(r))
	}
	px.lastSeen = now
	px.polls += polling
	px.addr = p.addr(r)
	if v := r.Header.Get(VersionHeader); v != "" && len(v) <= 64 {
		px.version = v
	}
}

// poll hands a proxy a job, or nothing after PollWait.
func (p *Pool) poll(w http.ResponseWriter, r *http.Request) {
	px := p.authenticate(w, r)
	if px == nil {
		return
	}
	p.mu.Lock()
	enabled, polls := px.Enabled, px.polls
	p.mu.Unlock()
	if !enabled {
		p.seen(px, r, 0)
		writeJSON(w, http.StatusForbidden, map[string]string{"state": "disabled", "error": "the proxy is disabled on the server"})
		return
	}
	if polls >= maxPolls {
		fail(w, http.StatusTooManyRequests, fmt.Sprintf("at most %d polls at once", maxPolls))
		return
	}
	p.seen(px, r, 1)
	defer p.seen(px, r, -1)
	wait := time.NewTimer(PollWait)
	defer wait.Stop()
	select {
	case j := <-p.queue:
		p.mu.Lock()
		j.proxy = px.ID
		p.mu.Unlock()
		writeJSON(w, http.StatusOK, j.Job)
	case <-wait.C:
		w.WriteHeader(http.StatusNoContent)
	case <-r.Context().Done():
	}
}

// result takes a proxy's answer to a job it took.
func (p *Pool) result(w http.ResponseWriter, r *http.Request) {
	px := p.authenticate(w, r)
	if px == nil {
		return
	}
	p.seen(px, r, 0)
	id := r.PathValue("id")
	p.mu.Lock()
	j, ok := p.jobs[id]
	if ok && j.proxy == px.ID {
		delete(p.jobs, id) // one answer per job
	}
	enabled := px.Enabled
	p.mu.Unlock()
	if !ok || j.proxy != px.ID {
		fail(w, http.StatusNotFound, "no such job, or no longer waited for")
		return
	}
	if !enabled {
		j.done <- jobResult{err: fmt.Errorf("%w: the proxy was disabled", httpx.ErrNoRelay)}
		fail(w, http.StatusForbidden, "the proxy is disabled on the server")
		return
	}
	var res Result
	if raw := r.Header.Get(ResultHeader); len(raw) > 16<<10 || json.Unmarshal([]byte(raw), &res) != nil {
		j.done <- jobResult{err: fmt.Errorf("proxy %q: unreadable result", px.Name)}
		p.noted(px.ID, false, 0, "unreadable result")
		fail(w, http.StatusBadRequest, "unreadable "+ResultHeader)
		return
	}
	if res.Error != "" || res.Status < 100 || res.Status > 599 {
		failure := cmpOr(res.Error, "no status")
		j.done <- jobResult{err: fmt.Errorf("proxy %q: %s", px.Name, failure)}
		p.noted(px.ID, false, res.Millis, failure)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBody))
	if err != nil {
		j.done <- jobResult{err: fmt.Errorf("proxy %q: the answer's body: %w", px.Name, err)}
		p.noted(px.ID, false, res.Millis, "the answer's body: "+err.Error())
		fail(w, http.StatusBadRequest, "unreadable body")
		return
	}
	hdr := Header(res.Header, false)
	hdr.Set(ProxyHeader, px.Name)
	resp := &http.Response{
		Status:        fmt.Sprintf("%d %s", res.Status, http.StatusText(res.Status)),
		StatusCode:    res.Status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        hdr,
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
	}
	// a refusal or a failure is the proxy's trouble too; a page not found
	// is an answer
	ok = res.Status < 400 || res.Status == http.StatusNotFound
	p.noted(px.ID, ok, res.Millis, fmt.Sprintf("HTTP %d for %s", res.Status, j.URL))
	j.done <- jobResult{resp: resp}
	w.WriteHeader(http.StatusNoContent)
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// self tells a proxy how the server sees it.
func (p *Pool) self(w http.ResponseWriter, r *http.Request) {
	px := p.authenticate(w, r)
	if px == nil {
		return
	}
	p.seen(px, r, 0)
	p.mu.Lock()
	s := Self{ID: px.ID, Name: px.Name, Enabled: px.Enabled}
	p.mu.Unlock()
	writeJSON(w, http.StatusOK, s)
}
