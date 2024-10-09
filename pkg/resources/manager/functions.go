// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package manager

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	core "github.com/ironcore-dev/ironcore/api/core/v1alpha1"
	iri "github.com/ironcore-dev/ironcore/iri/apis/machine/v1alpha1"
	"github.com/ironcore-dev/libvirt-provider/pkg/api"
	"github.com/ironcore-dev/libvirt-provider/pkg/resources/sources"
	"k8s.io/apimachinery/pkg/api/resource"
)

// AddSource just registers source into manager
func AddSource(source Source) error {
	return mng.addSource(source)
}

// Allocate reserve resources base on machine class.
// Allocated resources are saved into machine specification.
// All resources has to allocated, partially allocation isn't supported.
func Allocate(machine *api.Machine, requiredResources core.ResourceList) error {
	if len(requiredResources) == 0 {
		return ErrResourcesEmpty
	}

	return mng.allocate(machine, requiredResources)
}

// Deallocate free all resources from machine class.
// Deallocated resources are deleted from machine specification.
func Deallocate(machine *api.Machine, deallocateResources core.ResourceList) error {
	if len(deallocateResources) == 0 {
		return ErrResourcesEmpty
	}

	if !HasMachineAllocatedResources(machine) {
		return nil
	}

	return mng.deallocate(machine, deallocateResources)
}

// SetLogger sets logger for internal logging.
// It will add resource-manager into name of logger
func SetLogger(logger logr.Logger) error {
	return mng.setLogger(logger)
}

// SetMachineClasses just registers supported machineclasses
func SetMachineClasses(classes []*iri.MachineClass) error {
	return mng.setMachineClasses(classes)
}

// SetVMLimit just registers maximum limit for VMs
func SetVMLimit(maxVMsLimit uint64) error {
	return mng.setVMLimit(maxVMsLimit)
}

// GetMachineClassStatus return status of machineclasses with current quantity
func GetMachineClassStatus() []*iri.MachineClassStatus {
	return mng.getAvailableMachineClasses()
}

// Initialize inits resource mng.
// Initialize can be call just one time.
// Before Initialize you can call SetMachineClasses, SetLogger, AddSource functions.
// It will calculate available resources during start of app.
// After Initialize you can call Allocate and Deallocate functions.
func Initialize(ctx context.Context, listMachines func(context.Context) ([]*api.Machine, error)) error {
	if listMachines == nil {
		return ErrManagerListFuncInvalid
	}

	machines, err := listMachines(ctx)
	if err != nil {
		return err
	}

	return mng.initialize(ctx, machines)
}

func HasMachineAllocatedResources(machine *api.Machine) bool {
	return len(machine.Spec.Resources) != 0
}

func GetSource(name string, options sources.Options) (Source, error) {
	switch name {
	case sources.SourceMemory:
		return sources.NewSourceMemory(options), nil
	case sources.SourceCPU:
		return sources.NewSourceCPU(options), nil
	case sources.SourceHugepages:
		return sources.NewSourceHugepages(options), nil
	case sources.SourceMellanox:
		return sources.NewSourceMellanox(options), nil
	case sources.SourceNic:
		return sources.NewSourceNIC(options), nil
	default:
		return nil, fmt.Errorf("unsupported source %s", name)
	}
}

func GetSourcesAvailable() []string {
	return []string{sources.SourceCPU, sources.SourceMemory, sources.SourceHugepages}
}

func GetRequiredResources(machineSpec iri.MachineSpec) (core.ResourceList, error) {
	class, err := mng.getMachineClass(machineSpec.Class)
	if err != nil {
		return nil, err
	}

	RequiredResourceList := class.resources.DeepCopy()
	RequiredResourceList[sources.ResourceNic] = *resource.NewQuantity(int64(len(machineSpec.NetworkInterfaces)), resource.DecimalSI)

	return RequiredResourceList, nil
}

func ValidateOptions(options sources.Options) error {
	// To handle the limitations of floating-point arithmetic, where small rounding errors can occur
	// due to the finite precision of floating-point numbers.
	if options.OvercommitVCPU < 1e-9 {
		return errors.New("overcommitVCPU cannot be zero or negative")
	}

	availableSources := make(map[string]bool)

	for _, source := range options.Sources {
		availableSources[source] = true
	}

	if options.ReservedMemorySize != 0 && !availableSources[sources.SourceMemory] {
		return fmt.Errorf("reserved memory size can only be set with %s source", sources.SourceMemory)
	}

	if options.BlockedHugepages != 0 && !availableSources[sources.SourceHugepages] {
		return fmt.Errorf("blocked hugepages can only be set with %s source", sources.SourceHugepages)
	}

	if options.NicLimit != 0 && !availableSources[sources.SourceNic] {
		return fmt.Errorf("NIC limit can only be set with %s source", sources.SourceNic)
	}

	if availableSources[sources.SourceNic] && options.NicLimit == 0 {
		return fmt.Errorf("NIC limit has to be set while using source %s", sources.SourceNic)
	}

	if options.ReservedNics != 0 && !availableSources[sources.SourceNic] && !availableSources[sources.SourceMellanox] {
		return fmt.Errorf("reserved nics can only be set with source %s or %s", sources.SourceNic, sources.SourceMellanox)
	}

	return nil
}
