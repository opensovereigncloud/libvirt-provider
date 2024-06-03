// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package manager

import (
	"context"
	"os"
	"testing"

	"github.com/go-logr/logr"
	"github.com/ironcore-dev/libvirt-provider/internal/resources/sources"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestManager(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Resource Manager Suite")
}

var (
	logger             logr.Logger
	rm                 *resourceManager
	ctx                context.Context
	machineClassesFile *os.File
	err                error
	dummySrc           *sources.Dummy
)

var _ = BeforeSuite(func() {
	logger = zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true))

	machineClassData := `[
		{
			"name": "extra-huge",
			"capabilities": {
				"cpu": 2000,
				"memory": 16106127361
			}
		},
		{
			"name": "t3-small",
			"capabilities": {
				"cpu": 2000,
				"memory": 2147483648,
				"memory.epc.sgx": 2147483648
			}
		},
		{
			"name": "x3-xlarge",
			"capabilities": {
				"cpu": 4000,
				"memory": 8589934592
			}
		}
	]`

	machineClassesFile, err = os.CreateTemp(GinkgoT().TempDir(), "machineclasses")
	Expect(err).NotTo(HaveOccurred())
	Expect(os.WriteFile(machineClassesFile.Name(), []byte(machineClassData), 0600)).To(Succeed())
	DeferCleanup(os.Remove, machineClassesFile.Name())
})
