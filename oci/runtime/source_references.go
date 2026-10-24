// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"fmt"
	"slices"
)

func (s *Source) listReferences(
	ctx context.Context,
	client RegistryClient,
	target TargetConfig,
) ([]string, bool, error) {
	if len(target.References) > 0 {
		return append([]string(nil), target.References...), false, nil
	}
	response, err := client.ListTags(ctx, target.Repository, target.TagLimit)
	if err != nil {
		return nil, false, fmt.Errorf("list OCI registry tags: %w", err)
	}
	tags := response.Tags
	slices.Sort(tags)
	tags = slices.Compact(tags)
	truncated := !response.Complete || len(tags) > target.TagLimit
	if len(tags) > target.TagLimit {
		tags = tags[:target.TagLimit]
	}
	return tags, truncated, nil
}
