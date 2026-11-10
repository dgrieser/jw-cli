package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/remote"
	"github.com/dgrieser/jw-cli/internal/results"
	"github.com/dgrieser/jw-cli/internal/service"
)

// fakeServe is a jw serve that answers POST /api/v1/cli from a script,
// recording every request.
func fakeServe(t *testing.T, answer func(n int, req remote.Request) remote.Response) (string, *[]remote.Request) {
	t.Helper()
	var reqs []remote.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != remote.Path {
			http.NotFound(w, r)
			return
		}
		var req remote.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("request: %v", err)
		}
		reqs = append(reqs, req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(answer(len(reqs), req))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &reqs
}

// runClient runs a command line in remote mode against server.
func runClient(t *testing.T, server, cacheDir string, args ...string) (string, error) {
	t.Helper()
	a := app.New(app.Flags{})
	var out bytes.Buffer
	a.Stdout, a.Stderr = &out, &out
	root := NewRootCmd(a)
	root.SetArgs(append([]string{"--server", server, "--cache-dir", cacheDir}, args...))
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.Execute()
	return out.String(), err
}

// An unfold level that needs many requests is asked about on the client, as
// it is locally: with nobody at the terminal, the run stops with the local
// run's error rather than spending the requests on the server.
func TestRemoteConfirmStopsWithoutATerminal(t *testing.T) {
	server, reqs := fakeServe(t, func(int, remote.Request) remote.Response {
		return remote.Response{Error: &remote.Error{Code: remote.CodeConfirm, Message: "waiting",
			Confirm: &remote.Confirm{Level: 2, Requests: 3000}}}
	})
	_, err := runClient(t, server, t.TempDir(), "bible", "read", "John 3:16", "--unfold", "2")
	if err == nil || !strings.Contains(err.Error(), "unfolding level 2 needs 3000 requests") ||
		!strings.Contains(err.Error(), "--yes") {
		t.Fatalf("want the local run's error, got %v", err)
	}
	if len(*reqs) != 1 {
		t.Errorf("sent %d requests, want 1", len(*reqs))
	}
}

// On the server, a level the client answered goes by the answer; any other
// waits for one.
func TestCapturedConfirmerTakesTheClientsAnswers(t *testing.T) {
	c := app.NewCapture(&remote.Request{Answers: map[int]bool{1: true, 2: false}})
	a := app.NewCaptured(app.Flags{}, &service.Service{}, c)
	confirm := unfoldConfirmer(a, false)
	for level, want := range map[int]bool{1: true, 2: false} {
		if yes, err := confirm(level, 3000); err != nil || yes != want {
			t.Errorf("level %d: %v, %v; want %v", level, yes, err, want)
		}
	}
	_, err := confirm(3, 4000)
	var ce *remote.ConfirmError
	if !errors.As(err, &ce) || ce.Level != 3 || ce.Requests != 4000 {
		t.Errorf("unanswered level: %v", err)
	}
	if unfoldConfirmer(a, true) != nil {
		t.Error("--yes asks nothing, on the server too")
	}
}

// A command answered with events is carried out here: written, its listing
// kept for jw show|download <n>, its downloads made by the client.
func TestRemoteReplaysWhatTheCommandDid(t *testing.T) {
	files := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("PDF"))
	}))
	t.Cleanup(files.Close)
	server, reqs := fakeServe(t, func(n int, req remote.Request) remote.Response {
		if n == 1 {
			return remote.Response{
				Language: &model.Language{Symbol: "X", Locale: "de"},
				Events: []remote.Event{
					{Kind: remote.KindStderr, Text: "warning: partial\n"},
					{Kind: remote.KindListing, Header: "Treffer", Listing: &results.ResultSet{Kind: "pub-files",
						Items: []model.Result{{Kind: "file", Title: "Der Wachtturm", FileURL: files.URL + "/w.pdf"}}}},
				},
			}
		}
		var dir string
		for _, arg := range req.Args {
			if d, ok := strings.CutPrefix(arg, "--dir="); ok {
				dir = d
			}
		}
		return remote.Response{Events: []remote.Event{
			{Kind: remote.KindDownload, Download: &remote.Download{URL: req.Listing.Items[0].FileURL, Dir: dir}},
		}}
	})
	cache, dir := t.TempDir(), t.TempDir()
	out, err := runClient(t, server, cache, "pub", "w")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "warning: partial") || !strings.Contains(out, "Treffer") || !strings.Contains(out, "1.") {
		t.Errorf("listing not written:\n%s", out)
	}
	if rs, err := results.Load(cache); err != nil || len(rs.Items) != 1 || rs.Items[0].Index != 1 {
		t.Fatalf("listing not kept: %+v, %v", rs, err)
	}
	// the dir goes to the server and comes back with the download: the file
	// lands where the client says
	out, err = runClient(t, server, cache, "-l", "de", "download", "-d", dir, "1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Heruntergeladen") {
		t.Errorf("download not reported in the server's language:\n%s", out)
	}
	last := (*reqs)[len(*reqs)-1]
	if last.Listing == nil || len(last.Listing.Items) != 1 {
		t.Errorf("download sent no listing: %+v", last)
	}
	for _, want := range []string{"--lang=de", "--dir=" + dir, "download", "--", "1"} {
		if !slices.Contains(last.Args, want) {
			t.Errorf("args %q lack %q", last.Args, want)
		}
	}
}

// What only concerns this machine stays here; the language always goes.
func TestRemoteArgs(t *testing.T) {
	server, reqs := fakeServe(t, func(int, remote.Request) remote.Response { return remote.Response{} })
	if _, err := runClient(t, server, t.TempDir(), "search", "-e", "wol", "--exclude", "bi,dx",
		"--no-color", "-f", t.TempDir()+"/out.md", "--", "-faith"); err != nil {
		t.Fatal(err)
	}
	args := (*reqs)[0].Args
	for _, want := range []string{"search", "--engine=wol", "--exclude=bi,dx", "--", "-faith"} {
		if !slices.Contains(args, want) {
			t.Errorf("args %q lack %q", args, want)
		}
	}
	if !slices.ContainsFunc(args, func(s string) bool { return strings.HasPrefix(s, "--lang=") }) {
		t.Errorf("args %q send no language", args)
	}
	for _, arg := range args {
		for _, local := range []string{"--no-color", "--file", "--server", "--cache-dir"} {
			if strings.HasPrefix(arg, local) {
				t.Errorf("args %q send %s", args, local)
			}
		}
	}
	if (*reqs)[0].Listing != nil {
		t.Error("a listing was sent with a command that takes no index")
	}
}

// jw open acts on this machine and never asks the server.
func TestRemoteOpenRunsHere(t *testing.T) {
	server, reqs := fakeServe(t, func(int, remote.Request) remote.Response { return remote.Response{} })
	cache := t.TempDir()
	rs := results.ResultSet{Kind: "search", Items: []model.Result{{Title: "A", JWLink: "https://www.jw.org/a"}}}
	if err := results.Save(cache, &rs); err != nil {
		t.Fatal(err)
	}
	out, err := runClient(t, server, cache, "open", "1")
	if err != nil || !strings.Contains(out, "https://www.jw.org/a") {
		t.Fatalf("open 1: %v\n%s", err, out)
	}
	if len(*reqs) != 0 {
		t.Errorf("open asked the server: %+v", *reqs)
	}
}

// The server runs the commands a client sends, and nothing else.
func TestRunForClientRefuses(t *testing.T) {
	svc := app.New(app.Flags{CacheDir: t.TempDir()}).Service()
	for _, args := range [][]string{
		{"serve"}, {"open", "1"}, {"bible"}, {"completion", "bash"}, {"nonsense"},
		{"search", "-i", "kingdom"}, {"media", "browse", "--interactive"},
	} {
		resp := RunForClient(context.Background(), svc, remote.Request{Args: args})
		if resp.Error == nil {
			t.Errorf("%q was run for a client", args)
		}
	}
}

// jw bible intro --download leaves the video to the client: the server
// records the download, saves nothing, and the client prints the bare path as
// a local run does.
func TestRemoteBibleIntroDownloadsOnTheClient(t *testing.T) {
	upstream := httptest.NewServer(bookDocsMux(t))
	t.Cleanup(upstream.Close)
	svc := app.New(app.Flags{BaseCDN: upstream.URL, BaseJWOrg: upstream.URL, BaseWOL: upstream.URL,
		CacheDir: t.TempDir()}).Service()
	dir := t.TempDir()
	resp := RunForClient(context.Background(), svc, remote.Request{
		Args: []string{"bible", "intro", "--lang=en", "--download", "--dir=" + dir, "--", "40"}})
	if resp.Error != nil {
		t.Fatalf("%+v", resp.Error)
	}
	if len(resp.Events) != 1 || resp.Events[0].Kind != remote.KindDownload {
		t.Fatalf("want one download event, got %+v", resp.Events)
	}
	if d := resp.Events[0].Download; d.Dir != dir || !d.PathOnly || d.URL == "" {
		t.Errorf("download %+v", d)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("the server saved %v", entries)
	}
}

// $JW_LANG is the language when -l is not given, and goes to the server like
// -l does; -l wins over it.
func TestRemoteLangFromEnv(t *testing.T) {
	t.Setenv("JW_LANG", "X")
	server, reqs := fakeServe(t, func(int, remote.Request) remote.Response { return remote.Response{} })
	if _, err := runClient(t, server, t.TempDir(), "search", "faith"); err != nil {
		t.Fatal(err)
	}
	if args := (*reqs)[0].Args; !slices.Contains(args, "--lang=X") {
		t.Errorf("args %q lack --lang=X from $JW_LANG", args)
	}
	if _, err := runClient(t, server, t.TempDir(), "-l", "E", "search", "faith"); err != nil {
		t.Fatal(err)
	}
	if args := (*reqs)[1].Args; !slices.Contains(args, "--lang=E") || slices.Contains(args, "--lang=X") {
		t.Errorf("args %q: -l E should win over $JW_LANG", args)
	}
}
