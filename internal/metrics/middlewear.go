// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

func NewHTTPMetricsMiddlewareHandler(serverName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			rw := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(rw, r)

			duration := time.Since(start).Milliseconds()
			path := r.URL.Path
			method := r.Method

			statusCodeStr := strconv.Itoa(rw.Status())

			httpServerRequestDuration.WithLabelValues(serverName, method, path, statusCodeStr).Observe(float64(duration) / 1000)
			httpServerTotalRequests.WithLabelValues(serverName, method, path, statusCodeStr).Inc()
		})
	}
}
