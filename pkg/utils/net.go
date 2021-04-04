package utils

import (
	"fmt"
	"net"
)

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
