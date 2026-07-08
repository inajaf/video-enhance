package enhancer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Server struct {
	config   Config
	staticFS http.FileSystem
	mu       sync.Mutex
	jobs     map[string]*Job
}

func NewServer(config Config, staticFS http.FileSystem) *Server {
	if config.WorkDir == "" {
		config.WorkDir = "jobs"
	}
	if config.OutputDir == "" {
		config.OutputDir = "outputs"
	}
	return &Server{
		config:   config,
		staticFS: staticFS,
		jobs:     make(map[string]*Job),
	}
}

func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/jobs", s.handleJobs)
	mux.HandleFunc("/api/jobs/", s.handleJobPath)
	mux.Handle("/", http.FileServer(s.staticFS))
	return withSecurityHeaders(mux)
}

func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, health())
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.createJob(w, r)
	case http.MethodGet:
		s.listJobs(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) listJobs(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs := make([]JobSnapshot, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job.Snapshot())
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) createJob(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "could not parse upload")
		return
	}

	mode := normalizeMode(r.FormValue("mode"))
	preset := normalizePreset(r.FormValue("preset"))
	format := normalizeFormat(r.FormValue("format"))
	outputDir := strings.TrimSpace(r.FormValue("outputDir"))

	file, header, err := r.FormFile("video")
	if err != nil {
		writeError(w, http.StatusBadRequest, "choose a video file")
		return
	}
	defer file.Close()

	id := newID()
	workDir := filepath.Join(s.config.WorkDir, id)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create job workspace")
		return
	}

	inputName := sanitizeFilename(header.Filename)
	if inputName == "" {
		inputName = "input.mp4"
	}
	inputPath := filepath.Join(workDir, inputName)
	dst, err := os.Create(inputPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save upload")
		return
	}
	if _, err := io.Copy(dst, file); closeWith(err, dst.Close()) != nil {
		writeError(w, http.StatusInternalServerError, "could not write upload")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	now := time.Now()
	job := &Job{
		ID:           id,
		Status:       StatusQueued,
		Mode:         mode,
		Preset:       preset,
		Format:       format,
		InputName:    inputName,
		InputPath:    inputPath,
		OutputDir:    s.config.OutputDir,
		Stage:        "Queued",
		Progress:     0,
		CreatedAt:    now,
		UpdatedAt:    now,
		cancel:       cancel,
		workDir:      workDir,
		requestedDir: outputDir,
	}

	s.mu.Lock()
	s.jobs[id] = job
	s.mu.Unlock()

	go s.runJob(ctx, id)
	writeJSON(w, http.StatusAccepted, job.Snapshot())
}

func (s *Server) handleJobPath(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/jobs/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch {
	case r.Method == http.MethodGet && action == "":
		s.getJob(w, r, id)
	case r.Method == http.MethodPost && action == "cancel":
		s.cancelJob(w, r, id)
	case r.Method == http.MethodGet && action == "download":
		s.downloadJob(w, r, id)
	default:
		writeError(w, http.StatusNotFound, "route not found")
	}
}

func (s *Server) getJob(w http.ResponseWriter, _ *http.Request, id string) {
	job, ok := s.snapshot(id)
	if !ok {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) cancelJob(w http.ResponseWriter, _ *http.Request, id string) {
	s.mu.Lock()
	job, ok := s.jobs[id]
	if !ok {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	cancel := job.cancel
	if job.Status == StatusQueued || job.Status == StatusRunning {
		job.Stage = "Canceling"
		job.UpdatedAt = time.Now()
	}
	snapshot := job.Snapshot()
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) downloadJob(w http.ResponseWriter, r *http.Request, id string) {
	s.mu.Lock()
	job, ok := s.jobs[id]
	if !ok {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	outputPath := job.OutputPath
	outputName := job.OutputName
	status := job.Status
	s.mu.Unlock()

	if status != StatusSucceeded || outputPath == "" {
		writeError(w, http.StatusBadRequest, "output is not ready yet")
		return
	}
	if _, err := os.Stat(outputPath); err != nil {
		writeError(w, http.StatusNotFound, "output file is missing")
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", outputName))
	http.ServeFile(w, r, outputPath)
}

func (s *Server) snapshot(id string) (JobSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return JobSnapshot{}, false
	}
	return job.Snapshot(), true
}

func (s *Server) updateJob(id string, update func(*Job)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job, ok := s.jobs[id]; ok {
		update(job)
		job.UpdatedAt = time.Now()
	}
}

func (s *Server) appendLog(id, message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	s.updateJob(id, func(job *Job) {
		job.Logs = append(job.Logs, LogLine{Time: time.Now(), Message: message})
		if len(job.Logs) > 240 {
			job.Logs = job.Logs[len(job.Logs)-240:]
		}
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%d-%s", time.Now().Unix(), hex.EncodeToString(b[:]))
}

func closeWith(err error, closeErr error) error {
	if err != nil {
		return err
	}
	return closeErr
}

func normalizeMode(value string) string {
	switch strings.TrimSpace(value) {
	case "fast-upscale", "ai-2x", "ai-4x", "anime":
		return strings.TrimSpace(value)
	default:
		return "fast"
	}
}

func normalizePreset(value string) string {
	switch strings.TrimSpace(value) {
	case "fast", "best":
		return strings.TrimSpace(value)
	default:
		return "balanced"
	}
}

func normalizeFormat(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "mov", "mkv":
		return strings.TrimSpace(strings.ToLower(value))
	default:
		return "mp4"
	}
}

func sanitizeFilename(value string) string {
	value = filepath.Base(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, string(filepath.Separator), "_")
	value = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '.', r == '-', r == '_', r == ' ':
			return r
		default:
			return '_'
		}
	}, value)
	return strings.Trim(value, " .")
}

func isCanceled(err error) bool {
	return errors.Is(err, context.Canceled)
}
