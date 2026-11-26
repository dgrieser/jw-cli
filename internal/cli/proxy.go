package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/dgrieser/jw-cli/internal/app"
	"github.com/dgrieser/jw-cli/internal/relay"
	"github.com/dgrieser/jw-cli/internal/version"
)

func newProxyCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "proxy",
		Short: "Fetch jw.org for a jw serve that cannot (bot protection, a blocked network)",
		Long: `Run this machine as a proxy of one jw serve: when the server cannot read
jw.org itself — a bot protection's challenge, a refusal, a network that
fails — it hands the request to its proxies, and one of them fetches it and
sends the answer back. The server always asks itself first.

A proxy only ever connects out to its server (a long poll over HTTPS) and
takes no connections, so it runs behind a home router as well. It fetches
nothing but GET and HEAD of jw.org's own sites.

Set one up with the command the server's admin page (Proxies) shows:

  jw proxy setup https://jw.example.org jwpe_…

That installs a locked-down systemd service for the machine
(jw-proxy.service, the binary under /usr/local/bin, the credential in
/etc/jw/proxy.json), asking for sudo when not run as root. --user installs a
service of your own instead; --no-service only registers, for running
jw proxy run under something else.`,
	}
	cmd.AddCommand(newProxySetupCmd(a), newProxyRunCmd(a), newProxyStatusCmd(a), newProxyUninstallCmd(a))
	// the proxy is this machine's, never a server's (jw --server): a client
	// of a server may not set one up, run or remove it there
	for _, sub := range cmd.Commands() {
		runHere(sub)
	}
	return cmd
}

func newProxySetupCmd(a *app.App) *cobra.Command {
	var (
		user, noService bool
		workers         int
	)
	cmd := &cobra.Command{
		Use:   "setup <server-url> <enrollment-token>",
		Short: "Register with a jw serve and install the proxy as a service",
		Long: `Register this machine as a proxy of a jw serve, with the one-time
enrollment token an admin made on the server's Proxies page (valid for an
hour), and install and start it as a systemd service for the whole machine:
/usr/local/bin/jw, /etc/jw/proxy.json (root only),
/etc/systemd/system/jw-proxy.service, running as a user systemd makes up for
it. That takes root: run by another user, setup says what it will do and
runs itself again with sudo, which asks for the password; without sudo it
stops before the token is used, to be run as root.

  --user        a service for this user instead: ~/.config/jw/proxy.json and
                a user service; it runs while the user is logged in, or
                always after 'sudo loginctl enable-linger <user>'
  --no-service  register and write the file only; run 'jw proxy run' then
                however you like (so does a machine without systemd)

A token of "-" is read from standard input. Running setup again with a new
token replaces the proxy's registration.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			server, err := relay.CheckServerURL(args[0])
			if err != nil {
				return err
			}
			token := args[1]
			if token == "-" {
				line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				if err != nil && line == "" {
					return fmt.Errorf("no token on standard input: %w", err)
				}
				token = strings.TrimSpace(line)
			}
			service := !noService && hasSystemd()
			var in relay.Install
			switch {
			case !service:
				path, err := relay.UserConfigPath()
				if err != nil {
					return err
				}
				in = relay.Install{Config: path}
			case geteuid() == 0 && !user:
				in = relay.SystemInstall()
			case !user:
				// the machine's service takes root: this setup again, with sudo
				return setupWithSudo(cmd, server, token, workers)
			default:
				if in, err = relay.UserInstall(); err != nil {
					return err
				}
			}
			if service {
				if err := in.Prepare(); err != nil {
					return fmt.Errorf("%w (run as root for a system service, or with --user)", err)
				}
			}
			cfg, err := relay.Register(cmd.Context(), server, token, version.String())
			if err != nil {
				return err
			}
			cfg.Workers = workers
			fmt.Fprintf(out, "Registered with %s as %q.\n", cfg.Server, cfg.Name)
			if !service {
				if err := cfg.Save(in.Config); err != nil {
					return err
				}
				fmt.Fprintf(out, "Credential written to %s (readable by you alone).\n", in.Config)
				if !noService {
					fmt.Fprintln(out, "No systemd here: start the proxy yourself and keep it running:")
				} else {
					fmt.Fprintln(out, "Start the proxy and keep it running:")
				}
				fmt.Fprintf(out, "\n  jw proxy run --config %s\n", in.Config)
				return nil
			}
			copied, err := in.Apply(cfg)
			if err != nil {
				return fmt.Errorf("registered, but the service could not be installed: %w", err)
			}
			if copied {
				fmt.Fprintf(out, "Binary installed at %s.\n", in.Binary)
			}
			fmt.Fprintf(out, "Credential written to %s, service %s started.\n", in.Config, in.Unit)
			printServiceHelp(out, in.System)
			return nil
		},
	}
	fl := cmd.Flags()
	fl.BoolVar(&user, "user", false, "install a user service even as root")
	fl.BoolVar(&noService, "no-service", false, "only register and write the credential, install no service")
	fl.IntVar(&workers, "workers", 0, fmt.Sprintf("requests made at once (default %d)", relay.DefaultWorkers))
	return cmd
}

// The machine's state, as setup asks it; variables for the tests.
var (
	hasSystemd = relay.HasSystemd
	geteuid    = os.Geteuid
	lookSudo   = func() (string, error) { return exec.LookPath("sudo") }
	// runSudo runs the setup as root: sudo asks on the terminal for the
	// password, the token goes in on standard input so it is never on a
	// command line other users can see
	runSudo = func(sudo string, args []string, stdin io.Reader, out, errOut io.Writer) error {
		c := exec.Command(sudo, args...)
		c.Stdin, c.Stdout, c.Stderr = stdin, out, errOut
		return c.Run()
	}
)

// setupWithSudo installs the machine's service from a user who is not root:
// it says what root will do, and runs this setup again with sudo. Without
// sudo it stops before the token is used.
func setupWithSudo(cmd *cobra.Command, server, token string, workers int) error {
	out := cmd.OutOrStdout()
	in := relay.SystemInstall()
	sudo, err := lookSudo()
	if err != nil {
		return errors.New("the proxy's service for the whole machine is installed as root, and sudo is not available here: " +
			"run this command again as root, or add --user for a service of your own (the token is not used yet)")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	fmt.Fprintf(out, `Installing the proxy as a service for the whole machine takes root.
With sudo, this setup runs again as root and will:
  - register with %s
  - install %s as %s
  - write the credential to %s (readable by root alone)
  - install %s and start it, as a user systemd makes up for it
sudo may ask for your password.

`, server, exe, in.Binary, in.Config, in.Unit)
	args := []string{"--", exe, "proxy", "setup", server, "-"}
	if workers > 0 {
		args = append(args, "--workers", strconv.Itoa(workers))
	}
	if err := runSudo(sudo, args, strings.NewReader(token+"\n"), out, cmd.ErrOrStderr()); err != nil {
		return fmt.Errorf("the setup as root did not finish (%v); see above. Run it as root instead, or add --user for a service of your own", err)
	}
	return nil
}

func printServiceHelp(out io.Writer, system bool) {
	ctl, journal := "systemctl", "journalctl -u jw-proxy -f"
	if !system {
		ctl, journal = "systemctl --user", "journalctl --user -u jw-proxy -f"
	}
	fmt.Fprintf(out, `
  %[1]s status jw-proxy     is it running
  %[2]s      what it does
  %[1]s restart jw-proxy    after replacing the binary
  jw proxy status                how the server sees it
  jw proxy uninstall             stop and remove it
`, ctl, journal)
	if !system {
		fmt.Fprintln(out, "\nA user service stops when you log out; to keep it running: sudo loginctl enable-linger $USER")
	}
}

func newProxyRunCmd(a *app.App) *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the proxy in the foreground (what the service runs)",
		Long: `Fetch for the server until stopped (SIGINT, SIGTERM). The credential is
read from --config, else the file systemd hands the service, else
~/.config/jw/proxy.json, else /etc/jw/proxy.json. Logs go to stderr.

Exits with status 3 when the server refused the credential (the proxy was
deleted there): the service is not restarted then.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := configPath
			if path == "" {
				var err error
				if path, err = relay.ConfigPath(); err != nil {
					return err
				}
			}
			cfg, err := relay.LoadConfig(path)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			logf := func(format string, args ...any) {
				fmt.Fprintf(os.Stderr, format+"\n", args...)
			}
			err = relay.NewAgent(cfg, version.String(), logf).Run(ctx)
			if errors.Is(err, relay.ErrRevoked) {
				return &exitCodeError{code: relay.ExitRevoked, err: err}
			}
			return err
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "the proxy's file (from jw proxy setup)")
	return cmd
}

func newProxyStatusCmd(a *app.App) *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the proxy's server and how the server sees it",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			path := configPath
			if path == "" {
				var err error
				if path, err = relay.ConfigPath(); err != nil {
					return err
				}
			}
			cfg, err := relay.LoadConfig(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "Proxy %q (%s) of %s\nCredential: %s\n", cfg.Name, cfg.ID, cfg.Server, path)
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()
			self, err := relay.NewAgent(cfg, version.String(), nil).Self(ctx)
			switch {
			case err != nil:
				fmt.Fprintf(out, "Server: %v\n", err)
			case self.Enabled:
				fmt.Fprintln(out, "Server: registered, enabled")
			default:
				fmt.Fprintln(out, "Server: registered, disabled by an admin")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "the proxy's file (from jw proxy setup)")
	return cmd
}

func newProxyUninstallCmd(a *app.App) *cobra.Command {
	var user bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Stop and remove the proxy's service and credential",
		Long: `Stop the proxy's service, and remove it and the credential: the system
service as root, else (or with --user) the user's. The proxy stays listed on
the server until an admin deletes it there.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			in := relay.SystemInstall()
			if os.Geteuid() != 0 || user {
				var err error
				if in, err = relay.UserInstall(); err != nil {
					return err
				}
			}
			if err := in.Remove(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed %s and %s. Delete the proxy on the server's Proxies page too.\n", in.Unit, in.Config)
			return nil
		},
	}
	cmd.Flags().BoolVar(&user, "user", false, "remove the user service even as root")
	return cmd
}
