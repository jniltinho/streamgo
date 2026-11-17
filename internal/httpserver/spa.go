package httpserver

// Portions adapted from MeetHub (MIT): internal/httpserver/spa.go

import (
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"

	"github.com/labstack/echo/v5"
)

const notBuiltPage = `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>StreamGo</title></head>
<body><h1>StreamGo</h1><p>The web app is not built into this binary. Build it with <code>make build</code>.</p></body></html>`

// spaHandler serves the embedded web app: real files as they are (hashed assets cached for
// good), and index.html for any other path so client routes (/studio/:id, /join/:token…)
// survive a reload.
func spaHandler(dist fs.FS) echo.HandlerFunc {
	var index []byte
	if dist != nil {
		index, _ = fs.ReadFile(dist, "index.html")
	}
	return func(c *echo.Context) error {
		r := c.Request()
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			return echo.ErrMethodNotAllowed
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if dist != nil && name != "" && name != "index.html" {
			data, err := fs.ReadFile(dist, name)
			switch {
			case err == nil:
				h := c.Response().Header()
				ct := mime.TypeByExtension(path.Ext(name))
				if ct == "" {
					ct = echo.MIMEOctetStream
				}
				if strings.HasPrefix(name, "assets/") {
					h.Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					h.Set("Cache-Control", "no-cache")
				}
				return c.Blob(http.StatusOK, ct, data)
			case !errors.Is(err, fs.ErrNotExist):
				return err
			case path.Ext(name) != "": // a missing file, not a client route
				return echo.ErrNotFound
			}
		}
		c.Response().Header().Set("Cache-Control", "no-cache")
		if index == nil {
			return c.HTML(http.StatusOK, notBuiltPage)
		}
		return c.HTMLBlob(http.StatusOK, index)
	}
}
