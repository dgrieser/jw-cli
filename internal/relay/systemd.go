package relay

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// A proxy runs as a systemd service, which jw proxy setup installs: for the
// whole machine when run as root — the binary under /usr/local/bin, the
// credential under /etc/jw readable by root alone and handed to the service
// by systemd, the service itself running as a user of its own made up for
// it, locked down — or for the user who runs it otherwise.

const (
	// ServiceName is the proxy's service.
	ServiceName = "jw-proxy.service"
	// ExitRevoked is the status jw proxy run ends with when its server
	// refused the credential; the service is not restarted after it.
	ExitRevoked = 3

	systemBinary = "/usr/local/bin/jw"
	systemConfig = "/etc/jw/proxy.json"
	systemUnit   = "/etc/systemd/system/" + ServiceName
	// credentialName is the name systemd hands the credential under.
	credentialName = "proxy"
)

// Install is where a proxy's service and its files go.
type Install struct {
	// System installs for the whole machine (as root); else for the user.
	System bool
	// Binary is the jw the service runs; Config the proxy's file; Unit the
	// service's file.
	Binary, Config, Unit string
}

// SystemInstall is the install for the whole machine.
func SystemInstall() Install {
	return Install{System: true, Binary: systemBinary, Config: systemConfig, Unit: systemUnit}
}

// UserInstall is the install for the user who runs it, with the binary
// that runs now.
func UserInstall() (Install, error) {
	exe, err := executable()
	if err != nil {
		return Install{}, err
	}
	cfg, err := UserConfigPath()
	if err != nil {
		return Install{}, err
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return Install{}, err
	}
	return Install{Binary: exe, Config: cfg, Unit: filepath.Join(dir, "systemd", "user", ServiceName)}, nil
}

// UserConfigPath is where a proxy run by hand keeps its file.
func UserConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("no folder for the proxy's file: %w", err)
	}
	return filepath.Join(dir, "jw", "proxy.json"), nil
}

// ConfigPath is the proxy's file for jw proxy run: the one systemd hands
// the service, else the user's, else the machine's.
func ConfigPath() (string, error) {
	if dir := os.Getenv("CREDENTIALS_DIRECTORY"); dir != "" {
		if p := filepath.Join(dir, credentialName); exists(p) {
			return p, nil
		}
	}
	if p, err := UserConfigPath(); err == nil && exists(p) {
		return p, nil
	}
	if exists(systemConfig) {
		return systemConfig, nil
	}
	return "", errors.New("no proxy set up here: run jw proxy setup with the command the server's proxies page shows")
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// UnitFile is the service's file.
func (in Install) UnitFile() string {
	var b strings.Builder
	fmt.Fprintf(&b, `[Unit]
Description=jw proxy: fetches jw.org for its jw serve when the server cannot
Documentation=https://github.com/dgrieser/jw-cli#proxies-jw-proxy
Wants=network-online.target
After=network-online.target

[Service]
`)
	if in.System {
		fmt.Fprintf(&b, `ExecStart=%s proxy run
# the credential, readable by root alone, handed to a user made up for the
# service; nothing else of the machine is open to it
LoadCredential=%s:%s
DynamicUser=yes
CacheDirectory=jw-proxy
Environment=XDG_CACHE_HOME=%%C/jw-proxy XDG_CONFIG_HOME=%%C/jw-proxy
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectKernelLogs=yes
ProtectControlGroups=yes
ProtectClock=yes
ProtectHostname=yes
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
RestrictNamespaces=yes
RestrictRealtime=yes
RestrictSUIDSGID=yes
LockPersonality=yes
MemoryDenyWriteExecute=yes
SystemCallArchitectures=native
SystemCallFilter=@system-service
CapabilityBoundingSet=
NoNewPrivileges=yes
`, quote(in.Binary), credentialName, in.Config)
	} else {
		fmt.Fprintf(&b, `ExecStart=%s proxy run --config %s
NoNewPrivileges=yes
PrivateTmp=yes
`, quote(in.Binary), quote(in.Config))
	}
	fmt.Fprintf(&b, `Restart=on-failure
RestartSec=30
# %d: the server deleted the proxy; restarting changes nothing
RestartPreventExitStatus=%d

[Install]
WantedBy=%s
`, ExitRevoked, ExitRevoked, map[bool]string{true: "multi-user.target", false: "default.target"}[in.System])
	return b.String()
}

// quote quotes a path for a unit file when it holds a space.
func quote(p string) string {
	if strings.ContainsAny(p, " \t\"") {
		return `"` + strings.ReplaceAll(p, `"`, `\"`) + `"`
	}
	return p
}

// Systemctl runs systemctl; a variable for the tests.
var Systemctl = func(args ...string) error {
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// HasSystemd reports whether systemd runs the machine.
func HasSystemd() bool {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	return exists("/run/systemd/system")
}

func (in Install) systemctl(args ...string) error {
	if !in.System {
		args = append([]string{"--user"}, args...)
	}
	return Systemctl(args...)
}

// Prepare checks that the install can be made, before the proxy registers:
// an enrollment is used up by registering.
func (in Install) Prepare() error {
	for _, dir := range []string{filepath.Dir(in.Config), filepath.Dir(in.Unit)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("cannot write to %s: %w", dir, err)
		}
	}
	if in.System {
		if err := os.MkdirAll(filepath.Dir(in.Binary), 0o755); err != nil {
			return fmt.Errorf("cannot write to %s: %w", filepath.Dir(in.Binary), err)
		}
	}
	return nil
}

// Apply writes the proxy's file and its service, puts the binary in place
// for a system install, and starts the service (again).
func (in Install) Apply(cfg Config) (copied bool, err error) {
	if in.System {
		if copied, err = placeBinary(in.Binary); err != nil {
			return false, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(in.Config), 0o700); err != nil {
		return copied, err
	}
	if err := cfg.Save(in.Config); err != nil {
		return copied, err
	}
	if err := os.WriteFile(in.Unit, []byte(in.UnitFile()), 0o644); err != nil {
		return copied, err
	}
	if err := in.systemctl("daemon-reload"); err != nil {
		return copied, err
	}
	if err := in.systemctl("enable", ServiceName); err != nil {
		return copied, err
	}
	return copied, in.systemctl("restart", ServiceName)
}

// Installed reports whether anything of the install is there: its service
// or its file.
func (in Install) Installed() bool { return exists(in.Unit) || exists(in.Config) }

// Remove stops the service and removes it and the proxy's file. A service
// that cannot be stopped is left as it is, files and all: a proxy still
// running with its credential is not one to report removed.
func (in Install) Remove() error {
	installed := exists(in.Unit)
	if installed {
		if err := in.systemctl("disable", "--now", ServiceName); err != nil {
			return fmt.Errorf("the service could not be stopped, nothing was removed: %w", err)
		}
	}
	var errs []error
	for _, p := range []string{in.Unit, in.Config} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	if installed {
		if err := in.systemctl("daemon-reload"); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// placeBinary copies the running jw to dst, unless it is that already.
func placeBinary(dst string) (bool, error) {
	exe, err := executable()
	if err != nil {
		return false, err
	}
	if same, _ := filepath.EvalSymlinks(dst); same == exe {
		return false, nil
	}
	src, err := os.Open(exe)
	if err != nil {
		return false, err
	}
	defer src.Close()
	tmp := dst + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return false, err
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return false, err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	return true, os.Rename(tmp, dst)
}
