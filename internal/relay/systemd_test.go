package relay

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The machine's service runs as a user made up for it, gets the credential
// from systemd, and is locked down; a deleted proxy is not restarted.
func TestSystemUnit(t *testing.T) {
	u := SystemInstall().UnitFile()
	for _, want := range []string{
		"ExecStart=/usr/local/bin/jw proxy run\n",
		"LoadCredential=proxy:/etc/jw/proxy.json\n",
		"DynamicUser=yes\n", "ProtectSystem=strict\n", "ProtectHome=yes\n", "NoNewPrivileges=yes\n", "CapabilityBoundingSet=\n",
		"Restart=on-failure\n", "RestartPreventExitStatus=3\n", "WantedBy=multi-user.target\n",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("system unit without %q:\n%s", want, u)
		}
	}
	user := Install{Binary: "/home/a b/jw", Config: "/home/a b/.config/jw/proxy.json"}.UnitFile()
	for _, want := range []string{`ExecStart="/home/a b/jw" proxy run --config "/home/a b/.config/jw/proxy.json"`, "WantedBy=default.target"} {
		if !strings.Contains(user, want) {
			t.Errorf("user unit without %q:\n%s", want, user)
		}
	}
	if strings.Contains(user, "DynamicUser") || strings.Contains(user, "LoadCredential") {
		t.Errorf("user unit with the machine's settings:\n%s", user)
	}
}

// jw proxy run finds the credential systemd hands it before any other.
func TestConfigPath(t *testing.T) {
	creds, home := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("CREDENTIALS_DIRECTORY", creds)
	if _, err := ConfigPath(); err == nil && !exists(systemConfig) {
		t.Error("a path without any file")
	}
	user := filepath.Join(home, "jw", "proxy.json")
	_ = os.MkdirAll(filepath.Dir(user), 0o700)
	_ = os.WriteFile(user, []byte("{}"), 0o600)
	if p, _ := ConfigPath(); p != user {
		t.Errorf("user's file: %s", p)
	}
	_ = os.WriteFile(filepath.Join(creds, "proxy"), []byte("{}"), 0o600)
	if p, _ := ConfigPath(); p != filepath.Join(creds, "proxy") {
		t.Errorf("systemd's credential: %s", p)
	}
}

// Apply writes the file and the unit and has systemd start the service;
// Remove takes both away again.
func TestUserInstallApply(t *testing.T) {
	dir := t.TempDir()
	var calls []string
	old := Systemctl
	Systemctl = func(args ...string) error {
		calls = append(calls, strings.Join(args, " "))
		return nil
	}
	defer func() { Systemctl = old }()
	in := Install{Binary: "/usr/bin/jw", Config: filepath.Join(dir, "cfg", "proxy.json"), Unit: filepath.Join(dir, "units", ServiceName)}
	if err := in.Prepare(); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Server: "https://jw.example.org", ID: "ab", Name: "pi", Credential: "jwp_ab_" + strings.Repeat("x", 43)}
	if _, err := in.Apply(cfg); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadConfig(in.Config); err != nil || got.Credential != cfg.Credential {
		t.Fatalf("config: %v %+v", err, got)
	}
	if st, _ := os.Stat(in.Config); st.Mode().Perm() != 0o600 {
		t.Errorf("config is %v", st.Mode().Perm())
	}
	want := "--user daemon-reload|--user enable jw-proxy.service|--user restart jw-proxy.service"
	if got := strings.Join(calls, "|"); got != want {
		t.Errorf("systemctl: %s", got)
	}
	if err := in.Remove(); err != nil || exists(in.Config) || exists(in.Unit) {
		t.Errorf("remove: %v", err)
	}
}

// A service that cannot be stopped is not reported removed, and its files
// stay; with no service there is nothing for systemctl to do.
func TestRemoveWhenStopFails(t *testing.T) {
	dir := t.TempDir()
	in := Install{Binary: "/usr/bin/jw", Config: filepath.Join(dir, "proxy.json"), Unit: filepath.Join(dir, ServiceName)}
	_ = os.WriteFile(in.Config, []byte("{}"), 0o600)
	_ = os.WriteFile(in.Unit, []byte("[Unit]"), 0o644)
	var calls []string
	old := Systemctl
	defer func() { Systemctl = old }()
	Systemctl = func(args ...string) error {
		calls = append(calls, strings.Join(args, " "))
		return errors.New("Failed to stop jw-proxy.service: Access denied")
	}
	if err := in.Remove(); err == nil || !strings.Contains(err.Error(), "could not be stopped") {
		t.Errorf("remove: %v", err)
	}
	if !exists(in.Config) || !exists(in.Unit) {
		t.Error("files removed though the service still runs")
	}

	calls = nil
	_ = os.Remove(in.Unit)
	Systemctl = func(args ...string) error { calls = append(calls, strings.Join(args, " ")); return nil }
	if err := in.Remove(); err != nil || len(calls) != 0 || exists(in.Config) {
		t.Errorf("no service: %v, systemctl %v", err, calls)
	}
}
