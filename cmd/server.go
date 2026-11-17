package cmd

// Portions adapted from MeetHub (MIT): cmd/server.go

import (
	"context"
	"errors"
	"net"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/spf13/cobra"

	"github.com/jniltinho/streamgo/internal/buildinfo"
	"github.com/jniltinho/streamgo/internal/config"
	"github.com/jniltinho/streamgo/internal/core/account"
	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/destination"
	"github.com/jniltinho/streamgo/internal/core/stage"
	"github.com/jniltinho/streamgo/internal/core/studio"
	"github.com/jniltinho/streamgo/internal/httpserver"
	"github.com/jniltinho/streamgo/internal/httpserver/handlers"
	"github.com/jniltinho/streamgo/internal/livekit"
	"github.com/jniltinho/streamgo/internal/ratelimit"
	"github.com/jniltinho/streamgo/internal/sealed"
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

	// Integration (task 3.I) registers each package's routes here and appends the LiveKit and MediaMTX checks.
	accounts := account.New(store.NewAccounts(db), account.Options{})
	authDeps := handlers.AuthDeps{
		Accounts:     accounts,
		Limiter:      ratelimit.New(10, time.Minute, 100_000), // sign-in attempts per client address
		SecureCookie: cfg.SecureCookies(),
	}
	rooms := livekit.New(cfg.LiveKit.APIURL, cfg.LiveKit.APIKey, cfg.LiveKit.APISecret)
	srv := httpserver.New(httpserver.Options{ //nolint:contextcheck // handlers use each request's own context
		Addr:           cfg.Server.Addr,
		PublicOrigin:   cfg.PublicOrigin(),
		TrustedProxies: cfg.TrustedProxyPrefixes(),
		Logger:         logger,
		Dist:           web.Dist(),
		BodyLimits:     handlers.LogoBodyLimits,
		ReadyChecks: []httpserver.Check{
			{Name: "database", Check: db.Ping},
			{Name: "livekit", Check: rooms.Ready},
		},
		Authenticate: handlers.Authenticator(authDeps), //nolint:contextcheck // it uses each request's own context
	})
	handlers.RegisterAuth(srv.API(), authDeps)
	handlers.RegisterThemes(srv.API())
	handlers.RegisterMe(srv.UserAPI(), handlers.MeDeps{Accounts: accounts})

	// Broadcasts and destinations. Statuses and Notifier (live output) are plugged in when that package is
	// integrated: until then every destination status is "off".
	box, err := sealed.New(cfg.Secret.Bytes())
	if err != nil {
		return err
	}
	locks := &broadcast.Locks{}
	broadcasts := store.NewBroadcasts(db)
	broadcastSvc := &broadcast.Service{
		Repo: broadcasts, Locks: locks, Sealer: box, PublicURL: cfg.Server.PublicURL, Logger: logger,
	}
	destinationSvc := &destination.Service{
		Repo: store.NewDestinations(db), Broadcasts: broadcasts, Locks: locks, Sealer: box,
		AllowPrivate: cfg.Output.AllowPrivateDestinations, MaxPerBroadcast: cfg.Output.MaxDestinations,
	}
	handlers.RegisterBroadcasts(srv.UserAPI(), handlers.BroadcastsDeps{
		Broadcasts: broadcastSvc, Destinations: destinationSvc,
	})
	handlers.RegisterDestinations(srv.UserAPI(), handlers.DestinationsDeps{Destinations: destinationSvc})

	// Studio: the stage's single writer, joining, guests, banners, the logo, and the room reconciler.
	writer := &stage.Writer{
		Broadcasts: broadcasts, Locks: locks, Rooms: rooms, MaxOnStage: cfg.Studio.MaxOnStage, Logger: logger,
	}
	studioSvc := &studio.Service{
		Broadcasts: broadcasts, Locks: locks, Stage: writer, Rooms: rooms,
		Guests: store.NewGuests(db), Banners: store.NewBanners(db),
		OwnerName: func(ctx context.Context, userID string) (string, error) {
			u, err := accounts.User(ctx, userID)
			if err != nil {
				return "", err
			}
			return u.Name, nil
		},
		URL: cfg.LiveKit.URL, MaxParticipants: cfg.Studio.MaxParticipants,
		AssetsDir: filepath.Join(cfg.Server.DataDir, "assets"), Logger: logger,
	}
	broadcastSvc.Remover = roomRemover{studioSvc}
	handlers.RegisterStudio(srv.UserAPI(), handlers.StudioDeps{Studio: studioSvc}) //nolint:contextcheck // handlers use each request's own context
	handlers.RegisterJoin(srv.API(), handlers.JoinDeps{                            //nolint:contextcheck // as above
		Studio:        studioSvc,
		Limiter:       ratelimit.New(20, time.Minute, 100_000), // guest joins per client address
		SecureCookies: cfg.SecureCookies(),
	})
	if root, ok := srv.Handler().(*echo.Echo); ok {
		handlers.RegisterAssets(root, studioSvc)
	}

	// The reconciler is owned here: it stops, and is waited for, before the database closes.
	reconciler := &studio.Reconciler{Studio: studioSvc}
	rctx, stopReconciler := context.WithCancel(ctx)
	reconciled := make(chan struct{})
	go func() {
		defer close(reconciled)
		reconciler.Run(rctx)
	}()
	defer func() {
		stopReconciler()
		<-reconciled
	}()

	if err := srv.Run(ctx, onListen); err != nil {
		return err
	}
	logger.Info("streamgo stopped")
	return nil
}

// roomRemover is the broadcasts package's Remover: deleting a broadcast closes its room and removes its logo.
type roomRemover struct{ studio *studio.Service }

func (r roomRemover) RemoveBroadcast(ctx context.Context, b broadcast.Broadcast) error {
	return r.studio.Teardown(ctx, b.ID, b.Logo)
}
