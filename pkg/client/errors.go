package client

import "errors"

// Sentinel errors. Callers compare via errors.Is to make decisions
// without parsing HTTP status codes themselves.
var (
	// ErrUnauthorized is returned when the server rejects the token
	// (missing, invalid, or expired). A CLI that sees this should
	// typically prompt the user to log in again.
	ErrUnauthorized = errors.New("client: unauthorized")

	// ErrInvalidCredentials is returned by Login when the email or
	// password does not match. Distinct from ErrUnauthorized so the
	// CLI can show a clearer message.
	ErrInvalidCredentials = errors.New("client: invalid credentials")

	// ErrNotFound is returned when the server replies 404 for a
	// resource lookup.
	ErrNotFound = errors.New("client: not found")

	// ErrServer is the catchall for any 5xx response. Callers usually
	// retry or surface the underlying message verbatim.
	ErrServer = errors.New("client: server error")
)
