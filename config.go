// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu-collector-oci-registry/oci"
	"github.com/eshu-hq/eshu-collector-oci-registry/oci/acr"
	"github.com/eshu-hq/eshu-collector-oci-registry/oci/dockerhub"
	"github.com/eshu-hq/eshu-collector-oci-registry/oci/ecr"
	"github.com/eshu-hq/eshu-collector-oci-registry/oci/gar"
	"github.com/eshu-hq/eshu-collector-oci-registry/oci/ghcr"
	"github.com/eshu-hq/eshu-collector-oci-registry/oci/harbor"
	"github.com/eshu-hq/eshu-collector-oci-registry/oci/jfrog"
	"github.com/eshu-hq/eshu-collector-oci-registry/oci/runtime"
)

// targetJSON is one repository target in the activation config. It keeps the
// in-tree wire shape (ESHU_OCI_REGISTRY_TARGETS_JSON entries) so activations
// move unchanged: secrets travel by environment reference, never inline.
type targetJSON struct {
	Provider       string   `json:"provider"`
	Registry       string   `json:"registry"`
	BaseURL        string   `json:"base_url"`
	RepositoryKey  string   `json:"repository_key"`
	Repository     string   `json:"repository"`
	Region         string   `json:"region"`
	RegistryID     string   `json:"registry_id"`
	RegistryHost   string   `json:"registry_host"`
	References     []string `json:"references"`
	TagLimit       int      `json:"tag_limit"`
	Visibility     string   `json:"visibility"`
	AuthMode       string   `json:"auth_mode"`
	SourceURI      string   `json:"source_uri"`
	UsernameEnv    string   `json:"username_env"`
	PasswordEnv    string   `json:"password_env"`
	BearerTokenEnv string   `json:"bearer_token_env"`
	AWSProfile     string   `json:"aws_profile"`
	FencingToken   int64    `json:"fencing_token"`
	// AllowEmptySuccessor is the explicit operator attestation that an
	// empty or sharply shrunken successor generation is legitimate for
	// this repository. It defaults to false.
	AllowEmptySuccessor bool `json:"allow_empty_successor"`

	// Limits bounds one claim's emission for this target. It uses the
	// snake_case keys of the enforcement bounds; every non-positive field
	// falls back to the handler value, then the built-in default.
	Limits ResourceUse `json:"limits"`

	// TLSCACertPath points at a PEM bundle the collector trusts in addition to
	// the system pool. Use it to scan a registry served with a private or
	// self-signed CA, such as a local registry:2 over TLS, without weakening
	// trust globally.
	TLSCACertPath string `json:"tls_ca_cert_path"`
	// TLSInsecureSkipVerify disables certificate verification for this target.
	// It is test/local-only, never a production default, and is rejected when a
	// CA bundle is also supplied.
	TLSInsecureSkipVerify bool `json:"tls_insecure_skip_verify"`
}

// resolveTarget decodes the activation config and returns the repository
// target whose scope ID matches the claim, plus that target's limits. The
// config is either one target object or {"targets":[...]}; every target
// requires a scope match, including a lone target, so a mismatched lone
// target is rejected at config decode instead of at the ScanClaim match.
// Secrets resolve from the process environment.
func resolveTarget(raw map[string]any, scopeID string, getenv func(string) string) (runtime.TargetConfig, ResourceUse, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	targets, err := decodeTargets(raw)
	if err != nil {
		return runtime.TargetConfig{}, ResourceUse{}, err
	}
	if len(targets) == 0 {
		return runtime.TargetConfig{}, ResourceUse{}, fmt.Errorf("collector configuration requires at least one target")
	}

	for i, target := range targets {
		mapped, err := mapTarget(target, getenv)
		if err != nil {
			return runtime.TargetConfig{}, ResourceUse{}, fmt.Errorf("targets[%d]: %w", i, err)
		}
		scope, err := scopeIDFor(mapped)
		if err != nil {
			return runtime.TargetConfig{}, ResourceUse{}, fmt.Errorf("targets[%d]: %w", i, err)
		}
		if scope == scopeID {
			return mapped, target.Limits, nil
		}
	}
	return runtime.TargetConfig{}, ResourceUse{}, fmt.Errorf("no configured target matches scope_id %q", scopeID)
}

func decodeTargets(raw map[string]any) ([]targetJSON, error) {
	if raw == nil {
		return nil, fmt.Errorf("collector configuration is required")
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("decode collector configuration: %w", err)
	}
	// Unknown keys fail closed: the rejection never names the key, because an
	// operator key can be a pasted credential.
	decode := func(encoded []byte, target any) error {
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.DisallowUnknownFields()
		return decoder.Decode(target)
	}
	if _, ok := raw["targets"]; ok {
		var decoded struct {
			Targets []targetJSON `json:"targets"`
		}
		if err := decode(encoded, &decoded); err != nil {
			return nil, fmt.Errorf("decode collector configuration targets: %s", decodeReason(err))
		}
		return decoded.Targets, nil
	}
	var single targetJSON
	if err := decode(encoded, &single); err != nil {
		return nil, fmt.Errorf("decode collector configuration: %s", decodeReason(err))
	}
	return []targetJSON{single}, nil
}

// envNamePattern bounds credential references to plain environment variable
// names. The rejection names the config key and never quotes what was
// configured, so a credential pasted into one of them cannot be echoed.
var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// envValue resolves a credential reference to its process value. A blank
// reference stays blank; anything else must be a plain environment variable
// name.
func envValue(getenv func(string) string, name, key string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil
	}
	if len(name) > 128 || !envNamePattern.MatchString(name) {
		return "", fmt.Errorf("invalid environment variable name for %s", key)
	}
	return getenv(name), nil
}

func scopeIDFor(target runtime.TargetConfig) (string, error) {
	return target.ScopeID()
}

// fencingToken parses the claim fencing token. Empty stays zero; anything
// else must be an integer, because the scan stamps it on every fact boundary.
func fencingToken(raw string) (int64, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, nil
	}
	token, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("claim fencing_token %q is not an integer", raw)
	}
	return token, nil
}

func mapTarget(target targetJSON, getenv func(string) string) (runtime.TargetConfig, error) {
	provider := oci.Provider(strings.TrimSpace(target.Provider))
	repository := strings.TrimSpace(target.Repository)
	registry := strings.TrimSpace(firstNonBlank(target.Registry, target.RegistryHost))
	switch provider {
	case oci.ProviderDockerHub:
		name, err := dockerhub.RepositoryName(repository)
		if err != nil {
			return runtime.TargetConfig{}, err
		}
		repository = name
		if registry == "" {
			registry = dockerhub.RegistryHost
		}
	case oci.ProviderGHCR:
		name, err := ghcr.RepositoryName(repository)
		if err != nil {
			return runtime.TargetConfig{}, err
		}
		repository = name
		if registry == "" {
			registry = ghcr.RegistryHost
		}
	case oci.ProviderJFrog:
		identity, err := jfrog.RepositoryIdentity(target.BaseURL, target.RepositoryKey, repository)
		if err != nil {
			return runtime.TargetConfig{}, err
		}
		registry = identity.Registry
		repository = identity.Repository
	case oci.ProviderECR:
		if registry == "" {
			host, err := ecr.PrivateRegistryHost(target.RegistryID, target.Region)
			if err != nil {
				return runtime.TargetConfig{}, err
			}
			registry = host
		}
	case oci.ProviderHarbor:
		identity, err := harbor.RepositoryIdentity(target.BaseURL, repository)
		if err != nil {
			return runtime.TargetConfig{}, err
		}
		registry = identity.Registry
		repository = identity.Repository
	case oci.ProviderGoogleArtifactRegistry:
		host := firstNonBlank(target.RegistryHost, registry)
		identity, err := gar.RepositoryIdentity(host, repository)
		if err != nil {
			return runtime.TargetConfig{}, err
		}
		registry = identity.Registry
		repository = identity.Repository
	case oci.ProviderAzureContainerRegistry:
		host := firstNonBlank(target.RegistryHost, registry)
		identity, err := acr.RepositoryIdentity(host, repository)
		if err != nil {
			return runtime.TargetConfig{}, err
		}
		registry = identity.Registry
		repository = identity.Repository
	default:
		return runtime.TargetConfig{}, fmt.Errorf("unsupported provider %q", target.Provider)
	}
	// The host validator requires a source URI on every fact; fail fast on a
	// blank one instead of emitting facts the host must reject.
	if strings.TrimSpace(target.SourceURI) == "" {
		return runtime.TargetConfig{}, fmt.Errorf("source_uri is required")
	}
	username, err := envValue(getenv, target.UsernameEnv, "username_env")
	if err != nil {
		return runtime.TargetConfig{}, err
	}
	password, err := envValue(getenv, target.PasswordEnv, "password_env")
	if err != nil {
		return runtime.TargetConfig{}, err
	}
	bearerToken, err := envValue(getenv, target.BearerTokenEnv, "bearer_token_env")
	if err != nil {
		return runtime.TargetConfig{}, err
	}
	return runtime.TargetConfig{
		Provider:      provider,
		Registry:      registry,
		BaseURL:       strings.TrimSpace(target.BaseURL),
		RepositoryKey: strings.TrimSpace(target.RepositoryKey),
		Repository:    repository,
		Region:        strings.TrimSpace(target.Region),
		RegistryID:    strings.TrimSpace(target.RegistryID),
		RegistryHost:  registry,
		References:    target.References,
		TagLimit:      target.TagLimit,
		Visibility:    oci.Visibility(strings.TrimSpace(target.Visibility)),
		AuthMode:      oci.AuthMode(strings.TrimSpace(target.AuthMode)),
		SourceURI:     strings.TrimSpace(target.SourceURI),
		Username:      username,
		Password:      password,
		BearerToken:   bearerToken,
		AWSProfile:    strings.TrimSpace(target.AWSProfile),
		FencingToken:  target.FencingToken,
		// Only an explicitly attested repository bypasses the ingestion
		// store's successor-emptiness backstop.
		AllowEmptySuccessor: target.AllowEmptySuccessor,
		TLS: runtime.TLSConfig{
			CACertPath:         strings.TrimSpace(target.TLSCACertPath),
			InsecureSkipVerify: target.TLSInsecureSkipVerify,
		},
	}, nil
}

// decodeReason keeps a JSON decode failure to its reason without echoing
// decoded values. An unknown key is reported without its name: the key comes
// from the operator or the host and can be a pasted credential, and the
// standard library's message would quote it.
func decodeReason(err error) string {
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.As(err, &typeErr):
		return "field " + typeErr.Field + " has the wrong type"
	case strings.HasPrefix(err.Error(), "json: unknown field"):
		return "unknown field"
	default:
		return "malformed JSON"
	}
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
