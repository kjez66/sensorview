package display

import (
	"testing"

	"github.com/kjez66/sensorview/pkg/lan"
)

// hostAddresses stands in for a machine with a VPN adapter beside its LAN one.
var hostAddresses = []lan.Address{
	{Interface: "ProtonVPN", IP: "10.2.0.2"},
	{Interface: "Ethernet", IP: "192.168.31.92"},
}

func TestReachableURLsNoneOnLoopback(t *testing.T) {
	for _, address := range []string{"127.0.0.1:19847", "[::1]:19847"} {
		if urls := reachableURLs(address, hostAddresses); len(urls) != 0 {
			t.Errorf("reachableURLs(%q) = %v, want none: other devices cannot connect", address, urls)
		}
	}
}

func TestReachableURLsAllOnWildcard(t *testing.T) {
	for _, address := range []string{"0.0.0.0:19847", "[::]:19847"} {
		urls := reachableURLs(address, hostAddresses)
		if len(urls) != len(hostAddresses) {
			t.Fatalf("reachableURLs(%q) = %v, want one per host address", address, urls)
		}
		for i, entry := range urls {
			if entry.Interface != hostAddresses[i].Interface {
				t.Errorf("entry %d interface = %q, want %q", i, entry.Interface, hostAddresses[i].Interface)
			}
		}
	}
}

func TestReachableURLsOnlyTheBoundAddress(t *testing.T) {
	urls := reachableURLs("192.168.31.92:19847", hostAddresses)

	want := []URLEntry{{Interface: "Ethernet", URL: "http://192.168.31.92:19847/?ws=19847"}}
	if len(urls) != 1 || urls[0] != want[0] {
		t.Errorf("reachableURLs = %v, want %v", urls, want)
	}
}

func TestReachableURLsReportsABoundAddressNoInterfaceLists(t *testing.T) {
	urls := reachableURLs("[fe80::1]:19847", hostAddresses)

	want := URLEntry{Interface: boundInterfaceLabel, URL: "http://[fe80::1]:19847/?ws=19847"}
	if len(urls) != 1 || urls[0] != want {
		t.Errorf("reachableURLs = %v, want [%v]", urls, want)
	}
}

func TestReachableURLsRejectsMalformedAddresses(t *testing.T) {
	for _, address := range []string{"", "19847", "localhost:19847", "0.0.0.0:0"} {
		if urls := reachableURLs(address, hostAddresses); len(urls) != 0 {
			t.Errorf("reachableURLs(%q) = %v, want none", address, urls)
		}
	}
}

func TestURLsEmptyOnLoopback(t *testing.T) {
	srv := runServer(t, Options{DistDir: themeDist(t), Address: "127.0.0.1:0"})

	if urls := srv.URLs(); len(urls) != 0 {
		t.Errorf("URLs = %v on a loopback listener, want none", urls)
	}
}

func TestLoopbackListenerIsLocalOnly(t *testing.T) {
	srv := runServer(t, Options{DistDir: themeDist(t), Address: "127.0.0.1:0"})

	if srv.Exposed() {
		t.Error("Exposed = true on a loopback listener")
	}
	want := "http://localhost:" + itoa(srv.Port()) + "/?ws=" + itoa(srv.Port())
	if got := srv.LocalURL(); got != want {
		t.Errorf("LocalURL = %q, want %q", got, want)
	}
}

func TestWildcardListenerIsExposedAndLocal(t *testing.T) {
	srv := runServer(t, Options{DistDir: themeDist(t), Address: "0.0.0.0:0"})

	if !srv.Exposed() {
		t.Error("Exposed = false on a wildcard listener")
	}
	if srv.LocalURL() == "" {
		t.Error("LocalURL is empty, but a wildcard listener accepts loopback")
	}
}

func TestSpecificAddressListenerHasNoLocalURL(t *testing.T) {
	interfaces, err := lan.SystemInterfaces()
	if err != nil {
		t.Fatalf("SystemInterfaces: %v", err)
	}
	addresses := lan.Addresses(interfaces)
	if len(addresses) == 0 {
		t.Skip("host has no non-loopback IPv4 address")
	}

	srv := runServer(t, Options{DistDir: themeDist(t), Address: addresses[0].IP + ":0"})

	if !srv.Exposed() {
		t.Error("Exposed = false on a LAN listener, so the no-authentication warning would be skipped")
	}
	if got := srv.LocalURL(); got != "" {
		t.Errorf("LocalURL = %q, but localhost cannot reach a listener on %s", got, addresses[0].IP)
	}
	urls := srv.URLs()
	if len(urls) != 1 || urls[0].Interface != addresses[0].Interface {
		t.Errorf("URLs = %v, want only %s", urls, addresses[0].Interface)
	}
}

func TestListenerAddressHelpersBeforeRun(t *testing.T) {
	srv, err := New(Options{DistDir: themeDist(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if srv.Exposed() || srv.LocalURL() != "" {
		t.Errorf("Exposed = %v, LocalURL = %q before Run, want false and empty", srv.Exposed(), srv.LocalURL())
	}
}
