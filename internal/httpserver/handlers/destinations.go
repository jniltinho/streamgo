package handlers

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/streamgo/internal/core/destination"
	"github.com/jniltinho/streamgo/internal/httpserver"
)

// DestinationsDeps are what the destination routes need.
type DestinationsDeps struct {
	Destinations *destination.Service
}

// RegisterDestinations adds the destination routes to g (srv.UserAPI()).
func RegisterDestinations(g *echo.Group, d DestinationsDeps) {
	g.GET("/destinations", d.list)
	g.POST("/destinations", d.create)
	g.PATCH("/destinations/:id", d.update)
	g.DELETE("/destinations/:id", d.delete)
}

// destinationView is everything of a destination the API returns: never the address or key.
type destinationView struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Scheme string `json:"scheme"`
	Host   string `json:"host"`
	HasKey bool   `json:"has_key"`
}

func newDestinationView(d *destination.Destination) destinationView {
	return destinationView{ID: d.ID, Name: d.Name, Scheme: d.Scheme, Host: d.Host, HasKey: d.HasKey}
}

const destinationInUse = "The destination is used by a broadcast that is live; end it first."

func (d DestinationsDeps) list(c *echo.Context) error {
	list, err := d.Destinations.List(c.Request().Context(), httpserver.UserID(c))
	if err != nil {
		return err
	}
	out := make([]destinationView, len(list))
	for i := range list {
		out[i] = newDestinationView(&list[i])
	}
	return c.JSON(http.StatusOK, map[string]any{"destinations": out})
}

func (d DestinationsDeps) create(c *echo.Context) error {
	var body struct {
		Name    string `json:"name"`
		Address string `json:"address"`
		Key     string `json:"key"`
	}
	if err := c.Bind(&body); err != nil {
		return err
	}
	dest, err := d.Destinations.Create(c.Request().Context(), httpserver.UserID(c),
		destination.Input{Name: body.Name, Address: body.Address, Key: body.Key})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, newDestinationView(dest))
}

func (d DestinationsDeps) update(c *echo.Context) error {
	var body struct {
		Name    *string `json:"name"`
		Address *string `json:"address"`
		Key     *string `json:"key"`
	}
	if err := c.Bind(&body); err != nil {
		return err
	}
	dest, err := d.Destinations.Update(c.Request().Context(), httpserver.UserID(c), c.Param("id"),
		destination.Patch{Name: body.Name, Address: body.Address, Key: body.Key})
	if err != nil {
		return conflict(err, destinationInUse)
	}
	return c.JSON(http.StatusOK, newDestinationView(dest))
}

func (d DestinationsDeps) delete(c *echo.Context) error {
	if err := d.Destinations.Delete(c.Request().Context(), httpserver.UserID(c), c.Param("id")); err != nil {
		return conflict(err, destinationInUse)
	}
	return c.NoContent(http.StatusNoContent)
}
