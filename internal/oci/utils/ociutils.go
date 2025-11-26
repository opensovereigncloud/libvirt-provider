// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"fmt"
	"runtime"

	ocispecv1 "github.com/opencontainers/image-spec/specs-go/v1"
)

func Platform() (*ocispecv1.Platform, error) {
	platform := ocispecv1.Platform{
		//TODO change if others should be supported
		OS: "linux",
	}
	switch architecture := runtime.GOARCH; architecture {
	case "amd64":
		platform.Architecture = architecture
	case "arm64":
		platform.Architecture = architecture
	default:
		return nil, fmt.Errorf("unsupported architecture: %s", architecture)
	}

	return &platform, nil
}
