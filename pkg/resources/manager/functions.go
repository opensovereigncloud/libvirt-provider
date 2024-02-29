// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package manager

import (
	"context"

	"github.com/go-logr/logr"
	iri "github.com/ironcore-dev/ironcore/iri/apis/machine/v1alpha1"
	"github.com/ironcore-dev/libvirt-provider/pkg/api"
)

// TODO: we will need to solve additional allocation/deallocation (adding disk, network cards, ...)

// AddSource just registers source into manager
func AddSource(source Source) error {
	return manager.addSource(source)
}

// Allocate reserve resources base on machine class.
// Allocated resources are saved into machine specification.
// All resources has to allocated, partially allocation isn't supported.
func Allocate(machine *api.Machine) error {
	if HasMachineAllocatedResources(machine) {
		return ErrResourceAlreadyRegistred
	}

	return manager.allocate(machine)
}

// Deallocate free all resources from machine class.
// Deallocated resources are deleted from machine specification.
// All resources has to deallocated, partially deallocation isn't supported.
func Deallocate(machine *api.Machine) error {
	if !HasMachineAllocatedResources(machine) {
		return nil
	}

	return manager.deallocate(machine)
}

// SetLogger sets logger for internal logging.
// It will add resource-manager into name of logger
func SetLogger(logger logr.Logger) error {
	return manager.setLogger(logger)
}

// SetMachineClasses just registers supported machineclasses
func SetMachineClasses(classes []*iri.MachineClass) error {
	return manager.setMachineClasses(classes)
}

// GetMachineClassStatus return status of machineclasses with current quantity
func GetMachineClassStatus() []*iri.MachineClassStatus {
	return manager.getAvailableMachineClasses()
}

// Initialize inits resource manager.
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

	return manager.initialize(ctx, machines)
}

func HasMachineAllocatedResources(machine *api.Machine) bool {
	return len(machine.Spec.Resources) != 0
}
