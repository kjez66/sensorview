//go:build windows

package sensors

import (
	"errors"
	"testing"

	"github.com/shirou/gopsutil/v4/mem"
)

const gib = 1024 * 1024 * 1024

func testMemoryProvider(total, available uint64) *windowsMemoryProvider {
	p := newWindowsMemoryProvider()
	p.readVirtualMemory = func() (*mem.VirtualMemoryStat, error) {
		return &mem.VirtualMemoryStat{Total: total, Available: available}, nil
	}
	return p
}

func TestWindowsMemoryReportsMegabytes(t *testing.T) {
	p := testMemoryProvider(16*gib, 8*gib)
	data := p.Collect(NewCollectorState())

	if got := data["total"].(float64); got != 16384 {
		t.Errorf("total = %v MB, want 16384", got)
	}
	if got := data["available"].(float64); got != 8192 {
		t.Errorf("available = %v MB, want 8192", got)
	}
	if got := data["used"].(float64); got != 8192 {
		t.Errorf("used = %v MB, want 8192", got)
	}
	if got := data["percent"].(float64); got != 50 {
		t.Errorf("percent = %v, want 50", got)
	}
}

func TestWindowsMemoryUsedIsTotalMinusAvailable(t *testing.T) {
	// The gopsutil Used field on Windows counts committed bytes, which runs
	// well above what the Linux provider reports. Themes compare the two
	// platforms, so derive used the same way Linux does.
	p := newWindowsMemoryProvider()
	p.readVirtualMemory = func() (*mem.VirtualMemoryStat, error) {
		return &mem.VirtualMemoryStat{Total: 16 * gib, Available: 12 * gib, Used: 15 * gib}, nil
	}
	data := p.Collect(NewCollectorState())

	if got := data["used"].(float64); got != 4096 {
		t.Errorf("used = %v MB, want 4096 (total - available)", got)
	}
}

func TestWindowsMemoryZeroTotalYieldsZeroPercent(t *testing.T) {
	p := testMemoryProvider(0, 0)
	data := p.Collect(NewCollectorState())

	if got := data["percent"].(float64); got != 0 {
		t.Errorf("percent = %v, want 0 (no NaN from dividing by a zero total)", got)
	}
}

func TestWindowsMemoryCollectNilOnReadError(t *testing.T) {
	p := newWindowsMemoryProvider()
	p.readVirtualMemory = func() (*mem.VirtualMemoryStat, error) { return nil, errors.New("boom") }

	if data := p.Collect(NewCollectorState()); data != nil {
		t.Errorf("Collect = %v, want nil when memory cannot be read", data)
	}
}

func TestWindowsMemoryAvailability(t *testing.T) {
	p := testMemoryProvider(16*gib, 8*gib)
	if !p.Available() {
		t.Error("Available() = false with readable memory, want true")
	}

	p.readVirtualMemory = func() (*mem.VirtualMemoryStat, error) { return nil, errors.New("boom") }
	if p.Available() {
		t.Error("Available() = true with unreadable memory, want false")
	}
}
