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
	status  string

	// Event State
	hammerHeld     bool
	activeTargetID string
}

func NewApp() *App {
	return &App{
		logs:   []string{"Anniversary Bot initialized..."},
		status: "IDLE",
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	a.ext = g.NewExt(g.ExtInfo{
		Title:       "Anniversary Bot",
		Description: "Automates Toby Hammer and Presents",
		Version:     "1.0.2",
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

func (a *App) UpdateStatus(status string) {
	a.mu.Lock()
	a.status = status
	a.mu.Unlock()
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "statusUpdate", status)
	}
}

func (a *App) GetStatus() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status
}

func (a *App) GetHammerHeld() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.hammerHeld
}

func (a *App) handleObjectAdd(e *g.Intercept) {
	a.mu.Lock()
	enabled := a.enabled
	hammerHeld := a.hammerHeld
	busy := a.activeTargetID != ""
	a.mu.Unlock()

	if !enabled {
		return
	}

	data := e.Packet.Data
	packetStr := string(data)
	
	// Check for targets first
	isHammer := strings.Contains(packetStr, "toby_hammer")
	isPresent := strings.Contains(packetStr, "Manniv_present_gen")

	if isHammer || isPresent {
		a.AddLog(fmt.Sprintf("[DEBUG] Incoming Object Packet (Len: %d): %s", len(data), packetStr))
	}

	if busy {
		return
	}
	
	if isHammer && !hammerHeld {
		a.AddLog(">>> TOBY HAMMER DETECTED! <<<")
		go a.processObject(packetStr, "toby_hammer")
	} else if isPresent && hammerHeld {
		a.AddLog(">>> ANNIVERSARY PRESENT DETECTED! <<<")
		go a.processObject(packetStr, "Manniv_present_gen")
	}
}

func (a *App) handleObjectRemove(e *g.Intercept) {
	packetStr := string(e.Packet.Data)
	
	a.mu.Lock()
	targetID := a.activeTargetID
	a.mu.Unlock()

	if strings.Contains(packetStr, "toby_hammer") || strings.Contains(packetStr, "Manniv_present_gen") {
		a.AddLog(fmt.Sprintf("[DEBUG] Object Removed: %s", packetStr))
	}
	
	if targetID != "" && strings.Contains(packetStr, targetID) {
		a.AddLog(fmt.Sprintf("Target ID %s removed from room. Resetting search.", targetID))
		a.mu.Lock()
		a.activeTargetID = ""
		enabled := a.enabled
		held := a.hammerHeld
		a.mu.Unlock()
		
		if enabled {
			if held {
				a.UpdateStatus("WAITING FOR PRESENTS")
			} else {
				a.UpdateStatus("WAITING FOR HAMMER")
			}
		}
	}
}

func (a *App) processObject(packetStr string, targetName string) {
	// Find index of targetName
	nameIdx := strings.Index(packetStr, targetName)
	if nameIdx == -1 {
		a.AddLog(fmt.Sprintf("[ERROR] %s found in Contains but not in Index search?", targetName))
		return
	}
	a.AddLog(fmt.Sprintf("[DEBUG] Found '%s' at index %d", targetName, nameIdx))
	
	locIdx := -1
	searchMethod := ""
	if dotIdx := strings.LastIndex(packetStr, "1.0"); dotIdx != -1 {
		locIdx = dotIdx - 7
		searchMethod = "1.0 offset"
	} else if dotIdx := strings.LastIndex(packetStr, ".0"); dotIdx != -1 {
		locIdx = dotIdx - 8
		searchMethod = ".0 offset"
	}

	if locIdx < 0 || locIdx+4 >= len(packetStr) {
		if iihIdx := strings.LastIndex(packetStr, "IIH"); iihIdx != -1 {
			locIdx = iihIdx - 4
			searchMethod = "IIH offset"
		}
	}
	
	if locIdx < 0 || locIdx+4 >= len(packetStr) {
		a.AddLog(fmt.Sprintf("[ERROR] Could not parse location string. Packet tail: %q", packetStr[max(0, len(packetStr)-20):]))
		return
	}
	
	locStr := packetStr[locIdx : locIdx+4]
	a.AddLog(fmt.Sprintf("[DEBUG] Extracted LocString %q via %s", locStr, searchMethod))
	
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
		a.AddLog("[ERROR] Could not find 9-digit ID in packet.")
		return
	}
	
	a.mu.Lock()
	a.activeTargetID = id
	a.mu.Unlock()
	
	a.AddLog(fmt.Sprintf("Pursuing %s (ID: %s) at Coords [%d, %d]", targetName, id, int(locStr[0])-64, int(locStr[2])-64))
	
	// 1. Move
	a.UpdateStatus(fmt.Sprintf("MOVING TO %s", strings.ToUpper(targetName)))
	a.MoveToLoc(locStr)
	
	// Wait for move to complete
	time.Sleep(1200 * time.Millisecond)
	
	// Check if target is still active
	a.mu.Lock()
	stillActive := a.activeTargetID == id
	a.mu.Unlock()
	
	if !stillActive {
		a.AddLog(fmt.Sprintf("[ABORT] Target %s (ID: %s) was removed during movement.", targetName, id))
		return
	}
	
	// 2. Interact
	a.UpdateStatus(fmt.Sprintf("OPENING %s", strings.ToUpper(targetName)))
	a.Interact(id)
	time.Sleep(1000 * time.Millisecond)
	
	// If it was the hammer, mark it as held
	if targetName == "toby_hammer" {
		a.mu.Lock()
		a.hammerHeld = true
		a.activeTargetID = ""
		a.mu.Unlock()
		a.AddLog(">>> SUCCESS: Hammer Picked Up! <<<")
		a.UpdateStatus("WAITING FOR PRESENTS")
	} else {
		a.mu.Lock()
		a.activeTargetID = ""
		a.mu.Unlock()
		a.AddLog(fmt.Sprintf(">>> SUCCESS: Opened %s <<<", targetName))
		a.UpdateStatus("WAITING FOR PRESENTS")
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (a *App) MoveToLoc(locStr string) {
	if a.ext == nil {
		return
	}
	payload := "Su" + locStr + "H"
	a.AddLog(fmt.Sprintf("Sending Move: %s", payload))
	a.ext.Send(g.Out.Id("ORIGINS_MOVE"), []byte(payload))
}

func (a *App) Interact(id string) {
	if a.ext == nil {
		return
	}
	payload := fmt.Sprintf("AJ @I%s@A0", id)
	a.ext.Send(g.Out.Id("SETSTUFFDATA"), []byte(payload))
}

func (a *App) ToggleEvent(enabled bool) {
	a.mu.Lock()
	a.enabled = enabled
	a.activeTargetID = ""
	if !enabled {
		a.hammerHeld = false
		a.mu.Unlock()
		a.UpdateStatus("IDLE")
	} else {
		held := a.hammerHeld
		a.mu.Unlock()
		if held {
			a.UpdateStatus("WAITING FOR PRESENTS")
		} else {
			a.UpdateStatus("WAITING FOR HAMMER")
		}
	}
	
	status := "Enabled"
	if !enabled {
		status = "Disabled"
	}
	a.AddLog(fmt.Sprintf("Anniversary Bot %s", status))
}

func (a *App) ResetHammer() {
	a.mu.Lock()
	a.hammerHeld = false
	a.activeTargetID = ""
	enabled := a.enabled
	a.mu.Unlock()
	a.AddLog("Hammer state reset.")
	if enabled {
		a.UpdateStatus("WAITING FOR HAMMER")
	}
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
