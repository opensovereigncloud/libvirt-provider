// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"encoding/json"
	"fmt"

	"github.com/ironcore-dev/controller-utils/metautils"
	irimeta "github.com/ironcore-dev/ironcore/iri/apis/meta/v1alpha1"
)

func GetObjectMetadata(o Metadata) (*irimeta.ObjectMetadata, error) {
	annotations, err := GetAnnotationsAnnotation(o)
	if err != nil {
		return nil, err
	}

	labels, err := GetLabelsAnnotation(o)
	if err != nil {
		return nil, err
	}

	var deletedAt int64
	if o.DeletedAt != nil && !o.DeletedAt.IsZero() {
		deletedAt = o.DeletedAt.UnixNano()
	}

	return &irimeta.ObjectMetadata{
		Id:          o.ID,
		Annotations: annotations,
		Labels:      labels,
		Generation:  o.GetGeneration(),
		CreatedAt:   o.CreatedAt.UnixNano(),
		DeletedAt:   deletedAt,
	}, nil
}

func SetObjectMetadata(o Object, metadata *irimeta.ObjectMetadata) error {
	if err := SetAnnotationsAnnotation(o, metadata.Annotations); err != nil {
		return err
	}
	if err := SetLabelsAnnotation(o, metadata.Labels); err != nil {
		return err
	}
	return nil
}

func SetLabelsAnnotation(o Object, labels map[string]string) error {
	data, err := json.Marshal(labels)
	if err != nil {
		return fmt.Errorf("error marshalling labels: %w", err)
	}
	metautils.SetAnnotation(o, LabelsAnnotation, string(data))
	return nil
}

func GetLabelsAnnotation(o Metadata) (map[string]string, error) {
	data, ok := o.GetAnnotations()[LabelsAnnotation]
	if !ok {
		return nil, fmt.Errorf("object has no labels at %s", LabelsAnnotation)
	}

	var labels map[string]string
	if err := json.Unmarshal([]byte(data), &labels); err != nil {
		return nil, err
	}

	return labels, nil
}

func SetAnnotationsAnnotation(o Object, annotations map[string]string) error {
	data, err := json.Marshal(annotations)
	if err != nil {
		return fmt.Errorf("error marshalling annotations: %w", err)
	}
	metautils.SetAnnotation(o, AnnotationsAnnotation, string(data))

	return nil
}

func GetAnnotationsAnnotation(o Metadata) (map[string]string, error) {
	data, ok := o.GetAnnotations()[AnnotationsAnnotation]
	if !ok {
		return nil, fmt.Errorf("object has no annotations at %s", AnnotationsAnnotation)
	}

	var annotations map[string]string
	if err := json.Unmarshal([]byte(data), &annotations); err != nil {
		return nil, err
	}

	return annotations, nil
}

func SetManagerLabel(o Object, manager string) {
	metautils.SetLabel(o, ManagerLabel, manager)
}

func SetClassLabel(o Object, class string) {
	metautils.SetLabel(o, ClassLabel, class)
}

func GetClassLabel(o Object) (string, bool) {
	class, found := o.GetLabels()[ClassLabel]
	return class, found
}

func IsManagedBy(o Object, manager string) bool {
	actual, ok := o.GetLabels()[ManagerLabel]
	return ok && actual == manager
}

// GetExistingPCICount returns the total count of unique PCI-related resources for a given machine.
// It includes:
//   - Unique volumes from both the spec and status sections
//   - Unique network interfaces from both the spec and status sections
//   - The total number of PCI devices
func GetExistingPCICount(machine *Machine) int {
	if machine == nil {
		return 0
	}

	volumeCount := getUniqueElementCount(
		machine.Spec.Volumes,
		func(v *VolumeSpec) string {
			if v == nil {
				return ""
			}
			return v.Name
		},
		machine.Status.VolumeStatus,
		func(v VolumeStatus) string {
			return v.Name
		},
	)

	nicCount := getUniqueElementCount(
		machine.Spec.NetworkInterfaces,
		func(n *NetworkInterfaceSpec) string {
			return n.Name
		},
		machine.Status.NetworkInterfaceStatus,
		func(n NetworkInterfaceStatus) string {
			return n.Name
		},
	)

	return volumeCount + nicCount + len(machine.Status.PCIDevices)
}

// getUniqueElementCount returns the total number of unique non-empty names extracted from two slices.
// It accepts two slices of different types and their respective name-extraction functions.
// A name is considered unique if it is non-empty and not duplicated across both slices.
// This is useful when merging data from two distinct sources while avoiding duplicates.
func getUniqueElementCount[T1, T2 any](
	slice1 []T1, getName1 func(T1) string,
	slice2 []T2, getName2 func(T2) string,
) int {
	seen := make(map[string]struct{}, len(slice1)+len(slice2))

	for _, item := range slice1 {
		if name := getName1(item); name != "" {
			seen[name] = struct{}{}
		}
	}

	for _, item := range slice2 {
		if name := getName2(item); name != "" {
			seen[name] = struct{}{}
		}
	}

	return len(seen)
}
