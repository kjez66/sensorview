package lan

import (
	"net"
	"strings"
	"testing"
)

// ipNet builds an interface address the way the net package reports one.
func ipNet(cidr string) net.Addr {
	ip, network, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}
	network.IP = ip
	return network
}
func TestAddressesSkipLoopbackAndLinkLocal(t *testing.T) {
	t.Parallel()

	interfaces := []NetworkInterface{
		{Name: "Loopback Pseudo-Interface 1", Up: true, Addrs: []net.Addr{ipNet("127.0.0.1/8")}},
		{Name: "Ethernet", Up: true, Addrs: []net.Addr{ipNet("192.168.1.50/24")}},
		{Name: "Wi-Fi", Up: true, Addrs: []net.Addr{ipNet("169.254.12.3/16")}},
	}

	got := Addresses(interfaces)

	if len(got) != 1 {
		t.Fatalf("addresses = %v, want only the Ethernet address", got)
	}
	if got[0].IP != "192.168.1.50" || got[0].Interface != "Ethernet" {
		t.Errorf("address = %+v, want 192.168.1.50 on Ethernet", got[0])
	}
}
func TestAddressesSkipInterfacesThatAreDown(t *testing.T) {
	t.Parallel()

	interfaces := []NetworkInterface{
		{Name: "Ethernet 2", Up: false, Addrs: []net.Addr{ipNet("192.168.1.51/24")}},
	}

	if got := Addresses(interfaces); len(got) != 0 {
		t.Errorf("addresses = %v, want none from an interface that is down", got)
	}
}
func TestAddressesSkipIPv6(t *testing.T) {
	t.Parallel()

	// The themes are reached by typing the address on a phone, so only IPv4 is
	// worth printing.
	interfaces := []NetworkInterface{
		{Name: "Ethernet", Up: true, Addrs: []net.Addr{
			ipNet("fe80::1/64"),
			ipNet("2001:db8::1/64"),
			ipNet("192.168.1.50/24"),
		}},
	}

	got := Addresses(interfaces)

	if len(got) != 1 || got[0].IP != "192.168.1.50" {
		t.Errorf("addresses = %v, want only the IPv4 address", got)
	}
}
func TestAddressesNameEachInterface(t *testing.T) {
	t.Parallel()

	// This machine has a VPN adapter alongside the real one, so the address
	// alone does not say which one to type.
	interfaces := []NetworkInterface{
		{Name: "ProtonVPN", Up: true, Addrs: []net.Addr{ipNet("10.2.0.2/32")}},
		{Name: "Ethernet", Up: true, Addrs: []net.Addr{ipNet("192.168.1.50/24")}},
	}

	got := Addresses(interfaces)

	if len(got) != 2 {
		t.Fatalf("addresses = %v, want both", got)
	}
	for _, address := range got {
		if address.Interface == "" {
			t.Errorf("address %+v has no interface name", address)
		}
	}
}
func TestAddressesToleratesNoInterfaces(t *testing.T) {
	t.Parallel()

	if got := Addresses(nil); len(got) != 0 {
		t.Errorf("addresses = %v, want none", got)
	}
}
func TestURLCarriesTheSensorPort(t *testing.T) {
	t.Parallel()

	// The theme SDK probes ports 19847-19851, but naming the port explicitly
	// saves the probe and matches what the browser is opened with locally.
	got := URL(Address{Interface: "Ethernet", IP: "192.168.1.50"}, 15173, 19847)

	if got != "http://192.168.1.50:15173/?ws=19847" {
		t.Errorf("URL = %q, want http://192.168.1.50:15173/?ws=19847", got)
	}
}
func TestSystemInterfacesReportsThisHost(t *testing.T) {
	// Not a pure function, so this only asserts it runs and reports something
	// shaped correctly on whatever machine the suite runs on.
	interfaces, err := SystemInterfaces()
	if err != nil {
		t.Fatalf("SystemInterfaces: %v", err)
	}
	if len(interfaces) == 0 {
		t.Fatal("no interfaces reported, want at least the loopback adapter")
	}

	for _, address := range Addresses(interfaces) {
		if strings.Count(address.IP, ".") != 3 {
			t.Errorf("address %q is not IPv4 dotted quad", address.IP)
		}
		t.Logf("reachable on %s (%s)", URL(address, 15173, 19847), address.Interface)
	}
}

func TestURLBracketsIPv6(t *testing.T) {
	t.Parallel()

	got := URL(Address{Interface: "Ethernet", IP: "fe80::1"}, 19847, 19847)

	if got != "http://[fe80::1]:19847/?ws=19847" {
		t.Errorf("URL = %q, want http://[fe80::1]:19847/?ws=19847", got)
	}
}
