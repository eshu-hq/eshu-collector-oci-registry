// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oci

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestURLAuthority(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		value          string
		wantRewritten  bool
		wantUser       bool
		wantErr        bool
		wantErrLeak    string
		wantErrMessage string
	}{
		"plain https URL": {
			value:         "https://example.com/tool",
			wantRewritten: false,
		},
		"opaque body shaped like an authority": {
			value:         "svc:PASSWORD@h.internal:5432/tool",
			wantRewritten: true,
			wantUser:      true,
		},
		"opaque tool path without credentials": {
			value:         "mcp:tool/name",
			wantRewritten: false,
		},
		"purl without credentials": {
			value:         "pkg:npm/lodash@4.17.21",
			wantRewritten: false,
		},
		"unparseable value": {
			value:          "http://exa mple.com",
			wantRewritten:  false,
			wantErr:        true,
			wantErrMessage: "parse value: parse: invalid character in host name",
		},
		"authority re-parse failure": {
			value:         "svc:PASS@wo rd/x",
			wantRewritten: true,
			wantErr:       true,
			wantErrLeak:   "wo rd",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			parsed, rewritten, err := urlAuthority(tc.value)
			if rewritten != tc.wantRewritten {
				t.Fatalf("urlAuthority(%q) rewritten = %v, want %v", tc.value, rewritten, tc.wantRewritten)
			}
			if tc.wantErr {
				if err == nil {
					t.Fatalf("urlAuthority(%q) error = nil, want failure", tc.value)
				}
				if tc.wantErrLeak != "" && strings.Contains(err.Error(), tc.wantErrLeak) {
					t.Fatalf("urlAuthority(%q) error = %q, leaks input", tc.value, err)
				}
				if tc.wantErrMessage != "" && err.Error() != tc.wantErrMessage {
					t.Fatalf("urlAuthority(%q) error = %q, want %q", tc.value, err, tc.wantErrMessage)
				}
				return
			}
			if err != nil {
				t.Fatalf("urlAuthority(%q) error = %v", tc.value, err)
			}
			if got := parsed.User != nil; got != tc.wantUser {
				t.Fatalf("urlAuthority(%q) userinfo = %v, want %v", tc.value, got, tc.wantUser)
			}
		})
	}
}

func TestCarriesUserinfo(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		value string
		want  bool
	}{
		"direct userinfo":                {"https://user:pass@example.com/x", true},
		"no userinfo":                    {"https://example.com/x", false},
		"opaque authority with password": {"svc:PASSWORD@h.internal:5432/tool", true},
		"mailto looks like a credential": {"mailto:user@example.com", true},
		"unparseable fails closed":       {"http://exa mple.com", true},
		"opaque tool path":               {"mcp:tool/name", false},
		"purl":                           {"pkg:npm/lodash@4.17.21", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := carriesUserinfo(tc.value); got != tc.want {
				t.Fatalf("carriesUserinfo(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestOpaqueHasAuthority(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		opaque string
		want   bool
	}{
		"empty":                    {"", false},
		"password ahead of slash":  {"PASSWORD@h.internal:5432/tool", true},
		"tool path":                {"tool/name", false},
		"purl at after slash only": {"npm/lodash@4.17.21", false},
		"bare credential":          {"user:pass@host", true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := opaqueHasAuthority(tc.opaque); got != tc.want {
				t.Fatalf("opaqueHasAuthority(%q) = %v, want %v", tc.opaque, got, tc.want)
			}
		})
	}
}

func TestErrWithoutValue(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("boom")
	if got := errWithoutValue(sentinel); got != sentinel {
		t.Fatalf("errWithoutValue(non-URL error) = %v, want the error unchanged", got)
	}

	// The nested invalid-port message quotes the value; the rebuilt error
	// must keep the operation and reason but drop the quoted input. The
	// invalid URL is concatenated so the static checker does not flag the
	// intentionally unparseable literal.
	leakyPort := strings.Join([]string{"http://example.com", ":secretnotaport/x"}, "")
	_, parseErr := url.Parse(leakyPort)
	if parseErr == nil {
		t.Fatal("url.Parse unexpectedly accepted an invalid port")
	}
	got := errWithoutValue(parseErr)
	if got.Error() != "parse: invalid port after host" {
		t.Fatalf("errWithoutValue() = %q, want %q", got, "parse: invalid port after host")
	}
}

func TestParseErrorReason(t *testing.T) {
	t.Parallel()

	// Concatenated so the static checker does not flag the intentionally
	// unparseable literal.
	unparseable := strings.Join([]string{"http://exa", " mple.com"}, "")
	_, envelope := url.Parse(unparseable)
	cases := map[string]struct {
		err  error
		want string
	}{
		"whole url.Error envelope":    {envelope, "invalid character in host name"},
		"escape error":                {url.EscapeError("%"), "invalid URL escape"},
		"invalid host error":          {url.InvalidHostError("bad host!"), "invalid character in host name"},
		"missing protocol scheme":     {errors.New("missing protocol scheme"), "missing protocol scheme"},
		"empty url":                   {errors.New("empty url"), "empty url"},
		"invalid URI for request":     {errors.New("invalid URI for request"), "invalid URI for request"},
		"colon in first segment":      {errors.New("first path segment in URL cannot contain colon"), "first path segment in URL cannot contain colon"},
		"control character":           {errors.New("net/url: invalid control character in URL"), "net/url: invalid control character in URL"},
		"invalid userinfo":            {errors.New("net/url: invalid userinfo"), "net/url: invalid userinfo"},
		"invalid IP-literal":          {errors.New("invalid IP-literal"), "invalid IP-literal"},
		"missing bracket in host":     {errors.New("missing ']' in host"), "missing ']' in host"},
		"invalid port with input":     {errors.New(`invalid port ":secret" after host`), "invalid port after host"},
		"invalid host with input":     {errors.New("invalid host: zone secret"), "invalid host"},
		"future message fails closed": {errors.New("some new Go release wording"), "malformed URL"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := parseErrorReason(tc.err); got != tc.want {
				t.Fatalf("parseErrorReason() = %q, want %q", got, tc.want)
			}
		})
	}
}
