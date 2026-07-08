package enhancer

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type videoProbe struct {
	Width       int
	Height      int
	FPS         float64
	FPSString   string
	DurationSec float64
}

type ffprobeResult struct {
	Streams []struct {
		Width        int    `json:"width"`
		Height       int    `json:"height"`
		AvgFrameRate string `json:"avg_frame_rate"`
		RFrameRate   string `json:"r_frame_rate"`
		Duration     string `json:"duration"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func probeVideo(ctx context.Context, ffprobePath, inputPath string) (videoProbe, error) {
	if ffprobePath == "" {
		return videoProbe{}, fmt.Errorf("ffprobe not found")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	cmd := exec.CommandContext(
		probeCtx,
		ffprobePath,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height,avg_frame_rate,r_frame_rate,duration:format=duration",
		"-of", "json",
		inputPath,
	)
	out, err := cmd.Output()
	if err != nil {
		return videoProbe{}, err
	}
	var parsed ffprobeResult
	if err := json.Unmarshal(out, &parsed); err != nil {
		return videoProbe{}, err
	}
	if len(parsed.Streams) == 0 {
		return videoProbe{}, fmt.Errorf("no video stream found")
	}
	stream := parsed.Streams[0]
	fps := parseRate(stream.AvgFrameRate)
	fpsString := normalizeRate(stream.AvgFrameRate)
	if fps <= 0 {
		fps = parseRate(stream.RFrameRate)
		fpsString = normalizeRate(stream.RFrameRate)
	}
	if fps <= 0 {
		fps = 30
		fpsString = "30"
	}
	duration := parseFloat(stream.Duration)
	if duration <= 0 {
		duration = parseFloat(parsed.Format.Duration)
	}
	return videoProbe{
		Width:       stream.Width,
		Height:      stream.Height,
		FPS:         fps,
		FPSString:   fpsString,
		DurationSec: duration,
	}, nil
}

func parseRate(value string) float64 {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) == 2 {
		num := parseFloat(parts[0])
		den := parseFloat(parts[1])
		if den == 0 {
			return 0
		}
		return num / den
	}
	return parseFloat(value)
}

func normalizeRate(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "0/0" {
		return "30"
	}
	return value
}

func parseFloat(value string) float64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	parsed, _ := strconv.ParseFloat(value, 64)
	return parsed
}
