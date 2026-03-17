package webui

import (
	"embed"
	"io/fs"
)

// dist embeds the production web bundle used by the API server.
//
//go:embed all:dist
var dist embed.FS

// DistFS returns the embedded web bundle root filesystem.
func DistFS() (fs.FS, error) {
	return fs.Sub(dist, "dist")
}
