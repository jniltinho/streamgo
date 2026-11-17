package handlers

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/streamgo/internal/core/account"
	"github.com/jniltinho/streamgo/internal/httpserver"
	"github.com/jniltinho/streamgo/internal/ratelimit"
)

// SessionCookie is the name of the signed-in user's session cookie.
const SessionCookie = "streamgo_session"

// AuthDeps are what sign-in, sign-out and the session check need.
type AuthDeps struct {
	Accounts *account.Service
	// Limiter counts sign-in attempts per client address (10 per minute).
	Limiter *ratelimit.Limiter
	// SecureCookie marks the cookie Secure (server.public_url is HTTPS).
	SecureCookie bool
}

var (
	errInvalidCredentials = httpserver.NewError(http.StatusUnauthorized, "invalid_credentials", "The e-mail or password is incorrect.")
	errRateLimited        = httpserver.NewError(http.StatusTooManyRequests, "rate_limited", "Too many attempts. Try again in a minute.")
)

// RegisterAuth adds POST /auth/login and POST /auth/logout; g is srv.API().
func RegisterAuth(g *echo.Group, d AuthDeps) {
	g.POST("/auth/login", d.login)
	g.POST("/auth/logout", d.logout)
}

// Authenticator is httpserver.Options.Authenticate: it resolves the session cookie to the
// user's id and renews the cookie with the session.
func Authenticator(d AuthDeps) httpserver.Authenticator {
	return func(c *echo.Context) (string, error) {
		ck, err := c.Cookie(SessionCookie)
		if err != nil {
			return "", httpserver.ErrUnauthenticated
		}
		id, err := d.Accounts.Authenticate(c.Request().Context(), ck.Value)
		if errors.Is(err, account.ErrUnauthenticated) {
			return "", httpserver.ErrUnauthenticated
		}
		if err != nil {
			return "", err
		}
		c.SetCookie(d.cookie(ck.Value, int(account.SessionIdle.Seconds())))
		return id, nil
	}
}

func (d AuthDeps) login(c *echo.Context) error {
	if !d.Limiter.Allow(c.RealIP()) {
		return errRateLimited
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.Bind(&in); err != nil {
		return err
	}
	u, token, err := d.Accounts.Login(c.Request().Context(), in.Email, in.Password)
	if errors.Is(err, account.ErrInvalidCredentials) {
		return errInvalidCredentials
	}
	if err != nil {
		return err
	}
	c.SetCookie(d.cookie(token, int(account.SessionIdle.Seconds())))
	return c.JSON(http.StatusOK, toMeJSON(u))
}

func (d AuthDeps) logout(c *echo.Context) error {
	if ck, err := c.Cookie(SessionCookie); err == nil {
		if err := d.Accounts.Logout(c.Request().Context(), ck.Value); err != nil {
			return err
		}
	}
	c.SetCookie(d.cookie("", -1))
	return c.NoContent(http.StatusNoContent)
}

// cookie is the session cookie; maxAge -1 deletes it.
func (d AuthDeps) cookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{ //nolint:gosec // Secure follows server.public_url: plain HTTP is allowed on localhost only

		Name: SessionCookie, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: d.SecureCookie,
	}
}
