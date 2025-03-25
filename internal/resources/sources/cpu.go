// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package sources

import (
	"context"
	"fmt"
	"math"

	"github.com/go-logr/logr"
	"github.com/shirou/gopsutil/v3/cpu"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"

	core "github.com/ironcore-dev/ironcore/api/core/v1alpha1"
	"github.com/ironcore-dev/libvirt-provider/api"
	"github.com/ironcore-dev/libvirt-provider/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	SourceCPU string = "cpu"
)

type CPU struct {
	overcommitVCPU float64
	availableCPU   *resource.Quantity
	log            logr.Logger
}

func NewSourceCPU(options Options) *CPU {
	return &CPU{
		overcommitVCPU: options.OvercommitVCPU,
		log:            options.Log.WithName(SourceCPU),
	}
}

func (c *CPU) GetName() string {
	return SourceCPU
}

// Modify rounding up cpu to total cores
func (c *CPU) Modify(_ core.ResourceList) error {
	return nil
}

func (c *CPU) CalculateMachineClassQuantity(_ core.ResourceName, quantity *resource.Quantity) int64 {
	return int64(math.Floor(float64(c.availableCPU.Value()) / float64(quantity.Value())))
}

func (c *CPU) Init(ctx context.Context) (sets.Set[core.ResourceName], error) {
	hostCPU, err := cpu.InfoWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get host cpu information: %w", err)
	}

	var hostCPUSum int64
	for _, v := range hostCPU {
		hostCPUSum += int64(v.Cores)
	}

	// Convert the calculated CPU quantity to an int64 to ensure that it represents a whole number of CPUs.
	cpuQuantity := float64(hostCPUSum) * c.overcommitVCPU
	c.availableCPU = resource.NewQuantity(int64(cpuQuantity), resource.DecimalSI)

	return sets.New(core.ResourceCPU), nil
}

func (c *CPU) Allocate(_ *api.Machine, requiredResources core.ResourceList) (core.ResourceList, error) {
	cpu, ok := requiredResources[core.ResourceCPU]
	if !ok {
		return nil, nil
	}

	if c.availableCPU.Cmp(cpu) < 0 {
		return nil, fmt.Errorf("failed to allocate %s: %w", core.ResourceCPU, ErrResourceNotAvailable)
	}

	c.availableCPU.Sub(cpu)

	return core.ResourceList{core.ResourceCPU: cpu}, nil
}

func (c *CPU) Deallocate(_ *api.Machine, requiredResources core.ResourceList) []core.ResourceName {
	cpu, ok := requiredResources[core.ResourceCPU]
	if !ok {
		return nil
	}

	c.availableCPU.Add(cpu)

	return []core.ResourceName{core.ResourceCPU}
}

func (c *CPU) GetAvailableResources() core.ResourceList {
	return core.ResourceList{core.ResourceCPU: *c.availableCPU}
}

func (c *CPU) SetResourcesMetric(metric *prometheus.GaugeVec) {
	labels := prometheus.Labels{
		metrics.LabelSource:   c.GetName(),
		metrics.LabelResource: GetMetricsResourceName(string(core.ResourceCPU), resourceCPUUnit),
	}

	cpuGauge, err := metrics.GetGaugeWithLabels(metric, labels)
	if err != nil {
		c.log.Error(err, "failed to get cpu metric", metrics.LogKeyLabels, labels)
	}
	cpuGauge.Set(float64(c.availableCPU.Value()))
}
