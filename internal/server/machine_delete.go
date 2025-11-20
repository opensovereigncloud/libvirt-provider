// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"fmt"

	iri "github.com/ironcore-dev/ironcore/iri/apis/machine/v1alpha1"
	internalutils "github.com/ironcore-dev/libvirt-provider/internal/utils"
)

func (s *Server) DeleteMachine(ctx context.Context, req *iri.DeleteMachineRequest) (*iri.DeleteMachineResponse, error) {
	log := s.loggerFrom(ctx, internalutils.LogKeyMachineID, req.MachineId)

	log.V(1).Info("Deleting machine")
	if err := s.machineStore.Delete(ctx, req.MachineId); err != nil {
		return nil, convertInternalErrorToGRPC(fmt.Errorf("error deleting machine: %w", err))
	}

	return &iri.DeleteMachineResponse{}, nil
}
