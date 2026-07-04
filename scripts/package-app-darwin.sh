#!/usr/bin/env bash
# package-app-darwin.sh — bundle a javinizer desktop binary into a macOS .app
#
# Usage: package-app-darwin.sh <binary> <output.app> <version> [icon.icns]
#
# Produces a minimal but valid .app bundle:
#   Javinizer.app/
#     Contents/
#       Info.plist
#       MacOS/<executable>
#       Resources/app.icns   (only if an icon is provided)
set -euo pipefail

if [ "$#" -lt 3 ]; then
	echo "usage: $0 <binary> <output.app> <version> [icon.icns]" >&2
	exit 64
fi

binary="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
out_app="$2"
version="$3"
icon="${4:-}"

if [ ! -f "$binary" ]; then
	echo "error: binary not found: $binary" >&2
	exit 1
fi

app_name="$(basename "$out_app" .app)"
contents="$out_app/Contents"
macos_dir="$contents/MacOS"
resources_dir="$contents/Resources"

rm -rf "$out_app"
mkdir -p "$macos_dir" "$resources_dir"

# Copy the binary in as the bundle executable.
cp "$binary" "$macos_dir/$app_name"
chmod +x "$macos_dir/$app_name"

# Icon (optional — macOS falls back to a generic icon if absent).
if [ -n "$icon" ] && [ -f "$icon" ]; then
	cp "$icon" "$resources_dir/app.icns"
	icon_entry="<key>CFBundleIconFile</key>
	<string>app.icns</string>"
else
	icon_entry=""
fi

cat > "$contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key>
	<string>${app_name}</string>
	<key>CFBundleDisplayName</key>
	<string>${app_name}</string>
	<key>CFBundleExecutable</key>
	<string>${app_name}</string>
	<key>CFBundleIdentifier</key>
	<string>com.javinizer.javinizer-go</string>
	<key>CFBundleVersion</key>
	<string>${version}</string>
	<key>CFBundleShortVersionString</key>
	<string>${version}</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleSignature</key>
	<string>????</string>
	<key>LSMinimumSystemVersion</key>
	<string>11.0</string>
	<key>NSHighResolutionCapable</key>
	<true/>
	<key>NSAppTransportSecurity</key>
	<dict>
		<key>NSAllowsLocalNetworking</key>
		<true/>
	</dict>
	<key>LSUIElement</key>
	<false/>
	${icon_entry}
</dict>
</plist>
PLIST

# Refresh LaunchServices metadata so the bundle is recognized immediately.
touch "$out_app"

echo "packaged: $out_app"
