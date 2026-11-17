package handlers

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/streamgo/internal/core/account"
	"github.com/jniltinho/streamgo/internal/httpserver"
)

// UsersDeps are what the administrators' user routes need.
type UsersDeps struct {
	Accounts *account.Service
}

// adminUserJSON is the API's AdminUser.
type adminUserJSON struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Admin    bool   `json:"admin"`
	Disabled bool   `json:"disabled"`
}

func toAdminUserJSON(u *account.User) adminUserJSON {
	return adminUserJSON{ID: u.ID, Name: u.Name, Email: u.Email, Admin: u.Admin, Disabled: u.Disabled()}
}

// RegisterUsers adds GET /users, POST /users and PATCH /users/:id; g is srv.UserAPI(). The core
// answers 403 to anyone who is not an enabled administrator.
func RegisterUsers(g *echo.Group, d UsersDeps) {
	g.GET("/users", d.list)
	g.POST("/users", d.create)
	g.PATCH("/users/:id", d.update)
}

func (d UsersDeps) list(c *echo.Context) error {
	users, err := d.Accounts.ListFor(c.Request().Context(), httpserver.UserID(c))
	if err != nil {
		return err
	}
	out := make([]adminUserJSON, len(users))
	for i := range users {
		out[i] = toAdminUserJSON(&users[i])
	}
	return c.JSON(http.StatusOK, map[string]any{"users": out})
}

func (d UsersDeps) create(c *echo.Context) error {
	var in struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
		Admin    bool   `json:"admin"`
	}
	if err := c.Bind(&in); err != nil {
		return err
	}
	u, err := d.Accounts.CreateBy(c.Request().Context(), httpserver.UserID(c),
		account.NewUser{Email: in.Email, Name: in.Name, Password: in.Password, Admin: in.Admin})
	if err != nil {
		return conflict(err, "A user with this e-mail already exists.")
	}
	return c.JSON(http.StatusCreated, toAdminUserJSON(u))
}

func (d UsersDeps) update(c *echo.Context) error {
	var in struct {
		Name     *string `json:"name"`
		Admin    *bool   `json:"admin"`
		Disabled *bool   `json:"disabled"`
		Password *string `json:"password"`
	}
	if err := c.Bind(&in); err != nil {
		return err
	}
	u, err := d.Accounts.UpdateBy(c.Request().Context(), httpserver.UserID(c), c.Param("id"),
		account.Changes{Name: in.Name, Admin: in.Admin, Disabled: in.Disabled, Password: in.Password})
	switch {
	case errors.Is(err, account.ErrLastAdmin):
		return httpserver.NewError(http.StatusConflict, "conflict", "An install needs an administrator: this is the last enabled one.")
	case errors.Is(err, account.ErrSelf):
		return httpserver.NewError(http.StatusConflict, "conflict", "You cannot disable your own account or remove your own administrator role.")
	case err != nil:
		return err
	}
	return c.JSON(http.StatusOK, toAdminUserJSON(u))
}
