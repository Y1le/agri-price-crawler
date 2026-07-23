package identity

import "errors"

var (
	ErrNotFound            = errors.New("identity: not found")
	ErrConflict            = errors.New("identity: conflict")
	ErrInvalidRequest      = errors.New("identity: invalid request")
	ErrRateLimited         = errors.New("identity: rate limited")
	ErrCodeInvalid         = errors.New("identity: code invalid")
	ErrCodeExpired         = errors.New("identity: code expired")
	ErrAccountDisabled     = errors.New("identity: account disabled")
	ErrStateUnavailable    = errors.New("identity: state unavailable")
	ErrUpstreamUnavailable = errors.New("identity: upstream unavailable")
	ErrTokenInvalid        = errors.New("identity: token invalid")
	ErrTokenReused         = errors.New("identity: token reused")
)
