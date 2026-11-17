package httpserver

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/labstack/echo/v5"
)

// liveKitPrefix is where browsers reach LiveKit's signalling through StreamGo's own origin and certificate.
const liveKitPrefix = "/livekit"

// liveKitProxy forwards /livekit/<path> to <target>/<path>, WebSocket upgrades included. The upstream is always
// target: the client's path, Host and headers never choose it.
func liveKitProxy(target *url.URL, logger *slog.Logger) echo.HandlerFunc {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.Out.URL.Path = strings.TrimPrefix(r.In.URL.Path, liveKitPrefix)
			r.Out.URL.RawPath = ""
			r.SetURL(target)           // scheme and host from target, Host header too
			r.Out.Header.Del("Cookie") // StreamGo's session is none of LiveKit's business
		},
		// the request URL carries the participant's token in its query: log the cause only
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			if ue := (*url.Error)(nil); errors.As(err, &ue) {
				err = ue.Err
			}
			logger.Warn("livekit proxy", "error", err.Error())
			w.WriteHeader(http.StatusBadGateway)
		},
	}
	return func(c *echo.Context) error {
		proxy.ServeHTTP(c.Response(), c.Request())
		return nil
	}
}
