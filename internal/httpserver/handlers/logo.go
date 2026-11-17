package handlers

import (
	"bytes"
	"errors"
	"image"
	_ "image/jpeg" // registers the JPEG header and decoder
	_ "image/png"  // registers the PNG header and decoder
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/labstack/echo/v5"
	_ "golang.org/x/image/webp" // registers the WebP header and decoder

	"github.com/jniltinho/streamgo/internal/core"
	"github.com/jniltinho/streamgo/internal/core/studio"
	"github.com/jniltinho/streamgo/internal/httpserver"
)

// Logo limits (design D8).
const (
	MaxLogoBytes = 2 << 20
	MaxLogoSide  = 2000
)

// LogoBodyLimits go into httpserver.Options.BodyLimits: the file plus room for the multipart
// envelope, refused before anything is read.
var LogoBodyLimits = map[string]int64{"POST /api/v1/broadcasts/:id/logo": MaxLogoBytes + 16<<10}

// logoFormats maps the image formats accepted for a logo to their file extensions.
var logoFormats = map[string]string{"png": ".png", "jpeg": ".jpg", "webp": ".webp"}

var errNotImage = core.Invalid("file", "The logo must be a PNG, JPEG or WebP image.")

func registerLogo(g *echo.Group, s *studio.Service) {
	g.POST("/broadcasts/:id/logo", func(c *echo.Context) error {
		data, err := readLogo(c.Request())
		if err != nil {
			return err
		}
		ext, err := inspectLogo(data)
		if err != nil {
			return err
		}
		return stageAnswer(c)(s.SetLogo(c.Request().Context(), httpserver.UserID(c), c.Param("id"), data, ext))
	})
	g.DELETE("/broadcasts/:id/logo", func(c *echo.Context) error {
		return stageAnswer(c)(s.DeleteLogo(c.Request().Context(), httpserver.UserID(c), c.Param("id")))
	})
}

// readLogo streams the multipart body to the "file" part and reads at most MaxLogoBytes of it.
func readLogo(r *http.Request) ([]byte, error) {
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, httpserver.NewError(http.StatusUnsupportedMediaType, "unsupported_media_type", "Upload the logo as multipart/form-data.")
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return nil, core.Invalid("file", "Choose an image to upload.")
		}
		if err != nil {
			return nil, bodyError(err)
		}
		if part.FormName() != "file" {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(part, MaxLogoBytes+1))
		if err != nil {
			return nil, bodyError(err)
		}
		if len(data) > MaxLogoBytes {
			return nil, httpserver.NewError(http.StatusRequestEntityTooLarge, "too_large", "The logo must be at most 2 MB.")
		}
		return data, nil
	}
}

// bodyError keeps the body limit's 413; anything else is a malformed upload.
func bodyError(err error) error {
	var sc echo.HTTPStatusCoder
	if errors.As(err, &sc) {
		return err
	}
	return httpserver.ErrBadRequest
}

// inspectLogo reads the image's header for its format and size before decoding it, then
// decodes it fully so a file that only starts like an image is refused.
func inspectLogo(data []byte) (ext string, err error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", errNotImage
	}
	ext, ok := logoFormats[format]
	if !ok {
		return "", errNotImage
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > MaxLogoSide || cfg.Height > MaxLogoSide {
		return "", core.Invalid("file", "The logo must be at most 2000 pixels a side.")
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return "", errNotImage
	}
	return ext, nil
}

// Router is where RegisterAssets adds its route: the Echo instance (srv.Handler()) or a
// group at the root.
type Router interface {
	GET(path string, h echo.HandlerFunc, m ...echo.MiddlewareFunc) echo.RouteInfo
}

// RegisterAssets serves the logos under studio.LogoURLPrefix from the service's assets
// directory, with their real content type and nosniff. It must be added at the root, outside
// /api/v1; the web app's own /assets/ files are not shadowed.
func RegisterAssets(r Router, s *studio.Service) {
	r.GET(studio.LogoURLPrefix+":name", func(c *echo.Context) error {
		name := c.Param("name")
		p := s.LogoPath(name)
		if p == "" {
			return httpserver.ErrNotFound
		}
		data, err := os.ReadFile(p) //nolint:gosec // p is a validated name under the assets directory
		if errors.Is(err, os.ErrNotExist) {
			return httpserver.ErrNotFound
		}
		if err != nil {
			return err
		}
		h := c.Response().Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "public, max-age=31536000, immutable") // names are random and never reused
		return c.Blob(http.StatusOK, studio.LogoTypes[filepath.Ext(name)], data)
	})
}
