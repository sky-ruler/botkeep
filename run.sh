#!/usr/bin/env bash
# ==============================================================================
# BotKeep Cloud Startup Script (Optimized for Pterodactyl & botkeep.cloud)
# ==============================================================================
set -e

PORT_NUM="${SERVER_PORT:-${PORT:-8080}}"

# If binary does not exist, download pre-compiled release binary to save disk & CPU
if [ ! -f ./botkeep ]; then
    echo "[BotKeep] Fetching lightweight pre-compiled Linux binary (~12MB)..."
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL -o ./botkeep "https://github.com/sky-ruler/botkeep/releases/download/v2.1.1/botkeep-linux-amd64" || \
        curl -fsSL -o ./botkeep "https://github.com/sky-ruler/botkeep/releases/download/v2.1.0/botkeep-linux-amd64" || true
    elif command -v wget >/dev/null 2>&1; then
        wget -q -O ./botkeep "https://github.com/sky-ruler/botkeep/releases/download/v2.1.1/botkeep-linux-amd64" || \
        wget -q -O ./botkeep "https://github.com/sky-ruler/botkeep/releases/download/v2.1.0/botkeep-linux-amd64" || true
    fi

    # Fallback: compile locally if network download is unavailable
    if [ ! -s ./botkeep ]; then
        echo "[BotKeep] Compiling from local Go source..."
        go build -o ./botkeep ./cmd/server
        # Clean build cache to stay strictly under disk quota
        rm -rf ~/.cache/go-build ~/go/pkg/mod 2>/dev/null || true
    fi
    chmod +x ./botkeep
fi

echo "[BotKeep] Launching BotKeep on port ${PORT_NUM}..."
exec ./botkeep -port "${PORT_NUM}" -fleet ./fleet.yaml
