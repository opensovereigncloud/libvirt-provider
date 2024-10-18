// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package sources

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	"github.com/go-playground/validator/v10"
	core "github.com/ironcore-dev/ironcore/api/core/v1alpha1"
	"github.com/ironcore-dev/libvirt-provider/api"
	"github.com/ironcore-dev/libvirt-provider/internal/osutils"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	SourcePCI           = "pci"
	sysPCIDevicesFolder = "/sys/bus/pci/devices"

	attributeVendor = "vendor"
	attributeClass  = "class"
)

type HexID string

// DeviceList holds a list of vendors and validates unique IDs
type DeviceList struct {
	Vendors []*Vendor `yaml:"vendors" validate:"unique=ID"`
}

// Vendor represents a PCI vendor with a list of devices
type Vendor struct {
	ID            HexID     `yaml:"id" validate:"required,hexadecimal"`
	Name          string    `yaml:"name" validate:"required"`
	Devices       []*Device `yaml:"devices" validate:"required,unique=Name"`
	loadedDevices map[HexID]*Device
}

// Device represents a PCI device
type Device struct {
	ID   HexID  `yaml:"id" validate:"required,hexadecimal"`
	Name string `yaml:"name" validate:"required"`
	Type string `yaml:"type" validate:"required"`
}

// PCI manages the allocation and deallocation of PCI devices and their resources.
type PCI struct {
	deviceFilePath     string
	devices            map[core.ResourceName][]*api.PCIAddress
	availableResources core.ResourceList
	log                logr.Logger
}

func NewSourcePCI(options Options) *PCI {
	return &PCI{
		deviceFilePath:     options.PCIDevicesFile,
		devices:            map[core.ResourceName][]*api.PCIAddress{},
		availableResources: core.ResourceList{},
		log:                options.log.WithName("source-pci"),
	}
}

func (p *PCI) GetName() string {
	return SourcePCI
}

// Modify is dummy function
func (p *PCI) Modify(_ core.ResourceList) error {
	return nil
}

func (p *PCI) CalculateMachineClassQuantity(resource core.ResourceName, quantity *resource.Quantity) int64 {
	if availableQuantity, exists := p.availableResources[resource]; exists {
		return int64(math.Floor(float64(availableQuantity.Value()) / float64(quantity.Value())))
	}
	return 0
}

func (p *PCI) Init(ctx context.Context) (sets.Set[core.ResourceName], error) {
	err := p.discoverDevices()
	if err != nil {
		return nil, err
	}

	supportedResources := make(sets.Set[core.ResourceName], len(p.availableResources))
	for key := range p.availableResources {
		supportedResources.Insert(key)
	}
	return supportedResources, nil
}

func (p *PCI) Allocate(requiredResources core.ResourceList) (core.ResourceList, error) {
	allocatedResources := core.ResourceList{}

	for key, requiredQty := range requiredResources {
		if availableQty, exists := p.availableResources[key]; exists {
			newQty := availableQty.DeepCopy()
			newQty.Sub(requiredQty)

			if newQty.Sign() == -1 {
				return nil, fmt.Errorf("failed to allocate resource %s: %w", key, ErrResourceNotAvailable)
			}

			allocatedResources[key] = availableQty
			p.availableResources[key] = newQty
		}
	}

	return allocatedResources, nil
}

func (p *PCI) Deallocate(requiredResources core.ResourceList) []core.ResourceName {
	deallocatedResources := []core.ResourceName{}

	for key, quantity := range requiredResources {
		if availableQty, exists := p.availableResources[key]; exists {
			availableQty.Add(quantity)
			p.availableResources[key] = availableQty
			deallocatedResources = append(deallocatedResources, key)
		}
	}
	return deallocatedResources
}

func (p *PCI) GetAvailableResources() core.ResourceList {
	return p.availableResources.DeepCopy()
}

func (p *PCI) AllocatePCIAddress(resources core.ResourceList) ([]api.PCIDevice, error) {
	var domainAddrs []api.PCIDevice

	for resourceName, addrs := range p.devices {
		if requiredQty, exists := resources[resourceName]; exists {
			if int64(len(addrs)) < requiredQty.Value() {
				return nil, fmt.Errorf("not enough PCI addresses for device %s", resourceName)
			}

			for i := int64(0); i < requiredQty.Value(); i++ {
				domainAddrs = append(domainAddrs, api.PCIDevice{Addr: *addrs[i], Name: resourceName})
			}
			p.devices[resourceName] = addrs[requiredQty.Value():]
		}
	}

	return domainAddrs, nil
}

func (p *PCI) DeallocatePCIAddress(devices []api.PCIDevice) error {
	for _, device := range devices {
		addrs, ok := p.devices[device.Name]
		if !ok {
			continue
		}

		// Check if the address is already in the list to avoid duplicates
		addressToAdd := &device.Addr
		for _, addr := range addrs {
			if addr == addressToAdd {
				// Address already exists, skip adding it again
				continue
			}
		}

		// Add the address to the list
		p.devices[device.Name] = append(p.devices[device.Name], addressToAdd)
	}

	return nil
}

func (p *PCI) loadSupportedDevices() (map[HexID]*Vendor, error) {
	fd, err := os.Open(p.deviceFilePath)
	if err != nil {
		return nil, err
	}
	defer osutils.CloseWithErrorLogging(fd, fmt.Sprintf("error closing file. Path: %s", fd.Name()), &p.log)

	var devices DeviceList
	err = yaml.NewDecoder(fd).Decode(&devices)
	if err != nil {
		return nil, err
	}

	val := validator.New()
	err = val.Struct(devices)
	if err != nil {
		return nil, err
	}

	deviceMap := make(map[HexID]*Vendor, len(devices.Vendors))
	for _, vendor := range devices.Vendors {
		vendor.loadedDevices = make(map[HexID]*Device, len(vendor.Devices))
		deviceMap[vendor.ID] = vendor

		for _, device := range vendor.Devices {
			vendor.loadedDevices[device.ID] = device
		}
	}

	return deviceMap, nil
}

func (p *PCI) discoverDevices() error {
	supportedDevices, err := p.loadSupportedDevices()
	if err != nil {
		return err
	}

	dirEntries, err := os.ReadDir(sysPCIDevicesFolder)
	if err != nil {
		return fmt.Errorf("error reading PCI devices: %w", err)
	}

	for _, entry := range dirEntries {
		devicePath := filepath.Join(sysPCIDevicesFolder, entry.Name())
		err = p.processPCIDevice(supportedDevices, devicePath)
		if err != nil {
			p.log.Error(err, "error processing PCI device", "device", entry.Name())
		}
	}

	return nil
}

func (p *PCI) processPCIDevice(supportedDevices map[HexID]*Vendor, deviceFolder string) error {
	vendorID, err := p.readPCIAttribute(deviceFolder, attributeVendor)
	if err != nil {
		return err
	}

	vendor, vendorExists := supportedDevices[HexID(vendorID)]
	if !vendorExists {
		return fmt.Errorf("unsupported vendor ID: %s", vendorID)
	}

	classID, err := p.readPCIAttribute(deviceFolder, attributeClass)
	if err != nil {
		return err
	}

	device, deviceExists := vendor.loadedDevices[HexID(classID)]
	if !deviceExists {
		return fmt.Errorf("unsupported class ID: %s for vendor: %s", classID, vendor.Name)
	}

	pciAddr, err := parsePCIAddress(filepath.Base(deviceFolder))
	if err != nil {
		return err
	}

	resourceName := core.ResourceName(fmt.Sprintf("%s.%s/%s", device.Type, vendor.Name, device.Name))
	quantity := p.availableResources[resourceName]
	quantity.Add(*resource.NewQuantity(1, resource.DecimalSI))
	p.availableResources[resourceName] = quantity

	p.devices[resourceName] = append(p.devices[resourceName], pciAddr)
	return nil
}

func (p *PCI) readPCIAttribute(devicePath, attributeName string) (string, error) {
	attributePath := filepath.Join(devicePath, attributeName)
	file, err := os.Open(attributePath)
	if err != nil {
		return "", err
	}

	defer osutils.CloseWithErrorLogging(file, fmt.Sprintf("error closing file. Path: %s", file.Name()), &p.log)

	// attributeFileSize is higher as file contant can be.
	const attributeFileSize = 16
	buff := make([]byte, attributeFileSize)

	n, err := file.Read(buff)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}

	if n == attributeFileSize {
		return "", fmt.Errorf("file %s has bigger content as expected", file.Name())
	}

	s := string(buff[:n])

	return strings.ToLower(strings.TrimSpace(s)), nil
}

func parsePCIAddress(address string) (*api.PCIAddress, error) {
	var domainStr, busStr, slotStr, functionStr string
	_, err := fmt.Sscanf(address, "%4s:%2s:%2s.%1s", &domainStr, &busStr, &slotStr, &functionStr)
	if err != nil {
		return nil, fmt.Errorf("error parsing PCI address: %w", err)
	}

	domain, err := parseHexStringToUint(domainStr)
	if err != nil {
		return nil, fmt.Errorf("error parsing domain: %w", err)
	}

	bus, err := parseHexStringToUint(busStr)
	if err != nil {
		return nil, fmt.Errorf("error parsing bus: %w", err)
	}

	slot, err := parseHexStringToUint(slotStr)
	if err != nil {
		return nil, fmt.Errorf("error parsing slot: %w", err)
	}

	function, err := parseHexStringToUint(functionStr)
	if err != nil {
		return nil, fmt.Errorf("error parsing function: %w", err)
	}

	return &api.PCIAddress{
		Domain:   domain,
		Bus:      bus,
		Slot:     slot,
		Function: function,
	}, nil
}

func parseHexStringToUint(hexStr string) (uint, error) {
	hexValue, err := strconv.ParseUint(hexStr, 16, 32) // Assuming 32-bit uint
	if err != nil {
		return 0, err
	}

	return uint(hexValue), nil
}
