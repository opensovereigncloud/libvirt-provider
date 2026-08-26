// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package sources

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	tmpDir   string
	yamlPath string
	pci      *PCI
)

const (
	testVendorID   = "0x10de"
	testVendorName = "NVIDIA"

	newDeviceName        = "MellanoxNIC"
	newDeviceType        = "network"
	newDeviceID          = "0x1db6"
	newSubsystemVendorID = "0x15b3"
	newSubsystemDeviceID = "0x2000"
	newRevision          = "0xa1"

	deviceIDWithWhitespaces = " 0x0200 "

	permTestFile   = 0o644
	permTestFolder = 0o755
)

func TestServer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Sources Suite")
}
