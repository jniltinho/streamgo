package handlers

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/streamgo/internal/core/studio"
	"github.com/jniltinho/streamgo/internal/httpserver"
)

func registerBanners(g *echo.Group, s *studio.Service) {
	g.GET("/broadcasts/:id/banners", func(c *echo.Context) error {
		list, err := s.ListBanners(c.Request().Context(), httpserver.UserID(c), c.Param("id"))
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, map[string][]studio.Banner{"banners": list})
	})
	g.POST("/broadcasts/:id/banners", func(c *echo.Context) error {
		var body struct {
			Text string `json:"text"`
		}
		if err := c.Bind(&body); err != nil {
			return httpserver.ErrBadRequest
		}
		bn, err := s.AddBanner(c.Request().Context(), httpserver.UserID(c), c.Param("id"), body.Text)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusCreated, bn)
	})
	g.DELETE("/broadcasts/:id/banners/:bid", func(c *echo.Context) error {
		return stageAnswer(c)(s.DeleteBanner(c.Request().Context(), httpserver.UserID(c), c.Param("id"), c.Param("bid")))
	})
}
