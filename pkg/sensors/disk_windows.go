//go:build windows

package sensors

func init() {
	Register(&windowsDiskProvider{})
}

// windowsDiskProvider provides disk stats on Windows using GetDiskFreeSpaceEx.
type windowsDiskProvider struct {
	mounts []string
}

// defaultDiskMounts returns the default disk mount points for Windows.
func defaultDiskMounts() []string {
	return []string{"C:\\"}
}

func (p *windowsDiskProvider) Meta() SensorMeta {
	return SensorMeta{
		ID:          "disk",
		Name:        "Disk",
		Description: "Disk usage statistics (Windows)",
		Category:    "storage",
		Platforms:   []string{"windows"},
		IsArray:     true,
		ArrayKey:    "mount",
		Fields: []FieldDef{
			{Name: "Mount", JSONName: "mount", TSName: "mount", Type: FieldTypeString, Unit: "", Description: "Drive letter"},
			{Name: "Total", JSONName: "total", TSName: "total", Type: FieldTypeNumber, Unit: "GB", Description: "Total disk space"},
			{Name: "Used", JSONName: "used", TSName: "used", Type: FieldTypeNumber, Unit: "GB", Description: "Used disk space"},
			{Name: "Free", JSONName: "free", TSName: "free", Type: FieldTypeNumber, Unit: "GB", Description: "Free disk space"},
			{Name: "Percent", JSONName: "percent", TSName: "percent", Type: FieldTypeNumber, Unit: "%", Description: "Disk usage percentage"},
		},
	}
}

func (p *windowsDiskProvider) Available() bool {
	return true // GetDiskFreeSpaceEx works on Windows
}

// Configure applies the given config to the provider.
func (p *windowsDiskProvider) Configure(config *Config) {
	if mounts, ok := config.GetStringSliceOption("disk.mounts"); ok {
		p.mounts = mounts
	}
}

// Options returns the configuration options for this provider.
func (p *windowsDiskProvider) Options() []OptionDef {
	return []OptionDef{
		{
			Key:         "disk.mounts",
			Type:        "[]string",
			Default:     "/ (Linux/macOS), C:\\ (Windows)",
			Description: "Disk mount points to monitor",
			Example:     "--opt disk.mounts=C:\\,D:\\",
		},
	}
}

func (p *windowsDiskProvider) Collect(state *CollectorState) map[string]interface{} {
	mounts := p.mounts
	if len(mounts) == 0 {
		mounts = defaultDiskMounts()
	}

	disks := make([]map[string]interface{}, 0, len(mounts))

	for _, mount := range mounts {
		var stat syscallStatfs
		if err := statfs(mount, &stat); err != nil {
			continue
		}

		// Windows statfs returns bytes directly (Bsize=1)
		totalBytes := stat.Blocks
		freeBytes := stat.Bfree
		availBytes := stat.Bavail
		usedBytes := totalBytes - freeBytes

		totalGB := float64(totalBytes) / (1024 * 1024 * 1024)
		usedGB := float64(usedBytes) / (1024 * 1024 * 1024)
		freeGB := float64(availBytes) / (1024 * 1024 * 1024)

		var percent float64
		if totalGB > 0 {
			percent = (usedGB / totalGB) * 100.0
		}

		disks = append(disks, map[string]interface{}{
			"mount":   mount,
			"total":   totalGB,
			"used":    usedGB,
			"free":    freeGB,
			"percent": percent,
		})
	}

	return map[string]interface{}{
		"_items": disks,
	}
}
