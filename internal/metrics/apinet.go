// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package metrics

import "github.com/prometheus/client_golang/prometheus"

const (
	subsystemAPINet = "apinet"
)

var (
	APINetNicsVirtFn = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystemAPINet,
			Name:      "nics_virtfn",
			Help:      "Reports assigned virtual function names for Machines NICs.",
		},
		[]string{LabelName, LabelMachineID, LabelRootMachineName, LabelRootMachineNamespace, LabelNic},
	)
)
