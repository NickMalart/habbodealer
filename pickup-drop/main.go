package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	g "xabbo.b7c.io/goearth"
)

//go:embed all:frontend/dist
var assets embed.FS

type App struct {
	ctx     context.Context
	ext     *g.Ext
	mu      sync.Mutex
	logs    []string
	running bool
}

func NewApp() *App {
	return &App{
		logs: []string{"Pickup-Drop initialized..."},
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	a.ext = g.NewExt(g.ExtInfo{
		Title:       "Pickup-Drop",
		Description: "Pickup and Drop items automatically",
		Version:     "1.0.0",
		Author:      "Gemini CLI",
	})

	// Register PICK_ALL header
	a.ext.Headers().Add("PICK_ALL", g.Header{Dir: g.Out, Value: 401})

	a.ext.Activated(func() {
		a.ShowWindow()
	})

	a.AddLog("Extension registered. Waiting for connection...")
	go a.ext.Run()
}

func (a *App) ShowWindow() {
	if a.ctx != nil {
		runtime.WindowShow(a.ctx)
	}
}

func (a *App) AddLog(msg string) {
	fullMsg := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), msg)
	log.Println(fullMsg)
	a.mu.Lock()
	a.logs = append(a.logs, fullMsg)
	if len(a.logs) > 50 {
		a.logs = a.logs[len(a.logs)-50:]
	}
	logsCopy := make([]string, len(a.logs))
	copy(logsCopy, a.logs)
	a.mu.Unlock()

	if a.ctx != nil {
		go runtime.EventsEmit(a.ctx, "logsUpdate", logsCopy)
	}
}

func (a *App) GetLogs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.logs
}

// ExecuteCommands is the placeholder for the button action
func (a *App) ExecuteCommands() {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		a.AddLog("Commands already running...")
		return
	}
	a.running = true
	a.mu.Unlock()

	go func() {
		defer func() {
			a.mu.Lock()
			a.running = false
			a.mu.Unlock()
		}()

		a.AddLog("Starting command execution...")

		if a.ext == nil {
			a.AddLog("ERROR: Extension not initialized")
			return
		}

		// Rollorigins123![PICK_ALL][401]
		// Outgoing[401] -> FQa[123]aMK
		// Hex: 46 51 61 7b 61 4d 4b
		a.AddLog("Sending PICK_ALL packet (401)...")

		// We use SendPacket with the exact bytes. 
		// Header 401 (FQ) + Data "a{aMK"
		packet := &g.Packet{
			Header: g.Header{Dir: g.Out, Value: 401},
			Data:   []byte{0x61, 0x7b, 0x61, 0x4d, 0x4b},
			Client: g.Shockwave,
		}
		a.ext.SendPacket(packet)

		a.AddLog("Packet sent. Command execution finished.")
	}()
}

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:  "Pickup-Drop",
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
