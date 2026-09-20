//go:build windows

package sensors

import (
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
)

// fixedTimes returns a readTimes func serving the given counters.
func fixedTimes(user, system, idle float64) func() (cpu.TimesStat, error) {
	return func() (cpu.TimesStat, error) {
		return cpu.TimesStat{CPU: "cpu-total", User: user, System: system, Idle: idle}, nil
	}
}

func fixedInfo(name string, mhz float64) func() ([]cpu.InfoStat, error) {
	return func() ([]cpu.InfoStat, error) {
		return []cpu.InfoStat{{ModelName: name, Mhz: mhz}}, nil
	}
}

func testCPUProvider() *windowsCPUProvider {
	p := newWindowsCPUProvider()
	p.readTimes = fixedTimes(30, 10, 60)
	p.readInfo = fixedInfo("Test CPU", 3600)
	p.lhm = offlineLHMSource()
	return p
}

func TestWindowsCPUFirstTickReportsZeroLoad(t *testing.T) {
	p := testCPUProvider()
	data := p.Collect(NewCollectorState())

	if got := data["load"].(float64); got != 0 {
		t.Errorf("first tick load = %v, want 0 (no previous sample to delta against)", got)
	}
}

func TestWindowsCPULoadFromCounterDelta(t *testing.T) {
	state := NewCollectorState()
	now := time.Now()
	p := testCPUProvider()
	p.now = func() time.Time { return now }

	// First sample: total 100, idle 60.
	p.readTimes = fixedTimes(30, 10, 60)
	p.Collect(state)

	// Second sample one second later: total 200, idle 110.
	// idleDelta 50 of totalDelta 100 => 50% busy.
	p.now = func() time.Time { return now.Add(time.Second) }
	p.readTimes = fixedTimes(70, 20, 110)
	data := p.Collect(state)

	if got := data["load"].(float64); got != 50 {
		t.Errorf("load = %v, want 50", got)
	}
}

func TestWindowsCPUStaleSampleReportsZeroLoad(t *testing.T) {
	state := NewCollectorState()
	now := time.Now()
	p := testCPUProvider()
	p.now = func() time.Time { return now }
	p.Collect(state)

	// Three minutes later the previous sample is too old to be a rate.
	p.now = func() time.Time { return now.Add(3 * time.Minute) }
	p.readTimes = fixedTimes(70, 20, 110)
	data := p.Collect(state)

	if got := data["load"].(float64); got != 0 {
		t.Errorf("load after stale sample = %v, want 0", got)
	}
}

func TestWindowsCPUIdenticalCountersReportZeroLoad(t *testing.T) {
	state := NewCollectorState()
	p := testCPUProvider()
	p.Collect(state)
	data := p.Collect(state)

	if got := data["load"].(float64); got != 0 {
		t.Errorf("load with no counter movement = %v, want 0", got)
	}
}

func TestWindowsCPULoadNeverNegative(t *testing.T) {
	state := NewCollectorState()
	now := time.Now()
	p := testCPUProvider()
	p.now = func() time.Time { return now }
	p.readTimes = fixedTimes(30, 10, 60)
	p.Collect(state)

	// Idle grows faster than total, which GetSystemTimes can report across
	// cores. Must clamp rather than produce a negative load.
	p.now = func() time.Time { return now.Add(time.Second) }
	p.readTimes = fixedTimes(10, 10, 120)
	data := p.Collect(state)

	if got := data["load"].(float64); got != 0 {
		t.Errorf("load with idle outpacing total = %v, want 0", got)
	}
}

func TestWindowsCPULoadNeverExceeds100(t *testing.T) {
	state := NewCollectorState()
	now := time.Now()
	p := testCPUProvider()
	p.now = func() time.Time { return now }
	p.readTimes = fixedTimes(30, 10, 60)
	p.Collect(state)

	// Idle counter goes backwards; busy delta then exceeds total delta.
	p.now = func() time.Time { return now.Add(time.Second) }
	p.readTimes = fixedTimes(130, 10, 20)
	data := p.Collect(state)

	if got := data["load"].(float64); got > 100 {
		t.Errorf("load = %v, want <= 100", got)
	}
}

func TestWindowsCPUReportsNameFrequencyAndCores(t *testing.T) {
	p := testCPUProvider()
	data := p.Collect(NewCollectorState())

	if got := data["name"]; got != "Test CPU" {
		t.Errorf("name = %v, want %q", got, "Test CPU")
	}
	if got := data["frequency"]; got != float64(3600) {
		t.Errorf("frequency = %v, want 3600", got)
	}
	if got := data["cores"]; got != runtime.NumCPU() {
		t.Errorf("cores = %v, want %d", got, runtime.NumCPU())
	}
}

func TestWindowsCPUOmitsTemperature(t *testing.T) {
	p := testCPUProvider()
	data := p.Collect(NewCollectorState())

	// Without the LibreHardwareMonitor bridge there is no way to read a CPU
	// temperature on Windows. Reporting a zero would render as a real 0 °C.
	if _, ok := data["temperature"]; ok {
		t.Error("temperature reported on Windows, want the field omitted entirely")
	}
}

func TestWindowsCPUStaticInfoReadOnce(t *testing.T) {
	calls := 0
	p := testCPUProvider()
	p.readInfo = func() ([]cpu.InfoStat, error) {
		calls++
		return []cpu.InfoStat{{ModelName: "Test CPU", Mhz: 3600}}, nil
	}

	state := NewCollectorState()
	p.Collect(state)
	p.Collect(state)

	if calls != 1 {
		t.Errorf("cpu.Info calls = %d, want 1 (static data must be cached, it is a WMI/registry read)", calls)
	}
}

func TestWindowsCPUFallsBackWhenInfoUnavailable(t *testing.T) {
	p := testCPUProvider()
	p.readInfo = func() ([]cpu.InfoStat, error) { return nil, errors.New("wmi unavailable") }
	data := p.Collect(NewCollectorState())

	if got := data["name"]; got != "Unknown CPU" {
		t.Errorf("name = %v, want %q", got, "Unknown CPU")
	}
	if _, ok := data["frequency"]; ok {
		t.Error("frequency reported despite unavailable cpu.Info, want it omitted")
	}
}

func TestWindowsCPUCollectNilOnReadError(t *testing.T) {
	p := testCPUProvider()
	p.readTimes = func() (cpu.TimesStat, error) { return cpu.TimesStat{}, errors.New("boom") }

	if data := p.Collect(NewCollectorState()); data != nil {
		t.Errorf("Collect = %v, want nil when counters cannot be read", data)
	}
}

func TestWindowsCPUAvailability(t *testing.T) {
	p := testCPUProvider()
	if !p.Available() {
		t.Error("Available() = false with readable counters, want true")
	}

	p.readTimes = func() (cpu.TimesStat, error) { return cpu.TimesStat{}, errors.New("boom") }
	if p.Available() {
		t.Error("Available() = true with unreadable counters, want false")
	}
}

func TestWindowsCPUMetaMatchesLinuxFieldContract(t *testing.T) {
	// Themes consume cpu.name as a required string; the Windows meta must
	// declare the same fields as the Linux provider or generated TypeScript
	// diverges by platform.
	want := []string{"name", "load", "temperature", "frequency", "cores"}
	fields := newWindowsCPUProvider().Meta().Fields

	if len(fields) != len(want) {
		t.Fatalf("got %d fields, want %d", len(fields), len(want))
	}
	for i, name := range want {
		if fields[i].JSONName != name {
			t.Errorf("field %d = %q, want %q", i, fields[i].JSONName, name)
		}
	}
}

func TestWindowsCPUReportsTemperatureFromLHM(t *testing.T) {
	source, _ := newFixtureSource(t, intelTree)
	p := testCPUProvider()
	p.lhm = source

	data := p.Collect(NewCollectorState())

	if got := data["temperature"]; got != float64(44) {
		t.Errorf("temperature = %v, want 44 from the LibreHardwareMonitor bridge", got)
	}
}

func TestWindowsCPUOmitsTemperatureWhenLHMReportsNone(t *testing.T) {
	// LHM reachable but with no CPU temperature, which is what a machine
	// without PawnIO installed looks like: GPU temperatures present, CPU
	// temperatures absent from the tree entirely.
	source, _ := newFixtureSource(t, dimmTree)
	p := testCPUProvider()
	p.lhm = source

	data := p.Collect(NewCollectorState())

	if _, ok := data["temperature"]; ok {
		t.Errorf("temperature present as %v, want the key omitted", data["temperature"])
	}
}

func TestWindowsCPUStillCollectsWhenLHMIsDown(t *testing.T) {
	// The bridge is an enhancement, never a dependency: load must survive it.
	state := NewCollectorState()
	p := testCPUProvider()
	p.Collect(state)
	p.readTimes = fixedTimes(70, 20, 110)
	data := p.Collect(state)

	if data == nil {
		t.Fatal("Collect = nil with LHM unreachable, want the ordinary CPU data")
	}
	if got := data["load"].(float64); got != 50 {
		t.Errorf("load = %v, want 50", got)
	}
}
