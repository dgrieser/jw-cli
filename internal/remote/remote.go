// Package remote carries a command of the jw CLI to a running jw serve and
// its output back: the client sends the command line, the server runs the very
// same command against its own service — its caches, what it read ahead, what
// its expansions keep in memory — and answers with what the command wrote, in
// the order it wrote it. What has to happen on the reader's machine — styling
// for the terminal, saving the listing that jw show|open|download <n> act on,
// writing a downloaded file, asking before an expensive unfold — is not done on
// the server; it comes back as an event for the client to carry out.
//
// Because the server runs the CLI's own code, remote output is the local
// output by construction. What keeps the two in sync is that every command
// writes through the app's writers and the cli package's helpers for listings,
// downloads and confirmations, which record an event when the command runs
// for a client (see app.Capture).
package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/results"
)

// Path is the endpoint of jw serve that runs a command.
const Path = "/api/v1/cli"

// Request is one command line to run on the server.
type Request struct {
	// Args is the command line without the program name: the command path,
	// its arguments and the flags given, as on the command line.
	Args []string `json:"args"`
	// Listing is the client's last listing, which a command taking an index
	// (jw show 3, jw download 2) resolves against: listings are the reader's
	// own and never kept on the server.
	Listing *results.ResultSet `json:"listing,omitempty"`
	// Answers are the reader's answers to unfold confirmations already asked,
	// by level: a level answered here is not asked about again.
	Answers map[int]bool `json:"answers,omitempty"`
}

// Event kinds, in the order a command wrote them.
const (
	// KindWrite is output written as is (App.Write).
	KindWrite = "write"
	// KindWritef is output written without the line ending Write adds
	// (App.Writef).
	KindWritef = "writef"
	// KindMarkdown is a markdown body, styled for a terminal by the client
	// (App.WriteMarkdown).
	KindMarkdown = "markdown"
	// KindStderr is a line written to stderr: a warning.
	KindStderr = "stderr"
	// KindListing is a numbered listing, rendered and saved by the client.
	KindListing = "listing"
	// KindSave is a listing saved without being printed (jw media info saves
	// the files it details).
	KindSave = "save"
	// KindDownload is a file for the client to download and report.
	KindDownload = "download"
)

// Event is one thing a command did that the client carries out.
type Event struct {
	Kind     string             `json:"kind"`
	Text     string             `json:"text,omitempty"`
	Header   string             `json:"header,omitempty"`
	Listing  *results.ResultSet `json:"listing,omitempty"`
	Download *Download          `json:"download,omitempty"`
}

// Download is one file to fetch: from the public CDN, straight to the reader.
type Download struct {
	URL      string `json:"url"`
	Checksum string `json:"checksum,omitempty"`
	Size     int64  `json:"size,omitempty"`
	Dir      string `json:"dir,omitempty"`
	Name     string `json:"name,omitempty"`
	// Wrap prefixes the error a failed download reports ("subtitles").
	Wrap string `json:"wrap,omitempty"`
	// PathOnly reports the saved file by its bare path rather than the
	// "Downloaded" line, as jw bible intro --download prints it.
	PathOnly bool `json:"path_only,omitempty"`
}

// Response is what the command did, and how it ended.
type Response struct {
	Events []Event `json:"events"`
	// Language is the content language the command resolved, if it did: the
	// client words what it adds itself — listing labels, prompts — in it.
	Language *model.Language `json:"language,omitempty"`
	// Version is the server's build, so a client can say when it differs.
	Version string `json:"version,omitempty"`
	// Error is how the command failed, after writing the events.
	Error *Error `json:"error,omitempty"`
}

// Error codes of a failed command.
const (
	// CodeFailed is a command that returned an error.
	CodeFailed = "failed"
	// CodeConfirm is an unfold level waiting for the reader's answer.
	CodeConfirm = "confirm"
)

// Error is a command's failure.
type Error struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Confirm *Confirm `json:"confirm,omitempty"`
}

func (e *Error) Error() string { return e.Message }

// Confirm is the question an unfold asks before a level that needs many
// requests.
type Confirm struct {
	Level    int `json:"level"`
	Requests int `json:"requests"`
}

// ConfirmError stops a command run for a client at an unfold level nobody has
// answered for yet.
type ConfirmError struct{ Confirm }

func (e *ConfirmError) Error() string {
	return fmt.Sprintf("unfolding level %d needs %d requests: waiting for an answer", e.Level, e.Requests)
}

// Client sends commands to one jw serve.
type Client struct {
	// Base is the server's address, without the endpoint path.
	Base string
	// User and Password log in with basic authentication when User is set:
	// the email address and password of an account of the server.
	User, Password string
	HTTP           *http.Client
}

// NewClient parses a server address. Credentials in it (https://user:pw@host)
// log in; otherwise user and password do, when user is set.
func NewClient(addr, user, password string) (*Client, error) {
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	u, err := url.Parse(addr)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("invalid server address %q (want http://host:port)", addr)
	}
	c := &Client{User: user, Password: password, HTTP: &http.Client{}}
	if u.User != nil {
		c.User = u.User.Username()
		c.Password, _ = u.User.Password()
		u.User = nil
	}
	c.Base = strings.TrimRight(u.String(), "/")
	return c, nil
}

// Run sends one command and returns what it did. A command that failed on the
// server is a Response with Error set, not an error: the events it wrote
// before failing are still to be carried out. The error is for a request that
// did not get that far.
func (c *Client) Run(ctx context.Context, req Request) (Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return Response{}, err
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+Path, bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("Accept", "application/json")
	if c.User != "" {
		hr.SetBasicAuth(c.User, c.Password)
	}
	resp, err := c.HTTP.Do(hr)
	if err != nil {
		return Response{}, fmt.Errorf("jw serve at %s: %w", c.Base, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, fmt.Errorf("jw serve at %s: %w", c.Base, err)
	}
	if resp.StatusCode != http.StatusOK {
		return Response{}, statusError(c.Base, resp.StatusCode, data)
	}
	var out Response
	if err := json.Unmarshal(data, &out); err != nil {
		return Response{}, fmt.Errorf("jw serve at %s: unreadable answer: %w", c.Base, err)
	}
	return out, nil
}

// statusError words a refused request: the API's own error message where it
// sent one.
func statusError(base string, status int, body []byte) error {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	msg := strings.TrimSpace(string(body))
	if json.Unmarshal(body, &envelope) == nil && envelope.Error.Message != "" {
		msg = envelope.Error.Message
	}
	switch status {
	case http.StatusUnauthorized:
		return fmt.Errorf("jw serve at %s refused the login (%s): set the account's email address and password in $JW_SERVER_USER and $JW_SERVER_PASSWORD, or in the address (http://name%%40example.org:password@host)", base, msg)
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return fmt.Errorf("jw serve at %s does not run commands (HTTP %d): it is older than this client, or not jw serve", base, status)
	}
	if msg == "" {
		msg = http.StatusText(status)
	}
	return errors.New("jw serve at " + base + ": " + msg)
}
