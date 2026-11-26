package cli

import (
	"bytes"
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
