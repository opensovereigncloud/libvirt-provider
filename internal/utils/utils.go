// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/ironcore-dev/libvirt-provider/internal/osutils"
)

const (
	LogKeyMachineID = "machineID"
	LogKeyNICName   = "nicName"

	FolderSysPCIDevices = "/sys/bus/pci/devices"
)

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

func ReadPCIAttribute(log *logr.Logger, devicePath, attributeName string) (string, error) {
	attributePath := filepath.Join(devicePath, attributeName)
	file, err := os.Open(attributePath)
	if err != nil {
		return "", err
	}

	defer osutils.CloseWithErrorLogging(file, fmt.Sprintf("error closing file. Path: %s", file.Name()), log)

	// attributeFileSize is higher as file content can be.
	const attributeFileSize = 16
	buff := make([]byte, attributeFileSize)

	n, err := file.Read(buff)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}

	if n == attributeFileSize {
		return "", fmt.Errorf("file %s has bigger content as expected", file.Name())
	}

	s := string(buff[:n])

	return strings.ToLower(strings.TrimSpace(s)), nil
}
