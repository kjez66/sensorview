//go:build linux || windows

package sensors

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func init() {
	Register(newNvidiaGPUProvider())
}

// nvidiaSMIQuery is the column list requested from nvidia-smi, in the order
// parseNvidiaSMI expects them back.
const nvidiaSMIQuery = "--query-gpu=name,temperature.gpu,utilization.gpu,memory.used," +
	"memory.total,power.draw,fan.speed,clocks.current.graphics,clocks.current.memory"

// nvidiaSMIFields names the numeric columns following the GPU name.
var nvidiaSMIFields = []string{
	"temperature",
	"load",
	"memory_used",
	"memory_total",
	"power",
	"fan_speed",
	"clock",
	"memory_clock",
}

// NvidiaGPUProvider provides NVIDIA GPU sensor data. NVML is preferred and
// nvidia-smi is the fallback: NVIDIA does not guarantee the text output of
// nvidia-smi across driver releases and points long-lived parsers at NVML.
//
// The system seams are fields so that behaviour on a machine without an NVIDIA
// GPU is covered by tests on a machine that has one.
type NvidiaGPUProvider struct {
	nvidiaSMIPath string

	nvmlAvailable func() bool
	queryNVML     func() (map[string]interface{}, error)
	runSMI        func(path string) ([]byte, error)
	smiCandidates func() []string
	fileExists    func(path string) bool
	lookPath      func(name string) (string, error)
}

// newNvidiaGPUProvider returns a provider reading from the live system.
func newNvidiaGPUProvider() *NvidiaGPUProvider {
	return &NvidiaGPUProvider{
		nvmlAvailable: nvmlAvailable,
		queryNVML:     queryNVML,
		runSMI:        runNvidiaSMI,
		smiCandidates: nvidiaSMICandidates,
		fileExists:    fileExists,
		lookPath:      exec.LookPath,
	}
}

// Meta returns the sensor metadata.
func (p *NvidiaGPUProvider) Meta() SensorMeta {
	return SensorMeta{
		ID:          "nvidia_gpu",
		Name:        "NVIDIA GPU",
		Description: "NVIDIA GPU statistics via NVML with nvidia-smi fallback",
		Category:    "gpu",
		Platforms:   []string{"linux", "windows"},
		Fields: []FieldDef{
			{Name: "Name", JSONName: "name", TSName: "name", Type: FieldTypeString, Unit: "", Description: "GPU name"},
			{Name: "Temperature", JSONName: "temperature", TSName: "temperature", Type: FieldTypeOptionalNumber, Unit: "°C", Description: "GPU temperature"},
			{Name: "Load", JSONName: "load", TSName: "load", Type: FieldTypeOptionalNumber, Unit: "%", Description: "GPU utilization"},
			{Name: "MemoryUsed", JSONName: "memory_used", TSName: "memoryUsed", Type: FieldTypeOptionalNumber, Unit: "MB", Description: "VRAM used"},
			{Name: "MemoryTotal", JSONName: "memory_total", TSName: "memoryTotal", Type: FieldTypeOptionalNumber, Unit: "MB", Description: "VRAM total"},
			{Name: "Power", JSONName: "power", TSName: "power", Type: FieldTypeOptionalNumber, Unit: "W", Description: "Power draw"},
			{Name: "FanSpeed", JSONName: "fan_speed", TSName: "fanSpeed", Type: FieldTypeOptionalNumber, Unit: "%", Description: "Fan speed"},
			{Name: "Clock", JSONName: "clock", TSName: "clock", Type: FieldTypeOptionalNumber, Unit: "MHz", Description: "GPU clock speed"},
			{Name: "MemoryClock", JSONName: "memory_clock", TSName: "memoryClock", Type: FieldTypeOptionalNumber, Unit: "MHz", Description: "Memory clock speed"},
		},
	}
}

// Available returns true if NVIDIA GPU data can be collected.
func (p *NvidiaGPUProvider) Available() bool {
	return p.nvmlAvailable() || p.findNvidiaSMI() != ""
}

// Configure applies the given config to the provider.
func (p *NvidiaGPUProvider) Configure(config *Config) {
	if path, ok := config.GetStringOption("nvidia_gpu.smi_path"); ok {
		p.nvidiaSMIPath = path
	}
}

// Options returns the configuration options for this provider.
func (p *NvidiaGPUProvider) Options() []OptionDef {
	return []OptionDef{
		{
			Key:         "nvidia_gpu.smi_path",
			Type:        "string",
			Default:     "nvidia-smi (searched in PATH)",
			Description: "Custom path to nvidia-smi binary",
			Example:     "--opt nvidia_gpu.smi_path=/usr/local/bin/nvidia-smi",
		},
	}
}

// Collect gathers NVIDIA GPU sensor data.
func (p *NvidiaGPUProvider) Collect(state *CollectorState) map[string]interface{} {
	if result, err := p.queryNVML(); err == nil && len(result) > 0 {
		return result
	}

	smiPath := p.findNvidiaSMI()
	if smiPath == "" {
		return nil
	}

	output, err := p.runSMI(smiPath)
	if err != nil {
		return nil
	}
	return parseNvidiaSMI(string(output))
}

// parseNvidiaSMI reads one CSV row of nvidia-smi output. Columns a card does
// not expose come back as [N/A] and are omitted rather than zeroed.
func parseNvidiaSMI(output string) map[string]interface{} {
	line := strings.TrimSpace(output)
	if line == "" {
		return nil
	}

	parts := strings.Split(line, ",")
	if len(parts) < 7 {
		return nil
	}

	result := map[string]interface{}{
		"name": strings.TrimSpace(parts[0]),
	}

	for i, field := range nvidiaSMIFields {
		column := i + 1
		if column >= len(parts) {
			break
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(parts[column]), 64)
		if err != nil {
			continue
		}
		result[field] = value
	}

	return result
}

// runNvidiaSMI queries nvidia-smi for one CSV row.
func runNvidiaSMI(path string) ([]byte, error) {
	return exec.Command(path, nvidiaSMIQuery, "--format=csv,noheader,nounits").Output()
}

// findNvidiaSMI locates nvidia-smi, preferring an explicitly configured path,
// then the known driver locations, then PATH. The result is cached on first
// success.
func (p *NvidiaGPUProvider) findNvidiaSMI() string {
	if p.nvidiaSMIPath != "" {
		return p.nvidiaSMIPath
	}

	for _, path := range p.smiCandidates() {
		if p.fileExists(path) {
			p.nvidiaSMIPath = path
			return path
		}
	}

	// The bare name is deliberate: on Windows LookPath applies PATHEXT and
	// finds nvidia-smi.exe.
	if path, err := p.lookPath("nvidia-smi"); err == nil {
		p.nvidiaSMIPath = path
		return path
	}

	return ""
}

// fileExists reports whether the path can be stat-ed.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
