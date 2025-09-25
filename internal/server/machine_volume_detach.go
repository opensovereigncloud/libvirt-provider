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

func (s *Server) DetachVolume(ctx context.Context, req *iri.DetachVolumeRequest) (*iri.DetachVolumeResponse, error) {

	if req == nil || req.MachineId == "" || req.Name == "" {
		return nil, convertInternalErrorToGRPC(wrapErrorRequestIsNil(ErrInvalidRequest))
	}

	log := s.loggerFrom(ctx, internalutils.LogKeyMachineID, req.MachineId, internalutils.LogKeyVolumeName, req.Name)
	log.V(1).Info("Requesting to detach volume")
	apiMachine, err := s.machineStore.Get(ctx, req.MachineId)
	if err != nil {
		return nil, convertInternalErrorToGRPC(wrapErrorFailedToGetMachine(err))
	}

	var updatedVolumes []*api.VolumeSpec
	found := false
	for _, volume := range apiMachine.Spec.Volumes {
		if volume.Name != req.Name {
			updatedVolumes = append(updatedVolumes, volume)
		} else {
			found = true
		}
	}

	if !found {
		return nil, convertInternalErrorToGRPC(fmt.Errorf("volume '%s' not found in machine: %w", req.Name, ErrVolumeNotFound))
	}

	apiMachine.Spec.Volumes = updatedVolumes

	if _, err := s.machineStore.Update(ctx, apiMachine); err != nil {
		return nil, convertInternalErrorToGRPC(fmt.Errorf("failed to update machine after detaching volume: %w", err))
	}

	return &iri.DetachVolumeResponse{}, nil
}
