// Package httpserver is StreamGo's HTTP layer on Echo v5: the /api/v1 JSON API with its
// error envelope and request protections, the health and readiness probes, the route-pattern
// request log, client addresses behind trusted proxies and the embedded web app.
//
// Feature packages add their routes with a registration function in handlers/ that receives
// API() (anyone) or UserAPI() (a signed-in user); nobody edits the router itself.
//
// Portions adapted from MeetHub (MIT): internal/httpserver/server.go
package httpserver

import (
	"context"
	"crypto/tls"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"github.com/jniltinho/streamgo/internal/tlsutil"
)

// DefaultBodyLimit bounds every request body (JSON and SDP) unless Options.BodyLimits says otherwise.
const DefaultBodyLimit = 64 << 10

// ShutdownTimeout is how long in-flight requests get to finish once the server stops.
const ShutdownTimeout = 10 * time.Second

// Check is a named readiness probe; it returns nil when the dependency is ready. The database
// check is the foundation's; the LiveKit and MediaMTX packages add theirs.
type Check struct {
	Name  string
	Check func(ctx context.Context) error
}

// Authenticator resolves the request's session to the signed-in user's id, or returns an
// error (ErrUnauthenticated for no valid session). The accounts package provides it.
type Authenticator func(c *echo.Context) (userID string, err error)

// Options configure a Server.
type Options struct {
	Addr string
	// PublicOrigin is scheme://host[:port] of server.public_url: the only Origin accepted on
	// state-changing requests.
	PublicOrigin   string
	TrustedProxies []netip.Prefix
	// BodyLimits replaces DefaultBodyLimit for single routes, keyed by method and route pattern
	// ("POST /api/v1/broadcasts/:id/logo").
	BodyLimits   map[string]int64
	Logger       *slog.Logger
	Dist         fs.FS // the built web app (web.Dist())
	ReadyChecks  []Check
	Authenticate Authenticator // nil: every UserAPI route answers 401
	// TLS, when set, makes the server speak HTTPS with its first certificate.
	TLS *tls.Config
	// LiveKitAPIURL, when set, is where /livekit/ proxies LiveKit's signalling (livekit.api_url).
	LiveKitAPIURL *url.URL
}

// Server is the configured Echo instance.
type Server struct {
	echo *echo.Echo
	opts Options
	api  *echo.Group
	user *echo.Group
}

// New builds the server with its middleware and the always-present routes (probes, the API's
// JSON 404 and the web app). Feature routes are added through API and UserAPI.
func New(o Options) *Server {
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	// The router's NotFoundHandler runs only when no route matches the path, so a known path
	// with another method still answers 405.
	spa := spaHandler(o.Dist)
	notFound := func(c *echo.Context) error {
		if strings.HasPrefix(c.Request().URL.Path, "/api/") {
			return ErrNotFound
		}
		return spa(c)
	}
	e := echo.NewWithConfig(echo.Config{
		Logger:           o.Logger,
		Router:           echo.NewRouter(echo.RouterConfig{NotFoundHandler: notFound}),
		HTTPErrorHandler: errorHandler(o.Logger),
		IPExtractor:      clientIP(o.TrustedProxies),
		// a group's middleware must not turn the API's 404 into its own answer (e.g. 401)
		NoGroupAutoRegister404Routes: true,
	})
	e.Use(
		requestLogger(o.Logger), // outermost: it logs what Recover makes of a panic
		middleware.Recover(),
		noStore,
		sameOrigin(o.PublicOrigin),
		bodyLimit(DefaultBodyLimit, o.BodyLimits),
	)
	s := &Server{echo: e, opts: o}
	e.GET("/healthz", func(c *echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})
	e.GET("/readyz", s.ready)
	if o.LiveKitAPIURL != nil {
		e.Any("/livekit/*", liveKitProxy(o.LiveKitAPIURL, o.Logger))
	}
	s.api = e.Group("/api/v1")
	s.user = s.api.Group("", s.requireUser)
	return s
}

// API returns the /api/v1 group for routes anyone may call.
func (s *Server) API() *echo.Group { return s.api }

// UserAPI returns the /api/v1 group whose routes require a signed-in user; handlers read the
// user's id with UserID.
func (s *Server) UserAPI() *echo.Group { return s.user }

// Handler returns the HTTP handler (for tests and custom listeners).
func (s *Server) Handler() http.Handler { return s.echo }

const userKey = "streamgo.user"

// requireUser runs the Authenticator and keeps the user's id for UserID.
func (s *Server) requireUser(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if s.opts.Authenticate == nil {
			return ErrUnauthenticated
		}
		id, err := s.opts.Authenticate(c)
		if err != nil {
			return err
		}
		c.Set(userKey, id)
		return next(c)
	}
}

// UserID is the signed-in user's id on a UserAPI route ("" elsewhere).
func UserID(c *echo.Context) string {
	id, _ := c.Get(userKey).(string)
	return id
}

// Run serves until ctx is cancelled, then stops accepting requests and lets in-flight ones
// finish within ShutdownTimeout. onListen, when not nil, receives the bound address.
func (s *Server) Run(ctx context.Context, onListen func(net.Addr)) error {
	sc := echo.StartConfig{
		Address:          s.opts.Addr,
		HideBanner:       true,
		HidePort:         true,
		GracefulTimeout:  ShutdownTimeout,
		ListenerAddrFunc: onListen,
		TLSConfig:        s.opts.TLS,
		BeforeServeFunc: func(srv *http.Server) error {
			srv.ReadHeaderTimeout = 10 * time.Second
			srv.IdleTimeout = 120 * time.Second
			return nil
		},
	}
	if s.opts.TLS != nil {
		leaf := s.opts.TLS.Certificates[0].Leaf // never the key
		s.opts.Logger.Info("https certificate", "subject", leaf.Subject.String(), "sha256", tlsutil.Fingerprint(leaf),
			"not_after", leaf.NotAfter)
	}
	s.opts.Logger.Info("http server listening", "addr", s.opts.Addr, "tls", s.opts.TLS != nil)
	if err := sc.Start(ctx, s.echo); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// ready runs every readiness check and answers 200, or 503 naming what is not ready.
func (s *Server) ready(c *echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), 3*time.Second)
	defer cancel()
	results := make(map[string]string, len(s.opts.ReadyChecks))
	status, overall := http.StatusOK, "ready"
	for _, chk := range s.opts.ReadyChecks {
		if err := chk.Check(ctx); err != nil {
			results[chk.Name] = err.Error()
			status, overall = http.StatusServiceUnavailable, "not_ready"
			continue
		}
		results[chk.Name] = "ok"
	}
	return c.JSON(status, map[string]any{"status": overall, "checks": results})
}

// noStore keeps API responses out of every cache.
func noStore(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if strings.HasPrefix(c.Request().URL.Path, "/api/") {
			c.Response().Header().Set("Cache-Control", "no-store")
		}
		return next(c)
	}
}

// sameOrigin refuses state-changing requests whose Origin is not the public origin; a missing
// or "null" origin is refused too.
func sameOrigin(origin string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			switch c.Request().Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				return next(c)
			}
			if got := c.Request().Header.Get("Origin"); origin == "" || !strings.EqualFold(got, origin) {
				return ErrCrossOrigin
			}
			return next(c)
		}
	}
}

// bodyLimit applies the default request body limit, or a route's own, before the body is read.
func bodyLimit(def int64, routes map[string]int64) echo.MiddlewareFunc {
	std := middleware.BodyLimit(def)
	own := map[string]echo.MiddlewareFunc{}
	for k, n := range routes {
		own[k] = middleware.BodyLimit(n)
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		limited := std(next)
		return func(c *echo.Context) error {
			if mw, ok := own[c.Request().Method+" "+c.Path()]; ok {
				return mw(next)(c)
			}
			return limited(c)
		}
	}
}

// routeOf is the matched route pattern, never the raw path: paths carry invite tokens.
func routeOf(c *echo.Context) string {
	if p := c.Path(); p != "" {
		return p
	}
	return "(no route)"
}

// requestLogger logs one line per request with the route pattern; no raw path, query, body,
// cookie or header.
func requestLogger(logger *slog.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			start := time.Now()
			err := next(c)
			if err != nil {
				c.Echo().HTTPErrorHandler(c, err) // write the response now so its status is logged
			}
			status := 0
			if r, uerr := echo.UnwrapResponse(c.Response()); uerr == nil {
				status = r.Status
			}
			level := slog.LevelInfo
			switch route := c.Path(); {
			case status >= http.StatusInternalServerError:
				level = slog.LevelError
			case route == "/healthz" || route == "/readyz":
				level = slog.LevelDebug
			}
			logger.LogAttrs(c.Request().Context(), level, "http request",
				slog.String("method", c.Request().Method),
				slog.String("route", routeOf(c)),
				slog.Int("status", status),
				slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
				slog.String("client_ip", c.RealIP()),
			)
			return nil
		}
	}
}
