package theme

import (
	"net"
	"slices"
	"strings"
	"testing"
)

func TestViteArgsBindEveryInterface(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pm   PackageManager
		want []string
	}{
		{name: "npm", pm: NPM, want: []string{"npm", "run", "dev", "--", "--port", "15173", "--host"}},
		{name: "yarn", pm: Yarn, want: []string{"yarn", "dev", "--", "--port", "15173", "--host"}},
		{name: "pnpm", pm: PNPM, want: []string{"pnpm", "run", "dev", "--", "--port", "15173", "--host"}},
		{name: "bun", pm: Bun, want: []string{"bun", "run", "dev", "--", "--port", "15173", "--host"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := viteArgs(tt.pm, 15173)
			if !slices.Equal(got, tt.want) {
				t.Errorf("viteArgs(%v, 15173) = %v, want %v", tt.pm, got, tt.want)
			}
		})
	}
}

func TestViteArgsPassHostAfterTheSeparator(t *testing.T) {
	t.Parallel()

	// Anything before -- is consumed by the package manager rather than Vite,
	// so --host has to follow it to reach the dev server.
	args := viteArgs(NPM, 15173)
	separator := slices.Index(args, "--")

	if separator < 0 {
		t.Fatalf("args = %v, want a -- separator", args)
	}
	if !slices.Contains(args[separator:], "--host") {
		t.Errorf("args = %v, want --host after the separator", args)
	}
}

// ipNet builds an interface address the way the net package reports one.
func ipNet(cidr string) net.Addr {
	ip, network, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}
	network.IP = ip
	return network
}

func TestLANAddressesSkipLoopbackAndLinkLocal(t *testing.T) {
	t.Parallel()

	interfaces := []networkInterface{
		{Name: "Loopback Pseudo-Interface 1", Up: true, Addrs: []net.Addr{ipNet("127.0.0.1/8")}},
		{Name: "Ethernet", Up: true, Addrs: []net.Addr{ipNet("192.168.1.50/24")}},
		{Name: "Wi-Fi", Up: true, Addrs: []net.Addr{ipNet("169.254.12.3/16")}},
	}

	got := lanAddresses(interfaces)

	if len(got) != 1 {
		t.Fatalf("addresses = %v, want only the Ethernet address", got)
	}
	if got[0].IP != "192.168.1.50" || got[0].Interface != "Ethernet" {
		t.Errorf("address = %+v, want 192.168.1.50 on Ethernet", got[0])
	}
}

func TestLANAddressesSkipInterfacesThatAreDown(t *testing.T) {
	t.Parallel()

	interfaces := []networkInterface{
		{Name: "Ethernet 2", Up: false, Addrs: []net.Addr{ipNet("192.168.1.51/24")}},
	}

	if got := lanAddresses(interfaces); len(got) != 0 {
		t.Errorf("addresses = %v, want none from an interface that is down", got)
	}
}

func TestLANAddressesSkipIPv6(t *testing.T) {
	t.Parallel()

	// The themes are reached by typing the address on a phone, so only IPv4 is
	// worth printing.
	interfaces := []networkInterface{
		{Name: "Ethernet", Up: true, Addrs: []net.Addr{
			ipNet("fe80::1/64"),
			ipNet("2001:db8::1/64"),
			ipNet("192.168.1.50/24"),
		}},
	}

	got := lanAddresses(interfaces)

	if len(got) != 1 || got[0].IP != "192.168.1.50" {
		t.Errorf("addresses = %v, want only the IPv4 address", got)
	}
}

func TestLANAddressesNameEachInterface(t *testing.T) {
	t.Parallel()

	// This machine has a VPN adapter alongside the real one, so the address
	// alone does not say which one to type.
	interfaces := []networkInterface{
		{Name: "ProtonVPN", Up: true, Addrs: []net.Addr{ipNet("10.2.0.2/32")}},
		{Name: "Ethernet", Up: true, Addrs: []net.Addr{ipNet("192.168.1.50/24")}},
	}

	got := lanAddresses(interfaces)

	if len(got) != 2 {
		t.Fatalf("addresses = %v, want both", got)
	}
	for _, address := range got {
		if address.Interface == "" {
			t.Errorf("address %+v has no interface name", address)
		}
	}
}

func TestLANAddressesToleratesNoInterfaces(t *testing.T) {
	t.Parallel()

	if got := lanAddresses(nil); len(got) != 0 {
		t.Errorf("addresses = %v, want none", got)
	}
}

func TestLANURLCarriesTheSensorPort(t *testing.T) {
	t.Parallel()

	// The theme SDK probes ports 19847-19851, but naming the port explicitly
	// saves the probe and matches what the browser is opened with locally.
	got := lanURL(lanAddress{Interface: "Ethernet", IP: "192.168.1.50"}, 15173, 19847)

	if got != "http://192.168.1.50:15173/?ws=19847" {
		t.Errorf("lanURL = %q, want http://192.168.1.50:15173/?ws=19847", got)
	}
}

func TestSystemInterfacesReportsThisHost(t *testing.T) {
	// Not a pure function, so this only asserts it runs and reports something
	// shaped correctly on whatever machine the suite runs on.
	interfaces, err := systemInterfaces()
	if err != nil {
		t.Fatalf("systemInterfaces: %v", err)
	}
	if len(interfaces) == 0 {
		t.Fatal("no interfaces reported, want at least the loopback adapter")
	}

	for _, address := range lanAddresses(interfaces) {
		if strings.Count(address.IP, ".") != 3 {
			t.Errorf("address %q is not IPv4 dotted quad", address.IP)
		}
		t.Logf("reachable on %s (%s)", lanURL(address, 15173, 19847), address.Interface)
	}
}
