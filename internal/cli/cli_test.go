package cli

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/remote"
	"github.com/dgrieser/jw-cli/internal/server"
)

// runCmd executes the jw command tree with all base URLs pointed at a mock
// server built from mux, returning captured stdout. The same command is then
// run in remote mode (jw --server) against a jw serve built on the same mock,
// and must write exactly the same and fail exactly the same: every command
// test is a test of remote mode too.
func runCmd(t *testing.T, mux *http.ServeMux, args ...string) (string, error) {
	t.Helper()
	upstream := httptest.NewServer(mux)
	t.Cleanup(upstream.Close)
	out, err := runLocalCmd(t, upstream.URL, args...)
	rout, rerr := runRemoteCmd(t, upstream.URL, args...)
	checkParity(t, args, out, err, rout, rerr)
	return out, err
}

// checkParity holds a command run in remote mode to what it does locally.
func checkParity(t *testing.T, args []string, out string, err error, rout string, rerr error) {
	t.Helper()
	if unstable(rout) != unstable(out) {
		t.Errorf("jw --server %s writes differently than locally:\n--- local\n%s\n--- remote\n%s", strings.Join(args, " "), out, rout)
	}
	if fmt.Sprint(rerr) != fmt.Sprint(err) {
		t.Errorf("jw --server %s fails differently than locally:\nlocal:  %v\nremote: %v", strings.Join(args, " "), err, rerr)
	}
}

// stamp is the time a listing was saved, and rate the speed a progress bar
// shows: two runs never share them.
var (
	stamp = regexp.MustCompile(`"timestamp": "[^"]*"`)
	rate  = regexp.MustCompile(`\([0-9.]+ [kMGT]?B/s\)`)
)

// unstable leaves out of an output what differs from run to run.
func unstable(out string) string {
	return rate.ReplaceAllString(stamp.ReplaceAllString(out, ""), "")
}

// runCmdHere runs a command here only, for a test that counts the requests
// the mock sees: a second run in remote mode would count twice. What it
// writes is held to remote mode by every other test.
func runCmdHere(t *testing.T, mux *http.ServeMux, args ...string) (string, error) {
	t.Helper()
	upstream := httptest.NewServer(mux)
	t.Cleanup(upstream.Close)
	return runLocalCmd(t, upstream.URL, args...)
}

// runRemoteCmd runs a command in remote mode: on a jw serve whose upstream is
// the mock, from a client with a cache of its own.
func runRemoteCmd(t *testing.T, upstream string, args ...string) (string, error) {
	t.Helper()
	sa := app.New(app.Flags{BaseCDN: upstream, BaseJWOrg: upstream, BaseWOL: upstream, CacheDir: t.TempDir()})
	svc := sa.Service()
	srv := server.New(server.Config{Svc: svc, RunCLI: func(ctx context.Context, req remote.Request) remote.Response {
		return RunForClient(ctx, svc, req)
	}})
	jw := httptest.NewServer(srv.Handler())
	t.Cleanup(jw.Close)

	a := app.New(app.Flags{})
	var out bytes.Buffer
	a.Stdout = &out
	a.Stderr = &out
	root := NewRootCmd(a)
	if !slices.Contains(args, "--cache-dir") {
		args = append([]string{"--cache-dir", t.TempDir()}, args...)
	}
	root.SetArgs(append([]string{"--server", jw.URL}, args...))
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.Execute()
	return out.String(), err
}

// runLocalCmd runs a command here, against the mock.
func runLocalCmd(t *testing.T, upstream string, args ...string) (string, error) {
	t.Helper()

	a := app.New(app.Flags{})
	var out bytes.Buffer
	a.Stdout = &out
	a.Stderr = &out

	root := NewRootCmd(a)
	if !slices.Contains(args, "--cache-dir") {
		args = append([]string{"--cache-dir", t.TempDir()}, args...)
	}
	full := append([]string{
		"--base-cdn", upstream, "--base-jworg", upstream, "--base-wol", upstream,
	}, args...)
	root.SetArgs(full)
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.Execute()
	return out.String(), err
}

// fixture serves a testdata file with the given content type.
func fixture(t *testing.T, path, contentType string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := os.ReadFile(filepath.Join("testdata", path))
		if err != nil {
			t.Errorf("fixture %s: %v", path, err)
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Write(b)
	}
}

func languagesMux(t *testing.T) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/apis/mediator/v1/languages/E/all", fixture(t, "languages.json", "application/json"))
	return mux
}

func TestLanguagesCommand(t *testing.T) {
	out, err := runCmd(t, languagesMux(t), "languages")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"SYMBOL", "X", "de", "German", "Deutsch"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestLanguagesSearch(t *testing.T) {
	out, err := runCmd(t, languagesMux(t), "languages", "-s", "german")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "German") || strings.Contains(out, "French") {
		t.Errorf("filter failed:\n%s", out)
	}
}

func TestLanguagesJSON(t *testing.T) {
	out, err := runCmd(t, languagesMux(t), "languages", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"symbol": "X"`) || !strings.Contains(out, `"locale": "de"`) {
		t.Errorf("json output unexpected:\n%s", out)
	}
}
