// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"

	libvirtutils "github.com/ironcore-dev/libvirt-provider/pkg/libvirt/utils"
)

func (s *Server) LibvirtConnectionCheckHandler(w http.ResponseWriter, r *http.Request) {
	err := libvirtutils.ReconnectLibvirt(s.libvirt)
	if err == nil {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusInternalServerError)
	}
}
