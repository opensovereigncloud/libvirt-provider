// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"fmt"

	"github.com/ironcore-dev/libvirt-provider/api"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	subsystemMachineClasses = "machineclasses"
)

var (
	MachineClassesMachineCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: subsystemMachineClasses,
		Name:      "machine_count",
		Help:      "Current count of machines with spefic machine class.",
	}, []string{"machineclass"})
)

func init() {
	prometheus.MustRegister(MachineClassesMachineCount)
}

func InitializeMachineClassesMetrics(machines []*api.Machine) error {
	for _, machine := range machines {
		class, ok := machine.GetLabels()[api.ClassLabel]
		if !ok {
			return fmt.Errorf("failed to get machine class for machine %s", machine.GetID())
		}

		MachineClassesMachineCount.WithLabelValues(class).Inc()
	}

	return nil
}
