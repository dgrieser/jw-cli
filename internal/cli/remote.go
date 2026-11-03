package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/lang"
	"github.com/dgrieser/jw-cli/internal/model"
	"github.com/dgrieser/jw-cli/internal/remote"
	"github.com/dgrieser/jw-cli/internal/results"
	"github.com/dgrieser/jw-cli/internal/service"
	"github.com/dgrieser/jw-cli/internal/tui"
	"github.com/dgrieser/jw-cli/internal/version"
)

// Remote mode: with --server (or $JW_SERVER) every command runs on that jw
// serve rather than here. The server runs the same command — this package's
// code — against its own service, and answers with what the command wrote;
// the client styles it, keeps the listing, downloads the files and asks the
// questions. See package remote.
//
// Every runnable command is sent, unless it is marked runsHere: there is no
// list of remote commands to keep up to date. What a new command must do to
// work both ways is write through the app (Write, WriteMarkdown, WriteJSON),
// list through writeListing, keep a listing through saveListing, read one
// through listingItem, download through downloadFile, and ask through
// unfoldConfirmer — never through a.Cache().Dir(), downloadURL or os.Stdin
// directly. TestRemoteParity holds every test of the package to that.

// Command annotations of remote mode.
const (
	// remoteAnnotation marks how a command runs with --server.
	remoteAnnotation = "jw/remote"
	// runsHere is a command that never goes to the server: it acts on the
	// reader's machine only (jw open), or is the server (jw serve).
	runsHere = "local"
	// runsThere is a command sent to the server; the server runs nothing
	// else.
	runsThere = "remote"
	// listingAnnotation marks a command that takes an index into the last
	// listing, which is sent along with it.
	listingAnnotation = "jw/listing"
)

// clientFlags are the flags that are about this machine — where output and
// the cache go, how the terminal is styled, which server to ask — and are not
// sent along. The language is sent by remoteCall itself.
var clientFlags = map[string]bool{
	"file": true, "no-color": true, "verbose": true, "server": true,
	"base-cdn": true, "base-jworg": true, "base-wol": true,
	"cache-dir": true, "cache-ttl": true, "cache-max": true,
	"interactive": true, "lang": true, "help": true,
}

// runHere marks a command as one that never goes to the server.
func runHere(cmd *cobra.Command) *cobra.Command {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[remoteAnnotation] = runsHere
	return cmd
}

// takesListing marks a command as one that takes an index into the last
// listing.
func takesListing(cmd *cobra.Command) *cobra.Command {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[listingAnnotation] = "true"
	return cmd
}

// wireRemote has every runnable command of the tree under cmd run on the
// server when the app is in remote mode.
func wireRemote(a *app.App, cmd *cobra.Command) {
	for _, c := range cmd.Commands() {
		wireRemote(a, c)
	}
	if cmd.RunE == nil || cmd.Annotations[remoteAnnotation] == runsHere {
		return
	}
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[remoteAnnotation] = runsThere
	run := cmd.RunE
	cmd.RunE = func(c *cobra.Command, args []string) error {
		interactive := flagOn(c, "interactive")
		switch {
		case a.Capture != nil && interactive:
			return errors.New("the interactive browser runs on the client, not on jw serve")
		case !a.Remote():
			return run(c, args)
		case interactive:
			return runRemoteTUI(c.Context(), a, c, args)
		}
		return runRemote(c.Context(), a, c, args)
	}
}

// flagOn reports whether a boolean flag of cmd is set.
func flagOn(cmd *cobra.Command, name string) bool {
	f := cmd.Flags().Lookup(name)
	return f != nil && f.Value.String() == "true"
}

// remoteArgs is the command line to send for cmd: its path, every flag given
// that is not about this machine, and its arguments.
func remoteArgs(cmd *cobra.Command, args []string) []string {
	out := strings.Fields(cmd.CommandPath())[1:]
	cmd.Flags().Visit(func(f *pflag.Flag) {
		if clientFlags[f.Name] {
			return
		}
		out = append(out, "--"+f.Name+"="+flagValue(f))
	})
	// after the flags, so an argument starting with a dash stays one
	return append(append(out, "--"), args...)
}

// flagValue spells a flag's value the way it is given on the command line.
func flagValue(f *pflag.Flag) string {
	if sv, ok := f.Value.(pflag.SliceValue); ok {
		return strings.Join(sv.GetSlice(), ",")
	}
	return f.Value.String()
}

// remoteClient is the client of the server the app names.
func remoteClient(a *app.App) (*remote.Client, error) {
	return remote.NewClient(a.Flags.Server, os.Getenv("JW_SERVER_USER"), os.Getenv("JW_SERVER_PASSWORD"))
}

// versionWarned keeps the warning about a server of another version to one
// per run.
var versionWarned sync.Once

// remoteCall sends one command line and takes over what the server says about
// the run itself: its language, and its version. The language is always sent:
// the reader's, or the one this machine's locale names, as a local run would
// read it — never the server's.
func remoteCall(ctx context.Context, a *app.App, req remote.Request) (remote.Response, error) {
	c, err := remoteClient(a)
	if err != nil {
		return remote.Response{}, err
	}
	spec := a.Flags.Lang
	if spec == "" {
		spec = lang.DetectBCP47()
	}
	req.Args = withFlags(req.Args, "--lang="+spec)
	resp, err := c.Run(ctx, req)
	if err != nil {
		return resp, err
	}
	if resp.Language != nil {
		a.SetLanguage(*resp.Language)
	}
	if v := version.String(); resp.Version != "" && resp.Version != v && !devBuild(v) && !devBuild(resp.Version) {
		versionWarned.Do(func() {
			fmt.Fprintf(a.Stderr, "warning: jw serve at %s runs %s, this client is %s; the output follows the server's\n",
				c.Base, resp.Version, v)
		})
	}
	return resp, nil
}

// devBuild reports whether a version names a build from a working tree or a
// branch rather than a release: those differ from build to build.
func devBuild(v string) bool { return v == "" || strings.HasPrefix(v, "dev") }

// runRemote runs cmd on the server and carries out what it did. An unfold
// level that needs many requests is asked about here, as a local run asks,
// and the command is sent again with the answer.
func runRemote(ctx context.Context, a *app.App, cmd *cobra.Command, args []string) error {
	req := remote.Request{Args: remoteArgs(cmd, args)}
	if cmd.Annotations[listingAnnotation] != "" {
		if rs, err := results.Load(a.Cache().Dir()); err == nil {
			req.Listing = &rs
		}
	}
	var confirm func(level, requests int) (bool, error)
	for {
		resp, err := remoteCall(ctx, a, req)
		if err != nil {
			return err
		}
		if e := resp.Error; e != nil && e.Code == remote.CodeConfirm && e.Confirm != nil {
			// what the run wrote before the question is written again by
			// the run that follows the answer
			if confirm == nil {
				confirm = unfoldConfirmer(a, false)
			}
			yes, err := confirm(e.Confirm.Level, e.Confirm.Requests)
			if err != nil {
				return err
			}
			if req.Answers == nil {
				req.Answers = map[int]bool{}
			}
			req.Answers[e.Confirm.Level] = yes
			continue
		}
		if err := replay(ctx, a, resp.Events); err != nil {
			return err
		}
		if resp.Error != nil {
			return resp.Error
		}
		return nil
	}
}

// replay carries out what a command did on the server, in order, the way the
// command does it when it runs here.
func replay(ctx context.Context, a *app.App, events []remote.Event) error {
	for _, e := range events {
		var err error
		switch e.Kind {
		case remote.KindWrite:
			err = a.Write(e.Text)
		case remote.KindWritef:
			err = a.Writef("%s", e.Text)
		case remote.KindMarkdown:
			err = a.WriteMarkdown(e.Text)
		case remote.KindStderr:
			_, err = io.WriteString(a.Stderr, e.Text)
		case remote.KindListing:
			if e.Listing == nil {
				return errors.New("jw serve sent a listing without its rows")
			}
			err = writeListing(a, *e.Listing, e.Header)
		case remote.KindSave:
			if e.Listing == nil {
				return errors.New("jw serve sent a listing without its rows")
			}
			err = saveListing(a, e.Listing)
		case remote.KindDownload:
			if e.Download == nil {
				return errors.New("jw serve sent a download without its file")
			}
			err = downloadFile(ctx, a, *e.Download)
		default:
			return fmt.Errorf("jw serve sent %q, which this client does not know: update it", e.Kind)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// remoteOutput runs a command line on the server for what it writes: the
// -o json of a command, which a client reads back into the model it printed.
func remoteOutput(ctx context.Context, a *app.App, req remote.Request) (string, error) {
	resp, err := remoteCall(ctx, a, req)
	if err != nil {
		return "", err
	}
	if resp.Error != nil {
		return "", resp.Error
	}
	var b strings.Builder
	for _, e := range resp.Events {
		switch e.Kind {
		case remote.KindWrite, remote.KindWritef, remote.KindMarkdown:
			b.WriteString(e.Text)
		}
	}
	return b.String(), nil
}

// remoteJSON runs a command line with -o json on the server and reads what it
// printed into v.
func remoteJSON(ctx context.Context, a *app.App, req remote.Request, v any) error {
	req.Args = withFlags(req.Args, "--output=json")
	out, err := remoteOutput(ctx, a, req)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(out), v); err != nil {
		return fmt.Errorf("jw serve: unreadable answer to %s: %w", strings.Join(req.Args, " "), err)
	}
	return nil
}

// remoteArticle is fetchArticle on the server: jw article -o json, read back
// into the article it printed.
func remoteArticle(ctx context.Context, a *app.App, target string) (model.Article, error) {
	var art model.Article
	err := remoteJSON(ctx, a, remote.Request{Args: []string{"article", "--", target}}, &art)
	return art, err
}

// remoteMediaOf is mediaOf on the server: jw show -o json on the row alone,
// read back into the media item it printed. ok is false for a row that names
// no media item, which needs no server to tell.
func remoteMediaOf(ctx context.Context, a *app.App, r model.Result) (model.MediaItem, bool, error) {
	if !isMediaLANK(r.LANK) && !isGalleryVideo(r) {
		return model.MediaItem{}, false, nil
	}
	r.Index = 1
	var item model.MediaItem
	err := remoteJSON(ctx, a, remote.Request{
		Args:    []string{"show", "--", "1"},
		Listing: &results.ResultSet{Kind: "media", Items: []model.Result{r}},
	}, &item)
	return item, true, err
}

// remoteListing runs a listing command on the server for its listing: the rows
// and the header it is printed under.
func remoteListing(ctx context.Context, a *app.App, args []string) (results.ResultSet, string, error) {
	req := remote.Request{Args: args}
	resp, err := remoteCall(ctx, a, req)
	if err != nil {
		return results.ResultSet{}, "", err
	}
	if resp.Error != nil {
		return results.ResultSet{}, "", resp.Error
	}
	for _, e := range resp.Events {
		if e.Kind == remote.KindListing && e.Listing != nil {
			return *e.Listing, e.Header, nil
		}
	}
	return results.ResultSet{}, "", errors.New("jw serve listed nothing")
}

// withoutFlag drops a flag from a command line remoteArgs spelled.
func withoutFlag(args []string, name string) []string {
	out := args[:0:0]
	for _, arg := range args {
		if !strings.HasPrefix(arg, "--"+name+"=") {
			out = append(out, arg)
		}
	}
	return out
}

// withFlags adds flags to a command line, ahead of its arguments.
func withFlags(args []string, flags ...string) []string {
	i := len(args)
	for j, arg := range args {
		if arg == "--" {
			i = j
			break
		}
	}
	out := append(append(args[:i:i], flags...), args[i:]...)
	return out
}

// runRemoteTUI browses a listing of the server in the interactive browser,
// which runs here: every page, every item shown and every category opened is
// asked of the server.
func runRemoteTUI(ctx context.Context, a *app.App, cmd *cobra.Command, args []string) error {
	base := remoteArgs(cmd, args)
	var (
		fetch  tui.Fetcher
		header func() string
	)
	switch cmd.CommandPath() {
	case "jw search":
		fetch = remoteSearchFetcher(ctx, a, withoutFlag(base, "page"))
		header = func() string { return fmt.Sprintf(a.Text().SearchHeader, strings.Join(args, " ")) }
	case "jw media browse":
		key := argOrEmpty(args)
		fetch = remoteCategoryFetcher(ctx, a, key)
		header = func() string { return browseHeader(key) }
	case "jw bible cited":
		var first string
		fetch = func(page int) (results.ResultSet, string, error) {
			if page > 1 {
				return results.ResultSet{}, "", errors.New(a.Text().NoMoreResults)
			}
			rs, h, err := remoteListing(ctx, a, base)
			if err != nil {
				return rs, h, err
			}
			first = h
			_ = saveListing(a, &rs)
			return rs, h, nil
		}
		header = func() string { return first }
	default:
		return fmt.Errorf("%s -i is not available with --server", cmd.CommandPath())
	}
	// the first page answers what the header is worded in
	rs, h, err := fetch(1)
	if err != nil {
		return err
	}
	once := true
	pages := func(page int) (results.ResultSet, string, error) {
		if page == 1 && once {
			once = false
			return rs, h, nil
		}
		return fetch(page)
	}
	lng, _ := a.ResolvedLanguage()
	return tui.Run(header(), pages, tuiActions(ctx, a, lng))
}

// remoteSearchFetcher is searchFetcher on the server: each page is jw search
// --page n there.
func remoteSearchFetcher(ctx context.Context, a *app.App, base []string) tui.Fetcher {
	listed := map[string]int{}
	return func(page int) (results.ResultSet, string, error) {
		args := withFlags(base, fmt.Sprintf("--page=%d", page))
		rs, header, err := remoteListing(ctx, a, args)
		if err != nil {
			return results.ResultSet{}, "", err
		}
		if len(rs.Items) == 0 && page > 1 {
			return results.ResultSet{}, "", errors.New(a.Text().NoMoreResults)
		}
		rs.Items = dropRepeats(rs.Items, page, listed)
		_ = saveListing(a, &rs)
		return rs, header, nil
	}
}

// remoteCategoryFetcher is categoryFetcher on the server: each page is jw
// media browse there, a page's worth at its offset.
func remoteCategoryFetcher(ctx context.Context, a *app.App, key string) tui.Fetcher {
	return func(page int) (results.ResultSet, string, error) {
		args := []string{"media", "browse"}
		if key != "" {
			args = append(args, fmt.Sprintf("--limit=%d", categoryPageSize),
				fmt.Sprintf("--offset=%d", (page-1)*categoryPageSize), "--", key)
		}
		rs, header, err := remoteListing(ctx, a, args)
		if err != nil {
			return results.ResultSet{}, "", err
		}
		if key != "" {
			if len(rs.Items) == 0 && page > 1 {
				return results.ResultSet{}, "", errors.New(a.Text().NoMoreItems)
			}
			rs.Page = page
			if page > 1 {
				header += fmt.Sprintf(a.Text().PageSuffixShort, page)
			}
		}
		_ = saveListing(a, &rs)
		return rs, header, nil
	}
}

// RunForClient runs one command line of a client of jw serve against the
// server's service and returns what the command did. Only the commands a
// client sends are run (see wireRemote); jw serve itself and the commands that
// act on the reader's machine are refused.
func RunForClient(ctx context.Context, svc *service.Service, req remote.Request) remote.Response {
	c := app.NewCapture(&req)
	a := app.NewCaptured(app.Flags{}, svc, c)
	root := NewRootCmd(a)
	root.SetArgs(req.Args)
	root.SetIn(strings.NewReader(""))
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	resp := remote.Response{Version: version.String()}
	cmd, _, err := root.Find(req.Args)
	if err == nil && (cmd.Annotations[remoteAnnotation] != runsThere) {
		err = fmt.Errorf("%q is not a command jw serve runs for a client", strings.Join(strings.Fields(cmd.CommandPath())[1:], " "))
	}
	if err == nil {
		err = root.ExecuteContext(ctx)
	}
	resp.Events = c.Events()
	if lng, ok := a.ResolvedLanguage(); ok {
		resp.Language = &lng
	}
	var ce *remote.ConfirmError
	switch {
	case errors.As(err, &ce):
		resp.Error = &remote.Error{Code: remote.CodeConfirm, Message: ce.Error(), Confirm: &ce.Confirm}
	case err != nil:
		resp.Error = &remote.Error{Code: remote.CodeFailed, Message: err.Error()}
	}
	return resp
}
