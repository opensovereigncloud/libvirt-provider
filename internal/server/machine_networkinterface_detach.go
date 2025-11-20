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

func (s *Server) DetachNetworkInterface(
	ctx context.Context,
	req *iri.DetachNetworkInterfaceRequest,
) (*iri.DetachNetworkInterfaceResponse, error) {
	log := s.loggerFrom(ctx, internalutils.LogKeyMachineID, req.MachineId, internalutils.LogKeyNICName, req.Name)
	log.V(1).Info("Requesting to detach nic")

	apiMachine, err := s.machineStore.Get(ctx, req.MachineId)
	if err != nil {
		return nil, convertInternalErrorToGRPC(wrapErrorFailedToGetMachine(err))
	}

	var updatedNICS []*api.NetworkInterfaceSpec
	found := false
	for _, nic := range apiMachine.Spec.NetworkInterfaces {
		if nic.Name != req.Name {
			updatedNICS = append(updatedNICS, nic)
		} else {
			found = true
		}
	}

	if !found {
		return nil, convertInternalErrorToGRPC(fmt.Errorf("nic '%s' not found in machine: %w", req.Name, ErrNicNotFound))
	}

	apiMachine.Spec.NetworkInterfaces = updatedNICS

	if _, err := s.machineStore.Update(ctx, apiMachine); err != nil {
		return nil, convertInternalErrorToGRPC(fmt.Errorf("failed to update machine: %w", err))
	}

	return &iri.DetachNetworkInterfaceResponse{}, nil
}
