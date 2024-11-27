// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package manager

import (
	"context"

	core "github.com/ironcore-dev/ironcore/api/core/v1alpha1"
	"github.com/ironcore-dev/libvirt-provider/api"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"
)

type Source interface {
	// GetName return name of source, ideally it has to be uniq
	GetName() string
	// Modify serves for modification resources base (rounding, create subresource).
	// Example: Machineclasses contains memory size only, but if libvirt provider will use hugepages source.
	//   Memory size has to be rounded to whole hugepages and it will create additional resource which count of hugepages.
	Modify(core.ResourceList) error
	// Init ititializes total resources in the source
	Init(context.Context) (sets.Set[core.ResourceName], error)
	// Allocate allocates the resources in the source
	Allocate(*api.Machine, core.ResourceList) (core.ResourceList, error)
	// Deallocate deallocates the resources from the source
	Deallocate(*api.Machine, core.ResourceList) []core.ResourceName
	// GetAvailableResource provides the available resourcelist in the source
	GetAvailableResources() core.ResourceList
	// Calculate allocatable quantity of machines classes for specific resource
	CalculateMachineClassQuantity(core.ResourceName, *resource.Quantity) int64
}

type NumaScheduler interface {
	Pin(cores uint, machine *api.Machine) error
	Unpin(machine *api.Machine) error
}
