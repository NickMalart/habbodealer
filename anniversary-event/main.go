package main

import (
	"context"
	"embed"
	"log"
	"sync"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	g "xabbo.b7c.io/goearth"
)

//go:embed all:frontend/dist
var assets embed.FS

var ext = g.NewExt(g.ExtInfo{
	Title:       "Fresh Bot",
	Description: "Started fresh",
	Version:     "1.0.0",
	Author:      "Dubbo",
})

type App struct {
	ctx  context.Context
	mu   sync.Mutex
	logs []string
}

func NewApp() *App {
	return &App{
		logs: []string{"App initialized. Waiting for extension..."},
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	go a.runExt()
}

func (a *App) runExt() {
	ext.Activated(func() {
		a.addLog("Extension activated!")
	})

	ext.Run()
}

func (a *App) addLog(msg string) {
	a.mu.Lock()
	a.logs = append(a.logs, msg)
	if len(a.logs) > 100 {
		a.logs = a.logs[1:]
	}
	a.mu.Unlock()
	
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "logsUpdate", a.logs)
	}
}

func (a *App) GetLogs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.logs
}

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:  "Fresh Bot",
		Width:  400,
		Height: 500,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup: app.startup,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		log.Fatal(err)
	}
}
