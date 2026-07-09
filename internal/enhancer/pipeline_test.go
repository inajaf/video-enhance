package enhancer

import (
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

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

	paths := aiJobPaths{
		framesDir:   "/tmp/frames",
		upscaledDir: "/tmp/upscaled",
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
		"-j",
		"2:2:2",
	} {
		if !slices.Contains(args, want) {
			t.Fatalf("args = %v, missing %q", args, want)
		}
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
