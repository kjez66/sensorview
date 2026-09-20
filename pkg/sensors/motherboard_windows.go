//go:build windows

package sensors

func init() {
	Register(newWindowsMotherboardProvider())
}

// Field keys for the repeated slots, in the order LHM reports the sensors.
var (
	motherboardFanKeys  = []string{"system_fan1", "system_fan2", "system_fan3"}
	motherboardDIMMKeys = []string{"dimm1_temp", "dimm2_temp", "dimm3_temp", "dimm4_temp"}
)

// windowsMotherboardProvider provides fan, voltage and memory temperatures on
// Windows through the LibreHardwareMonitor bridge. Unlike Linux, where hwmon
// exposes these directly, there is no way to reach them without a kernel
// driver, so this provider is unavailable unless LHM is running with its web
// server enabled.
type windowsMotherboardProvider struct {
	lhm *lhmSource
}

// newWindowsMotherboardProvider returns a provider reading from the shared LHM
// source.
func newWindowsMotherboardProvider() *windowsMotherboardProvider {
	return &windowsMotherboardProvider{lhm: defaultLHMSource}
}

// Meta returns the sensor metadata. The fields match the Linux provider so
// generated TypeScript is identical on both platforms.
func (p *windowsMotherboardProvider) Meta() SensorMeta {
	return SensorMeta{
		ID:          "motherboard",
		Name:        "Motherboard",
		Description: "Motherboard sensors (fans, voltages, temperatures)",
		Category:    "system",
		Platforms:   []string{"windows"},
		Fields: []FieldDef{
			{Name: "CPUFan", JSONName: "cpu_fan", TSName: "cpuFan", Type: FieldTypeOptionalNumber, Unit: "RPM", Description: "CPU fan speed"},
			{Name: "ChipsetFan", JSONName: "chipset_fan", TSName: "chipsetFan", Type: FieldTypeOptionalNumber, Unit: "RPM", Description: "Chipset fan speed"},
			{Name: "SystemFan1", JSONName: "system_fan1", TSName: "systemFan1", Type: FieldTypeOptionalNumber, Unit: "RPM", Description: "System fan 1 speed"},
			{Name: "SystemFan2", JSONName: "system_fan2", TSName: "systemFan2", Type: FieldTypeOptionalNumber, Unit: "RPM", Description: "System fan 2 speed"},
			{Name: "SystemFan3", JSONName: "system_fan3", TSName: "systemFan3", Type: FieldTypeOptionalNumber, Unit: "RPM", Description: "System fan 3 speed"},
			{Name: "CPUVoltage", JSONName: "cpu_voltage", TSName: "cpuVoltage", Type: FieldTypeOptionalNumber, Unit: "V", Description: "CPU core voltage"},
			{Name: "Dimm1Temp", JSONName: "dimm1_temp", TSName: "dimm1Temp", Type: FieldTypeOptionalNumber, Unit: "°C", Description: "DIMM 1 temperature"},
			{Name: "Dimm2Temp", JSONName: "dimm2_temp", TSName: "dimm2Temp", Type: FieldTypeOptionalNumber, Unit: "°C", Description: "DIMM 2 temperature"},
			{Name: "Dimm3Temp", JSONName: "dimm3_temp", TSName: "dimm3Temp", Type: FieldTypeOptionalNumber, Unit: "°C", Description: "DIMM 3 temperature"},
			{Name: "Dimm4Temp", JSONName: "dimm4_temp", TSName: "dimm4Temp", Type: FieldTypeOptionalNumber, Unit: "°C", Description: "DIMM 4 temperature"},
		},
	}
}

// Available returns true if the bridge reports at least one of these sensors.
func (p *windowsMotherboardProvider) Available() bool {
	reading, err := p.lhm.read()
	if err != nil {
		return false
	}
	return len(motherboardSensorData(reading)) > 0
}

// Configure applies the given config to the provider.
func (p *windowsMotherboardProvider) Configure(config *Config) {
	p.lhm.configure(config)
}

// Options returns the configuration options for this provider. The option is
// declared here rather than on every provider that reads from the bridge, so it
// is listed once.
func (p *windowsMotherboardProvider) Options() []OptionDef {
	return []OptionDef{
		{
			Key:         "lhm.url",
			Type:        "string",
			Default:     lhmDefaultURL,
			Description: "LibreHardwareMonitor remote web server URL (enable it in LHM options; CPU temperatures also need PawnIO installed)",
			Example:     "--opt lhm.url=http://localhost:8085/data.json",
		},
	}
}

// Collect gathers motherboard sensor data.
func (p *windowsMotherboardProvider) Collect(state *CollectorState) map[string]interface{} {
	reading, err := p.lhm.read()
	if err != nil {
		return nil
	}

	data := motherboardSensorData(reading)
	if len(data) == 0 {
		// Nothing matched. Publishing an empty sensor would render a panel of
		// zeroes instead of an unavailable one.
		return nil
	}
	return data
}

// motherboardSensorData maps a bridge reading onto the declared fields,
// omitting whatever the board does not report.
func motherboardSensorData(reading lhmReading) map[string]interface{} {
	data := make(map[string]interface{}, len(motherboardFanKeys)+len(motherboardDIMMKeys)+2)

	if reading.CPUFan != nil {
		data["cpu_fan"] = *reading.CPUFan
	}
	if reading.ChipsetFan != nil {
		data["chipset_fan"] = *reading.ChipsetFan
	}
	for i, fan := range reading.SystemFans {
		if i >= len(motherboardFanKeys) {
			break
		}
		data[motherboardFanKeys[i]] = fan
	}
	if reading.CPUVoltage != nil {
		data["cpu_voltage"] = *reading.CPUVoltage
	}
	for i, temp := range reading.DIMMTemps {
		if i >= len(motherboardDIMMKeys) {
			break
		}
		data[motherboardDIMMKeys[i]] = temp
	}

	return data
}
