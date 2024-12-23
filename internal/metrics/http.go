// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package metrics

import "github.com/prometheus/client_golang/prometheus"

func newHTTPMetrics(serverName string) (*httpMetricsMiddleware, error) {
	requestDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "httpserver_" + serverName,
			Name:      "request_duration_seconds",
			Help:      "Histogram of HTTP request durations in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"method", "path", "status"},
	)

	totalRequests := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "httpserver_" + serverName,
			Name:      "requests_total",
			Help:      "Total number of HTTP requests.",
		},
		[]string{"method", "path", "status"},
	)

	err := register([]prometheus.Collector{requestDuration, totalRequests})
	if err != nil {
		return nil, err
	}

	return &httpMetricsMiddleware{
		requestDuration: requestDuration,
		totalRequests:   totalRequests,
	}, err
}

func register(collectors []prometheus.Collector) error {
	for _, collector := range collectors {
		err := prometheus.Register(collector)
		if err != nil {
			return err
		}
	}
	return nil
}
