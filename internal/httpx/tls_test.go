// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package httpx

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// mintCACertPEM mints an ephemeral self-signed CA and returns its PEM bytes.
// Tests use it to exercise the custom-CA trust path without touching disk.
func mintCACertPEM(t *testing.T) []byte {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate test CA key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "eshu collector test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("mint test CA certificate: %v", err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestTLSOptionsModeResolvesTrustPosture(t *testing.T) {
	t.Parallel()

	pemBytes := mintCACertPEM(t)

	cases := []struct {
		name    string
		options TLSOptions
		want    TLSMode
	}{
		{"zero value trusts the system pool", TLSOptions{}, TLSModeSystem},
		{"whitespace path is not a CA", TLSOptions{CACertPath: "  "}, TLSModeSystem},
		{"whitespace path with skip still skips", TLSOptions{CACertPath: "  ", InsecureSkipVerify: true}, TLSModeInsecureSkipVerify},
		{"path selects custom CA", TLSOptions{CACertPath: "/tmp/ca.pem"}, TLSModeCustomCA},
		{"PEM selects custom CA", TLSOptions{CACertPEM: pemBytes}, TLSModeCustomCA},
		{"skip alone selects insecure", TLSOptions{InsecureSkipVerify: true}, TLSModeInsecureSkipVerify},
		{"CA wins over skip in the reported mode", TLSOptions{CACertPEM: pemBytes, InsecureSkipVerify: true}, TLSModeCustomCA},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.options.Mode(); got != tc.want {
				t.Errorf("Mode() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTLSOptionsConfigured(t *testing.T) {
	t.Parallel()

	if (TLSOptions{}).configured() {
		t.Error("zero TLSOptions must report unconfigured")
	}

	for _, options := range []TLSOptions{
		{CACertPath: "/tmp/ca.pem"},
		{CACertPath: "  /tmp/ca.pem  "},
		{CACertPEM: []byte("pem")},
		{InsecureSkipVerify: true},
	} {
		if !options.configured() {
			t.Errorf("%+v must report configured", options)
		}
	}

	if (TLSOptions{CACertPath: "   "}).configured() {
		t.Error("a whitespace-only path must report unconfigured")
	}
}

func TestHTTPClientWithTLSDefaultsMatchDefaultClient(t *testing.T) {
	t.Parallel()

	client, mode, err := HTTPClientWithTLS(0, TLSOptions{})
	if err != nil {
		t.Fatalf("HTTPClientWithTLS() error = %v", err)
	}

	if mode != TLSModeSystem {
		t.Errorf("mode = %q, want %q", mode, TLSModeSystem)
	}

	if client.Timeout != DefaultTimeout {
		t.Errorf("timeout = %s, want %s", client.Timeout, DefaultTimeout)
	}
}

func TestHTTPClientWithTLSRejectsCAPlusSkipVerify(t *testing.T) {
	t.Parallel()

	pemBytes := mintCACertPEM(t)

	for name, options := range map[string]TLSOptions{
		"path plus skip": {CACertPath: "/tmp/ca.pem", InsecureSkipVerify: true},
		"PEM plus skip":  {CACertPEM: pemBytes, InsecureSkipVerify: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client, mode, err := HTTPClientWithTLS(time.Second, options)
			if err == nil || client != nil || mode != "" {
				t.Fatalf("HTTPClientWithTLS() = (%v, %q, %v), want a rejection", client, mode, err)
			}
		})
	}
}

func TestHTTPClientWithTLSInsecureSkipVerify(t *testing.T) {
	t.Parallel()

	client, mode, err := HTTPClientWithTLS(5*time.Second, TLSOptions{InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("HTTPClientWithTLS() error = %v", err)
	}

	if mode != TLSModeInsecureSkipVerify {
		t.Errorf("mode = %q, want %q", mode, TLSModeInsecureSkipVerify)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", client.Transport)
	}

	if transport.TLSClientConfig == nil || !transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("skip-verify client must carry InsecureSkipVerify in its TLS config")
	}
}

func TestHTTPClientWithTLSCustomCAPEM(t *testing.T) {
	t.Parallel()

	pemBytes := mintCACertPEM(t)

	client, mode, err := HTTPClientWithTLS(5*time.Second, TLSOptions{CACertPEM: pemBytes})
	if err != nil {
		t.Fatalf("HTTPClientWithTLS() error = %v", err)
	}

	if mode != TLSModeCustomCA {
		t.Errorf("mode = %q, want %q", mode, TLSModeCustomCA)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", client.Transport)
	}

	if transport.TLSClientConfig == nil || transport.TLSClientConfig.RootCAs == nil {
		t.Fatal("custom-CA client must carry a root pool in its TLS config")
	}
}

func TestHTTPClientWithTLSCustomCATrustsATestServer(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(server.Close)

	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})

	client, mode, err := HTTPClientWithTLS(5*time.Second, TLSOptions{CACertPEM: caPEM})
	if err != nil {
		t.Fatalf("HTTPClientWithTLS() error = %v", err)
	}

	if mode != TLSModeCustomCA {
		t.Errorf("mode = %q, want %q", mode, TLSModeCustomCA)
	}

	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}

	var out struct {
		OK bool `json:"ok"`
	}
	if err := DoJSON(t.Context(), JSONRequest{Provider: "oci", BaseURL: base, Endpoint: "/", Client: client, Out: &out}); err != nil {
		t.Fatalf("DoJSON over custom-CA TLS: %v", err)
	}

	if !out.OK {
		t.Fatal("expected the test server payload to decode")
	}
}

func TestHTTPClientWithTLSCACertPath(t *testing.T) {
	t.Parallel()

	pemBytes := mintCACertPEM(t)

	bundle := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(bundle, pemBytes, 0o600); err != nil {
		t.Fatalf("write test CA bundle: %v", err)
	}

	client, mode, err := HTTPClientWithTLS(0, TLSOptions{CACertPath: " " + bundle + " "})
	if err != nil {
		t.Fatalf("HTTPClientWithTLS() error = %v", err)
	}

	if mode != TLSModeCustomCA {
		t.Errorf("mode = %q, want %q", mode, TLSModeCustomCA)
	}

	if client.Timeout != DefaultTimeout {
		t.Errorf("timeout = %s, want fallback %s", client.Timeout, DefaultTimeout)
	}
}

func TestHTTPClientWithTLSPathFailures(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "absent.pem")
	if _, _, err := HTTPClientWithTLS(time.Second, TLSOptions{CACertPath: missing}); err == nil {
		t.Error("a missing CA bundle path must fail")
	}

	empty := filepath.Join(t.TempDir(), "empty.pem")
	if err := os.WriteFile(empty, []byte("not a certificate"), 0o600); err != nil {
		t.Fatalf("write bad CA bundle: %v", err)
	}

	if _, _, err := HTTPClientWithTLS(time.Second, TLSOptions{CACertPath: empty}); err == nil {
		t.Error("a bundle with no usable certificate must fail")
	}

	if _, _, err := HTTPClientWithTLS(time.Second, TLSOptions{CACertPEM: []byte("not a certificate")}); err == nil {
		t.Error("in-memory PEM with no usable certificate must fail")
	}
}

func TestAppendCACertRejectsEmptyOptions(t *testing.T) {
	t.Parallel()

	pool := x509.NewCertPool()
	if err := appendCACert(pool, TLSOptions{}); err == nil {
		t.Error("empty TLS options must fail appendCACert")
	}
}

func TestAppendCACertAcceptsBothSources(t *testing.T) {
	t.Parallel()

	pemBytes := mintCACertPEM(t)

	bundle := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(bundle, pemBytes, 0o600); err != nil {
		t.Fatalf("write test CA bundle: %v", err)
	}

	if err := appendCACert(x509.NewCertPool(), TLSOptions{CACertPath: bundle, CACertPEM: pemBytes}); err != nil {
		t.Fatalf("appendCACert() error = %v", err)
	}
}

func TestSystemCertPoolReturnsUsablePool(t *testing.T) {
	t.Parallel()

	pool, err := systemCertPool()
	if err != nil {
		t.Fatalf("systemCertPool() error = %v", err)
	}

	if pool == nil {
		t.Fatal("systemCertPool() must never return a nil pool")
	}
}

func TestTLSClientConfigDisablesOldTLS(t *testing.T) {
	t.Parallel()

	client, _, err := HTTPClientWithTLS(time.Second, TLSOptions{InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("HTTPClientWithTLS() error = %v", err)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", client.Transport)
	}

	if transport.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %x, want TLS 1.2", transport.TLSClientConfig.MinVersion)
	}
}
