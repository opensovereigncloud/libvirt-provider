// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package raw

import (
	"fmt"
	"io"
	"os"

	"github.com/go-logr/logr"
	"github.com/ironcore-dev/libvirt-provider/internal/osutils"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
)

type Exec struct{}

const permFile = 0660

func (Exec) Create(filename string, opts ...CreateOption) error {
	o := &CreateOptions{}
	o.ApplyOptions(opts)
	log := ctrl.Log.WithName("raw-disk").WithValues("filename", filename)

	if o.SourceFile == "" {
		if o.Size == nil {
			return fmt.Errorf("must specify Size when creating without source file")
		}
		seek := *o.Size
		// Position the file cursor one byte before the desired seek position to write a single byte,
		// to ensure that data is written at the exact byte position specified by seek.
		if err := createEmptyFileWithSeek(log, filename, seek-1); err != nil {
			return fmt.Errorf("failed creating the empty ephemeral disk at %s: %w", filename, err)
		}
	} else {
		if err := copyFile(log, o.SourceFile, filename); err != nil {
			return fmt.Errorf("failed creating virtual disk image, source: %s, destination: %s: %w", o.SourceFile, filename, err)
		}
	}

	return nil
}

func createEmptyFileWithSeek(log logr.Logger, filename string, seek int64) error {
	dstFile, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, permFile)
	if err != nil {
		return fmt.Errorf("failed opening destination file: %w", err)
	}

	defer osutils.CloseWithErrorLogging(dstFile, fmt.Sprintf("error closing file in createEmptyFileWithSeek. Path: %s", dstFile.Name()), &log)

	if _, err = dstFile.Seek(seek, io.SeekStart); err != nil {
		return fmt.Errorf("failed seeking destination file: %w", err)
	}

	if _, err = dstFile.Write([]byte{0}); err != nil {
		return fmt.Errorf("failed to write data to destination file: %w", err)
	}

	return nil
}

func copyFile(log logr.Logger, src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed opening source file: %w", err)
	}
	defer osutils.CloseWithErrorLogging(srcFile, fmt.Sprintf("error closing file. Path: %s", srcFile.Name()), &log)

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, permFile)
	if err != nil {
		return fmt.Errorf("failed opening destination file: %w", err)
	}
	defer osutils.CloseWithErrorLogging(dstFile, fmt.Sprintf("error closing file. Path: %s", dstFile.Name()), &log)

	if _, err = io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy data from source file to destination file: %w", err)
	}

	return nil
}

func init() {
	utilruntime.Must(impls.Add("exec", 0, Exec{}))
}
