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
	// frameExt is the intermediate frame extension without a leading dot (jpg or png).
	// Fast/balanced presets use high-quality JPEG to cut disk I/O; best keeps lossless PNG.
	frameExt string
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
		"-threads", "0",
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

	if err := s.extractFrames(ctx, id, tools.FFmpeg, paths, probe); err != nil {
		return err
	}

	frameCount := countFrames(paths.framesDir, paths.frameExt)
	if frameCount == 0 {
		return fmt.Errorf("no frames were extracted from the input video")
	}

	if err := s.upscaleFrames(ctx, id, tools.RealESRGAN, paths, job.preset, options, frameCount); err != nil {
		return err
	}

	if done := countFrames(paths.upscaledDir, paths.frameExt); done == 0 {
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
		frameExt:    intermediateFrameExt(job.preset),
	}

	if err := os.MkdirAll(paths.framesDir, 0o755); err != nil {
		return aiJobPaths{}, err
	}

	if err := os.MkdirAll(paths.upscaledDir, 0o755); err != nil {
		return aiJobPaths{}, err
	}

	return paths, nil
}

// intermediateFrameExt chooses AI intermediate still format by preset.
// JPEG (fast/balanced) cuts temp disk use and decode I/O vs PNG; best keeps lossless PNG.
func intermediateFrameExt(preset string) string {
	if preset == "best" {
		return "png"
	}
	return "jpg"
}

func framePattern(dir, ext string) string {
	return filepath.Join(dir, "%08d."+ext)
}

func extractFrameArgs(inputPath, framesDir, frameExt string) []string {
	args := []string{
		"-y",
		"-hide_banner",
		"-nostats",
		"-threads", "0",
		"-i", inputPath,
		"-fps_mode", "passthrough",
		"-an",
		"-sn",
		"-dn",
	}
	// High-quality JPEG stills are far smaller than PNG while remaining near-lossless at q=2.
	if frameExt == "jpg" {
		args = append(args, "-q:v", "2")
	}
	args = append(args, "-progress", "pipe:1", framePattern(framesDir, frameExt))
	return args
}

func (s *Server) extractFrames(
	ctx context.Context,
	id string,
	ffmpegPath string,
	paths aiJobPaths,
	probe videoProbe,
) error {
	s.updateJob(id, func(job *Job) {
		job.Stage = "Extracting frames"
		job.Progress = 3
	})

	return runCommand(ctx, commandHooks{
		Line: s.ffmpegProgressHook(id, probe.DurationSec, 3, 17),
	}, ffmpegPath, extractFrameArgs(paths.inputPath, paths.framesDir, paths.frameExt)...)
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

	counter := newFrameCounter(paths.upscaledDir, paths.frameExt)
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
	frameExt := paths.frameExt
	if frameExt == "" {
		frameExt = intermediateFrameExt(preset)
	}

	args := []string{
		"-i", paths.framesDir,
		"-o", paths.upscaledDir,
		"-n", options.model,
		"-s", strconv.Itoa(options.scale),
		"-f", frameExt,
	}

	if modelPath := strings.TrimSpace(os.Getenv("REALESRGAN_MODEL_PATH")); modelPath != "" {
		args = append(args, "-m", absOrOriginal(modelPath))
	}

	args = append(args, "-j", realESRGANThreads(preset, runtime.NumCPU()))

	if strings.EqualFold(strings.TrimSpace(os.Getenv("REALESRGAN_TTA")), "1") {
		args = append(args, "-x")
	}

	return args
}

// realESRGANThreads returns load:proc:save workers scaled to available CPUs.
// Fast favors throughput; best stays conservative to limit peak memory/VRAM contention.
func realESRGANThreads(preset string, numCPU int) string {
	if numCPU < 1 {
		numCPU = 1
	}

	clamp := func(v, lo, hi int) int {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}

	switch preset {
	case "fast":
		load := clamp(numCPU/2, 2, 4)
		proc := clamp(numCPU/2, 2, 4)
		save := clamp(numCPU/3, 2, 4)
		return fmt.Sprintf("%d:%d:%d", load, proc, save)
	case "best":
		return "1:2:2"
	default: // balanced
		load := clamp(numCPU/3, 1, 3)
		proc := clamp(numCPU/3, 2, 3)
		save := clamp(numCPU/4, 1, 2)
		return fmt.Sprintf("%d:%d:%d", load, proc, save)
	}
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

func rebuildVideoArgs(ffmpegPath string, paths aiJobPaths, job pipelineJob, probe videoProbe, encodeArgs []string) []string {
	frameExt := paths.frameExt
	if frameExt == "" {
		frameExt = intermediateFrameExt(job.preset)
	}

	// Even dimensions + yuv420p keep progressive H.264-compatible output after AI rebuild.
	args := []string{
		"-y",
		"-hide_banner",
		"-nostats",
		"-threads", "0",
		"-framerate", probe.FPSString,
		"-i", framePattern(paths.upscaledDir, frameExt),
		"-i", paths.inputPath,
		"-map", "0:v:0",
		"-map", "1:a?",
		"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2,format=yuv420p",
	}
	args = append(args, encodeArgs...)
	args = append(args, "-c:a", "copy", "-shortest")

	if isFastStartFormat(job.format) {
		args = append(args, "-movflags", "+faststart")
	}

	return append(args, "-progress", "pipe:1", paths.outputPath)
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

	encodeArgs := encoderArgs(ctx, ffmpegPath, job.preset)
	rebuildArgs := rebuildVideoArgs(ffmpegPath, paths, job, probe, encodeArgs)

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
	// Leaner denoise/sharpen on fast; balanced keeps a light temporal denoise;
	// best uses stronger spatial cleanup. Always force even geometry + yuv420p.
	denoise := "1.0:1.0:3:3"
	sharpen := "5:5:0.50:3:3:0.10"
	eq := "eq=contrast=1.04:saturation=1.05:gamma=1.01"
	switch preset {
	case "fast":
		// Skip heavy temporal denoise; light unsharp + color lift only.
		denoise = ""
		sharpen = "5:5:0.40:3:3:0.0"
		eq = "eq=contrast=1.03:saturation=1.04"
	case "best":
		denoise = "1.6:1.6:6:6"
		sharpen = "7:7:0.70:5:5:0.12"
		eq = "eq=contrast=1.05:saturation=1.06:gamma=1.01"
	}

	parts := make([]string, 0, 5)
	if denoise != "" {
		parts = append(parts, "hqdn3d="+denoise)
	}
	parts = append(parts, "unsharp="+sharpen, eq)
	if mode == "fast-upscale" {
		// 2x Lanczos with accurate chroma for cleaner progressive H.264 output.
		parts = append(parts, "scale=trunc(iw*2/2)*2:trunc(ih*2/2)*2:flags=lanczos+accurate_rnd+full_chroma_int")
	} else {
		parts = append(parts, "scale=trunc(iw/2)*2:trunc(ih/2)*2")
	}
	parts = append(parts, "format=yuv420p")
	return strings.Join(parts, ",")
}

func encoderArgs(ctx context.Context, ffmpegPath, preset string) []string {
	// Prefer hardware H.264 on macOS when available; otherwise libx264 with auto threads.
	if runtime.GOOS == "darwin" && encoderAvailable(ctx, ffmpegPath, "h264_videotoolbox") {
		bitrate := "14M"
		switch preset {
		case "fast":
			bitrate = "8M"
		case "best":
			bitrate = "28M"
		}
		return []string{
			"-c:v", "h264_videotoolbox",
			"-b:v", bitrate,
			"-tag:v", "avc1",
			"-pix_fmt", "yuv420p",
		}
	}

	crf := "18"
	speed := "medium"
	switch preset {
	case "fast":
		crf = "22"
		speed = "veryfast"
	case "best":
		crf = "15"
		speed = "slow"
	}
	return []string{
		"-c:v", "libx264",
		"-preset", speed,
		"-crf", crf,
		"-pix_fmt", "yuv420p",
		"-threads", "0",
	}
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

type frameCounter struct {
	dir         string
	ext         string
	minRefresh  time.Duration
	lastRefresh time.Time
	count       int
}

func newFrameCounter(dir, ext string) *frameCounter {
	return &frameCounter{
		dir:        dir,
		ext:        ext,
		minRefresh: 500 * time.Millisecond,
	}
}

// newPNGCounter is kept as a thin alias for tests that still pass a PNG-only dir.
func newPNGCounter(dir string) *frameCounter {
	return newFrameCounter(dir, "png")
}

func (c *frameCounter) Count() int {
	return c.CountAt(time.Now())
}

func (c *frameCounter) CountAt(now time.Time) int {
	if c.lastRefresh.IsZero() || now.Sub(c.lastRefresh) >= c.minRefresh {
		c.count = countFrames(c.dir, c.ext)
		c.lastRefresh = now
	}

	return c.count
}

func countPNG(dir string) int {
	return countFrames(dir, "png")
}

func countFrames(dir, ext string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	wantExt := "." + strings.TrimPrefix(strings.ToLower(ext), ".")
	if wantExt == "." {
		wantExt = ".png"
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), wantExt) {
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
