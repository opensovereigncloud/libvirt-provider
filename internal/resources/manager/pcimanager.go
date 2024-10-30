// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package manager

import (
	core "github.com/ironcore-dev/ironcore/api/core/v1alpha1"
	"github.com/ironcore-dev/libvirt-provider/api"
)

type DummyPCIManager struct{}

func (d *DummyPCIManager) AllocatePCIAddress(core.ResourceList) ([]api.PCIDevice, error) {
	return nil, nil
}

func (d *DummyPCIManager) DeallocatePCIAddress([]api.PCIDevice) error {
	return nil
}
