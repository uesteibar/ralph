package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// DistFS returns the embedded dist/ directory as an fs.FS with the "dist"
// prefix stripped, so files are served from the root (e.g. "index.html").
func DistFS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
