// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package httpx holds the bounded HTTP request, TLS trust, transport, and
// failure-classification helpers the registry clients use.
//
// COPIED CODE: failure.go and http.go are reduced copies of the Eshu core
// package go/internal/collector/sdk (eshu-hq/eshu), which a standalone
// collector cannot import. Only what the registry clients use is copied; the
// in-process retry loop is omitted because the extension host owns retries by
// releasing and reclaiming the claim. tls.go and transport.go are copies of
// the trust and transport-classifier helpers from the same core package.
// Replace this package with the published eshu-sdk helpers when
// eshu-hq/eshu#8851 lands.
//
// One deliberate hardening differs from the in-tree helper: response bodies
// are read through a size cap (MaxResponseBytes), so a hostile or
// misbehaving endpoint cannot make the process allocate without bound.
package httpx
