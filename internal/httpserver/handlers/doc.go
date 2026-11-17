// Package handlers holds the thin HTTP handlers of the API (decode, call the core, encode).
//
// Each work package owns its files here and exposes one registration function that the
// composition root (cmd/server.go) calls, so no package edits the router:
//
//	func RegisterBroadcasts(g *echo.Group, d BroadcastsDeps) // g = srv.UserAPI()
//	func RegisterJoin(g *echo.Group, d JoinDeps)             // g = srv.API()
//
// Routes on httpserver.Server.UserAPI() require a session; httpserver.UserID(c) returns the
// signed-in user. Handlers return core errors (core.ErrNotFound, core.ErrConflict,
// *core.InvalidError) or *httpserver.Error; the server writes the error envelope. A route that
// needs a body larger than httpserver.DefaultBodyLimit exports its entry for
// httpserver.Options.BodyLimits ("POST /api/v1/broadcasts/:id/logo": 2 << 20).
package handlers
