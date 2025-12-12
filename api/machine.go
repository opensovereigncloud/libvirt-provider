// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"slices"
	"strings"
	"time"

	core "github.com/ironcore-dev/ironcore/api/core/v1alpha1"
	"k8s.io/utils/ptr"
	"libvirt.org/go/libvirtxml"
)

type Machine struct {
	Metadata `json:"metadata,omitempty"`

	Spec   MachineSpec   `json:"spec"`
	Status MachineStatus `json:"status"`
}

func (m *Machine) GetState() string {
	if m.Status.State != "" {
		return string(m.Status.State)
	}

	return string(MachineStatePending)
}

// Unify function unify object.
// It mainly sorts slices according to key.
// It will avoid triggering the reconciliation loop by different orders of items.
// Another benefit is better readability of machine in store.
func (m *Machine) Unify() {
	m.Spec.SortVolumes()
	m.Spec.SortNetworkInterfaces()

	m.Status.SortPCIDevices()
	m.Status.SortVolumes()
	m.Status.SortNetworkInterfaces()
}

type MachineSpec struct {
	Power PowerState `json:"power"`

	Resources core.ResourceList `json:"resources,omitempty"`

	Ignition []byte `json:"ignition"`

	Volumes           []*VolumeSpec           `json:"volumes"`
	NetworkInterfaces []*NetworkInterfaceSpec `json:"networkInterfaces"`

	ShutdownAt time.Time `json:"shutdownAt,omitempty"`

	GuestAgent GuestAgent `json:"guestAgent"`

	PCIControllerTotal int `json:"pciControllerTotal"`
}

func (m *MachineSpec) SortVolumes() {
	slices.SortStableFunc(m.Volumes, func(x, y *VolumeSpec) int {
		return strings.Compare(x.Name, y.Name)
	})
}

func (m *MachineSpec) SortNetworkInterfaces() {
	slices.SortStableFunc(m.NetworkInterfaces, func(x, y *NetworkInterfaceSpec) int {
		return strings.Compare(x.Name, y.Name)
	})

	for _, inf := range m.NetworkInterfaces {
		// It is sorted as string, not as IP address.
		slices.SortStableFunc(inf.Ips, func(x, y string) int {
			return strings.Compare(x, y)
		})
	}
}

type GuestAgent string

const (
	GuestAgentNone GuestAgent = "None"
	GuestAgentQemu GuestAgent = "Qemu"
)

type MachineStatus struct {
	VolumeStatus            []VolumeStatus           `json:"volumeStatus"`
	NetworkInterfaceStatus  []NetworkInterfaceStatus `json:"networkInterfaceStatus"`
	State                   MachineState             `json:"state"`
	ImageRef                string                   `json:"imageRef"`
	GuestAgentStatus        *GuestAgentStatus        `json:"guestAgentStatus,omitempty"`
	PCIDevices              []PCIDevice              `json:"pciDevices"`
	PCIControllersAllocated int                      `json:"pciControllersAllocated"`
}

func (m *MachineStatus) SortPCIDevices() {
	slices.SortStableFunc(m.PCIDevices, func(x, y PCIDevice) int {
		resourceCmp := strings.Compare(string(x.Name), string(y.Name))
		if resourceCmp != 0 {
			return resourceCmp
		}

		return x.Addr.Compare(y.Addr)
	})
}

func (m *MachineStatus) SortVolumes() {
	slices.SortStableFunc(m.VolumeStatus, func(x, y VolumeStatus) int {
		return strings.Compare(x.Name, y.Name)
	})
}

func (m *MachineStatus) SortNetworkInterfaces() {
	slices.SortStableFunc(m.NetworkInterfaceStatus, func(x, y NetworkInterfaceStatus) int {
		return strings.Compare(x.Name, y.Name)
	})
}

type MachineState string

const (
	MachineStatePending     MachineState = "Pending"
	MachineStateRunning     MachineState = "Running"
	MachineStateSuspended   MachineState = "Suspended"
	MachineStateTerminating MachineState = "Terminating"
	MachineStateTerminated  MachineState = "Terminated"
)

type PowerState int32

const (
	PowerStatePowerOn  PowerState = 0
	PowerStatePowerOff PowerState = 1
)

type VolumeSpec struct {
	Name       string            `json:"name"`
	Device     string            `json:"device"`
	LocalDisk  *LocalDiskSpec    `json:"localDisk,omitempty"`
	Connection *VolumeConnection `json:"cephDisk,omitempty"`
}

type VolumeStatus struct {
	Name   string      `json:"name,omitempty"`
	Handle string      `json:"handle,omitempty"`
	State  VolumeState `json:"state,omitempty"`
	Size   int64       `json:"size,omitempty"`
}

type LocalDiskSpec struct {
	Size  int64   `json:"size"`
	Image *string `json:"image"`
}

type VolumeConnection struct {
	Driver                string            `json:"driver,omitempty"`
	Handle                string            `json:"handle,omitempty"`
	Attributes            map[string]string `json:"attributes,omitempty"`
	SecretData            map[string][]byte `json:"secret_data,omitempty"`
	EncryptionData        map[string][]byte `json:"encryption_data,omitempty"`
	EffectiveStorageBytes int64             `json:"effective_storage_bytes,omitempty"`
}

type VolumeState string

const (
	VolumeStatePending  VolumeState = "Pending"
	VolumeStateAttached VolumeState = "Attached"
)

type NetworkInterfaceSpec struct {
	Name       string            `json:"name"`
	NetworkId  string            `json:"networkId"`
	Ips        []string          `json:"ips"`
	Attributes map[string]string `json:"attributes"`
}

type NetworkInterfaceStatus struct {
	Name   string                `json:"name"`
	Handle string                `json:"handle"`
	State  NetworkInterfaceState `json:"state"`
}

type NetworkInterfaceState string

const (
	NetworkInterfaceStatePending  NetworkInterfaceState = "Pending"
	NetworkInterfaceStateAttached NetworkInterfaceState = "Attached"
)

type GuestAgentStatus struct {
	Addr string `json:"addr,omitempty"`
}

type PCIDevice struct {
	Addr PCIAddress
	Name core.ResourceName
}

type PCIAddress struct {
	Domain   uint
	Bus      uint
	Slot     uint
	Function uint
}

func (a PCIAddress) Compare(m PCIAddress) int {
	if a.Domain != m.Domain {
		if a.Domain > m.Domain {
			return 1
		}
		return -1
	}

	if a.Bus != m.Bus {
		if a.Bus > m.Bus {
			return 1
		}
		return -1
	}

	if a.Slot != m.Slot {
		if a.Slot > m.Slot {
			return 1
		}
		return -1
	}

	if a.Function != m.Function {
		if a.Function > m.Function {
			return 1
		}
		return -1
	}

	return 0
}

func (p PCIAddress) GetDomainSubsysPCI() *libvirtxml.DomainHostdevSubsysPCI {
	return &libvirtxml.DomainHostdevSubsysPCI{
		Source: &libvirtxml.DomainHostdevSubsysPCISource{
			Address: &libvirtxml.DomainAddressPCI{
				Domain:   &p.Domain,
				Bus:      &p.Bus,
				Slot:     &p.Slot,
				Function: &p.Function,
			},
		},
	}
}

func (m *MachineStatus) GetVolumesAsMap() map[string]*VolumeStatus {
	if m == nil {
		return map[string]*VolumeStatus{}
	}

	result := make(map[string]*VolumeStatus, len(m.VolumeStatus))
	for index := range m.VolumeStatus {
		result[m.VolumeStatus[index].Name] = &m.VolumeStatus[index]
	}

	return result
}

func (m *MachineStatus) GetNetworkInterfacesAsMap() map[string]*NetworkInterfaceStatus {
	if m == nil {
		return map[string]*NetworkInterfaceStatus{}
	}

	results := make(map[string]*NetworkInterfaceStatus, len(m.NetworkInterfaceStatus))
	for index := range m.NetworkInterfaceStatus {
		results[m.NetworkInterfaceStatus[index].Name] = &m.NetworkInterfaceStatus[index]
	}

	return results
}

func HasBootImage(machine *Machine) *string {
	for _, volume := range machine.Spec.Volumes {
		if volume.LocalDisk == nil {
			continue
		}

		if volume.LocalDisk.Image != nil {
			return volume.LocalDisk.Image
		}
	}
	return nil
}

func IsImageReferenced(machine *Machine, image string) bool {
	bootImage := HasBootImage(machine)
	if bootImage == nil {
		return false
	}

	return ptr.Deref(bootImage, "") == image
}
