//go:build desktop

package desktop

import (
	"context"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/logger"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

// Run starts the embedded API server (REST + Web UI) on a free localhost port
// and opens a native desktop window that loads it.
//
// This file is compiled only with the `desktop` build tag
// (`go build -tags desktop`) so the wails dependency — which needs per-platform
// webview headers — never enters the normal CLI/test build.
func Run(opts Options) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv, err := StartServer(ctx, opts.ConfigFile)
	if err != nil {
		return err
	}
	logging.Infof("desktop: API server ready at %s", srv.BaseURL())

	// Guard: shut the server down even if wails.Run returns without firing
	// OnShutdown (e.g. on early error). Shutdown is idempotent.
	defer func() { _ = srv.Shutdown() }()

	return wails.Run(&options.App{
		Title:                    "Javinizer",
		Width:                    1280,
		Height:                   800,
		MinWidth:                 900,
		MinHeight:                600,
		BackgroundColour:         options.NewRGB(245, 245, 247),
		AssetServer:              &assetserver.Options{Handler: newRedirectorHandler(srv.BaseURL())},
		LogLevel:                 logger.INFO,
		LogLevelProduction:       logger.WARNING,
		EnableDefaultContextMenu: true,
		OnShutdown: func(_ context.Context) {
			_ = srv.Shutdown()
		},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarDefault(),
			About: &mac.AboutInfo{
				Title:   "Javinizer",
				Message: "JAV metadata scraper and organizer\nhttps://github.com/javinizer/javinizer-go",
			},
		},
		Windows: &windows.Options{
			Theme: windows.SystemDefault,
		},
		Linux: &linux.Options{
			// ProgramName helps window managers group the app when the .desktop
			// Name differs from the executable filename.
			ProgramName: "Javinizer",
		},
	})
}

// redirectorHTML and newRedirectorHandler live in redirector.go (no build tag)
// so they can be unit-tested in normal CI without the wails dependency.
