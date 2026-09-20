//go:build windows

package sensors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CPU temperature, fan RPM and voltages cannot be read from user space on
// Windows: they need MSR and super I/O access through a signed kernel driver.
// LibreHardwareMonitor ships that driver and exposes its readings over an
// optional HTTP server, which this file bridges. The server is off by default
// in LHM, and CPU temperatures additionally need PawnIO installed, so every
// path here degrades to an absent field rather than a zero.
const (
	lhmDefaultURL     = "http://localhost:8085/data.json"
	lhmDefaultTTL     = time.Second
	lhmErrorTTL       = 30 * time.Second
	lhmRequestTimeout = 500 * time.Millisecond
	lhmMaxBodyBytes   = 4 << 20
	lhmMaxTreeDepth   = 32
)

// Units as LHM formats them, used to classify a sensor without relying on the
// Type field, which older builds do not emit.
const (
	lhmUnitCelsius = "°C"
	lhmUnitRPM     = "RPM"
	lhmUnitVolts   = "V"
)

var (
	// errLHMUnavailable reports that the LHM web server could not be reached,
	// which is the ordinary case: it is disabled by default.
	errLHMUnavailable = errors.New("librehardwaremonitor is not reachable")

	// lhmPackageLabels are the whole-package temperature sensors, in the order
	// they should be preferred.
	lhmPackageLabels = []string{"CPU Package", "Core (Tctl/Tdie)", "Core (Tctl)", "CPU Die (average)"}

	// lhmVcoreLabels are the core voltage sensors. These often come from the
	// super I/O chip rather than the CPU, so they are matched by label alone.
	lhmVcoreLabels = []string{"CPU Core", "Vcore", "CPU VCore", "CPU Voltage"}

	lhmHTTPClient = &http.Client{Timeout: lhmRequestTimeout}
)

// lhmNode is one node of the LHM sensor tree. Leaves carry Value formatted for
// display, such as "45,0 °C"; SensorId and Type are absent on older builds.
type lhmNode struct {
	Text     string    `json:"Text"`
	Value    string    `json:"Value"`
	SensorID string    `json:"SensorId"`
	Type     string    `json:"Type"`
	Children []lhmNode `json:"Children"`
}

// lhmSensor is one leaf of the tree with its value parsed.
type lhmSensor struct {
	Text     string
	SensorID string
	Value    float64
	Unit     string
}

// lhmReading holds the sensors this project consumes. A nil field means LHM did
// not report it.
type lhmReading struct {
	CPUTemperature *float64
	CPUVoltage     *float64
	CPUFan         *float64
	ChipsetFan     *float64
	SystemFans     []float64
	DIMMTemps      []float64
}

// isEmpty reports whether nothing at all was matched.
func (r lhmReading) isEmpty() bool {
	return r.CPUTemperature == nil &&
		r.CPUVoltage == nil &&
		r.CPUFan == nil &&
		r.ChipsetFan == nil &&
		len(r.SystemFans) == 0 &&
		len(r.DIMMTemps) == 0
}

// lhmSource polls the LHM web server and caches the result. The cpu provider
// collects every second and the motherboard provider every five, so without
// the cache the same tree would be fetched twice.
type lhmSource struct {
	url      string
	ttl      time.Duration
	errorTTL time.Duration
	timeout  time.Duration
	maxBody  int64
	now      func() time.Time
	fetch    func(ctx context.Context, url string) ([]byte, error)

	mu        sync.Mutex
	cached    lhmReading
	cachedErr error
	fetchedAt time.Time
	hasCached bool
}

// defaultLHMSource is shared by every provider that reads from LHM.
var defaultLHMSource = newLHMSource()

// newLHMSource returns a source polling the local LHM web server.
func newLHMSource() *lhmSource {
	source := &lhmSource{
		url:      lhmDefaultURL,
		ttl:      lhmDefaultTTL,
		errorTTL: lhmErrorTTL,
		timeout:  lhmRequestTimeout,
		maxBody:  lhmMaxBodyBytes,
		now:      time.Now,
	}
	source.fetch = source.httpFetch
	return source
}

// configure applies the given config to the source.
func (s *lhmSource) configure(config *Config) {
	url, ok := config.GetStringOption("lhm.url")
	if !ok || url == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.url = url
	s.hasCached = false
}

// read returns the current reading, polling only when the cached one has aged
// out. A failed poll is cached for longer than a successful one: a port that
// drops packets rather than refusing the connection would otherwise stall the
// collection loop on every tick.
func (s *lhmSource) read() (lhmReading, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	if s.hasCached && now.Sub(s.fetchedAt) < s.cacheLifetime() {
		return s.cached, s.cachedErr
	}

	reading, err := s.refresh()
	s.cached = reading
	s.cachedErr = err
	s.fetchedAt = now
	s.hasCached = true
	return reading, err
}

// cacheLifetime returns how long the cached result stays valid.
func (s *lhmSource) cacheLifetime() time.Duration {
	if s.cachedErr != nil {
		return s.errorTTL
	}
	return s.ttl
}

// refresh polls the server and interprets the tree.
func (s *lhmSource) refresh() (lhmReading, error) {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	body, err := s.fetch(ctx, s.url)
	if err != nil {
		return lhmReading{}, err
	}

	var root lhmNode
	if err := json.Unmarshal(body, &root); err != nil {
		return lhmReading{}, fmt.Errorf("decode sensor tree: %w", err)
	}

	return interpretLHMSensors(flattenLHMTree(root, 0)), nil
}

// httpFetch reads the sensor tree over HTTP.
func (s *lhmSource) httpFetch(ctx context.Context, url string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	response, err := lhmHTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errLHMUnavailable, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", errLHMUnavailable, response.StatusCode)
	}

	// Read one byte past the limit so an oversized body is detected rather than
	// silently truncated into invalid JSON.
	body, err := io.ReadAll(io.LimitReader(response.Body, s.maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("read sensor tree: %w", err)
	}
	if int64(len(body)) > s.maxBody {
		return nil, fmt.Errorf("sensor tree exceeds %d bytes", s.maxBody)
	}
	return body, nil
}

// flattenLHMTree walks the tree and returns every node whose value parses as a
// number. Group and hardware nodes carry an empty or non-numeric value and drop
// out on their own.
func flattenLHMTree(node lhmNode, depth int) []lhmSensor {
	if depth > lhmMaxTreeDepth {
		return nil
	}

	var sensors []lhmSensor
	if value, unit, ok := parseLHMValue(node.Value); ok {
		sensors = append(sensors, lhmSensor{
			Text:     strings.TrimSpace(node.Text),
			SensorID: node.SensorID,
			Value:    value,
			Unit:     unit,
		})
	}

	for _, child := range node.Children {
		sensors = append(sensors, flattenLHMTree(child, depth+1)...)
	}
	return sensors
}

// parseLHMValue splits an LHM display value into its number and unit. The
// decimal separator follows the Windows locale, so both "45.0 °C" and
// "45,0 °C" occur, sometimes with a non-breaking space before the unit.
func parseLHMValue(raw string) (float64, string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, "", false
	}

	end := 0
	for end < len(trimmed) {
		c := trimmed[end]
		if (c >= '0' && c <= '9') || c == '+' || c == '-' || c == '.' || c == ',' {
			end++
			continue
		}
		break
	}

	value, ok := parseLHMNumber(trimmed[:end])
	if !ok {
		return 0, "", false
	}

	unit := strings.TrimSpace(strings.TrimLeft(trimmed[end:], "  "))
	return value, unit, true
}

// parseLHMNumber reads a number that may use either separator convention.
// Where both appear, the last one is the decimal separator and the other is
// grouping.
func parseLHMNumber(number string) (float64, bool) {
	lastDot := strings.LastIndexByte(number, '.')
	lastComma := strings.LastIndexByte(number, ',')

	switch {
	case lastDot >= 0 && lastComma >= 0:
		if lastComma > lastDot {
			number = strings.ReplaceAll(number, ".", "")
			number = strings.Replace(number, ",", ".", 1)
		} else {
			number = strings.ReplaceAll(number, ",", "")
		}
	case lastComma >= 0:
		if strings.Count(number, ",") > 1 {
			// Several commas cannot all be decimal points, so they group.
			number = strings.ReplaceAll(number, ",", "")
		} else {
			number = strings.Replace(number, ",", ".", 1)
		}
	}

	value, err := strconv.ParseFloat(number, 64)
	return value, err == nil
}

// interpretLHMSensors picks the handful of sensors this project publishes out
// of everything LHM reports.
func interpretLHMSensors(sensors []lhmSensor) lhmReading {
	cpuFan, chipsetFan, systemFans := lhmBoardFans(sensors)

	return lhmReading{
		CPUTemperature: lhmCPUTemperature(sensors),
		CPUVoltage:     lhmCPUVoltage(sensors),
		CPUFan:         cpuFan,
		ChipsetFan:     chipsetFan,
		SystemFans:     systemFans,
		DIMMTemps:      lhmDIMMTemperatures(sensors),
	}
}

// lhmCPUTemperature prefers a whole-package sensor and falls back to the
// hottest core.
func lhmCPUTemperature(sensors []lhmSensor) *float64 {
	for _, label := range lhmPackageLabels {
		for _, sensor := range sensors {
			if sensor.Unit != lhmUnitCelsius || lhmNotCPU(sensor) {
				continue
			}
			if strings.EqualFold(sensor.Text, label) {
				value := sensor.Value
				return &value
			}
		}
	}

	var hottest *float64
	for _, sensor := range sensors {
		if sensor.Unit != lhmUnitCelsius || lhmNotCPU(sensor) {
			continue
		}
		if !strings.HasPrefix(sensor.Text, "CPU Core #") {
			continue
		}
		if hottest == nil || sensor.Value > *hottest {
			value := sensor.Value
			hottest = &value
		}
	}
	return hottest
}

// lhmCPUVoltage reads the core voltage.
func lhmCPUVoltage(sensors []lhmSensor) *float64 {
	for _, label := range lhmVcoreLabels {
		for _, sensor := range sensors {
			if sensor.Unit != lhmUnitVolts || lhmIsGPU(sensor) {
				continue
			}
			if strings.EqualFold(sensor.Text, label) {
				value := sensor.Value
				return &value
			}
		}
	}
	return nil
}

// lhmBoardFans sorts the board fan headers into the CPU fan, the chipset fan
// and the rest. Headers reading zero have no fan attached. GPU fans belong to
// the nvidia_gpu sensor.
func lhmBoardFans(sensors []lhmSensor) (cpuFan, chipsetFan *float64, systemFans []float64) {
	for _, sensor := range sensors {
		if sensor.Unit != lhmUnitRPM || sensor.Value <= 0 || lhmIsGPU(sensor) {
			continue
		}

		label := strings.ToUpper(sensor.Text)
		value := sensor.Value
		switch {
		case cpuFan == nil && strings.Contains(label, "CPU"):
			cpuFan = &value
		case chipsetFan == nil && strings.Contains(label, "CHIPSET"):
			chipsetFan = &value
		default:
			systemFans = append(systemFans, value)
		}
	}

	// Mirror the Linux provider, where the first populated header becomes the
	// CPU fan when no sensor names itself.
	if cpuFan == nil && len(systemFans) > 0 {
		value := systemFans[0]
		cpuFan = &value
		systemFans = systemFans[1:]
	}

	return cpuFan, chipsetFan, systemFans
}

// lhmDIMMTemperatures collects per-module temperatures, which only boards with
// SMBus access to the modules report.
func lhmDIMMTemperatures(sensors []lhmSensor) []float64 {
	var temps []float64
	for _, sensor := range sensors {
		if sensor.Unit != lhmUnitCelsius {
			continue
		}
		if !strings.Contains(strings.ToUpper(sensor.Text), "DIMM") &&
			!strings.HasPrefix(sensor.SensorID, "/ram/") {
			continue
		}
		temps = append(temps, sensor.Value)
	}
	return temps
}

// lhmNotCPU reports whether a sensor identifies as belonging to other hardware.
// A sensor with no SensorId, as older builds emit, is judged by its label only.
func lhmNotCPU(sensor lhmSensor) bool {
	if sensor.SensorID != "" &&
		!strings.HasPrefix(sensor.SensorID, "/intelcpu/") &&
		!strings.HasPrefix(sensor.SensorID, "/amdcpu/") {
		return true
	}
	return lhmIsGPU(sensor)
}

// lhmIsGPU reports whether a sensor belongs to a graphics card.
func lhmIsGPU(sensor lhmSensor) bool {
	if strings.HasPrefix(sensor.SensorID, "/gpu-") {
		return true
	}
	return strings.HasPrefix(strings.ToUpper(sensor.Text), "GPU")
}
