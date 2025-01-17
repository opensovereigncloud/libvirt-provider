// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package host

const (
	permFile = 0600
	// libvirt-qemu needs access into folder
	permFolderForLibvirtQemu = 0750
	// folder contains data manage by libvirt-provider only
	permFolderForLibvirtProvider = 0700
)
