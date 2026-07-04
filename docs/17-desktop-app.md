# Desktop App (macOS / Windows)

Javinizer can be packaged as a single clickable desktop application that opens
a native window over the embedded API server and Web UI. All CLI and TUI
subcommands remain available inside the same binary.

## How it works

The desktop app starts the existing API server (REST + embedded SvelteKit Web
UI) on a free `127.0.0.1` port and opens a native webview window (via
[Wails v2](https://wails.io)) that loads it. Because the webview navigates to
the real server origin, the Web UI's relative REST URLs and the `/ws/progress`
WebSocket both work with **zero frontend changes**.

- **macOS** — WKWebView (WebKit)
- **Windows** — WebView2

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
make build-app-all
```

Both targets run `make web-build` first, so the frontend is embedded into the
binary via `//go:embed`.

### Prerequisites

- **macOS**: Xcode Command Line Tools (for the WebKit/Obj-C toolchain). The
  `build-app-darwin` target passes `CGO_LDFLAGS="-framework UniformTypeIdentifiers"`,
  which Wails v2.12.0 needs on macOS 15+ SDKs (it references `UTType` without
  linking the framework).
- **Windows**: the `build-app-windows` target uses `CGO_ENABLED=1` (Wails v2
  requires CGO for `go-webview2`). Cross-compiling from macOS needs a mingw
  cross-compiler as `CC` (e.g. `x86_64-w64-mingw32-gcc`); for releases, build on
  a `windows-latest` runner instead (see `.github/workflows/cli-release.yml`).

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
