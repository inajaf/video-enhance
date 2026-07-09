package enhancer

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type tools struct {
	FFmpeg     string
	FFprobe    string
	RealESRGAN string
}

func detectTools() tools {
	return tools{
		FFmpeg:     findExecutable("ffmpeg", "FFMPEG_BIN", localToolCandidates("ffmpeg")...),
		FFprobe:    findExecutable("ffprobe", "FFPROBE_BIN", localToolCandidates("ffprobe")...),
		RealESRGAN: findExecutable("realesrgan-ncnn-vulkan", "REALESRGAN_BIN", realESRGANCandidates()...),
	}
}

func health() HealthResponse {
	t := detectTools()
	ffmpegVersion := versionLine(t.FFmpeg)
	ffprobeVersion := versionLine(t.FFprobe)
	realVersion := versionLine(t.RealESRGAN)

	return HealthResponse{
		ReadyForFast: t.FFmpeg != "",
		ReadyForAI:   t.FFmpeg != "" && t.FFprobe != "" && t.RealESRGAN != "",
		Tools: []ToolStatus{
			{
				Name:     "ffmpeg",
				Found:    t.FFmpeg != "",
				Path:     t.FFmpeg,
				Required: true,
				Message:  "Required for every enhancement mode.",
				Version:  ffmpegVersion,
				Install:  "Run the setup script for your OS, or install ffmpeg and ensure it is on PATH.",
			},
			{
				Name:     "ffprobe",
				Found:    t.FFprobe != "",
				Path:     t.FFprobe,
				Required: false,
				Message:  "Required for accurate AI upscaling FPS and progress.",
				Version:  ffprobeVersion,
				Install:  "Usually installed together with ffmpeg.",
			},
			{
				Name:     "realesrgan-ncnn-vulkan",
				Found:    t.RealESRGAN != "",
				Path:     t.RealESRGAN,
				Required: false,
				Message:  "Required for AI upscale and anime modes.",
				Version:  realVersion,
				Install:  "Run the setup script for your OS, or set REALESRGAN_BIN.",
			},
		},
	}
}

func findExecutable(name, envName string, candidates ...string) string {
	if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
		if fileExists(value) {
			return absOrOriginal(value)
		}
	}
	for _, candidate := range candidates {
		if fileExists(candidate) {
			return absOrOriginal(candidate)
		}
	}
	if path, err := exec.LookPath(name); err == nil {
		return absOrOriginal(path)
	}
	return ""
}

func localToolCandidates(name string) []string {
	candidates := []string{
		filepath.Join("tools", name),
		filepath.Join("bin", name),
		filepath.Join(".", name),
	}
	if !strings.HasSuffix(strings.ToLower(name), ".exe") {
		exeName := name + ".exe"
		candidates = append(candidates,
			filepath.Join("tools", exeName),
			filepath.Join("bin", exeName),
			filepath.Join(".", exeName),
		)
	}
	return candidates
}

func realESRGANCandidates() []string {
	return []string{
		filepath.Join("tools", "realesrgan-ncnn-vulkan", "realesrgan-ncnn-vulkan.exe"),
		filepath.Join("tools", "realesrgan-ncnn-vulkan-20220424-windows", "realesrgan-ncnn-vulkan.exe"),
		filepath.Join("tools", "realesrgan-ncnn-vulkan", "realesrgan-ncnn-vulkan"),
		filepath.Join("tools", "realesrgan-ncnn-vulkan-20220424-macos", "realesrgan-ncnn-vulkan"),
		filepath.Join("tools", "realesrgan-ncnn-vulkan-20220424-ubuntu", "realesrgan-ncnn-vulkan"),
		filepath.Join("bin", "realesrgan-ncnn-vulkan"),
		filepath.Join("bin", "realesrgan-ncnn-vulkan.exe"),
		filepath.Join(".", "realesrgan-ncnn-vulkan"),
		filepath.Join(".", "realesrgan-ncnn-vulkan.exe"),
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func absOrOriginal(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

func versionLine(path string) string {
	if path == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "-version")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return ""
	}
	line := strings.TrimSpace(strings.Split(out.String(), "\n")[0])
	if len(line) > 180 {
		return line[:180]
	}
	return line
}
