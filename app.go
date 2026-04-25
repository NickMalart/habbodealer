package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	g "xabbo.b7c.io/goearth"
)

//go:embed all:frontend/dist
var assets embed.FS

var ext = g.NewExt(g.ExtInfo{
	Title:       "[RO] All In One Dealer",
	Description: "A tool to assist with card games in RO, including poker and blackjack.",
	Version:     "1.0.0",
	Author:      "Dubbo",
})

var (
	CurrentVersion = "1.0.0"
)

var app *App

func (a *App) GetCurrentVersion() string {
	return CurrentVersion
}

func main() {
	// Redirect stdout/stderr early so runtime panics and prints go to a log file.
	var crashLog *os.File
	if f, err := os.OpenFile("crash_runtime.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
		crashLog = f
		os.Stdout = f
		os.Stderr = f
		log.SetOutput(f)
	} else {
		log.Printf("failed to open crash log: %v", err)
	}

	// Capture panics in main goroutine and write stack to the crash log.
	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 1<<20)
			n := runtime.Stack(buf, true)
			if crashLog != nil {
				fmt.Fprintf(crashLog, "panic: %v\n%s\n", r, buf[:n])
				crashLog.Sync()
				crashLog.Close()
			} else {
				fmt.Printf("panic: %v\n%s\n", r, buf[:n])
			}
		}
	}()

	app = NewApp(ext, assets)
	setupExt()
	err := wails.Run(&options.App{
		Title:  "[RO] All In One Dealer by JTD",
		Width:  500,
		Height: 890,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 44, G: 62, B: 80, A: 1},
		OnStartup:        app.startup,
		Bind: []interface{}{
			app,
		},
		StartHidden:       true,
		HideWindowOnClose: true,
		DisableResize:     true,
		MinWidth:          500,
		MaxWidth:          500,
		MinHeight:         890,
		MaxHeight:         890,
	})

	if err != nil {
		log.Fatal(err)
	}
}

func setupExt() {
	ext.Initialized(func(e g.InitArgs) {
		log.Printf("initialized (connected=%t)", e.Connected)
	})

	ext.Activated(func() {
		log.Printf("activated")
		app.ShowWindow()
	})

	ext.Connected(func(e g.ConnectArgs) {
		log.Printf("connected (%s:%d)", e.Host, e.Port)
		log.Printf("client %s (%s)", e.Client.Identifier, e.Client.Version)
	})

	ext.Disconnected(func() {
		log.Printf("connection lost")
	})
}
