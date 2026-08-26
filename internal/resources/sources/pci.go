// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package sources

import (
	"context"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"strconv"

	"github.com/go-logr/logr"
	"github.com/go-playground/validator/v10"
	core "github.com/ironcore-dev/ironcore/api/core/v1alpha1"
	"github.com/ironcore-dev/libvirt-provider/api"
	"github.com/ironcore-dev/libvirt-provider/internal/metrics"
	"github.com/ironcore-dev/libvirt-provider/internal/osutils"
	internalutils "github.com/ironcore-dev/libvirt-provider/internal/utils"
	"github.com/prometheus/client_golang/prometheus"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	SourcePCI = "pci"

	attributeVendor          = "vendor"
	attributeDevice          = "device"
	attributeSubsystemVendor = "subsystem_vendor"
	attributeSubsystemDevice = "subsystem_device"
	attributeRevision        = "revision"
)

type HexID = string

// DeviceList holds a list of vendors and validates unique IDs
type DeviceList struct {
	Vendors []*Vendor `yaml:"vendors" validate:"unique=ID,dive"`
}

// Vendor represents a PCI vendor with a list of devices
type Vendor struct {
	ID            HexID     `yaml:"id" validate:"required,hexadecimal"`
	Name          string    `yaml:"name" validate:"required"`
	Devices       []*Device `yaml:"devices" validate:"required,dive"`
	loadedDevices map[HexID]*Device
}

// Device represents a PCI device
type Device struct {
	ID              HexID  `yaml:"id" validate:"required,hexadecimal"`
	Name            string `yaml:"name" validate:"required"`
	Type            string `yaml:"type" validate:"required"`
	SubsystemVendor HexID  `yaml:"subsystemVendor,omitempty" validate:"required_with=SubsystemDevice,omitempty,hexadecimal"`
	SubsystemDevice HexID  `yaml:"subsystemDevice,omitempty" validate:"required_with=Revision,omitempty,hexadecimal"`
	Revision        HexID  `yaml:"revision,omitempty" validate:"required_with=SubsystemVendor,omitempty,hexadecimal"`
}

func (d *Device) getKey() string {
	return d.ID + d.SubsystemVendor + d.SubsystemDevice + d.Revision
}

// PCI manages the allocation and deallocation of PCI devices and their resources.
type PCI struct {
	deviceFilePath string
	devices        map[core.ResourceName][]*api.PCIAddress
	log            logr.Logger
}

func NewSourcePCI(options Options) *PCI {
	return &PCI{
		deviceFilePath: options.PCIDevicesFile,
		devices:        map[core.ResourceName][]*api.PCIAddress{},
		log:            options.Log.WithName(SourcePCI),
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
	if availableQuantity := len(p.devices[resource]); availableQuantity > 0 {
		return int64(math.Floor(float64(availableQuantity) / float64(quantity.Value())))
	}
	return 0
}

func (p *PCI) Init(ctx context.Context) (sets.Set[core.ResourceName], error) {
	err := p.discoverDevices(internalutils.FolderSysPCIDevices)
	if err != nil {
		return nil, err
	}

	supportedResources := make(sets.Set[core.ResourceName], len(p.devices))
	for key := range p.devices {
		supportedResources.Insert(key)
	}

	return supportedResources, nil
}

func (p *PCI) Allocate(machine *api.Machine, requiredResources core.ResourceList) (core.ResourceList, error) {
	allocatedResources := core.ResourceList{}
	var allocatedPCIDevices []api.PCIDevice
	tempAvailableResources := maps.Clone(p.devices)

	// First pass: Check availability without modifying actual available resources
	for resourceName, requiredQty := range requiredResources {
		availableDevices, exists := tempAvailableResources[resourceName]
		if !exists {
			continue
		}

		if int64(len(availableDevices)) < requiredQty.Value() {
			return nil, fmt.Errorf("failed to allocate resource %s: %w", resourceName, ErrResourceNotAvailable)
		}

		for i := int64(0); i < requiredQty.Value(); i++ {
			allocatedPCIDevices = append(allocatedPCIDevices, api.PCIDevice{
				Addr: *availableDevices[i],
				Name: resourceName,
			})
		}

		tempAvailableResources[resourceName] = availableDevices[requiredQty.Value():]
		allocatedResources[resourceName] = requiredQty
	}

	// Second pass: Update the actual available resources after confirming allocation
	p.devices = tempAvailableResources

	machine.Status.PCIDevices = allocatedPCIDevices

	return allocatedResources, nil
}

func (p *PCI) Deallocate(machine *api.Machine, requiredResources core.ResourceList) []core.ResourceName {
	deallocatedResources := []core.ResourceName{}

	for _, device := range machine.Status.PCIDevices {
		if addrs, ok := p.devices[device.Name]; ok {
			p.devices[device.Name] = append(addrs, &device.Addr)
			deallocatedResources = append(deallocatedResources, device.Name)
		}
	}

	machine.Status.PCIDevices = nil

	return deallocatedResources
}

func (p *PCI) GetAvailableResources() core.ResourceList {
	availableResources := make(core.ResourceList, len(p.devices))
	for resourceName, addrs := range p.devices {
		availableResources[resourceName] = *resource.NewQuantity(int64(len(addrs)), resource.DecimalSI)
	}
	return availableResources
}

func (p *PCI) SetResourcesMetric(metric *prometheus.GaugeVec) {
	for resourceName, addrs := range p.devices {
		labels := prometheus.Labels{
			metrics.LabelSource:   p.GetName(),
			metrics.LabelResource: string(resourceName),
		}

		pciGauge, err := metrics.GetGaugeWithLabels(metric, labels)
		if err != nil {
			p.log.Error(err, "failed to get pci metric", metrics.LogKeyLabels, labels)
		}
		pciGauge.Set(float64(len(addrs)))
	}
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

	validate := validator.New()
	err = validate.Struct(devices)
	if err != nil {
		return nil, err
	}

	deviceMap := make(map[HexID]*Vendor, len(devices.Vendors))
	for _, vendor := range devices.Vendors {
		vendor.loadedDevices = make(map[HexID]*Device, len(vendor.Devices))
		deviceMap[vendor.ID] = vendor

		for _, device := range vendor.Devices {
			vendor.loadedDevices[device.getKey()] = device
		}
	}

	return deviceMap, nil
}

func (p *PCI) discoverDevices(pciDevicesPath string) error {
	supportedDevices, err := p.loadSupportedDevices()
	if err != nil {
		return err
	}

	dirEntries, err := os.ReadDir(pciDevicesPath)
	if err != nil {
		return fmt.Errorf("error reading PCI devices: %w", err)
	}

	for _, entry := range dirEntries {
		devicePath := filepath.Join(pciDevicesPath, entry.Name())
		err = p.processPCIDevice(supportedDevices, devicePath)
		if err != nil {
			p.log.V(2).Info("error processing PCI device", "hostDevice", entry.Name(), "error", err)
		}
	}

	return nil
}

func (p *PCI) processPCIDevice(supportedDevices map[HexID]*Vendor, deviceFolder string) error {
	vendorID, err := internalutils.ReadPCIAttribute(&p.log, deviceFolder, attributeVendor)
	if err != nil {
		return err
	}

	vendor, vendorExists := supportedDevices[HexID(vendorID)]
	if !vendorExists {
		return fmt.Errorf("unsupported vendor ID: %s", vendorID)
	}

	deviceID, err := internalutils.ReadPCIAttribute(&p.log, deviceFolder, attributeDevice)
	if err != nil {
		return err
	}

	subsystemDeviceID, err := internalutils.ReadPCIAttribute(&p.log, deviceFolder, attributeSubsystemDevice)
	if err != nil {
		return err
	}

	subsystemVendorID, err := internalutils.ReadPCIAttribute(&p.log, deviceFolder, attributeSubsystemVendor)
	if err != nil {
		return err
	}

	revision, err := internalutils.ReadPCIAttribute(&p.log, deviceFolder, attributeRevision)
	if err != nil {
		return err
	}

	key := (&Device{
		ID:              deviceID,
		SubsystemDevice: subsystemDeviceID,
		SubsystemVendor: subsystemVendorID,
		Revision:        revision,
	}).getKey()

	device, exists := vendor.loadedDevices[key]
	if !exists {
		return fmt.Errorf(
			"unsupported YAML device: "+
				"vendorID=%s, deviceID=%s, subsystemVendorID=%s, subsystemDeviceID=%s, revision=%s",
			vendorID, deviceID, subsystemVendorID, subsystemDeviceID, revision)
	}
	pciAddr, err := parsePCIAddress(filepath.Base(deviceFolder))
	if err != nil {
		return err
	}
	resourceName := core.ResourceName(fmt.Sprintf("%s.%s/%s", device.Type, vendor.Name, device.Name))
	p.devices[resourceName] = append(p.devices[resourceName], pciAddr)
	return nil
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
