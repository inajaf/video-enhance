#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TOOLS_DIR="$ROOT_DIR/tools"
REALESRGAN_DIR="$TOOLS_DIR/realesrgan-ncnn-vulkan"
ZIP_URL="https://github.com/xinntao/Real-ESRGAN/releases/download/v0.2.5.0/realesrgan-ncnn-vulkan-20220424-ubuntu.zip"
ZIP_PATH="$TOOLS_DIR/realesrgan-ncnn-vulkan-linux.zip"

mkdir -p "$TOOLS_DIR"

install_ffmpeg() {
  if command -v apt-get >/dev/null 2>&1; then
    sudo apt-get update
    sudo apt-get install -y ffmpeg
  elif command -v dnf >/dev/null 2>&1; then
    sudo dnf install -y ffmpeg
  elif command -v pacman >/dev/null 2>&1; then
    sudo pacman -Sy --needed ffmpeg
  elif command -v zypper >/dev/null 2>&1; then
    sudo zypper install -y ffmpeg
  else
    echo "ffmpeg is required, but no supported package manager was found."
    echo "Install ffmpeg manually and ensure ffmpeg and ffprobe are on PATH."
    exit 1
  fi
}

if ! command -v ffmpeg >/dev/null 2>&1; then
  install_ffmpeg
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
if command -v curl >/dev/null 2>&1; then
  curl -L "$ZIP_URL" -o "$ZIP_PATH"
elif command -v wget >/dev/null 2>&1; then
  wget -O "$ZIP_PATH" "$ZIP_URL"
else
  echo "curl or wget is required to download Real-ESRGAN."
  exit 1
fi

if command -v unzip >/dev/null 2>&1; then
  unzip -q "$ZIP_PATH" -d "$tmp_dir"
else
  echo "unzip is required to extract Real-ESRGAN."
  exit 1
fi

rm -rf "$REALESRGAN_DIR"
mkdir -p "$REALESRGAN_DIR"
inner_dir="$(find "$tmp_dir" -type d -name 'realesrgan-ncnn-vulkan-*-ubuntu' | head -n 1)"
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
