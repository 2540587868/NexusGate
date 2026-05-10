package dashboard

import (
	"embed"
	"io/fs"
	"sync"
)

//go:embed static/*
var staticFS embed.FS

var (
	staticSubFS fs.FS
	staticOnce  sync.Once
)

func getStaticFS() fs.FS {
	staticOnce.Do(func() {
		sub, err := fs.Sub(staticFS, "static")
		if err != nil {
			panic("nexusgate: invalid embedded static directory: " + err.Error())
		}
		staticSubFS = sub
	})
	return staticSubFS
}
