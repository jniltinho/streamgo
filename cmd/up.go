package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/jniltinho/streamgo/internal/buildinfo"
	"github.com/jniltinho/streamgo/internal/config"
	"github.com/jniltinho/streamgo/internal/livekit"
	"github.com/jniltinho/streamgo/internal/mediamtx"
	"github.com/jniltinho/streamgo/internal/mediaservers"
	"github.com/jniltinho/streamgo/internal/store"
)

// upFlags are the flags of up; each one is applied only when given.
type upFlags struct {
	addr, publicURL, nodeIP, certFile, keyFile string
	liveKitTCP, liveKitUDP, webRTC             int
	selfSigned, noMigrate                      bool
}

func newUpCmd(c *cli) *cobra.Command {
	var f upFlags
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Run LiveKit, MediaMTX and the StreamGo server together, from this one file",
		Long: "up runs the two media servers (from the payload of this executable, else the files beside it, else PATH) " +
			"and the StreamGo server, creates <data-dir>/streamgo.env with the secrets on the first start, and stops the " +
			"three on SIGINT or SIGTERM. It does not run as root.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			return c.runUp(ctx, cmd.Flags(), f)
		},
	}
	d := config.Default()
	fl := cmd.Flags()
	fl.StringVar(&f.addr, "addr", d.Server.Addr, "address the server listens on (server.addr)")
	fl.StringVar(&f.publicURL, "public-url", d.Server.PublicURL, "public URL browsers use (server.public_url)")
	fl.StringVar(&f.nodeIP, "node-ip", "", "address the media servers advertise to browsers (media.node_ip) "+
		`(default: the IPv4 address of the default route, else 127.0.0.1)`)
	fl.IntVar(&f.liveKitTCP, "livekit-tcp-port", d.Media.LiveKitTCPPort, "LiveKit's WebRTC port over TCP (media.livekit_tcp_port)")
	fl.IntVar(&f.liveKitUDP, "livekit-udp-port", d.Media.LiveKitUDPPort, "LiveKit's WebRTC port over UDP (media.livekit_udp_port)")
	fl.IntVar(&f.webRTC, "webrtc-port", d.Media.WebRTCPort, "MediaMTX's WebRTC port, UDP and TCP (media.webrtc_port)")
	fl.BoolVar(&f.selfSigned, "self-signed", false, "HTTPS with a certificate generated under <data-dir>/tls (server.self_signed) (default false)")
	fl.StringVar(&f.certFile, "cert-file", "", "PEM certificate chain to serve HTTPS with (server.cert_file) (default none)")
	fl.StringVar(&f.keyFile, "key-file", "", "its PEM key (server.key_file) (default none)")
	fl.BoolVar(&f.noMigrate, "no-migrate", false, "do not apply pending migrations at startup (default false)")
	return cmd
}

// apply sets on cfg the flags the user gave.
func (f upFlags) apply(fl *pflag.FlagSet, cfg *config.Config) {
	set := fl.Changed
	if set("addr") {
		cfg.Server.Addr = f.addr
	}
	if set("public-url") {
		cfg.Server.PublicURL = f.publicURL
	}
	if set("node-ip") {
		cfg.Media.NodeIP = f.nodeIP
	}
	if set("livekit-tcp-port") {
		cfg.Media.LiveKitTCPPort = f.liveKitTCP
	}
	if set("livekit-udp-port") {
		cfg.Media.LiveKitUDPPort = f.liveKitUDP
	}
	if set("webrtc-port") {
		cfg.Media.WebRTCPort = f.webRTC
	}
	if set("self-signed") {
		cfg.Server.SelfSigned = f.selfSigned
	}
	if set("cert-file") {
		cfg.Server.CertFile = f.certFile
	}
	if set("key-file") {
		cfg.Server.KeyFile = f.keyFile
	}
}

// keys are the settings of the flags the user gave: the environment is not read for them.
func (upFlags) keys(fl *pflag.FlagSet) []string {
	var out []string
	for _, k := range [][2]string{
		{"addr", "server.addr"}, {"public-url", "server.public_url"}, {"node-ip", "media.node_ip"},
		{"livekit-tcp-port", "media.livekit_tcp_port"}, {"livekit-udp-port", "media.livekit_udp_port"},
		{"webrtc-port", "media.webrtc_port"}, {"self-signed", "server.self_signed"},
		{"cert-file", "server.cert_file"}, {"key-file", "server.key_file"},
	} {
		if fl.Changed(k[0]) {
			out = append(out, k[1])
		}
	}
	return out
}

// check rejects a flag value before anything is done, naming the flag.
func (f upFlags) check(fl *pflag.FlagSet) error {
	var errs []error
	for name, v := range map[string]int{"livekit-tcp-port": f.liveKitTCP, "livekit-udp-port": f.liveKitUDP, "webrtc-port": f.webRTC} {
		if fl.Changed(name) && (v < 1 || v > 65535) {
			errs = append(errs, fmt.Errorf("--%s: must be between 1 and 65535, got %d", name, v))
		}
	}
	if fl.Changed("node-ip") {
		if _, err := netip.ParseAddr(f.nodeIP); err != nil {
			errs = append(errs, fmt.Errorf("--node-ip: not an IP address: %q", f.nodeIP))
		}
	}
	slices.SortFunc(errs, func(a, b error) int { return strings.Compare(a.Error(), b.Error()) })
	return errors.Join(errs...)
}

// managedSetting is a setting up sets to the media servers it runs (design B6), with what it sets it to.
type managedSetting struct {
	key  string
	ptr  *string
	want string
}

func managed(cfg *config.Config, p mediaservers.Ports) []managedSetting {
	loop := func(scheme string, port int) string { return scheme + "://127.0.0.1:" + strconv.Itoa(port) }
	return []managedSetting{
		{"livekit.url", &cfg.LiveKit.URL, ""}, // browsers use the server's /livekit/ proxy
		{"livekit.api_url", &cfg.LiveKit.APIURL, loop("http", p.LiveKit)},
		{"mediamtx.whip_url", &cfg.MediaMTX.WHIPURL, loop("http", p.WHIP)},
		{"mediamtx.rtsp_url", &cfg.MediaMTX.RTSPURL, loop("rtsp", p.RTSP)},
		{"mediamtx.api_url", &cfg.MediaMTX.APIURL, loop("http", p.API)},
	}
}

func (c *cli) runUp(ctx context.Context, fl *pflag.FlagSet, f upFlags) error {
	euid := os.Geteuid
	if c.geteuid != nil {
		euid = c.geteuid
	}
	if euid() == 0 {
		return errors.New("streamgo up does not run as root: run it as an unprivileged user (it unpacks and runs programs in its data directory)")
	}
	if err := f.check(fl); err != nil {
		return err
	}
	// the internal ports are chosen once the configuration is known (below); here only what the operator set
	defaults := config.Default()
	var replaced []string
	c.overridden = f.keys(fl)
	c.override = func(cfg *config.Config) {
		f.apply(fl, cfg)
		replaced = replaced[:0]
		defs := managed(defaults, mediaservers.Ports{})
		for i, m := range managed(cfg, mediaservers.Ports{}) {
			if *m.ptr != *defs[i].ptr && *m.ptr != m.want { // the operator set it
				replaced = append(replaced, m.key)
			}
			*m.ptr = m.want // port 0 until the ports are chosen: what is validated is what up sets
		}
	}

	// the data directory and the lock before any change
	raw, err := c.source().Raw()
	if err != nil {
		return err
	}
	dataDir := raw.Server.DataDir
	if dataDir == "" {
		return errors.New("--data-dir: must not be empty")
	}
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return fmt.Errorf("--data-dir: %w", err)
	}
	instance, err := store.LockInstance(dataDir)
	if err != nil {
		return err
	}
	defer func() { _ = instance.Close() }()
	if err := config.CheckDataDir(dataDir); err != nil {
		return err
	}
	if err := config.CheckPrivateDir(dataDir); err != nil { // up runs programs from it
		return err
	}
	logger := c.logger()
	added, err := config.EnsureEnvFile(dataDir, os.LookupEnv, nil)
	if err != nil {
		return err
	}
	if len(added) > 0 {
		logger.Info("stored the secrets in the data directory: back it up", "file", config.EnvFilePath(dataDir), "added", strings.Join(added, ", "))
	}
	cfg, err := c.config()
	if err != nil {
		return err
	}
	if len(replaced) > 0 {
		logger.Info("up runs the media servers itself: these settings were replaced", "settings", strings.Join(replaced, ", "))
	}
	for flag, file := range map[string]string{"--cert-file": cfg.Server.CertFile, "--key-file": cfg.Server.KeyFile} {
		if file == "" {
			continue
		}
		if _, err := os.ReadFile(file); err != nil { //nolint:gosec // the operator's own setting
			return fmt.Errorf("%s: %w", flag, err)
		}
	}
	tlsConfig, err := serverTLS(cfg, logger)
	if err != nil {
		return err
	}
	if err := config.CheckFFmpeg(ctx, cfg.Output.FFmpegPath); err != nil {
		return err
	}
	ports, err := internalPorts(ctx, cfg)
	if err != nil {
		return err
	}
	for _, m := range managed(cfg, ports) {
		*m.ptr = m.want
	}
	if err := mediaservers.CheckMediaPorts(ctx, []mediaservers.MediaPort{
		{Network: "tcp", Port: ports.LiveKitTCP, Flag: "--livekit-tcp-port"},
		{Network: "udp", Port: ports.LiveKitUDP, Flag: "--livekit-udp-port"},
		{Network: "tcp", Port: ports.WebRTC, Flag: "--webrtc-port"},
		{Network: "udp", Port: ports.WebRTC, Flag: "--webrtc-port"},
	}); err != nil {
		return err
	}
	group, err := c.startMedia(ctx, cfg, ports)
	if err != nil {
		return err
	}
	defer group.Stop() // after the server stopped, before the lock is released
	return c.serve(ctx, cfg, tlsConfig, f.noMigrate, nil)
}

// internalPorts chooses the four internal ports, none of them a media port or the port of server.addr, and returns
// them with the media ports of cfg.
func internalPorts(ctx context.Context, cfg *config.Config) (mediaservers.Ports, error) {
	exclude := []int{cfg.Media.LiveKitTCPPort, cfg.Media.LiveKitUDPPort, cfg.Media.WebRTCPort}
	if _, port, err := net.SplitHostPort(cfg.Server.Addr); err == nil {
		if n, err := strconv.Atoi(port); err == nil {
			exclude = append(exclude, n)
		}
	}
	internal, err := mediaservers.FreePorts(ctx, 4, exclude...)
	if err != nil {
		return mediaservers.Ports{}, err
	}
	return mediaservers.Ports{LiveKit: internal[0], WHIP: internal[1], RTSP: internal[2], API: internal[3],
		LiveKitTCP: cfg.Media.LiveKitTCPPort, LiveKitUDP: cfg.Media.LiveKitUDPPort, WebRTC: cfg.Media.WebRTCPort}, nil
}

// startMedia chooses, configures and starts the two media servers and waits until both answer.
func (c *cli) startMedia(ctx context.Context, cfg *config.Config, ports mediaservers.Ports) (*mediaservers.Group, error) {
	logger := c.logger()
	nodeIP := cfg.Media.NodeIP
	if nodeIP == "" {
		nodeIP = mediaservers.NodeIP(net.Dial)
		logger.Info("the media servers advertise the address of the default route: set another with --node-ip", "node_ip", nodeIP)
	} else {
		logger.Info("the media servers advertise the address of media.node_ip (--node-ip)", "node_ip", nodeIP)
	}
	exe := c.executable
	if exe == "" {
		var err error
		if exe, err = os.Executable(); err != nil {
			return nil, fmt.Errorf("find the own executable: %w", err)
		}
	}
	programs, err := mediaservers.Finder{
		Executable: exe,
		DataDir:    cfg.Server.DataDir,
		Pinned:     map[string]string{mediaservers.LiveKit: buildinfo.LiveKitSHA256, mediaservers.MediaMTX: buildinfo.MediaMTXSHA256},
		Download:   "the single file of a release (streamgo_X.Y.Z_linux_amd64)",
	}.Find(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range programs {
		logger.Info("media server", "program", p.Name, "path", p.Path, "version", p.Version, "from", p.From)
	}
	dir, err := mediaservers.MediaDir(cfg.Server.DataDir)
	if err != nil {
		return nil, err
	}
	creds := mediaservers.Credentials{
		LiveKitKey: cfg.LiveKit.APIKey, LiveKitSecret: cfg.LiveKit.APISecret,
		MediaMTXUser: cfg.MediaMTX.User, MediaMTXPass: cfg.MediaMTX.Secret,
	}
	children, err := mediaservers.Children(mediaservers.Setup{
		Dir: dir, NodeIP: nodeIP, Ports: ports, Credentials: creds, Environ: os.Environ(),
	}, programs[0], programs[1])
	if err != nil {
		return nil, err
	}
	mtx, err := mediamtx.New(cfg.MediaMTX.WHIPURL, cfg.MediaMTX.APIURL, cfg.MediaMTX.User, cfg.MediaMTX.Secret)
	if err != nil {
		return nil, err
	}
	children[0].Ready = livekit.New(cfg.LiveKit.APIURL, cfg.LiveKit.APIKey, cfg.LiveKit.APISecret).Ready
	children[1].Ready = mtx.Ready
	return mediaservers.Start(ctx, mediaservers.Options{
		Logger:  logger,
		Secrets: []string{cfg.LiveKit.APISecret, cfg.MediaMTX.Secret, cfg.Secret.Key, cfg.Database.DSN},
	}, children)
}
