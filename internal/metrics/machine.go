// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/ironcore-dev/libvirt-provider/api"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	subsystemMachine = "machines"
)

var (
	MachinesDeleteMarked = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: subsystemMachine,
		Name:      "delete_marked",
		Help:      "Current count of manage machines mark for deletion.",
	})

	MachinesState = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: subsystemMachine,
		Name:      "state",
		Help:      "Current count of manage machines in specific state.",
	}, []string{"state"})

	MachinesDestroyed = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: subsystemMachine,
		Name:      "destroyed_total",
		Help:      "Total count of machines which weren't shutdown gracefully.",
	})
)

func init() {
	prometheus.MustRegister(MachinesDeleteMarked)
	prometheus.MustRegister(MachinesState)
	prometheus.MustRegister(MachinesDestroyed)
}

func InitializeMachineMetrics(machines []*api.Machine) {
	for _, machine := range machines {
		MachinesState.WithLabelValues(machine.GetState()).Inc()
		if machine.GetDeletedAt() != nil {
			MachinesDeleteMarked.Inc()
		}
	}
}
