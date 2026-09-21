package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateDirMovesLegacyDirectory(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, legacyAppName)
	current := filepath.Join(root, appName)

	if err := os.MkdirAll(filepath.Join(legacy, "themes", "trofeo"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := migrateDir(current); err != nil {
		t.Fatalf("migrateDir() = %v, want nil", err)
	}

	if _, err := os.Stat(filepath.Join(current, "themes", "trofeo")); err != nil {
		t.Errorf("migrated contents missing: %v", err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("legacy directory still present, err = %v", err)
	}
}

func TestMigrateDirKeepsExistingDirectory(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, legacyAppName)
	current := filepath.Join(root, appName)

	if err := os.MkdirAll(filepath.Join(legacy, "old"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(current, "new"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := migrateDir(current); err != nil {
		t.Fatalf("migrateDir() = %v, want nil", err)
	}

	if _, err := os.Stat(filepath.Join(current, "new")); err != nil {
		t.Errorf("existing directory was disturbed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(current, "old")); !os.IsNotExist(err) {
		t.Errorf("legacy contents were merged into an existing directory")
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Errorf("legacy directory should be left in place: %v", err)
	}
}

func TestMigrateDirWithoutLegacyDirectory(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, appName)

	if err := migrateDir(current); err != nil {
		t.Fatalf("migrateDir() = %v, want nil", err)
	}

	if _, err := os.Stat(current); !os.IsNotExist(err) {
		t.Errorf("migrateDir created a directory when there was nothing to migrate")
	}
}

func TestMigrateLegacyDirsIsIdempotent(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("XDG_DATA_HOME", root)
	t.Setenv("XDG_CACHE_HOME", root)
	t.Setenv("APPDATA", root)
	t.Setenv("LOCALAPPDATA", root)

	legacy := filepath.Join(root, legacyAppName)
	if err := os.MkdirAll(filepath.Join(legacy, "themes"), 0755); err != nil {
		t.Fatal(err)
	}

	for i := range 2 {
		if err := MigrateLegacyDirs(); err != nil {
			t.Fatalf("MigrateLegacyDirs() run %d = %v, want nil", i+1, err)
		}
	}

	if _, err := os.Stat(filepath.Join(root, appName, "themes")); err != nil {
		t.Errorf("themes not migrated: %v", err)
	}
}
