// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"slices"

	"github.com/google/uuid"
)

const LogKeyMachineID = "machineID"

func Zero[E any]() E {
	var zero E
	return zero
}

func DeleteSliceElement[E comparable](s []E, elem E) []E {
	idx := slices.Index(s, elem)
	if idx < 0 {
		return s
	}

	return slices.Delete(s, idx, idx+1)
}

type IdGenerateFunc func() string

func (g IdGenerateFunc) Generate() string {
	return g()
}

func GenerateUUIDv7() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}
