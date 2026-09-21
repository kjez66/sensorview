//go:build windows

package sensors

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

func u32(v uint32) *uint32 { return &v }
func u64(v uint64) *uint64 { return &v }

func TestNVMLReadingConvertsUnits(t *testing.T) {
	reading := nvmlReading{
		Name:        "NVIDIA GeForce RTX 5070",
		Temperature: u32(34),
		LoadPercent: u32(7),
		MemoryUsed:  u64(2 * 1024 * 1024 * 1024),
		MemoryTotal: u64(12 * 1024 * 1024 * 1024),
		PowerMilliW: u32(125500),
		FanPercent:  u32(41),
		ClockMHz:    u32(2550),
		MemClockMHz: u32(10501),
	}
	data := reading.sensorData()

	want := map[string]interface{}{
		"name":         "NVIDIA GeForce RTX 5070",
		"temperature":  float64(34),
		"load":         float64(7),
		"memory_used":  float64(2048),
		"memory_total": float64(12288),
		"power":        125.5,
		"fan_speed":    float64(41),
		"clock":        float64(2550),
		"memory_clock": float64(10501),
	}
	for key, wantValue := range want {
		if got := data[key]; got != wantValue {
			t.Errorf("%s = %v (%T), want %v (%T)", key, got, got, wantValue, wantValue)
		}
	}
	if len(data) != len(want) {
		t.Errorf("got %d keys %v, want %d", len(data), data, len(want))
	}
}

func TestNVMLReadingOmitsUnsupportedFields(t *testing.T) {
	// A driver without PawnIO, or a card that does not expose a fan, fails
	// individual NVML calls while the rest succeed. Absent must stay absent:
	// a zero renders as a real reading in themes.
	reading := nvmlReading{
		Name:        "NVIDIA GeForce RTX 5070",
		Temperature: u32(34),
	}
	data := reading.sensorData()

	if got := data["temperature"]; got != float64(34) {
		t.Errorf("temperature = %v, want 34", got)
	}
	for _, key := range []string{"load", "memory_used", "memory_total", "power", "fan_speed", "clock", "memory_clock"} {
		if _, ok := data[key]; ok {
			t.Errorf("%s present as %v, want the key omitted", key, data[key])
		}
	}
}

func TestNVMLReadingOmitsBlankName(t *testing.T) {
	data := nvmlReading{Name: "   ", Temperature: u32(34)}.sensorData()

	if _, ok := data["name"]; ok {
		t.Errorf("name present as %q, want the key omitted", data["name"])
	}
}

func TestNVMLReadingTrimsName(t *testing.T) {
	data := nvmlReading{Name: "NVIDIA GeForce RTX 5070\x00  "}.sensorData()

	if got := data["name"]; got != "NVIDIA GeForce RTX 5070" {
		t.Errorf("name = %q, want the trimmed name", got)
	}
}

func TestNVMLEmptyReadingYieldsNoData(t *testing.T) {
	if data := (nvmlReading{}).sensorData(); len(data) != 0 {
		t.Errorf("sensorData = %v, want empty", data)
	}
}

func TestNVMLStatusMessages(t *testing.T) {
	tests := []struct {
		status nvmlStatus
		want   string
	}{
		{nvmlErrorDriverNotLoaded, "nvml: driver not loaded"},
		{nvmlErrorNotSupported, "nvml: not supported by this device"},
		{nvmlStatus(4242), "nvml: error 4242"},
	}
	for _, tt := range tests {
		if got := tt.status.Error(); got != tt.want {
			t.Errorf("nvmlStatus(%d).Error() = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestNVMLSuccessIsNotAnError(t *testing.T) {
	if err := nvmlSuccess.err(); err != nil {
		t.Errorf("nvmlSuccess.err() = %v, want nil", err)
	}
	if err := nvmlErrorNotFound.err(); err == nil {
		t.Error("nvmlErrorNotFound.err() = nil, want an error")
	}
}

// The remaining tests run against whatever this host has. They assert the
// contract holds in both directions, so they are meaningful on a machine with
// an NVIDIA GPU and on one without.

func TestNVMLAvailableDoesNotPanic(t *testing.T) {
	available := nvmlAvailable()
	t.Logf("nvmlAvailable() = %v on this host", available)
}

func TestNVMLQueryReturnsDataOrError(t *testing.T) {
	data, err := queryNVML()

	if err != nil {
		if data != nil {
			t.Errorf("queryNVML returned both data %v and error %v, want one", data, err)
		}
		t.Logf("no NVML on this host: %v", err)
		return
	}
	if len(data) == 0 {
		t.Error("queryNVML returned no error and no fields, want at least one field")
	}
	t.Logf("NVML reported %v", data)
}

func TestOptionalProcReturnsNilForMissingExport(t *testing.T) {
	// An older driver exports only some of the entry points. Calling an
	// unresolved LazyProc panics rather than returning an error, so resolution
	// must happen up front and yield nil. kernel32 stands in for nvml.dll so
	// the test runs on a machine with no NVIDIA driver.
	dll := windows.NewLazySystemDLL("kernel32.dll")
	if err := dll.Load(); err != nil {
		t.Skipf("kernel32.dll unavailable: %v", err)
	}

	if proc := optionalProc(dll, "SensorViewNoSuchFunction"); proc != nil {
		t.Fatalf("optionalProc = %v, want nil for an export that does not exist", proc)
	}

	// The nil result must be safe to hand to the readers.
	lib := &nvmlLibrary{}
	if _, err := lib.uint32(lib.deviceGetPowerUsage, 1); !errors.Is(err, errNVMLProcMissing) {
		t.Errorf("uint32 with an unresolved proc = %v, want errNVMLProcMissing", err)
	}
	if _, err := lib.uint32With(lib.deviceGetClockInfo, 1, nvmlClockGraphics); !errors.Is(err, errNVMLProcMissing) {
		t.Errorf("uint32With with an unresolved proc = %v, want errNVMLProcMissing", err)
	}
	if _, err := lib.utilization(1); !errors.Is(err, errNVMLProcMissing) {
		t.Errorf("utilization with an unresolved proc = %v, want errNVMLProcMissing", err)
	}
	if _, err := lib.memory(1); !errors.Is(err, errNVMLProcMissing) {
		t.Errorf("memory with an unresolved proc = %v, want errNVMLProcMissing", err)
	}
	if _, err := lib.name(1); !errors.Is(err, errNVMLProcMissing) {
		t.Errorf("name with an unresolved proc = %v, want errNVMLProcMissing", err)
	}
	if _, err := lib.deviceCount(); !errors.Is(err, errNVMLProcMissing) {
		t.Errorf("deviceCount with an unresolved proc = %v, want errNVMLProcMissing", err)
	}
}

func TestMissingLibraryIsNotUsable(t *testing.T) {
	// The GPU-less case: the DLL simply is not there.
	dll := windows.NewLazySystemDLL("sensorview-no-such-library.dll")
	if err := dll.Load(); err == nil {
		t.Fatal("loading a nonexistent DLL succeeded, want an error")
	}
}
