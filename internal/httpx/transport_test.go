// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package httpx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"syscall"
	"testing"
)

// fakeNetError is a controllable net.Error for the timeout branches.
type fakeNetError struct {
	timeout bool
}

func (e fakeNetError) Error() string   { return "fake network error" }
func (e fakeNetError) Timeout() bool   { return e.timeout }
func (e fakeNetError) Temporary() bool { return e.timeout }

func TestIsTransientTransportError(t *testing.T) {
	t.Parallel()

	urlEOF := &url.Error{Op: "Get", URL: "http://example.invalid", Err: io.EOF}
	wrappedDeadline := fmt.Errorf("client timeout: %w", context.DeadlineExceeded)

	transientErrnos := []error{
		syscall.ECONNRESET,
		syscall.ECONNREFUSED,
		syscall.ECONNABORTED,
		syscall.EPIPE,
		syscall.ETIMEDOUT,
		syscall.EHOSTUNREACH,
		syscall.ENETUNREACH,
	}
	for _, errno := range transientErrnos {
		err := fmt.Errorf("dial: %w", errno)
		if !IsTransientTransportError(t.Context(), err) {
			t.Errorf("errno %v must be transient", errno)
		}
	}

	for name, err := range map[string]error{
		"unexpected EOF":              io.ErrUnexpectedEOF,
		"wrapped unexpected EOF":      fmt.Errorf("read: %w", io.ErrUnexpectedEOF),
		"EOF behind a url.Error":      urlEOF,
		"wrapped url EOF":             fmt.Errorf("do: %w", urlEOF),
		"DNS timeout":                 &net.DNSError{Err: "timeout", Name: "example.invalid", IsTimeout: true},
		"DNS temporary":               &net.DNSError{Err: "retry", Name: "example.invalid", IsTemporary: true},
		"timeout net.Error":           fakeNetError{timeout: true},
		"client deadline passthrough": wrappedDeadline,
	} {
		if !IsTransientTransportError(t.Context(), err) {
			t.Errorf("%s (%v) must be transient", name, err)
		}
	}

	for name, err := range map[string]error{
		"nil":                   nil,
		"bare EOF from a 200":   io.EOF,
		"content error":         errors.New("boom"),
		"TLS verification":      errors.New("remote error: tls: handshake failure"),
		"DNS not found":         &net.DNSError{Err: "no such host", Name: "example.invalid", IsNotFound: true},
		"non-timeout net.Error": fakeNetError{},
		"wrapped non-transient": fmt.Errorf("do: %w", errors.New("boom")),
	} {
		if IsTransientTransportError(t.Context(), err) {
			t.Errorf("%s (%v) must not be transient", name, err)
		}
	}

	if IsTransientTransportError(t.Context(), context.Canceled) {
		t.Error("context.Canceled must never be transient")
	}

	if IsTransientTransportError(t.Context(), fmt.Errorf("stop: %w", context.Canceled)) {
		t.Error("a wrapped context.Canceled must never be transient")
	}

	cancelled, stop := context.WithCancel(t.Context())
	stop()

	if IsTransientTransportError(cancelled, syscall.ECONNRESET) {
		t.Error("an error under a done context must never be transient")
	}
}

func TestTransportFailureClass(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil is other", nil, "other"},
		{"refused", syscall.ECONNREFUSED, "refused"},
		{"reset", syscall.ECONNRESET, "reset"},
		{"aborted is reset", syscall.ECONNABORTED, "reset"},
		{"broken pipe is reset", syscall.EPIPE, "reset"},
		{"host unreachable", syscall.EHOSTUNREACH, "unreachable"},
		{"network unreachable", syscall.ENETUNREACH, "unreachable"},
		{"timed out", syscall.ETIMEDOUT, "timeout"},
		{"unexpected EOF", io.ErrUnexpectedEOF, "eof"},
		{"bare EOF", io.EOF, "eof"},
		{"DNS", &net.DNSError{Err: "no such host", Name: "example.invalid", IsNotFound: true}, "dns"},
		{"net timeout", fakeNetError{timeout: true}, "timeout"},
		{"generic", errors.New("boom"), "other"},
		{"non-timeout net error", fakeNetError{}, "other"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := TransportFailureClass(tc.err); got != tc.want {
				t.Errorf("TransportFailureClass(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}
