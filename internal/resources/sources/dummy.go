// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package sources

import (
	"context"
	"fmt"

	core "github.com/ironcore-dev/ironcore/api/core/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	ResourceDummy core.ResourceName = "dummy"
	SourceDummy   string            = "dummy"
)

// Dummy source serves for dynamic change of available memory for unit tests
type Dummy struct {
	availableQuantity *resource.Quantity
}

func NewSourceDummy(availableQuantity *resource.Quantity) *Dummy {
	return &Dummy{availableQuantity: availableQuantity}
}

func (d *Dummy) GetName() string {
	return SourceDummy
}

// Modify is dummy function
func (d *Dummy) Modify(resources core.ResourceList) error {
	_, ok := resources[ResourceDummy]
	if ok {
		return fmt.Errorf("error while modifing resource")
	}

	return nil
}

func (d *Dummy) Allocate(requiredResources core.ResourceList) (core.ResourceList, error) {
	dummy, ok := requiredResources[ResourceDummy]
	if !ok {
		return nil, nil
	}

	newDummy := *d.availableQuantity
	newDummy.Sub(dummy)
	if newDummy.Sign() == -1 {
		return nil, fmt.Errorf("failed to allocate %s: %w", ResourceDummy, ErrResourceNotAvailable)
	}

	d.availableQuantity = &newDummy
	return core.ResourceList{ResourceDummy: dummy}, nil
}

func (d *Dummy) Deallocate(requiredResources core.ResourceList) []core.ResourceName {
	dummy, ok := requiredResources[ResourceDummy]
	if !ok {
		return nil
	}

	d.availableQuantity.Add(dummy)
	return []core.ResourceName{ResourceDummy}
}

func (d *Dummy) GetAvailableResources() core.ResourceList {
	return core.ResourceList{ResourceDummy: *d.availableQuantity}.DeepCopy()
}

func (d *Dummy) Init(ctx context.Context) (sets.Set[core.ResourceName], error) {
	return sets.New(ResourceDummy, core.ResourceCPU, core.ResourceMemory, core.ResourceName("memory.epc.sgx")), nil
}

func (d *Dummy) CalculateMachineClassQuantity(requiredResources core.ResourceList) int64 {
	return d.availableQuantity.Value()
}

func (d *Dummy) SetQuantity(quantity int64) {
	d.availableQuantity = resource.NewQuantity(quantity, resource.DecimalSI)
}
