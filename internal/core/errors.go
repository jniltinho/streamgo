// Package core holds what every domain package shares: the typed errors the HTTP layer maps
// to responses. The domain packages live under it (account, broadcast, stage, studio,
// destination, output) and import only the standard library, golang.org/x/crypto and each
// other (design D9; enforced by the depguard rule in .golangci.yml).
package core

import "errors"

// Domain errors. Packages wrap them (fmt.Errorf("...: %w", core.ErrNotFound)) and the HTTP
// layer maps them: ErrNotFound → 404, ErrConflict → 409, *InvalidError → 422 with its field.
var (
	// ErrNotFound is a missing resource, or one the caller does not own (authorization answers 404).
	ErrNotFound = errors.New("not found")
	// ErrConflict is a request the current state does not allow (e.g. deleting a live broadcast).
	ErrConflict = errors.New("conflict")
)

// InvalidError is input that fails validation; Message is safe to show to the user.
type InvalidError struct {
	Field   string
	Message string
}

func (e *InvalidError) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return e.Field + ": " + e.Message
}

// Invalid returns an *InvalidError for field.
func Invalid(field, message string) error { return &InvalidError{Field: field, Message: message} }
