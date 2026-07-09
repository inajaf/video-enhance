package enhancer

import (
	"path/filepath"
	"testing"
)

func TestNormalizeOptions(t *testing.T) {
	t.Parallel()

	if got := normalizeMode("ai-4x"); got != "ai-4x" {
		t.Fatalf("normalizeMode() = %q, want ai-4x", got)
	}
	if got := normalizeMode("bad"); got != "fast" {
		t.Fatalf("normalizeMode() = %q, want fast", got)
	}
	if got := normalizePreset("best"); got != "best" {
		t.Fatalf("normalizePreset() = %q, want best", got)
	}
	if got := normalizePreset("bad"); got != "balanced" {
		t.Fatalf("normalizePreset() = %q, want balanced", got)
	}
	if got := normalizeFormat(" MOV "); got != "mov" {
		t.Fatalf("normalizeFormat() = %q, want mov", got)
	}
	if got := normalizeFormat("webm"); got != "mp4" {
		t.Fatalf("normalizeFormat() = %q, want mp4", got)
	}
}

func TestSanitizeFilename(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{input: "../clip.mov", want: "clip.mov"},
		{input: " bad:name?.mp4 ", want: "bad_name_.mp4"},
		{input: " . ", want: ""},
	}

	for _, tt := range tests {
		if got := sanitizeFilename(tt.input); got != tt.want {
			t.Fatalf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestResolveOutputDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	got, err := resolveOutputDir("outputs", dir)
	if err != nil {
		t.Fatal(err)
	}

	want, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("resolveOutputDir() = %q, want %q", got, want)
	}
}
