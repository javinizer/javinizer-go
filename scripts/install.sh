#!/bin/bash
set -e

# Javinizer CLI Installer
# Usage:
#   curl -sSL https://raw.githubusercontent.com/javinizer/javinizer-go/main/scripts/install.sh | bash
#   # install the newest release including prereleases:
#   curl -sSL https://raw.githubusercontent.com/javinizer/javinizer-go/main/scripts/install.sh | bash -s -- --pre-release

GITHUB_REPO="javinizer/javinizer-go"
BINARY_NAME="javinizer"
INSTALL_DIR="/usr/local/bin"
USER_INSTALL_DIR="$HOME/bin"
PRE_RELEASE=false

# Parse flags. Passing args through `curl | bash` requires `bash -s -- <flags>`.
while [[ $# -gt 0 ]]; do
    case "$1" in
        --pre-release)
            PRE_RELEASE=true
            shift
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}" >&2
            echo "Usage: bash install.sh [--pre-release]" >&2
            exit 1
            ;;
    esac
done

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Detect OS and architecture
detect_platform() {
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)

    case "$OS" in
        linux)
            OS_NAME="linux"
            ;;
        darwin)
            OS_NAME="darwin"
            ;;
        mingw*|msys*|cygwin*)
            OS_NAME="windows"
            ;;
        *)
            echo -e "${RED}Unsupported OS: $OS${NC}"
            exit 1
            ;;
    esac

    case "$ARCH" in
        x86_64|amd64)
            ARCH_NAME="amd64"
            ;;
        aarch64|arm64)
            ARCH_NAME="arm64"
            ;;
        *)
            echo -e "${RED}Unsupported architecture: $ARCH${NC}"
            exit 1
            ;;
    esac

    # Use universal binary for macOS if available
    if [ "$OS_NAME" = "darwin" ]; then
        PLATFORM="${OS_NAME}-universal"
    else
        PLATFORM="${OS_NAME}-${ARCH_NAME}"
    fi

    if [ "$OS_NAME" = "windows" ]; then
        BINARY_EXT=".exe"
    else
        BINARY_EXT=""
    fi

    echo -e "${GREEN}Detected platform: $PLATFORM${NC}"
}

# Portable SHA256 checksum function
calculate_sha256() {
    local file="$1"

    if command -v sha256sum >/dev/null 2>&1; then
        # Linux
        sha256sum "$file" | awk '{print $1}'
    elif command -v shasum >/dev/null 2>&1; then
        # macOS (shasum is standard)
        shasum -a 256 "$file" | awk '{print $1}'
    elif command -v openssl >/dev/null 2>&1; then
        # Fallback to openssl
        openssl dgst -sha256 "$file" | awk '{print $NF}'
    else
        echo -e "${YELLOW}Warning: No SHA256 tool found, skipping checksum verification${NC}" >&2
        echo ""
    fi
}

# Get latest release version.
#
# By default only the latest STABLE release is installed (GitHub's
# /releases/latest excludes prereleases). If no stable release exists yet, the
# installer stops and points the user at --pre-release (or the Releases page)
# rather than silently installing a prerelease — prereleases are opt-in.
#
# With --pre-release, the /releases list endpoint is used instead, returning the
# newest release including prereleases (e.g. v1.0.0-rc3).
get_latest_version() {
    echo -e "${YELLOW}Fetching latest release...${NC}"
    # Branch on PRE_RELEASE first so the list endpoint is the primary path when
    # opted in: otherwise a newer prerelease is ignored whenever a stable exists.
    if [ "$PRE_RELEASE" = true ]; then
        VERSION=$(curl -fsSL "https://api.github.com/repos/$GITHUB_REPO/releases?per_page=1" 2>/dev/null | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/' | head -1)
    else
        VERSION=$(curl -fsSL "https://api.github.com/repos/$GITHUB_REPO/releases/latest" 2>/dev/null | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
        if [ -z "$VERSION" ]; then
            echo -e "${RED}No stable release is available yet.${NC}"
            echo -e "${YELLOW}Javinizer is currently in pre-release. To install the latest pre-release, re-run with --pre-release:${NC}"
            echo -e "${GREEN}  curl -sSL https://raw.githubusercontent.com/javinizer/javinizer-go/main/scripts/install.sh | bash -s -- --pre-release${NC}"
            echo -e "${YELLOW}Or download a specific release from: https://github.com/$GITHUB_REPO/releases${NC}"
            exit 1
        fi
    fi

    if [ -z "$VERSION" ]; then
        echo -e "${RED}Failed to fetch latest version${NC}"
        exit 1
    fi

    if [ "$PRE_RELEASE" = true ] && echo "$VERSION" | grep -q -- '-'; then
        echo -e "${YELLOW}Note: $VERSION is a pre-release.${NC}"
    fi
    echo -e "${GREEN}Latest version: $VERSION${NC}"
}

# Download binary
download_binary() {
    # Release assets use stable names (javinizer-<platform>) since the version
    # is baked into the binary via ldflags; the version only appears in the
    # download path, not the asset filename.
    DOWNLOAD_URL="https://github.com/$GITHUB_REPO/releases/download/$VERSION/${BINARY_NAME}-${PLATFORM}${BINARY_EXT}"
    CHECKSUM_URL="https://github.com/$GITHUB_REPO/releases/download/$VERSION/checksums.txt"

    echo -e "${YELLOW}Downloading $BINARY_NAME from $DOWNLOAD_URL${NC}"

    TMP_DIR=$(mktemp -d)
    TMP_FILE="$TMP_DIR/${BINARY_NAME}${BINARY_EXT}"

    if ! curl -L -o "$TMP_FILE" "$DOWNLOAD_URL"; then
        echo -e "${RED}Failed to download binary${NC}"
        rm -rf "$TMP_DIR"
        exit 1
    fi

    # Download and verify checksum. The Go self-upgrade and install.ps1 both
    # ABORT on any verification failure; this installer must do the same so a
    # network attacker (or a partial release) can never land an unverified
    # binary on PATH.
    echo -e "${YELLOW}Verifying checksum...${NC}"
    if ! curl -fsSL "$CHECKSUM_URL" -o "$TMP_DIR/checksums.txt"; then
        echo -e "${RED}Could not download checksums.txt — refusing to install unverified binary${NC}"
        rm -rf "$TMP_DIR"
        exit 1
    fi

    # Match the asset name as an exact field (sha256sum output: "<hash>  <name>"
    # or "<hash> *<name>"), not a substring, so a sibling asset whose name
    # contains this one can't match the wrong line.
    EXPECTED_CHECKSUM=$(awk -v name="${BINARY_NAME}-${PLATFORM}${BINARY_EXT}" '$2==name || $2=="*"name {print $1; exit}' "$TMP_DIR/checksums.txt")
    ACTUAL_CHECKSUM=$(calculate_sha256 "$TMP_FILE")

    if [ -z "$ACTUAL_CHECKSUM" ]; then
        echo -e "${RED}No SHA256 tool found (sha256sum/shasum/openssl) — refusing to install unverified binary${NC}"
        rm -rf "$TMP_DIR"
        exit 1
    fi
    if [ -z "$EXPECTED_CHECKSUM" ]; then
        echo -e "${RED}Checksum for ${BINARY_NAME}-${PLATFORM}${BINARY_EXT} not found in checksums.txt — refusing to install${NC}"
        rm -rf "$TMP_DIR"
        exit 1
    fi
    if [ "$EXPECTED_CHECKSUM" != "$ACTUAL_CHECKSUM" ]; then
        echo -e "${RED}Checksum verification failed!${NC}"
        echo -e "${RED}Expected: $EXPECTED_CHECKSUM${NC}"
        echo -e "${RED}Actual: $ACTUAL_CHECKSUM${NC}"
        rm -rf "$TMP_DIR"
        exit 1
    fi
    echo -e "${GREEN}Checksum verified!${NC}"

    chmod +x "$TMP_FILE"
}

# Install binary
install_binary() {
    # If the system install dir is directly writable, use a plain mv (no sudo)
    # — running sudo in a `curl | bash` pipe has no tty and would fail.
    if [ -w "$INSTALL_DIR" ]; then
        echo -e "${YELLOW}Installing to $INSTALL_DIR...${NC}"
        mv "$TMP_FILE" "$INSTALL_DIR/$BINARY_NAME"
        echo -e "${GREEN}Installed to $INSTALL_DIR/$BINARY_NAME${NC}"
    elif sudo -n true 2>/dev/null; then
        echo -e "${YELLOW}Installing to $INSTALL_DIR (requires sudo)...${NC}"
        sudo mv "$TMP_FILE" "$INSTALL_DIR/$BINARY_NAME"
        echo -e "${GREEN}Installed to $INSTALL_DIR/$BINARY_NAME${NC}"
    else
        # Fall back to user install
        echo -e "${YELLOW}Installing to $USER_INSTALL_DIR (user-local)...${NC}"
        mkdir -p "$USER_INSTALL_DIR"
        mv "$TMP_FILE" "$USER_INSTALL_DIR/$BINARY_NAME"
        echo -e "${GREEN}Installed to $USER_INSTALL_DIR/$BINARY_NAME${NC}"

        # Check if user bin is in PATH
        if [[ ":$PATH:" != *":$USER_INSTALL_DIR:"* ]]; then
            echo -e "${YELLOW}Note: $USER_INSTALL_DIR is not in your PATH${NC}"
            echo -e "${YELLOW}Add this line to your ~/.bashrc or ~/.zshrc:${NC}"
            echo -e "${GREEN}export PATH=\"\$HOME/bin:\$PATH\"${NC}"
        fi
    fi

    rm -rf "$TMP_DIR"
}

# Main installation flow
main() {
    echo -e "${GREEN}=== Javinizer CLI Installer ===${NC}"

    detect_platform
    get_latest_version
    download_binary
    install_binary

    echo -e "${GREEN}=== Installation Complete ===${NC}"
    echo -e "${GREEN}Run '${BINARY_NAME} --version' to verify installation${NC}"
}

main
