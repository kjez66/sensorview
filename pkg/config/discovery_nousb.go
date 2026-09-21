//go:build !usb

package config

// DiscoverDevices reports that this build cannot enumerate USB devices.
func DiscoverDevices(_ bool) ([]DiscoveredDevice, error) {
	return nil, ErrUSBUnsupported
}

// FindConfiguredDevice reports that this build cannot enumerate USB devices.
func FindConfiguredDevice() (*DiscoveredDevice, error) {
	return nil, ErrUSBUnsupported
}
