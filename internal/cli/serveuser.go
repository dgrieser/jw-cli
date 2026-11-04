package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/dgrieser/jw-cli/internal/accounts"
)

// serverDataDir is the folder of the accounts and settings of jw serve: dir,
// or <config dir>/jw/server.
func serverDataDir(dir string) (string, error) {
	if dir != "" {
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("no folder for the accounts (set --data-dir or $JW_DATA_DIR): %w", err)
	}
	return filepath.Join(base, "jw", "server"), nil
}

// openAccounts opens the accounts and the settings of a data folder.
func openAccounts(dir string) (*accounts.Store, *accounts.SettingsFile, error) {
	dir, err := serverDataDir(dir)
	if err != nil {
		return nil, nil, err
	}
	usersPath, settingsPath := accounts.Paths(dir)
	users, err := accounts.Open(usersPath)
	if err != nil {
		return nil, nil, fmt.Errorf("accounts: %w", err)
	}
	settings, err := accounts.LoadSettings(settingsPath)
	if err != nil {
		return nil, nil, fmt.Errorf("settings: %w", err)
	}
	return users, settings, nil
}

// newServeUserCmd manages the accounts of jw serve from the command line:
// the way in for a first admin without the setup page, and back in when the
// only admin forgot the password.
func newServeUserCmd(dataDir *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Manage the accounts of jw serve",
		Long: `Manage the accounts of jw serve in its data folder (--data-dir). A running
server picks the changes up within a few seconds.

Passwords are read from the terminal, or without one from the first line of
standard input.`,
		Args: cobra.NoArgs,
	}
	open := func() (*accounts.Store, error) {
		users, _, err := openAccounts(*dataDir)
		return users, err
	}
	done := func(c *cobra.Command, err error, msg string) error {
		if err == nil {
			fmt.Fprintln(c.OutOrStdout(), msg)
		}
		return err
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List the accounts",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			st, err := open()
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(c.OutOrStdout(), 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "EMAIL\tROLE\tSTATUS\tCREATED\tLAST SIGN-IN")
			for _, u := range st.List() {
				status := "active"
				switch {
				case u.Pending:
					status = "waiting for approval"
				case u.Disabled:
					status = "disabled"
				case u.Invited:
					status = "invited"
				}
				last := "-"
				if !u.LastLogin.IsZero() {
					last = u.LastLogin.Local().Format("2006-01-02 15:04")
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", u.Email, u.Role, status, u.Created.Local().Format("2006-01-02"), last)
			}
			_ = tw.Flush()
			fmt.Fprintf(c.ErrOrStderr(), "(%s)\n", st.Path())
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "add EMAIL [admin|user]",
		Short: "Add an account (asks for the password)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(c *cobra.Command, args []string) error {
			role := accounts.RoleUser
			if len(args) > 1 {
				if args[1] != accounts.RoleAdmin && args[1] != accounts.RoleUser {
					return errors.New("role: admin or user")
				}
				role = args[1]
			}
			st, err := open()
			if err != nil {
				return err
			}
			pw, err := readNewPassword(c)
			if err != nil {
				return err
			}
			return done(c, st.Create(args[0], pw, role, false, ""), fmt.Sprintf("Added %s (%s).", args[0], role))
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "passwd EMAIL",
		Short: "Set the password of an account (ends its sessions)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			st, err := open()
			if err != nil {
				return err
			}
			if _, ok := st.Get(args[0]); !ok {
				return accounts.ErrNotFound
			}
			pw, err := readNewPassword(c)
			if err != nil {
				return err
			}
			return done(c, st.Apply(args[0], accounts.Update{Password: &pw}), "Password set; the account's sessions are signed out.")
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "approve EMAIL",
		Short: "Approve an account waiting for an admin",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			st, err := open()
			if err != nil {
				return err
			}
			return done(c, st.Apply(args[0], accounts.Update{Approve: true}), "Approved.")
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "role EMAIL admin|user",
		Short: "Change the role of an account",
		Args:  cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			if args[1] != accounts.RoleAdmin && args[1] != accounts.RoleUser {
				return errors.New("role: admin or user")
			}
			st, err := open()
			if err != nil {
				return err
			}
			return done(c, st.Apply(args[0], accounts.Update{Role: &args[1]}), "Role changed.")
		},
	})
	for _, disable := range []bool{true, false} {
		use, short, msg := "enable EMAIL", "Enable a disabled account", "Enabled."
		if disable {
			use, short, msg = "disable EMAIL", "Disable an account (ends its sessions)", "Disabled."
		}
		cmd.AddCommand(&cobra.Command{
			Use:   use,
			Short: short,
			Args:  cobra.ExactArgs(1),
			RunE: func(c *cobra.Command, args []string) error {
				st, err := open()
				if err != nil {
					return err
				}
				return done(c, st.Apply(args[0], accounts.Update{Disabled: &disable}), msg)
			},
		})
	}
	cmd.AddCommand(&cobra.Command{
		Use:     "delete EMAIL",
		Aliases: []string{"del", "rm"},
		Short:   "Delete an account",
		Args:    cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			st, err := open()
			if err != nil {
				return err
			}
			return done(c, st.Delete(args[0]), "Deleted.")
		},
	})
	cmd.AddCommand(newServeKeyCmd(open))
	// the accounts are this machine's, never a server's (jw --server)
	var here func(c *cobra.Command)
	here = func(c *cobra.Command) {
		runHere(c)
		for _, sub := range c.Commands() {
			here(sub)
		}
	}
	here(cmd)
	return cmd
}

// newServeKeyCmd manages the API keys of the accounts.
func newServeKeyCmd(open func() (*accounts.Store, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "key",
		Short: "Manage the API keys of an account",
		Long: `Manage the API keys of an account: the keys programs use the JSON API with,
jw --server among them ($JW_SERVER_KEY). A key is named by its ID or its name.

A new key's secret is printed once, alone on standard output; it is not kept
and cannot be shown again. An expiry is never, a number of days, weeks or
years (30d, 12w, 1y), a duration (36h) or a date (2027-01-31).`,
		Args: cobra.NoArgs,
	}
	// find names a key by ID or name
	find := func(st *accounts.Store, email, ref string) (accounts.KeyInfo, error) {
		if _, ok := st.Get(email); !ok {
			return accounts.KeyInfo{}, accounts.ErrNotFound
		}
		keys := st.Keys(email)
		// an ID first: a key may be named like another's ID
		for _, k := range keys {
			if k.ID == ref {
				return k, nil
			}
		}
		for _, k := range keys {
			if strings.EqualFold(k.Name, ref) {
				return k, nil
			}
		}
		return accounts.KeyInfo{}, accounts.ErrKeyNotFound
	}
	expiry := func(v string) (time.Time, error) { return accounts.ParseExpiry(v, time.Now(), time.Local) }
	stamp := func(t time.Time, zero string) string {
		if t.IsZero() {
			return zero
		}
		return t.Local().Format("2006-01-02 15:04")
	}
	printSecret := func(c *cobra.Command, secret string, k accounts.KeyInfo) {
		fmt.Fprintln(c.OutOrStdout(), secret)
		fmt.Fprintf(c.ErrOrStderr(), "Key %s (%s) of %s, expires %s. Copy the secret now: it is not shown again.\n",
			k.Name, k.ID, k.Email, stamp(k.Expires, "never"))
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list EMAIL",
		Short: "List the API keys of an account",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			st, err := open()
			if err != nil {
				return err
			}
			if _, ok := st.Get(args[0]); !ok {
				return accounts.ErrNotFound
			}
			tw := tabwriter.NewWriter(c.OutOrStdout(), 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tNAME\tKEY\tCREATED\tEXPIRES\tLAST USED")
			for _, k := range st.Keys(args[0]) {
				exp := stamp(k.Expires, "never")
				if k.Expired() {
					exp += " (expired)"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", k.ID, k.Name, k.Hint, k.Created.Local().Format("2006-01-02"),
					exp, stamp(k.LastUsed, "-"))
			}
			return tw.Flush()
		},
	})

	var addExpires string
	add := &cobra.Command{
		Use:   "add EMAIL NAME",
		Short: "Make an API key and print its secret",
		Args:  cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			exp, err := expiry(addExpires)
			if err != nil {
				return err
			}
			st, err := open()
			if err != nil {
				return err
			}
			secret, k, err := st.CreateKey(args[0], args[1], exp)
			if err != nil {
				return err
			}
			printSecret(c, secret, k)
			return nil
		},
	}
	add.Flags().StringVar(&addExpires, "expires", "never", "when the key expires: never, 30d, 12w, 1y, 36h or a date")
	cmd.AddCommand(add)

	var rotExpires string
	var grace time.Duration
	rotate := &cobra.Command{
		Use:   "rotate EMAIL KEY",
		Short: "Give an API key a new secret and print it",
		Args:  cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			var exp *time.Time
			if c.Flags().Changed("expires") {
				t, err := expiry(rotExpires)
				if err != nil {
					return err
				}
				exp = &t
			}
			st, err := open()
			if err != nil {
				return err
			}
			k, err := find(st, args[0], args[1])
			if err != nil {
				return err
			}
			secret, k, err := st.RotateKey(args[0], k.ID, grace, exp)
			if err != nil {
				return err
			}
			printSecret(c, secret, k)
			if k.OldValid() {
				fmt.Fprintf(c.ErrOrStderr(), "The old secret works until %s.\n", stamp(k.OldUntil, ""))
			}
			return nil
		},
	}
	rotate.Flags().StringVar(&rotExpires, "expires", "", "a new expiry (default: as it is)")
	rotate.Flags().DurationVar(&grace, "grace", 0, "how long the old secret keeps working (such as 24h)")
	cmd.AddCommand(rotate)

	var newName, updExpires string
	update := &cobra.Command{
		Use:   "update EMAIL KEY",
		Short: "Rename an API key or change its expiry",
		Args:  cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			up := accounts.KeyUpdate{}
			if c.Flags().Changed("name") {
				up.Name = &newName
			}
			if c.Flags().Changed("expires") {
				t, err := expiry(updExpires)
				if err != nil {
					return err
				}
				up.Expires = &t
			}
			if up.Name == nil && up.Expires == nil {
				return errors.New("nothing to change: give --name or --expires")
			}
			st, err := open()
			if err != nil {
				return err
			}
			k, err := find(st, args[0], args[1])
			if err != nil {
				return err
			}
			k, err = st.UpdateKey(args[0], k.ID, up)
			if err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "Key %s (%s) expires %s.\n", k.Name, k.ID, stamp(k.Expires, "never"))
			return nil
		},
	}
	update.Flags().StringVar(&newName, "name", "", "the new name")
	update.Flags().StringVar(&updExpires, "expires", "", "the new expiry: never, 30d, 12w, 1y, 36h or a date")
	cmd.AddCommand(update)

	cmd.AddCommand(&cobra.Command{
		Use:     "delete EMAIL [KEY]",
		Aliases: []string{"del", "rm"},
		Short:   "Delete an API key, or without KEY all keys of the account",
		Args:    cobra.RangeArgs(1, 2),
		RunE: func(c *cobra.Command, args []string) error {
			st, err := open()
			if err != nil {
				return err
			}
			if len(args) == 1 {
				if _, ok := st.Get(args[0]); !ok {
					return accounts.ErrNotFound
				}
				n, err := st.DeleteKeys(args[0])
				if err != nil {
					return err
				}
				fmt.Fprintf(c.OutOrStdout(), "Deleted %d keys.\n", n)
				return nil
			}
			k, err := find(st, args[0], args[1])
			if err != nil {
				return err
			}
			if err := st.DeleteKey(args[0], k.ID); err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "Deleted key %s.\n", k.Name)
			return nil
		},
	})
	return cmd
}

// readNewPassword asks for a password twice on a terminal, or reads the
// first line of standard input.
func readNewPassword(c *cobra.Command) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		line, err := bufio.NewReader(c.InOrStdin()).ReadString('\n')
		if err != nil && line == "" {
			return "", errors.New("no password on standard input")
		}
		pw := strings.TrimRight(line, "\r\n")
		return pw, accounts.CheckPassword(pw)
	}
	errw := c.ErrOrStderr()
	fmt.Fprint(errw, "New password: ")
	a, err := term.ReadPassword(fd)
	fmt.Fprintln(errw)
	if err != nil {
		return "", err
	}
	fmt.Fprint(errw, "Repeat: ")
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(errw)
	if err != nil {
		return "", err
	}
	if string(a) != string(b) {
		return "", errors.New("the passwords differ")
	}
	return string(a), accounts.CheckPassword(string(a))
}
