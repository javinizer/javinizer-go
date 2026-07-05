# Desktop App (macOS / Windows / Linux)

Javinizer can be packaged as a single clickable desktop application that opens
a native window over the embedded API server and Web UI. All CLI and TUI
subcommands remain available inside the same binary.

## Installation

The desktop app is a **separate package** from the CLI so both can coexist. It
is published to the same package-manager taps on each **stable** release
(prereleases never reach them):

```bash
# macOS — Homebrew Cask (installs Javinizer.app to /Applications)
brew tap javinizer/homebrew-tap https://github.com/javinizer/homebrew-tap
brew install --cask javinizer-app
# trust the tap first on Homebrew 6.0+ if you skipped it above:
# brew trust --cask javinizer/tap/javinizer-app
```

```powershell
# Windows — Scoop (shim: javinizer-app; Start Menu shortcut: Javinizer)
scoop bucket add javinizer https://github.com/javinizer/scoop-javinizer
scoop install javinizer-app
```

```bash
# Linux — AppImage (direct download; self-contained, no package manager needed)
curl -L -o Javinizer.AppImage \
  https://github.com/javinizer/javinizer-go/releases/latest/download/Javinizer-linux-x86_64.AppImage
chmod +x Javinizer.AppImage
./Javinizer.AppImage
```

Linux has no Homebrew/Scoop package because AppImages are self-contained and
need no package manager — and Homebrew casks are macOS-only. For arm64 Linux,
swap `x86_64` for `aarch64` in the asset name.

To build from source instead, see [Build](#build) below.

## Updating

Desktop builds self-update in place. When a new release is available, the Web
UI's update banner shows an **"Update & restart"** button — click it and the
app downloads the new bundle, verifies it, swaps the old bundle out, and
relaunches itself. No terminal, package manager, or manual download required.
The button appears only for genuine desktop installs
(`install_environment === desktop`) that have an update pending, so if you're
running the CLI server instead of the desktop app you won't see it.

### How the in-app update works

1. **Download** — the backend fetches the new bundle for your platform (macOS
   `.zip`, Windows `.exe`, Linux `.AppImage`) to a temporary file next to the
   current one.
2. **Verify** — the bundle's SHA256 is checked against the published
   `checksums.txt` before anything is swapped.
3. **Swap** — a detached helper waits for the app to exit, then renames the
   running bundle aside and moves the new one into place.
4. **Relaunch** — the helper starts the new app, which reopens its window and
   reconnects to its (new) local server port automatically.

The app quits during the swap and the new window opens on its own when the
relaunch finishes — you don't need to relaunch anything by hand. The swapped
bundle is still unsigned (see [The app is unsigned](#the-app-is-unsigned)), but
the swap avoids re-triggering first-launch friction: on **macOS** the new
`.app` has its `com.apple.quarantine` attribute stripped, so Gatekeeper does
not re-prompt the relaunched copy; on **Windows** the old `.exe` is renamed to
`.old` and cleaned up on the next launch; on **Linux** the AppImage is renamed
in place (located via the AppImage runtime's `APPIMAGE` path).

### Alternative: package managers

If you installed via a package manager, or prefer not to use the in-app
button, the same taps republish on each **stable** release (prereleases never
reach them):

| Install method | Update command |
|----------------|-----------------|
| macOS Cask | `brew upgrade --cask javinizer-app` |
| Windows Scoop | `scoop update javinizer-app` |
| Linux AppImage | Download the latest `Javinizer-linux-x86_64.AppImage` from the [releases page](https://github.com/javinizer/javinizer-go/releases) and replace the old file |

For arm64 Linux, swap `x86_64` for `aarch64` in the asset name. Quit the
running app before replacing the bundle manually.

### `javinizer upgrade` (CLI)

The `javinizer upgrade` command still **hands off** for desktop installs: a
separate CLI process can't quit the running GUI or swap its own bundle, so it
points you to the in-app button (or the package-manager commands above) rather
than replacing the app in place. It continues to self-update CLI-only installs
normally.

## How it works

The desktop app starts the existing API server (REST + embedded SvelteKit Web
UI) on a free `127.0.0.1` port and opens a native webview window (via
[Wails v2](https://wails.io)) that loads it. Because the webview navigates to
the real server origin, the Web UI's relative REST URLs and the `/ws/progress`
WebSocket both work with **zero frontend changes**.

- **macOS** — WKWebView (WebKit)
- **Windows** — WebView2
- **Linux** — WebKitGTK (via the `webkit2_41` build tag, which selects
  libwebkit2gtk-4.1)

The `desktop` Go build tag isolates the Wails dependency so it never enters the
normal CLI/test build. A second tag, `production`, is required by Wails to
enable its real frontend. An ldflags-injected flag (`-X
.../internal/desktop.BuildDesktop=1`) makes a no-arg launch open the GUI
instead of printing help; CLI release builds keep the default (`0`) so no-args
still prints help.

## Build

```bash
make build-app-darwin    # → bin/Javinizer.app (universal: x86_64 + arm64)
make build-app-windows   # → bin/Javinizer.exe
make build-app-linux     # → bin/Javinizer-linux-<arch>.AppImage (host arch only)
make build-app-all       # → darwin + windows (Linux must be built on Linux)
```

All targets run `make web-build` first, so the frontend is embedded into the
binary via `//go:embed`. `build-app-all` covers macOS + Windows (the two that
cross-compile cleanly); the Linux AppImage must be built on Linux because its
packaging step (`scripts/package-app-linux.sh`) runs `linuxdeploy` +
`appimagetool`, which are Linux-only binaries.

### Prerequisites

- **macOS**: Xcode Command Line Tools (for the WebKit/Obj-C toolchain). The
  `build-app-darwin` target passes `CGO_LDFLAGS="-framework UniformTypeIdentifiers"`,
  which Wails v2.12.0 needs on macOS 15+ SDKs (it references `UTType` without
  linking the framework).
- **Windows**: the `build-app-windows` target uses `CGO_ENABLED=1` (Wails v2
  requires CGO for `go-webview2`). Cross-compiling from macOS needs a mingw
  cross-compiler as `CC` (e.g. `x86_64-w64-mingw32-gcc`); for releases, build on
  a `windows-latest` runner instead (see `.github/workflows/cli-release.yml`).
- **Linux**: the WebKitGTK development headers and GTK 3. On Ubuntu/Debian:
  ```bash
  sudo apt-get install -y libwebkit2gtk-4.1-dev libgtk-3-dev
  ```
  The `build-app-linux` target selects the `webkit2_41` build tag (libwebkit2gtk-4.1,
  Ubuntu 24.04's version). The AppImage packaging bundles `libwebkit2gtk` +
  `gtk3` so the resulting AppImage runs on any distro without preinstalling them.

## Launch

```bash
# macOS — double-click in Finder, or:
open bin/Javinizer.app
# or run the binary directly:
bin/Javinizer.app/Contents/MacOS/Javinizer

# Windows
bin\Javinizer.exe

# Linux — make it executable, then double-click or:
chmod +x bin/Javinizer-linux-x86_64.AppImage
./bin/Javinizer-linux-x86_64.AppImage
```

On first launch the app creates a per-user data directory for config, the
database, and logs, so it works regardless of the current working directory:

- **macOS**: `~/Library/Application Support/Javinizer/`
- **Windows**: `%APPDATA%\Javinizer\`
- **Linux**: `$XDG_CONFIG_HOME/Javinizer/` or `~/.config/Javinizer/`

CLI and TUI subcommands are still available inside the desktop binary:

```bash
bin/Javinizer.app/Contents/MacOS/Javinizer tui ~/Videos
bin/Javinizer.app/Contents/MacOS/Javinizer scrape IPX-123
```

For development, you can point the desktop binary at the worktree's config and
database instead of the portable location:

```bash
JAVINIZER_CONFIG=configs/config.yaml JAVINIZER_DB=data/javinizer.db \
  bin/Javinizer.app/Contents/MacOS/Javinizer
```

## The app is unsigned

The `.app` / `.exe` produced by `make build-app-*` is **not code-signed or
notarized**. This is intentional for now.

### What this means

- **No functional impact.** The app runs identically to a signed build once it
  is open. No features are disabled.
- **First-launch Gatekeeper prompt (macOS only).** When a copy carries a
  quarantine attribute — anything downloaded or copied from another machine —
  macOS Gatekeeper shows: *"Javinizer" cannot be opened because the developer
  cannot be verified.* This is expected for unsigned software.
- **Locally-built copies** (no quarantine attribute) open with no prompt.
- **Linux AppImages** have no equivalent signature friction — `chmod +x` and
  run. Some desktop environments may warn the first time an untrusted AppImage
  is opened; this is a per-distro prompt, not a code-signing requirement.

### Opening an unsigned build

Use any one of these the first time you launch a quarantined copy:

- **Right-click** the app → **Open** → confirm in the dialog (one-time), or
- **System Settings** → **Privacy & Security** → scroll down → **Open Anyway**, or
- Strip the quarantine attribute from the terminal:

  ```bash
  xattr -dr com.apple.quarantine /path/to/Javinizer.app
  ```

### Distribution

For public distribution, the bundle should be signed with a Developer ID
certificate and notarized through Apple's notary service so other users do not
see the Gatekeeper warning. Notarization requires an Apple Developer account
and is out of scope for the current build; see Apple's
[Notarizing macOS software before distribution](https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution).
