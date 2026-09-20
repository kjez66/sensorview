package theme

import (
	"slices"

	"testing"
)

func TestViteArgsBindEveryInterface(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pm   PackageManager
		want []string
	}{
		{name: "npm", pm: NPM, want: []string{"npm", "run", "dev", "--", "--port", "15173", "--host"}},
		{name: "yarn", pm: Yarn, want: []string{"yarn", "dev", "--", "--port", "15173", "--host"}},
		{name: "pnpm", pm: PNPM, want: []string{"pnpm", "run", "dev", "--", "--port", "15173", "--host"}},
		{name: "bun", pm: Bun, want: []string{"bun", "run", "dev", "--", "--port", "15173", "--host"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := viteArgs(tt.pm, 15173)
			if !slices.Equal(got, tt.want) {
				t.Errorf("viteArgs(%v, 15173) = %v, want %v", tt.pm, got, tt.want)
			}
		})
	}
}

func TestViteArgsPassHostAfterTheSeparator(t *testing.T) {
	t.Parallel()

	// Anything before -- is consumed by the package manager rather than Vite,
	// so --host has to follow it to reach the dev server.
	args := viteArgs(NPM, 15173)
	separator := slices.Index(args, "--")

	if separator < 0 {
		t.Fatalf("args = %v, want a -- separator", args)
	}
	if !slices.Contains(args[separator:], "--host") {
		t.Errorf("args = %v, want --host after the separator", args)
	}
}
