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
	SourcePCI           string = "pci"
	sysPCIDevicesFolder string = "/sys/bus/pci/devices"

	attributeVendor = "vendor"
	attributeClass  = "class"
)

type HexID = string

type DeviceList struct {
	Vendors []*Vendor `yaml:"vendors" validate:"unique=ID"`
}

type Vendor struct {
	ID            HexID     `yaml:"id" validate:"required,hexadecimal"`
	Name          string    `yaml:"name" validate:"required"`
	Devices       []*Device `yaml:"devices" validate:"required,unique=Name"`
	loadedDevices map[HexID]*Device
}

type Device struct {
	ID   HexID  `yaml:"id" validate:"required,hexadecimal"`
	Name string `yaml:"name" validate:"required"`
	Type string `yaml:"type" validate:"required"`
}

type PCI struct {
	deviceFilePath string
	// this can be optimize
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
	availableQuantity, ok := p.availableResources[resource]
	if !ok {
		// this cannot be call ever
		return 0
	}

	return int64(math.Floor(float64(availableQuantity.Value()) / float64(quantity.Value())))
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
	for key, quantity := range p.availableResources {
		dev, ok := requiredResources[key]
		if !ok {
			continue
		}

		newQuantity := quantity
		newQuantity.Sub(dev)
		if newQuantity.Sign() == -1 {
			return nil, fmt.Errorf("failed to allocate resource %s: %w", key, ErrResourceNotAvailable)
		}

		allocatedResources[key] = newQuantity
	}

	for key, quantity := range allocatedResources {
		p.availableResources[key] = quantity
	}

	return allocatedResources, nil
}

func (p *PCI) Deallocate(requiredResources core.ResourceList) []core.ResourceName {
	deallocatedResources := []core.ResourceName{}
	for key, quantity := range requiredResources {
		dev, ok := p.availableResources[key]
		if !ok {
			continue
		}

		dev.Add(quantity)
		p.availableResources[key] = dev
		deallocatedResources = append(deallocatedResources, key)
	}

	return deallocatedResources
}

func (p *PCI) GetAvailableResources() core.ResourceList {
	return p.availableResources.DeepCopy()
}

func (p *PCI) AllocatePCIAddress(resources core.ResourceList) ([]api.PCIDevice, error) {
	domainAddrs := []api.PCIDevice{}
	for key, addrs := range p.devices {
		quantity, ok := resources[key]
		if !ok {
			continue
		}

		availableAddressesCount := int64(len(addrs))

		if quantity.Value() > availableAddressesCount {
			return nil, fmt.Errorf("failed to get pci addresses for device %s: not enough pci addresses", key)
		}

		for i := int64(0); i < quantity.Value(); i++ {
			domainAddrs = append(domainAddrs, api.PCIDevice{Addr: *addrs[i], Name: key})
		}

		if quantity.Value() == availableAddressesCount {
			p.devices[key] = []*api.PCIAddress{}
		} else {
			p.devices[key] = addrs[quantity.Value():]
		}
	}

	return domainAddrs, nil
}

func (p *PCI) DeallocatePCIAddress(devices []api.PCIDevice) error {
	for index := range devices {
		addrs, ok := p.devices[devices[index].Name]
		if !ok {
			continue
		}

		addrs = append(addrs, &devices[index].Addr)
		p.devices[devices[index].Name] = addrs
	}

	return nil
}

func (p *PCI) loadSupportedDevices() (map[HexID]*Vendor, error) {
	fd, err := os.Open(p.deviceFilePath)
	if err != nil {
		return nil, err
	}

	devices := DeviceList{}
	decoder := yaml.NewDecoder(fd)
	err = decoder.Decode(&devices)
	if err != nil {
		return nil, err
	}

	val := validator.New()
	err = val.Struct(devices)
	if err != nil {
		return nil, err
	}

	deviceList := make(map[HexID]*Vendor, len(devices.Vendors))
	for _, vendor := range devices.Vendors {
		deviceList[strings.ToLower(vendor.ID)] = vendor
		vendor.loadedDevices = make(map[HexID]*Device, len(vendor.Devices))
		for _, device := range vendor.Devices {
			vendor.loadedDevices[strings.ToLower(device.ID)] = device
		}
	}

	return deviceList, nil
}

func (p *PCI) discoverDevices() error {
	devices, err := p.loadSupportedDevices()
	if err != nil {
		return err
	}

	dirEntries, err := os.ReadDir(sysPCIDevicesFolder)
	if err != nil {
		return fmt.Errorf("error reading the pci devices from %s: %w", sysPCIDevicesFolder, err)
	}

	for _, entry := range dirEntries {
		deviceFolder := filepath.Join(sysPCIDevicesFolder, entry.Name())

		err = p.processPCIDevice(devices, deviceFolder)
		if err != nil {
			p.log.Error(err, "error processing PCI device", "Device", entry.Name())
			continue
		}
	}

	return nil
}

func (p *PCI) processPCIDevice(supportedDevices map[HexID]*Vendor, folder string) error {
	vendorID, err := p.readPCIAttribute(folder, attributeVendor)
	if err != nil {
		return err
	}

	vendor, ok := supportedDevices[vendorID]
	if !ok {
		return nil
	}

	classID, err := p.readPCIAttribute(folder, attributeClass)
	if err != nil {
		return err
	}

	device, ok := vendor.loadedDevices[classID]
	if !ok {
		return nil
	}

	pciAddr, err := parsePCIAddress(filepath.Base(folder))
	if err != nil {
		return err
	}

	resourceName := core.ResourceName(device.Type + "." + vendor.Name + "/" + device.Name)

	quantity := p.availableResources[resourceName]
	quantity.Add(*resource.NewQuantity(1, resource.DecimalSI))
	p.availableResources[resourceName] = quantity

	addresses := p.devices[resourceName]
	if len(addresses) == 0 {
		addresses = []*api.PCIAddress{pciAddr}
	} else {
		addresses = append(addresses, pciAddr)
	}

	p.devices[resourceName] = addresses

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
		return nil, fmt.Errorf("error parsing domain to uint: %w", err)
	}

	bus, err := parseHexStringToUint(busStr)
	if err != nil {
		return nil, fmt.Errorf("error parsing bus to uint: %w", err)
	}

	slot, err := parseHexStringToUint(slotStr)
	if err != nil {
		return nil, fmt.Errorf("error parsing slot to uint: %w", err)
	}

	function, err := parseHexStringToUint(functionStr)
	if err != nil {
		return nil, fmt.Errorf("error parsing function to uint: %w", err)
	}

	addr := api.PCIAddress{
		Domain:   domain,
		Bus:      bus,
		Slot:     slot,
		Function: function,
	}

	return &addr, nil
}

func parseHexStringToUint(hexStr string) (uint, error) {
	hexValue, err := strconv.ParseUint(hexStr, 16, 32) // Assuming 32-bit uint
	if err != nil {
		return 0, err
	}

	return uint(hexValue), nil
}
