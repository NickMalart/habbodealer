package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"runtime"
	"time"

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
	defer func() {
		if r := recover(); r != nil {
			f, _ := os.OpenFile("crash.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if f != nil {
				now := time.Now().Format("2006-01-02 15:04:05")
				stack := make([]byte, 4096)
				stack = stack[:runtime.Stack(stack, false)]
				_, _ = f.WriteString(fmt.Sprintf("[%s] PANIC: %v\n%s\n", now, r, stack))
				f.Close()
			}
			time.Sleep(time.Second)
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
		OnShutdown:       app.shutdown,
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
		f, _ := os.OpenFile("crash.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if f != nil {
			now := time.Now().Format("2006-01-02 15:04:05")
			_, _ = f.WriteString(fmt.Sprintf("[%s] Wails Run Error: %v\n", now, err))
			f.Close()
		}
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
