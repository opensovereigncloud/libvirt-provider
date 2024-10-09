// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"fmt"

	core "github.com/ironcore-dev/ironcore/api/core/v1alpha1"
	iri "github.com/ironcore-dev/ironcore/iri/apis/machine/v1alpha1"
	"github.com/ironcore-dev/libvirt-provider/pkg/api"
	"github.com/ironcore-dev/libvirt-provider/pkg/resources/manager"
	"github.com/ironcore-dev/libvirt-provider/pkg/resources/sources"
	"k8s.io/apimachinery/pkg/api/resource"
)

func (s *Server) DetachNetworkInterface(
	ctx context.Context,
	req *iri.DetachNetworkInterfaceRequest,
) (*iri.DetachNetworkInterfaceResponse, error) {
	log := s.loggerFrom(ctx)
	log.V(1).Info("Detaching nic from machine")

	if req == nil {
		return nil, fmt.Errorf("DetachNetworkInterface is nil")
	}

	apiMachine, err := s.machineStore.Get(ctx, req.MachineId)
	if err != nil {
		return nil, fmt.Errorf("failed to get machine: %w", err)
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
		return nil, fmt.Errorf("nic '%s' not found in machine '%s'", req.Name, req.MachineId)
	}

	apiMachine.Spec.NetworkInterfaces = updatedNICS

	if _, err := s.machineStore.Update(ctx, apiMachine); err != nil {
		return nil, fmt.Errorf("failed to update machine: %w", err)
	}

	err = manager.Deallocate(apiMachine, core.ResourceList{sources.ResourceNic: *resource.NewQuantity(int64(1), resource.DecimalSI)})
	if err != nil {
		return nil, fmt.Errorf("failed to deallocate nic in resource manager: %w", err)
	}

	return &iri.DetachNetworkInterfaceResponse{}, nil
}
