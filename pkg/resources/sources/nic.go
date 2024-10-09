// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package sources

import (
	"context"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"

	core "github.com/ironcore-dev/ironcore/api/core/v1alpha1"
)

const (
	ResourceNic core.ResourceName = "nic"
	SourceNic   string            = "nic"
)

type NIC struct {
	availableNics *resource.Quantity
}

func NewSourceNIC(options Options) *NIC {
	return &NIC{availableNics: resource.NewQuantity(int64(options.NicLimit-options.ReservedNics), resource.DecimalSI)}
}

func (m *NIC) GetName() string {
	return SourceNic
}

func (m *NIC) Modify(resources core.ResourceList) error {
	return nil
}

func (m *NIC) Init(ctx context.Context) (sets.Set[core.ResourceName], error) {
	return sets.New(ResourceNic), nil
}

func (m *NIC) Allocate(requiredResources core.ResourceList) core.ResourceList {
	nic, ok := requiredResources[ResourceNic]
	if !ok {
		return nil
	}

	m.availableNics.Sub(nic)
	return core.ResourceList{ResourceNic: nic}
}

func (m *NIC) Deallocate(requiredResources core.ResourceList) []core.ResourceName {
	nic, ok := requiredResources[ResourceNic]
	if !ok {
		return nil
	}

	m.availableNics.Add(nic)
	return []core.ResourceName{ResourceNic}
}

func (m *NIC) GetAvailableResource() core.ResourceList {
	return core.ResourceList{ResourceNic: *m.availableNics}.DeepCopy()
}
