// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	subsystemResourceManager = "resourcemanager"
)

var (
	ResourcesAvailable = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystemResourceManager,
			Name:      "resources_available",
			Help:      "Available resources per source.",
		},
		[]string{"source", "resource"},
	)

	ResourcesTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystemResourceManager,
			Name:      "resources_total",
			Help:      "Total resources per source.",
		},
		[]string{"source", "resource"},
	)

	VMSlotsAvailable = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystemResourceManager,
			Name:      "vm_slot_available_total",
			Help:      "Total number of available VM slots in the resource manager.",
		},
	)

	MachineClassesSkipped = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystemResourceManager,
			Name:      "machineclasses_skipped_total",
			Help:      "Total count of skipped machine classes.",
		},
	)

	MachinesAvailable = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystemResourceManager,
			Name:      "machines_available",
			Help:      "Number of available machines per machineclass.",
		},
		[]string{"machineclass"},
	)

	MachinesTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystemResourceManager,
			Name:      "machines_total",
			Help:      "Total number of machines per machineclass.",
		},
		[]string{"machineclass"},
	)
)

func init() {
	prometheus.MustRegister(ResourcesAvailable)
	prometheus.MustRegister(ResourcesTotal)
	prometheus.MustRegister(VMSlotsAvailable)
	prometheus.MustRegister(MachineClassesSkipped)
	prometheus.MustRegister(MachinesAvailable)
	prometheus.MustRegister(MachinesTotal)
}
