package main

import (
	"context"
	"fmt"
	"log"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	g "xabbo.b7c.io/goearth"
)

// App struct
type App struct {
	ctx context.Context
	ext *g.Ext
}

// NewApp creates a new App application struct
func NewApp(ext *g.Ext) *App {
	return &App{
		ext: ext,
	}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.setupExt()
	go a.runExt()
}

func (a *App) setupExt() {
	a.ext.Initialized(func(e g.InitArgs) {
		log.Printf("G-Earth initialized (connected=%t)", e.Connected)
		runtime.EventsEmit(a.ctx, "gearth_status", map[string]interface{}{
			"status":    "initialized",
			"connected": e.Connected,
		})
	})

	a.ext.Connected(func(e g.ConnectArgs) {
		log.Printf("G-Earth connected (%s:%d)", e.Host, e.Port)
		runtime.EventsEmit(a.ctx, "gearth_status", map[string]interface{}{
			"status": "connected",
			"host":   e.Host,
			"port":   e.Port,
		})
	})

	a.ext.Disconnected(func() {
		log.Printf("G-Earth disconnected")
		runtime.EventsEmit(a.ctx, "gearth_status", map[string]interface{}{
			"status": "disconnected",
		})
	})
}

func (a *App) runExt() {
	a.ext.Run()
}

// ShowWindow shows the application window
func (a *App) ShowWindow() {
	if a.ctx != nil {
		runtime.WindowShow(a.ctx)
	}
}

// Greet returns a greeting for the given name
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}
