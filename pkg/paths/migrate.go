package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

// legacyAppName is the directory name used before the project was renamed from
// SensorPanel to SensorView. Installations that predate the rename keep their
// config, themes and browser cache under it.
const legacyAppName = "sensorpanel"

// MigrateLegacyDirs moves pre-rename sensorpanel directories to their sensorview
// equivalents. Each directory is only moved when the new location does not
// already exist, so a partially migrated install is left alone and a second run
// is a no-op.
//
// Config is migrated before data, and data before cache: on Windows the cache
// lives inside the data directory, so moving the data directory carries the
// cache with it and the cache step then finds its destination already present.
func MigrateLegacyDirs() error {
	steps := []struct {
		name string
		dir  func() (string, error)
	}{
		{"config", ConfigDir},
		{"data", DataDir},
		{"cache", CacheDir},
	}

	for _, step := range steps {
		current, err := step.dir()
		if err != nil {
			return fmt.Errorf("resolve %s directory: %w", step.name, err)
		}
		if err := migrateDir(current); err != nil {
			return fmt.Errorf("migrate %s directory: %w", step.name, err)
		}
	}

	return nil
}

// migrateDir renames the legacy sibling of current to current, when the legacy
// directory exists and current does not.
func migrateDir(current string) error {
	if _, err := os.Stat(current); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	legacy := filepath.Join(filepath.Dir(current), legacyAppName)
	info, err := os.Stat(legacy)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(current), 0755); err != nil {
		return err
	}

	return os.Rename(legacy, current)
}
