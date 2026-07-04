#!/usr/bin/env bash
# package-app-linux.sh — bundle a javinizer desktop binary into a Linux AppImage
#
# Usage: package-app-linux.sh <binary> <output.AppImage> <version> [icon.png]
#
# Produces a self-contained AppImage that bundles libwebkit2gtk + gtk3 so it
# runs on any Linux distro by double-clicking (after chmod +x), without
# requiring webkit2gtk to be preinstalled.
#
# Requires at build time: libwebkit2gtk-4.1 and gtk3 dev/runtime libs present
# (linuxdeploy-plugin-gtk scans and bundles them into the AppDir). Must run on
# a Linux host — the AppImage tooling is itself arch-specific.
#
# The webkit2gtk version bundled is whatever is present on the build host
# (Ubuntu 24.04 ships 4.1, matched by the -tags webkit2_41 build flag).
set -euo pipefail

if [ "$#" -lt 3 ]; then
	echo "usage: $0 <binary> <output.AppImage> <version> [icon.png]" >&2
	exit 64
fi

binary="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
out_appimage="$2"
version="$3"
icon="${4:-}"

if [ ! -f "$binary" ]; then
	echo "error: binary not found: $binary" >&2
	exit 1
fi

arch="$(uname -m)"
case "$arch" in
	x86_64)  ld_arch="x86_64" ;;
	aarch64|arm64) ld_arch="aarch64" ;;
	*) echo "error: unsupported architecture: $arch" >&2; exit 1 ;;
esac

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT
appdir="$workdir/AppDir"
mkdir -p "$appdir/usr/bin"

# Binary at the conventional AppDir path. The .desktop Exec= name must match
# the binary filename so linuxdeploy's generated AppRun finds it.
cp "$binary" "$appdir/usr/bin/javinizer"
chmod +x "$appdir/usr/bin/javinizer"

# Icon (optional). Without one, linuxdeploy uses a default; a real icon should
# be wired in for proper desktop integration.
icon_args=()
if [ -n "$icon" ] && [ -f "$icon" ]; then
	cp "$icon" "$appdir/javinizer.png"
	icon_args=(--icon-file "$appdir/javinizer.png")
fi

# .desktop entry
cat > "$appdir/javinizer.desktop" <<EOF
[Desktop Entry]
Name=Javinizer
Comment=JAV metadata scraper and organizer
Exec=javinizer
Icon=javinizer
Type=Application
Categories=Utility;
Terminal=false
EOF

# Download arch-appropriate AppImage tooling. GitHub runners often lack FUSE
# (needed to *run* AppImages), so extract-and-run via APPIMAGE_EXTRACT_AND_RUN.
export APPIMAGE_EXTRACT_AND_RUN=1
export ARCH="$ld_arch"
export LD_LIBRARY_PATH="${LD_LIBRARY_PATH:-}"

echo "Downloading linuxdeploy ($ld_arch)..."
curl -fsSL -o "$workdir/linuxdeploy" \
	"https://github.com/linuxdeploy/linuxdeploy/releases/download/continuous/linuxdeploy-${ld_arch}.AppImage"
chmod +x "$workdir/linuxdeploy"

echo "Downloading linuxdeploy-plugin-gtk ($ld_arch)..."
curl -fsSL -o "$workdir/linuxdeploy-plugin-gtk" \
	"https://raw.githubusercontent.com/linuxdeploy/linuxdeploy-plugin-gtk/master/linuxdeploy-plugin-gtk-${ld_arch}.sh"
chmod +x "$workdir/linuxdeploy-plugin-gtk"

# linuxdeploy + the GTK plugin scan the binary's library dependencies and
# bundle webkit2gtk + gtk3 (and their transitive deps) into the AppDir, then
# --output appimage invokes appimagetool internally to produce the AppImage.
# Run from $workdir so the output lands somewhere predictable.
cd "$workdir"
"$workdir/linuxdeploy" \
	--appdir "$appdir" \
	--plugin gtk \
	--output appimage \
	--desktop-file "$appdir/javinizer.desktop" \
	"${icon_args[@]}"

produced="$(ls ./*.AppImage 2>/dev/null | head -1 || true)"
if [ -z "$produced" ] || [ ! -f "$produced" ]; then
	echo "error: linuxdeploy did not produce an AppImage in $workdir" >&2
	exit 1
fi

mv "$produced" "$out_appimage"
chmod +x "$out_appimage"
echo "packaged: $out_appimage ($ld_arch)"
