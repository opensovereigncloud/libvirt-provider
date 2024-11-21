// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package sources

import (
	"context"
	"fmt"
	"math"

	core "github.com/ironcore-dev/ironcore/api/core/v1alpha1"
	"github.com/ironcore-dev/libvirt-provider/api"
	"github.com/shirou/gopsutil/v3/mem"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	ResourceHugepages core.ResourceName = "hugepages"
	SourceHugepages   string            = "hugepages"
)

type Hugepages struct {
	pageSize           uint64
	pageCount          uint64
	availableMemory    *resource.Quantity
	availableHugePages *resource.Quantity
	blockedCount       uint64
}

func NewSourceHugepages(options Options) *Hugepages {
	return &Hugepages{blockedCount: options.BlockedHugepages}
}

func (m *Hugepages) GetName() string {
	return SourceHugepages
}

// Modify set hugepages for resources and rounded up memory size
func (m *Hugepages) Modify(resources core.ResourceList) error {
	memory, ok := resources[core.ResourceMemory]
	if !ok {
		return fmt.Errorf("cannot found memory in resources")
	}

	if memory.Value() <= 0 {
		return fmt.Errorf("invalid value of memory resource %d", memory.Value())
	}

	size := float64(memory.Value())
	hugepages := uint64(math.Ceil(size / float64(m.pageSize)))
	resources[ResourceHugepages] = *resource.NewQuantity(int64(hugepages), resource.DecimalSI)
	// i don't want to do rounding
	resources[core.ResourceMemory] = *resource.NewQuantity(int64(hugepages)*int64(m.pageSize), resource.BinarySI)

	return nil
}

func (m *Hugepages) CalculateMachineClassQuantity(_ core.ResourceName, quantity *resource.Quantity) int64 {
	return int64(math.Floor(float64(m.availableMemory.Value()) / float64(quantity.Value())))
}

func (m *Hugepages) Init(ctx context.Context) (sets.Set[core.ResourceName], error) {
	hostMem, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get host memory information: %w", err)
	}

	m.pageSize = hostMem.HugePageSize
	m.pageCount = hostMem.HugePagesTotal

	availableHugepagesCount, err := calculateAvailableHugepages(m.pageCount, m.blockedCount)
	if err != nil {
		return nil, err
	}
	m.availableHugePages = resource.NewQuantity(int64(availableHugepagesCount), resource.DecimalSI)
	m.availableMemory = resource.NewQuantity(int64(availableHugepagesCount*m.pageSize), resource.BinarySI)

	return sets.New(core.ResourceMemory, ResourceHugepages), nil
}

func (m *Hugepages) Allocate(_ *api.Machine, requiredResources core.ResourceList) (core.ResourceList, error) {
	mem, ok := requiredResources[core.ResourceMemory]
	if !ok {
		return nil, nil
	}

	if m.availableMemory.Cmp(mem) < 0 {
		return nil, fmt.Errorf("failed to allocate resource %s: %w", core.ResourceMemory, ErrResourceNotAvailable)
	}

	hugepages, ok := requiredResources[ResourceHugepages]
	if !ok {
		return nil, fmt.Errorf("failed to allocate resource %s: %w", ResourceHugepages, ErrResourceMissing)
	}

	if m.availableHugePages.Cmp(hugepages) < 0 {
		return nil, fmt.Errorf("failed to allocate resource %s: %w", ResourceHugepages, ErrResourceNotAvailable)
	}

	m.availableMemory.Sub(mem)
	m.availableHugePages.Sub(hugepages)

	return core.ResourceList{core.ResourceMemory: mem, ResourceHugepages: hugepages}, nil
}

func (m *Hugepages) Deallocate(_ *api.Machine, requiredResources core.ResourceList) []core.ResourceName {
	deallocated := []core.ResourceName{}
	mem, ok := requiredResources[core.ResourceMemory]
	if ok {
		m.availableMemory.Add(mem)
		deallocated = append(deallocated, core.ResourceMemory)
	}

	hugepages, ok := requiredResources[ResourceHugepages]
	if ok {
		m.availableHugePages.Add(hugepages)
		deallocated = append(deallocated, ResourceHugepages)
	}

	return deallocated
}

func (m *Hugepages) GetAvailableResources() core.ResourceList {
	return core.ResourceList{core.ResourceMemory: *m.availableMemory, ResourceHugepages: *m.availableHugePages}
}

func calculateAvailableHugepages(totalHugepages, blockedHugepages uint64) (uint64, error) {
	if blockedHugepages > totalHugepages {
		return 0, fmt.Errorf("blockedHugepages cannot be greater than totalPage count: %d", totalHugepages)
	}

	return totalHugepages - blockedHugepages, nil
}
