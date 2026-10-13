#!/usr/bin/env bash
# ==============================================================================
# BotKeep: Autonomous DevOps Fleet Health & Consensus Engine
# 1-Line Zero-Dependency Installer Script
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/sky-ruler/botkeep/main/install.sh | bash
#
# Environment variables:
#   BOTKEEP_VERSION   Specify version tag (default: latest, e.g. v2.0.0)
#   INSTALL_DIR       Binary install directory (default: /usr/local/bin or ~/.local/bin)
# ==============================================================================

set -euo pipefail

GITHUB_REPO="sky-ruler/botkeep"
BINARY_NAME="botkeep"

BOLD="\033[1m"
GREEN="\033[32m"
CYAN="\033[36m"
YELLOW="\033[33m"
RED="\033[31m"
RESET="\033[0m"

log_info() {
    printf "${CYAN}[BotKeep]${RESET} %s\n" "$1"
}

log_success() {
    printf "${GREEN}✓${RESET} ${BOLD}%s${RESET}\n" "$1"
}

log_warn() {
    printf "${YELLOW}!${RESET} %s\n" "$1"
}

log_error() {
    printf "${RED}✗ Error:${RESET} %s\n" "$1" >&2
}

# 1. Detect OS
detect_os() {
    local os
    os="$(uname -s | tr '[:upper:]' '[:lower:]')"
    case "$os" in
        linux*)   echo "linux" ;;
        darwin*)  echo "darwin" ;;
        freebsd*) echo "freebsd" ;;
        msys*|mingw*|cygwin*) echo "windows" ;;
        *)
            log_error "Unsupported operating system: $os"
            exit 1
            ;;
    esac
}

# 2. Detect Architecture
detect_arch() {
    local arch
    arch="$(uname -m | tr '[:upper:]' '[:lower:]')"
    case "$arch" in
        x86_64|amd64)     echo "amd64" ;;
        aarch64|arm64)    echo "arm64" ;;
        armv7*|armhf)     echo "armv7" ;;
        i386|i686)        echo "386" ;;
        *)
            log_error "Unsupported CPU architecture: $arch"
            exit 1
            ;;
    esac
}

main() {
    printf "${CYAN}${BOLD}"
    cat << 'EOF'
  ____        _   _  __                
 |  _ \      | | | |/ /                
 | |_) | ___ | |_| ' / ___  ___ _ __   
 |  _ < / _ \| __|  < / _ \/ _ \ '_ \  
 | |_) | (_) | |_| . \  __/  __/ |_) | 
 |____/ \___/ \__|_|\_\___|\___| .__/  
                               | |     
                               |_|     
EOF
    printf "${RESET}\n"
    printf "⚡ Autonomous DevOps Fleet Health & Consensus Engine\n\n"

    local OS
    local ARCH
    OS="$(detect_os)"
    ARCH="$(detect_arch)"

    log_info "Detected Platform: ${OS} (${ARCH})"

    # Select Install Location
    if [ -z "${INSTALL_DIR:-}" ]; then
        if [ "$(id -u)" -eq 0 ]; then
            INSTALL_DIR="/usr/local/bin"
        elif [ -d "$HOME/.local/bin" ]; then
            INSTALL_DIR="$HOME/.local/bin"
        else
            INSTALL_DIR="/usr/local/bin"
        fi
    fi

    # Determine Version
    VERSION="${BOTKEEP_VERSION:-}"
    if [ -z "$VERSION" ]; then
        log_info "Fetching latest release from GitHub (${GITHUB_REPO})..."
        if command -v curl >/dev/null 2>&1; then
            VERSION="$(curl -sSL "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/' || true)"
        elif command -v wget >/dev/null 2>&1; then
            VERSION="$(wget -qO- "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/' || true)"
        fi
    fi

    if [ -z "$VERSION" ]; then
        VERSION="v2.0.0"
        log_warn "Could not fetch dynamic latest tag from GitHub API, falling back to ${VERSION}"
    fi

    log_info "Installing BotKeep ${VERSION}..."

    # Determine binary filename and download URL
    local EXT=""
    if [ "$OS" = "windows" ]; then
        EXT=".exe"
    fi

    local ASSET_NAME="botkeep-${OS}-${ARCH}${EXT}"
    local DOWNLOAD_URL="https://github.com/${GITHUB_REPO}/releases/download/${VERSION}/${ASSET_NAME}"

    TMP_DIR="$(mktemp -d -t botkeep-install-XXXXXX)"
    trap 'rm -rf "$TMP_DIR"' EXIT

    local TARGET_FILE="${TMP_DIR}/${BINARY_NAME}${EXT}"

    log_info "Downloading ${DOWNLOAD_URL}..."
    local HTTP_STATUS=0
    if command -v curl >/dev/null 2>&1; then
        HTTP_STATUS="$(curl -sSL -w "%{http_code}" -o "$TARGET_FILE" "$DOWNLOAD_URL" || true)"
    elif command -v wget >/dev/null 2>&1; then
        if wget -q -O "$TARGET_FILE" "$DOWNLOAD_URL"; then
            HTTP_STATUS=200
        fi
    fi

    # Fallback to source compile if binary asset is not yet on GitHub Releases
    if [ "$HTTP_STATUS" != "200" ] || [ ! -s "$TARGET_FILE" ]; then
        log_warn "Pre-compiled asset not found for ${OS}/${ARCH} on GitHub release (${DOWNLOAD_URL})."
        if command -v go >/dev/null 2>&1; then
            log_info "Found Go compiler on host. Building BotKeep directly from source..."
            go install "github.com/${GITHUB_REPO}/cmd/server@${VERSION}" 2>/dev/null || go install "github.com/${GITHUB_REPO}/cmd/server@latest"
            GOPATH_BIN="$(go env GOPATH)/bin/${BINARY_NAME}${EXT}"
            if [ -f "$GOPATH_BIN" ]; then
                cp "$GOPATH_BIN" "$TARGET_FILE"
            fi
        else
            log_error "Precompiled release binary not found and 'go' is not installed."
            log_error "Please download directly from https://github.com/${GITHUB_REPO}/releases"
            exit 1
        fi
    fi

    chmod +x "$TARGET_FILE"

    # Install into target directory
    log_info "Installing binary to ${INSTALL_DIR}/${BINARY_NAME}${EXT}..."
    if [ -w "$INSTALL_DIR" ]; then
        cp "$TARGET_FILE" "${INSTALL_DIR}/${BINARY_NAME}${EXT}"
    else
        log_warn "Elevated privileges required to write to ${INSTALL_DIR}."
        if command -v sudo >/dev/null 2>&1; then
            sudo cp "$TARGET_FILE" "${INSTALL_DIR}/${BINARY_NAME}${EXT}"
            sudo chmod +x "${INSTALL_DIR}/${BINARY_NAME}${EXT}"
        else
            log_error "Cannot write to ${INSTALL_DIR} and sudo is unavailable."
            exit 1
        fi
    fi

    # Verify Installation
    if command -v "${BINARY_NAME}" >/dev/null 2>&1; then
        log_success "BotKeep installed successfully!"
    else
        log_success "BotKeep installed to ${INSTALL_DIR}/${BINARY_NAME}${EXT}."
        if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
            log_warn "Notice: ${INSTALL_DIR} is not in your current PATH."
            log_warn "Add it to your ~/.bashrc or ~/.zshrc: export PATH=\"${INSTALL_DIR}:\$PATH\""
        fi
    fi

    printf "\n${BOLD}Quickstart Commands:${RESET}\n"
    printf "  Start Standalone Server:    ${GREEN}botkeep -port 8080 -seed${RESET}\n"
    printf "  Start With GitOps Fleet:    ${GREEN}botkeep -fleet ./fleet.yaml${RESET}\n"
    printf "  Run Lightweight Satellite:  ${GREEN}botkeep agent -server http://control-plane:8080 -region eu-west-1${RESET}\n"
    printf "  Export Fleet to GitOps:     ${GREEN}botkeep export-fleet -out fleet.yaml${RESET}\n\n"
}

main "$@"
