package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/streamgo/internal/core/studio"
	"github.com/jniltinho/streamgo/internal/httpserver"
)

// GuestCookieTTL is how long a guest cookie lasts (design D6).
const GuestCookieTTL = 12 * time.Hour

// Limiter allows a request for key or not; the shared internal/ratelimit limiter implements it.
type Limiter interface {
	Allow(key string) bool
}

// JoinDeps are what the guests' join routes need.
type JoinDeps struct {
	Studio *studio.Service
	// Limiter limits joins per client address (20 a minute).
	Limiter Limiter
	// SecureCookies sets Secure on the guest cookie (server.public_url is HTTPS).
	SecureCookies bool
}

var errRateLimited = httpserver.NewError(http.StatusTooManyRequests, "rate_limited", "Too many requests. Try again in a minute.")

// RegisterJoin adds the invite-link routes to g (srv.API()).
func RegisterJoin(g *echo.Group, d JoinDeps) {
	s := d.Studio
	g.GET("/join/:token", func(c *echo.Context) error {
		b, err := s.Invite(c.Request().Context(), c.Param("token"))
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, map[string]string{"title": b.Title})
	})
	g.POST("/join/:token", func(c *echo.Context) error {
		if !d.Limiter.Allow(c.RealIP()) {
			return errRateLimited
		}
		var body struct {
			Name string `json:"name"`
		}
		if err := c.Bind(&body); err != nil {
			return httpserver.ErrBadRequest
		}
		ctx := c.Request().Context()
		b, err := s.Invite(ctx, c.Param("token"))
		if err != nil {
			return err
		}
		name := guestCookie(b.ID)
		var cookie string
		if ck, err := c.Cookie(name); err == nil {
			cookie = ck.Value
		}
		j, err := s.JoinAsGuest(ctx, b, body.Name, cookie)
		if errors.Is(err, studio.ErrFull) {
			return httpserver.NewError(http.StatusTooManyRequests, "rate_limited", "This studio has too many guests.")
		}
		if err != nil {
			return err
		}
		c.SetCookie(&http.Cookie{ //nolint:gosec // Secure follows server.public_url: plain HTTP is allowed on localhost
			Name: name, Value: j.Cookie, Path: "/api/v1/join", MaxAge: int(GuestCookieTTL / time.Second),
			HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: d.SecureCookies,
		})
		return c.JSON(http.StatusOK, struct {
			studio.Join
			Title string `json:"title"`
		}{j.Join, j.Title})
	})
}

// guestCookie names the guest cookie of a broadcast, so two broadcasts do not interfere.
func guestCookie(broadcastID string) string { return "streamgo_guest_" + broadcastID }
