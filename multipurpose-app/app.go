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
	ctx         context.Context
	ext         *g.Ext
	gearthStatus string
	gearthHost   string
	gearthPort   int
}

// NewApp creates a new App application struct
func NewApp(ext *g.Ext) *App {
	return &App{
		ext:         ext,
		gearthStatus: "disconnected",
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
		a.gearthStatus = "initialized"
		if e.Connected {
			a.gearthStatus = "connected"
		}
		a.emitStatus()
	})

	a.ext.Connected(func(e g.ConnectArgs) {
		log.Printf("G-Earth connected (%s:%d)", e.Host, e.Port)
		a.gearthStatus = "connected"
		a.gearthHost = e.Host
		a.gearthPort = e.Port
		a.emitStatus()
	})

	a.ext.Disconnected(func() {
		log.Printf("G-Earth disconnected")
		a.gearthStatus = "disconnected"
		a.emitStatus()
	})
}

func (a *App) emitStatus() {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "gearth_status", a.GetGEarthStatus())
}

// GetGEarthStatus returns the current G-Earth connection status
func (a *App) GetGEarthStatus() map[string]interface{} {
	return map[string]interface{}{
		"status": a.gearthStatus,
		"host":   a.gearthHost,
		"port":   a.gearthPort,
	}
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
