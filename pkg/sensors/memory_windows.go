//go:build windows

package sensors

import (
	"github.com/shirou/gopsutil/v4/mem"
)

func init() {
	Register(newWindowsMemoryProvider())
}

// bytesPerMB converts the byte counts from GlobalMemoryStatusEx to the
// megabytes the themes expect.
const bytesPerMB = 1024 * 1024

// windowsMemoryProvider provides memory sensor data on Windows via
// GlobalMemoryStatusEx.
type windowsMemoryProvider struct {
	readVirtualMemory func() (*mem.VirtualMemoryStat, error)
}

// newWindowsMemoryProvider returns a provider reading from the live system.
func newWindowsMemoryProvider() *windowsMemoryProvider {
	return &windowsMemoryProvider{readVirtualMemory: mem.VirtualMemory}
}

// Meta returns the sensor metadata.
func (p *windowsMemoryProvider) Meta() SensorMeta {
	return SensorMeta{
		ID:          "memory",
		Name:        "Memory",
		Description: "System memory (RAM) usage",
		Category:    "system",
		Platforms:   []string{"windows"},
		Fields: []FieldDef{
			{Name: "Total", JSONName: "total", TSName: "total", Type: FieldTypeNumber, Unit: "MB", Description: "Total memory"},
			{Name: "Used", JSONName: "used", TSName: "used", Type: FieldTypeNumber, Unit: "MB", Description: "Used memory"},
			{Name: "Available", JSONName: "available", TSName: "available", Type: FieldTypeNumber, Unit: "MB", Description: "Available memory"},
			{Name: "Percent", JSONName: "percent", TSName: "percent", Type: FieldTypeNumber, Unit: "%", Description: "Memory usage percentage"},
		},
	}
}

// Available returns true if memory data can be collected.
func (p *windowsMemoryProvider) Available() bool {
	_, err := p.readVirtualMemory()
	return err == nil
}

// Collect gathers memory sensor data.
func (p *windowsMemoryProvider) Collect(state *CollectorState) map[string]interface{} {
	stat, err := p.readVirtualMemory()
	if err != nil || stat == nil {
		return nil
	}

	totalMB := float64(stat.Total) / bytesPerMB
	availableMB := float64(stat.Available) / bytesPerMB

	// Derived the same way as the Linux provider rather than taken from
	// stat.Used, which on Windows counts committed bytes and reads high.
	usedMB := totalMB - availableMB

	var percent float64
	if totalMB > 0 {
		percent = (usedMB / totalMB) * 100.0
	}

	return map[string]interface{}{
		"total":     totalMB,
		"used":      usedMB,
		"available": availableMB,
		"percent":   percent,
	}
}
