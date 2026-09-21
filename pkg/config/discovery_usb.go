//go:build usb

package config

import (
	"fmt"
	"strings"

	"github.com/google/gousb"
)

// isProbableDisplay uses heuristics to guess if a device might be a display.
func isProbableDisplay(desc *gousb.DeviceDesc, manufacturer, product string) bool {
	// Check known devices first
	if isKnownDisplay(uint16(desc.Vendor), uint16(desc.Product)) {
		return true
	}

	// Heuristics for unknown devices:
	// 1. Product name contains display-related keywords
	productLower := strings.ToLower(product)
	manufacturerLower := strings.ToLower(manufacturer)

	displayKeywords := []string{"display", "lcd", "screen", "panel", "aida64", "monitor"}
	for _, kw := range displayKeywords {
		if strings.Contains(productLower, kw) || strings.Contains(manufacturerLower, kw) {
			return true
		}
	}

	// 2. Has bulk endpoints (required for image transfer)
	hasBulkOut := false
	hasBulkIn := false
	for _, cfg := range desc.Configs {
		for _, intf := range cfg.Interfaces {
			for _, alt := range intf.AltSettings {
				for _, ep := range alt.Endpoints {
					if ep.TransferType == gousb.TransferTypeBulk {
						if ep.Direction == gousb.EndpointDirectionOut {
							hasBulkOut = true
						} else {
							hasBulkIn = true
						}
					}
				}
			}
		}
	}

	// Device with bulk endpoints and vendor ID 0x1908 is very likely
	if desc.Vendor == 0x1908 && hasBulkOut && hasBulkIn {
		return true
	}

	return false
}

// DiscoverDevices scans USB bus for potential display devices.
// If knownOnly is true, only returns devices matching KnownDisplayVendors.
// Otherwise, uses heuristics to find probable display devices.
func DiscoverDevices(knownOnly bool) ([]DiscoveredDevice, error) {
	ctx := gousb.NewContext()
	defer ctx.Close()

	var discovered []DiscoveredDevice

	// Enumerate all USB devices
	devices, err := ctx.OpenDevices(func(desc *gousb.DeviceDesc) bool {
		// Open all devices to inspect them
		return true
	})
	if err != nil {
		// Partial errors are common (permission denied, etc.)
		// Continue with devices we could open
	}

	for _, dev := range devices {
		defer dev.Close()

		manufacturer, _ := dev.Manufacturer()
		product, _ := dev.Product()
		serial, _ := dev.SerialNumber()

		isProbable := isProbableDisplay(dev.Desc, manufacturer, product)

		if knownOnly && !isKnownDisplay(uint16(dev.Desc.Vendor), uint16(dev.Desc.Product)) {
			continue
		}

		if !knownOnly || isProbable {
			discovered = append(discovered, DiscoveredDevice{
				VendorID:     uint16(dev.Desc.Vendor),
				ProductID:    uint16(dev.Desc.Product),
				Manufacturer: manufacturer,
				Product:      product,
				Serial:       serial,
				Speed:        dev.Desc.Speed.String(),
				BusAddr:      fmt.Sprintf("%d:%d", dev.Desc.Bus, dev.Desc.Address),
				IsProbable:   isProbable,
			})
		}
	}

	return discovered, nil
}

// FindConfiguredDevice attempts to find and validate the configured device.
// Returns the discovered device info if found, or an error if not found.
func FindConfiguredDevice() (*DiscoveredDevice, error) {
	cfg, err := Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	if cfg.Device.IsZero() {
		return nil, fmt.Errorf("no device configured")
	}

	ctx := gousb.NewContext()
	defer ctx.Close()

	devices, err := ctx.OpenDevices(func(desc *gousb.DeviceDesc) bool {
		return uint16(desc.Vendor) == cfg.Device.VendorID &&
			uint16(desc.Product) == cfg.Device.ProductID
	})
	if err != nil {
		// Partial errors are OK
	}

	if len(devices) == 0 {
		return nil, fmt.Errorf("configured device %s not found", cfg.Device)
	}

	// If serial is specified, find matching device
	for _, dev := range devices {
		defer dev.Close()

		serial, _ := dev.SerialNumber()

		// If config has serial, match it; otherwise take first match
		if cfg.Device.Serial != "" && serial != cfg.Device.Serial {
			continue
		}

		manufacturer, _ := dev.Manufacturer()
		product, _ := dev.Product()

		return &DiscoveredDevice{
			VendorID:     uint16(dev.Desc.Vendor),
			ProductID:    uint16(dev.Desc.Product),
			Manufacturer: manufacturer,
			Product:      product,
			Serial:       serial,
			Speed:        dev.Desc.Speed.String(),
			BusAddr:      fmt.Sprintf("%d:%d", dev.Desc.Bus, dev.Desc.Address),
			IsProbable:   true,
		}, nil
	}

	// Close remaining devices
	for _, dev := range devices {
		dev.Close()
	}

	return nil, fmt.Errorf("configured device with serial %s not found", cfg.Device.Serial)
}
