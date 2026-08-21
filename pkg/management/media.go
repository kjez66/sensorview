package management

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// ProcessRequest describes a crop/trim preprocessing job. Crop values are
// normalized display-oriented coordinates between zero and one.
type ProcessRequest struct {
	Theme   string  `json:"theme"`
	Source  string  `json:"source"`
	Kind    string  `json:"kind"` // image or video
	CropX   float64 `json:"crop_x"`
	CropY   float64 `json:"crop_y"`
	CropW   float64 `json:"crop_w"`
	CropH   float64 `json:"crop_h"`
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Width   int     `json:"width"`
	Height  int     `json:"height"`
	FPS     float64 `json:"fps"`
	Quality int     `json:"quality"`
}

type mediaJob struct {
	mu         sync.RWMutex
	ID         string  `json:"id"`
	Status     string  `json:"status"`
	Progress   float64 `json:"progress"`
	Output     string  `json:"output,omitempty"`
	Error      string  `json:"error,omitempty"`
	cancelFunc context.CancelFunc
}

func (j *mediaJob) snapshot() map[string]any {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return map[string]any{
		"id": j.ID, "status": j.Status, "progress": j.Progress,
		"output": j.Output, "error": j.Error,
	}
}

func (j *mediaJob) set(status string, progress float64, output string, err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Status, j.Progress, j.Output = status, progress, output
	if err != nil {
		j.Error = err.Error()
	}
}

func (j *mediaJob) cancelJob() {
	j.mu.RLock()
	cancel := j.cancelFunc
	j.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
}

// cancel is exposed through the HTTP DELETE handler.
func (j *mediaJob) cancel() {
	j.cancelJob()
}

type jobManager struct {
	mu     sync.RWMutex
	jobs   map[string]*mediaJob
	active *mediaJob
	ctx    context.Context
	cancel context.CancelFunc
}

func newJobManager() *jobManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &jobManager{jobs: make(map[string]*mediaJob), ctx: ctx, cancel: cancel}
}

func (m *jobManager) close() {
	m.cancel()
	m.mu.RLock()
	active := m.active
	m.mu.RUnlock()
	if active != nil {
		active.cancelJob()
	}
}

func (m *jobManager) start(themeRoot string, request ProcessRequest) (*mediaJob, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, fmt.Errorf("ffmpeg is required")
	}
	if request.Kind != "image" && request.Kind != "video" {
		return nil, fmt.Errorf("kind must be image or video")
	}
	if request.Width <= 0 || request.Height <= 0 {
		return nil, fmt.Errorf("positive output dimensions are required")
	}
	if request.Width > 8192 || request.Height > 8192 || int64(request.Width)*int64(request.Height) > 100_000_000 {
		return nil, fmt.Errorf("output dimensions exceed the safety limit")
	}
	if request.CropW <= 0 || request.CropH <= 0 ||
		request.CropX < 0 || request.CropY < 0 ||
		request.CropX+request.CropW > 1.000001 || request.CropY+request.CropH > 1.000001 {
		return nil, fmt.Errorf("invalid normalized crop rectangle")
	}
	if request.FPS <= 0 {
		request.FPS = 24
	}
	if request.FPS > 60 {
		return nil, fmt.Errorf("FPS must not exceed 60")
	}
	if request.Kind == "video" && (request.End <= request.Start || request.End-request.Start > 600) {
		return nil, fmt.Errorf("video trim must be between 0 and 600 seconds")
	}
	if request.Quality <= 0 {
		request.Quality = 4
	}
	if request.Quality < 2 || request.Quality > 31 {
		return nil, fmt.Errorf("JPEG quality must be between 2 and 31")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != nil {
		status := m.active.snapshot()["status"]
		if status == "queued" || status == "running" {
			return nil, fmt.Errorf("another media job is running")
		}
	}
	job := &mediaJob{ID: randomID(), Status: "queued"}
	m.jobs[job.ID] = job
	m.active = job
	go m.run(themeRoot, request, job)
	return job, nil
}

func (m *jobManager) run(themeRoot string, request ProcessRequest, job *mediaJob) {
	ctx, cancel := context.WithCancel(m.ctx)
	job.mu.Lock()
	job.cancelFunc = cancel
	job.mu.Unlock()
	defer cancel()

	source, err := safeThemePath(themeRoot, request.Source)
	if err != nil {
		job.set("failed", 0, "", err)
		return
	}
	generatedRoot := filepath.Join(themeRoot, "assets", "generated")
	if err := os.MkdirAll(generatedRoot, 0o755); err != nil {
		job.set("failed", 0, "", err)
		return
	}
	temporary := filepath.Join(generatedRoot, "."+job.ID+".tmp")
	final := filepath.Join(generatedRoot, job.ID)
	_ = os.RemoveAll(temporary)
	if err := os.MkdirAll(temporary, 0o755); err != nil {
		job.set("failed", 0, "", err)
		return
	}
	defer os.RemoveAll(temporary)

	filter := fmt.Sprintf(
		"crop=iw*%.8f:ih*%.8f:iw*%.8f:ih*%.8f,scale=%d:%d:flags=lanczos",
		request.CropW, request.CropH, request.CropX, request.CropY, request.Width, request.Height,
	)
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	if request.Start > 0 {
		args = append(args, "-ss", strconv.FormatFloat(request.Start, 'f', 3, 64))
	}
	args = append(args, "-i", source)
	if request.End > request.Start {
		args = append(args, "-t", strconv.FormatFloat(request.End-request.Start, 'f', 3, 64))
	}
	if request.Kind == "video" {
		filter += ",fps=" + strconv.FormatFloat(request.FPS, 'f', -1, 64)
		args = append(args, "-vf", filter, "-an", "-threads", "2", "-q:v", strconv.Itoa(request.Quality),
			"-progress", "pipe:1", filepath.Join(temporary, "frame_%04d.jpg"))
	} else {
		args = append(args, "-vf", filter, "-frames:v", "1", "-q:v", strconv.Itoa(request.Quality),
			filepath.Join(temporary, "frame_0001.jpg"))
	}
	command := exec.CommandContext(ctx, "ffmpeg", args...)
	configureMediaCommand(command)
	stdout, err := command.StdoutPipe()
	if err != nil {
		job.set("failed", 0, "", err)
		return
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	job.set("running", 0, "", nil)
	if err := command.Start(); err != nil {
		job.set("failed", 0, "", err)
		return
	}
	if command.Process != nil {
		lowerMediaPriority(command.Process)
	}
	if request.Kind == "video" {
		duration := request.End - request.Start
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			key, value, ok := strings.Cut(scanner.Text(), "=")
			if ok && key == "out_time_us" && duration > 0 {
				microseconds, _ := strconv.ParseFloat(value, 64)
				job.set("running", min(0.99, microseconds/(duration*1e6)), "", nil)
			}
		}
	}
	if err := command.Wait(); err != nil {
		if ctx.Err() != nil {
			job.set("cancelled", 0, "", ctx.Err())
		} else {
			message := strings.TrimSpace(stderr.String())
			if message != "" {
				err = fmt.Errorf("%w: %s", err, message)
			}
			job.set("failed", 0, "", err)
		}
		return
	}
	if err := os.Rename(temporary, final); err != nil {
		job.set("failed", 0, "", err)
		return
	}
	relative, _ := filepath.Rel(themeRoot, final)
	job.set("complete", 1, filepath.ToSlash(relative), nil)
}

func (m *jobManager) get(id string) *mediaJob {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.jobs[id]
}

func (m *jobManager) snapshots() []map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]map[string]any, 0, len(m.jobs))
	for _, job := range m.jobs {
		result = append(result, job.snapshot())
	}
	return result
}
