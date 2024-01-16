// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package mcr

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"k8s.io/apimachinery/pkg/api/resource"

	iri "github.com/ironcore-dev/ironcore/iri/apis/machine/v1alpha1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func LoadMachineClasses(reader io.Reader) ([]iri.MachineClass, error) {
	var classList []iri.MachineClass
	if err := yaml.NewYAMLOrJSONDecoder(reader, 4096).Decode(&classList); err != nil {
		return nil, fmt.Errorf("unable to unmarshal machine classes: %w", err)
	}

	return classList, nil
}

func LoadMachineClassesFile(filename string) ([]iri.MachineClass, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("unable to open machine class file (%s): %w", filename, err)
	}

	return LoadMachineClasses(file)
}

func NewMachineClassRegistry(ctx context.Context, classes []iri.MachineClass, disableHugepages bool) (*Mcr, error) {
	registry := Mcr{
		classes:          map[string]iri.MachineClass{},
		disableHugepages: disableHugepages,
	}

	var hugepageSize int64
	if !disableHugepages {
		hostMem, err := mem.VirtualMemoryWithContext(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get host memory: %w", err)
		}
		hugepageSize = int64(hostMem.HugePageSize)
	}

	for _, class := range classes {
		if _, ok := registry.classes[class.Name]; ok {
			return nil, fmt.Errorf("multiple classes with same name (%s) found", class.Name)
		}

		if hugepageSize != 0 {
			roundHugepagesUp(&class, hugepageSize)
		}
		registry.classes[class.Name] = class
	}

	return &registry, nil
}

type Mcr struct {
	classes          map[string]iri.MachineClass
	disableHugepages bool
}

func (m *Mcr) Get(machineClassName string) (*iri.MachineClass, bool) {
	class, found := m.classes[machineClassName]
	return &class, found
}

func (m *Mcr) List() []*iri.MachineClass {
	var classes []*iri.MachineClass
	for name := range m.classes {
		class := m.classes[name]
		classes = append(classes, &class)
	}
	return classes
}

func (m *Mcr) GetResources(ctx context.Context) (*Host, error) {
	return GetResources(ctx, m.disableHugepages)
}

func GetQuantity(class *iri.MachineClass, host *Host) int64 {
	cpuRatio := host.Cpu.Value() / class.Capabilities.CpuMillis
	memoryRatio := host.Mem.Value() / class.Capabilities.MemoryBytes

	return int64(math.Min(float64(cpuRatio), float64(memoryRatio)))
}

func GetResources(ctx context.Context, disableHugepages bool) (*Host, error) {
	hostMem, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get host memory: %w", err)
	}

	hostCPU, err := cpu.InfoWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get host memory: %w", err)
	}

	var hostCPUSum int64
	for _, v := range hostCPU {
		hostCPUSum += int64(v.Cores)
	}

	host := &Host{
		Cpu: resource.NewScaledQuantity(hostCPUSum, resource.Kilo),
	}

	if disableHugepages {
		host.Mem = resource.NewQuantity(int64(hostMem.Total), resource.BinarySI)
	} else {
		if hostMem.HugePagesFree == 0 {
			host.Mem = resource.NewQuantity(0, resource.BinarySI)
		} else {
			host.Mem = resource.NewQuantity(int64(hostMem.HugePageSize*hostMem.HugePagesFree), resource.BinarySI)
		}
	}
	return host, nil
}

func roundHugepagesUp(class *iri.MachineClass, hugepageSize int64) {
	hugepageCount := int64(math.Ceil(float64(class.GetCapabilities().MemoryBytes) / float64(hugepageSize)))
	class.GetCapabilities().MemoryBytes = hugepageCount * int64(hugepageSize)
	fmt.Println(hugepageCount, hugepageSize, class.GetCapabilities().MemoryBytes)
}

type Host struct {
	Cpu *resource.Quantity
	Mem *resource.Quantity
}
