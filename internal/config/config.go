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
)

// Database drivers accepted in database.driver.
const (
	DriverSQLite   = "sqlite"
	DriverMariaDB  = "mariadb"
	DriverPostgres = "postgres"
)

// Program resolutions accepted in output.resolution.
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
	// Secret holds STREAMGO_SECRET_KEY, which seals stored secrets (destinations, invite links).
	Secret Secret `toml:"secret"`
}

// Server holds the web server settings.
type Server struct {
	Addr           string   `toml:"addr"`
	PublicURL      string   `toml:"public_url"`      // canonical origin, e.g. https://studio.example.com
	DataDir        string   `toml:"data_dir"`        // SQLite database, assets, the instance lock
	TrustedProxies []string `toml:"trusted_proxies"` // IPs or CIDRs whose X-Forwarded-* headers are honoured
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
	VideoBitrateKbps         int      `toml:"video_bitrate_kbps"`
	AudioBitrateKbps         int      `toml:"audio_bitrate_kbps"`
	MaxDestinations          int      `toml:"max_destinations"`
	MaxConcurrent            int      `toml:"max_concurrent"`
	StartTimeout             Duration `toml:"start_timeout"`
	ReconnectGrace           Duration `toml:"reconnect_grace"`
	AllowPrivateDestinations bool     `toml:"allow_private_destinations"`
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
		MediaMTX: MediaMTX{WHIPURL: "http://localhost:8889", RTSPURL: "rtsp://localhost:8554"},
		Studio:   Studio{MaxParticipants: 10, MaxOnStage: 6},
		Output: Output{
			FFmpegPath:       "ffmpeg",
			Resolution:       Resolution720p,
			VideoBitrateKbps: 4000,
			AudioBitrateKbps: 128,
			MaxDestinations:  5,
			MaxConcurrent:    2,
			StartTimeout:     Duration(30 * time.Second),
			ReconnectGrace:   Duration(120 * time.Second),
		},
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
	checkURL("livekit.url", c.LiveKit.URL, "ws", "wss")
	checkURL("livekit.api_url", c.LiveKit.APIURL, "http", "https")
	checkURL("mediamtx.whip_url", c.MediaMTX.WHIPURL, "http", "https")
	checkURL("mediamtx.rtsp_url", c.MediaMTX.RTSPURL, "rtsp")

	between("studio.max_participants", c.Studio.MaxParticipants, 2, 100)
	between("studio.max_on_stage", c.Studio.MaxOnStage, 1, 6)

	if c.Output.FFmpegPath == "" {
		bad("output.ffmpeg_path", "must not be empty")
	}
	if c.Output.Resolution != Resolution720p && c.Output.Resolution != Resolution1080p {
		bad("output.resolution", "must be %q or %q, got %q", Resolution720p, Resolution1080p, c.Output.Resolution)
	}
	between("output.video_bitrate_kbps", c.Output.VideoBitrateKbps, 500, 12000)
	between("output.audio_bitrate_kbps", c.Output.AudioBitrateKbps, 64, 320)
	between("output.max_destinations", c.Output.MaxDestinations, 1, 10)
	between("output.max_concurrent", c.Output.MaxConcurrent, 1, 32)
	durationBetween("output.start_timeout", c.Output.StartTimeout, 5*time.Second, 5*time.Minute)
	durationBetween("output.reconnect_grace", c.Output.ReconnectGrace, 10*time.Second, 30*time.Minute)

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
