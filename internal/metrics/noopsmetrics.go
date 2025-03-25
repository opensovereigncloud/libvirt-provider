// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// Global No-Op instance to avoid multiple allocations
var NoOpsMetricProvider = noOpsMetrics{}

type noOpsMetrics struct{}

func (noOpsMetrics) Inc()                             {}
func (noOpsMetrics) Add(float64)                      {}
func (noOpsMetrics) Desc() *prometheus.Desc           { return nil }
func (noOpsMetrics) Write(*dto.Metric) error          { return nil }
func (noOpsMetrics) Describe(chan<- *prometheus.Desc) {}
func (noOpsMetrics) Collect(chan<- prometheus.Metric) {}
func (noOpsMetrics) SetToCurrentTime()                {}
func (noOpsMetrics) Set(float64)                      {}
func (noOpsMetrics) Dec()                             {}
func (noOpsMetrics) Sub(float64)                      {}
func (noOpsMetrics) Observe(float64)                  {}
