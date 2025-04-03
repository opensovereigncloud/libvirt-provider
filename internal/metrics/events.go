// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	subsystemIRIEvent     = "iri_events"
	subsystemLibvirtEvent = "libvirt_events"
)

var (
	IRIEventsOverriddenTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystemIRIEvent,
			Name:      "overrides_total",
			Help:      "Total number of overridden machine events in the circular buffer.",
		},
	)

	IRIEventsBufferUsageRatio = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystemIRIEvent,
			Name:      "buffer_usage_ratio",
			Help:      "Ratio of current buffer usage to maximum buffer capacity.",
		},
	)

	LibvirtEventsCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystemLibvirtEvent,
			Name:      "total",
			Help:      "Total number of libvirt events, categorized by event ID and event type, captured by the libvirt provider.",
		},
		[]string{LabelEventID, LabelEventType},
	)
)
