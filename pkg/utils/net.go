package utils

import (
	"fmt"
	"net"
)

// GetConfiguredOrSuitableInterfaceName returns the configured interface name, if not empty, or tries retrieving first suitable interface name.
func GetConfiguredOrSuitableInterfaceName(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	iface, err := GetFirstUpBroadcastInterface()
	if err != nil {
		return "", err
	}
	return iface.Name, nil
}

// GetFirstUpBroadcastInterface retrieves the first suitable up broadact capable interface.
func GetFirstUpBroadcastInterface() (net.Interface, error) {
	interfaces, err := GetUpBroadcastInterfaces()
	if err != nil {
		return net.Interface{}, err
	}
	if len(interfaces) == 0 {
		return net.Interface{}, fmt.Errorf("no suitable interfaces")
	}
	return interfaces[0], nil
}

// GetInterfaceV4Addr fetches an IPv4 address of an interface.
func GetInterfaceV4Addr(interfaceName string) (addr string, err error) {
	var (
		iface    *net.Interface
		addrs    []net.Addr
		ipv4Addr net.IP
	)
	if iface, err = net.InterfaceByName(interfaceName); err != nil { // get interface
		return
	}
	if addrs, err = iface.Addrs(); err != nil { // get addresses
		return
	}
	for _, addr := range addrs { // get ipv4 address
		if ipv4Addr = addr.(*net.IPNet).IP.To4(); ipv4Addr != nil {
			break
		}
	}
	if ipv4Addr == nil {
		return "", fmt.Errorf("interface %s don't have an ipv4 address", interfaceName)
	}
	return ipv4Addr.String(), nil
}

// GetUpBroadcastInterfaces retrieves the list of up broadcast interfaces.
// These are internally sorted by an index so the output is always deterministic.
func GetUpBroadcastInterfaces() ([]net.Interface, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return interfaces, err
	}
	result := []net.Interface{}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagBroadcast != 0 && iface.Flags&net.FlagUp != 0 {
			result = append(result, iface)
		}
	}
	return result, nil
}
