// Package web exposes the static UI assets as an embedded filesystem so the
// prober binary can serve them without external files on disk.
package web

import (
	"embed"
	"io/fs"
)

//go:embed index.html app.js styles.css
var assets embed.FS

// FS returns the embedded UI filesystem rooted at the web/ directory.
func FS() fs.FS { return assets }
