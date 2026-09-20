//go:build windows

package sensors

import (
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/net"
)

func init() {
	Register(newWindowsNetworkProvider())
}

// virtualInterfaceMarkers matches the adapters Windows always reports but which
// carry no traffic a panel should show: the loopback adapter and the ISATAP /
// Teredo tunnels, plus the Hyper-V switch ports that mirror a physical adapter.
var virtualInterfaceMarkers = []string{
	"loopback",
	"pseudo-interface",
	"isatap",
	"teredo",
	"vethernet",
}

// windowsNetworkProvider provides network sensor data on Windows via
// GetIfTable2.
type windowsNetworkProvider struct {
	readIOCounters func() ([]net.IOCountersStat, error)
	now            func() time.Time

	interfacePattern string
}

// newWindowsNetworkProvider returns a provider reading from the live system.
func newWindowsNetworkProvider() *windowsNetworkProvider {
	return &windowsNetworkProvider{
		readIOCounters: func() ([]net.IOCountersStat, error) { return net.IOCounters(true) },
		now:            time.Now,
	}
}

// windowsNetSample is the previous reading for one interface, used to turn the
// monotonic byte counters into rates.
type windowsNetSample struct {
	rxBytes uint64
	txBytes uint64
	time    time.Time
}

// Meta returns the sensor metadata.
func (p *windowsNetworkProvider) Meta() SensorMeta {
	return SensorMeta{
		ID:          "network",
		Name:        "Network",
		Description: "Network interface statistics",
		Category:    "network",
		Platforms:   []string{"windows"},
		IsArray:     true,
		ArrayKey:    "interface",
		Fields: []FieldDef{
			{Name: "Interface", JSONName: "interface", TSName: "interface", Type: FieldTypeString, Unit: "", Description: "Interface name"},
			{Name: "RxRate", JSONName: "rx_rate", TSName: "rxRate", Type: FieldTypeNumber, Unit: "B/s", Description: "Receive rate"},
			{Name: "TxRate", JSONName: "tx_rate", TSName: "txRate", Type: FieldTypeNumber, Unit: "B/s", Description: "Transmit rate"},
			{Name: "RxTotal", JSONName: "rx_total", TSName: "rxTotal", Type: FieldTypeNumber, Unit: "bytes", Description: "Total bytes received"},
			{Name: "TxTotal", JSONName: "tx_total", TSName: "txTotal", Type: FieldTypeNumber, Unit: "bytes", Description: "Total bytes transmitted"},
		},
	}
}

// Available returns true if network data can be collected.
func (p *windowsNetworkProvider) Available() bool {
	_, err := p.readIOCounters()
	return err == nil
}

// Configure applies the given config to the provider.
func (p *windowsNetworkProvider) Configure(config *Config) {
	if pattern, ok := config.GetStringOption("network.interface"); ok {
		p.interfacePattern = pattern
	}
}

// Options returns the configuration options for this provider.
func (p *windowsNetworkProvider) Options() []OptionDef {
	return []OptionDef{
		{
			Key:         "network.interface",
			Type:        "string",
			Default:     "* (all interfaces)",
			Description: "Network interface filter pattern (supports * wildcard)",
			Example:     "--opt network.interface=Ethernet",
		},
	}
}

// Collect gathers network sensor data.
func (p *windowsNetworkProvider) Collect(state *CollectorState) map[string]interface{} {
	counters, err := p.readIOCounters()
	if err != nil {
		return nil
	}
	now := p.now()

	networks := make([]map[string]interface{}, 0, len(counters))
	for _, counter := range counters {
		if isVirtualInterface(counter.Name) {
			continue
		}
		if !p.matchesFilter(counter.Name) {
			continue
		}

		stateKey := "network_" + counter.Name

		var rxRate, txRate float64
		if prev, ok := GetTyped[windowsNetSample](state, stateKey); ok {
			elapsed := now.Sub(prev.time).Seconds()
			if elapsed > 0 && elapsed < 2*60 {
				rxRate = counterRate(counter.BytesRecv, prev.rxBytes, elapsed)
				txRate = counterRate(counter.BytesSent, prev.txBytes, elapsed)
			}
		}

		state.Set(stateKey, windowsNetSample{
			rxBytes: counter.BytesRecv,
			txBytes: counter.BytesSent,
			time:    now,
		})

		networks = append(networks, map[string]interface{}{
			"interface": counter.Name,
			"rx_rate":   rxRate,
			"tx_rate":   txRate,
			"rx_total":  counter.BytesRecv,
			"tx_total":  counter.BytesSent,
		})
	}

	return map[string]interface{}{
		"_items": networks,
	}
}

// matchesFilter reports whether the interface passes the configured pattern.
func (p *windowsNetworkProvider) matchesFilter(name string) bool {
	if p.interfacePattern == "" || p.interfacePattern == "*" {
		return true
	}
	return strings.Contains(name, p.interfacePattern)
}

// isVirtualInterface reports whether an adapter should be hidden from panels.
func isVirtualInterface(name string) bool {
	lower := strings.ToLower(name)
	for _, marker := range virtualInterfaceMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// counterRate returns bytes per second, or zero when the adapter counter was
// reset and current has fallen behind previous.
func counterRate(current, previous uint64, elapsed float64) float64 {
	if current < previous {
		return 0
	}
	return float64(current-previous) / elapsed
}
