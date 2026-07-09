package enhancer

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const ffmpegProgressTimebase = 1_000_000

type pipelineJob struct {
	inputPath string
	mode      string
	preset    string
	format    string
	workDir   string
}

type aiOptions struct {
	scale int
	model string
}

type aiJobPaths struct {
	inputPath   string
	outputPath  string
	framesDir   string
	upscaledDir string
}

func (s *Server) runJob(ctx context.Context, id string) {
	s.updateJob(id, func(job *Job) {
		job.Status = StatusRunning
		job.Stage = "Preparing"
		job.Progress = 1
	})

	err := s.executeJob(ctx, id)
	if err != nil {
		status := StatusFailed
		stage := "Failed"
		if isCanceled(err) {
			status = StatusCanceled
			stage = "Canceled"
		}
		s.updateJob(id, func(job *Job) {
			job.Status = status
			job.Stage = stage
			job.Error = err.Error()
			if status == StatusCanceled {
				job.Error = ""
			}
		})
		s.appendLog(id, err.Error())
	} else {
		s.updateJob(id, func(job *Job) {
			job.Status = StatusSucceeded
			job.Stage = "Complete"
			job.Progress = 100
		})
	}

	s.mu.Lock()
	workDir := ""
	if job, ok := s.jobs[id]; ok {
		workDir = job.workDir
	}
	s.mu.Unlock()
	if workDir != "" {
		_ = os.RemoveAll(workDir)
	}
}

func (s *Server) executeJob(ctx context.Context, id string) error {
	if isAIMode(s.pipelineJob(id).mode) {
		return s.runAI(ctx, id)
	}
	return s.runFast(ctx, id)
}

func (s *Server) pipelineJob(id string) pipelineJob {
	s.mu.Lock()
	defer s.mu.Unlock()

	job := s.jobs[id]
	return pipelineJob{
		inputPath: job.InputPath,
		mode:      job.Mode,
		preset:    job.Preset,
		format:    job.Format,
		workDir:   job.workDir,
	}
}

func isAIMode(mode string) bool {
	return strings.HasPrefix(mode, "ai-") || mode == "anime"
}

// outputNameToken is the only abbreviation appended to the sanitized source base name.
const outputNameToken = "upscale"

// buildOutputFilename keeps the input video's sanitized base name and appends only
// the fixed "upscale" abbreviation (plus an optional short collision id).
// Mode and preset never appear in the filename.
func buildOutputFilename(inputName, format, collisionID string) string {
	base := strings.TrimSuffix(sanitizeFilename(inputName), filepath.Ext(inputName))
	if base == "" {
		base = "enhanced"
	}
	format = normalizeFormat(format)
	if collisionID != "" {
		return fmt.Sprintf("%s-%s-%s.%s", base, outputNameToken, collisionID, format)
	}
	return fmt.Sprintf("%s-%s.%s", base, outputNameToken, format)
}

func (s *Server) prepareOutput(id string) (string, string, error) {
	s.mu.Lock()
	job := s.jobs[id]
	inputName := job.InputName
	requestedDir := job.requestedDir
	format := job.Format
	s.mu.Unlock()

	outputDir, err := resolveOutputDir(s.config.OutputDir, requestedDir)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", "", err
	}

	name := buildOutputFilename(inputName, format, "")
	outputPath := filepath.Join(outputDir, name)
	if _, err := os.Stat(outputPath); err == nil {
		name = buildOutputFilename(inputName, format, idSuffix(id))
		outputPath = filepath.Join(outputDir, name)
	}

	s.updateJob(id, func(job *Job) {
		job.OutputDir = outputDir
		job.OutputName = name
		job.OutputPath = outputPath
	})
	return outputPath, name, nil
}

func (s *Server) runFast(ctx context.Context, id string) error {
	tools := detectTools()
	if tools.FFmpeg == "" {
		return fmt.Errorf("ffmpeg is required. Install with: brew install ffmpeg")
	}

	job := s.pipelineJob(id)
	outputPath, _, err := s.prepareOutput(id)
	if err != nil {
		return err
	}

	duration := probeDuration(ctx, tools.FFprobe, job.inputPath)
	args := fastCommandArgs(ctx, tools.FFmpeg, job, outputPath)

	s.updateJob(id, func(job *Job) {
		job.Stage = "Enhancing with FFmpeg"
		job.Progress = 5
	})

	return runCommand(ctx, commandHooks{
		Line: s.ffmpegProgressHook(id, duration, 5, 91),
	}, tools.FFmpeg, args...)
}

func probeDuration(ctx context.Context, ffprobePath, inputPath string) float64 {
	if ffprobePath == "" {
		return 0
	}

	probe, err := probeVideo(ctx, ffprobePath, inputPath)
	if err != nil {
		return 0
	}

	return probe.DurationSec
}

func fastCommandArgs(ctx context.Context, ffmpegPath string, job pipelineJob, outputPath string) []string {
	args := []string{
		"-y",
		"-hide_banner",
		"-nostats",
		"-i", job.inputPath,
		"-map", "0:v:0",
		"-map", "0:a?",
		"-vf", fastFilter(job.mode, job.preset),
	}

	args = append(args, encoderArgs(ctx, ffmpegPath, job.preset)...)
	args = append(args, "-c:a", "copy")

	if isFastStartFormat(job.format) {
		args = append(args, "-movflags", "+faststart")
	}

	return append(args, "-progress", "pipe:1", outputPath)
}

func (s *Server) runAI(ctx context.Context, id string) error {
	tools := detectTools()
	if err := validateAITools(tools); err != nil {
		return err
	}

	job := s.pipelineJob(id)
	options := aiOptionsForMode(job.mode)

	paths, err := s.prepareAIPaths(id, job)
	if err != nil {
		return err
	}

	probe, err := probeVideo(ctx, tools.FFprobe, paths.inputPath)
	if err != nil {
		return fmt.Errorf("could not inspect input video: %w", err)
	}

	if err := s.extractFrames(ctx, id, tools.FFmpeg, paths.framesDir, paths.inputPath, probe); err != nil {
		return err
	}

	frameCount := countPNG(paths.framesDir)
	if frameCount == 0 {
		return fmt.Errorf("no frames were extracted from the input video")
	}

	if err := s.upscaleFrames(ctx, id, tools.RealESRGAN, paths, job.preset, options, frameCount); err != nil {
		return err
	}

	if done := countPNG(paths.upscaledDir); done == 0 {
		return fmt.Errorf("AI upscaler produced no output frames")
	}

	return s.rebuildVideo(ctx, id, tools.FFmpeg, paths, job, probe)
}

func validateAITools(tools tools) error {
	if tools.FFmpeg == "" {
		return fmt.Errorf("ffmpeg is required. Install with: brew install ffmpeg")
	}

	if tools.FFprobe == "" {
		return fmt.Errorf("ffprobe is required for AI upscaling. It is usually installed with ffmpeg")
	}

	if tools.RealESRGAN == "" {
		return fmt.Errorf("realesrgan-ncnn-vulkan is required for AI upscaling. Run scripts/install-tools-macos.sh or set REALESRGAN_BIN")
	}

	return nil
}

func aiOptionsForMode(mode string) aiOptions {
	switch mode {
	case "ai-4x":
		return aiOptions{scale: 4, model: "realesrgan-x4plus"}
	case "anime":
		return aiOptions{scale: 2, model: "realesr-animevideov3"}
	default:
		return aiOptions{scale: 2, model: "realesrgan-x4plus"}
	}
}

func (s *Server) prepareAIPaths(id string, job pipelineJob) (aiJobPaths, error) {
	outputPath, _, err := s.prepareOutput(id)
	if err != nil {
		return aiJobPaths{}, err
	}

	paths := aiJobPaths{
		inputPath:   absOrOriginal(job.inputPath),
		outputPath:  absOrOriginal(outputPath),
		framesDir:   absOrOriginal(filepath.Join(job.workDir, "frames")),
		upscaledDir: absOrOriginal(filepath.Join(job.workDir, "upscaled")),
	}

	if err := os.MkdirAll(paths.framesDir, 0o755); err != nil {
		return aiJobPaths{}, err
	}

	if err := os.MkdirAll(paths.upscaledDir, 0o755); err != nil {
		return aiJobPaths{}, err
	}

	return paths, nil
}

func (s *Server) extractFrames(
	ctx context.Context,
	id string,
	ffmpegPath string,
	framesDir string,
	inputPath string,
	probe videoProbe,
) error {
	s.updateJob(id, func(job *Job) {
		job.Stage = "Extracting frames"
		job.Progress = 3
	})
	extractArgs := []string{
		"-y",
		"-hide_banner",
		"-nostats",
		"-i", inputPath,
		"-fps_mode", "passthrough",
		"-progress", "pipe:1",
		filepath.Join(framesDir, "%08d.png"),
	}

	return runCommand(ctx, commandHooks{
		Line: s.ffmpegProgressHook(id, probe.DurationSec, 3, 17),
	}, ffmpegPath, extractArgs...)
}

func (s *Server) upscaleFrames(
	ctx context.Context,
	id string,
	realESRGANPath string,
	paths aiJobPaths,
	preset string,
	options aiOptions,
	frameCount int,
) error {
	s.updateJob(id, func(job *Job) {
		job.Stage = fmt.Sprintf("AI upscaling %d frames", frameCount)
		job.Progress = 20
	})

	binPath := absOrOriginal(realESRGANPath)
	aiCmd := exec.CommandContext(ctx, binPath, realESRGANArgs(paths, preset, options)...)
	aiCmd.Dir = filepath.Dir(binPath)

	counter := newPNGCounter(paths.upscaledDir)
	return runPreparedCommand(ctx, commandHooks{
		Line: func(line string) {
			s.appendLog(id, line)
			if percent, ok := parsePercentLine(line); ok {
				done := counter.Count()
				s.updateAIProgress(id, done, frameCount, percent)
			}
		},
		Tick: func() {
			done := counter.Count()
			s.updateAIProgress(id, done, frameCount, 0)
		},
	}, aiCmd)
}

func realESRGANArgs(paths aiJobPaths, preset string, options aiOptions) []string {
	args := []string{
		"-i", paths.framesDir,
		"-o", paths.upscaledDir,
		"-n", options.model,
		"-s", strconv.Itoa(options.scale),
		"-f", "png",
	}

	if modelPath := strings.TrimSpace(os.Getenv("REALESRGAN_MODEL_PATH")); modelPath != "" {
		args = append(args, "-m", absOrOriginal(modelPath))
	}

	args = append(args, "-j", realESRGANThreads(preset))

	if strings.EqualFold(strings.TrimSpace(os.Getenv("REALESRGAN_TTA")), "1") {
		args = append(args, "-x")
	}

	return args
}

func realESRGANThreads(preset string) string {
	if preset == "fast" {
		return "2:2:2"
	}

	return "1:2:2"
}

func (s *Server) updateAIProgress(id string, done int, frameCount int, currentPercent float64) {
	progress := aiProgress(done, frameCount, currentPercent)
	stage := fmt.Sprintf("AI upscaling frames %d/%d", done, frameCount)

	if currentPercent > 0 && currentPercent < 100 {
		stage = fmt.Sprintf("%s (current %.0f%%)", stage, currentPercent)
	}

	s.updateJob(id, func(job *Job) {
		job.Progress = progress
		job.Stage = stage
	})
}

func aiProgress(done int, frameCount int, currentPercent float64) float64 {
	if frameCount <= 0 {
		return 20
	}

	completedFrames := float64(done) + currentPercent/100
	return 20 + math.Min(65, (completedFrames/float64(frameCount))*65)
}

func (s *Server) rebuildVideo(
	ctx context.Context,
	id string,
	ffmpegPath string,
	paths aiJobPaths,
	job pipelineJob,
	probe videoProbe,
) error {
	s.updateJob(id, func(job *Job) {
		job.Stage = "Rebuilding video"
		job.Progress = 86
	})
	rebuildArgs := []string{
		"-y",
		"-hide_banner",
		"-nostats",
		"-framerate", probe.FPSString,
		"-i", filepath.Join(paths.upscaledDir, "%08d.png"),
		"-i", paths.inputPath,
		"-map", "0:v:0",
		"-map", "1:a?",
	}
	rebuildArgs = append(rebuildArgs, encoderArgs(ctx, ffmpegPath, job.preset)...)
	rebuildArgs = append(rebuildArgs, "-c:a", "copy", "-shortest")

	if isFastStartFormat(job.format) {
		rebuildArgs = append(rebuildArgs, "-movflags", "+faststart")
	}

	rebuildArgs = append(rebuildArgs, "-progress", "pipe:1", paths.outputPath)

	return runCommand(ctx, commandHooks{
		Line: s.ffmpegProgressHook(id, probe.DurationSec, 86, 12),
	}, ffmpegPath, rebuildArgs...)
}

func (s *Server) ffmpegProgressHook(id string, durationSec float64, start float64, span float64) func(string) {
	return func(line string) {
		s.appendLog(id, line)

		progress, ok := ffmpegProgress(line, durationSec, start, span)
		if !ok {
			return
		}

		s.updateJob(id, func(job *Job) {
			job.Progress = progress
		})
	}
}

func ffmpegProgress(line string, durationSec float64, start float64, span float64) (float64, bool) {
	if durationSec <= 0 || !strings.HasPrefix(line, "out_time_ms=") {
		return 0, false
	}

	outTimeMS := parseFloat(strings.TrimPrefix(line, "out_time_ms="))
	if outTimeMS <= 0 {
		return start, true
	}

	completion := outTimeMS / (durationSec * ffmpegProgressTimebase)
	progress := start + math.Min(span, math.Max(0, completion*span))
	return progress, true
}

func isFastStartFormat(format string) bool {
	return format == "mp4" || format == "mov"
}

func fastFilter(mode, preset string) string {
	denoise := "1.2:1.2:4:4"
	sharpen := "5:5:0.45:3:3:0.10"
	switch preset {
	case "fast":
		denoise = "0.8:0.8:3:3"
		sharpen = "5:5:0.35:3:3:0.0"
	case "best":
		denoise = "1.8:1.8:7:7"
		sharpen = "7:7:0.75:5:5:0.15"
	}

	parts := []string{
		"hqdn3d=" + denoise,
		"unsharp=" + sharpen,
		"eq=contrast=1.03:saturation=1.04",
	}
	if mode == "fast-upscale" {
		parts = append(parts, "scale=trunc(iw*2/2)*2:trunc(ih*2/2)*2:flags=lanczos")
	} else {
		parts = append(parts, "scale=trunc(iw/2)*2:trunc(ih/2)*2")
	}
	parts = append(parts, "format=yuv420p")
	return strings.Join(parts, ",")
}

func encoderArgs(ctx context.Context, ffmpegPath, preset string) []string {
	if runtime.GOOS == "darwin" && encoderAvailable(ctx, ffmpegPath, "h264_videotoolbox") {
		bitrate := "12M"
		switch preset {
		case "fast":
			bitrate = "8M"
		case "best":
			bitrate = "24M"
		}
		return []string{"-c:v", "h264_videotoolbox", "-b:v", bitrate, "-tag:v", "avc1"}
	}

	crf := "19"
	speed := "medium"
	switch preset {
	case "fast":
		crf = "23"
		speed = "veryfast"
	case "best":
		crf = "16"
		speed = "slow"
	}
	return []string{"-c:v", "libx264", "-preset", speed, "-crf", crf, "-pix_fmt", "yuv420p"}
}

var encoderAvailabilityCache sync.Map

func encoderAvailable(ctx context.Context, ffmpegPath, encoder string) bool {
	if ffmpegPath == "" {
		return false
	}

	key := ffmpegPath + "\x00" + encoder
	if cached, ok := encoderAvailabilityCache.Load(key); ok {
		return cached.(bool)
	}

	available := probeEncoderAvailable(ctx, ffmpegPath, encoder)
	encoderAvailabilityCache.Store(key, available)
	return available
}

func probeEncoderAvailable(ctx context.Context, ffmpegPath, encoder string) bool {
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(checkCtx, ffmpegPath, "-hide_banner", "-encoders").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), encoder)
}

type pngCounter struct {
	dir         string
	minRefresh  time.Duration
	lastRefresh time.Time
	count       int
}

func newPNGCounter(dir string) *pngCounter {
	return &pngCounter{
		dir:        dir,
		minRefresh: 500 * time.Millisecond,
	}
}

func (c *pngCounter) Count() int {
	return c.CountAt(time.Now())
}

func (c *pngCounter) CountAt(now time.Time) int {
	if c.lastRefresh.IsZero() || now.Sub(c.lastRefresh) >= c.minRefresh {
		c.count = countPNG(c.dir)
		c.lastRefresh = now
	}

	return c.count
}

func countPNG(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".png") {
			count++
		}
	}
	return count
}

func parsePercentLine(line string) (float64, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasSuffix(line, "%") {
		return 0, false
	}
	value := strings.TrimSuffix(line, "%")
	percent, err := strconv.ParseFloat(value, 64)
	if err != nil || percent < 0 || percent > 100 {
		return 0, false
	}
	return percent, true
}

func resolveOutputDir(defaultDir, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		requested = defaultDir
	}
	if strings.HasPrefix(requested, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		requested = filepath.Join(home, strings.TrimPrefix(requested, "~/"))
	}
	return filepath.Abs(requested)
}

func idSuffix(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[len(id)-8:]
}
