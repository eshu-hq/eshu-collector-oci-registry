// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"fmt"
	"net/http"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awsecr "github.com/aws/aws-sdk-go-v2/service/ecr"

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

type providerFactory struct{}

func (providerFactory) Client(ctx context.Context, target runtime.TargetConfig) (runtime.RegistryClient, error) {
	// Resolve the per-target HTTP client once so every provider honors any
	// configured custom-CA trust (for example a localhost registry:2 over TLS).
	// The resolved tls_mode is a low-cardinality value safe to surface to an
	// operator; a misconfigured CA bundle fails loudly here rather than silently
	// falling back to the system trust pool.
	httpClient, _, err := target.HTTPClient()
	if err != nil {
		return nil, fmt.Errorf("resolve OCI registry TLS client (tls_mode=%s): %w", target.TLSMode(), err)
	}
	switch target.Provider {
	case oci.ProviderDockerHub:
		return dockerhub.NewDistributionClient(ctx, dockerhub.Config{
			Repository: target.Repository,
			Username:   target.Username,
			Password:   target.Password,
			Client:     httpClient,
		})
	case oci.ProviderGHCR:
		return ghcr.NewDistributionClient(ctx, ghcr.Config{
			Repository: target.Repository,
			Username:   target.Username,
			Password:   target.Password,
			Client:     httpClient,
		})
	case oci.ProviderJFrog:
		return jfrog.NewDistributionClient(jfrog.Config{
			BaseURL:       target.BaseURL,
			RepositoryKey: target.RepositoryKey,
			Username:      target.Username,
			Password:      target.Password,
			BearerToken:   target.BearerToken,
			Client:        httpClient,
		})
	case oci.ProviderECR:
		return newECRDistributionClient(ctx, target, httpClient)
	case oci.ProviderHarbor:
		return harbor.NewDistributionClient(harbor.Config{
			BaseURL:     target.Registry,
			Repository:  target.Repository,
			Username:    target.Username,
			Password:    target.Password,
			BearerToken: target.BearerToken,
			Client:      httpClient,
		})
	case oci.ProviderGoogleArtifactRegistry:
		return gar.NewDistributionClient(gar.Config{
			RegistryHost: target.Registry,
			Repository:   target.Repository,
			Username:     target.Username,
			Password:     target.Password,
			BearerToken:  target.BearerToken,
			Client:       httpClient,
		})
	case oci.ProviderAzureContainerRegistry:
		return acr.NewDistributionClient(acr.Config{
			RegistryHost: target.Registry,
			Repository:   target.Repository,
			Username:     target.Username,
			Password:     target.Password,
			BearerToken:  target.BearerToken,
			Client:       httpClient,
		})
	default:
		return nil, fmt.Errorf("unsupported OCI registry provider %q", target.Provider)
	}
}

func newECRDistributionClient(ctx context.Context, target runtime.TargetConfig, httpClient *http.Client) (runtime.RegistryClient, error) {
	options := make([]func(*awsconfig.LoadOptions) error, 0, 2)
	if target.Region != "" {
		options = append(options, awsconfig.WithRegion(target.Region))
	}
	if target.AWSProfile != "" {
		options = append(options, awsconfig.WithSharedConfigProfile(target.AWSProfile))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("load AWS config for ECR: %w", err)
	}
	credentials, err := ecr.GetDistributionCredentials(ctx, awsecr.NewFromConfig(cfg))
	if err != nil {
		return nil, err
	}
	registryHost := target.RegistryHost
	if registryHost == "" {
		registryHost = target.Registry
	}
	return ecr.NewDistributionClient(registryHost, credentials.Username, credentials.Password, httpClient)
}
