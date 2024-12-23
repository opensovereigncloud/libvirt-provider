// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type httpMetricsMiddleware struct {
	requestDuration *prometheus.HistogramVec
	totalRequests   *prometheus.CounterVec
}

func NewHTTPMetricsMiddleware(subsystem string) (*httpMetricsMiddleware, error) {
	return newHTTPMetrics(subsystem)
}

func (m *httpMetricsMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		rw := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(rw, r)

		duration := time.Since(start).Milliseconds()
		path := r.URL.Path
		method := r.Method
		status := rw.statusCode

		statusCodeStr := strconv.Itoa(status)

		m.requestDuration.WithLabelValues(method, path, statusCodeStr).Observe(float64(duration) / 1000)
		m.totalRequests.WithLabelValues(method, path, statusCodeStr).Inc()
	})
}

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *statusRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}
