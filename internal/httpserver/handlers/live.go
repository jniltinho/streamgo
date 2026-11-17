package handlers

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/output"
	"github.com/jniltinho/streamgo/internal/httpserver"
)

// LiveDeps are what the live output's routes (live and WHIP) need.
type LiveDeps struct {
	Output *output.Service
}

// RegisterLive adds going live and ending to g (srv.UserAPI()).
func RegisterLive(g *echo.Group, d LiveDeps) {
	g.POST("/broadcasts/:id/live", func(c *echo.Context) error {
		return outputAnswer(c)(d.Output.GoLive(c.Request().Context(), httpserver.UserID(c), c.Param("id")))
	})
	g.DELETE("/broadcasts/:id/live", func(c *echo.Context) error {
		return outputAnswer(c)(d.Output.End(c.Request().Context(), httpserver.UserID(c), c.Param("id")))
	})
}

func outputAnswer(c *echo.Context) func(broadcast.Output, error) error {
	return func(o broadcast.Output, err error) error {
		if err != nil {
			return outputError(err)
		}
		return c.JSON(http.StatusOK, map[string]outputView{"output": {
			State: string(o.State), Reason: o.Reason,
			StartedAt: optionalTime(o.StartedAt), Deadline: optionalTime(o.Deadline),
		}})
	}
}

// outputError gives a conflict its message and answers a media server failure 502.
func outputError(err error) error {
	var c *output.Conflict
	switch {
	case errors.As(err, &c):
		return httpserver.NewError(http.StatusConflict, "conflict", c.Message)
	case errors.Is(err, output.ErrUpstream):
		return httpserver.NewError(http.StatusBadGateway, "bad_gateway", "The media server did not accept the program.")
	}
	return err
}
