// Package theme - LAN address discovery for the dev server.
package theme

import (
	"fmt"
	"net"
)

// lanAddress is one address the dev server can be reached on from another
// device. The interface name is carried alongside it because a machine with a
// VPN adapter has several, and the address alone does not say which to use.
type lanAddress struct {
	Interface string
	IP        string
}

// networkInterface is the part of net.Interface this package needs, so that
// address filtering can be exercised without touching the host network.
type networkInterface struct {
	Name  string
	Up    bool
	Addrs []net.Addr
}

// systemInterfaces reports the interfaces of this host.
func systemInterfaces() ([]networkInterface, error) {
	found, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list network interfaces: %w", err)
	}

	interfaces := make([]networkInterface, 0, len(found))
	for _, iface := range found {
		// An interface whose addresses cannot be read is skipped rather than
		// failing the whole listing: one unusable adapter should not stop the
		// dev server from reporting the others.
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		interfaces = append(interfaces, networkInterface{
			Name:  iface.Name,
			Up:    iface.Flags&net.FlagUp != 0,
			Addrs: addrs,
		})
	}
	return interfaces, nil
}

// lanAddresses returns the IPv4 addresses another device on the network can
// reach, skipping interfaces that are down and addresses that are not routable
// from elsewhere.
func lanAddresses(interfaces []networkInterface) []lanAddress {
	var addresses []lanAddress

	for _, iface := range interfaces {
		if !iface.Up {
			continue
		}
		for _, addr := range iface.Addrs {
			ip := addrIPv4(addr)
			if ip == nil {
				continue
			}
			if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
				continue
			}
			addresses = append(addresses, lanAddress{Interface: iface.Name, IP: ip.String()})
		}
	}

	return addresses
}

// addrIPv4 returns the IPv4 form of an interface address, or nil when it has
// none. Only IPv4 is reported: these addresses get typed in by hand on a phone.
func addrIPv4(addr net.Addr) net.IP {
	switch typed := addr.(type) {
	case *net.IPNet:
		return typed.IP.To4()
	case *net.IPAddr:
		return typed.IP.To4()
	default:
		return nil
	}
}

// lanURL returns the address to open on another device. The sensor port is
// named explicitly so the theme SDK connects straight away instead of probing
// the port range.
func lanURL(address lanAddress, vitePort, wsPort int) string {
	return fmt.Sprintf("http://%s:%d/?ws=%d", address.IP, vitePort, wsPort)
}
