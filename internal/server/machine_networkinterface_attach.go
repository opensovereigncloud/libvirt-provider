// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"fmt"

	iri "github.com/ironcore-dev/ironcore/iri/apis/machine/v1alpha1"
	"github.com/ironcore-dev/libvirt-provider/api"
	internalutils "github.com/ironcore-dev/libvirt-provider/internal/utils"
)

func (s *Server) AttachNetworkInterface(ctx context.Context, req *iri.AttachNetworkInterfaceRequest) (res *iri.AttachNetworkInterfaceResponse, retErr error) {
	log := s.loggerFrom(ctx, internalutils.LogKeyMachineID, req.MachineId, internalutils.LogKeyNICName, req.NetworkInterface.Name)

	log.V(1).Info("Requesting to attach nic")
	apiMachine, err := s.machineStore.Get(ctx, req.MachineId)
	if err != nil {
		return nil, convertInternalErrorToGRPC(wrapErrorFailedToGetMachine(err))
	}

	if api.GetExistingPCICount(apiMachine) >= apiMachine.Spec.PCIControllerTotal {
		return nil, api.ErrPCIControllerMaxedOut
	}

	nicSpec, err := s.getNICFromIRINIC(req.NetworkInterface)
	if err != nil {
		return nil, convertInternalErrorToGRPC(fmt.Errorf("failed to get nic from iri nic: %w", err))
	}

	apiMachine.Spec.NetworkInterfaces = append(apiMachine.Spec.NetworkInterfaces, nicSpec)

	if _, err := s.machineStore.Update(ctx, apiMachine); err != nil {
		return nil, convertInternalErrorToGRPC(fmt.Errorf("failed to update machine: %w", err))
	}

	return &iri.AttachNetworkInterfaceResponse{}, nil
}
