//go:build windows

package sensors

import (
	"errors"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
)

func init() {
	Register(newWindowsCPUProvider())
}

// errNoCPUTimes reports that Windows returned no aggregate time counters.
var errNoCPUTimes = errors.New("no aggregate CPU time counters available")

// windowsCPUProvider provides CPU sensor data on Windows.
//
// Load is derived from the aggregate CPU time counters (GetSystemTimes) rather
// than gopsutil's cpu.Percent, which keeps its previous sample in package-level
// state shared with every other caller. Temperature is deliberately absent:
// reading it needs MSR access through a signed kernel driver, so there is
// nothing to report from user space.
type windowsCPUProvider struct {
	readTimes func() (cpu.TimesStat, error)
	readInfo  func() ([]cpu.InfoStat, error)
	now       func() time.Time

	discovered bool
	name       string
	frequency  *float64
}

// newWindowsCPUProvider returns a provider reading from the live system.
func newWindowsCPUProvider() *windowsCPUProvider {
	return &windowsCPUProvider{
		readTimes: aggregateCPUTimes,
		readInfo:  cpu.Info,
		now:       time.Now,
	}
}

// aggregateCPUTimes returns the system-wide CPU time counters.
func aggregateCPUTimes() (cpu.TimesStat, error) {
	times, err := cpu.Times(false)
	if err != nil {
		return cpu.TimesStat{}, err
	}
	if len(times) == 0 {
		return cpu.TimesStat{}, errNoCPUTimes
	}
	return times[0], nil
}

// Meta returns the sensor metadata.
func (p *windowsCPUProvider) Meta() SensorMeta {
	return SensorMeta{
		ID:          "cpu",
		Name:        "CPU",
		Description: "CPU usage, temperature, and frequency",
		Category:    "system",
		Platforms:   []string{"windows"},
		Fields: []FieldDef{
			{Name: "Name", JSONName: "name", TSName: "name", Type: FieldTypeString, Unit: "", Description: "CPU model name"},
			{Name: "Load", JSONName: "load", TSName: "load", Type: FieldTypeNumber, Unit: "%", Description: "CPU load percentage"},
			{Name: "Temperature", JSONName: "temperature", TSName: "temperature", Type: FieldTypeOptionalNumber, Unit: "°C", Description: "CPU temperature"},
			{Name: "Frequency", JSONName: "frequency", TSName: "frequency", Type: FieldTypeOptionalNumber, Unit: "MHz", Description: "CPU frequency"},
			{Name: "Cores", JSONName: "cores", TSName: "cores", Type: FieldTypeNumber, Unit: "", Description: "Number of CPU cores"},
		},
	}
}

// Available returns true if CPU data can be collected.
func (p *windowsCPUProvider) Available() bool {
	_, err := p.readTimes()
	return err == nil
}

// Collect gathers CPU sensor data.
func (p *windowsCPUProvider) Collect(state *CollectorState) map[string]interface{} {
	times, err := p.readTimes()
	if err != nil {
		return nil
	}
	p.discover()

	result := map[string]interface{}{
		"name":  p.name,
		"load":  p.collectLoad(state, times),
		"cores": runtime.NumCPU(),
	}

	if p.frequency != nil {
		result["frequency"] = *p.frequency
	}

	// No temperature key: see the type comment. An absent field renders as
	// "unavailable" in themes, whereas a zero renders as a real 0 °C reading.
	return result
}

// collectLoad turns the monotonic time counters into a percentage using the
// previous sample. The first tick after startup has nothing to compare against
// and reports zero, matching the Linux provider.
func (p *windowsCPUProvider) collectLoad(state *CollectorState, times cpu.TimesStat) float64 {
	now := p.now()
	total := times.Total()
	idle := times.Idle

	prevIdle, _ := GetTyped[float64](state, "cpu_prev_idle")
	prevTotal, _ := GetTyped[float64](state, "cpu_prev_total")
	prevTime, hasPrev := GetTyped[time.Time](state, "cpu_prev_time")

	state.Set("cpu_prev_idle", idle)
	state.Set("cpu_prev_total", total)
	state.Set("cpu_prev_time", now)

	if !hasPrev || now.Sub(prevTime) > 2*time.Minute {
		return 0
	}

	totalDelta := total - prevTotal
	idleDelta := idle - prevIdle
	if totalDelta <= 0 {
		return 0
	}

	return clampPercent((1.0 - idleDelta/totalDelta) * 100.0)
}

// discover reads the static CPU description once. cpu.Info goes through the
// registry and WMI on Windows, which is far too slow to repeat every tick.
func (p *windowsCPUProvider) discover() {
	if p.discovered {
		return
	}
	p.discovered = true
	p.name = "Unknown CPU"

	info, err := p.readInfo()
	if err != nil || len(info) == 0 {
		return
	}

	if name := strings.TrimSpace(info[0].ModelName); name != "" {
		p.name = name
	}
	if mhz := info[0].Mhz; mhz > 0 {
		p.frequency = &mhz
	}
}

// clampPercent keeps a computed percentage within 0-100. Counter deltas can
// fall outside that range when Windows updates per-core times unevenly.
func clampPercent(value float64) float64 {
	switch {
	case value < 0:
		return 0
	case value > 100:
		return 100
	default:
		return value
	}
}
