// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"fmt"
	"slices"

	iri "github.com/ironcore-dev/ironcore/iri/apis/machine/v1alpha1"
	"github.com/ironcore-dev/libvirt-provider/api"
	internalutils "github.com/ironcore-dev/libvirt-provider/internal/utils"
)

func (s *Server) UpdateVolume(ctx context.Context, req *iri.UpdateVolumeRequest) (*iri.UpdateVolumeResponse, error) {
	if req.MachineId == "" || req.Volume == nil {
		return nil, convertInternalErrorToGRPC(wrapErrorRequestIsNil(ErrInvalidRequest))
	}
	log := s.loggerFrom(ctx, internalutils.LogKeyMachineID, req.MachineId, internalutils.LogKeyVolumeName, req.Volume.Name)
	log.V(1).Info("Requesting to update volume")
	apiMachine, err := s.machineStore.Get(ctx, req.MachineId)
	if err != nil {
		return nil, convertInternalErrorToGRPC(wrapErrorFailedToGetMachine(err))
	}

	apiVolumeIndex := apiMachineVolumeIndex(apiMachine, req.Volume.Name)
	if apiVolumeIndex == -1 {
		return nil, convertInternalErrorToGRPC(fmt.Errorf("volume '%s' not found in machine: %w", req.Volume.Name, ErrVolumeNotFound))
	}

	apiBaseVolume := apiMachine.Spec.Volumes[apiVolumeIndex]
	apiBaseVolume.Device = req.Volume.Device
	if volumeConnection := req.Volume.Connection; volumeConnection != nil {
		apiBaseVolume.Connection = &api.VolumeConnection{
			Driver:                volumeConnection.Driver,
			Handle:                volumeConnection.Handle,
			Attributes:            volumeConnection.Attributes,
			SecretData:            volumeConnection.SecretData,
			EncryptionData:        volumeConnection.EncryptionData,
			EffectiveStorageBytes: volumeConnection.EffectiveStorageBytes,
		}
	}

	apiMachine.Spec.Volumes[apiVolumeIndex] = apiBaseVolume

	if _, err := s.machineStore.Update(ctx, apiMachine); err != nil {
		return nil, convertInternalErrorToGRPC(fmt.Errorf("failed to update machine after updating volume: %w", err))
	}

	return &iri.UpdateVolumeResponse{}, nil
}

func apiMachineVolumeIndex(apiMachine *api.Machine, name string) int {
	return slices.IndexFunc(
		apiMachine.Spec.Volumes,
		func(volume *api.VolumeSpec) bool {
			return volume.Name == name
		},
	)
}
