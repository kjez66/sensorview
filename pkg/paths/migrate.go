package paths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// legacyAppName is the directory name used before the project was renamed from
// SensorPanel to SensorView. Installations that predate the rename keep their
// config, themes and browser cache under it.
const legacyAppName = "sensorpanel"

// MigrateLegacyDirs merges pre-rename sensorpanel directories into their
// sensorview equivalents.
//
// The merge is entry by entry rather than a single rename of the top-level
// directory, because the new directory is easily created before a migration
// ever runs - the test suite writes to the real cache directory, for one - and
// a plain rename would then be skipped forever, stranding the old data.
//
// Nothing is ever overwritten: when both sides have the same path, a directory
// is merged recursively and anything else is left as it is on the new side. The
// legacy directory is removed once it is empty, so a second run is a no-op.
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

// migrateDir merges the legacy sibling of current into current.
func migrateDir(current string) error {
	legacy := filepath.Join(filepath.Dir(current), legacyAppName)

	// Guard against a legacy path that resolves to the directory itself, which
	// would happen if appName were ever set to legacyAppName.
	if legacy == current {
		return nil
	}

	info, err := os.Stat(legacy)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}

	if err := mergeTree(legacy, current); err != nil {
		return err
	}

	// Only succeeds once everything has moved across.
	if err := os.Remove(legacy); err != nil && !errors.Is(err, os.ErrNotExist) {
		var pathErr *os.PathError
		if !errors.As(err, &pathErr) {
			return err
		}
	}

	return nil
}

// mergeTree moves every entry under src into dst without overwriting. Entries
// that exist on both sides are merged when both are directories and skipped
// otherwise, leaving the dst copy in place.
func mergeTree(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		dstInfo, err := os.Stat(dstPath)
		switch {
		case errors.Is(err, os.ErrNotExist):
			if err := os.Rename(srcPath, dstPath); err != nil {
				return err
			}
		case err != nil:
			return err
		case dstInfo.IsDir() && entry.IsDir():
			if err := mergeTree(srcPath, dstPath); err != nil {
				return err
			}
			// Drop the source directory once its contents have moved.
			if err := os.Remove(srcPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				var pathErr *os.PathError
				if !errors.As(err, &pathErr) {
					return err
				}
			}
		default:
			// Same name on both sides and not two directories: keep the new one.
		}
	}

	return nil
}
