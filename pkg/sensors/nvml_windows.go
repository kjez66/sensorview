//go:build windows

package sensors

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// NVML constants, from nvml.h.
const (
	nvmlTemperatureGPU uintptr = 0  // NVML_TEMPERATURE_GPU
	nvmlClockGraphics  uintptr = 0  // NVML_CLOCK_GRAPHICS
	nvmlClockMemory    uintptr = 2  // NVML_CLOCK_MEM
	nvmlNameBufferSize uintptr = 96 // NVML_DEVICE_NAME_V2_BUFFER_SIZE
)

var (
	// errNVMLUnavailable reports that nvml.dll could not be loaded or
	// initialised, which is the ordinary case on a machine with no NVIDIA GPU.
	errNVMLUnavailable = errors.New("nvml is unavailable")

	// errNVMLNoFields reports that the device answered but exposed no usable
	// sensor, so there is nothing to publish.
	errNVMLNoFields = errors.New("nvml reported no usable fields")

	// errNVMLProcMissing reports that the installed driver does not export an
	// entry point. Older drivers omit some of the optional ones.
	errNVMLProcMissing = errors.New("nvml entry point not exported by this driver")
)

// nvmlStatus is an nvmlReturn_t result code.
type nvmlStatus uintptr

const (
	nvmlSuccess               nvmlStatus = 0
	nvmlErrorUninitialized    nvmlStatus = 1
	nvmlErrorInvalidArgument  nvmlStatus = 2
	nvmlErrorNotSupported     nvmlStatus = 3
	nvmlErrorNoPermission     nvmlStatus = 4
	nvmlErrorNotFound         nvmlStatus = 6
	nvmlErrorInsufficientSize nvmlStatus = 7
	nvmlErrorDriverNotLoaded  nvmlStatus = 9
	nvmlErrorLibraryNotFound  nvmlStatus = 12
	nvmlErrorFunctionNotFound nvmlStatus = 13
	nvmlErrorGPUIsLost        nvmlStatus = 15
	nvmlErrorUnknown          nvmlStatus = 999
)

// Error implements the error interface for an NVML result code.
func (s nvmlStatus) Error() string {
	switch s {
	case nvmlSuccess:
		return "nvml: success"
	case nvmlErrorUninitialized:
		return "nvml: library not initialised"
	case nvmlErrorInvalidArgument:
		return "nvml: invalid argument"
	case nvmlErrorNotSupported:
		return "nvml: not supported by this device"
	case nvmlErrorNoPermission:
		return "nvml: permission denied"
	case nvmlErrorNotFound:
		return "nvml: not found"
	case nvmlErrorInsufficientSize:
		return "nvml: buffer too small"
	case nvmlErrorDriverNotLoaded:
		return "nvml: driver not loaded"
	case nvmlErrorLibraryNotFound:
		return "nvml: library not found"
	case nvmlErrorFunctionNotFound:
		return "nvml: function not found"
	case nvmlErrorGPUIsLost:
		return "nvml: gpu is lost"
	case nvmlErrorUnknown:
		return "nvml: unknown error"
	default:
		return fmt.Sprintf("nvml: error %d", uintptr(s))
	}
}

// err returns nil for success and the status itself otherwise.
func (s nvmlStatus) err() error {
	if s == nvmlSuccess {
		return nil
	}
	return s
}

// nvmlUtilization mirrors nvmlUtilization_t.
type nvmlUtilization struct {
	GPU    uint32
	Memory uint32
}

// nvmlMemory mirrors nvmlMemory_t (the v1 layout, matching
// nvmlDeviceGetMemoryInfo rather than its _v2 successor).
type nvmlMemory struct {
	Total uint64
	Free  uint64
	Used  uint64
}

// nvmlReading is one device read in the units NVML reports, before conversion.
// A nil field means the driver could not answer that query, which is normal:
// cards vary in what they expose, and a missing fan or power reading is not an
// error worth failing the whole collection over.
type nvmlReading struct {
	Name        string
	Temperature *uint32 // °C
	LoadPercent *uint32 // %
	MemoryUsed  *uint64 // bytes
	MemoryTotal *uint64 // bytes
	PowerMilliW *uint32 // mW
	FanPercent  *uint32 // %
	ClockMHz    *uint32 // MHz
	MemClockMHz *uint32 // MHz
}

// sensorData converts the reading into the provider field names and units,
// omitting every field the device did not report.
func (r nvmlReading) sensorData() map[string]interface{} {
	data := make(map[string]interface{}, 9)

	if name := trimCName(r.Name); name != "" {
		data["name"] = name
	}
	if r.Temperature != nil {
		data["temperature"] = float64(*r.Temperature)
	}
	if r.LoadPercent != nil {
		data["load"] = float64(*r.LoadPercent)
	}
	if r.MemoryUsed != nil && r.MemoryTotal != nil {
		data["memory_used"] = float64(*r.MemoryUsed) / bytesPerMB
		data["memory_total"] = float64(*r.MemoryTotal) / bytesPerMB
	}
	if r.PowerMilliW != nil {
		data["power"] = float64(*r.PowerMilliW) / 1000
	}
	if r.FanPercent != nil {
		data["fan_speed"] = float64(*r.FanPercent)
	}
	if r.ClockMHz != nil {
		data["clock"] = float64(*r.ClockMHz)
	}
	if r.MemClockMHz != nil {
		data["memory_clock"] = float64(*r.MemClockMHz)
	}

	return data
}

// trimCName cuts a NUL-terminated C string down to its Go contents.
func trimCName(name string) string {
	if end := strings.IndexByte(name, 0); end >= 0 {
		name = name[:end]
	}
	return strings.TrimSpace(name)
}

// nvmlLibrary holds the entry points resolved from nvml.dll. The optional
// fields are nil when the installed driver does not export them.
type nvmlLibrary struct {
	deviceGetCount         *windows.LazyProc
	deviceGetHandleByIndex *windows.LazyProc
	deviceGetName          *windows.LazyProc
	deviceGetTemperature   *windows.LazyProc
	deviceGetUtilization   *windows.LazyProc
	deviceGetMemoryInfo    *windows.LazyProc
	deviceGetPowerUsage    *windows.LazyProc
	deviceGetFanSpeed      *windows.LazyProc
	deviceGetClockInfo     *windows.LazyProc
}

var (
	nvmlOnce sync.Once
	nvmlLib  *nvmlLibrary
)

// loadNVML resolves nvml.dll once per process, returning nil when it is not
// usable. Like the cgo implementation on Linux, a failed load is not retried:
// installing a driver mid-run is not a case worth paying a syscall per tick
// for, and a restart picks it up.
func loadNVML() *nvmlLibrary {
	nvmlOnce.Do(func() {
		nvmlLib = openNVML()
	})
	return nvmlLib
}

// openNVML loads nvml.dll and initialises the library.
func openNVML() *nvmlLibrary {
	// NewLazySystemDLL restricts the search to the system directory, where the
	// display driver installs nvml.dll. Resolving a bare name through the
	// default search order would try the executable directory first, which is
	// a DLL planting vector.
	dll := windows.NewLazySystemDLL("nvml.dll")
	if err := dll.Load(); err != nil {
		return nil
	}

	// Every device call needs a successful nvmlInit_v2 first.
	initProc := dll.NewProc("nvmlInit_v2")
	if err := initProc.Find(); err != nil {
		return nil
	}
	status, _, _ := initProc.Call()
	if nvmlStatus(status) != nvmlSuccess {
		return nil
	}

	lib := &nvmlLibrary{
		deviceGetCount:         optionalProc(dll, "nvmlDeviceGetCount_v2"),
		deviceGetHandleByIndex: optionalProc(dll, "nvmlDeviceGetHandleByIndex_v2"),
		deviceGetName:          optionalProc(dll, "nvmlDeviceGetName"),
		deviceGetTemperature:   optionalProc(dll, "nvmlDeviceGetTemperature"),
		deviceGetUtilization:   optionalProc(dll, "nvmlDeviceGetUtilizationRates"),
		deviceGetMemoryInfo:    optionalProc(dll, "nvmlDeviceGetMemoryInfo"),
		deviceGetPowerUsage:    optionalProc(dll, "nvmlDeviceGetPowerUsage"),
		deviceGetFanSpeed:      optionalProc(dll, "nvmlDeviceGetFanSpeed"),
		deviceGetClockInfo:     optionalProc(dll, "nvmlDeviceGetClockInfo"),
	}

	// Without a device handle there is nothing this provider can do.
	if lib.deviceGetHandleByIndex == nil {
		return nil
	}
	return lib
}

// optionalProc resolves an entry point, returning nil when the driver does not
// export it. Resolution must happen up front: calling an unresolved LazyProc
// panics rather than returning an error.
func optionalProc(dll *windows.LazyDLL, name string) *windows.LazyProc {
	proc := dll.NewProc(name)
	if err := proc.Find(); err != nil {
		return nil
	}
	return proc
}

// nvmlAvailable reports whether NVML can be used and sees at least one device.
// A machine with the driver installed but no usable GPU, such as a laptop with
// the discrete card disabled, answers false rather than publishing a sensor
// that only ever reports nothing.
func nvmlAvailable() bool {
	lib := loadNVML()
	if lib == nil {
		return false
	}
	count, err := lib.deviceCount()
	return err == nil && count > 0
}

// queryNVML reads the first GPU. It returns either data or an error, never both
// and never a zeroed reading.
func queryNVML() (map[string]interface{}, error) {
	lib := loadNVML()
	if lib == nil {
		return nil, errNVMLUnavailable
	}

	reading, err := lib.read(0)
	if err != nil {
		return nil, fmt.Errorf("read gpu: %w", err)
	}

	data := reading.sensorData()
	if len(data) == 0 {
		return nil, errNVMLNoFields
	}
	return data, nil
}

// deviceCount returns the number of devices NVML can see.
func (l *nvmlLibrary) deviceCount() (uint32, error) {
	if l.deviceGetCount == nil {
		return 0, errNVMLProcMissing
	}

	var count uint32
	status, _, _ := l.deviceGetCount.Call(uintptr(unsafe.Pointer(&count)))
	if err := nvmlStatus(status).err(); err != nil {
		return 0, err
	}
	return count, nil
}

// read collects every field the device exposes. Only the device handle is
// mandatory; each sensor is best effort, so one unsupported query does not
// discard the others.
func (l *nvmlLibrary) read(index uintptr) (nvmlReading, error) {
	device, err := l.device(index)
	if err != nil {
		return nvmlReading{}, err
	}

	var reading nvmlReading

	if name, err := l.name(device); err == nil {
		reading.Name = name
	}
	if temperature, err := l.uint32With(l.deviceGetTemperature, device, nvmlTemperatureGPU); err == nil {
		reading.Temperature = &temperature
	}
	if utilization, err := l.utilization(device); err == nil {
		load := utilization.GPU
		reading.LoadPercent = &load
	}
	if memory, err := l.memory(device); err == nil {
		used, total := memory.Used, memory.Total
		reading.MemoryUsed = &used
		reading.MemoryTotal = &total
	}
	if power, err := l.uint32(l.deviceGetPowerUsage, device); err == nil {
		reading.PowerMilliW = &power
	}
	if fan, err := l.uint32(l.deviceGetFanSpeed, device); err == nil {
		reading.FanPercent = &fan
	}
	if clock, err := l.uint32With(l.deviceGetClockInfo, device, nvmlClockGraphics); err == nil {
		reading.ClockMHz = &clock
	}
	if memoryClock, err := l.uint32With(l.deviceGetClockInfo, device, nvmlClockMemory); err == nil {
		reading.MemClockMHz = &memoryClock
	}

	return reading, nil
}

// device returns an opaque nvmlDevice_t handle for the given index.
func (l *nvmlLibrary) device(index uintptr) (uintptr, error) {
	var device uintptr
	status, _, _ := l.deviceGetHandleByIndex.Call(index, uintptr(unsafe.Pointer(&device)))
	if err := nvmlStatus(status).err(); err != nil {
		return 0, err
	}
	if device == 0 {
		return 0, errNVMLUnavailable
	}
	return device, nil
}

// name reads the device model name.
func (l *nvmlLibrary) name(device uintptr) (string, error) {
	if l.deviceGetName == nil {
		return "", errNVMLProcMissing
	}

	buffer := make([]byte, nvmlNameBufferSize)
	status, _, _ := l.deviceGetName.Call(
		device,
		uintptr(unsafe.Pointer(&buffer[0])),
		nvmlNameBufferSize,
	)
	if err := nvmlStatus(status).err(); err != nil {
		return "", err
	}
	return trimCName(string(buffer)), nil
}

// uint32 calls an entry point of the shape (device, unsigned int *value).
func (l *nvmlLibrary) uint32(proc *windows.LazyProc, device uintptr) (uint32, error) {
	if proc == nil {
		return 0, errNVMLProcMissing
	}

	var value uint32
	status, _, _ := proc.Call(device, uintptr(unsafe.Pointer(&value)))
	if err := nvmlStatus(status).err(); err != nil {
		return 0, err
	}
	return value, nil
}

// uint32With calls an entry point of the shape
// (device, unsigned int kind, unsigned int *value).
func (l *nvmlLibrary) uint32With(proc *windows.LazyProc, device, kind uintptr) (uint32, error) {
	if proc == nil {
		return 0, errNVMLProcMissing
	}

	var value uint32
	status, _, _ := proc.Call(device, kind, uintptr(unsafe.Pointer(&value)))
	if err := nvmlStatus(status).err(); err != nil {
		return 0, err
	}
	return value, nil
}

// utilization reads the GPU and memory utilisation rates.
func (l *nvmlLibrary) utilization(device uintptr) (nvmlUtilization, error) {
	if l.deviceGetUtilization == nil {
		return nvmlUtilization{}, errNVMLProcMissing
	}

	var utilization nvmlUtilization
	status, _, _ := l.deviceGetUtilization.Call(device, uintptr(unsafe.Pointer(&utilization)))
	if err := nvmlStatus(status).err(); err != nil {
		return nvmlUtilization{}, err
	}
	return utilization, nil
}

// memory reads VRAM totals.
func (l *nvmlLibrary) memory(device uintptr) (nvmlMemory, error) {
	if l.deviceGetMemoryInfo == nil {
		return nvmlMemory{}, errNVMLProcMissing
	}

	var memory nvmlMemory
	status, _, _ := l.deviceGetMemoryInfo.Call(device, uintptr(unsafe.Pointer(&memory)))
	if err := nvmlStatus(status).err(); err != nil {
		return nvmlMemory{}, err
	}
	return memory, nil
}
