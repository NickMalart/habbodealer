package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	g "xabbo.b7c.io/goearth"
)

//go:embed all:frontend/dist
var assets embed.FS

var ext = g.NewExt(g.ExtInfo{
	Title:       "Multipurpose App",
	Description: "A multipurpose app that attaches to G-Earth",
	Version:     "1.0.0",
	Author:      "Dubbo",
})

func main() {
	// Create an instance of the app structure
	app := NewApp(ext)

	ext.Activated(func() {
		app.ShowWindow()
	})

	// Create application with options
	err := wails.Run(&options.App{
		Title:  "Multipurpose App",
		Width:  1024,
		Height: 768,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		log.Fatal(err)
	}
}
