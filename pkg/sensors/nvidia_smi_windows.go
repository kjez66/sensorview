//go:build windows

package sensors

import (
	"os"
	"path/filepath"
	"slices"
)

// nvidiaSMICandidates returns the usual nvidia-smi locations on Windows.
// Current drivers install it into the system directory; older packages only
// placed it beside the rest of NVSMI under Program Files. Paths are derived
// from the environment so a relocated Windows directory still resolves, and an
// unset variable drops the candidate rather than yielding a relative path.
func nvidiaSMICandidates() []string {
	var paths []string

	if systemRoot := os.Getenv("SystemRoot"); systemRoot != "" {
		paths = append(paths, filepath.Join(systemRoot, "System32", "nvidia-smi.exe"))
	}

	// ProgramW6432 resolves to the 64-bit directory even from a 32-bit
	// process, and matches ProgramFiles otherwise.
	for _, variable := range []string{"ProgramFiles", "ProgramW6432"} {
		programFiles := os.Getenv(variable)
		if programFiles == "" {
			continue
		}
		path := filepath.Join(programFiles, "NVIDIA Corporation", "NVSMI", "nvidia-smi.exe")
		if !slices.Contains(paths, path) {
			paths = append(paths, path)
		}
	}

	return paths
}
