// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-collector-oci-registry/oci/runtime"
	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

// TestHandlerZeroValueReadsProcessEnv proves eshu-hq/eshu#9040: the documented
// zero value must resolve username_env/password_env from the process
// environment instead of silently dropping them to blank.
func TestHandlerZeroValueReadsProcessEnv(t *testing.T) {
	t.Setenv("ESHU_OCI_TEST_USER_9040", "zero-value-user-9040")
	t.Setenv("ESHU_OCI_TEST_PASS_9040", "zero-value-pass-9040")

	var got runtime.TargetConfig
	handler := Handler{
		Now: func() time.Time { return parityObservedAt },
		NewClient: runtime.ClientFactoryFunc(func(_ context.Context, target runtime.TargetConfig) (runtime.RegistryClient, error) {
			got = target
			return fullHandlerClient(t), nil
		}),
	}
	config := handlerTargetConfig()
	config["auth_mode"] = "credentialed"
	config["username_env"] = "ESHU_OCI_TEST_USER_9040"
	config["password_env"] = "ESHU_OCI_TEST_PASS_9040"

	result, err := handler.Handle(context.Background(), Request{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           handlerClaim(),
		Contract:        Contract(),
		Config:          config,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	if got.Username != "zero-value-user-9040" {
		t.Fatalf("zero-value Handler resolved Username = %q, want process env", got.Username)
	}
	if got.Password != "zero-value-pass-9040" {
		t.Fatalf("zero-value Handler resolved Password = %q, want process env", got.Password)
	}
	if result.State != sdk.ResultComplete {
		t.Fatalf("State = %q, want complete", result.State)
	}
	if _, err := sdk.NewValidator(Contract()).ValidateResult(result); err != nil {
		t.Fatalf("ValidateResult() error = %v", err)
	}
}

// TestResolveTargetNilGetenvReadsProcessEnv proves the resolveTarget half of
// eshu-hq/eshu#9040: a nil getenv must fall back to the process environment
// instead of resolving every credential reference to blank.
func TestResolveTargetNilGetenvReadsProcessEnv(t *testing.T) {
	t.Setenv("ESHU_OCI_TEST_USER_9040", "resolve-target-user-9040")

	config := handlerTargetConfig()
	config["username_env"] = "ESHU_OCI_TEST_USER_9040"

	target, err := resolveTarget(config, "oci-registry://registry-1.docker.io/library/busybox", nil)
	if err != nil {
		t.Fatalf("resolveTarget() error = %v", err)
	}
	if target.Username != "resolve-target-user-9040" {
		t.Fatalf("Username = %q, want process env", target.Username)
	}
}
