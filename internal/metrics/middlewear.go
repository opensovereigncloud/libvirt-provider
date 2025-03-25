// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-logr/logr"

	"github.com/prometheus/client_golang/prometheus"
)

func NewHTTPMetricsMiddlewareHandler(log logr.Logger, serverName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			rw := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(rw, r)

			duration := time.Since(start).Milliseconds()
			path := r.URL.Path
			method := r.Method

			statusCodeStr := strconv.Itoa(rw.Status())

			labels := prometheus.Labels{
				LabelServer: serverName,
				LabelMethod: method,
				LabelPath:   path,
				LabelStatus: statusCodeStr,
			}

			httpServerRequestDurationGauge, err := GetHistogramWithLabels(httpServerRequestDuration, labels)
			if err != nil {
				log.Error(err, "failed to get HTTP server duration metric", LogKeyLabels, labels)
			}
			httpServerRequestDurationGauge.Observe(float64(duration) / 1000)

			httpServerTotalRequestsGauge, err := GetCounterWithLabels(httpServerTotalRequests, labels)
			if err != nil {
				log.Error(err, "failed to get HTTP server total requests metric", LogKeyLabels, labels)
			}
			httpServerTotalRequestsGauge.Inc()
		})
	}
}
