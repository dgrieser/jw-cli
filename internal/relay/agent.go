package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dgrieser/jw-cli/internal/httpx"
)

// The proxy's side (jw proxy): it registers once with its server, keeps the
// credential it got in a file only it reads, and from then on asks the
// server for work — a few polls at once, so one slow page does not hold up
// the next — makes the requests it is given, and sends the answers back.

// Config is a proxy's file: its server and its credential.
type Config struct {
	Server     string `json:"server"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	Credential string `json:"credential"`
	// Workers is how many polls the proxy keeps open, so how many requests
	// it makes at once; 0 is DefaultWorkers.
	Workers int `json:"workers,omitempty"`
}

// DefaultWorkers is how many requests a proxy makes at once.
const DefaultWorkers = 4

// LoadConfig reads a proxy's file.
func LoadConfig(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	if _, err := CheckServerURL(c.Server); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	if _, _, ok := splitCredential(c.Credential); !ok {
		return Config{}, fmt.Errorf("%s: no credential", path)
	}
	return c, nil
}

// Save writes a proxy's file, readable by its owner alone.
func (c Config) Save(path string) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// apiClient is the client a proxy speaks to its server with. A poll is held
// for PollWait, so the timeout is well past it.
func apiClient() *http.Client {
	return &http.Client{Timeout: PollWait + 30*time.Second}
}

// Register registers a proxy with a server, with the enrollment token an
// admin made for it there.
func Register(ctx context.Context, server, token, version string) (Config, error) {
	server, err := CheckServerURL(server)
	if err != nil {
		return Config{}, err
	}
	host, _ := os.Hostname()
	body, _ := json.Marshal(Registration{Token: strings.TrimSpace(token), Name: host, Version: version})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server+PathPrefix+"register", bytes.NewReader(body))
	if err != nil {
		return Config{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := apiClient().Do(req)
	if err != nil {
		return Config{}, fmt.Errorf("register with %s: %w", server, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return Config{}, fmt.Errorf("register with %s: %s", server, failure(resp))
	}
	var reg Registered
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&reg); err != nil {
		return Config{}, fmt.Errorf("register with %s: unreadable answer: %w", server, err)
	}
	return Config{Server: server, ID: reg.ID, Name: reg.Name, Credential: reg.Credential}, nil
}

// failure is what a server's answer that is no success says.
func failure(resp *http.Response) string {
	var e struct {
		Error string `json:"error"`
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if json.Unmarshal(b, &e) == nil && e.Error != "" {
		return fmt.Sprintf("HTTP %d: %s", resp.StatusCode, e.Error)
	}
	if resp.StatusCode == http.StatusNotFound {
		return "HTTP 404: the server has no proxies (is it a jw serve, new enough, with accounts?)"
	}
	return fmt.Sprintf("HTTP %d", resp.StatusCode)
}

// ErrRevoked is a credential the server refuses: the proxy was deleted
// there. Nothing a restart changes; the proxy is set up anew.
var ErrRevoked = errors.New("the server refused the credential: the proxy was deleted there; set it up anew with a new enrollment")

// Agent is a running proxy.
type Agent struct {
	cfg     Config
	version string
	logf    func(format string, args ...any)
	api     *http.Client
	fetch   *httpx.Client

	mu       sync.Mutex
	disabled bool
}

// NewAgent is a proxy of cfg's server. logf receives what it does.
func NewAgent(cfg Config, version string, logf func(format string, args ...any)) *Agent {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{
		Timeout:       answerWait - 5*time.Second,
		Jar:           jar,
		CheckRedirect: stayOnJW,
	}
	return &Agent{cfg: cfg, version: version, logf: logf, api: apiClient(), fetch: httpx.New(httpx.WithHTTPClient(hc))}
}

// stayOnJW follows a redirect no further than a request could go.
func stayOnJW(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if !Allowed(http.MethodGet, req.URL.String()) {
		return fmt.Errorf("redirected off jw.org's sites, to %s", req.URL.Host)
	}
	return nil
}

// withFetchClient replaces the client the requests are made with (tests).
func (a *Agent) withFetchClient(c *httpx.Client) { a.fetch = c }

func (a *Agent) request(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, a.cfg.Server+PathPrefix+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", AuthScheme+" "+a.cfg.Credential)
	req.Header.Set(VersionHeader, a.version)
	return req, nil
}

// Self asks the server how it sees the proxy.
func (a *Agent) Self(ctx context.Context) (Self, error) {
	req, err := a.request(ctx, http.MethodGet, "self", nil)
	if err != nil {
		return Self{}, err
	}
	resp, err := a.api.Do(req)
	if err != nil {
		return Self{}, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return Self{}, ErrRevoked
	default:
		return Self{}, errors.New(failure(resp))
	}
	var s Self
	err = json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&s)
	return s, err
}

// Run makes the requests the server hands out until ctx is done, or the
// server refuses the credential (ErrRevoked). A server that cannot be
// reached is asked again, ever later up to a minute; a proxy disabled there
// asks every disabledWait whether it is enabled again.
func (a *Agent) Run(ctx context.Context) error {
	workers := a.cfg.Workers
	if workers <= 0 {
		workers = DefaultWorkers
	}
	workers = min(workers, maxPolls)
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	a.logf("proxy %q: serving %s with %d workers", a.cfg.Name, a.cfg.Server, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() { a.work(ctx, cancel) })
	}
	wg.Wait()
	if err := context.Cause(ctx); errors.Is(err, ErrRevoked) {
		return ErrRevoked
	}
	return nil
}

// disabledWait is how often a proxy disabled on the server asks again.
var disabledWait = time.Minute

// backoff is the wait after the try-th failure in a row to reach the
// server: doubling from a second up to a minute, spread a little.
func backoff(try int) time.Duration {
	d := time.Second << min(try, 6)
	d = min(d, time.Minute)
	return d/2 + rand.N(d/2+1)
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// work polls for jobs and does them, one at a time.
func (a *Agent) work(ctx context.Context, stop context.CancelCauseFunc) {
	failures := 0
	for ctx.Err() == nil {
		j, status, err := a.poll(ctx)
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			if failures == 0 {
				a.logf("proxy: cannot reach %s: %v", a.cfg.Server, err)
			}
			sleep(ctx, backoff(failures))
			failures++
			continue
		case status == http.StatusUnauthorized:
			stop(ErrRevoked)
			return
		case status == http.StatusForbidden:
			a.setDisabled(true)
			sleep(ctx, disabledWait)
			continue
		case status == http.StatusNoContent:
		case status == http.StatusOK:
			a.do(ctx, j)
		default:
			if failures == 0 {
				a.logf("proxy: %s answered HTTP %d to a poll", a.cfg.Server, status)
			}
			sleep(ctx, backoff(failures))
			failures++
			continue
		}
		if failures > 0 {
			a.logf("proxy: %s reached again", a.cfg.Server)
		}
		failures = 0
		a.setDisabled(false)
	}
}

func (a *Agent) setDisabled(on bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.disabled != on {
		a.logf("proxy %q: %s on the server", a.cfg.Name, map[bool]string{true: "disabled, waiting to be enabled", false: "enabled"}[on])
	}
	a.disabled = on
}

// poll asks the server for a job.
func (a *Agent) poll(ctx context.Context) (Job, int, error) {
	req, err := a.request(ctx, http.MethodGet, "poll", nil)
	if err != nil {
		return Job{}, 0, err
	}
	resp, err := a.api.Do(req)
	if err != nil {
		return Job{}, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return Job{}, resp.StatusCode, nil
	}
	var j Job
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&j); err != nil {
		return Job{}, 0, fmt.Errorf("unreadable job: %w", err)
	}
	return j, http.StatusOK, nil
}

// do makes a job's request and sends the answer back.
func (a *Agent) do(ctx context.Context, j Job) {
	start := time.Now()
	res, body := a.fetchJob(ctx, j)
	res.Millis = time.Since(start).Milliseconds()
	if res.Error != "" {
		a.logf("proxy: %s %s: %s", j.Method, j.URL, res.Error)
	}
	meta, _ := json.Marshal(res)
	req, err := a.request(ctx, http.MethodPost, "jobs/"+j.ID, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set(ResultHeader, string(meta))
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := a.api.Do(req)
	if err != nil {
		a.logf("proxy: sending the answer to %s back: %v", j.URL, err)
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	resp.Body.Close()
}

// fetchJob makes a job's request, if it is one a proxy makes at all.
func (a *Agent) fetchJob(ctx context.Context, j Job) (Result, []byte) {
	if !Allowed(j.Method, j.URL) {
		return Result{Error: "refused by the proxy: only GET and HEAD of jw.org's sites over https"}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, answerWait-5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, j.Method, j.URL, nil)
	if err != nil {
		return Result{Error: err.Error()}, nil
	}
	req.Header = Header(j.Header, true)
	resp, err := a.fetch.Do(req)
	if err != nil {
		return Result{Error: err.Error()}, nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody+1))
	if err != nil {
		return Result{Error: "reading the answer: " + err.Error()}, nil
	}
	if len(body) > MaxBody {
		return Result{Error: fmt.Sprintf("the answer is larger than %d MB", MaxBody>>20)}, nil
	}
	return Result{Status: resp.StatusCode, Header: AnswerHeader(resp.Header)}, body
}
