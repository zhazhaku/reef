package reef

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestIsRetryable_NilError(t *testing.T) {
	if IsRetryable(nil) {
		t.Error("nil error should not be retryable")
	}
}

func TestIsRetryable_ContextErr(t *testing.T) {
	if !IsRetryable(context.DeadlineExceeded) {
		t.Error("context deadline exceeded should be retryable")
	}
}

func TestIsRetryable_NetErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"net.OpError", &net.OpError{Err: errors.New("connection refused")}},
		{"connection refused string", errors.New("dial tcp: connection refused")},
		{"i/o timeout string", errors.New("read tcp: i/o timeout")},
		{"broken pipe string", errors.New("write tcp: broken pipe")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !IsRetryable(tt.err) {
				t.Errorf("expected retryable: %v", tt.err)
			}
		})
	}
}

func TestIsRetryable_PermanentErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"syntax error", errors.New("syntax error at line 5")},
		{"invalid argument", errors.New("invalid argument: name is required")},
		{"permission denied", errors.New("permission denied")},
		{"not found", errors.New("key not found")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if IsRetryable(tt.err) {
				t.Errorf("expected non-retryable: %v", tt.err)
			}
		})
	}
}

func TestIsRetryable_UnknownErr(t *testing.T) {
	// Unknown errors default to retryable (safe side)
	if !IsRetryable(errors.New("something went wrong")) {
		t.Error("unknown error should be retryable by default")
	}
}
