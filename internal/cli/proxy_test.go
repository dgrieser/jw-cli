package cli

import (
	"bytes"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/relay"
)

// jw proxy setup registers with the server and keeps the credential for
// this user; jw proxy status shows how the server sees the proxy.
func TestProxySetupNoService(t *testing.T) {
	pool, err := relay.OpenPool(filepath.Join(t.TempDir(), "proxies.json"), "v", nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(pool.Handler())
	defer srv.Close()
	token, _, err := pool.Enroll("laptop", "admin@example.org", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("CREDENTIALS_DIRECTORY", "")

	run := func(args ...string) (string, error) {
		root := NewRootCmd(app.New(app.Flags{}))
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(args)
		err := root.Execute()
		return out.String(), err
	}
	out, err := run("proxy", "setup", srv.URL, token, "--no-service", "--workers", "2")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	path := filepath.Join(home, "jw", "proxy.json")
	if !strings.Contains(out, `Registered with `+srv.URL+` as "laptop"`) || !strings.Contains(out, "jw proxy run --config "+path) {
		t.Errorf("setup said:\n%s", out)
	}
	cfg, err := relay.LoadConfig(path)
	if err != nil || cfg.Workers != 2 {
		t.Fatalf("config: %v %+v", err, cfg)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Errorf("credential file is %v", st.Mode().Perm())
	}
	out, err = run("proxy", "status")
	if err != nil || !strings.Contains(out, `Proxy "laptop"`) || !strings.Contains(out, "registered, enabled") {
		t.Errorf("status: %v\n%s", err, out)
	}
	// the token was used up
	if out, err := run("proxy", "setup", srv.URL, token, "--no-service"); err == nil {
		t.Errorf("a used token registered again:\n%s", out)
	}
	// a server not over https is refused before anything is sent
	if _, err := run("proxy", "setup", "http://jw.example.org", token, "--no-service"); err == nil || !strings.Contains(err.Error(), "https") {
		t.Errorf("http server: %v", err)
	}
}

// stubMachine has setup see systemd, a user that is not root, and sudo or
// none; it returns what sudo was asked to run, with what on stdin.
func stubMachine(t *testing.T, sudo bool) *[]string {
	t.Helper()
	oldS, oldE, oldL, oldR := hasSystemd, geteuid, lookSudo, runSudo
	t.Cleanup(func() { hasSystemd, geteuid, lookSudo, runSudo = oldS, oldE, oldL, oldR })
	hasSystemd = func() bool { return true }
	geteuid = func() int { return 1000 }
	lookSudo = func() (string, error) {
		if !sudo {
			return "", errors.New("not found")
		}
		return "/usr/bin/sudo", nil
	}
	var ran []string
	runSudo = func(sudo string, args []string, stdin io.Reader, out, errOut io.Writer) error {
		in, _ := io.ReadAll(stdin)
		ran = append(append([]string{sudo}, args...), "stdin="+string(in))
		return nil
	}
	return &ran
}

func runJW(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	root := NewRootCmd(app.New(app.Flags{}))
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

// Not root, with sudo: setup says what root will do and runs itself again
// with sudo, the token on stdin rather than on the command line; it does
// not register itself.
func TestProxySetupWithSudo(t *testing.T) {
	ran := stubMachine(t, true)
	out, err := runJW(t, "", "proxy", "setup", "https://jw.example.org", "jwpe_secret-token", "--workers", "3")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"takes root", "register with https://jw.example.org", "/usr/local/bin/jw", "/etc/jw/proxy.json", "/etc/systemd/system/jw-proxy.service", "sudo may ask for your password"} {
		if !strings.Contains(out, want) {
			t.Errorf("plan without %q:\n%s", want, out)
		}
	}
	got := strings.Join(*ran, " ")
	if !strings.HasPrefix(got, "/usr/bin/sudo -- ") || !strings.Contains(got, " proxy setup https://jw.example.org - --workers 3 stdin=jwpe_secret-token\n") {
		t.Errorf("sudo ran %q", got)
	}
	if strings.Count(got, "jwpe_secret-token") != 1 {
		t.Errorf("the token is on sudo's command line: %q", got)
	}
}

// Not root and no sudo: setup stops before the token is used, and says to
// run it as root or with --user.
func TestProxySetupWithoutSudo(t *testing.T) {
	stubMachine(t, false)
	pool, err := relay.OpenPool(filepath.Join(t.TempDir(), "proxies.json"), "v", nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(pool.Handler())
	defer srv.Close()
	token, _, _ := pool.Enroll("pi", "admin@example.org", time.Now())
	_, err = runJW(t, "", "proxy", "setup", srv.URL, token)
	if err == nil || !strings.Contains(err.Error(), "as root") || !strings.Contains(err.Error(), "--user") {
		t.Errorf("want to be told to run as root: %v", err)
	}
	if len(pool.Enrollments(time.Now())) != 1 || len(pool.Proxies(time.Now())) != 0 {
		t.Error("the token was used though nothing was installed")
	}
}

// A token of "-" is read from standard input, as sudo's setup gets it.
func TestProxySetupTokenOnStdin(t *testing.T) {
	pool, err := relay.OpenPool(filepath.Join(t.TempDir(), "proxies.json"), "v", nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(pool.Handler())
	defer srv.Close()
	token, _, _ := pool.Enroll("pi", "admin@example.org", time.Now())
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("HOME", home)
	if out, err := runJW(t, token+"\n", "proxy", "setup", srv.URL, "-", "--no-service"); err != nil || !strings.Contains(out, `as "pi"`) {
		t.Fatalf("%v\n%s", err, out)
	}
}

// stubSystemInstall points the machine's install at dir, with its files
// there or not.
func stubSystemInstall(t *testing.T, installed bool) relay.Install {
	t.Helper()
	dir := t.TempDir()
	in := relay.Install{System: true, Binary: filepath.Join(dir, "jw"), Config: filepath.Join(dir, "proxy.json"), Unit: filepath.Join(dir, relay.ServiceName)}
	if installed {
		_ = os.WriteFile(in.Unit, []byte("[Unit]"), 0o644)
		_ = os.WriteFile(in.Config, []byte("{}"), 0o600)
	}
	old := systemInstall
	t.Cleanup(func() { systemInstall = old })
	systemInstall = func() relay.Install { return in }
	return in
}

// The machine's service, which setup installs with sudo, is what uninstall
// removes — with sudo too, from a user who is not root.
func TestProxyUninstallSystemWithSudo(t *testing.T) {
	ran := stubMachine(t, true)
	stubSystemInstall(t, true)
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("HOME", home)
	out, err := runJW(t, "", "proxy", "uninstall")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "removing it takes root") || !strings.HasSuffix(strings.TrimSuffix(strings.Join(*ran, " "), " stdin="), " proxy uninstall") {
		t.Errorf("sudo ran %q\n%s", strings.Join(*ran, " "), out)
	}

	stubMachine(t, false)
	if _, err := runJW(t, "", "proxy", "uninstall"); err == nil || !strings.Contains(err.Error(), "as root") {
		t.Errorf("no sudo: %v", err)
	}
}

// With nothing installed, uninstall says so rather than that it removed it.
func TestProxyUninstallNothing(t *testing.T) {
	stubMachine(t, true)
	stubSystemInstall(t, false)
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("HOME", home)
	if out, err := runJW(t, "", "proxy", "uninstall"); err == nil || !strings.Contains(err.Error(), "no proxy is installed here") {
		t.Errorf("nothing installed: %v\n%s", err, out)
	}
}
