// Package frontend embeds the built dashboard (`npm run build` writes
// dist/) so web-ui-api can serve it. dist/ is gitignored except for
// .gitkeep, which keeps this package compiling before the first build; the
// dashboard is then served as 404s.
package frontend

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the build output rooted at dist/.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // dist is a literal embedded directory
	}
	return sub
}
