// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	"github.com/ironcore-dev/libvirt-provider/internal/metrics"
	"google.golang.org/grpc"
)

// Recover handles panics and logs the error with the provided logger.
// This is reusable across the application.
func Recover(log logr.Logger, panicCatcher string) {
	if r := recover(); r != nil {
		metrics.PanicsRecovered.Inc()
		LogPanic(log, r, panicCatcher)
	}
}

func LogPanic(log logr.Logger, r interface{}, panicCatcher string) {
	log.Error(fmt.Errorf("%v", r), "caught panic", "panicCatcher", panicCatcher)
}

// RecoveryMiddleware is the middleware to recover from panics in HTTP handlers.
func RecoveryMiddleware(log logr.Logger, panicCatcher string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		// inspired by https://github.com/go-chi/chi/blob/master/middleware/recoverer.go
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rvr := recover(); rvr != nil {
					if rvr == http.ErrAbortHandler {
						// we don't recover http.ErrAbortHandler so the response
						// to the client is aborted, this should not be logged
						panic(rvr)
					}

					metrics.PanicsRecovered.Inc()
					LogPanic(log, rvr, panicCatcher)

					if r.Header.Get("Connection") != "Upgrade" {
						w.WriteHeader(http.StatusInternalServerError)
					}
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// RecoveryInterceptor recovers from panics in gRPC handlers.
func RecoveryInterceptor(log logr.Logger, panicCatcher string) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp interface{}, err error) {
		defer Recover(log, panicCatcher)
		return handler(ctx, req)
	}
}
