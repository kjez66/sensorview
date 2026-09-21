package management

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestImageCropJobProducesExactDimensions(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	root := t.TempDir()
	source := filepath.Join(root, "source.png")
	if output, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi",
		"-i", "color=c=blue:s=320x180", "-frames:v", "1", source).CombinedOutput(); err != nil {
		t.Fatalf("create fixture: %v: %s", err, output)
	}
	manager := newJobManager()
	defer manager.close()
	job, err := manager.start(root, ProcessRequest{
		Theme: "test", Source: "source.png", Kind: "image",
		CropX: 0.25, CropY: 0, CropW: 0.5, CropH: 1,
		Width: 64, Height: 128, Quality: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		state := job.snapshot()
		if state["status"] == "complete" {
			outputDir := filepath.Join(root, state["output"].(string))
			probe, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "stream=width,height",
				"-of", "csv=p=0:s=x", filepath.Join(outputDir, "frame_0001.jpg")).Output()
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(string(probe)) != "64x128" {
				t.Fatalf("dimensions = %q", probe)
			}
			return
		}
		if state["status"] == "failed" {
			t.Fatalf("job failed: %v", state["error"])
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("job timed out")
}
