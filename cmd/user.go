package cmd

// Portions adapted from MeetHub (MIT): cmd/user.go

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/jniltinho/streamgo/internal/core/account"
	"github.com/jniltinho/streamgo/internal/store"
)

func newUserCmd(c *cli) *cobra.Command {
	cmd := &cobra.Command{Use: "user", Short: "Manage accounts: add, list, passwd, disable, enable, admin", Args: cobra.NoArgs}
	var passwordStdin bool
	pwFlag := func(cc *cobra.Command) {
		cc.Flags().BoolVar(&passwordStdin, "password-stdin", false, "read the password from standard input instead of a prompt")
	}

	var email, name string
	var admin bool
	add := &cobra.Command{
		Use:   "add",
		Short: "Create an account (the first account of an install is an administrator)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return c.withAccounts(cmd.Context(), func(ctx context.Context, s *account.Service) error {
				pw, err := c.readPassword(passwordStdin)
				if err != nil {
					return err
				}
				u, err := s.Create(ctx, account.NewUser{Email: email, Name: name, Password: pw, Admin: admin})
				if err != nil {
					return err
				}
				if c.asJSON {
					return c.printJSON(userView(*u))
				}
				_, err = fmt.Fprintf(c.out, "created %s <%s> (id %s, %s)\n", u.Name, u.Email, u.ID, userView(*u).Role)
				return err
			})
		},
	}
	add.Flags().StringVar(&email, "email", "", "e-mail address (required)")
	add.Flags().StringVar(&name, "name", "", "name (required)")
	add.Flags().BoolVar(&admin, "admin", false, "make the account an administrator")
	_ = add.MarkFlagRequired("email")
	_ = add.MarkFlagRequired("name")
	pwFlag(add)

	list := &cobra.Command{
		Use:   "list",
		Short: "List accounts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return c.withAccounts(cmd.Context(), func(ctx context.Context, s *account.Service) error {
				users, err := s.List(ctx)
				if err != nil {
					return err
				}
				if c.asJSON {
					views := make([]userJSON, len(users))
					for i, u := range users {
						views[i] = userView(u)
					}
					return c.printJSON(views)
				}
				tw := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
				_, _ = fmt.Fprintln(tw, "ID\tE-MAIL\tNAME\tROLE\tSTATUS")
				for _, u := range users {
					v := userView(u)
					_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", u.ID, u.Email, u.Name, v.Role, v.Status)
				}
				return tw.Flush()
			})
		},
	}

	passwd := &cobra.Command{
		Use:   "passwd EMAIL",
		Short: "Set an account's password and end its sessions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.withAccounts(cmd.Context(), func(ctx context.Context, s *account.Service) error {
				pw, err := c.readPassword(passwordStdin)
				if err != nil {
					return err
				}
				if err := s.SetPassword(ctx, args[0], pw); err != nil {
					return err
				}
				_, err = fmt.Fprintf(c.out, "password changed for %s; their sessions were ended\n", args[0])
				return err
			})
		},
	}
	pwFlag(passwd)

	setDisabled := func(use, short, done string, disabled bool) *cobra.Command {
		return &cobra.Command{
			Use:   use + " EMAIL",
			Short: short,
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return c.withAccounts(cmd.Context(), func(ctx context.Context, s *account.Service) error {
					if err := s.SetDisabled(ctx, args[0], disabled); err != nil {
						return err
					}
					_, err := fmt.Fprintf(c.out, "%s %s\n", done, args[0])
					return err
				})
			},
		}
	}
	var revoke bool
	adminCmd := &cobra.Command{
		Use:   "admin EMAIL",
		Short: "Make an account an administrator, or with --revoke a user again",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.withAccounts(cmd.Context(), func(ctx context.Context, s *account.Service) error {
				if err := s.SetAdmin(ctx, args[0], !revoke); err != nil {
					return err
				}
				done := "%s is an administrator\n"
				if revoke {
					done = "%s is no longer an administrator\n"
				}
				_, err := fmt.Fprintf(c.out, done, args[0])
				return err
			})
		},
	}
	adminCmd.Flags().BoolVar(&revoke, "revoke", false, "remove the administrator role")

	cmd.AddCommand(add, list, passwd, adminCmd,
		setDisabled("disable", "Disable an account: it cannot sign in and its sessions end at once", "disabled", true),
		setDisabled("enable", "Enable a disabled account", "enabled", false),
	)
	return cmd
}

// userJSON is an account as the CLI prints it (no password hash).
type userJSON struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	Theme    string `json:"theme"`
	Language string `json:"language"`
	Role     string `json:"role"` // "administrator" or "user"
	Status   string `json:"status"`
}

func userView(u account.User) userJSON {
	status, role := "active", "user"
	if u.Disabled() {
		status = "disabled since " + u.DisabledAt.Format(time.RFC3339)
	}
	if u.Admin {
		role = "administrator"
	}
	return userJSON{ID: u.ID, Email: u.Email, Name: u.Name, Theme: u.Theme, Language: u.Language, Role: role, Status: status}
}

// withAccounts opens the database (its migrations must be applied) and runs fn with the
// accounts service.
func (c *cli) withAccounts(ctx context.Context, fn func(context.Context, *account.Service) error) error {
	cfg, err := c.config()
	if err != nil {
		return err
	}
	db, err := openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	m, err := store.NewMigrator(db, c.logger())
	if err != nil {
		return err
	}
	if pending, err := m.HasPending(ctx); err != nil {
		return err
	} else if pending {
		return errors.New("the database has pending migrations: run streamgo migrate up first")
	}
	return fn(ctx, account.New(store.NewAccounts(db), account.Options{}))
}

// readPassword reads a password from standard input (--password-stdin) or from a terminal
// prompt without echo, asked twice; never from the arguments.
func (c *cli) readPassword(fromStdin bool) (string, error) {
	if fromStdin {
		line, err := bufio.NewReader(c.in).ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("read the password from standard input: %w", err)
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	f, ok := c.in.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) { //nolint:gosec // file descriptors fit in int
		return "", errors.New("no terminal to prompt for the password: use --password-stdin")
	}
	read := func(prompt string) (string, error) {
		_, _ = fmt.Fprint(c.errOut, prompt)
		b, err := term.ReadPassword(int(f.Fd())) //nolint:gosec // file descriptors fit in int
		_, _ = fmt.Fprintln(c.errOut)
		return string(b), err
	}
	pw, err := read("Password: ")
	if err != nil {
		return "", err
	}
	again, err := read("Repeat password: ")
	if err != nil {
		return "", err
	}
	if again != pw {
		return "", errors.New("the passwords do not match")
	}
	return pw, nil
}
