//go:build linux || windows

package sensors

import (
	"errors"
	"os/exec"
	"slices"
	"testing"
)

// realSMILine is an actual nvidia-smi CSV row, taken from an RTX 5070.
const realSMILine = "NVIDIA GeForce RTX 5070, 34, 1, 2185, 12227, 4.76, 0, 300, 405\n"

// testNvidiaProvider returns a provider with every system seam stubbed out, so
// a test opts in to the behaviour it needs.
func testNvidiaProvider() *NvidiaGPUProvider {
	p := newNvidiaGPUProvider()
	p.nvmlAvailable = func() bool { return false }
	p.queryNVML = func() (map[string]interface{}, error) { return nil, errors.New("no nvml") }
	p.runSMI = func(string) ([]byte, error) { return nil, errors.New("no nvidia-smi") }
	p.smiCandidates = func() []string { return nil }
	p.fileExists = func(string) bool { return false }
	p.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	return p
}

func TestNvidiaSMIParsesRealOutput(t *testing.T) {
	data := parseNvidiaSMI(realSMILine)

	want := map[string]interface{}{
		"name":         "NVIDIA GeForce RTX 5070",
		"temperature":  float64(34),
		"load":         float64(1),
		"memory_used":  float64(2185),
		"memory_total": float64(12227),
		"power":        4.76,
		"fan_speed":    float64(0),
		"clock":        float64(300),
		"memory_clock": float64(405),
	}
	for key, wantValue := range want {
		if got := data[key]; got != wantValue {
			t.Errorf("%s = %v, want %v", key, got, wantValue)
		}
	}
	if len(data) != len(want) {
		t.Errorf("got %d keys %v, want %d", len(data), data, len(want))
	}
}

func TestNvidiaSMIOmitsNotAvailableFields(t *testing.T) {
	// nvidia-smi prints [N/A] for values a card does not expose, commonly fan
	// speed and power draw on laptop GPUs.
	data := parseNvidiaSMI("NVIDIA GeForce RTX 4050 Laptop GPU, 41, 12, 500, 6141, [N/A], [N/A], 1200, 800")

	if got := data["name"]; got != "NVIDIA GeForce RTX 4050 Laptop GPU" {
		t.Errorf("name = %v, want the card name", got)
	}
	if got := data["temperature"]; got != float64(41) {
		t.Errorf("temperature = %v, want 41", got)
	}
	for _, key := range []string{"power", "fan_speed"} {
		if _, ok := data[key]; ok {
			t.Errorf("%s present as %v, want the key omitted", key, data[key])
		}
	}
}

func TestNvidiaSMIRejectsShortOutput(t *testing.T) {
	if data := parseNvidiaSMI("NVIDIA GeForce RTX 5070, 34, 1"); data != nil {
		t.Errorf("parseNvidiaSMI = %v, want nil for a truncated row", data)
	}
}

func TestNvidiaSMIRejectsEmptyOutput(t *testing.T) {
	if data := parseNvidiaSMI("\n"); data != nil {
		t.Errorf("parseNvidiaSMI = %v, want nil for empty output", data)
	}
}

func TestNvidiaSMIToleratesMissingClockColumns(t *testing.T) {
	// Older drivers accept the query but return only the first seven columns.
	data := parseNvidiaSMI("NVIDIA GeForce GTX 1060, 45, 10, 1024, 6144, 75.5, 30")

	if got := data["fan_speed"]; got != float64(30) {
		t.Errorf("fan_speed = %v, want 30", got)
	}
	for _, key := range []string{"clock", "memory_clock"} {
		if _, ok := data[key]; ok {
			t.Errorf("%s present as %v, want the key omitted", key, data[key])
		}
	}
}

func TestNvidiaGPUPrefersNVMLOverSMI(t *testing.T) {
	smiCalled := false
	p := testNvidiaProvider()
	p.queryNVML = func() (map[string]interface{}, error) {
		return map[string]interface{}{"name": "from nvml", "temperature": float64(40)}, nil
	}
	p.runSMI = func(string) ([]byte, error) {
		smiCalled = true
		return []byte(realSMILine), nil
	}
	p.fileExists = func(string) bool { return true }
	p.smiCandidates = func() []string { return []string{"nvidia-smi"} }

	data := p.Collect(NewCollectorState())

	if got := data["name"]; got != "from nvml" {
		t.Errorf("name = %v, want the NVML reading", got)
	}
	if smiCalled {
		t.Error("nvidia-smi was executed although NVML answered; NVML text output is the stable interface")
	}
}

func TestNvidiaGPUFallsBackToSMIWhenNVMLFails(t *testing.T) {
	p := testNvidiaProvider()
	p.smiCandidates = func() []string { return []string{"/usr/bin/nvidia-smi"} }
	p.fileExists = func(string) bool { return true }
	p.runSMI = func(string) ([]byte, error) { return []byte(realSMILine), nil }

	data := p.Collect(NewCollectorState())

	if got := data["name"]; got != "NVIDIA GeForce RTX 5070" {
		t.Errorf("name = %v, want the nvidia-smi reading", got)
	}
}

func TestNvidiaGPUFallsBackToSMIWhenNVMLReturnsNothing(t *testing.T) {
	p := testNvidiaProvider()
	p.queryNVML = func() (map[string]interface{}, error) {
		return map[string]interface{}{}, nil
	}
	p.smiCandidates = func() []string { return []string{"/usr/bin/nvidia-smi"} }
	p.fileExists = func(string) bool { return true }
	p.runSMI = func(string) ([]byte, error) { return []byte(realSMILine), nil }

	data := p.Collect(NewCollectorState())

	if got := data["name"]; got != "NVIDIA GeForce RTX 5070" {
		t.Errorf("name = %v, want the nvidia-smi reading", got)
	}
}

func TestNvidiaGPUCollectNilWithoutAnySource(t *testing.T) {
	if data := testNvidiaProvider().Collect(NewCollectorState()); data != nil {
		t.Errorf("Collect = %v, want nil on a machine with no NVIDIA GPU", data)
	}
}

func TestNvidiaGPUCollectNilWhenSMIFails(t *testing.T) {
	p := testNvidiaProvider()
	p.smiCandidates = func() []string { return []string{"/usr/bin/nvidia-smi"} }
	p.fileExists = func(string) bool { return true }

	if data := p.Collect(NewCollectorState()); data != nil {
		t.Errorf("Collect = %v, want nil when nvidia-smi cannot be executed", data)
	}
}

func TestNvidiaGPUUnavailableWithoutNVMLOrSMI(t *testing.T) {
	if testNvidiaProvider().Available() {
		t.Error("Available() = true on a machine with no NVIDIA GPU, want false")
	}
}

func TestNvidiaGPUAvailableViaNVML(t *testing.T) {
	p := testNvidiaProvider()
	p.nvmlAvailable = func() bool { return true }

	if !p.Available() {
		t.Error("Available() = false with NVML present, want true")
	}
}

func TestNvidiaGPUAvailableViaSMI(t *testing.T) {
	p := testNvidiaProvider()
	p.lookPath = func(string) (string, error) { return "/usr/bin/nvidia-smi", nil }

	if !p.Available() {
		t.Error("Available() = false with nvidia-smi on PATH, want true")
	}
}

func TestNvidiaGPUDiscoveryPrefersConfiguredPath(t *testing.T) {
	p := testNvidiaProvider()
	p.smiCandidates = func() []string { return []string{"/usr/bin/nvidia-smi"} }
	p.fileExists = func(string) bool { return true }
	p.Configure(&Config{Options: map[string]interface{}{"nvidia_gpu.smi_path": "/opt/custom/nvidia-smi"}})

	if got := p.findNvidiaSMI(); got != "/opt/custom/nvidia-smi" {
		t.Errorf("findNvidiaSMI = %q, want the configured path", got)
	}
}

func TestNvidiaGPUDiscoveryUsesFirstExistingCandidate(t *testing.T) {
	p := testNvidiaProvider()
	p.smiCandidates = func() []string { return []string{"/missing/nvidia-smi", "/usr/bin/nvidia-smi"} }
	p.fileExists = func(path string) bool { return path == "/usr/bin/nvidia-smi" }

	if got := p.findNvidiaSMI(); got != "/usr/bin/nvidia-smi" {
		t.Errorf("findNvidiaSMI = %q, want the candidate that exists", got)
	}
}

func TestNvidiaGPUDiscoveryFallsBackToPath(t *testing.T) {
	p := testNvidiaProvider()
	p.lookPath = func(name string) (string, error) {
		if name != "nvidia-smi" {
			t.Errorf("lookPath(%q), want lookPath to be given the bare name so PATHEXT applies on Windows", name)
		}
		return "/usr/bin/nvidia-smi", nil
	}

	if got := p.findNvidiaSMI(); got != "/usr/bin/nvidia-smi" {
		t.Errorf("findNvidiaSMI = %q, want the path lookup result", got)
	}
}

func TestNvidiaGPUMetaCoversBothPlatforms(t *testing.T) {
	meta := newNvidiaGPUProvider().Meta()

	for _, platform := range []string{"linux", "windows"} {
		if !slices.Contains(meta.Platforms, platform) {
			t.Errorf("Platforms = %v, want it to include %q", meta.Platforms, platform)
		}
	}
}
