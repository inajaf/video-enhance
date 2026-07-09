$ErrorActionPreference = "Stop"

$RootDir = Resolve-Path (Join-Path $PSScriptRoot "..")
$ToolsDir = Join-Path $RootDir "tools"
$RealESRGANDir = Join-Path $ToolsDir "realesrgan-ncnn-vulkan"
$ZipUrl = "https://github.com/xinntao/Real-ESRGAN/releases/download/v0.2.5.0/realesrgan-ncnn-vulkan-20220424-windows.zip"
$ZipPath = Join-Path $ToolsDir "realesrgan-ncnn-vulkan-windows.zip"

New-Item -ItemType Directory -Force -Path $ToolsDir | Out-Null

if (-not (Get-Command ffmpeg -ErrorAction SilentlyContinue)) {
    if (Get-Command winget -ErrorAction SilentlyContinue) {
        Write-Host "ffmpeg not found. Installing with winget..."
        winget install --id Gyan.FFmpeg -e --accept-package-agreements --accept-source-agreements
        Write-Host "If ffmpeg is still not detected, close and reopen PowerShell so PATH refreshes."
    } else {
        Write-Host "ffmpeg is required but was not found."
        Write-Host "Install it manually, then make sure ffmpeg.exe and ffprobe.exe are on PATH."
        exit 1
    }
} else {
    Write-Host "ffmpeg found: $((Get-Command ffmpeg).Source)"
}

$RealExe = Join-Path $RealESRGANDir "realesrgan-ncnn-vulkan.exe"
if (Test-Path $RealExe) {
    Write-Host "Real-ESRGAN found: $RealExe"
    exit 0
}

$TmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ("realesrgan-" + [System.Guid]::NewGuid())
New-Item -ItemType Directory -Force -Path $TmpDir | Out-Null

try {
    Write-Host "Downloading Real-ESRGAN ncnn Vulkan..."
    Invoke-WebRequest -Uri $ZipUrl -OutFile $ZipPath
    Expand-Archive -Path $ZipPath -DestinationPath $TmpDir -Force

    if (Test-Path $RealESRGANDir) {
        Remove-Item -Recurse -Force $RealESRGANDir
    }
    New-Item -ItemType Directory -Force -Path $RealESRGANDir | Out-Null

    $InnerDir = Get-ChildItem -Path $TmpDir -Directory -Recurse |
        Where-Object { $_.Name -like "realesrgan-ncnn-vulkan-*-windows" } |
        Select-Object -First 1

    if ($InnerDir) {
        Copy-Item -Path (Join-Path $InnerDir.FullName "*") -Destination $RealESRGANDir -Recurse -Force
    } elseif (Test-Path (Join-Path $TmpDir "realesrgan-ncnn-vulkan.exe")) {
        Copy-Item -Path (Join-Path $TmpDir "*") -Destination $RealESRGANDir -Recurse -Force
    } else {
        Write-Host "Could not find extracted Real-ESRGAN folder."
        exit 1
    }

    Remove-Item -Force $ZipPath
    Write-Host "Installed Real-ESRGAN at $RealExe"
} finally {
    if (Test-Path $TmpDir) {
        Remove-Item -Recurse -Force $TmpDir
    }
}
