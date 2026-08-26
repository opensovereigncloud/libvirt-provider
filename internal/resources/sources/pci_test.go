// // SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// // SPDX-License-Identifier: Apache-2.0

package sources

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

func writeDeviceListToFile(path string, list *DeviceList) {
	bytes, err := yaml.Marshal(list)
	Expect(err).NotTo(HaveOccurred(), "failed to marshal YAML")
	Expect(os.WriteFile(path, bytes, permTestFile)).To(Succeed(), "failed to write YAML file")
}

func createPCIPath(basePath, devName string, attrs map[string]string) string {
	devPath := filepath.Join(basePath, devName)
	Expect(os.Mkdir(devPath, permTestFolder)).To(Succeed(), "failed to create device path")
	for attr, val := range attrs {
		Expect(os.WriteFile(filepath.Join(devPath, attr), []byte(val+"\n"), permTestFile)).To(Succeed(), "failed to write attribute: "+attr)
	}
	return devPath
}

var _ = Describe("PCI Device Manager", func() {
	BeforeEach(func() {
		tmpDir = GinkgoT().TempDir()
		yamlPath = filepath.Join(tmpDir, "pci_devices.yaml")
		pci = NewSourcePCI(Options{
			PCIDevicesFile: yamlPath,
			Log:            logr.Discard(),
		})
	})

	DescribeTable("device list YAML validation",
		func(vendors []*Vendor, expectErr bool, errMatcher func(error)) {
			deviceList := &DeviceList{
				Vendors: vendors,
			}
			writeDeviceListToFile(yamlPath, deviceList)
			_, err := pci.loadSupportedDevices()
			if expectErr {
				Expect(err).To(HaveOccurred())
				if errMatcher != nil {
					errMatcher(err)
				}
			} else {
				Expect(err).NotTo(HaveOccurred())
			}
		},
		Entry("valid format devices",
			[]*Vendor{
				{
					ID:   testVendorID,
					Name: testVendorName,
					Devices: []*Device{
						{
							ID:              newDeviceID,
							SubsystemVendor: newSubsystemVendorID,
							SubsystemDevice: newSubsystemDeviceID,
							Revision:        newRevision,
							Name:            newDeviceName,
							Type:            newDeviceType,
						},
					},
				},
			},
			false,
			nil,
		),
		Entry("device attribute with whitespaces",
			[]*Vendor{
				{
					ID:   testVendorID,
					Name: testVendorName,
					Devices: []*Device{
						{
							ID:              deviceIDWithWhitespaces,
							SubsystemVendor: newSubsystemVendorID,
							Name:            newDeviceName,
							Type:            newDeviceType,
						},
					},
				},
			},
			true,
			func(err error) {
				Expect(err).To(MatchError(ContainSubstring("'ID' failed on the 'hexadecimal' tag")))
			},
		),
		Entry("correct format field set",
			[]*Vendor{
				{
					ID:   testVendorID,
					Name: testVendorName,
					Devices: []*Device{
						{
							ID:              newDeviceID,
							SubsystemVendor: newSubsystemVendorID,
							Name:            newDeviceName,
							Type:            newDeviceType,
						},
					},
				},
			},
			true,
			func(err error) {
				Expect(err).To(MatchError(ContainSubstring("failed on the 'required_with' tag")))
			},
		),
		Entry("duplicate vendors with same ID",
			[]*Vendor{
				{
					ID:   testVendorID,
					Name: testVendorName,
					Devices: []*Device{
						{ID: newDeviceID, Name: newDeviceName, Type: newDeviceType},
					},
				},
				{
					ID:   testVendorID,
					Name: testVendorName,
					Devices: []*Device{
						{ID: newDeviceID, Name: newDeviceName, Type: newDeviceType},
					},
				},
			},
			true,
			func(err error) {
				Expect(err.Error()).To(ContainSubstring("'Vendors' failed on the 'unique' tag"))
			},
		),
	)

	Describe("device discovery", func() {
		BeforeEach(func() {
			deviceList := &DeviceList{
				Vendors: []*Vendor{
					{
						ID:   testVendorID,
						Name: testVendorName,
						Devices: []*Device{
							{
								ID:              newDeviceID,
								SubsystemVendor: newSubsystemVendorID,
								SubsystemDevice: newSubsystemDeviceID,
								Revision:        newRevision,
								Name:            newDeviceName,
								Type:            newDeviceType,
							},
						},
					},
				},
			}
			writeDeviceListToFile(yamlPath, deviceList)

			createPCIPath(tmpDir, "0000:8a:00.1", map[string]string{
				"vendor":           testVendorID,
				"device":           newDeviceID,
				"subsystem_vendor": newSubsystemVendorID,
				"subsystem_device": newSubsystemDeviceID,
				"revision":         newRevision,
			})
		})

		It("matches device format by composite key", func() {
			Expect(pci.discoverDevices(tmpDir)).To(Succeed())
			res := pci.GetAvailableResources()
			Expect(res).To(HaveLen(1))
			for name := range res {
				Expect(string(name)).To(HavePrefix("network.NVIDIA/MellanoxNIC"))
			}
		})

		It("skips unsupported vendor", func() {
			createPCIPath(tmpDir, "0000:99:00.0", map[string]string{
				"vendor": "0x9999",
				"device": "0x0001",
				"class":  newDeviceID,
			})
			Expect(pci.discoverDevices(tmpDir)).To(Succeed())
			res := pci.GetAvailableResources()
			Expect(res).To(HaveLen(1)) // From initial setup
		})

		It("handles invalid sysfs entries without crash", func() {
			badPath := filepath.Join(tmpDir, "0000:88:00.0")
			Expect(os.Mkdir(badPath, permTestFolder)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(badPath, "vendor"), []byte("not-a-hex\n"), permTestFile)).To(Succeed())

			Expect(pci.discoverDevices(tmpDir)).To(Succeed())
		})
	})

	Describe("PCI Device Resource Grouping and Validation", func() {
		When("configurations of both the host devices matches with each other", func() {
			It("groups devices with same resource name", func() {
				deviceList := &DeviceList{
					Vendors: []*Vendor{
						{
							ID:   testVendorID,
							Name: testVendorName,
							Devices: []*Device{
								{
									ID:              newDeviceID,
									SubsystemVendor: newSubsystemVendorID,
									SubsystemDevice: newSubsystemDeviceID,
									Revision:        newRevision,
									Name:            newDeviceName,
									Type:            newDeviceType,
								},
							},
						},
					},
				}
				writeDeviceListToFile(yamlPath, deviceList)

				createPCIPath(tmpDir, "0000:8a:00.1", map[string]string{
					"vendor":           testVendorID,
					"device":           newDeviceID, // "0x1db6"
					"subsystem_vendor": newSubsystemVendorID,
					"subsystem_device": newSubsystemDeviceID,
					"revision":         newRevision,
				})
				createPCIPath(tmpDir, "0000:8a:00.2", map[string]string{
					"vendor":           testVendorID,
					"device":           newDeviceID,
					"subsystem_vendor": newSubsystemVendorID,
					"subsystem_device": newSubsystemDeviceID,
					"revision":         newRevision,
				})

				Expect(pci.discoverDevices(tmpDir)).To(Succeed())
				res := pci.GetAvailableResources()
				Expect(res).To(HaveLen(1))

				for name, devices := range res {
					Expect(string(name)).To(Equal(fmt.Sprintf("%s.%s/%s", newDeviceType, testVendorName, newDeviceName)))
					Expect(devices.Value()).To(BeEquivalentTo(2))
				}
			})
		})

		When("configurations of both the host devices differs from each other", func() {
			It("forcefully groups devices with same resource name", func() {
				deviceList := &DeviceList{
					Vendors: []*Vendor{
						{
							ID:   testVendorID,
							Name: testVendorName,
							Devices: []*Device{
								{
									ID:              newDeviceID,
									SubsystemVendor: newSubsystemVendorID,
									SubsystemDevice: newSubsystemDeviceID,
									Revision:        newRevision,
									Name:            newDeviceName,
									Type:            newDeviceType,
								},
								{
									ID:              newDeviceID,
									SubsystemVendor: newSubsystemVendorID,
									SubsystemDevice: newSubsystemDeviceID,
									Revision:        "0x00",
									Name:            newDeviceName,
									Type:            newDeviceType,
								},
							},
						},
					},
				}
				writeDeviceListToFile(yamlPath, deviceList)

				createPCIPath(tmpDir, "0000:8a:00.1", map[string]string{
					"vendor":           testVendorID,
					"device":           newDeviceID,
					"subsystem_vendor": newSubsystemVendorID,
					"subsystem_device": newSubsystemDeviceID,
					"revision":         newRevision,
				})
				createPCIPath(tmpDir, "0000:8a:00.2", map[string]string{
					"vendor":           testVendorID,
					"device":           newDeviceID,
					"subsystem_vendor": newSubsystemVendorID,
					"subsystem_device": newSubsystemDeviceID,
					"revision":         "0x00",
				})

				Expect(pci.discoverDevices(tmpDir)).To(Succeed())
				res := pci.GetAvailableResources()
				Expect(res).To(HaveLen(1))

				for name, devices := range res {
					Expect(string(name)).To(Equal(fmt.Sprintf("%s.%s/%s", newDeviceType, testVendorName, newDeviceName)))
					Expect(devices.Value()).To(BeEquivalentTo(2))
				}
			})
		})
	})
})
