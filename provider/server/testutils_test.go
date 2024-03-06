package server_test

import (
	"fmt"
	"net"
	"time"

	"libvirt.org/go/libvirtxml"
)

func getAnyDomainIPForNetwork(networkName string, domainDesc *libvirtxml.Domain) (string, error) {
	ip := ""

MAIN:
	for _, netIF := range domainDesc.Devices.Interfaces {
		if isInterfaceInSpecificNetwork(netIF.Source, networkName) {
			for index := range netIF.IP {
				if netIF.IP[index].Address != "" {
					ip = netIF.IP[index].Address
					break MAIN
				}
			}
		}
	}

	if ip == "" {
		return "", fmt.Errorf("failed to find ip address for domain %s", domainDesc.Name)
	}

	return ip, nil
}

func isInterfaceInSpecificNetwork(source *libvirtxml.DomainInterfaceSource, networkName string) bool {
	return source != nil && source.Network != nil && source.Network.Network == networkName
}

func isSSHOpen(ip string) bool {
	timeout := time.Second
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, "22"), timeout)
	if err != nil {
		return false
	}

	if conn != nil {
		defer conn.Close()
		return true
	}

	return false
}
