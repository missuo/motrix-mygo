package main

import (
	"embed"
	"log"

	"github.com/egoist/mygo/ui"
)

//go:embed icons/*.svg
var iconFiles embed.FS

//go:embed art/*.webp
var artFiles embed.FS

var (
	icons = map[string]*ui.SVG{}
	arts  = map[string]*ui.Bitmap{}
)

// icon returns a Lucide icon by the name of its file in icons.
func icon(name string) *ui.SVG {
	if s, ok := icons[name]; ok {
		return s
	}
	data, err := iconFiles.ReadFile("icons/" + name + ".svg")
	if err != nil {
		log.Panicf("icon %q: %v", name, err)
	}
	s := ui.MustParseSVG(data)
	icons[name] = s
	return s
}

// art returns one of the pictures of the settings' sections.
func art(name string) *ui.Bitmap {
	if b, ok := arts[name]; ok {
		return b
	}
	data, err := artFiles.ReadFile("art/" + name + ".webp")
	if err != nil {
		log.Panicf("art %q: %v", name, err)
	}
	b, err := ui.DecodeBitmap(data)
	if err != nil {
		log.Panicf("art %q: %v", name, err)
	}
	arts[name] = b
	return b
}
