// Package config loads StreamGo's settings: defaults, then a TOML file, then
// STREAMGO_<SECTION>_<KEY> environment variables. Secrets are accepted only
// from the environment and are never part of the TOML file, logs or output.
//
// Portions adapted from MeetHub (MIT): internal/config/config.go
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jniltinho/streamgo/internal/core/broadcast"
)

// Database drivers accepted in database.driver.
const (
	DriverSQLite   = "sqlite"
	DriverMariaDB  = "mariadb"
	DriverPostgres = "postgres"
)

// Defaults of output.resolution and output.max_resolution; the accepted values are broadcast.Resolutions().
const (
	Resolution720p  = "720p"
	Resolution1080p = "1080p"
)

// SecretKeyBytes is the length of STREAMGO_SECRET_KEY once decoded.
const SecretKeyBytes = 32

// Config is the complete server configuration.
type Config struct {
	Server   Server   `toml:"server"`
	Database Database `toml:"database"`
	LiveKit  LiveKit  `toml:"livekit"`
	MediaMTX MediaMTX `toml:"mediamtx"`
	Studio   Studio   `toml:"studio"`
	Output   Output   `toml:"output"`
	Media    Media    `toml:"media"`
	// Secret holds STREAMGO_SECRET_KEY, which seals stored secrets (destinations, invite links).
	Secret Secret `toml:"secret"`
}

// Server holds the web server settings.
type Server struct {
	Addr           string   `toml:"addr"`
	PublicURL      string   `toml:"public_url"`      // canonical origin, e.g. https://studio.example.com
	DataDir        string   `toml:"data_dir"`        // SQLite database, assets, the instance lock
	TrustedProxies []string `toml:"trusted_proxies"` // IPs or CIDRs whose X-Forwarded-* headers are honoured
	CertFile       string   `toml:"cert_file"`       // PEM certificate chain served over HTTPS
	KeyFile        string   `toml:"key_file"`        // its PEM key
	SelfSigned     bool     `toml:"self_signed"`     // HTTPS with a certificate generated under <data_dir>/tls
}

// Database selects the one database StreamGo uses.
type Database struct {
	Driver string `toml:"driver"` // sqlite, mariadb or postgres
	Path   string `toml:"path"`   // SQLite file; empty: <data_dir>/streamgo.db
	DSN    string `toml:"dsn" secret:"true"`
}

// LiveKit reaches the LiveKit server that hosts the studio rooms.
type LiveKit struct {
	URL       string `toml:"url"`     // WebSocket URL the browsers use
	APIURL    string `toml:"api_url"` // HTTP URL the server uses
	APIKey    string `toml:"api_key" secret:"true"`
	APISecret string `toml:"api_secret" secret:"true"`
}

// MediaMTX reaches the media server that receives the program by WHIP.
type MediaMTX struct {
	WHIPURL string `toml:"whip_url"` // HTTP base URL of MediaMTX's WebRTC server
	RTSPURL string `toml:"rtsp_url"` // RTSP base URL ffmpeg reads and writes
	APIURL  string `toml:"api_url"`  // HTTP base URL of MediaMTX's control API
	User    string `toml:"user" secret:"true"`
	Secret  string `toml:"secret" secret:"true"`
}

// Studio limits the studio rooms.
type Studio struct {
	MaxParticipants int `toml:"max_participants"`
	MaxOnStage      int `toml:"max_on_stage"`
}

// Output configures the live output (ffmpeg transcoder and relays).
type Output struct {
	FFmpegPath               string   `toml:"ffmpeg_path"`
	Resolution               string   `toml:"resolution"`
	MaxResolution            string   `toml:"max_resolution"`
	VideoBitrateKbps         int      `toml:"video_bitrate_kbps"`
	AudioBitrateKbps         int      `toml:"audio_bitrate_kbps"`
	MaxDestinations          int      `toml:"max_destinations"`
	MaxConcurrent            int      `toml:"max_concurrent"`
	StartTimeout             Duration `toml:"start_timeout"`
	ReconnectGrace           Duration `toml:"reconnect_grace"`
	AllowPrivateDestinations bool     `toml:"allow_private_destinations"`
}

// Media holds the settings of the media servers `streamgo up` runs; the server alone ignores them.
type Media struct {
	NodeIP         string `toml:"node_ip"`          // address browsers reach the media ports at; empty: the default route's
	LiveKitTCPPort int    `toml:"livekit_tcp_port"` // LiveKit's WebRTC over TCP
	LiveKitUDPPort int    `toml:"livekit_udp_port"` // LiveKit's WebRTC over UDP
	WebRTCPort     int    `toml:"webrtc_port"`      // MediaMTX's WebRTC, UDP and TCP
}

// Secret is the key section; its only field is read from STREAMGO_SECRET_KEY.
type Secret struct {
	Key string `toml:"key" secret:"true"`
}

// Bytes is the decoded secret key; nil when unset or invalid (Validate rejects an invalid one).
func (s Secret) Bytes() []byte {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s.Key))
	if err != nil || len(b) != SecretKeyBytes {
		return nil
	}
	return b
}

// Duration is a time.Duration written as a Go duration string ("30s", "2m").
type Duration time.Duration

// UnmarshalText parses a duration string.
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

// MarshalText formats the duration.
func (d Duration) MarshalText() ([]byte, error) { return []byte(time.Duration(d).String()), nil }

// Std returns the value as a time.Duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// Default returns the built-in defaults.
func Default() *Config {
	return &Config{
		Server: Server{
			Addr:      ":8080",
			PublicURL: "http://localhost:8080",
			DataDir:   "./data",
		},
		Database: Database{Driver: DriverSQLite},
		LiveKit:  LiveKit{URL: "ws://localhost:7880", APIURL: "http://localhost:7880"},
		MediaMTX: MediaMTX{WHIPURL: "http://localhost:8889", RTSPURL: "rtsp://localhost:8554", APIURL: "http://localhost:9997"},
		Studio:   Studio{MaxParticipants: 10, MaxOnStage: 6},
		Output: Output{
			FFmpegPath:       "ffmpeg",
			Resolution:       Resolution720p,
			MaxResolution:    Resolution1080p,
			VideoBitrateKbps: 0,
			AudioBitrateKbps: 128,
			MaxDestinations:  5,
			MaxConcurrent:    2,
			StartTimeout:     Duration(30 * time.Second),
			ReconnectGrace:   Duration(120 * time.Second),
		},
		Media: Media{LiveKitTCPPort: 7881, LiveKitUDPPort: 7882, WebRTCPort: 8189},
	}
}

// SQLitePath is the SQLite database file: database.path or <data_dir>/streamgo.db.
func (c *Config) SQLitePath() string {
	if c.Database.Path != "" {
		return c.Database.Path
	}
	return filepath.Join(c.Server.DataDir, "streamgo.db")
}

// PublicOrigin is the scheme://host[:port] of server.public_url.
func (c *Config) PublicOrigin() string {
	u, err := url.Parse(c.Server.PublicURL)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// PublicHost is the host name of server.public_url, without the port.
func (c *Config) PublicHost() string {
	u, err := url.Parse(c.Server.PublicURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// TLSFiles returns the certificate and the key the server serves HTTPS with: the configured pair (which wins over
// self_signed), or the self-signed one under <data_dir>/tls. Both are empty when it speaks plain HTTP.
func (c *Config) TLSFiles() (cert, key string) {
	switch {
	case c.Server.CertFile != "" || c.Server.KeyFile != "":
		return c.Server.CertFile, c.Server.KeyFile
	case c.Server.SelfSigned:
		dir := filepath.Join(c.Server.DataDir, "tls")
		return filepath.Join(dir, "selfsigned.crt"), filepath.Join(dir, "selfsigned.key")
	}
	return "", ""
}

// BrowserLiveKitURL is the LiveKit URL given to browsers: livekit.url unchanged, or, when it is empty, the public
// origin with /livekit (the server's own signalling proxy), as wss:// under HTTPS and ws:// under HTTP.
func (c *Config) BrowserLiveKitURL() string {
	if c.LiveKit.URL != "" {
		return c.LiveKit.URL
	}
	origin := c.PublicOrigin()
	if rest, ok := strings.CutPrefix(origin, "https://"); ok {
		return "wss://" + rest + "/livekit"
	}
	return "ws://" + strings.TrimPrefix(origin, "http://") + "/livekit"
}

// SecureCookies reports whether cookies must be Secure (the public URL is HTTPS).
func (c *Config) SecureCookies() bool { return strings.HasPrefix(c.Server.PublicURL, "https://") }

// TrustedProxyPrefixes parses server.trusted_proxies (validated by Validate).
func (c *Config) TrustedProxyPrefixes() []netip.Prefix {
	out := make([]netip.Prefix, 0, len(c.Server.TrustedProxies))
	for _, s := range c.Server.TrustedProxies {
		if p, err := parsePrefix(s); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// Validate checks every setting against its allowed values and returns all problems joined.
func (c *Config) Validate() error {
	var errs []error
	bad := func(key, format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", key, fmt.Sprintf(format, args...)))
	}
	between := func(key string, v, lo, hi int) {
		if v < lo || v > hi {
			bad(key, "must be between %d and %d, got %d", lo, hi, v)
		}
	}
	durationBetween := func(key string, v Duration, lo, hi time.Duration) {
		if v.Std() < lo || v.Std() > hi {
			bad(key, "must be between %s and %s, got %s", lo, hi, v.Std())
		}
	}

	if _, _, err := net.SplitHostPort(c.Server.Addr); err != nil {
		bad("server.addr", "must be host:port or :port, got %q", c.Server.Addr)
	}
	if msg := publicURLProblem(c.Server.PublicURL); msg != "" {
		bad("server.public_url", "%s", msg)
	}
	if (c.Server.CertFile == "") != (c.Server.KeyFile == "") {
		bad("server.cert_file", "and server.key_file must be set together")
	}
	if cert, _ := c.TLSFiles(); cert != "" && !strings.HasPrefix(c.Server.PublicURL, "https://") {
		bad("server.public_url", "must be https:// when the server speaks HTTPS (server.cert_file or server.self_signed), got %q", c.Server.PublicURL)
	}
	if c.Server.DataDir == "" {
		bad("server.data_dir", "must not be empty")
	}
	for _, p := range c.Server.TrustedProxies {
		if _, err := parsePrefix(p); err != nil {
			bad("server.trusted_proxies", "%q is not an IP address or CIDR", p)
		}
	}

	switch c.Database.Driver {
	case DriverSQLite:
	case DriverMariaDB, DriverPostgres:
		if c.Database.DSN == "" {
			bad("database.dsn", "%sDATABASE_DSN is required for %s", EnvPrefix, c.Database.Driver)
		}
	default:
		bad("database.driver", "must be %q, %q or %q, got %q", DriverSQLite, DriverMariaDB, DriverPostgres, c.Database.Driver)
	}

	checkURL := func(key, raw string, schemes ...string) {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || !slices.Contains(schemes, u.Scheme) {
			bad(key, "must be an absolute %s URL, got %q", strings.Join(schemes, " or "), raw)
		}
	}
	if c.LiveKit.URL != "" { // empty: browsers use the server's own /livekit proxy
		checkURL("livekit.url", c.LiveKit.URL, "ws", "wss")
	}
	checkURL("livekit.api_url", c.LiveKit.APIURL, "http", "https")
	checkURL("mediamtx.whip_url", c.MediaMTX.WHIPURL, "http", "https")
	checkURL("mediamtx.rtsp_url", c.MediaMTX.RTSPURL, "rtsp")
	checkURL("mediamtx.api_url", c.MediaMTX.APIURL, "http", "https")

	between("studio.max_participants", c.Studio.MaxParticipants, 2, 100)
	between("studio.max_on_stage", c.Studio.MaxOnStage, 1, 6)

	if c.Output.FFmpegPath == "" {
		bad("output.ffmpeg_path", "must not be empty")
	}
	resolutions := strings.Join(broadcast.Resolutions(), ", ")
	if !broadcast.Known(c.Output.Resolution) {
		bad("output.resolution", "must be one of %s, got %q", resolutions, c.Output.Resolution)
	}
	if !broadcast.Known(c.Output.MaxResolution) {
		bad("output.max_resolution", "must be one of %s, got %q", resolutions, c.Output.MaxResolution)
	}
	if broadcast.Known(c.Output.Resolution) && broadcast.Known(c.Output.MaxResolution) &&
		broadcast.Above(c.Output.Resolution, c.Output.MaxResolution) {
		bad("output.resolution", "%q is above output.max_resolution %q", c.Output.Resolution, c.Output.MaxResolution)
	}
	if c.Output.VideoBitrateKbps != 0 { // 0: the ladder's bitrate of each resolution
		between("output.video_bitrate_kbps", c.Output.VideoBitrateKbps, 500, 40000)
	}
	between("output.audio_bitrate_kbps", c.Output.AudioBitrateKbps, 64, 320)
	between("output.max_destinations", c.Output.MaxDestinations, 1, 10)
	between("output.max_concurrent", c.Output.MaxConcurrent, 1, 32)
	durationBetween("output.start_timeout", c.Output.StartTimeout, 5*time.Second, 5*time.Minute)
	durationBetween("output.reconnect_grace", c.Output.ReconnectGrace, 10*time.Second, 30*time.Minute)

	if c.Media.NodeIP != "" {
		if _, err := netip.ParseAddr(c.Media.NodeIP); err != nil {
			bad("media.node_ip", "must be an IP address, got %q", c.Media.NodeIP)
		}
	}
	between("media.livekit_tcp_port", c.Media.LiveKitTCPPort, 1, 65535)
	between("media.livekit_udp_port", c.Media.LiveKitUDPPort, 1, 65535)
	between("media.webrtc_port", c.Media.WebRTCPort, 1, 65535)
	if c.Media.WebRTCPort == c.Media.LiveKitTCPPort || c.Media.WebRTCPort == c.Media.LiveKitUDPPort {
		bad("media.webrtc_port", "must differ from media.livekit_tcp_port and media.livekit_udp_port, got %d", c.Media.WebRTCPort)
	}

	// never the value: only whether it is there and its length
	switch {
	case c.Secret.Key == "":
		errs = append(errs, fmt.Errorf("%sSECRET_KEY is required: %d random bytes in base64 (make init creates one)", EnvPrefix, SecretKeyBytes))
	case c.Secret.Bytes() == nil:
		errs = append(errs, fmt.Errorf("%sSECRET_KEY must be %d bytes in base64", EnvPrefix, SecretKeyBytes))
	}
	return errors.Join(errs...)
}

// publicURLProblem explains why raw cannot be the public URL, or returns "".
func publicURLProblem(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Sprintf("must be an absolute https:// URL, got %q", raw)
	}
	if u.Scheme == "http" && !loopbackHost(u.Hostname()) {
		return "must use https:// (plain http only on localhost or a loopback address): browsers need HTTPS for camera and microphone"
	}
	return ""
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	a, err := netip.ParseAddr(host)
	return err == nil && a.IsLoopback()
}

func parsePrefix(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		return netip.ParsePrefix(s)
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(a, a.BitLen()), nil
}
