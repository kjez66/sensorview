// Package config provides USB device discovery for sensorview.
//
// USB support is optional. The gousb dependency needs cgo and libusb, which the
// network display path does not, so the USB-backed half of this file lives in
// discovery_usb.go behind the "usb" build tag. Builds without that tag get the
// stubs in discovery_nousb.go, which report ErrUSBUnsupported.
package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kjez66/sensorview/pkg/device"
)

// ErrUSBUnsupported is returned by the USB discovery functions in builds
// compiled without the "usb" build tag.
var ErrUSBUnsupported = errors.New("this build has no USB panel support - rebuild with 'go build -tags usb' (needs a C toolchain and libusb)")

// DiscoveredDevice represents a USB device found during scanning.
type DiscoveredDevice struct {
	VendorID     uint16
	ProductID    uint16
	Manufacturer string
	Product      string
	Serial       string
	Speed        string
	BusAddr      string // Bus:Address for identification
	IsProbable   bool   // Likely to be a display panel based on heuristics
}

// String returns a human-readable description.
func (d DiscoveredDevice) String() string {
	var parts []string
	parts = append(parts, fmt.Sprintf("%04x:%04x", d.VendorID, d.ProductID))

	if d.Manufacturer != "" || d.Product != "" {
		name := strings.TrimSpace(d.Manufacturer + " " + d.Product)
		if name != "" {
			parts = append(parts, name)
		}
	}

	if d.Serial != "" {
		parts = append(parts, fmt.Sprintf("S/N:%s", d.Serial))
	}

	return strings.Join(parts, " - ")
}

// ToUSBDevice converts to a USBDevice for config storage.
func (d DiscoveredDevice) ToUSBDevice() USBDevice {
	return USBDevice{
		VendorID:  d.VendorID,
		ProductID: d.ProductID,
		Serial:    d.Serial,
	}
}

// isKnownDisplay checks if a device matches known display panels from the device registry.
func isKnownDisplay(vid, pid uint16) bool {
	return device.IsKnownDevice(vid, pid)
}

// AutoDetectOrPrompt tries to find a display device automatically.
// Returns the device to use, whether user interaction was needed, and any error.
func AutoDetectOrPrompt() (*DiscoveredDevice, bool, error) {
	// First, try to find configured device
	if dev, err := FindConfiguredDevice(); err == nil {
		return dev, false, nil
	}

	// Try to find known devices
	known, err := DiscoverDevices(true)
	if errors.Is(err, ErrUSBUnsupported) {
		return nil, false, err
	}
	if err == nil && len(known) == 1 {
		// Exactly one known device found - use it
		return &known[0], false, nil
	}

	if err == nil && len(known) > 1 {
		// Multiple known devices - need user selection
		return nil, true, fmt.Errorf("multiple display devices found, please select one")
	}

	// No known devices - try heuristics
	probable, err := DiscoverDevices(false)
	if err != nil {
		return nil, false, fmt.Errorf("failed to scan USB devices: %w", err)
	}

	// Filter to only probable displays
	var displays []DiscoveredDevice
	for _, d := range probable {
		if d.IsProbable {
			displays = append(displays, d)
		}
	}

	if len(displays) == 0 {
		return nil, true, fmt.Errorf("no display devices found")
	}

	if len(displays) == 1 {
		return &displays[0], false, nil
	}

	// Multiple probable devices - need user selection
	return nil, true, fmt.Errorf("multiple potential display devices found, please select one")
}
