// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package manager

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"

	"github.com/go-logr/logr"
	core "github.com/ironcore-dev/ironcore/api/core/v1alpha1"
	iri "github.com/ironcore-dev/ironcore/iri/apis/machine/v1alpha1"
	"github.com/ironcore-dev/libvirt-provider/api"
	"github.com/ironcore-dev/libvirt-provider/internal/resources/sources"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/resource"
)

var _ = Describe("ResourceManager", func() {
	BeforeEach(func() {
		ctx = context.Background()
		rm = &resourceManager{}
		rm.reset()
		dummySrc = sources.NewSourceDummy(resource.NewQuantity(100, resource.DecimalSI))
	})

	Context("initialization", func() {
		When("manager is already initialized", func() {
			It("should return an error", func() {
				rm.addSource(dummySrc)
				rm.initialized = true
				err := rm.initialize(ctx, nil)
				Expect(err).To(MatchError(ErrManagerAlreadyInitialized))
			})
		})

		When("no sources are registered", func() {
			It("should return an error", func() {
				err := rm.initialize(ctx, nil)
				Expect(err).To(MatchError(ErrManagerSourcesMissing))
			})
		})

		When("sources are valid", func() {
			BeforeEach(func() {
				rm.addSource(dummySrc)
				rm.machineclassesFile = machineClassesFile.Name()
			})

			It("should initialize successfully", func() {
				err := rm.initialize(ctx, nil)
				Expect(err).ToNot(HaveOccurred())
				Expect(rm.initialized).To(BeTrue())
			})

			It("should set available slot to 0 if existing machine count equals to vm limit", func() {
				rm.setVMLimit(1)
				machines := []*api.Machine{{}}
				err := rm.initialize(ctx, machines)
				Expect(err).ToNot(HaveOccurred())
				Expect(rm.availableVMSlots).To(Equal(int64(0)))
			})

			It("should initialize sources and register resources", func() {
				err := rm.initialize(ctx, nil)
				Expect(err).ToNot(HaveOccurred())
				Expect(rm.sources).To(HaveKey(dummySrc.GetName()))
			})

			It("should return an error if sources have conflicting resources", func() {
				memorySrc := sources.NewSourceMemory(sources.Options{})
				rm.addSource(memorySrc)

				conflictMemorySrc := sources.NewSourceHugepages(sources.Options{})
				rm.addSource(conflictMemorySrc)

				err := rm.initialize(ctx, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(ErrCommonResources.Error()))
			})

			It("should allocate resources for pre-existing machines", func() {
				machine := &api.Machine{Spec: api.MachineSpec{Resources: core.ResourceList{sources.ResourceDummy: *resource.NewQuantity(1, resource.DecimalSI)}}}
				err = rm.initialize(ctx, []*api.Machine{machine})
				Expect(err).ToNot(HaveOccurred())
			})

			It("should return error if source allocation fails for pre-existing machines", func() {
				machine := &api.Machine{Spec: api.MachineSpec{Resources: core.ResourceList{sources.ResourceDummy: *resource.NewQuantity(101, resource.DecimalSI)}}}
				err := rm.initialize(ctx, []*api.Machine{machine})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(sources.ErrResourceNotAvailable.Error()))
			})

			It("should initialize machine classes successfully", func() {
				err := rm.initialize(ctx, nil)
				Expect(err).ToNot(HaveOccurred())
				Expect(len(rm.machineClasses)).To(Equal(3))
			})

			It("should set the operation error to nil on successful initialization", func() {
				rm.operationError = errors.New("previous error")
				err := rm.initialize(ctx, nil)
				Expect(err).ToNot(HaveOccurred())
				Expect(rm.operationError).To(BeNil())
			})

			It("should return an error if initializing machine classes fails", func() {
				dummyMachineClassData := `[
					{
						"name": "extra-huge",
						"capabilities": {
							"memory": 16106127361
						}
					}
				]`
				dummyMachineClassesFile, err := os.CreateTemp(GinkgoT().TempDir(), "machineclasses")
				Expect(err).NotTo(HaveOccurred())
				Expect(os.WriteFile(dummyMachineClassesFile.Name(), []byte(dummyMachineClassData), 0600)).To(Succeed())
				DeferCleanup(machineClassesFile.Close)
				DeferCleanup(os.Remove, dummyMachineClassesFile.Name())

				rm.machineclassesFile = dummyMachineClassesFile.Name()
				err = rm.initialize(ctx, nil)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("required resource cpu is missing in machine class file"))
			})
		})
	})

	Context("adding a source", func() {
		When("manager is already initialized", func() {
			It("should return an error", func() {
				rm.initialized = true
				err := rm.addSource(dummySrc)
				Expect(err).To(MatchError(ErrManagerAlreadyInitialized))
			})
		})

		When("manager is being initialized for the first time", func() {
			It("should add the source successfully", func() {
				err := rm.addSource(dummySrc)
				Expect(err).ToNot(HaveOccurred())
				Expect(rm.sources).To(HaveKey(dummySrc.GetName()))
			})
		})
	})

	Context("setting logger", func() {
		var logger logr.Logger

		When("manager is already initialized", func() {
			It("should return an error", func() {
				rm.initialized = true
				err := rm.setLogger(logger)
				Expect(err).To(MatchError(ErrManagerAlreadyInitialized))
			})
		})

		When("manager is being initialized for the first time", func() {
			It("should set the logger successfully", func() {
				err := rm.setLogger(logger)
				Expect(err).ToNot(HaveOccurred())
				Expect(rm.log).To(Equal(logger.WithName("resource-manager")))
			})
		})
	})

	Context("setting machine classes filename", func() {
		When("manager is already initialized", func() {
			It("should return an error", func() {
				rm.initialized = true
				err := rm.setMachineClassesFilename("dummy-file")
				Expect(err).To(MatchError(ErrManagerAlreadyInitialized))
			})
		})

		When("manager is being initialized for the first time", func() {
			It("should set the filename successfully", func() {
				err := rm.setMachineClassesFilename("dummy-file")
				Expect(err).ToNot(HaveOccurred())
				Expect(rm.machineclassesFile).To(Equal("dummy-file"))
			})
		})
	})

	Context("setting VM limit", func() {
		When("manager is already initialized", func() {
			It("should return an error", func() {
				rm.initialized = true
				err := rm.setVMLimit(10)
				Expect(err).To(MatchError(ErrManagerAlreadyInitialized))
			})
		})

		When("manager is being initialized for the first time", func() {
			It("should set the VM limit successfully", func() {
				err := rm.setVMLimit(10)
				Expect(err).ToNot(HaveOccurred())
				Expect(rm.maxVMsLimit).To(Equal(uint64(10)))
			})
		})
	})

	Context("resource allocation", func() {
		BeforeEach(func() {
			rm.addSource(dummySrc)
			rm.machineclassesFile = machineClassesFile.Name()
		})

		When("there is operation error", func() {
			It("should return error", func() {
				demoErr := errors.New("demo operation error")
				rm.operationError = demoErr
				err = rm.allocate(&api.Machine{}, core.ResourceList{})
				Expect(err).To(MatchError(demoErr))
			})
		})

		When("VM limit is reached", func() {
			It("should return an error", func() {
				rm.maxVMsLimit = 1
				err := rm.initialize(ctx, nil)
				Expect(err).ToNot(HaveOccurred())
				rm.availableVMSlots = 0
				err = rm.allocate(&api.Machine{}, core.ResourceList{})
				Expect(err).To(MatchError(ErrVMLimitReached))
			})
		})

		When("VM limit is not set and availableVMSlots is 0", func() {
			It("should not return error", func() {
				err := rm.initialize(ctx, nil)
				Expect(err).ToNot(HaveOccurred())
				rm.availableVMSlots = 0
				err = rm.allocate(&api.Machine{}, core.ResourceList{})
				Expect(err).NotTo(HaveOccurred())
			})
		})

		When("the parent context is cancelled", func() {
			It("should return error", func() {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				rm.ctx = ctx
				demoErr := context.Canceled

				err := rm.initialize(ctx, nil)
				Expect(err).ToNot(HaveOccurred())
				err = rm.allocate(&api.Machine{}, core.ResourceList{})
				Expect(err).To(MatchError(demoErr))
			})
		})

		When("required resources are unsupported", func() {
			It("should return an error", func() {
				err := rm.initialize(ctx, nil)
				Expect(err).ToNot(HaveOccurred())
				requiredResources := core.ResourceList{core.ResourceName("unsupported"): *resource.NewQuantity(1, resource.DecimalSI)}
				err = rm.allocate(&api.Machine{}, requiredResources)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(ErrResourceUnsupported.Error()))
			})
		})

		When("resource is available with no error", func() {
			It("should allocate resource", func() {
				err := rm.initialize(ctx, nil)
				Expect(err).ToNot(HaveOccurred())
				s := rm.registredResources[sources.ResourceDummy]
				Expect(s.GetAvailableResources()[sources.ResourceDummy]).To(Equal(*resource.NewQuantity(100, resource.DecimalSI)))

				requiredResources := core.ResourceList{sources.ResourceDummy: *resource.NewQuantity(10, resource.DecimalSI)}
				err = rm.allocate(&api.Machine{}, requiredResources)
				Expect(err).NotTo(HaveOccurred())
				s1 := rm.registredResources[sources.ResourceDummy]
				Expect(s1.GetAvailableResources()[sources.ResourceDummy]).To(Equal(*resource.NewQuantity(90, resource.DecimalSI)))
			})

			It("should reduce the available vm count by 1", func() {
				err := rm.initialize(ctx, nil)
				Expect(err).ToNot(HaveOccurred())

				rm.availableVMSlots = 10
				err = rm.allocate(&api.Machine{}, core.ResourceList{})
				Expect(err).NotTo(HaveOccurred())
				Expect(rm.availableVMSlots).To(Equal(int64(9)))
			})

			It("should update available machineclass", func() {
				err := rm.initialize(ctx, nil)
				Expect(err).ToNot(HaveOccurred())
				Expect(rm.getAvailableMachineClasses()[0].Quantity).To(Equal(int64(100)))

				rm.availableVMSlots = 10
				requiredResources := core.ResourceList{sources.ResourceDummy: *resource.NewQuantity(10, resource.DecimalSI)}
				err = rm.allocate(&api.Machine{}, requiredResources)
				Expect(err).NotTo(HaveOccurred())
				Expect(rm.availableVMSlots).To(Equal(int64(9)))
				Expect(rm.getAvailableMachineClasses()[0].Quantity).To(Equal(int64(90)))
			})
		})
	})

	Context("resource deallocation", func() {
		BeforeEach(func() {
			rm.addSource(dummySrc)
			rm.machineclassesFile = machineClassesFile.Name()
		})

		When("there is operation error", func() {
			It("should return error", func() {
				demoErr := errors.New("demo operation error")
				rm.operationError = demoErr
				err = rm.deallocate(&api.Machine{}, core.ResourceList{})
				Expect(err).To(MatchError(demoErr))
			})
		})

		When("the parent context is cancelled", func() {
			It("should return error", func() {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				rm.ctx = ctx
				demoErr := context.Canceled

				err := rm.initialize(ctx, nil)
				Expect(err).ToNot(HaveOccurred())
				err = rm.deallocate(&api.Machine{}, core.ResourceList{})
				Expect(err).To(MatchError(demoErr))
			})
		})

		When("ideal resource deallocation case", func() {
			It("should deallocate resource", func() {
				machine := &api.Machine{Spec: api.MachineSpec{Resources: core.ResourceList{sources.ResourceDummy: *resource.NewQuantity(10, resource.DecimalSI)}}}
				err := rm.initialize(ctx, []*api.Machine{machine})
				Expect(err).ToNot(HaveOccurred())
				s := rm.registredResources[sources.ResourceDummy]
				Expect(s.GetAvailableResources()[sources.ResourceDummy]).To(Equal(*resource.NewQuantity(90, resource.DecimalSI)))

				deallocateResources := core.ResourceList{sources.ResourceDummy: *resource.NewQuantity(10, resource.DecimalSI)}
				err = rm.deallocate(machine, deallocateResources)
				Expect(err).NotTo(HaveOccurred())
				s1 := rm.registredResources[sources.ResourceDummy]
				Expect(s1.GetAvailableResources()[sources.ResourceDummy]).To(Equal(*resource.NewQuantity(100, resource.DecimalSI)))
			})

			It("should delete the resource from machine spec", func() {
				machine := &api.Machine{Spec: api.MachineSpec{Resources: core.ResourceList{sources.ResourceDummy: *resource.NewQuantity(10, resource.DecimalSI)}}}
				err := rm.initialize(ctx, []*api.Machine{machine})
				Expect(err).ToNot(HaveOccurred())

				deallocateResources := core.ResourceList{sources.ResourceDummy: *resource.NewQuantity(10, resource.DecimalSI)}
				err = rm.deallocate(machine, deallocateResources)
				Expect(err).NotTo(HaveOccurred())
				Expect(machine.Spec.Resources).To(BeEmpty())
			})

			It("should increase the available vm count by 1", func() {
				err := rm.initialize(ctx, nil)
				Expect(err).ToNot(HaveOccurred())

				rm.availableVMSlots = 10
				err = rm.deallocate(&api.Machine{}, core.ResourceList{})
				Expect(err).NotTo(HaveOccurred())
				Expect(rm.availableVMSlots).To(Equal(int64(11)))
			})

			It("should update available machineclass", func() {
				err := rm.initialize(ctx, nil)
				Expect(err).ToNot(HaveOccurred())
				Expect(rm.getAvailableMachineClasses()[0].Quantity).To(Equal(int64(100)))

				rm.availableVMSlots = 10
				requiredResources := core.ResourceList{sources.ResourceDummy: *resource.NewQuantity(10, resource.DecimalSI)}
				err = rm.deallocate(&api.Machine{}, requiredResources)
				Expect(err).NotTo(HaveOccurred())
				Expect(rm.getAvailableMachineClasses()[0].Quantity).To(Equal(int64(110)))
			})
		})

	})

	Context("machine class availability", func() {
		When("there are no machine classes", func() {
			It("should return an empty slice", func() {
				status := rm.getAvailableMachineClasses()
				Expect(status).To(BeEmpty())
			})
		})

		When("there are machine classes", func() {
			BeforeEach(func() {
				rm.addSource(dummySrc)
				rm.machineclassesFile = machineClassesFile.Name()

				err := rm.initialize(ctx, nil)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should return the correct status for each machine class", func() {
				status := rm.getAvailableMachineClasses()
				Expect(status).To(HaveLen(3))

				expectedStatus := []*iri.MachineClassStatus{
					{
						MachineClass: &iri.MachineClass{
							Name: "extra-huge",
							Capabilities: &iri.MachineClassCapabilities{
								CpuMillis:   2000000,
								MemoryBytes: 16106127361,
							},
						},
						Quantity: 100,
					},
					{
						MachineClass: &iri.MachineClass{
							Name: "t3-small",
							Capabilities: &iri.MachineClassCapabilities{
								CpuMillis:   2000000,
								MemoryBytes: 2147483648,
							},
						},
						Quantity: 100,
					},
					{
						MachineClass: &iri.MachineClass{
							Name: "x3-xlarge",
							Capabilities: &iri.MachineClassCapabilities{
								CpuMillis:   4000000,
								MemoryBytes: 8589934592,
							},
						},
						Quantity: 100,
					},
				}

				for i, classStatus := range status {
					Expect(classStatus.MachineClass.Name).To(Equal(expectedStatus[i].MachineClass.Name))
					Expect(classStatus.MachineClass.Capabilities.CpuMillis).To(Equal(expectedStatus[i].MachineClass.Capabilities.CpuMillis))
					Expect(classStatus.MachineClass.Capabilities.MemoryBytes).To(Equal(expectedStatus[i].MachineClass.Capabilities.MemoryBytes))
					Expect(classStatus.Quantity).To(Equal(expectedStatus[i].Quantity))
				}
			})

			It("should not modify the original machine class references", func() {
				status := rm.getAvailableMachineClasses()

				for i, classStatus := range status {
					// Modify the returned status
					classStatus.MachineClass.Name = "modified"
					classStatus.Quantity = 0

					// Verify that original machineClasses are not modified
					Expect(rm.machineClasses[i].Name).ToNot(Equal("modified"))
					Expect(rm.machineClasses[i].available).ToNot(Equal(0))
				}
			})
		})
	})

	Context("calculate machine class quantity", func() {
		var class *MachineClass

		BeforeEach(func() {
			class = &MachineClass{
				Capabilities: core.ResourceList{
					core.ResourceCPU:    *resource.NewQuantity(2, resource.DecimalSI),
					core.ResourceMemory: *resource.NewQuantity(4*1024*1024*1024, resource.BinarySI),
				},
			}

			rm.registredResources[core.ResourceCPU] = dummySrc
			rm.registredResources[core.ResourceMemory] = dummySrc
		})

		When("all resources are available", func() {
			It("should calculate the correct machine class quantity", func() {
				err = rm.calculateMachineClassQuantity(class)
				Expect(err).ToNot(HaveOccurred())
				Expect(class.available).To(Equal(dummySrc.CalculateMachineClassQuantity(class.Capabilities)))
			})
		})

		When("a resource source is missing", func() {
			It("should return an error", func() {
				delete(rm.registredResources, core.ResourceMemory)
				err = rm.calculateMachineClassQuantity(class)
				Expect(err).To(HaveOccurred())
				Expect(err).To(MatchError(fmt.Errorf("failed to find source for resource %s: %w", core.ResourceMemory, ErrManagerSourcesMissing)))
			})
		})

		When("sourceCount is zero", func() {
			It("should set class available to 0", func() {
				dummySrc.SetQuantity(0)
				err = rm.calculateMachineClassQuantity(class)
				Expect(err).ToNot(HaveOccurred())
				Expect(class.available).To(Equal(int64(0)))
			})
		})

		When("sourceCount is QuantityCountIgnore", func() {
			It("should ignore the resource", func() {
				dummySrc.SetQuantity(sources.QuantityCountIgnore)
				err = rm.calculateMachineClassQuantity(class)
				Expect(err).ToNot(HaveOccurred())
				// Since the source is ignored, class.available should be MaxInt64
				Expect(class.available).To(Equal(int64(math.MaxInt64)))
			})
		})

		When("maxVMsLimit is set and is less than calculated count", func() {
			It("should set class available to maxVMsLimit", func() {
				rm.maxVMsLimit = 10
				rm.availableVMSlots = 10
				err = rm.calculateMachineClassQuantity(class)
				Expect(err).ToNot(HaveOccurred())
				Expect(class.available).To(Equal(int64(10)))
			})
		})

		When("maxVMsLimit is set and is greater than availableVMSlots", func() {
			It("should set class available to availableVMSlots", func() {
				rm.maxVMsLimit = 20
				rm.availableVMSlots = 15
				err = rm.calculateMachineClassQuantity(class)
				Expect(err).ToNot(HaveOccurred())
				Expect(class.available).To(Equal(int64(15)))
			})
		})

		When("count is less than zero", func() {
			It("should set class available to 0", func() {
				dummySrc.SetQuantity(-10)
				err = rm.calculateMachineClassQuantity(class)
				Expect(err).ToNot(HaveOccurred())
				Expect(class.available).To(Equal(int64(0)))
			})
		})
	})

	Context("checking context", func() {
		BeforeEach(func() {
			rm.machineclassesFile = machineClassesFile.Name()
			rm.addSource(dummySrc)
			err := rm.initialize(ctx, nil)
			Expect(err).ToNot(HaveOccurred())
		})

		When("the context is not cancelled", func() {
			It("should not return an error", func() {
				rm.ctx = ctx

				err = rm.checkContext()
				Expect(err).To(BeNil())
				Expect(rm.operationError).To(BeNil())
			})
		})

		When("the context is cancelled", func() {
			It("should return a context canceled error", func() {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				rm.ctx = ctx
				err := rm.checkContext()
				Expect(err).To(HaveOccurred())
				Expect(errors.Is(err, context.Canceled)).To(BeTrue())
				Expect(rm.operationError).To(MatchError(fmt.Errorf("context error: %w", context.Canceled)))
			})
		})

		When("the context is deadline exceeded", func() {
			It("should return a context deadline exceeded error", func() {
				ctx, cancel := context.WithTimeout(context.Background(), 0)
				defer cancel()
				rm.ctx = ctx
				err := rm.checkContext()
				Expect(err).To(HaveOccurred())
				Expect(errors.Is(err, context.DeadlineExceeded)).To(BeTrue())
				Expect(rm.operationError).To(MatchError(fmt.Errorf("context error: %w", context.DeadlineExceeded)))
			})
		})
	})

	Context("get machine class", func() {
		var (
			class1 *MachineClass
			class2 *MachineClass
			class3 *MachineClass
		)

		BeforeEach(func() {
			class1 = &MachineClass{Name: "class1"}
			class2 = &MachineClass{Name: "class2"}
			class3 = &MachineClass{Name: "class3"}

			rm.machineClasses = append(rm.machineClasses, class1, class2, class3)
		})

		When("the machine class exists", func() {
			It("should return the correct machine class", func() {
				result, err := rm.getMachineClass("class1")
				Expect(err).To(BeNil())
				Expect(result).To(Equal(class1))

				result, err = rm.getMachineClass("class2")
				Expect(err).To(BeNil())
				Expect(result).To(Equal(class2))

				result, err = rm.getMachineClass("class3")
				Expect(err).To(BeNil())
				Expect(result).To(Equal(class3))
			})
		})

		When("the machine class does not exist", func() {
			It("should return an error", func() {
				result, err := rm.getMachineClass("class4")
				Expect(err).To(MatchError(ErrMachineClassMissing))
				Expect(result).To(BeNil())
			})
		})

		When("there are no machine classes", func() {
			It("should return an error", func() {
				rm.machineClasses = []*MachineClass{}
				result, err := rm.getMachineClass("class1")
				Expect(err).To(MatchError(ErrMachineClassMissing))
				Expect(result).To(BeNil())
			})
		})

		When("the machine class name is an empty string", func() {
			It("should return an error", func() {
				result, err := rm.getMachineClass("")
				Expect(err).To(MatchError(ErrMachineClassMissing))
				Expect(result).To(BeNil())
			})
		})

		When("the machine class name has leading/trailing spaces", func() {
			It("should return an error", func() {
				rm.machineClasses = append(rm.machineClasses, &MachineClass{Name: " class5 "})
				result, err := rm.getMachineClass("class5")
				Expect(err).To(MatchError(ErrMachineClassMissing))
				Expect(result).To(BeNil())
			})
		})
	})

	Context("initializing machine classes", func() {
		When("the machine classes file is loaded successfully", func() {
			It("should initialize machine classes successfully", func() {
				rm.addSource(dummySrc)
				rm.machineclassesFile = machineClassesFile.Name()

				err := rm.initialize(ctx, nil)
				Expect(err).ToNot(HaveOccurred())

				err = rm.initMachineClasses()
				Expect(err).ToNot(HaveOccurred())
			})
		})

		When("the machine classes file cannot be loaded", func() {
			It("should return an error", func() {
				rm.addSource(dummySrc)
				rm.machineclassesFile = machineClassesFile.Name()

				err := rm.initialize(ctx, nil)
				Expect(err).ToNot(HaveOccurred())

				rm.machineclassesFile = ""
				err = rm.initMachineClasses()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("unable to open machine class file"))
			})
		})

		When("a machine class is missing required resources", func() {
			It("should return error if cpu is missing in machine class", func() {
				dummyMachineClassData := `[
					{
						"name": "extra-huge",
						"capabilities": {
							"memory": 16106127361
						}
					}
				]`
				dummyMachineClassesFile, err := os.CreateTemp(GinkgoT().TempDir(), "machineclasses")
				Expect(err).NotTo(HaveOccurred())
				Expect(os.WriteFile(dummyMachineClassesFile.Name(), []byte(dummyMachineClassData), 0600)).To(Succeed())
				DeferCleanup(os.Remove, dummyMachineClassesFile.Name())

				rm.machineclassesFile = dummyMachineClassesFile.Name()
				err = rm.initMachineClasses()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("required resource cpu is missing in machine class file"))
			})

			It("should return error if memory is missing in machine class", func() {
				dummyMachineClassData := `[
					{
						"name": "extra-huge",
						"capabilities": {
							"cpu": 2000
						}
					}
				]`
				dummyMachineClassesFile, err := os.CreateTemp(GinkgoT().TempDir(), "machineclasses")
				Expect(err).NotTo(HaveOccurred())
				Expect(os.WriteFile(dummyMachineClassesFile.Name(), []byte(dummyMachineClassData), 0600)).To(Succeed())
				DeferCleanup(os.Remove, dummyMachineClassesFile.Name())

				rm.machineclassesFile = dummyMachineClassesFile.Name()
				err = rm.initMachineClasses()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal("required resource memory is missing in machine class file"))
			})
		})

		When("a machine class has unsupported resources", func() {
			It("should ignore the machine class", func() {
				dummyMachineClassData := `[
					{
						"name": "extra-huge",
						"capabilities": {
							"cpu": 2000,
							"memory": 16106127361,
							"unsupported": 2000
						}
					}
				]`
				dummyMachineClassesFile, err := os.CreateTemp(GinkgoT().TempDir(), "machineclasses")
				Expect(err).NotTo(HaveOccurred())
				Expect(os.WriteFile(dummyMachineClassesFile.Name(), []byte(dummyMachineClassData), 0600)).To(Succeed())
				DeferCleanup(os.Remove, dummyMachineClassesFile.Name())

				rm.machineclassesFile = dummyMachineClassesFile.Name()
				err = rm.initMachineClasses()
				Expect(err).ToNot(HaveOccurred())
				Expect(rm.machineClasses).To(BeEmpty())
			})
		})

		When("modifyResources returns an error", func() {
			It("should return error", func() {
				rm.machineclassesFile = machineClassesFile.Name()
				rm.addSource(dummySrc)
				err := rm.initialize(ctx, nil)
				Expect(err).ToNot(HaveOccurred())

				dummyMachineClassData := `[
					{
						"name": "extra-huge",
						"capabilities": {
							"cpu": 2000,
							"memory": 16106127361,
							"dummy": 2000
						}
					}
				]`
				dummyMachineClassesFile, err := os.CreateTemp(GinkgoT().TempDir(), "machineclasses")
				Expect(err).NotTo(HaveOccurred())
				Expect(os.WriteFile(dummyMachineClassesFile.Name(), []byte(dummyMachineClassData), 0600)).To(Succeed())
				DeferCleanup(os.Remove, dummyMachineClassesFile.Name())

				rm.machineclassesFile = dummyMachineClassesFile.Name()
				err = rm.initMachineClasses()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("error while modifing resource"))
			})
		})

		When("machineclass has all the resource with no error", func() {
			It("should initialize machine classes", func() {
				rm.machineclassesFile = machineClassesFile.Name()
				rm.registredResources = map[core.ResourceName]Source{
					core.ResourceName("memory.epc.sgx"): dummySrc,
					core.ResourceCPU:                    dummySrc,
					core.ResourceMemory:                 dummySrc,
				}

				err = rm.initMachineClasses()
				Expect(err).NotTo(HaveOccurred())
				Expect(len(rm.machineClasses)).To(Equal(3))

				// Validate the first MachineClass
				Expect(rm.machineClasses[0].Name).To(Equal("extra-huge"))
				Expect(rm.machineClasses[0].Capabilities).To(HaveKey(core.ResourceCPU))
				Expect(rm.machineClasses[0].Capabilities[core.ResourceCPU]).To(Equal(*resource.NewQuantity(2000, resource.DecimalSI)))
				Expect(rm.machineClasses[0].Capabilities).To(HaveKey(core.ResourceMemory))
				memory1 := rm.machineClasses[0].Capabilities[core.ResourceMemory]
				Expect(memory1.Value()).To(Equal(int64(16106127361)))

				// Validate the second MachineClass
				Expect(rm.machineClasses[1].Name).To(Equal("t3-small"))
				Expect(rm.machineClasses[1].Capabilities).To(HaveKey(core.ResourceCPU))
				Expect(rm.machineClasses[1].Capabilities[core.ResourceCPU]).To(Equal(*resource.NewQuantity(2000, resource.DecimalSI)))
				Expect(rm.machineClasses[1].Capabilities).To(HaveKey(core.ResourceMemory))
				memory2 := rm.machineClasses[1].Capabilities[core.ResourceMemory]
				Expect(memory2.Value()).To(Equal(int64(2147483648)))
				Expect(rm.machineClasses[1].Capabilities).To(HaveKey(core.ResourceName("memory.epc.sgx")))
				sgx := rm.machineClasses[1].Capabilities[core.ResourceName("memory.epc.sgx")]
				Expect(sgx.Value()).To(Equal(int64(2147483648)))

				// Validate the third MachineClass
				Expect(rm.machineClasses[2].Name).To(Equal("x3-xlarge"))
				Expect(rm.machineClasses[2].Capabilities).To(HaveKey(core.ResourceCPU))
				Expect(rm.machineClasses[2].Capabilities[core.ResourceCPU]).To(Equal(*resource.NewQuantity(4000, resource.DecimalSI)))
				Expect(rm.machineClasses[2].Capabilities).To(HaveKey(core.ResourceMemory))
				memory3 := rm.machineClasses[2].Capabilities[core.ResourceMemory]
				Expect(memory3.Value()).To(Equal(int64(8589934592)))
			})
		})
	})

	Context("deallocate unassigned resource", func() {
		When("all sources are registered", func() {
			It("should deallocate the resources", func() {
				rm.machineclassesFile = machineClassesFile.Name()

				rm.addSource(dummySrc)
				machine := &api.Machine{Spec: api.MachineSpec{Resources: core.ResourceList{sources.ResourceDummy: *resource.NewQuantity(10, resource.DecimalSI)}}}
				err := rm.initialize(ctx, []*api.Machine{machine})
				Expect(err).ToNot(HaveOccurred())
				s := rm.registredResources[sources.ResourceDummy]
				Expect(s.GetAvailableResources()[sources.ResourceDummy]).To(Equal(*resource.NewQuantity(90, resource.DecimalSI)))

				rm.deallocateUnassignResources(core.ResourceList{sources.ResourceDummy: *resource.NewQuantity(10, resource.DecimalSI)})
				s1 := rm.registredResources[sources.ResourceDummy]
				Expect(s1.GetAvailableResources()[sources.ResourceDummy]).To(Equal(*resource.NewQuantity(100, resource.DecimalSI)))
			})
		})
	})
})
