package main

import (
	"context"
	"embed"
	"encoding/hex"
	"fmt"
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
	myX, myY int
}

func NewApp() *App {
	return &App{
		logs: []string{"App initialized. Waiting for extension..."},
		myX: 18,
		myY: 20,
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	runtime.WindowShow(ctx)
	go a.runExt()
}

func (a *App) runExt() {
	ext.Activated(func() {
		if a.ctx != nil {
			runtime.WindowShow(a.ctx)
		}
		a.addLog("Extension activated!")
	})

	// Register Headers
	ext.Headers().Add("ACTIVEOBJECT_ADD", g.Header{Dir: g.In, Value: 93})
	ext.Headers().Add("MOVE", g.Header{Dir: g.Out, Value: 1269})

	ext.Run()
}

func (a *App) RunSimulation() {
	// Hammer packet: ID 900000001, loc QCSE -> (16, 22)
	packetHex := "415d393030303030303031024d746f62795f68616d6d65720251435345494948312e3002302c302c300202483002484d4d"
	data, err := hex.DecodeString(packetHex)
	if err != nil {
		a.addLog(fmt.Sprintf("Error decoding packet: %v", err))
		return
	}

	a.addLog("📥 [SIM] Hammer appeared at (16, 22)")
	
	// Send to client
	ext.Send(g.In.Id("ACTIVEOBJECT_ADD"), data[2:])

	// Execute Walk immediately to (16, 22)
	tx, ty := 16, 22
	a.mu.Lock()
	mx, my := a.myX, a.myY
	a.mu.Unlock()

	moveData := []byte{
		byte(tx + 63),
		byte(ty + 45),
		byte(mx + 65),
		byte(my + 49),
		'H',
	}

	a.addLog(fmt.Sprintf("🚶 [SIM] Walking to hammer (16, 22) from (%d, %d)", mx, my))
	ext.Send(g.Out.Id("MOVE"), moveData)
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
