// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ecr

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecr/types"
)

// tokenOutput builds an AuthorizationToken output minting the given password
// against the given proxy endpoint.
func tokenOutput(t *testing.T, password, proxyEndpoint string) *ecr.GetAuthorizationTokenOutput {
	t.Helper()
	return &ecr.GetAuthorizationTokenOutput{
		AuthorizationData: []types.AuthorizationData{{
			AuthorizationToken: awsv2.String(base64.StdEncoding.EncodeToString([]byte("AWS:" + password))),
			ProxyEndpoint:      awsv2.String(proxyEndpoint),
		}},
	}
}

// TestNewReferrerClientPropagatesTokenExchangeError covers the credential
// error return when the token exchange fails.
func TestNewReferrerClientPropagatesTokenExchangeError(t *testing.T) {
	t.Parallel()

	_, err := NewReferrerClient(context.Background(), ReferrerClientOptions{
		AuthorizationClient: &fakeAuthorizationTokenAPI{
			t:   t,
			err: errors.New("ecr is unreachable"),
		},
		RegistryHost: "123456789012.dkr.ecr.us-east-1.amazonaws.com",
	})
	if err == nil {
		t.Fatal("NewReferrerClient() error = nil, want token exchange failure")
	}
}

// TestNewReferrerClientRequiresRegistryHost covers the missing-host return
// when neither an explicit host nor a proxy endpoint seeds the registry.
func TestNewReferrerClientRequiresRegistryHost(t *testing.T) {
	t.Parallel()

	_, err := NewReferrerClient(context.Background(), ReferrerClientOptions{
		AuthorizationClient: &fakeAuthorizationTokenAPI{
			t:      t,
			output: tokenOutput(t, "fake-password", ""),
		},
	})
	if err == nil {
		t.Fatal("NewReferrerClient() error = nil, want missing registry host")
	}
}

// TestNewReferrerClientRejectsUnusableRegistryHost covers the base-URL
// error return when the configured host cannot become a Distribution URL.
func TestNewReferrerClientRejectsUnusableRegistryHost(t *testing.T) {
	t.Parallel()

	_, err := NewReferrerClient(context.Background(), ReferrerClientOptions{
		AuthorizationClient: &fakeAuthorizationTokenAPI{
			t:      t,
			output: tokenOutput(t, "fake-password", "https://123456789012.dkr.ecr.us-east-1.amazonaws.com"),
		},
		RegistryHost: "nota url with spaces",
	})
	if err == nil {
		t.Fatal("NewReferrerClient() error = nil, want unusable registry host")
	}
}

// TestNewReferrerClientResolvesBareHostThroughDistributionBaseURL proves a
// schemeless registry host (the production ECR shape) resolves through the
// shared DistributionBaseURL helper into a usable client.
func TestNewReferrerClientResolvesBareHostThroughDistributionBaseURL(t *testing.T) {
	t.Parallel()

	client, err := NewReferrerClient(context.Background(), ReferrerClientOptions{
		AuthorizationClient: &fakeAuthorizationTokenAPI{
			t:      t,
			output: tokenOutput(t, "fake-password", "https://123456789012.dkr.ecr.us-east-1.amazonaws.com"),
		},
		RegistryHost: "123456789012.dkr.ecr.us-east-1.amazonaws.com",
	})
	if err != nil {
		t.Fatalf("NewReferrerClient() error = %v", err)
	}
	if client == nil {
		t.Fatal("NewReferrerClient() = nil")
	}
}
