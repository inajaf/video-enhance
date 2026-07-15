package enhancer

import (
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestBuildOutputFilename(t *testing.T) {
	t.Parallel()

	// Naming must keep the sanitized source base and only the fixed "upscale"
	// abbreviation, independent of enhancement mode or quality preset.
	modes := []string{"fast", "fast-upscale", "ai-2x", "ai-4x", "anime"}
	presets := []string{"fast", "balanced", "best"}
	forbidden := []string{"fast-upscale", "ai-2x", "ai-4x", "balanced", "best", "anime"}

	for _, mode := range modes {
		for _, preset := range presets {
			t.Run(mode+"/"+preset, func(t *testing.T) {
				t.Parallel()

				// Mode/preset are intentionally unused for the filename; only
				// input base + format matter. Looping them documents the contract.
				_ = mode
				_ = preset

				got := buildOutputFilename("My Clip.mp4", "mp4", "")
				want := "My Clip-upscale.mp4"
				if got != want {
					t.Fatalf("buildOutputFilename() = %q, want %q", got, want)
				}
				for _, token := range forbidden {
					if token == "upscale" {
						continue
					}
					// Mode/preset tokens must not appear as name segments.
					if containsNameToken(got, token) {
						t.Fatalf("output name %q must not contain mode/preset token %q", got, token)
					}
				}
			})
		}
	}

	if got := buildOutputFilename("clip.mp4", "MOV", ""); got != "clip-upscale.mov" {
		t.Fatalf("format normalize = %q, want clip-upscale.mov", got)
	}
	if got := buildOutputFilename("../path/How_Google.mp4", "mp4", ""); got != "How_Google-upscale.mp4" {
		t.Fatalf("sanitized base = %q, want How_Google-upscale.mp4", got)
	}
	if got := buildOutputFilename("clip.mp4", "mp4", "9c208fe5"); got != "clip-upscale-9c208fe5.mp4" {
		t.Fatalf("collision = %q, want clip-upscale-9c208fe5.mp4", got)
	}
	if got := buildOutputFilename("clip.mp4", "mp4", "9c208fe5"); containsNameToken(got, "fast-upscale") || containsNameToken(got, "best") {
		t.Fatalf("collision name %q must omit mode/preset tokens", got)
	}
	if got := buildOutputFilename(" . ", "webm", ""); got != "enhanced-upscale.mp4" {
		t.Fatalf("empty base fallback = %q, want enhanced-upscale.mp4", got)
	}
}

func TestPrepareOutputUsesUpscaleAbbreviation(t *testing.T) {
	t.Parallel()

	outDir := t.TempDir()
	server := NewServer(Config{WorkDir: t.TempDir(), OutputDir: outDir}, nil)

	jobID := "job-abcdefgh"
	server.mu.Lock()
	server.jobs[jobID] = &Job{
		ID:           jobID,
		InputName:    "How_Google_Keeps_the_Internet_Running.mp4",
		Mode:         "fast-upscale",
		Preset:       "best",
		Format:       "mp4",
		requestedDir: outDir,
	}
	server.mu.Unlock()

	path, name, err := server.prepareOutput(jobID)
	if err != nil {
		t.Fatal(err)
	}
	wantName := "How_Google_Keeps_the_Internet_Running-upscale.mp4"
	if name != wantName {
		t.Fatalf("prepareOutput name = %q, want %q", name, wantName)
	}
	if filepath.Base(path) != wantName {
		t.Fatalf("prepareOutput path base = %q, want %q", filepath.Base(path), wantName)
	}
	for _, token := range []string{"fast-upscale", "best", "ai-2x", "balanced"} {
		if containsNameToken(name, token) {
			t.Fatalf("name %q must not contain %q", name, token)
		}
	}

	// Collision: existing file forces short id disambiguator, still no mode tokens.
	if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	path2, name2, err := server.prepareOutput(jobID)
	if err != nil {
		t.Fatal(err)
	}
	wantCollision := "How_Google_Keeps_the_Internet_Running-upscale-abcdefgh.mp4"
	if name2 != wantCollision {
		t.Fatalf("collision name = %q, want %q", name2, wantCollision)
	}
	if filepath.Base(path2) != wantCollision {
		t.Fatalf("collision path base = %q, want %q", filepath.Base(path2), wantCollision)
	}
	for _, token := range []string{"fast-upscale", "best", "ai-2x", "balanced"} {
		if containsNameToken(name2, token) {
			t.Fatalf("collision name %q must not contain %q", name2, token)
		}
	}

	server.mu.Lock()
	snap := server.jobs[jobID].Snapshot()
	server.mu.Unlock()
	if snap.OutputName != wantCollision {
		t.Fatalf("job OutputName = %q, want %q", snap.OutputName, wantCollision)
	}
	if snap.OutputPath != path2 {
		t.Fatalf("job OutputPath = %q, want %q", snap.OutputPath, path2)
	}
}

// containsNameToken reports whether token appears as a hyphen-delimited segment
// (or the whole stem) in the basename before the extension.
func containsNameToken(filename, token string) bool {
	base := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	for _, part := range strings.Split(base, "-") {
		if part == token {
			return true
		}
	}
	return strings.Contains(base, token)
}

func TestFFmpegProgress(t *testing.T) {
	t.Parallel()

	progress, ok := ffmpegProgress("out_time_ms=5000000", 10, 5, 91)
	if !ok {
		t.Fatal("expected progress line to parse")
	}
	assertFloat(t, progress, 50.5)

	progress, ok = ffmpegProgress("out_time_ms=20000000", 10, 5, 91)
	if !ok {
		t.Fatal("expected progress line to parse")
	}
	assertFloat(t, progress, 96)

	if _, ok := ffmpegProgress("frame=1", 10, 5, 91); ok {
		t.Fatal("non-progress line parsed as progress")
	}
}

func TestAIProgress(t *testing.T) {
	t.Parallel()

	assertFloat(t, aiProgress(0, 0, 0), 20)
	assertFloat(t, aiProgress(5, 10, 50), 55.75)
	assertFloat(t, aiProgress(10, 10, 100), 85)
}

func TestParsePercentLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
		want float64
		ok   bool
	}{
		{name: "integer", line: "42%", want: 42, ok: true},
		{name: "decimal with spaces", line: " 99.5% ", want: 99.5, ok: true},
		{name: "negative", line: "-1%", ok: false},
		{name: "over one hundred", line: "100.1%", ok: false},
		{name: "not percent", line: "progress=42", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := parsePercentLine(tt.line)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if ok {
				assertFloat(t, got, tt.want)
			}
		})
	}
}

func TestRealESRGANArgs(t *testing.T) {
	t.Setenv("REALESRGAN_MODEL_PATH", "")
	t.Setenv("REALESRGAN_TTA", "")

	// Fast/balanced use JPEG intermediates; path.frameExt must wire into -f.
	paths := aiJobPaths{
		framesDir:   "/tmp/frames",
		upscaledDir: "/tmp/upscaled",
		frameExt:    "jpg",
	}
	args := realESRGANArgs(paths, "fast", aiOptions{scale: 4, model: "realesrgan-x4plus"})

	for _, want := range []string{
		"-i",
		"/tmp/frames",
		"-o",
		"/tmp/upscaled",
		"-n",
		"realesrgan-x4plus",
		"-s",
		"4",
		"-f",
		"jpg",
		"-j",
	} {
		if !slices.Contains(args, want) {
			t.Fatalf("args = %v, missing %q", args, want)
		}
	}

	// Thread string is present and non-empty (scaled by CPU at call time).
	jIdx := slices.Index(args, "-j")
	if jIdx < 0 || jIdx+1 >= len(args) || args[jIdx+1] == "" {
		t.Fatalf("args = %v, expected non-empty -j thread string", args)
	}

	// Best keeps PNG for quality.
	bestPaths := aiJobPaths{framesDir: "/tmp/f", upscaledDir: "/tmp/u", frameExt: "png"}
	bestArgs := realESRGANArgs(bestPaths, "best", aiOptions{scale: 2, model: "realesrgan-x4plus"})
	if !slices.Contains(bestArgs, "png") {
		t.Fatalf("best args = %v, want png output format", bestArgs)
	}
}

func TestIntermediateFrameExt(t *testing.T) {
	t.Parallel()

	if got := intermediateFrameExt("fast"); got != "jpg" {
		t.Fatalf("fast ext = %q, want jpg", got)
	}
	if got := intermediateFrameExt("balanced"); got != "jpg" {
		t.Fatalf("balanced ext = %q, want jpg", got)
	}
	if got := intermediateFrameExt("best"); got != "png" {
		t.Fatalf("best ext = %q, want png", got)
	}
}

func TestRealESRGANThreads(t *testing.T) {
	t.Parallel()

	// Known CPU counts: assert shape load:proc:save and relative aggressiveness.
	// 8 CPUs → load=4, proc=4, save=2
	if got := realESRGANThreads("fast", 8); got != "4:4:2" {
		t.Fatalf("fast threads(8) = %q, want 4:4:2", got)
	}
	if got := realESRGANThreads("best", 16); got != "1:2:2" {
		t.Fatalf("best threads = %q, want conservative 1:2:2", got)
	}
	// balanced on 8: load=2, proc=2, save=2
	if got := realESRGANThreads("balanced", 8); got != "2:2:2" {
		t.Fatalf("balanced threads(8) = %q, want 2:2:2", got)
	}
	// Single-core still returns a valid triple without panicking.
	single := realESRGANThreads("fast", 1)
	if strings.Count(single, ":") != 2 {
		t.Fatalf("single-cpu threads = %q, want load:proc:save", single)
	}
}

func TestExtractFrameArgs(t *testing.T) {
	t.Parallel()

	jpgArgs := extractFrameArgs("/in.mp4", "/frames", "jpg")
	joined := strings.Join(jpgArgs, " ")
	if !strings.Contains(joined, "-q:v 2") {
		t.Fatalf("jpg extract missing high-quality -q:v 2: %v", jpgArgs)
	}
	if !strings.Contains(joined, "%08d.jpg") {
		t.Fatalf("jpg extract pattern missing: %v", jpgArgs)
	}
	if !slices.Contains(jpgArgs, "-threads") || !slices.Contains(jpgArgs, "0") {
		t.Fatalf("extract should auto-thread: %v", jpgArgs)
	}
	if !slices.Contains(jpgArgs, "-an") {
		t.Fatalf("extract should drop audio/subtitles to save I/O: %v", jpgArgs)
	}

	pngArgs := extractFrameArgs("/in.mp4", "/frames", "png")
	pngJoined := strings.Join(pngArgs, " ")
	if strings.Contains(pngJoined, "-q:v") {
		t.Fatalf("png extract must not set jpeg quality: %v", pngArgs)
	}
	if !strings.Contains(pngJoined, "%08d.png") {
		t.Fatalf("png extract pattern missing: %v", pngArgs)
	}
}

func TestFastFilterQualityInvariants(t *testing.T) {
	t.Parallel()

	modes := []string{"fast", "fast-upscale"}
	presets := []string{"fast", "balanced", "best"}

	for _, mode := range modes {
		for _, preset := range presets {
			filter := fastFilter(mode, preset)
			if !strings.Contains(filter, "format=yuv420p") {
				t.Fatalf("%s/%s filter missing yuv420p: %s", mode, preset, filter)
			}
			if !strings.Contains(filter, "trunc(") {
				t.Fatalf("%s/%s filter missing even-dimension scale: %s", mode, preset, filter)
			}
			if mode == "fast-upscale" {
				if !strings.Contains(filter, "lanczos") {
					t.Fatalf("upscale filter must use lanczos: %s", filter)
				}
				if !strings.Contains(filter, "iw*2") {
					t.Fatalf("upscale filter must 2x: %s", filter)
				}
			}
			// Fast skips hqdn3d for lower CPU; balanced/best include it.
			if preset == "fast" && strings.Contains(filter, "hqdn3d") {
				t.Fatalf("fast preset should skip heavy denoise: %s", filter)
			}
			if preset != "fast" && !strings.Contains(filter, "hqdn3d") {
				t.Fatalf("%s preset should denoise: %s", preset, filter)
			}
		}
	}
}

func TestEncoderArgsQualityInvariants(t *testing.T) {
	// Not parallel: encoderAvailable may probe ffmpeg and uses a process-wide cache.
	args := encoderArgs(t.Context(), "", "balanced")
	joined := strings.Join(args, " ")
	// Without ffmpeg path, videotoolbox is unavailable → libx264 path.
	if !slices.Contains(args, "libx264") {
		t.Fatalf("encoder without ffmpeg should fall back to libx264: %v", args)
	}
	if !slices.Contains(args, "yuv420p") {
		t.Fatalf("encoder must force yuv420p: %v", args)
	}
	if !strings.Contains(joined, "-crf") {
		t.Fatalf("libx264 path must set crf: %v", args)
	}
	if !slices.Contains(args, "-threads") {
		t.Fatalf("libx264 should enable auto threads: %v", args)
	}

	fast := encoderArgs(t.Context(), "", "fast")
	best := encoderArgs(t.Context(), "", "best")
	// Lower CRF number = higher quality; best must be stricter than fast.
	fastCRF := crfFromArgs(fast)
	bestCRF := crfFromArgs(best)
	if bestCRF >= fastCRF {
		t.Fatalf("best crf %v should be lower (higher quality) than fast crf %v", bestCRF, fastCRF)
	}
}

func crfFromArgs(args []string) float64 {
	for i, a := range args {
		if a == "-crf" && i+1 < len(args) {
			return parseFloat(args[i+1])
		}
	}
	return -1
}

func TestRebuildVideoArgs(t *testing.T) {
	t.Parallel()

	paths := aiJobPaths{
		inputPath:   "/in.mp4",
		outputPath:  "/out.mp4",
		upscaledDir: "/upscaled",
		frameExt:    "jpg",
	}
	job := pipelineJob{preset: "balanced", format: "mp4"}
	probe := videoProbe{FPSString: "30/1"}
	encodeArgs := []string{"-c:v", "libx264", "-preset", "medium", "-crf", "18", "-pix_fmt", "yuv420p"}

	args := rebuildVideoArgs("ffmpeg", paths, job, probe, encodeArgs)
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "%08d.jpg") {
		t.Fatalf("rebuild must read jpg frames: %v", args)
	}
	if !slices.Contains(args, "0:v:0") || !slices.Contains(args, "1:a?") {
		t.Fatalf("rebuild must map video + optional audio: %v", args)
	}
	if !strings.Contains(joined, "format=yuv420p") {
		t.Fatalf("rebuild must enforce yuv420p: %v", args)
	}
	if !strings.Contains(joined, "trunc(iw/2)*2") {
		t.Fatalf("rebuild must force even dimensions: %v", args)
	}
	if !slices.Contains(args, "+faststart") {
		t.Fatalf("mp4 rebuild should use faststart: %v", args)
	}
	if !slices.Contains(args, "-shortest") {
		t.Fatalf("rebuild should use -shortest with audio map: %v", args)
	}
}

func TestFastCommandArgs(t *testing.T) {
	// Not parallel: may touch encoder cache via encoderArgs.
	job := pipelineJob{
		inputPath: "/video.mp4",
		mode:      "fast-upscale",
		preset:    "balanced",
		format:    "mp4",
	}
	args := fastCommandArgs(t.Context(), "", job, "/out.mp4")
	joined := strings.Join(args, " ")

	if !slices.Contains(args, "-threads") {
		t.Fatalf("fast command should auto-thread: %v", args)
	}
	if !slices.Contains(args, "0:v:0") || !slices.Contains(args, "0:a?") {
		t.Fatalf("fast command must map video + optional audio: %v", args)
	}
	vfIdx := slices.Index(args, "-vf")
	if vfIdx < 0 || vfIdx+1 >= len(args) {
		t.Fatalf("missing -vf: %v", args)
	}
	filter := args[vfIdx+1]
	if !strings.Contains(filter, "lanczos") || !strings.Contains(filter, "yuv420p") {
		t.Fatalf("filter = %q, want lanczos upscale + yuv420p", filter)
	}
	if !strings.Contains(joined, "+faststart") {
		t.Fatalf("mp4 should use faststart: %v", args)
	}
}

func TestCountPNG(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "00000001.png"))
	writeTestFile(t, filepath.Join(dir, "00000002.PNG"))
	writeTestFile(t, filepath.Join(dir, "notes.txt"))

	if err := os.Mkdir(filepath.Join(dir, "directory.png"), 0o755); err != nil {
		t.Fatal(err)
	}

	if got := countPNG(dir); got != 2 {
		t.Fatalf("countPNG() = %d, want 2", got)
	}
}

func TestCountFrames(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "00000001.jpg"))
	writeTestFile(t, filepath.Join(dir, "00000002.JPG"))
	writeTestFile(t, filepath.Join(dir, "00000003.png"))
	writeTestFile(t, filepath.Join(dir, "readme.txt"))

	if got := countFrames(dir, "jpg"); got != 2 {
		t.Fatalf("countFrames(jpg) = %d, want 2", got)
	}
	if got := countFrames(dir, "png"); got != 1 {
		t.Fatalf("countFrames(png) = %d, want 1", got)
	}
}

func TestPNGCounterCachesWithinRefreshWindow(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "00000001.png"))

	counter := newPNGCounter(dir)
	counter.minRefresh = time.Hour
	start := time.Unix(100, 0)

	if got := counter.CountAt(start); got != 1 {
		t.Fatalf("initial CountAt() = %d, want 1", got)
	}

	writeTestFile(t, filepath.Join(dir, "00000002.png"))
	if got := counter.CountAt(start.Add(time.Second)); got != 1 {
		t.Fatalf("cached CountAt() = %d, want 1", got)
	}

	if got := counter.CountAt(start.Add(time.Hour)); got != 2 {
		t.Fatalf("refreshed CountAt() = %d, want 2", got)
	}
}

func TestFrameCounterCountsJPEG(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "00000001.jpg"))
	counter := newFrameCounter(dir, "jpg")
	if got := counter.CountAt(time.Unix(1, 0)); got != 1 {
		t.Fatalf("jpeg CountAt() = %d, want 1", got)
	}
}

func BenchmarkCountPNG(b *testing.B) {
	dir := benchmarkPNGDir(b, 1000)

	b.ResetTimer()
	for range b.N {
		_ = countPNG(dir)
	}
}

func BenchmarkPNGCounterCached(b *testing.B) {
	dir := benchmarkPNGDir(b, 1000)
	counter := newPNGCounter(dir)
	counter.minRefresh = time.Hour
	now := time.Unix(100, 0)
	counter.CountAt(now)

	b.ResetTimer()
	for range b.N {
		_ = counter.CountAt(now.Add(time.Second))
	}
}

func benchmarkPNGDir(b *testing.B, total int) string {
	b.Helper()

	dir := b.TempDir()
	for i := range total {
		writeTestFile(b, filepath.Join(dir, "frame-"+time.Duration(i).String()+".png"))
	}

	return dir
}

func writeTestFile(tb testing.TB, path string) {
	tb.Helper()

	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		tb.Fatal(err)
	}
}

func assertFloat(tb testing.TB, got float64, want float64) {
	tb.Helper()

	if math.Abs(got-want) > 0.0001 {
		tb.Fatalf("got %v, want %v", got, want)
	}
}
