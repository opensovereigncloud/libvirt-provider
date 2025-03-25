// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package metrics

import "github.com/prometheus/client_golang/prometheus"

const (
	subsystemHTTPServer = "httpserver"
)

var (
	httpServerRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystemHTTPServer,
			Name:      "request_duration_seconds",
			Help:      "Histogram of HTTP request durations in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{LabelServer, LabelMethod, LabelPath, LabelStatus},
	)

	httpServerTotalRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystemHTTPServer,
			Name:      "requests_total",
			Help:      "Total number of HTTP requests.",
		},
		[]string{LabelServer, LabelMethod, LabelPath, LabelStatus},
	)
)
