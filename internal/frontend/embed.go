// Package frontend embeds the compiled web UI into the binary.
//
// The //go:embed directive packs the entire www/ subtree at compile time,
// so the binary is fully self-contained and requires no external files at
// runtime (Phase 2 ISO goal).
//
// Usage:
//
//	app.Use("/", filesystem.New(filesystem.Config{
//	    Root:  frontend.FS(),
//	    Index: "index.html",
//	}))
package frontend

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:www
var assets embed.FS

// FS returns an http.FileSystem rooted at the embedded www/ directory.
// The "www/" prefix is stripped so files are served at their natural paths
// (e.g. "www/js/app.js" → "/js/app.js").
func FS() http.FileSystem {
	sub, err := fs.Sub(assets, "www")
	if err != nil {
		// Should never happen: "www" is always present (embed directive).
		panic("frontend: failed to sub embedded FS: " + err.Error())
	}
	return http.FS(sub)
}
