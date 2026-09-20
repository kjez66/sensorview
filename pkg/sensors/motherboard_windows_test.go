//go:build windows

package sensors

import (
	"context"
	"testing"
)

// offlineLHMSource returns a source that always reports LHM as unreachable,
// which is the state of a machine that is not running it.
func offlineLHMSource() *lhmSource {
	source := newLHMSource()
	source.fetch = func(context.Context, string) ([]byte, error) { return nil, errLHMUnavailable }
	return source
}

func testMotherboardProvider(t *testing.T, payload string) *windowsMotherboardProvider {
	t.Helper()

	source, _ := newFixtureSource(t, payload)
	p := newWindowsMotherboardProvider()
	p.lhm = source
	return p
}

func TestWindowsMotherboardMetaMatchesLinuxFieldContract(t *testing.T) {
	// The themes read data.motherboard.cpuFan and dimm1Temp through dimm4Temp.
	// Declaring the same fields as the Linux provider keeps the generated
	// TypeScript identical on both platforms.
	want := []string{
		"cpu_fan", "chipset_fan", "system_fan1", "system_fan2", "system_fan3",
		"cpu_voltage", "dimm1_temp", "dimm2_temp", "dimm3_temp", "dimm4_temp",
	}
	fields := newWindowsMotherboardProvider().Meta().Fields

	if len(fields) != len(want) {
		t.Fatalf("got %d fields %v, want %d", len(fields), fields, len(want))
	}
	for i, name := range want {
		if fields[i].JSONName != name {
			t.Errorf("field %d = %q, want %q", i, fields[i].JSONName, name)
		}
	}
}

func TestWindowsMotherboardReportsFansAndVoltage(t *testing.T) {
	data := testMotherboardProvider(t, intelTree).Collect(NewCollectorState())

	want := map[string]interface{}{
		"cpu_fan":     float64(1015),
		"chipset_fan": float64(2400),
		"system_fan1": float64(780),
		"cpu_voltage": 1.272,
	}
	for key, wantValue := range want {
		if got := data[key]; got != wantValue {
			t.Errorf("%s = %v, want %v", key, got, wantValue)
		}
	}
}

func TestWindowsMotherboardOmitsAbsentSensors(t *testing.T) {
	data := testMotherboardProvider(t, intelTree).Collect(NewCollectorState())

	// This board reports one usable chassis fan and no per-module temperatures.
	for _, key := range []string{"system_fan2", "system_fan3", "dimm1_temp", "dimm2_temp", "dimm3_temp", "dimm4_temp"} {
		if _, ok := data[key]; ok {
			t.Errorf("%s present as %v, want the key omitted", key, data[key])
		}
	}
}

func TestWindowsMotherboardMapsDIMMTemperatures(t *testing.T) {
	data := testMotherboardProvider(t, dimmTree).Collect(NewCollectorState())

	if got := data["dimm1_temp"]; got != float64(38) {
		t.Errorf("dimm1_temp = %v, want 38", got)
	}
	if got := data["dimm2_temp"]; got != 39.5 {
		t.Errorf("dimm2_temp = %v, want 39.5", got)
	}
}

func TestWindowsMotherboardIgnoresExtraDIMMs(t *testing.T) {
	// The field set stops at four slots; a board with more must not panic.
	reading := lhmReading{DIMMTemps: []float64{30, 31, 32, 33, 34, 35}}
	data := motherboardSensorData(reading)

	if got := data["dimm4_temp"]; got != float64(33) {
		t.Errorf("dimm4_temp = %v, want 33", got)
	}
	if len(data) != 4 {
		t.Errorf("data = %v, want only the four declared slots", data)
	}
}

func TestWindowsMotherboardCollectNilWithoutLHM(t *testing.T) {
	p := newWindowsMotherboardProvider()
	p.lhm = offlineLHMSource()

	if data := p.Collect(NewCollectorState()); data != nil {
		t.Errorf("Collect = %v, want nil when LibreHardwareMonitor is not reachable", data)
	}
}

func TestWindowsMotherboardCollectNilWhenNoSensorsMatch(t *testing.T) {
	// LHM running but reporting nothing we map: publishing an empty sensor
	// would show a panel of zeroes rather than an unavailable one.
	p := testMotherboardProvider(t, emptyTree)

	if data := p.Collect(NewCollectorState()); data != nil {
		t.Errorf("Collect = %v, want nil when no sensor matched", data)
	}
}

func TestWindowsMotherboardUnavailableWithoutLHM(t *testing.T) {
	p := newWindowsMotherboardProvider()
	p.lhm = offlineLHMSource()

	if p.Available() {
		t.Error("Available() = true without LibreHardwareMonitor, want false")
	}
}

func TestWindowsMotherboardAvailableWithLHM(t *testing.T) {
	if !testMotherboardProvider(t, intelTree).Available() {
		t.Error("Available() = false with LibreHardwareMonitor reachable, want true")
	}
}

func TestWindowsMotherboardUnavailableWhenNoSensorsMatch(t *testing.T) {
	if testMotherboardProvider(t, emptyTree).Available() {
		t.Error("Available() = true when no sensor matched, want false")
	}
}

func TestWindowsMotherboardDeclaresURLOption(t *testing.T) {
	opts := newWindowsMotherboardProvider().Options()
	for _, opt := range opts {
		if opt.Key == "lhm.url" {
			return
		}
	}
	t.Errorf("options = %v, want one keyed lhm.url", opts)
}
