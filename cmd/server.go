package cmd

// Portions adapted from MeetHub (MIT): cmd/server.go

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/url"
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
	"github.com/jniltinho/streamgo/internal/core/output"
	"github.com/jniltinho/streamgo/internal/core/stage"
	"github.com/jniltinho/streamgo/internal/core/studio"
	"github.com/jniltinho/streamgo/internal/ffmpeg"
	"github.com/jniltinho/streamgo/internal/httpserver"
	"github.com/jniltinho/streamgo/internal/httpserver/handlers"
	"github.com/jniltinho/streamgo/internal/livekit"
	"github.com/jniltinho/streamgo/internal/mediamtx"
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
	tlsConfig, err := serverTLS(cfg, logger)
	if err != nil {
		return err
	}
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
	warnFixedBitrate(logger, cfg)

	// Integration (task 3.I) registers each package's routes here and appends the LiveKit and MediaMTX checks.
	accounts := account.New(store.NewAccounts(db), account.Options{})
	authDeps := handlers.AuthDeps{
		Accounts:     accounts,
		Limiter:      ratelimit.New(10, time.Minute, 100_000), // sign-in attempts per client address
		SecureCookie: cfg.SecureCookies(),
	}
	rooms := livekit.New(cfg.LiveKit.APIURL, cfg.LiveKit.APIKey, cfg.LiveKit.APISecret)
	mtx, err := mediamtx.New(cfg.MediaMTX.WHIPURL, cfg.MediaMTX.APIURL, cfg.MediaMTX.User, cfg.MediaMTX.Secret)
	if err != nil {
		return err
	}
	liveKitAPI, err := url.Parse(cfg.LiveKit.APIURL) // validated by config
	if err != nil {
		return err
	}
	srv := httpserver.New(httpserver.Options{ //nolint:contextcheck // handlers use each request's own context
		Addr:           cfg.Server.Addr,
		TLS:            tlsConfig,
		LiveKitAPIURL:  liveKitAPI,
		PublicOrigin:   cfg.PublicOrigin(),
		TrustedProxies: cfg.TrustedProxyPrefixes(),
		Logger:         logger,
		Dist:           web.Dist(),
		BodyLimits:     handlers.LogoBodyLimits,
		ReadyChecks: []httpserver.Check{
			{Name: "database", Check: db.Ping},
			{Name: "livekit", Check: rooms.Ready},
			{Name: "mediamtx", Check: mtx.Ready},
		},
		Authenticate: handlers.Authenticator(authDeps), //nolint:contextcheck // it uses each request's own context
	})
	handlers.RegisterAuth(srv.API(), authDeps)
	handlers.RegisterThemes(srv.API())
	handlers.RegisterMe(srv.UserAPI(), handlers.MeDeps{Accounts: accounts, Limiter: authDeps.Limiter})
	handlers.RegisterUsers(srv.UserAPI(), handlers.UsersDeps{Accounts: accounts})

	// Broadcasts and destinations. The live output below gives the destinations their statuses and hears
	// about selection changes of a broadcast that is not offline.
	box, err := sealed.New(cfg.Secret.Bytes())
	if err != nil {
		return err
	}
	locks := &broadcast.Locks{}
	broadcasts := store.NewBroadcasts(db)
	broadcastSvc := &broadcast.Service{
		Repo: broadcasts, Locks: locks, Sealer: box, PublicURL: cfg.Server.PublicURL, Logger: logger,
		Quality: quality(cfg),
	}
	destinations := store.NewDestinations(db)
	destinationSvc := &destination.Service{
		Repo: destinations, Broadcasts: broadcasts, Locks: locks, Sealer: box,
		AllowPrivate: cfg.Output.AllowPrivateDestinations, MaxPerBroadcast: cfg.Output.MaxDestinations,
	}
	handlers.RegisterBroadcasts(srv.UserAPI(), handlers.BroadcastsDeps{
		Broadcasts: broadcastSvc, Destinations: destinationSvc, Studio: studioSettings(cfg),
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
		URL: cfg.BrowserLiveKitURL(), MaxParticipants: cfg.Studio.MaxParticipants,
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

	// Live output: the state machine, the ffmpeg supervisor and the program ingest through MediaMTX.
	supervisor := ffmpeg.New(ffmpeg.Options{
		Path: cfg.Output.FFmpegPath, RTSPURL: cfg.MediaMTX.RTSPURL,
		User: cfg.MediaMTX.User, Secret: cfg.MediaMTX.Secret, AudioKbps: cfg.Output.AudioBitrateKbps, Logger: logger,
	})
	out := &output.Service{
		Broadcasts: broadcasts, Locks: locks, Destinations: destinations, Targets: destinationSvc,
		Stage: writer, Ingest: mtx, Processes: supervisor,
		MaxConcurrent: cfg.Output.MaxConcurrent, StartTimeout: cfg.Output.StartTimeout.Std(),
		Grace: cfg.Output.ReconnectGrace.Std(), Quality: quality(cfg), Logger: logger,
	}
	destinationSvc.Statuses, destinationSvc.Notifier = out, out
	handlers.RegisterLive(srv.UserAPI(), handlers.LiveDeps{Output: out}) //nolint:contextcheck // handlers use each request's own context
	handlers.RegisterWHIP(srv.UserAPI(), handlers.LiveDeps{Output: out}) //nolint:contextcheck // as above

	// The output and the reconciler are owned here: on a signal the HTTP server drains while the output
	// stops every ffmpeg at once, leaving the stored state alone; both are waited for before the database
	// closes. Run begins with restart recovery.
	octx, stopOutput := context.WithCancel(ctx)
	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		out.Run(octx)
	}()
	defer func() {
		stopOutput()
		<-outputDone
	}()
	reconciler := &studio.Reconciler{Studio: studioSvc, Steps: []studio.Step{out.CloseOrphans}}
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

// studioSettings are the server's settings the studio reads from every broadcast detail; the handler adds the
// broadcast's own program size and bitrate.
func studioSettings(cfg *config.Config) handlers.StudioSettings {
	return handlers.StudioSettings{MaxOnStage: cfg.Studio.MaxOnStage}
}

// quality is the output's resolutions: the default of new broadcasts, the ceiling and the bitrate override.
func quality(cfg *config.Config) broadcast.Quality {
	return broadcast.Quality{Default: cfg.Output.Resolution, Max: cfg.Output.MaxResolution, VideoKbps: cfg.Output.VideoBitrateKbps}
}

// warnFixedBitrate tells the operator when a fixed video bitrate starves the highest resolution a broadcast may
// use. A config.toml copied from the example of 0.1.0 spells video_bitrate_kbps = 4000, which then applies to
// every resolution: 0 lets the bitrate follow the resolution.
func warnFixedBitrate(logger *slog.Logger, cfg *config.Config) {
	fixed := cfg.Output.VideoBitrateKbps
	byResolution := broadcast.Quality{Default: cfg.Output.MaxResolution, Max: cfg.Output.MaxResolution}.Profile(cfg.Output.MaxResolution)
	if fixed != 0 && fixed < byResolution.VideoKbps {
		logger.Warn("output.video_bitrate_kbps is fixed below what the highest resolution is meant for: set it to 0 to follow the resolution",
			"video_bitrate_kbps", fixed, "max_resolution", cfg.Output.MaxResolution, "by_resolution_kbps", byResolution.VideoKbps)
	}
}

// roomRemover is the broadcasts package's Remover: deleting a broadcast closes its room and removes its logo.
type roomRemover struct{ studio *studio.Service }

func (r roomRemover) RemoveBroadcast(ctx context.Context, b broadcast.Broadcast) error {
	return r.studio.Teardown(ctx, b.ID, b.Logo)
}
