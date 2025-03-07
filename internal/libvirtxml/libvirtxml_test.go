// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package libvirtxml_test

import (
	"os"
	"path/filepath"

	providerlibvirtxml "github.com/ironcore-dev/libvirt-provider/internal/libvirtxml"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"libvirt.org/go/libvirtxml"
)

var _ = Describe("Domain Merge Function", func() {

	DescribeTable("Basic struct merging",
		func(override, generated, expected *libvirtxml.Domain) {
			final := providerlibvirtxml.MergeDomains(override, generated)
			Expect(final).To(equalNormalized(expected))
		},
		Entry("should merge two non-nil domains correctly",
			&libvirtxml.Domain{Description: "override Domain"},
			&libvirtxml.Domain{Description: "generated Domain"},
			&libvirtxml.Domain{Description: "override Domain"},
		),
		Entry("should return generated domain if override is nil",
			nil,
			&libvirtxml.Domain{Description: "generated Domain"},
			&libvirtxml.Domain{Description: "generated Domain"},
		),
		Entry("should return override domain if generated is nil",
			&libvirtxml.Domain{Description: "override Domain"},
			nil,
			&libvirtxml.Domain{Description: "override Domain"},
		),
		Entry("should return nil if both override and generated are nil",
			nil, nil, nil,
		),
	)

	Context("Handling struct field order", func() {
		It("should merge domains even if fields are of different values", func() {
			override := &libvirtxml.Domain{
				Memory: &libvirtxml.DomainMemory{
					Value: 1024,
				},
			}
			generated := &libvirtxml.Domain{
				Memory: &libvirtxml.DomainMemory{
					Value: 512,
				},
			}
			final := providerlibvirtxml.MergeDomains(override, generated)

			Expect(final.Memory.Value).To(Equal(override.Memory.Value))
		})
	})

	DescribeTable("Handling pointer to struct",
		func(override, generated *libvirtxml.Domain, expected string) {
			final := providerlibvirtxml.MergeDomains(override, generated)
			Expect(final.Metadata.XML).To(Equal(expected))
		},
		Entry("should correctly handle nil pointers",
			&libvirtxml.Domain{Metadata: nil},
			&libvirtxml.Domain{Metadata: &libvirtxml.DomainMetadata{XML: "<data/>"}},
			"<data/>"),

		Entry("should allocate memory for struct pointers when merging",
			&libvirtxml.Domain{Metadata: &libvirtxml.DomainMetadata{XML: "<override/>"}},
			&libvirtxml.Domain{Metadata: &libvirtxml.DomainMetadata{XML: "<generated/>"}},
			"<override/>"),
	)

	Context("Slice merging & deduplication", func() {
		It("should append values from both domains in slices", func() {
			override := &libvirtxml.Domain{
				OS: &libvirtxml.DomainOS{
					BootDevices: []libvirtxml.DomainBootDevice{
						{Dev: "Dev1"},
					},
				},
			}
			generated := &libvirtxml.Domain{
				OS: &libvirtxml.DomainOS{
					BootDevices: []libvirtxml.DomainBootDevice{
						{Dev: "Dev2"},
					},
				},
			}
			final := providerlibvirtxml.MergeDomains(override, generated)

			Expect(final.OS.BootDevices).To(HaveLen(2))
			Expect(final.OS.BootDevices).To(ContainElements(
				override.OS.BootDevices[0],
				generated.OS.BootDevices[0],
			))
		})

		It("should remove duplicates when merging slices", func() {
			override := &libvirtxml.Domain{
				OS: &libvirtxml.DomainOS{
					BootDevices: []libvirtxml.DomainBootDevice{
						{Dev: "Dev1"},
					},
				},
			}
			generated := &libvirtxml.Domain{
				OS: &libvirtxml.DomainOS{
					BootDevices: []libvirtxml.DomainBootDevice{
						{Dev: "Dev1"},
					},
				},
			}
			final := providerlibvirtxml.MergeDomains(override, generated)

			Expect(final.OS.BootDevices).To(HaveLen(1))
			Expect(final.OS.BootDevices).To(ContainElements(
				override.OS.BootDevices[0],
			))
		})

		It("should remove duplicates when merging nested slices of structs", func() {
			override := &libvirtxml.Domain{
				Devices: &libvirtxml.DomainDeviceList{
					Disks: []libvirtxml.DomainDisk{
						{
							Driver: &libvirtxml.DomainDiskDriver{
								Name: "qemu",
							},
							Source: &libvirtxml.DomainDiskSource{
								File: &libvirtxml.DomainDiskSourceFile{
									SecLabel: []libvirtxml.DomainDeviceSecLabel{
										{
											Model: "model1",
										},
									},
								}},
						},
					},
				},
			}
			generated := &libvirtxml.Domain{
				Devices: &libvirtxml.DomainDeviceList{
					Disks: []libvirtxml.DomainDisk{
						{
							Driver: &libvirtxml.DomainDiskDriver{
								Name: "qemu",
							},
							Source: &libvirtxml.DomainDiskSource{
								File: &libvirtxml.DomainDiskSourceFile{
									SecLabel: []libvirtxml.DomainDeviceSecLabel{
										{
											Model: "model1",
										},
									},
								}},
						},
					},
				},
			}
			final := providerlibvirtxml.MergeDomains(override, generated)
			Expect(final.Devices.Disks).To(HaveLen(1))
			Expect(final.Devices.Disks).To(ContainElements(
				override.Devices.Disks[0],
			))
		})
		It("should append values when merging nested slices of structs", func() {
			override := &libvirtxml.Domain{
				Devices: &libvirtxml.DomainDeviceList{
					Disks: []libvirtxml.DomainDisk{
						{
							Driver: &libvirtxml.DomainDiskDriver{
								Name: "qemu",
							},
							Source: &libvirtxml.DomainDiskSource{
								File: &libvirtxml.DomainDiskSourceFile{
									SecLabel: []libvirtxml.DomainDeviceSecLabel{
										{
											Model: "model1",
										},
									},
								}},
						},
					},
				},
			}
			generated := &libvirtxml.Domain{
				Devices: &libvirtxml.DomainDeviceList{
					Disks: []libvirtxml.DomainDisk{
						{
							Driver: &libvirtxml.DomainDiskDriver{
								Name: "qemu",
							},
							Source: &libvirtxml.DomainDiskSource{
								File: &libvirtxml.DomainDiskSourceFile{
									SecLabel: []libvirtxml.DomainDeviceSecLabel{
										{
											Model: "model2",
										},
									},
								}},
						},
					},
				},
			}
			final := providerlibvirtxml.MergeDomains(override, generated)
			Expect(final.Devices.Disks).To(HaveLen(2))
			Expect(final.Devices.Disks).To(ContainElements(
				override.Devices.Disks[0],
				generated.Devices.Disks[0],
			))
		})
	})

	Context("Edge cases & stress testing", func() {
		It("should correctly merge deeply nested structures with mixed types in random order", func() {
			override := &libvirtxml.Domain{
				Devices: &libvirtxml.DomainDeviceList{
					Disks: []libvirtxml.DomainDisk{
						{
							Device: "disk",
							Source: &libvirtxml.DomainDiskSource{
								File: &libvirtxml.DomainDiskSourceFile{
									File: "/override.img",
								},
							},
						},
					},
				},
				Type:        "kvm",
				Description: "override Domain",
				Memory: &libvirtxml.DomainMemory{
					Value:    8192,
					DumpCore: "core1",
				},
				Metadata: &libvirtxml.DomainMetadata{
					XML: "<override_metadata/>",
				},
				OS: &libvirtxml.DomainOS{
					Firmware: "firmware",
					FirmwareInfo: &libvirtxml.DomainOSFirmwareInfo{
						Features: []libvirtxml.DomainOSFirmwareFeature{
							{
								Name: "feature1",
							},
						},
					},
				},
				CPU: &libvirtxml.DomainCPU{
					Mode: "host-passthrough",
					Topology: &libvirtxml.DomainCPUTopology{
						Sockets: 2,
						Cores:   4,
					},
				},
			}

			generated := &libvirtxml.Domain{
				Type:        "qemu",
				Description: "generated Domain",
				Memory: &libvirtxml.DomainMemory{
					Value:    4096,
					DumpCore: "core2",
				},
				CPU: &libvirtxml.DomainCPU{
					Mode: "custom",
					Topology: &libvirtxml.DomainCPUTopology{
						Sockets: 1,
						Cores:   2,
					},
				},
				Metadata: &libvirtxml.DomainMetadata{
					XML: "<generated_metadata/>",
				},
				OS: &libvirtxml.DomainOS{
					FirmwareInfo: &libvirtxml.DomainOSFirmwareInfo{
						Features: []libvirtxml.DomainOSFirmwareFeature{
							{
								Name: "feature2",
							},
						},
					},
				},
				Devices: &libvirtxml.DomainDeviceList{
					Disks: []libvirtxml.DomainDisk{
						{Device: "disk",
							Source: &libvirtxml.DomainDiskSource{
								File: &libvirtxml.DomainDiskSourceFile{
									File: "/generated.img",
								},
							},
						},
					},
				},
			}

			final := providerlibvirtxml.MergeDomains(override, generated)

			Expect(final.Type).To(Equal(override.Type))
			Expect(final.Description).To(Equal(override.Description))
			Expect(final.Memory.Value).To(Equal(override.Memory.Value))
			Expect(final.Memory.DumpCore).To(Equal(override.Memory.DumpCore))
			Expect(final.CPU.Mode).To(Equal(override.CPU.Mode))
			Expect(final.CPU.Topology.Sockets).To(Equal(override.CPU.Topology.Sockets))
			Expect(final.CPU.Topology.Cores).To(Equal(override.CPU.Topology.Cores))

			Expect(final.Metadata.XML).To(Equal(override.Metadata.XML))

			Expect(final.OS.Firmware).To(Equal(override.OS.Firmware))
			Expect(final.OS.FirmwareInfo.Features).To(HaveLen(2))
			Expect(final.OS.FirmwareInfo.Features).To(ContainElements(
				override.OS.FirmwareInfo.Features[0],
				generated.OS.FirmwareInfo.Features[0],
			))

			Expect(final.Devices.Disks).To(HaveLen(2))
			Expect(final.Devices.Disks).To(ContainElements(
				override.Devices.Disks[0],
				generated.Devices.Disks[0],
			))
		})
	})
})

var _ = Describe("LoadOverrideDomainXML", func() {
	createTestFile := func(filename, content string) string {
		filePath := filepath.Join(tempDir, filename)
		err := os.WriteFile(filePath, []byte(content), 0644)
		Expect(err).ToNot(HaveOccurred())
		return filePath
	}

	It("should correctly parse a valid domain XML file", func() {
		xmlData := `<domain type="kvm">
			<name>test-vm</name>
			<memory unit="KiB">8192</memory>
		</domain>`

		testFile := createTestFile("valid.xml", xmlData)

		domain, err := providerlibvirtxml.LoadOverrideDomainXML(testFile)
		Expect(err).ToNot(HaveOccurred())
		Expect(domain.Type).To(Equal("kvm"))
		Expect(domain.Name).To(Equal("test-vm"))
		Expect(domain.Memory.Value).To(Equal(uint(8192)))
	})

	It("should return nil if path is empty", func() {
		domain, err := providerlibvirtxml.LoadOverrideDomainXML("")
		Expect(err).ToNot(HaveOccurred())
		Expect(domain).To(BeNil())
	})

	It("should return an error when the file does not exist", func() {
		_, err := providerlibvirtxml.LoadOverrideDomainXML(filepath.Join(tempDir, "file.xml"))
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(providerlibvirtxml.ErrOpenOverrideTemplate))
	})

	It("should return an error when XML is malformed", func() {
		xmlData := `<domain><name>test-vm<name></domain>`

		testFile := createTestFile("malformed.xml", xmlData)

		_, err := providerlibvirtxml.LoadOverrideDomainXML(testFile)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(providerlibvirtxml.ErrDecodeOverrideTemplate))
	})

	It("should return an empty domain struct when file is empty", func() {
		testFile := createTestFile("empty.xml", "")

		_, err := providerlibvirtxml.LoadOverrideDomainXML(testFile)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(providerlibvirtxml.ErrDecodeOverrideTemplate))
	})
})

// normalizeDomain normalize empty slices to nil to avoid test failures due to Go treating `nil` and `[]` differently.
// In Go, an empty slice (`[]Type{}`) and a nil slice (`nil`) are not considered equal when using `reflect.DeepEqual`.
// This ensures consistency when comparing structs in tests.
func normalizeDomain(d *libvirtxml.Domain) *libvirtxml.Domain {
	if d == nil {
		return nil
	}

	// Ensure slices are consistently nil or empty
	if len(d.SysInfo) == 0 {
		d.SysInfo = nil
	}
	if len(d.SecLabel) == 0 {
		d.SecLabel = nil
	}

	return d
}

// equalNormalized is a custom Gomega matcher that compares two *libvirtxml.Domain
// structs after normalizing them. This ensures that fields with nil vs. empty slices
// or other non-significant differences do not cause test failures.
//
// It applies the normalizeDomain function to both the actual and expected values
// before performing the equality check using Gomega’s Equal matcher.
func equalNormalized(expected *libvirtxml.Domain) types.GomegaMatcher {
	return WithTransform(normalizeDomain, Equal(normalizeDomain(expected)))
}
