package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestMigrateDirMovesLegacyTree(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, legacyAppName)
	current := filepath.Join(root, appName)

	writeFile(t, filepath.Join(legacy, "config.json"), "cfg")
	writeFile(t, filepath.Join(legacy, "themes", "trofeo", "index.html"), "theme")

	if err := migrateDir(current); err != nil {
		t.Fatalf("migrateDir() = %v, want nil", err)
	}

	if got := readFile(t, filepath.Join(current, "config.json")); got != "cfg" {
		t.Errorf("config.json = %q, want %q", got, "cfg")
	}
	if got := readFile(t, filepath.Join(current, "themes", "trofeo", "index.html")); got != "theme" {
		t.Errorf("theme file = %q, want %q", got, "theme")
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("legacy directory should be gone, err = %v", err)
	}
}

// The case that the first implementation got wrong: the test suite writes to
// the real cache directory, so the new directory can exist - holding nothing but
// a stray cache - before any migration runs. The old config and themes must
// still come across.
func TestMigrateDirMergesIntoPartiallyCreatedDir(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, legacyAppName)
	current := filepath.Join(root, appName)

	writeFile(t, filepath.Join(legacy, "config.json"), "cfg")
	writeFile(t, filepath.Join(legacy, "themes", "trofeo", "index.html"), "theme")
	writeFile(t, filepath.Join(legacy, "cache", "lyrics", "a.json"), "old-lyric")
	writeFile(t, filepath.Join(current, "cache", "lyrics", "b.json"), "new-lyric")

	if err := migrateDir(current); err != nil {
		t.Fatalf("migrateDir() = %v, want nil", err)
	}

	if got := readFile(t, filepath.Join(current, "config.json")); got != "cfg" {
		t.Errorf("config.json = %q, want %q", got, "cfg")
	}
	if got := readFile(t, filepath.Join(current, "themes", "trofeo", "index.html")); got != "theme" {
		t.Errorf("theme file = %q, want %q", got, "theme")
	}
	if got := readFile(t, filepath.Join(current, "cache", "lyrics", "a.json")); got != "old-lyric" {
		t.Errorf("legacy lyric = %q, want %q", got, "old-lyric")
	}
	if got := readFile(t, filepath.Join(current, "cache", "lyrics", "b.json")); got != "new-lyric" {
		t.Errorf("existing lyric = %q, want %q", got, "new-lyric")
	}
}

func TestMigrateDirNeverOverwrites(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, legacyAppName)
	current := filepath.Join(root, appName)

	writeFile(t, filepath.Join(legacy, "config.json"), "old")
	writeFile(t, filepath.Join(current, "config.json"), "new")

	if err := migrateDir(current); err != nil {
		t.Fatalf("migrateDir() = %v, want nil", err)
	}

	if got := readFile(t, filepath.Join(current, "config.json")); got != "new" {
		t.Errorf("config.json = %q, want the newer %q to survive", got, "new")
	}
	// The conflicting file stays behind rather than being deleted.
	if got := readFile(t, filepath.Join(legacy, "config.json")); got != "old" {
		t.Errorf("legacy config.json = %q, want it left in place", got)
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
	writeFile(t, filepath.Join(legacy, "themes", "trofeo", "index.html"), "theme")

	for i := range 2 {
		if err := MigrateLegacyDirs(); err != nil {
			t.Fatalf("MigrateLegacyDirs() run %d = %v, want nil", i+1, err)
		}
	}

	if got := readFile(t, filepath.Join(root, appName, "themes", "trofeo", "index.html")); got != "theme" {
		t.Errorf("theme file = %q, want %q", got, "theme")
	}
}
