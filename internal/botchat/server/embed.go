package server

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

func DistFS() (fs.FS, bool) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, false
	}
	_, statErr := fs.Stat(sub, "index.html")
	return sub, statErr == nil
}
