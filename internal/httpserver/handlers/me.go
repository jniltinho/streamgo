package handlers

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/streamgo/internal/core/account"
	"github.com/jniltinho/streamgo/internal/httpserver"
	"github.com/jniltinho/streamgo/internal/ratelimit"
	"github.com/jniltinho/streamgo/internal/theme"
)

// MeDeps are what the signed-in user's routes need.
type MeDeps struct {
	Accounts *account.Service
	// Limiter counts password changes per client address (the sign-in limiter).
	Limiter *ratelimit.Limiter
}

// meJSON is the API's User: theme and language are "" when not chosen.
type meJSON struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Theme    string `json:"theme"`
	Language string `json:"language"`
	Admin    bool   `json:"admin"`
}

func toMeJSON(u *account.User) meJSON {
	return meJSON{ID: u.ID, Name: u.Name, Email: u.Email, Theme: u.Theme, Language: u.Language, Admin: u.Admin}
}

// RegisterMe adds GET /me, PATCH /me and PUT /me/password; g is srv.UserAPI().
func RegisterMe(g *echo.Group, d MeDeps) {
	g.GET("/me", d.get)
	g.PATCH("/me", d.patch)
	g.PUT("/me/password", d.password)
}

// password changes the user's own password; the session of this request stays, the others end.
func (d MeDeps) password(c *echo.Context) error {
	if !d.Limiter.Allow(c.RealIP()) {
		return errRateLimited
	}
	var in struct {
		Current  string `json:"current"`
		Password string `json:"password"`
	}
	if err := c.Bind(&in); err != nil {
		return err
	}
	var token string
	if ck, err := c.Cookie(SessionCookie); err == nil {
		token = ck.Value
	}
	if err := d.Accounts.ChangePassword(c.Request().Context(), httpserver.UserID(c), token, in.Current, in.Password); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (d MeDeps) get(c *echo.Context) error {
	u, err := d.Accounts.User(c.Request().Context(), httpserver.UserID(c))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toMeJSON(u))
}

func (d MeDeps) patch(c *echo.Context) error {
	var in struct {
		Name     *string `json:"name"`
		Theme    *string `json:"theme"`
		Language *string `json:"language"`
	}
	if err := c.Bind(&in); err != nil {
		return err
	}
	if in.Theme != nil && *in.Theme != "" && !theme.Valid(*in.Theme) {
		return &httpserver.Error{Status: http.StatusUnprocessableEntity, Code: "invalid_theme", Message: "This theme does not exist.", Field: "theme"}
	}
	if in.Language != nil {
		switch *in.Language {
		case "", "en", "pt-BR":
		default:
			return &httpserver.Error{Status: http.StatusUnprocessableEntity, Code: "invalid_language", Message: "This language is not available.", Field: "language"}
		}
	}
	if in.Name != nil { // validated before anything is stored
		if _, err := d.Accounts.Rename(c.Request().Context(), httpserver.UserID(c), *in.Name); err != nil {
			return err
		}
	}
	u, err := d.Accounts.SetPreferences(c.Request().Context(), httpserver.UserID(c), in.Theme, in.Language)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toMeJSON(u))
}
