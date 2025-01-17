// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	subsystemRecover = "panics"
)

var (
	PanicsRecovered = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: subsystemRecover,
		Name:      "recovered_total",
		Help:      "Total count of panics which were recovered.",
	})
)

func init() {
	prometheus.MustRegister(PanicsRecovered)
}
