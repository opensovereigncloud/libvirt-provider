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
	// eventIDToLibvirtDomainLifecycleEvent maps domain lifecycle event IDs to their corresponding human-readable event types.
	// These IDs represent the various states that a domain can go through during its lifecycle.
	// Ref: https://libvirt.org/html/libvirt-libvirt-domain.html#virDomainEventType
	eventIDToLibvirtDomainLifecycleEvent = map[int32]string{
		0: "defined",
		1: "undefined",
		2: "started",
		3: "suspended",
		4: "resumed",
		5: "stopped",
		6: "shutdown",
		7: "pmsuspended",
		8: "crashed",
		9: "last",
	}
)

var (
	EventsOverriddenTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystemIRIEvent,
			Name:      "overrides_total",
			Help:      "Total number of overridden machine events in the circular buffer.",
		},
	)

	EventsBufferUsageRatio = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystemIRIEvent,
			Name:      "buffer_usage_ratio",
			Help:      "Ratio of current buffer usage to maximum buffer capacity.",
		},
	)

	EventsLifecycleCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystemLibvirtEvent,
			Name:      "total",
			Help:      "Total number of libvirt lifecycle events, categorized by event ID and event type, captured by the libvirt provider.",
		},
		[]string{"event_id", "event_type"},
	)
)

func GetLibvirtDomainLifecycleEvent(id int32) string {
	eventType, exists := eventIDToLibvirtDomainLifecycleEvent[id]
	if !exists {
		eventType = "unknown"
	}

	return eventType
}
