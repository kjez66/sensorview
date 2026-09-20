//go:build windows

package sensors

import (
	"errors"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/net"
)

func testNetworkProvider(counters ...net.IOCountersStat) *windowsNetworkProvider {
	p := newWindowsNetworkProvider()
	p.readIOCounters = func() ([]net.IOCountersStat, error) { return counters, nil }
	return p
}

// networkItem returns the collected item for the named interface.
func networkItem(t *testing.T, data map[string]interface{}, name string) map[string]interface{} {
	t.Helper()
	for _, item := range networkItems(t, data) {
		if item["interface"] == name {
			return item
		}
	}
	return nil
}

func networkItems(t *testing.T, data map[string]interface{}) []map[string]interface{} {
	t.Helper()
	items, ok := data["_items"].([]map[string]interface{})
	if !ok {
		t.Fatalf("_items missing or of wrong type in %v", data)
	}
	return items
}

func networkNames(t *testing.T, data map[string]interface{}) []string {
	t.Helper()
	items := networkItems(t, data)
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item["interface"].(string))
	}
	return names
}

func TestWindowsNetworkFirstTickReportsTotalsWithoutRates(t *testing.T) {
	p := testNetworkProvider(net.IOCountersStat{Name: "Ethernet", BytesRecv: 5000, BytesSent: 1000})
	item := networkItem(t, p.Collect(NewCollectorState()), "Ethernet")

	if item == nil {
		t.Fatal("Ethernet missing from collected items")
	}
	if got := item["rx_total"].(uint64); got != 5000 {
		t.Errorf("rx_total = %v, want 5000", got)
	}
	if got := item["tx_total"].(uint64); got != 1000 {
		t.Errorf("tx_total = %v, want 1000", got)
	}
	if got := item["rx_rate"].(float64); got != 0 {
		t.Errorf("first tick rx_rate = %v, want 0", got)
	}
}

func TestWindowsNetworkRatesFromCounterDelta(t *testing.T) {
	state := NewCollectorState()
	now := time.Now()
	p := testNetworkProvider(net.IOCountersStat{Name: "Ethernet", BytesRecv: 5000, BytesSent: 1000})
	p.now = func() time.Time { return now }
	p.Collect(state)

	// Two seconds later: 4000 more bytes received, 2000 more sent.
	p.now = func() time.Time { return now.Add(2 * time.Second) }
	p.readIOCounters = func() ([]net.IOCountersStat, error) {
		return []net.IOCountersStat{{Name: "Ethernet", BytesRecv: 9000, BytesSent: 3000}}, nil
	}
	item := networkItem(t, p.Collect(state), "Ethernet")

	if got := item["rx_rate"].(float64); got != 2000 {
		t.Errorf("rx_rate = %v B/s, want 2000", got)
	}
	if got := item["tx_rate"].(float64); got != 1000 {
		t.Errorf("tx_rate = %v B/s, want 1000", got)
	}
}

func TestWindowsNetworkCounterResetYieldsZeroRate(t *testing.T) {
	state := NewCollectorState()
	now := time.Now()
	p := testNetworkProvider(net.IOCountersStat{Name: "Ethernet", BytesRecv: 9000, BytesSent: 3000})
	p.now = func() time.Time { return now }
	p.Collect(state)

	// Adapter reset sends the counters backwards. Unsigned subtraction would
	// wrap and report an absurd rate.
	p.now = func() time.Time { return now.Add(time.Second) }
	p.readIOCounters = func() ([]net.IOCountersStat, error) {
		return []net.IOCountersStat{{Name: "Ethernet", BytesRecv: 100, BytesSent: 50}}, nil
	}
	item := networkItem(t, p.Collect(state), "Ethernet")

	if got := item["rx_rate"].(float64); got != 0 {
		t.Errorf("rx_rate after counter reset = %v, want 0", got)
	}
	if got := item["tx_rate"].(float64); got != 0 {
		t.Errorf("tx_rate after counter reset = %v, want 0", got)
	}
}

func TestWindowsNetworkStaleSampleYieldsZeroRate(t *testing.T) {
	state := NewCollectorState()
	now := time.Now()
	p := testNetworkProvider(net.IOCountersStat{Name: "Ethernet", BytesRecv: 5000})
	p.now = func() time.Time { return now }
	p.Collect(state)

	p.now = func() time.Time { return now.Add(3 * time.Minute) }
	p.readIOCounters = func() ([]net.IOCountersStat, error) {
		return []net.IOCountersStat{{Name: "Ethernet", BytesRecv: 9000}}, nil
	}
	item := networkItem(t, p.Collect(state), "Ethernet")

	if got := item["rx_rate"].(float64); got != 0 {
		t.Errorf("rx_rate after stale sample = %v, want 0", got)
	}
}

func TestWindowsNetworkSkipsLoopbackAndTunnelAdapters(t *testing.T) {
	p := testNetworkProvider(
		net.IOCountersStat{Name: "Ethernet", BytesRecv: 1},
		net.IOCountersStat{Name: "Loopback Pseudo-Interface 1", BytesRecv: 1},
		net.IOCountersStat{Name: "isatap.{3B2F1C4D-0000-0000-0000-000000000000}", BytesRecv: 1},
		net.IOCountersStat{Name: "Teredo Tunneling Pseudo-Interface", BytesRecv: 1},
		net.IOCountersStat{Name: "vEthernet (Default Switch)", BytesRecv: 1},
	)
	got := networkNames(t, p.Collect(NewCollectorState()))

	if len(got) != 1 || got[0] != "Ethernet" {
		t.Errorf("interfaces = %v, want only [Ethernet]", got)
	}
}

func TestWindowsNetworkKeepsPhysicalAdapters(t *testing.T) {
	p := testNetworkProvider(
		net.IOCountersStat{Name: "Wi-Fi", BytesRecv: 1},
		net.IOCountersStat{Name: "Ethernet 2", BytesRecv: 1},
	)
	if got := networkNames(t, p.Collect(NewCollectorState())); len(got) != 2 {
		t.Errorf("interfaces = %v, want both Wi-Fi and Ethernet 2", got)
	}
}

func TestWindowsNetworkAppliesInterfaceFilter(t *testing.T) {
	p := testNetworkProvider(
		net.IOCountersStat{Name: "Ethernet", BytesRecv: 1},
		net.IOCountersStat{Name: "Wi-Fi", BytesRecv: 1},
	)
	p.Configure(&Config{Options: map[string]interface{}{"network.interface": "Wi-Fi"}})
	got := networkNames(t, p.Collect(NewCollectorState()))

	if len(got) != 1 || got[0] != "Wi-Fi" {
		t.Errorf("interfaces = %v, want only [Wi-Fi]", got)
	}
}

func TestWindowsNetworkDeclaresInterfaceOption(t *testing.T) {
	opts := newWindowsNetworkProvider().Options()
	for _, opt := range opts {
		if opt.Key == "network.interface" {
			return
		}
	}
	t.Errorf("options = %v, want one keyed network.interface", opts)
}

func TestWindowsNetworkCollectNilOnReadError(t *testing.T) {
	p := newWindowsNetworkProvider()
	p.readIOCounters = func() ([]net.IOCountersStat, error) { return nil, errors.New("boom") }

	if data := p.Collect(NewCollectorState()); data != nil {
		t.Errorf("Collect = %v, want nil when counters cannot be read", data)
	}
}

func TestWindowsNetworkAvailability(t *testing.T) {
	p := testNetworkProvider(net.IOCountersStat{Name: "Ethernet"})
	if !p.Available() {
		t.Error("Available() = false with readable counters, want true")
	}

	p.readIOCounters = func() ([]net.IOCountersStat, error) { return nil, errors.New("boom") }
	if p.Available() {
		t.Error("Available() = true with unreadable counters, want false")
	}
}
