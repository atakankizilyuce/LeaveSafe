package web

import (
	"embed"
	"io/fs"
)

//go:embed index.html style.css app.js sw.js
var content embed.FS

// StaticFiles returns the embedded web UI files.
func StaticFiles() fs.FS {
	return content
}
