#!/bin/bash
set -e

# Javinizer CLI Installer
# Usage: curl -sSL https://raw.githubusercontent.com/javinizer/javinizer-go/master/scripts/install.sh | bash

GITHUB_REPO="javinizer/javinizer-go"
BINARY_NAME="javinizer"
INSTALL_DIR="/usr/local/bin"
USER_INSTALL_DIR="$HOME/bin"

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

# Get latest release version
get_latest_version() {
    echo -e "${YELLOW}Fetching latest release...${NC}"
    VERSION=$(curl -s "https://api.github.com/repos/$GITHUB_REPO/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

    if [ -z "$VERSION" ]; then
        echo -e "${RED}Failed to fetch latest version${NC}"
        exit 1
    fi

    echo -e "${GREEN}Latest version: $VERSION${NC}"
}

# Download binary
download_binary() {
    DOWNLOAD_URL="https://github.com/$GITHUB_REPO/releases/download/$VERSION/${BINARY_NAME}-${VERSION}-${PLATFORM}${BINARY_EXT}"
    CHECKSUM_URL="https://github.com/$GITHUB_REPO/releases/download/$VERSION/checksums.txt"

    echo -e "${YELLOW}Downloading $BINARY_NAME from $DOWNLOAD_URL${NC}"

    TMP_DIR=$(mktemp -d)
    TMP_FILE="$TMP_DIR/${BINARY_NAME}${BINARY_EXT}"

    if ! curl -L -o "$TMP_FILE" "$DOWNLOAD_URL"; then
        echo -e "${RED}Failed to download binary${NC}"
        rm -rf "$TMP_DIR"
        exit 1
    fi

    # Download and verify checksum
    echo -e "${YELLOW}Verifying checksum...${NC}"
    if curl -sL "$CHECKSUM_URL" -o "$TMP_DIR/checksums.txt"; then
        EXPECTED_CHECKSUM=$(grep "${BINARY_NAME}-${VERSION}-${PLATFORM}${BINARY_EXT}" "$TMP_DIR/checksums.txt" | awk '{print $1}')
        ACTUAL_CHECKSUM=$(calculate_sha256 "$TMP_FILE")

        if [ -z "$ACTUAL_CHECKSUM" ]; then
            echo -e "${YELLOW}Warning: Could not calculate checksum, skipping verification${NC}"
        elif [ "$EXPECTED_CHECKSUM" != "$ACTUAL_CHECKSUM" ]; then
            echo -e "${RED}Checksum verification failed!${NC}"
            echo -e "${RED}Expected: $EXPECTED_CHECKSUM${NC}"
            echo -e "${RED}Actual: $ACTUAL_CHECKSUM${NC}"
            rm -rf "$TMP_DIR"
            exit 1
        else
            echo -e "${GREEN}Checksum verified!${NC}"
        fi
    else
        echo -e "${YELLOW}Warning: Could not verify checksum (checksums.txt not found)${NC}"
    fi

    chmod +x "$TMP_FILE"
}

# Install binary
install_binary() {
    # Try system-wide install first (requires sudo)
    if [ -w "$INSTALL_DIR" ] || sudo -n true 2>/dev/null; then
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
