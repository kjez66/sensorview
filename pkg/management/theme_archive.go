package management

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/oae/sensorpanel/pkg/paths"
	"github.com/oae/sensorpanel/pkg/theme"
)

func (s *Server) handleThemeExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	name := r.URL.Query().Get("name")
	if !validThemeName(name) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid theme name"))
		return
	}
	loaded, err := theme.Load(name)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, name))
	archive := zip.NewWriter(w)
	err = filepath.WalkDir(loaded.Path, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(loaded.Path, path)
		if err != nil || relative == "." {
			return err
		}
		if relative == "node_modules" || strings.HasPrefix(relative, "node_modules"+string(filepath.Separator)) ||
			relative == "native.theme.draft.json" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		output, err := archive.Create(filepath.ToSlash(relative))
		if err != nil {
			_ = input.Close()
			return err
		}
		_, copyErr := io.Copy(output, input)
		closeErr := input.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	closeErr := archive.Close()
	if err != nil || closeErr != nil {
		return
	}
}

func (s *Server) handleThemeImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	name := r.URL.Query().Get("name")
	if !validThemeName(name) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid theme name"))
		return
	}
	destination, _ := paths.ThemeDir(name)
	if _, err := os.Stat(destination); err == nil {
		writeError(w, http.StatusConflict, fmt.Errorf("theme already exists"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<30)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	defer file.Close()
	temporary, err := os.CreateTemp("", "sensorpanel-theme-*.zip")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := io.Copy(temporary, file); err != nil {
		_ = temporary.Close()
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := temporary.Close(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	reader, err := zip.OpenReader(temporaryPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	defer reader.Close()
	if err := os.MkdirAll(destination, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(destination)
		}
	}()
	var extracted int64
	for _, entry := range reader.File {
		clean := filepath.Clean(entry.Name)
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			writeError(w, http.StatusBadRequest, fmt.Errorf("archive contains unsafe path"))
			return
		}
		extracted += int64(entry.UncompressedSize64)
		if extracted > 4<<30 {
			writeError(w, http.StatusRequestEntityTooLarge, fmt.Errorf("extracted theme exceeds 4 GiB"))
			return
		}
		target := filepath.Join(destination, clean)
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		input, err := entry.Open()
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		mode := entry.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
		if err != nil {
			_ = input.Close()
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		_, copyErr := io.Copy(output, input)
		_ = input.Close()
		closeErr := output.Close()
		if copyErr != nil || closeErr != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("extract theme: %v %v", copyErr, closeErr))
			return
		}
	}
	if err := renameClonedTheme(destination, name); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	loaded, err := theme.Load(name)
	if err != nil || !loaded.HasNative {
		writeError(w, http.StatusBadRequest, fmt.Errorf("archive does not contain a native theme"))
		return
	}
	success = true
	writeJSON(w, http.StatusCreated, loaded)
}
