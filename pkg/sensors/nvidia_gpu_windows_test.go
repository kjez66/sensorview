//go:build windows

package sensors

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestWindowsSMICandidatesCoverDriverLocations(t *testing.T) {
	t.Setenv("SystemRoot", `C:\Windows`)
	t.Setenv("ProgramFiles", `C:\Program Files`)

	got := nvidiaSMICandidates()

	want := []string{
		`C:\Windows\System32\nvidia-smi.exe`,
		`C:\Program Files\NVIDIA Corporation\NVSMI\nvidia-smi.exe`,
	}
	for _, path := range want {
		if !slices.Contains(got, path) {
			t.Errorf("candidates = %v, want it to include %q", got, path)
		}
	}
}

func TestWindowsSMICandidatesSkipUnsetEnvironment(t *testing.T) {
	t.Setenv("SystemRoot", "")
	t.Setenv("ProgramFiles", "")

	for _, path := range nvidiaSMICandidates() {
		if !filepath.IsAbs(path) {
			t.Errorf("candidate %q is not absolute; an unset variable must drop the candidate, not yield a relative path", path)
		}
	}
}
