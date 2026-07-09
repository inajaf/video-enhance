#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

case "$(uname -s)" in
  Darwin)
    exec "$SCRIPT_DIR/install-tools-macos.sh"
    ;;
  Linux)
    exec "$SCRIPT_DIR/install-tools-linux.sh"
    ;;
  *)
    echo "Unsupported Unix platform: $(uname -s)"
    echo "On Windows, run: powershell -ExecutionPolicy Bypass -File scripts/install-tools-windows.ps1"
    exit 1
    ;;
esac
