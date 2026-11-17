package handlers

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/stage"
	"github.com/jniltinho/streamgo/internal/core/studio"
	"github.com/jniltinho/streamgo/internal/httpserver"
)

// StudioDeps are what the owner's studio routes need.
type StudioDeps struct {
	Studio *studio.Service
}

// RegisterStudio adds the owner's studio routes to g (srv.UserAPI()): the studio token, the
// stage patch, removing a guest, banners and the logo. The logo route needs
// LogoBodyLimits in httpserver.Options.BodyLimits.
func RegisterStudio(g *echo.Group, d StudioDeps) {
	s := d.Studio
	g.POST("/broadcasts/:id/studio-token", func(c *echo.Context) error {
		j, err := s.StudioToken(c.Request().Context(), httpserver.UserID(c), c.Param("id"))
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, j)
	})
	g.PATCH("/broadcasts/:id/stage", func(c *echo.Context) error {
		p, err := decodePatch(c.Request().Body)
		if err != nil {
			return err
		}
		return stageAnswer(c)(s.PatchStage(c.Request().Context(), httpserver.UserID(c), c.Param("id"), p))
	})
	g.DELETE("/broadcasts/:id/participants/:identity", func(c *echo.Context) error {
		return stageAnswer(c)(s.RemoveGuest(c.Request().Context(), httpserver.UserID(c), c.Param("id"), c.Param("identity")))
	})
	registerBanners(g, s)
	registerLogo(g, s)
}

// stageAnswer writes {"stage": ...}, the answer of every route that changes the stage.
func stageAnswer(c *echo.Context) func(stage.State, error) error {
	return func(st stage.State, err error) error {
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, map[string]stage.State{"stage": st})
	}
}

// decodePatch reads a stage patch: any of layout, sources, banner_id (null hides the banner)
// and accent; any other field, logo included, is refused.
func decodePatch(body io.Reader) (studio.Patch, error) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(body).Decode(&raw); err != nil || raw == nil {
		return studio.Patch{}, httpserver.ErrBadRequest
	}
	var p studio.Patch
	for k, v := range raw {
		var err error
		switch k {
		case "layout":
			err = json.Unmarshal(v, &p.Layout)
			if p.Layout == nil {
				err = core.Invalid(k, "Unknown layout.")
			}
		case "accent":
			err = json.Unmarshal(v, &p.Accent)
			if p.Accent == nil {
				err = core.Invalid(k, "The accent must be a #rrggbb colour.")
			}
		case "sources":
			err = json.Unmarshal(v, &p.Sources)
			if p.Sources == nil {
				p.Sources = &[]stage.Source{}
			}
		case "banner_id":
			p.BannerSet = true
			err = json.Unmarshal(v, &p.BannerID)
		case "logo":
			return studio.Patch{}, core.Invalid(k, "The logo changes only through its upload and delete routes.")
		default:
			return studio.Patch{}, core.Invalid(k, "Unknown field.")
		}
		if err != nil {
			if _, ok := err.(*core.InvalidError); ok { //nolint:errorlint // set just above, never wrapped
				return studio.Patch{}, err
			}
			return studio.Patch{}, core.Invalid(k, "Wrong type.")
		}
	}
	return p, nil
}
