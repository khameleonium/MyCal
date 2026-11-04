package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static/*
var staticEmbedFS embed.FS

// StaticHandler returns an http.Handler that serves the embedded web frontend.
func StaticHandler() http.Handler {
	sub, err := fs.Sub(staticEmbedFS, "static")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}
