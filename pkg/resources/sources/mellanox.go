// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package sources

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"

	core "github.com/ironcore-dev/ironcore/api/core/v1alpha1"
)

const (
	ResourceMellanox core.ResourceName = "mellanox"
	SourceMellanox   string            = "mellanox"
	devicePath       string            = "/sys/bus/pci/devices"
	mellanoxVendorID string            = "0x15b3"
	vendorAttribute  string            = "vendor"
)

type Mellanox struct {
	availableNics *resource.Quantity
	reservedNics  uint64
}

func NewSourceMellanox(options Options) *Mellanox {
	return &Mellanox{reservedNics: options.ReservedNics}
}

func (m *Mellanox) GetName() string {
	return SourceMellanox
}

func (m *Mellanox) Modify(resources core.ResourceList) error {
	return nil
}

func (m *Mellanox) Init(ctx context.Context) (sets.Set[core.ResourceName], error) {
	files, err := os.ReadDir(devicePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read device directory: %w", err)
	}

	mellanoxCount := 0
	for _, file := range files {
		if isMellanoxNIC(filepath.Join(devicePath, file.Name())) {
			mellanoxCount++
		}
	}

	availableNicCount := mellanoxCount - int(m.reservedNics)
	m.availableNics = resource.NewQuantity(int64(availableNicCount), resource.DecimalSI)

	return sets.New(ResourceNic), nil
}

func (m *Mellanox) Allocate(requiredResources core.ResourceList) core.ResourceList {
	quantity, melOk := requiredResources[ResourceMellanox]
	if !melOk {
		nic, nicOk := requiredResources[ResourceNic]
		if !nicOk {
			return nil
		}
		quantity = nic
	}

	m.availableNics.Sub(quantity)
	return core.ResourceList{ResourceMellanox: quantity}
}

func (m *Mellanox) Deallocate(requiredResources core.ResourceList) []core.ResourceName {
	quantity, melOk := requiredResources[ResourceMellanox]
	if !melOk {
		nic, nicOk := requiredResources[ResourceNic]
		if !nicOk {
			return nil
		}
		quantity = nic
	}

	m.availableNics.Add(quantity)
	return []core.ResourceName{ResourceMellanox}
}

func (m *Mellanox) GetAvailableResource() core.ResourceList {
	return core.ResourceList{ResourceMellanox: *m.availableNics}.DeepCopy()
}

func isMellanoxNIC(devicePath string) bool {
	deviceInfoPath := filepath.Join(devicePath, vendorAttribute)
	deviceName, err := os.ReadFile(deviceInfoPath)
	if err != nil {
		return false
	}
	if strings.TrimSpace(string(deviceName)) == mellanoxVendorID {
		return true
	}
	return false
}
