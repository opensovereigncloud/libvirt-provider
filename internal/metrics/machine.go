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
	}, []string{LabelState})

	MachinesDestroyed = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: subsystemMachine,
		Name:      "destroyed_total",
		Help:      "Total count of machines which weren't shutdown gracefully.",
	})
)

func InitializeMachineMetrics(machines []*api.Machine) error {
	for _, machine := range machines {
		labels := prometheus.Labels{LabelState: machine.GetState()}
		machinestateGauge, err := GetGaugeWithLabels(MachinesState, labels)
		if err != nil {
			return err
		}
		machinestateGauge.Inc()

		if machine.GetDeletedAt() != nil {
			MachinesDeleteMarked.Inc()
		}
	}
	return nil
}
