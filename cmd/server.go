package cmd

// Portions adapted from MeetHub (MIT): cmd/server.go

import (
	"context"
	"errors"
	"net"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/jniltinho/streamgo/internal/buildinfo"
	"github.com/jniltinho/streamgo/internal/config"
	"github.com/jniltinho/streamgo/internal/httpserver"
	"github.com/jniltinho/streamgo/internal/store"
	"github.com/jniltinho/streamgo/web"
)

func newServerCmd(c *cli) *cobra.Command {
	var noMigrate bool
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Run the StreamGo server (HTTP API and web app)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			return c.runServer(ctx, noMigrate, nil)
		},
	}
	cmd.Flags().BoolVar(&noMigrate, "no-migrate", false, "do not apply pending migrations at startup")
	return cmd
}

// runServer checks the configuration and the host, wires the server and runs it until ctx
// ends. onListen, when not nil, receives the bound address.
func (c *cli) runServer(ctx context.Context, noMigrate bool, onListen func(net.Addr)) error {
	cfg, err := c.config()
	if err != nil {
		return err
	}
	logger := c.logger()
	if err := config.CheckDataDir(cfg.Server.DataDir); err != nil {
		return err
	}
	instance, err := store.LockInstance(cfg.Server.DataDir)
	if err != nil {
		return err
	}
	defer func() { _ = instance.Close() }()
	if err := config.CheckFFmpeg(ctx, cfg.Output.FFmpegPath); err != nil {
		return err
	}
	logger.Info("starting streamgo", "version", buildinfo.Version, "database", cfg.Database.Driver)

	db, err := openStore(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	migrator, err := store.NewMigrator(db, logger)
	if err != nil {
		return err
	}
	if !noMigrate {
		n, err := migrator.Up(ctx)
		if err != nil {
			return err
		}
		logger.Info("database migrated", "applied", n)
	} else if pending, err := migrator.HasPending(ctx); err != nil {
		return err
	} else if pending {
		return errors.New("the database has pending migrations: run streamgo migrate up (or start without --no-migrate)")
	}

	// Integration (task 3.I) registers each package's routes here, e.g.
	// handlers.RegisterBroadcasts(srv.UserAPI(), deps), and appends the LiveKit and MediaMTX checks.
	srv := httpserver.New(httpserver.Options{ //nolint:contextcheck // handlers use each request's own context
		Addr:           cfg.Server.Addr,
		PublicOrigin:   cfg.PublicOrigin(),
		TrustedProxies: cfg.TrustedProxyPrefixes(),
		Logger:         logger,
		Dist:           web.Dist(),
		ReadyChecks:    []httpserver.Check{{Name: "database", Check: db.Ping}},
	})
	if err := srv.Run(ctx, onListen); err != nil {
		return err
	}
	logger.Info("streamgo stopped")
	return nil
}
