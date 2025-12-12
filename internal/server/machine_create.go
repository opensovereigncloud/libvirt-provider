// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"errors"
	"fmt"
	"maps"

	"github.com/go-logr/logr"
	"github.com/ironcore-dev/controller-utils/metautils"
	iri "github.com/ironcore-dev/ironcore/iri/apis/machine/v1alpha1"
	api "github.com/ironcore-dev/libvirt-provider/api"
	"github.com/ironcore-dev/libvirt-provider/internal/resources/manager"
	internalutils "github.com/ironcore-dev/libvirt-provider/internal/utils"
)

func (s *Server) createMachineFromIRIMachine(ctx context.Context, log logr.Logger, iriMachine *iri.Machine, machineID string) (*api.Machine, error) {
	log.V(2).Info("Getting libvirt machine config")

	switch {
	case iriMachine == nil:
		return nil, fmt.Errorf("iri machine is nil")
	case iriMachine.Spec == nil:
		return nil, fmt.Errorf("iri machine spec is nil")
	case iriMachine.Metadata == nil:
		return nil, fmt.Errorf("iri machine metadata is nil")
	}

	requiredResources, err := manager.GetMachineClassRequiredResources(iriMachine.Spec.Class)
	if err != nil {
		return nil, fmt.Errorf("failed to get class resources: %w", err)
	}
	log.V(2).Info("Validated class")

	power, err := s.getPowerStateFromIRI(iriMachine.Spec.Power)
	if err != nil {
		return nil, fmt.Errorf("failed to get power state: %w", err)
	}

	var volumes []*api.VolumeSpec
	for _, iriVolume := range iriMachine.Spec.Volumes {
		volumeSpec, err := s.getVolumeFromIRIVolume(iriVolume)
		if err != nil {
			return nil, fmt.Errorf("error converting volume: %w", err)
		}

		volumes = append(volumes, volumeSpec)
	}

	var networkInterfaces []*api.NetworkInterfaceSpec
	for _, iriNetworkInterface := range iriMachine.Spec.NetworkInterfaces {
		networkInterfaceSpec := &api.NetworkInterfaceSpec{
			Name:       iriNetworkInterface.Name,
			NetworkId:  iriNetworkInterface.NetworkId,
			Ips:        iriNetworkInterface.Ips,
			Attributes: iriNetworkInterface.Attributes,
		}
		networkInterfaces = append(networkInterfaces, networkInterfaceSpec)
	}

	machine := &api.Machine{
		Metadata: api.Metadata{
			ID: machineID,
		},
		Spec: api.MachineSpec{
			Power:              power,
			Volumes:            volumes,
			Ignition:           iriMachine.Spec.IgnitionData,
			NetworkInterfaces:  networkInterfaces,
			GuestAgent:         s.guestAgent,
			PCIControllerTotal: s.pciControllerTotal,
		},
	}

	if err := api.SetObjectMetadata(machine, iriMachine.Metadata); err != nil {
		return nil, fmt.Errorf("failed to set metadata: %w", err)
	}

	// we have to break reference between maps
	iriMachineLabels := make(map[string]string, len(iriMachine.Metadata.Labels))
	maps.Copy(iriMachineLabels, iriMachine.Metadata.Labels)
	metautils.SetLabels(machine, iriMachineLabels)

	api.SetClassLabel(machine, iriMachine.Spec.Class)
	api.SetManagerLabel(machine, api.MachineManager)

	log.V(2).Info("allocating machine with machineclass:" + iriMachine.Spec.Class)
	err = manager.Allocate(machine, requiredResources)
	if err != nil {
		return nil, fmt.Errorf("cannot allocate resources: %w", err)
	}

	if api.GetExistingPCICount(machine) > s.pciControllerTotal {
		deallocErr := manager.Deallocate(machine, machine.Spec.Resources.DeepCopy())
		if deallocErr != nil {
			return nil, errors.Join(deallocErr, api.ErrPCIControllerMaxedOut)
		}
		return nil, api.ErrPCIControllerMaxedOut
	}

	apiMachine, err := s.machineStore.Create(ctx, machine)
	if err != nil {
		locErr := manager.Deallocate(machine, machine.Spec.Resources.DeepCopy())
		if locErr != nil {
			log.Error(locErr, "failed to deallocate resources")
		}
		return nil, fmt.Errorf("failed to create machine: %w", err)
	}

	return apiMachine, nil
}

func (s *Server) CreateMachine(ctx context.Context, req *iri.CreateMachineRequest) (res *iri.CreateMachineResponse, retErr error) {
	if req == nil {
		return nil, convertInternalErrorToGRPC(wrapErrorRequestIsNil(ErrInvalidRequest))
	}
	machineID := s.idGen.Generate()
	log := s.loggerFrom(ctx, internalutils.LogKeyReqMachineID, req.Machine.Metadata.Id, internalutils.LogKeyMachineID, machineID)
	log.V(1).Info("Creating machine from iri machine")
	machine, err := s.createMachineFromIRIMachine(ctx, log, req.Machine, machineID)
	if err != nil {
		return nil, convertInternalErrorToGRPC(fmt.Errorf("unable to get libvirt machine config: %w", err))
	}

	log.V(1).Info("Converting machine to iri machine")
	iriMachine, err := s.convertMachineToIRIMachine(machine)
	if err != nil {
		return nil, convertInternalErrorToGRPC(fmt.Errorf("unable to convert machine: %w", err))
	}

	return &iri.CreateMachineResponse{
		Machine: iriMachine,
	}, nil
}
