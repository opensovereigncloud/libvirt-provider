// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package api_test

import (
	core "github.com/ironcore-dev/ironcore/api/core/v1alpha1"
	"github.com/ironcore-dev/libvirt-provider/api"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	dummyVolume1 = "vol1"
	dummyVolume2 = "vol2"
	dummyVolume3 = "vol3"
	dummyVolume4 = "vol4"

	dummyNIC1 = "nic1"
	dummyNIC2 = "nic2"
	dummyNIC3 = "nic3"
	dummyNIC4 = "nic4"

	dummyPCI1 = "pci1"
	dummyPCI2 = "pci2"
)

var _ = Describe("GetExistingPCICount", func() {
	DescribeTable("should return the correct PCI count",
		func(machine *api.Machine, expectedCount int) {
			Expect(api.GetExistingPCICount(machine)).To(Equal(expectedCount))
		},
		Entry("nil machine", nil, 0),
		Entry("empty machine", &api.Machine{}, 0),
		Entry("only volumes in spec", &api.Machine{
			Spec: api.MachineSpec{
				Volumes: []*api.VolumeSpec{
					{Name: dummyVolume1}, {Name: dummyVolume2},
				},
			},
		}, 2),
		Entry("only volumes in status", &api.Machine{
			Status: api.MachineStatus{
				VolumeStatus: []api.VolumeStatus{
					{Name: dummyVolume3}, {Name: dummyVolume4},
				},
			},
		}, 2),
		Entry("volumes overlap between spec and status", &api.Machine{
			Spec: api.MachineSpec{
				Volumes: []*api.VolumeSpec{
					{Name: dummyVolume1}, {Name: dummyVolume2},
				},
			},
			Status: api.MachineStatus{
				VolumeStatus: []api.VolumeStatus{
					{Name: dummyVolume2}, {Name: dummyVolume3},
				},
			},
		}, 3),
		Entry("volume spec has nil entries", &api.Machine{
			Spec: api.MachineSpec{
				Volumes: []*api.VolumeSpec{
					nil, {Name: dummyVolume1},
				},
			},
			Status: api.MachineStatus{
				VolumeStatus: []api.VolumeStatus{
					{Name: dummyVolume2},
				},
			},
		}, 2),
		Entry("only NICs in spec", &api.Machine{
			Spec: api.MachineSpec{
				NetworkInterfaces: []*api.NetworkInterfaceSpec{
					{Name: dummyNIC1}, {Name: dummyNIC2},
				},
			},
		}, 2),
		Entry("only NICs in status", &api.Machine{
			Status: api.MachineStatus{
				NetworkInterfaceStatus: []api.NetworkInterfaceStatus{
					{Name: dummyNIC3}, {Name: dummyNIC4},
				},
			},
		}, 2),
		Entry("NICs overlap between spec and status", &api.Machine{
			Spec: api.MachineSpec{
				NetworkInterfaces: []*api.NetworkInterfaceSpec{
					{Name: dummyNIC1}, {Name: dummyNIC2},
				},
			},
			Status: api.MachineStatus{
				NetworkInterfaceStatus: []api.NetworkInterfaceStatus{
					{Name: dummyNIC2}, {Name: dummyNIC3},
				},
			},
		}, 3),
		Entry("only PCIDevices present", &api.Machine{
			Status: api.MachineStatus{
				PCIDevices: []api.PCIDevice{
					{Name: core.ResourceName(dummyPCI1)}, {Name: core.ResourceName(dummyPCI2)},
				},
			},
		}, 2),
		Entry("unique PCIDevices, volumes and NICs", &api.Machine{
			Spec: api.MachineSpec{
				Volumes: []*api.VolumeSpec{
					{Name: dummyVolume1},
				},
				NetworkInterfaces: []*api.NetworkInterfaceSpec{
					{Name: dummyNIC1},
				},
			},
			Status: api.MachineStatus{
				VolumeStatus: []api.VolumeStatus{
					{Name: dummyVolume2},
				},
				NetworkInterfaceStatus: []api.NetworkInterfaceStatus{
					{Name: dummyNIC2},
				},
				PCIDevices: []api.PCIDevice{
					{Name: core.ResourceName(dummyPCI1)}, {Name: core.ResourceName(dummyPCI2)},
				},
			},
		}, 6),
		Entry("mixed case with overlaps and nils", &api.Machine{
			Spec: api.MachineSpec{
				Volumes: []*api.VolumeSpec{
					{Name: dummyVolume1}, nil, {Name: dummyVolume2},
				},
				NetworkInterfaces: []*api.NetworkInterfaceSpec{
					{Name: dummyNIC1}, {Name: dummyNIC2},
				},
			},
			Status: api.MachineStatus{
				VolumeStatus: []api.VolumeStatus{
					{Name: dummyVolume2}, {Name: dummyVolume3},
				},
				NetworkInterfaceStatus: []api.NetworkInterfaceStatus{
					{Name: dummyNIC2}, {Name: dummyNIC3},
				},
				PCIDevices: []api.PCIDevice{
					{Name: core.ResourceName(dummyPCI1)},
				},
			},
		}, 7),
	)
})
