package enhancer

import (
	"path/filepath"
	"testing"
)

func TestLocalToolCandidatesIncludeWindowsExecutables(t *testing.T) {
	candidates := localToolCandidates("ffmpeg")

	requireContains(t, candidates, filepath.Join("tools", "ffmpeg"))
	requireContains(t, candidates, filepath.Join("tools", "ffmpeg.exe"))
	requireContains(t, candidates, filepath.Join("bin", "ffmpeg.exe"))
}

func TestRealESRGANCandidatesCoverSupportedPlatforms(t *testing.T) {
	candidates := realESRGANCandidates()

	requireContains(t, candidates, filepath.Join("tools", "realesrgan-ncnn-vulkan", "realesrgan-ncnn-vulkan"))
	requireContains(t, candidates, filepath.Join("tools", "realesrgan-ncnn-vulkan", "realesrgan-ncnn-vulkan.exe"))
	requireContains(t, candidates, filepath.Join("tools", "realesrgan-ncnn-vulkan-20220424-macos", "realesrgan-ncnn-vulkan"))
	requireContains(t, candidates, filepath.Join("tools", "realesrgan-ncnn-vulkan-20220424-ubuntu", "realesrgan-ncnn-vulkan"))
	requireContains(t, candidates, filepath.Join("tools", "realesrgan-ncnn-vulkan-20220424-windows", "realesrgan-ncnn-vulkan.exe"))
}

func requireContains(t *testing.T, values []string, want string) {
	t.Helper()
	for _, value := range values {
		if value == want {
			return
		}
	}
	t.Fatalf("expected %q in %v", want, values)
}
