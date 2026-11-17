package handlers

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/streamgo/internal/core/account"
	"github.com/jniltinho/streamgo/internal/httpserver"
	"github.com/jniltinho/streamgo/internal/theme"
)

// MeDeps are what the signed-in user's routes need.
type MeDeps struct {
	Accounts *account.Service
}

// meJSON is the API's User: theme and language are "" when not chosen.
type meJSON struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Theme    string `json:"theme"`
	Language string `json:"language"`
}

func toMeJSON(u *account.User) meJSON {
	return meJSON{ID: u.ID, Name: u.Name, Email: u.Email, Theme: u.Theme, Language: u.Language}
}

// RegisterMe adds GET /me and PATCH /me; g is srv.UserAPI().
func RegisterMe(g *echo.Group, d MeDeps) {
	g.GET("/me", d.get)
	g.PATCH("/me", d.patch)
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
