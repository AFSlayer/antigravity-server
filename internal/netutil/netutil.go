// Package netutil holds small helpers for dialing loopback listeners.
package netutil

import (
	"errors"
	"strings"
	"syscall"
)

// IsConnRefused reports whether err means nothing listens on the dialed
// port, on Unix or on Windows (WSAECONNREFUSED, 10061).
func IsConnRefused(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.Errno(10061)) {
		return true
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "connection refused") || strings.Contains(lower, "actively refused")
}
