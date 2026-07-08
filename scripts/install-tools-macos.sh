#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TOOLS_DIR="$ROOT_DIR/tools"
REALESRGAN_DIR="$TOOLS_DIR/realesrgan-ncnn-vulkan"
ZIP_URL="https://github.com/xinntao/Real-ESRGAN/releases/download/v0.2.5.0/realesrgan-ncnn-vulkan-20220424-macos.zip"
ZIP_PATH="$TOOLS_DIR/realesrgan-ncnn-vulkan-macos.zip"

mkdir -p "$TOOLS_DIR"

if ! command -v ffmpeg >/dev/null 2>&1; then
  if ! command -v brew >/dev/null 2>&1; then
    echo "Homebrew is required to install ffmpeg automatically."
    echo "Install Homebrew first, or install ffmpeg manually and ensure it is on PATH."
    exit 1
  fi
  brew install ffmpeg
else
  echo "ffmpeg found: $(command -v ffmpeg)"
fi

if [ -x "$REALESRGAN_DIR/realesrgan-ncnn-vulkan" ]; then
  echo "Real-ESRGAN found: $REALESRGAN_DIR/realesrgan-ncnn-vulkan"
  exit 0
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

echo "Downloading Real-ESRGAN ncnn Vulkan..."
curl -L "$ZIP_URL" -o "$ZIP_PATH"
unzip -q "$ZIP_PATH" -d "$tmp_dir"

rm -rf "$REALESRGAN_DIR"
mkdir -p "$REALESRGAN_DIR"
inner_dir="$(find "$tmp_dir" -type d -name 'realesrgan-ncnn-vulkan-*-macos' | head -n 1)"
if [ -n "$inner_dir" ]; then
  cp -R "$inner_dir"/. "$REALESRGAN_DIR"/
elif [ -f "$tmp_dir/realesrgan-ncnn-vulkan" ]; then
  cp -R "$tmp_dir"/. "$REALESRGAN_DIR"/
else
  echo "Could not find extracted Real-ESRGAN folder."
  exit 1
fi

chmod +x "$REALESRGAN_DIR/realesrgan-ncnn-vulkan"
rm -f "$ZIP_PATH"

echo "Installed Real-ESRGAN at $REALESRGAN_DIR/realesrgan-ncnn-vulkan"
