package desktop

// BuildDesktop is injected via -ldflags "-X internal/desktop.BuildDesktop=1" for
// desktop (clickable-app) builds. When "1", running the binary with no
// subcommand launches the GUI instead of printing CLI help. The CLI release
// keeps the default "0" so existing no-arg behavior (help) is unchanged.
var BuildDesktop = "0"

// IsDesktopBuild reports whether this binary was built as a desktop app.
func IsDesktopBuild() bool { return BuildDesktop == "1" }
