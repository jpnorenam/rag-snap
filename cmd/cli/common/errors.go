package common

import "errors"

var (
	// ErrPermissionDenied reports an operation that needs root privileges.
	ErrPermissionDenied = errors.New("permission denied, try again with sudo")
)
