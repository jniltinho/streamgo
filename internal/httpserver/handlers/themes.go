package handlers

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/streamgo/internal/theme"
)

// RegisterThemes adds GET /themes, the theme registry; g is srv.API().
func RegisterThemes(g *echo.Group) {
	g.GET("/themes", func(c *echo.Context) error {
		return c.JSON(http.StatusOK, map[string][]theme.Theme{"themes": theme.All()})
	})
}
