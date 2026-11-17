package handlers

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/streamgo/internal/core/broadcast"
	"github.com/jniltinho/streamgo/internal/core/settings"
	"github.com/jniltinho/streamgo/internal/httpserver"
)

// SettingsDeps are what the server settings routes need.
type SettingsDeps struct {
	Settings *settings.Service
}

// RegisterSettings adds GET /settings and PUT /settings (add-server-settings, design S5); g is srv.UserAPI(). The
// core answers 403 to anyone who is not an enabled administrator.
func RegisterSettings(g *echo.Group, d SettingsDeps) {
	g.GET("/settings", d.get)
	g.PUT("/settings", d.put)
}

type outputSettingsJSON struct {
	Resolution       string   `json:"resolution"`
	MaxResolution    string   `json:"max_resolution"`
	VideoBitrateKbps int      `json:"video_bitrate_kbps"`
	Resolutions      []string `json:"resolutions"`
	Locked           []string `json:"locked"`
}

func toSettingsJSON(o *settings.Output) map[string]any {
	return map[string]any{"output": outputSettingsJSON{
		Resolution: o.Default, MaxResolution: o.Max, VideoBitrateKbps: o.VideoKbps,
		Resolutions: broadcast.Resolutions(), Locked: append([]string{}, o.Locked...),
	}}
}

func (d SettingsDeps) get(c *echo.Context) error {
	o, err := d.Settings.Get(c.Request().Context(), httpserver.UserID(c))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toSettingsJSON(o))
}

func (d SettingsDeps) put(c *echo.Context) error {
	var in struct {
		Output struct {
			Resolution       *string `json:"resolution"`
			MaxResolution    *string `json:"max_resolution"`
			VideoBitrateKbps *int    `json:"video_bitrate_kbps"`
		} `json:"output"`
	}
	if err := c.Bind(&in); err != nil {
		return err
	}
	o, err := d.Settings.Update(c.Request().Context(), httpserver.UserID(c), settings.Patch{
		Resolution: in.Output.Resolution, MaxResolution: in.Output.MaxResolution, VideoKbps: in.Output.VideoBitrateKbps,
	})
	if err != nil {
		return conflict(err, "This setting is set by the server's environment: change it there.")
	}
	return c.JSON(http.StatusOK, toSettingsJSON(o))
}
