// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package sources

import (
	"errors"
	"fmt"
)

const (
	QuantityCountIgnore = -1

	ResourceMemoryUnit = "bytes"
	resourceCPUUnit    = "cores"
)

var (
	ErrResourceNotAvailable = errors.New("not enough available resources")
	ErrResourceMissing      = errors.New("resource is missing")

	ErrSourceResourceUnsupport = errors.New("unsupported resource in source")
)

func GetMetricsResourceName(resourceName, unit string) string {
	return fmt.Sprintf("%s_%s", resourceName, unit)
}
