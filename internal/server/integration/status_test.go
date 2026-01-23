// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package integration_test

import (
	corev1alpha1 "github.com/ironcore-dev/ironcore/api/core/v1alpha1"
	iriv1alpha1 "github.com/ironcore-dev/ironcore/iri/apis/machine/v1alpha1"
	"github.com/ironcore-dev/libvirt-provider/internal/resources/manager"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Status", func() {
	It("should get list of supported machine class with calculated quantity in status", func(ctx SpecContext) {
		By("getting machine class status")
		statusResp, err := machineClient.Status(ctx, &iriv1alpha1.StatusRequest{})
		Expect(err).NotTo(HaveOccurred())
		/*
			By("loading machine classes from file")
			machineClasses, err := mcr.LoadMachineClasses(machineClassesFile)
			Expect(err).NotTo(HaveOccurred())
		*/
		By("getting host resources")
		classesStatus := manager.GetMachineClassStatus()
		Expect(err).NotTo(HaveOccurred())

		By("validating machine class and calculated quantity in MachineClassStatus")
		Expect(statusResp.MachineClassStatus).To(ContainElements(&iriv1alpha1.MachineClassStatus{
			MachineClass: &iriv1alpha1.MachineClass{
				Name: classesStatus[0].MachineClass.Name,
				Capabilities: &iriv1alpha1.MachineClassCapabilities{
					Resources: map[string]int64{
						string(corev1alpha1.ResourceCPU):    classesStatus[0].MachineClass.Capabilities.Resources[string(corev1alpha1.ResourceCPU)],
						string(corev1alpha1.ResourceMemory): classesStatus[0].MachineClass.Capabilities.Resources[string(corev1alpha1.ResourceMemory)],
					},
				},
			},
			Quantity: classesStatus[0].Quantity,
		}, &iriv1alpha1.MachineClassStatus{
			MachineClass: &iriv1alpha1.MachineClass{
				Name: classesStatus[1].MachineClass.Name,
				Capabilities: &iriv1alpha1.MachineClassCapabilities{
					Resources: map[string]int64{
						string(corev1alpha1.ResourceCPU):    classesStatus[1].MachineClass.Capabilities.Resources[string(corev1alpha1.ResourceCPU)],
						string(corev1alpha1.ResourceMemory): classesStatus[1].MachineClass.Capabilities.Resources[string(corev1alpha1.ResourceMemory)],
					},
				},
			},
			Quantity: classesStatus[1].Quantity,
		}))
	})
})
