package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/destination"
	"github.com/jniltinho/streamgo/internal/httpserver"
)

// BroadcastsDeps are what the broadcast routes need.
type BroadcastsDeps struct {
	Broadcasts   *broadcast.Service
	Destinations *destination.Service
	// Studio is given read-only to the studio in every broadcast detail.
	Studio StudioSettings
}

// StudioSettings are the server settings the owner's studio builds on: the stage limit, the program size and
// the transcoder's video bitrate (the WHIP sender's ceiling stays above it).
type StudioSettings struct {
	MaxOnStage       int `json:"max_on_stage"`
	ProgramWidth     int `json:"program_width"`
	ProgramHeight    int `json:"program_height"`
	VideoBitrateKbps int `json:"video_bitrate_kbps"`
}

// RegisterBroadcasts adds the broadcast routes to g (srv.UserAPI()).
func RegisterBroadcasts(g *echo.Group, d BroadcastsDeps) {
	g.GET("/broadcasts", d.list)
	g.POST("/broadcasts", d.create)
	g.GET("/broadcasts/:id", d.get)
	g.PATCH("/broadcasts/:id", d.rename)
	g.DELETE("/broadcasts/:id", d.delete)
	g.POST("/broadcasts/:id/invite", d.rotateInvite)
	g.PUT("/broadcasts/:id/destinations", d.selectDestinations)
}

type outputView struct {
	State     string     `json:"state"`
	Reason    string     `json:"reason"`
	StartedAt *time.Time `json:"started_at"`
	Deadline  *time.Time `json:"deadline"`
}

type broadcastView struct {
	ID     string     `json:"id"`
	Title  string     `json:"title"`
	Output outputView `json:"output"`
}

type broadcastDetailView struct {
	broadcastView
	InviteURL    string                     `json:"invite_url"`
	Stage        json.RawMessage            `json:"stage"`
	Destinations []broadcastDestinationView `json:"destinations"`
	Studio       StudioSettings             `json:"studio"`
}

type broadcastDestinationView struct {
	destinationView
	Status string `json:"status"`
	Error  string `json:"error"`
}

func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func newBroadcastView(b *broadcast.Broadcast) broadcastView {
	return broadcastView{ID: b.ID, Title: b.Title, Output: outputView{
		State: string(b.Output.State), Reason: b.Output.Reason,
		StartedAt: optionalTime(b.Output.StartedAt), Deadline: optionalTime(b.Output.Deadline),
	}}
}

func selectedViews(list []destination.Selected) []broadcastDestinationView {
	out := make([]broadcastDestinationView, len(list))
	for i, s := range list {
		out[i] = broadcastDestinationView{destinationView: newDestinationView(&s.Destination),
			Status: s.Status.State, Error: s.Status.Error}
	}
	return out
}

func (d BroadcastsDeps) detail(ctx context.Context, b *broadcast.Broadcast) (broadcastDetailView, error) {
	invite, err := d.Broadcasts.InviteURL(b)
	if err != nil {
		return broadcastDetailView{}, err
	}
	dests, err := d.Destinations.ForBroadcast(ctx, b.ID)
	if err != nil {
		return broadcastDetailView{}, err
	}
	return broadcastDetailView{broadcastView: newBroadcastView(b), InviteURL: invite,
		Stage: json.RawMessage(b.Stage), Destinations: selectedViews(dests), Studio: d.Studio}, nil
}

func (d BroadcastsDeps) writeDetail(c *echo.Context, status int, b *broadcast.Broadcast) error {
	v, err := d.detail(c.Request().Context(), b)
	if err != nil {
		return err
	}
	return c.JSON(status, v)
}

func (d BroadcastsDeps) list(c *echo.Context) error {
	list, err := d.Broadcasts.List(c.Request().Context(), httpserver.UserID(c))
	if err != nil {
		return err
	}
	out := make([]broadcastView, len(list))
	for i := range list {
		out[i] = newBroadcastView(&list[i])
	}
	return c.JSON(http.StatusOK, map[string]any{"broadcasts": out})
}

type titleBody struct {
	Title string `json:"title"`
}

func (d BroadcastsDeps) create(c *echo.Context) error {
	var body titleBody
	if err := c.Bind(&body); err != nil {
		return err
	}
	b, err := d.Broadcasts.Create(c.Request().Context(), httpserver.UserID(c), body.Title)
	if err != nil {
		return err
	}
	return d.writeDetail(c, http.StatusCreated, b)
}

func (d BroadcastsDeps) get(c *echo.Context) error {
	b, err := d.Broadcasts.Get(c.Request().Context(), httpserver.UserID(c), c.Param("id"))
	if err != nil {
		return err
	}
	return d.writeDetail(c, http.StatusOK, b)
}

func (d BroadcastsDeps) rename(c *echo.Context) error {
	var body titleBody
	if err := c.Bind(&body); err != nil {
		return err
	}
	b, err := d.Broadcasts.Rename(c.Request().Context(), httpserver.UserID(c), c.Param("id"), body.Title)
	if err != nil {
		return err
	}
	return d.writeDetail(c, http.StatusOK, b)
}

func (d BroadcastsDeps) delete(c *echo.Context) error {
	err := d.Broadcasts.Delete(c.Request().Context(), httpserver.UserID(c), c.Param("id"))
	if err != nil {
		return conflict(err, "The broadcast is live; end it before deleting it.")
	}
	return c.NoContent(http.StatusNoContent)
}

func (d BroadcastsDeps) rotateInvite(c *echo.Context) error {
	url, err := d.Broadcasts.RotateInvite(c.Request().Context(), httpserver.UserID(c), c.Param("id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"invite_url": url})
}

func (d BroadcastsDeps) selectDestinations(c *echo.Context) error {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := c.Bind(&body); err != nil {
		return err
	}
	list, err := d.Destinations.Select(c.Request().Context(), httpserver.UserID(c), c.Param("id"), body.IDs)
	if err != nil {
		return outputError(err) // a refusal by the live output says why: the last destination, one in use
	}
	return c.JSON(http.StatusOK, map[string]any{"destinations": selectedViews(list)})
}

// conflict gives a core.ErrConflict the message saying why.
func conflict(err error, message string) error {
	if errors.Is(err, core.ErrConflict) {
		return httpserver.NewError(http.StatusConflict, "conflict", message)
	}
	return err
}
