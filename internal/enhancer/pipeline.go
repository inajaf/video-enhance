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
	"time"
)

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
	s.mu.Lock()
	job := s.jobs[id]
	mode := job.Mode
	s.mu.Unlock()

	if strings.HasPrefix(mode, "ai-") || mode == "anime" {
		return s.runAI(ctx, id)
	}
	return s.runFast(ctx, id)
}

func (s *Server) prepareOutput(id, suffix string) (string, string, error) {
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

	base := strings.TrimSuffix(sanitizeFilename(inputName), filepath.Ext(inputName))
	if base == "" {
		base = "enhanced"
	}
	name := fmt.Sprintf("%s-%s.%s", base, suffix, format)
	outputPath := filepath.Join(outputDir, name)
	if _, err := os.Stat(outputPath); err == nil {
		name = fmt.Sprintf("%s-%s-%s.%s", base, suffix, idSuffix(id), format)
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

	s.mu.Lock()
	job := s.jobs[id]
	inputPath := job.InputPath
	mode := job.Mode
	preset := job.Preset
	format := job.Format
	s.mu.Unlock()

	outputPath, _, err := s.prepareOutput(id, mode+"-"+preset)
	if err != nil {
		return err
	}

	duration := 0.0
	if tools.FFprobe != "" {
		if probe, err := probeVideo(ctx, tools.FFprobe, inputPath); err == nil {
			duration = probe.DurationSec
		}
	}

	filter := fastFilter(mode, preset)
	args := []string{
		"-y",
		"-hide_banner",
		"-nostats",
		"-i", inputPath,
		"-map", "0:v:0",
		"-map", "0:a?",
		"-vf", filter,
	}
	args = append(args, encoderArgs(ctx, tools.FFmpeg, format, preset)...)
	args = append(args, "-c:a", "copy")
	if format == "mp4" || format == "mov" {
		args = append(args, "-movflags", "+faststart")
	}
	args = append(args, "-progress", "pipe:1", outputPath)

	s.updateJob(id, func(job *Job) {
		job.Stage = "Enhancing with FFmpeg"
		job.Progress = 5
	})

	return runCommand(ctx, commandHooks{
		Line: func(line string) {
			s.appendLog(id, line)
			if strings.HasPrefix(line, "out_time_ms=") && duration > 0 {
				value := parseFloat(strings.TrimPrefix(line, "out_time_ms="))
				progress := math.Min(96, math.Max(5, (value/(duration*1000000))*96))
				s.updateJob(id, func(job *Job) {
					job.Progress = progress
				})
			}
		},
	}, tools.FFmpeg, args...)
}

func (s *Server) runAI(ctx context.Context, id string) error {
	tools := detectTools()
	if tools.FFmpeg == "" {
		return fmt.Errorf("ffmpeg is required. Install with: brew install ffmpeg")
	}
	if tools.FFprobe == "" {
		return fmt.Errorf("ffprobe is required for AI upscaling. It is usually installed with ffmpeg")
	}
	if tools.RealESRGAN == "" {
		return fmt.Errorf("realesrgan-ncnn-vulkan is required for AI upscaling. Run scripts/install-tools-macos.sh or set REALESRGAN_BIN")
	}

	s.mu.Lock()
	job := s.jobs[id]
	inputPath := job.InputPath
	mode := job.Mode
	preset := job.Preset
	format := job.Format
	workDir := job.workDir
	s.mu.Unlock()

	scale := 2
	model := "realesrgan-x4plus"
	if mode == "ai-4x" {
		scale = 4
	}
	if mode == "anime" {
		scale = 2
		model = "realesr-animevideov3"
	}

	outputPath, _, err := s.prepareOutput(id, fmt.Sprintf("%s-%s", mode, preset))
	if err != nil {
		return err
	}
	inputPath = absOrOriginal(inputPath)
	outputPath = absOrOriginal(outputPath)

	probe, err := probeVideo(ctx, tools.FFprobe, inputPath)
	if err != nil {
		return fmt.Errorf("could not inspect input video: %w", err)
	}

	framesDir := absOrOriginal(filepath.Join(workDir, "frames"))
	upscaledDir := absOrOriginal(filepath.Join(workDir, "upscaled"))
	if err := os.MkdirAll(framesDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(upscaledDir, 0o755); err != nil {
		return err
	}

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
	if err := runCommand(ctx, commandHooks{
		Line: func(line string) {
			s.appendLog(id, line)
			if strings.HasPrefix(line, "out_time_ms=") && probe.DurationSec > 0 {
				value := parseFloat(strings.TrimPrefix(line, "out_time_ms="))
				progress := 3 + math.Min(17, (value/(probe.DurationSec*1000000))*17)
				s.updateJob(id, func(job *Job) {
					job.Progress = progress
				})
			}
		},
	}, tools.FFmpeg, extractArgs...); err != nil {
		return err
	}

	frameCount := countPNG(framesDir)
	if frameCount == 0 {
		return fmt.Errorf("no frames were extracted from the input video")
	}

	s.updateJob(id, func(job *Job) {
		job.Stage = fmt.Sprintf("AI upscaling %d frames", frameCount)
		job.Progress = 20
	})
	aiArgs := []string{
		"-i", framesDir,
		"-o", upscaledDir,
		"-n", model,
		"-s", strconv.Itoa(scale),
		"-f", "png",
	}
	if modelPath := strings.TrimSpace(os.Getenv("REALESRGAN_MODEL_PATH")); modelPath != "" {
		aiArgs = append(aiArgs, "-m", absOrOriginal(modelPath))
	}
	switch preset {
	case "fast":
		aiArgs = append(aiArgs, "-j", "2:2:2")
	case "best":
		aiArgs = append(aiArgs, "-j", "1:2:2")
	default:
		aiArgs = append(aiArgs, "-j", "1:2:2")
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("REALESRGAN_TTA")), "1") {
		aiArgs = append(aiArgs, "-x")
	}

	realESRGANPath := absOrOriginal(tools.RealESRGAN)
	aiCmd := exec.CommandContext(ctx, realESRGANPath, aiArgs...)
	aiCmd.Dir = filepath.Dir(realESRGANPath)
	if err := runPreparedCommand(ctx, commandHooks{
		Line: func(line string) {
			s.appendLog(id, line)
			if percent, ok := parsePercentLine(line); ok {
				done := countPNG(upscaledDir)
				progress := 20 + math.Min(65, ((float64(done)+(percent/100))/float64(frameCount))*65)
				s.updateJob(id, func(job *Job) {
					job.Progress = progress
					job.Stage = fmt.Sprintf("AI upscaling frames %d/%d (current %.0f%%)", done, frameCount, percent)
				})
			}
		},
		Tick: func() {
			done := countPNG(upscaledDir)
			progress := 20 + math.Min(65, (float64(done)/float64(frameCount))*65)
			s.updateJob(id, func(job *Job) {
				job.Progress = progress
				job.Stage = fmt.Sprintf("AI upscaling frames %d/%d", done, frameCount)
			})
		},
	}, aiCmd); err != nil {
		return err
	}

	if done := countPNG(upscaledDir); done == 0 {
		return fmt.Errorf("AI upscaler produced no output frames")
	}

	s.updateJob(id, func(job *Job) {
		job.Stage = "Rebuilding video"
		job.Progress = 86
	})
	rebuildArgs := []string{
		"-y",
		"-hide_banner",
		"-nostats",
		"-framerate", probe.FPSString,
		"-i", filepath.Join(upscaledDir, "%08d.png"),
		"-i", inputPath,
		"-map", "0:v:0",
		"-map", "1:a?",
	}
	rebuildArgs = append(rebuildArgs, encoderArgs(ctx, tools.FFmpeg, format, preset)...)
	rebuildArgs = append(rebuildArgs, "-c:a", "copy", "-shortest")
	if format == "mp4" || format == "mov" {
		rebuildArgs = append(rebuildArgs, "-movflags", "+faststart")
	}
	rebuildArgs = append(rebuildArgs, "-progress", "pipe:1", outputPath)

	return runCommand(ctx, commandHooks{
		Line: func(line string) {
			s.appendLog(id, line)
			if strings.HasPrefix(line, "out_time_ms=") && probe.DurationSec > 0 {
				value := parseFloat(strings.TrimPrefix(line, "out_time_ms="))
				progress := 86 + math.Min(12, (value/(probe.DurationSec*1000000))*12)
				s.updateJob(id, func(job *Job) {
					job.Progress = progress
				})
			}
		},
	}, tools.FFmpeg, rebuildArgs...)
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

func encoderArgs(ctx context.Context, ffmpegPath, format, preset string) []string {
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

func encoderAvailable(ctx context.Context, ffmpegPath, encoder string) bool {
	if ffmpegPath == "" {
		return false
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(checkCtx, ffmpegPath, "-hide_banner", "-encoders").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), encoder)
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
