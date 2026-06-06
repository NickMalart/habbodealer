package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"strings"
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
	enabled bool

	// Event State
	hammerHeld bool
}

func NewApp() *App {
	return &App{
		logs: []string{"Anniversary Bot initialized..."},
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	a.ext = g.NewExt(g.ExtInfo{
		Title:       "Anniversary Bot",
		Description: "Automates Toby Hammer and Presents",
		Version:     "1.0.0",
		Author:      "Gemini CLI",
	})

	// Register headers
	a.ext.Headers().Add("ACTIVEOBJECT_ADD", g.Header{Dir: g.In, Value: 93})
	a.ext.Headers().Add("ACTIVEOBJECT_REMOVE", g.Header{Dir: g.In, Value: 94})
	a.ext.Headers().Add("SETSTUFFDATA", g.Header{Dir: g.Out, Value: 74})
	a.ext.Headers().Add("ORIGINS_MOVE", g.Header{Dir: g.Out, Value: 1269})

	a.ext.Activated(func() {
		a.ShowWindow()
		a.AddLog("Extension activated!")
	})

	// Intercept ACTIVEOBJECT_ADD
	a.ext.Intercept(g.In.Id("ACTIVEOBJECT_ADD")).With(a.handleObjectAdd)
	a.ext.Intercept(g.In.Id("ACTIVEOBJECT_REMOVE")).With(a.handleObjectRemove)

	a.AddLog("Extension registered. Waiting for connection...")
	go a.ext.Run()
}

func (a *App) handleObjectAdd(e *g.Intercept) {
	a.mu.Lock()
	enabled := a.enabled
	hammerHeld := a.hammerHeld
	a.mu.Unlock()

	if !enabled {
		return
	}

	// Packet structure for 93: [ID][Name][...][LocationString]
	// We need to parse this carefully.
	data := e.Packet.Data
	
	// ID is 9 digits, usually encoded as string or VL64? 
	// Snapshot says "a 9-digit unique ID"
	// Let's assume it's string for now if it's easy to find.
	// Actually, let's use a simpler approach: scan the data for known names.
	
	packetStr := string(data)
	
	if strings.Contains(packetStr, "toby_hammer") && !hammerHeld {
		a.AddLog(">>> TOBY HAMMER DETECTED! <<<")
		go a.processObject(packetStr, "toby_hammer")
	} else if strings.Contains(packetStr, "Manniv_present_gen") && hammerHeld {
		a.AddLog(">>> ANNIVERSARY PRESENT DETECTED! <<<")
		go a.processObject(packetStr, "Manniv_present_gen")
	}
}

func (a *App) handleObjectRemove(e *g.Intercept) {
	// Optional: track if hammer was picked up
}

func (a *App) processObject(packetStr string, targetName string) {
	// Find index of targetName
	nameIdx := strings.Index(packetStr, targetName)
	if nameIdx == -1 {
		return
	}
	
	locIdx := -1
	// Look for a pattern like [Char][Char][Char][Char]IIH or similar
	// Location string is usually preceded by a delimiter or fixed offset
	if dotIdx := strings.LastIndex(packetStr, "1.0"); dotIdx != -1 {
		locIdx = dotIdx - 7
	} else if dotIdx := strings.LastIndex(packetStr, ".0"); dotIdx != -1 {
		locIdx = dotIdx - 8
	}

	if locIdx < 0 || locIdx+4 >= len(packetStr) {
		// Fallback: look for IIH
		if iihIdx := strings.LastIndex(packetStr, "IIH"); iihIdx != -1 {
			locIdx = iihIdx - 4
		}
	}
	
	if locIdx < 0 || locIdx+4 >= len(packetStr) {
		a.AddLog("ERROR: Could not parse location string")
		return
	}
	
	locStr := packetStr[locIdx : locIdx+4] // Take the first 4 chars (X, padding, Y, padding)
	x := int(locStr[0]) - 64
	y := int(locStr[2]) - 64
	
	a.AddLog(fmt.Sprintf("Target %s found at (%d, %d) [Encoded: %s]", targetName, x, y, locStr))
	
	// Extract ID
	var id string
	for i := 0; i < len(packetStr)-8; i++ {
		isDigit := true
		for j := 0; j < 9; j++ {
			if packetStr[i+j] < '0' || packetStr[i+j] > '9' {
				isDigit = false
				break
			}
		}
		if isDigit {
			id = packetStr[i : i+9]
			break
		}
	}
	
	if id == "" {
		a.AddLog("ERROR: Could not find 9-digit ID")
		return
	}
	
	a.AddLog(fmt.Sprintf("Interacting with ID: %s", id))
	
	// 1. Move
	a.MoveToLoc(locStr)
	time.Sleep(800 * time.Millisecond)
	
	// 2. Interact
	a.Interact(id)
	
	if targetName == "toby_hammer" {
		a.mu.Lock()
		a.hammerHeld = true
		a.mu.Unlock()
		a.AddLog("Hammer marked as held. Now watching for presents.")
	}
}

func (a *App) MoveToLoc(locStr string) {
	if a.ext == nil {
		return
	}
	// Move payload: Su[4 chars]H
	payload := "Su" + locStr + "H"
	a.AddLog(fmt.Sprintf("Sending Move payload: %s", payload))
	a.ext.Send(g.Out.Id("ORIGINS_MOVE"), []byte(payload))
}

func (a *App) Interact(id string) {
	if a.ext == nil {
		return
	}
	
	// AJ @I[ID]@A0
	// @I is length 9
	// @A is 1
	payload := fmt.Sprintf("AJ @I%s@A0", id)
	a.AddLog(fmt.Sprintf("Interacting with %s", id))
	a.ext.Send(g.Out.Id("SETSTUFFDATA"), []byte(payload))
}

func (a *App) ToggleEvent(enabled bool) {
	a.mu.Lock()
	a.enabled = enabled
	if !enabled {
		a.hammerHeld = false
	}
	a.mu.Unlock()
	
	status := "Enabled"
	if !enabled {
		status = "Disabled"
	}
	a.AddLog(fmt.Sprintf("Anniversary Bot %s", status))
}

func (a *App) ResetHammer() {
	a.mu.Lock()
	a.hammerHeld = false
	a.mu.Unlock()
	a.AddLog("Hammer state reset.")
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

func main() {
	app := NewApp()
	err := wails.Run(&options.App{
		Title:  "Anniversary Bot",
		Width:  400,
		Height: 600,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup: app.startup,
		StartHidden:       true,
		HideWindowOnClose: true,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
