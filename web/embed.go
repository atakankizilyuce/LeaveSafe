// Package web carries the phone interface.
//
// The interface is a Vite + TypeScript + Preact app under src/, built into
// dist/. That build output is committed, so `go build` and `go install`
// produce a working binary on a machine with no Node installed. CI rebuilds it
// from source and fails if the committed output has drifted.
package web

import (
	"embed"
	"io/fs"
	"log"
)

//go:embed all:dist
var content embed.FS

// StaticFiles returns the embedded web UI, rooted at dist so index.html is
// served from /.
func StaticFiles() fs.FS {
	sub, err := fs.Sub(content, "dist")
	if err != nil {
		// Only reachable if the embed directive and this path disagree, which
		// is a build-time mistake rather than a runtime condition.
		log.Panicf("web: embedded dist is unreadable: %v", err)
	}
	return sub
}
