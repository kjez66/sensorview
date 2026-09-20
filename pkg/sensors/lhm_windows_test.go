//go:build windows

package sensors

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The fixtures below follow the shape LibreHardwareMonitor documents for
// /data.json: a tree of nodes, each with Children, where leaves carry a Value
// formatted for display rather than a number. They were written from that
// documented shape, not captured from a running instance.

// intelTree covers the ordinary desktop case: a package temperature alongside
// per-core temperatures, board fans on the super I/O chip, and a GPU whose
// sensors must not be mistaken for CPU ones. Values use the comma decimal
// separator a Swedish Windows produces.
const intelTree = `{
  "id": 0, "Text": "Sensor", "Min": "Min", "Value": "Value", "Max": "Max", "ImageURL": "",
  "Children": [
    { "id": 1, "Text": "SPELDATORN", "Min": "", "Value": "", "Max": "", "ImageURL": "", "Children": [
      { "id": 2, "Text": "Intel Core i5-10600K", "Value": "", "Children": [
        { "id": 3, "Text": "Voltages", "Value": "", "Children": [
          {"id": 4, "Text": "CPU Core", "Min": "0,712 V", "Value": "1,272 V", "Max": "1,352 V", "Children": [], "SensorId": "/intelcpu/0/voltage/0", "Type": "Voltage"}
        ]},
        { "id": 5, "Text": "Temperatures", "Value": "", "Children": [
          {"id": 6, "Text": "CPU Core #1", "Min": "35,0 °C", "Value": "41,0 °C", "Max": "62,0 °C", "Children": [], "SensorId": "/intelcpu/0/temperature/0", "Type": "Temperature"},
          {"id": 7, "Text": "CPU Core #2", "Min": "36,0 °C", "Value": "47,0 °C", "Max": "64,0 °C", "Children": [], "SensorId": "/intelcpu/0/temperature/1", "Type": "Temperature"},
          {"id": 8, "Text": "CPU Package", "Min": "36,0 °C", "Value": "44,0 °C", "Max": "65,0 °C", "Children": [], "SensorId": "/intelcpu/0/temperature/8", "Type": "Temperature"}
        ]}
      ]},
      { "id": 9, "Text": "Nuvoton NCT6798D", "Value": "", "Children": [
        { "id": 10, "Text": "Fans", "Value": "", "Children": [
          {"id": 11, "Text": "CPU Fan", "Min": "0 RPM", "Value": "1015 RPM", "Max": "1120 RPM", "Children": [], "SensorId": "/lpc/nct6798d/fan/0", "Type": "Fan"},
          {"id": 12, "Text": "Chassis Fan #1", "Min": "0 RPM", "Value": "0 RPM", "Max": "0 RPM", "Children": [], "SensorId": "/lpc/nct6798d/fan/1", "Type": "Fan"},
          {"id": 13, "Text": "Chassis Fan #2", "Min": "0 RPM", "Value": "780 RPM", "Max": "980 RPM", "Children": [], "SensorId": "/lpc/nct6798d/fan/2", "Type": "Fan"},
          {"id": 14, "Text": "Chipset Fan", "Min": "0 RPM", "Value": "2400 RPM", "Max": "2600 RPM", "Children": [], "SensorId": "/lpc/nct6798d/fan/3", "Type": "Fan"}
        ]},
        { "id": 20, "Text": "Voltages", "Value": "", "Children": [
          {"id": 21, "Text": "+12V", "Min": "12,0 V", "Value": "12,1 V", "Max": "12,2 V", "Children": [], "SensorId": "/lpc/nct6798d/voltage/4", "Type": "Voltage"}
        ]}
      ]},
      { "id": 15, "Text": "NVIDIA GeForce RTX 5070", "Value": "", "Children": [
        { "id": 16, "Text": "Temperatures", "Value": "", "Children": [
          {"id": 17, "Text": "GPU Core", "Min": "30,0 °C", "Value": "37,0 °C", "Max": "61,0 °C", "Children": [], "SensorId": "/gpu-nvidia/0/temperature/0", "Type": "Temperature"}
        ]},
        { "id": 18, "Text": "Fans", "Value": "", "Children": [
          {"id": 19, "Text": "GPU Fan", "Min": "0 RPM", "Value": "1200 RPM", "Max": "1400 RPM", "Children": [], "SensorId": "/gpu-nvidia/0/fan/0", "Type": "Fan"}
        ]}
      ]}
    ]}
  ]
}`

// amdTree names its package sensor differently and reports in the English
// format.
const amdTree = `{
  "id": 0, "Text": "Sensor", "Value": "", "Children": [
    { "id": 1, "Text": "DESKTOP", "Value": "", "Children": [
      { "id": 2, "Text": "AMD Ryzen 7 5800X", "Value": "", "Children": [
        { "id": 3, "Text": "Temperatures", "Value": "", "Children": [
          {"id": 4, "Text": "Core (Tctl/Tdie)", "Value": "52.4 °C", "Children": [], "SensorId": "/amdcpu/0/temperature/0", "Type": "Temperature"},
          {"id": 5, "Text": "CCD1 (Tdie)", "Value": "50.1 °C", "Children": [], "SensorId": "/amdcpu/0/temperature/1", "Type": "Temperature"}
        ]}
      ]}
    ]}
  ]
}`

// coresOnlyTree has no package sensor, so the hottest core stands in.
const coresOnlyTree = `{
  "id": 0, "Text": "Sensor", "Value": "", "Children": [
    { "id": 1, "Text": "PC", "Value": "", "Children": [
      { "id": 2, "Text": "Intel Core i7-8700K", "Value": "", "Children": [
        { "id": 3, "Text": "Temperatures", "Value": "", "Children": [
          {"id": 4, "Text": "CPU Core #1", "Value": "41,0 °C", "Children": [], "SensorId": "/intelcpu/0/temperature/0", "Type": "Temperature"},
          {"id": 5, "Text": "CPU Core #2", "Value": "47,0 °C", "Children": [], "SensorId": "/intelcpu/0/temperature/1", "Type": "Temperature"},
          {"id": 6, "Text": "CPU Core #3", "Value": "44,0 °C", "Children": [], "SensorId": "/intelcpu/0/temperature/2", "Type": "Temperature"}
        ]}
      ]}
    ]}
  ]
}`

// legacyTree omits SensorId and Type, as builds before those fields existed
// did. Classification must fall back to the unit and the sensor label.
const legacyTree = `{
  "id": 0, "Text": "Sensor", "Value": "", "Children": [
    { "id": 1, "Text": "PC", "Value": "", "Children": [
      { "id": 2, "Text": "Intel Core i7-4790K", "Value": "", "Children": [
        { "id": 3, "Text": "Temperatures", "Value": "", "Children": [
          {"id": 4, "Text": "CPU Package", "Value": "55,0 °C", "Children": []}
        ]}
      ]},
      { "id": 5, "Text": "Nuvoton NCT6791D", "Value": "", "Children": [
        { "id": 6, "Text": "Fans", "Value": "", "Children": [
          {"id": 7, "Text": "CPU Fan", "Value": "900 RPM", "Children": []}
        ]}
      ]}
    ]}
  ]
}`

// dimmTree exposes per-module temperatures, which only some boards do.
const dimmTree = `{
  "id": 0, "Text": "Sensor", "Value": "", "Children": [
    { "id": 1, "Text": "PC", "Value": "", "Children": [
      { "id": 2, "Text": "Generic Memory", "Value": "", "Children": [
        { "id": 3, "Text": "Temperatures", "Value": "", "Children": [
          {"id": 4, "Text": "DIMM #1", "Value": "38,0 °C", "Children": [], "SensorId": "/ram/temperature/0", "Type": "Temperature"},
          {"id": 5, "Text": "DIMM #2", "Value": "39,5 °C", "Children": [], "SensorId": "/ram/temperature/1", "Type": "Temperature"}
        ]}
      ]}
    ]}
  ]
}`

// emptyTree is what LHM serves before it has polled anything.
const emptyTree = `{"id": 0, "Text": "Sensor", "Value": "", "Children": []}`

// newFixtureSource returns a source reading the given payload over real HTTP,
// plus a pointer to the request count.
func newFixtureSource(t *testing.T, payload string) (*lhmSource, *int) {
	t.Helper()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(payload)); err != nil {
			t.Errorf("writing fixture: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	source := newLHMSource()
	source.url = server.URL
	return source, &requests
}

// readFixture returns the interpreted reading for a payload.
func readFixture(t *testing.T, payload string) lhmReading {
	t.Helper()

	source, _ := newFixtureSource(t, payload)
	reading, err := source.read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return reading
}

func TestParseLHMValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		raw      string
		want     float64
		wantUnit string
		wantOK   bool
	}{
		{name: "celsius with comma separator", raw: "45,0 °C", want: 45, wantUnit: "°C", wantOK: true},
		{name: "celsius with point separator", raw: "45.0 °C", want: 45, wantUnit: "°C", wantOK: true},
		{name: "celsius with non breaking space", raw: "45,0 °C", want: 45, wantUnit: "°C", wantOK: true},
		{name: "rpm", raw: "1015 RPM", want: 1015, wantUnit: "RPM", wantOK: true},
		{name: "volts with comma separator", raw: "1,272 V", want: 1.272, wantUnit: "V", wantOK: true},
		{name: "volts with point separator", raw: "1.272 V", want: 1.272, wantUnit: "V", wantOK: true},
		{name: "grouped thousands then decimal", raw: "1,234.5 MHz", want: 1234.5, wantUnit: "MHz", wantOK: true},
		{name: "grouped with point then comma decimal", raw: "1.234,5 MHz", want: 1234.5, wantUnit: "MHz", wantOK: true},
		{name: "negative", raw: "-5,0 °C", want: -5, wantUnit: "°C", wantOK: true},
		{name: "percent", raw: "37,5 %", want: 37.5, wantUnit: "%", wantOK: true},
		{name: "no unit", raw: "42", want: 42, wantUnit: "", wantOK: true},
		{name: "placeholder dash", raw: "-", wantOK: false},
		{name: "empty", raw: "", wantOK: false},
		{name: "not a number", raw: "N/A", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			value, unit, ok := parseLHMValue(tt.raw)
			if ok != tt.wantOK {
				t.Fatalf("parseLHMValue(%q) ok = %v, want %v", tt.raw, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if value != tt.want {
				t.Errorf("parseLHMValue(%q) value = %v, want %v", tt.raw, value, tt.want)
			}
			if unit != tt.wantUnit {
				t.Errorf("parseLHMValue(%q) unit = %q, want %q", tt.raw, unit, tt.wantUnit)
			}
		})
	}
}

func TestLHMPrefersPackageTemperatureOverCores(t *testing.T) {
	reading := readFixture(t, intelTree)

	if reading.CPUTemperature == nil {
		t.Fatal("no CPU temperature, want the package sensor")
	}
	if *reading.CPUTemperature != 44 {
		t.Errorf("CPU temperature = %v, want 44 from CPU Package rather than the hottest core", *reading.CPUTemperature)
	}
}

func TestLHMFallsBackToHottestCore(t *testing.T) {
	reading := readFixture(t, coresOnlyTree)

	if reading.CPUTemperature == nil {
		t.Fatal("no CPU temperature, want the hottest core")
	}
	if *reading.CPUTemperature != 47 {
		t.Errorf("CPU temperature = %v, want 47 (hottest of the cores)", *reading.CPUTemperature)
	}
}

func TestLHMReadsAMDPackageSensor(t *testing.T) {
	reading := readFixture(t, amdTree)

	if reading.CPUTemperature == nil {
		t.Fatal("no CPU temperature, want Core (Tctl/Tdie)")
	}
	if *reading.CPUTemperature != 52.4 {
		t.Errorf("CPU temperature = %v, want 52.4", *reading.CPUTemperature)
	}
}

func TestLHMIgnoresGPUSensors(t *testing.T) {
	reading := readFixture(t, intelTree)

	// The GPU runs cooler than the CPU here, so a GPU temperature leaking in
	// would be visible.
	if reading.CPUTemperature != nil && *reading.CPUTemperature == 37 {
		t.Error("CPU temperature came from the GPU sensor")
	}
	for _, fan := range reading.SystemFans {
		if fan == 1200 {
			t.Error("GPU fan counted as a system fan; the nvidia_gpu sensor reports it")
		}
	}
	if reading.CPUFan != nil && *reading.CPUFan == 1200 {
		t.Error("GPU fan reported as the CPU fan")
	}
}

func TestLHMMapsFansByLabel(t *testing.T) {
	reading := readFixture(t, intelTree)

	if reading.CPUFan == nil || *reading.CPUFan != 1015 {
		t.Errorf("CPU fan = %v, want 1015", reading.CPUFan)
	}
	if reading.ChipsetFan == nil || *reading.ChipsetFan != 2400 {
		t.Errorf("chipset fan = %v, want 2400", reading.ChipsetFan)
	}
	// The first chassis fan reads 0 RPM, which means no fan on that header.
	if len(reading.SystemFans) != 1 || reading.SystemFans[0] != 780 {
		t.Errorf("system fans = %v, want [780] with the 0 RPM header skipped", reading.SystemFans)
	}
}

func TestLHMReadsCPUVoltage(t *testing.T) {
	reading := readFixture(t, intelTree)

	if reading.CPUVoltage == nil || *reading.CPUVoltage != 1.272 {
		t.Errorf("CPU voltage = %v, want 1.272 from the CPU Core sensor", reading.CPUVoltage)
	}
}

func TestLHMIgnoresBoardRailVoltages(t *testing.T) {
	reading := readFixture(t, intelTree)

	if reading.CPUVoltage != nil && *reading.CPUVoltage == 12.1 {
		t.Error("CPU voltage came from the +12V rail")
	}
}

func TestLHMWorksWithoutSensorIDs(t *testing.T) {
	reading := readFixture(t, legacyTree)

	if reading.CPUTemperature == nil || *reading.CPUTemperature != 55 {
		t.Errorf("CPU temperature = %v, want 55 from a tree with no SensorId fields", reading.CPUTemperature)
	}
	if reading.CPUFan == nil || *reading.CPUFan != 900 {
		t.Errorf("CPU fan = %v, want 900 from a tree with no SensorId fields", reading.CPUFan)
	}
}

func TestLHMReadsDIMMTemperatures(t *testing.T) {
	reading := readFixture(t, dimmTree)

	if len(reading.DIMMTemps) != 2 {
		t.Fatalf("DIMM temperatures = %v, want two", reading.DIMMTemps)
	}
	if reading.DIMMTemps[0] != 38 || reading.DIMMTemps[1] != 39.5 {
		t.Errorf("DIMM temperatures = %v, want [38 39.5]", reading.DIMMTemps)
	}
}

func TestLHMEmptyTreeYieldsNothing(t *testing.T) {
	source, _ := newFixtureSource(t, emptyTree)

	reading, err := source.read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !reading.isEmpty() {
		t.Errorf("reading = %+v, want every field absent", reading)
	}
}

func TestLHMCachesWithinTTL(t *testing.T) {
	source, requests := newFixtureSource(t, intelTree)
	now := time.Now()
	source.now = func() time.Time { return now }

	for i := 0; i < 5; i++ {
		if _, err := source.read(); err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
	}

	if *requests != 1 {
		t.Errorf("made %d requests, want 1; the cpu and motherboard providers poll on different cadences and must share one poll", *requests)
	}
}

func TestLHMRefetchesAfterTTL(t *testing.T) {
	source, requests := newFixtureSource(t, intelTree)
	now := time.Now()
	source.now = func() time.Time { return now }

	if _, err := source.read(); err != nil {
		t.Fatalf("first read: %v", err)
	}
	now = now.Add(source.ttl + time.Millisecond)
	if _, err := source.read(); err != nil {
		t.Fatalf("second read: %v", err)
	}

	if *requests != 2 {
		t.Errorf("made %d requests, want 2 once the cached reading went stale", *requests)
	}
}

func TestLHMBacksOffAfterFailure(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	source := newLHMSource()
	source.url = server.URL
	now := time.Now()
	source.now = func() time.Time { return now }

	if _, err := source.read(); err == nil {
		t.Fatal("read succeeded against a failing server, want an error")
	}

	// A failure must not be retried on the ordinary cadence: a port that drops
	// packets instead of refusing would stall the render loop once per tick.
	now = now.Add(source.ttl + time.Millisecond)
	if _, err := source.read(); err == nil {
		t.Fatal("second read succeeded, want the cached error")
	}
	if requests != 1 {
		t.Errorf("made %d requests, want 1 while the failure is still cached", requests)
	}

	now = now.Add(source.errorTTL)
	if _, err := source.read(); err == nil {
		t.Fatal("third read succeeded, want an error")
	}
	if requests != 2 {
		t.Errorf("made %d requests, want 2 after the backoff elapsed", requests)
	}
}

func TestLHMRejectsServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	source := newLHMSource()
	source.url = server.URL

	if _, err := source.read(); err == nil {
		t.Error("read succeeded on a 404, want an error")
	}
}

func TestLHMRejectsMalformedJSON(t *testing.T) {
	source, _ := newFixtureSource(t, `{"Children": [`)

	if _, err := source.read(); err == nil {
		t.Error("read succeeded on truncated JSON, want an error")
	}
}

func TestLHMRejectsOversizedBody(t *testing.T) {
	source, _ := newFixtureSource(t, intelTree)
	source.maxBody = 16

	if _, err := source.read(); err == nil {
		t.Error("read succeeded on a body over the limit, want an error")
	}
}

func TestLHMUnavailableWhenNothingListens(t *testing.T) {
	source := newLHMSource()
	source.fetch = func(context.Context, string) ([]byte, error) { return nil, errLHMUnavailable }

	_, err := source.read()
	if !errors.Is(err, errLHMUnavailable) {
		t.Errorf("read error = %v, want errLHMUnavailable", err)
	}
}

func TestLHMDefaultURLIsTheLocalWebServer(t *testing.T) {
	if got := newLHMSource().url; got != lhmDefaultURL {
		t.Errorf("default url = %q, want %q", got, lhmDefaultURL)
	}
	if lhmDefaultURL != "http://localhost:8085/data.json" {
		t.Errorf("default url = %q, want the documented LHM remote web server address", lhmDefaultURL)
	}
}

func TestLHMURLIsConfigurable(t *testing.T) {
	source := newLHMSource()
	source.configure(&Config{Options: map[string]interface{}{"lhm.url": "http://192.168.1.10:8085/data.json"}})

	if source.url != "http://192.168.1.10:8085/data.json" {
		t.Errorf("url = %q, want the configured address", source.url)
	}
}
