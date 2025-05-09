// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package api

import "errors"

var ErrPCIControllerMaxedOut = errors.New("pci controllers count already maxed out")
