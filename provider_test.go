// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu-collector-oci-registry/oci"
	"github.com/eshu-hq/eshu-collector-oci-registry/oci/runtime"
)

// fakeECR serves the minimum ECR GetAuthorizationToken JSON shape the AWS SDK
// parses, with the given HTTP status.
func fakeECR(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Amz-Target") != "AmazonEC2ContainerRegistry_V20150921.GetAuthorizationToken" {
			t.Errorf("X-Amz-Target = %q, want the GetAuthorizationToken operation", r.Header.Get("X-Amz-Target"))
		}
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

// pinAWSEnv isolates the AWS SDK from ambient credentials and config
// so LoadDefaultConfig resolves exactly the knobs the target under test sets.
func pinAWSEnv(t *testing.T, endpoint string) {
	t.Helper()
	empty, err := os.CreateTemp(t.TempDir(), "aws-empty-*")
	if err != nil {
		t.Fatal(err)
	}
	if err := empty.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_ACCESS_KEY_ID", "test-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_CONFIG_FILE", empty.Name())
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", empty.Name())
	if endpoint == "" {
		t.Setenv("AWS_ENDPOINT_URL_ECR", "http://127.0.0.1:1")
	} else {
		t.Setenv("AWS_ENDPOINT_URL_ECR", endpoint)
	}
	for _, key := range []string{"AWS_PROFILE", "AWS_DEFAULT_PROFILE"} {
		if value, ok := os.LookupEnv(key); ok {
			if err := os.Unsetenv(key); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = os.Setenv(key, value)
			})
		}
	}
}

func ecrToken(t *testing.T) string {
	t.Helper()
	return base64.StdEncoding.EncodeToString([]byte("AWS:test-secret"))
}

func ecrSuccessBody(t *testing.T) string {
	t.Helper()
	return `{"authorizationData":[{"authorizationToken":"` + ecrToken(t) + `","proxyEndpoint":"https://123456789012.dkr.ecr.us-east-1.amazonaws.com"}]}`
}

func TestProviderFactoryClientDispatch(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		target  runtime.TargetConfig
		wantErr string
	}{
		"jfrog": {
			target: runtime.TargetConfig{
				Provider:      oci.ProviderJFrog,
				Registry:      "jfrog.example.com",
				BaseURL:       "https://jfrog.example.com",
				RepositoryKey: "demo",
				Repository:    "team/api",
			},
		},
		"harbor": {
			target: runtime.TargetConfig{
				Provider:   oci.ProviderHarbor,
				Registry:   "harbor.example.com",
				Repository: "project/image",
			},
		},
		"gar": {
			target: runtime.TargetConfig{
				Provider:   oci.ProviderGoogleArtifactRegistry,
				Registry:   "us-docker.pkg.dev",
				Repository: "project/repository/image",
			},
		},
		"acr": {
			target: runtime.TargetConfig{
				Provider:   oci.ProviderAzureContainerRegistry,
				Registry:   "myregistry.azurecr.io",
				Repository: "samples/image",
			},
		},
		// Docker Hub and GHCR mint a pull token over the network, so these
		// cases use repository names the adapters reject before any dial.
		// The dispatch branch still executes either way.
		"dockerhub invalid repository": {
			target:  runtime.TargetConfig{Provider: oci.ProviderDockerHub},
			wantErr: "dockerhub repository is required",
		},
		"ghcr invalid repository": {
			target:  runtime.TargetConfig{Provider: oci.ProviderGHCR, Repository: "single"},
			wantErr: "ghcr repository must include owner and image name",
		},
		"unsupported provider": {
			target:  runtime.TargetConfig{Provider: "example"},
			wantErr: `unsupported OCI registry provider "example"`,
		},
		"tls client failure": {
			target: runtime.TargetConfig{
				Provider:      oci.ProviderJFrog,
				BaseURL:       "https://jfrog.example.com",
				RepositoryKey: "demo",
				Repository:    "team/api",
				TLS:           runtime.TLSConfig{CACertPath: filepath.Join(t.TempDir(), "missing-ca.pem")},
			},
			wantErr: "resolve OCI registry TLS client",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client, err := providerFactory{}.Client(context.Background(), tc.target)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Client() error = %v", err)
				}
				if client == nil {
					t.Fatal("Client() = nil, want a registry client")
				}
				return
			}
			if err == nil {
				t.Fatalf("Client() = %+v, want error containing %q", client, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Client() error = %q, want substring %q", err, tc.wantErr)
			}
			// Adapters return a typed-nil client alongside the error, so
			// only the error carries the assertion here.
			_ = client
		})
	}
}

func TestProviderFactoryClientECR(t *testing.T) {
	server := fakeECR(t, http.StatusOK, ecrSuccessBody(t))
	pinAWSEnv(t, server.URL)

	client, err := providerFactory{}.Client(context.Background(), runtime.TargetConfig{
		Provider:     oci.ProviderECR,
		Registry:     "123456789012.dkr.ecr.us-east-1.amazonaws.com",
		RegistryHost: "123456789012.dkr.ecr.us-east-1.amazonaws.com",
		Repository:   "team/api",
	})
	if err != nil {
		t.Fatalf("Client() error = %v", err)
	}
	if client == nil {
		t.Fatal("Client() = nil, want a registry client")
	}
}

func TestNewECRDistributionClientUsesExplicitRegistryHost(t *testing.T) {
	server := fakeECR(t, http.StatusOK, ecrSuccessBody(t))
	pinAWSEnv(t, server.URL)

	client, err := newECRDistributionClient(context.Background(), runtime.TargetConfig{
		Region:       "us-east-1",
		Registry:     "fallback.example.com",
		RegistryHost: "123456789012.dkr.ecr.us-east-1.amazonaws.com",
	}, nil)
	if err != nil {
		t.Fatalf("newECRDistributionClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("newECRDistributionClient() = nil, want a registry client")
	}
}

func TestNewECRDistributionClientFallsBackToRegistry(t *testing.T) {
	server := fakeECR(t, http.StatusOK, ecrSuccessBody(t))
	pinAWSEnv(t, server.URL)

	// A shared-config profile carrying a region and static credentials
	// exercises the profile option while the empty RegistryHost exercises
	// the Registry fallback. Selecting a profile drops environment
	// credentials from the chain, so the profile needs its own.
	configFile := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(configFile, []byte("[profile test]\nregion = eu-west-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AWS_CONFIG_FILE", configFile)
	credentialsFile := filepath.Join(t.TempDir(), "credentials")
	if err := os.WriteFile(credentialsFile, []byte("[test]\naws_access_key_id = test-key\naws_secret_access_key = test-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credentialsFile)

	client, err := newECRDistributionClient(context.Background(), runtime.TargetConfig{
		AWSProfile: "test",
		Registry:   "123456789012.dkr.ecr.us-east-1.amazonaws.com",
	}, nil)
	if err != nil {
		t.Fatalf("newECRDistributionClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("newECRDistributionClient() = nil, want a registry client")
	}
}

func TestNewECRDistributionClientReportsAuthorizationFailure(t *testing.T) {
	server := fakeECR(t, http.StatusBadRequest, `{"message":"denied"}`)
	pinAWSEnv(t, server.URL)

	client, err := newECRDistributionClient(context.Background(), runtime.TargetConfig{
		Region:       "us-east-1",
		RegistryHost: "123456789012.dkr.ecr.us-east-1.amazonaws.com",
	}, nil)
	if err == nil {
		t.Fatalf("newECRDistributionClient() = %+v, want an authorization error", client)
	}
	if !strings.Contains(err.Error(), "get ecr authorization token") {
		t.Fatalf("newECRDistributionClient() error = %q, want an authorization failure", err)
	}
}

func TestNewECRDistributionClientReportsConfigFailure(t *testing.T) {
	pinAWSEnv(t, "")
	// A directory where the SDK expects an INI file fails shared-config
	// loading deterministically.
	t.Setenv("AWS_CONFIG_FILE", t.TempDir())

	client, err := newECRDistributionClient(context.Background(), runtime.TargetConfig{
		Region:       "us-east-1",
		RegistryHost: "123456789012.dkr.ecr.us-east-1.amazonaws.com",
	}, nil)
	if err == nil {
		t.Fatalf("newECRDistributionClient() = %+v, want a config-load error", client)
	}
	if !strings.Contains(err.Error(), "load AWS config for ECR") {
		t.Fatalf("newECRDistributionClient() error = %q, want a config-load failure", err)
	}
}
