package services

import "errors"

var (
	ErrForbidden  = errors.New("permission denied")
	ErrNotFound   = errors.New("resource not found")
	ErrBadRequest = errors.New("invalid request")
)
