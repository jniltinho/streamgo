package httpserver

// Portions adapted from MeetHub (MIT): internal/httpserver/errors.go

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/streamgo/internal/core"
)

// Error is an API error: an HTTP status, a stable machine code and an English message.
// Handlers return it, or a core error that the error handler maps.
type Error struct {
	Status  int
	Code    string
	Message string
	Field   string // the input field at fault, for invalid_input
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// NewError builds an API error.
func NewError(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// Common API errors.
var (
	ErrBadRequest      = NewError(http.StatusBadRequest, "bad_request", "The request is malformed.")
	ErrUnauthenticated = NewError(http.StatusUnauthorized, "unauthenticated", "You must sign in.")
	ErrCrossOrigin     = NewError(http.StatusForbidden, "cross_origin", "The request does not come from this site.")
	ErrForbidden       = NewError(http.StatusForbidden, "forbidden", "You are not allowed to do this.")
	ErrNotFound        = NewError(http.StatusNotFound, "not_found", "The requested resource does not exist.")
	ErrConflict        = NewError(http.StatusConflict, "conflict", "The request conflicts with the current state.")
	ErrTooLarge        = NewError(http.StatusRequestEntityTooLarge, "too_large", "The request body is too large.")
	ErrInternal        = NewError(http.StatusInternalServerError, "internal", "An internal error occurred.")
)

// errorBody is the JSON envelope of every API error: {"error":{"code","message"}}, with the
// field at fault when the input is invalid.
type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Field   string `json:"field,omitempty"`
	} `json:"error"`
}

// errorHandler writes every error in the envelope; server errors are logged with the route
// pattern, never the raw path.
func errorHandler(logger *slog.Logger) echo.HTTPErrorHandler {
	return func(c *echo.Context, err error) {
		if resp, uerr := echo.UnwrapResponse(c.Response()); uerr == nil && resp.Committed {
			return
		}
		apiErr := toAPIError(err)
		if apiErr.Status >= http.StatusInternalServerError {
			logger.ErrorContext(c.Request().Context(), "request failed",
				"method", c.Request().Method, "route", routeOf(c), "error", err.Error())
		}
		if c.Request().Method == http.MethodHead {
			_ = c.NoContent(apiErr.Status)
			return
		}
		var body errorBody
		body.Error.Code, body.Error.Message, body.Error.Field = apiErr.Code, apiErr.Message, apiErr.Field
		_ = c.JSON(apiErr.Status, body)
	}
}

func toAPIError(err error) *Error {
	var apiErr *Error
	var invalid *core.InvalidError
	var sc echo.HTTPStatusCoder
	switch {
	case errors.As(err, &apiErr):
		return apiErr
	case errors.As(err, &invalid):
		return &Error{
			Status: http.StatusUnprocessableEntity, Code: "invalid_input",
			Message: invalid.Message, Field: invalid.Field,
		}
	case errors.Is(err, core.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, core.ErrForbidden):
		return ErrForbidden
	case errors.Is(err, core.ErrConflict):
		return ErrConflict
	case errors.As(err, &sc):
		switch sc.StatusCode() {
		case http.StatusNotFound:
			return ErrNotFound
		case http.StatusMethodNotAllowed:
			return NewError(http.StatusMethodNotAllowed, "method_not_allowed", "This method is not allowed here.")
		case http.StatusRequestEntityTooLarge:
			return ErrTooLarge
		case http.StatusUnsupportedMediaType:
			return NewError(http.StatusUnsupportedMediaType, "unsupported_media_type", "The request has an unsupported content type.")
		case http.StatusBadRequest:
			return ErrBadRequest
		case http.StatusUnauthorized:
			return ErrUnauthenticated
		case http.StatusServiceUnavailable:
			return NewError(http.StatusServiceUnavailable, "unavailable", "The service is unavailable.")
		}
	}
	return ErrInternal
}
