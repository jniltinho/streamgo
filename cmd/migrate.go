package cmd

// Portions adapted from MeetHub (MIT): cmd/migrate.go

import (
	"context"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/jniltinho/streamgo/internal/config"
	"github.com/jniltinho/streamgo/internal/store"
)

func newMigrateCmd(c *cli) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply or list database migrations",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "up",
			Short: "Apply every pending migration",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return c.withMigrator(cmd.Context(), func(m *store.Migrator) error {
					n, err := m.Up(cmd.Context())
					if err != nil {
						return err
					}
					if c.asJSON {
						return c.printJSON(map[string]int{"applied": n})
					}
					if n == 0 {
						_, err = fmt.Fprintln(c.out, "nothing pending: the database is up to date")
					} else {
						_, err = fmt.Fprintf(c.out, "applied %d migration(s)\n", n)
					}
					return err
				})
			},
		},
		&cobra.Command{
			Use:   "status",
			Short: "List each migration and whether it is applied",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return c.withMigrator(cmd.Context(), func(m *store.Migrator) error {
					st, err := m.Status(cmd.Context())
					if err != nil {
						return err
					}
					if c.asJSON {
						return c.printJSON(st)
					}
					tw := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
					_, _ = fmt.Fprintln(tw, "VERSION\tNAME\tSTATE\tAPPLIED AT")
					for _, s := range st {
						state, at := "pending", ""
						if s.Applied {
							state, at = "applied", s.AppliedAt.Format(time.RFC3339)
						}
						_, _ = fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", s.Version, s.Name, state, at)
					}
					return tw.Flush()
				})
			},
		},
	)
	return cmd
}

// withMigrator opens the database, runs fn with a Migrator and closes it.
func (c *cli) withMigrator(ctx context.Context, fn func(*store.Migrator) error) error {
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
	return fn(m)
}

// openStore connects to the configured database.
func openStore(ctx context.Context, cfg *config.Config) (*store.DB, error) {
	return store.Open(ctx, store.Settings{Driver: cfg.Database.Driver, SQLitePath: cfg.SQLitePath(), DSN: cfg.Database.DSN})
}
