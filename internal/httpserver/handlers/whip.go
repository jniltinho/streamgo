package handlers

import (
	"io"
	"mime"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/streamgo/internal/httpserver"
)

// RegisterWHIP adds the program's WHIP routes to g (srv.UserAPI()), design D3. There is no
// PATCH (trickle ICE): the router answers 405. Offers fit the default 64 kB body limit.
func RegisterWHIP(g *echo.Group, d LiveDeps) {
	g.POST("/broadcasts/:id/program/whip", d.publish)
	g.DELETE("/broadcasts/:id/program/whip/sessions/:sid", func(c *echo.Context) error {
		if err := d.Output.Unpublish(c.Request().Context(), httpserver.UserID(c), c.Param("id"), c.Param("sid")); err != nil {
			return outputError(err)
		}
		return c.NoContent(http.StatusOK)
	})
}

func (d LiveDeps) publish(c *echo.Context) error {
	req := c.Request()
	if mt, _, err := mime.ParseMediaType(req.Header.Get("Content-Type")); err != nil || mt != "application/sdp" {
		return httpserver.NewError(http.StatusUnsupportedMediaType, "unsupported_media_type", "The offer must be application/sdp.")
	}
	offer, err := io.ReadAll(req.Body)
	if err != nil {
		return err // the body limit's 413
	}
	if len(offer) == 0 {
		return httpserver.ErrBadRequest
	}
	id := c.Param("id")
	a, sid, err := d.Output.Publish(req.Context(), httpserver.UserID(c), id, offer)
	if err != nil {
		return outputError(err)
	}
	h := c.Response().Header()
	h.Set("Location", "/api/v1/broadcasts/"+id+"/program/whip/sessions/"+sid)
	if a.ETag != "" {
		h.Set("ETag", a.ETag)
	}
	for _, l := range a.Links {
		h.Add("Link", l)
	}
	return c.Blob(http.StatusCreated, "application/sdp", a.SDP)
}
