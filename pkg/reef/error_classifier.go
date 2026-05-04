package reef

import (
	"context"
	"net"
	"strings"
)

// IsRetryable determines whether an error is eligible for local retry.
// Network errors and context deadline exceeded are retryable.
// Permanent errors like invalid arguments and syntax errors are not.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Context deadline exceeded — likely transient load/network
	if isContextError(err) {
		return true
	}

	// Network errors — retryable
	if isNetError(err) {
		return true
	}

	msg := err.Error()

	// Permanent failures — not retryable
	permanentSignals := []string{
		"syntax error",
		"invalid argument",
		"invalid parameter",
		"permission denied",
		"not found",
		"already exists",
	}
	for _, signal := range permanentSignals {
		if strings.Contains(strings.ToLower(msg), signal) {
			return false
		}
	}

	// Default: retryable (safe assumption for unknown errors)
	return true
}

func isContextError(err error) bool {
	if err == context.DeadlineExceeded || err == context.Canceled {
		return true
	}
	return false
}

func isNetError(err error) bool {
	if _, ok := err.(net.Error); ok {
		return true
	}
	// Check for wrapped net errors via error string
	msg := err.Error()
	netSignals := []string{
		"connection refused",
		"connection reset",
		"no such host",
		"i/o timeout",
		"broken pipe",
		"use of closed network connection",
	}
	for _, signal := range netSignals {
		if strings.Contains(strings.ToLower(msg), signal) {
			return true
		}
	}
	return false
}
