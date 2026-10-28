// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ecr

import (
	"context"
	"errors"
	"strings"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecr/types"
)

// TestGetDistributionCredentialsRequiresClient covers the nil-client guard.
func TestGetDistributionCredentialsRequiresClient(t *testing.T) {
	t.Parallel()

	if _, err := GetDistributionCredentials(context.Background(), nil); err == nil {
		t.Fatal("GetDistributionCredentials(nil) error = nil")
	}
}

// TestGetDistributionCredentialsWrapsTokenAPIError proves an ECR API
// failure surfaces without leaking request details.
func TestGetDistributionCredentialsWrapsTokenAPIError(t *testing.T) {
	t.Parallel()

	_, err := GetDistributionCredentials(context.Background(), &fakeAuthorizationTokenAPI{
		t:   t,
		err: errors.New("POST https://api.ecr.us-east-1.amazonaws.com secret account details"),
	})
	if err == nil {
		t.Fatal("GetDistributionCredentials() error = nil, want token API failure")
	}
	for _, leaked := range []string{"secret account", "api.ecr.us-east-1.amazonaws.com"} {
		if strings.Contains(err.Error(), leaked) {
			t.Fatalf("token API failure leaked %q: %v", leaked, err)
		}
	}
}

// TestGetDistributionCredentialsPreservesContextCancellation proves a
// cancelled token exchange keeps its context identity through the wrap.
func TestGetDistributionCredentialsPreservesContextCancellation(t *testing.T) {
	t.Parallel()

	_, err := GetDistributionCredentials(context.Background(), &fakeAuthorizationTokenAPI{
		t:   t,
		err: context.Canceled,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("GetDistributionCredentials() error = %v, want context.Canceled", err)
	}
}

// TestGetDistributionCredentialsRejectsUndecodableToken covers the token
// decode error return when ECR hands back a malformed authorization token.
func TestGetDistributionCredentialsRejectsUndecodableToken(t *testing.T) {
	t.Parallel()

	_, err := GetDistributionCredentials(context.Background(), &fakeAuthorizationTokenAPI{
		t: t,
		output: &ecr.GetAuthorizationTokenOutput{
			AuthorizationData: []types.AuthorizationData{{
				AuthorizationToken: awsv2.String("not-base64!!!"),
				ProxyEndpoint:      awsv2.String("https://123456789012.dkr.ecr.us-east-1.amazonaws.com"),
			}},
		},
	})
	if err == nil {
		t.Fatal("GetDistributionCredentials() error = nil, want decode failure")
	}
}

// TestSafeAuthorizationErrorNilStaysNil covers the nil guard.
func TestSafeAuthorizationErrorNilStaysNil(t *testing.T) {
	t.Parallel()

	if err := safeAuthorizationError(nil); err != nil {
		t.Fatalf("safeAuthorizationError(nil) = %v, want nil", err)
	}
}

// TestSafeAuthorizationErrorPreservesCancellation covers the
// context.Canceled identity branch.
func TestSafeAuthorizationErrorPreservesCancellation(t *testing.T) {
	t.Parallel()

	if !errors.Is(safeAuthorizationError(context.Canceled), context.Canceled) {
		t.Fatal("safeAuthorizationError(context.Canceled) did not preserve context error")
	}
}
