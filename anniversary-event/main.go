package main

import (
	"context"
	"embed"
	"encoding/hex"
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

type TargetItem struct {
	ID        string
	Name      string
	Loc       string
	AddedAt   time.Time
}

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
	
	// Room State
	roomItems map[string]TargetItem
}

func NewApp() *App {
	return &App{
		logs:      []string{"Anniversary Bot initialized..."},
		status:    "IDLE",
		roomItems: make(map[string]TargetItem),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	a.ext = g.NewExt(g.ExtInfo{
		Title:       "Anniversary Bot",
		Description: "Automates Toby Hammer and Presents",
		Version:     "1.0.4",
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

	// Intercept packets
	a.ext.Intercept(g.In.Id("ACTIVEOBJECT_ADD")).With(a.handleObjectAdd)
	a.ext.Intercept(g.In.Id("ACTIVEOBJECT_REMOVE")).With(a.handleObjectRemove)

	// Start the logic loop
	go a.pursuitLoop()

	a.AddLog("Extension registered. Waiting for connection...")
	go a.ext.Run()
}

func (a *App) pursuitLoop() {
	for {
		time.Sleep(500 * time.Millisecond)
		
		a.mu.Lock()
		enabled := a.enabled
		busy := a.activeTargetID != ""
		hammerHeld := a.hammerHeld
		items := a.roomItems
		a.mu.Unlock()

		if !enabled || busy {
			continue
		}

		// 1. Look for Hammer if not held
		var bestTarget *TargetItem
		if !hammerHeld {
			for _, item := range items {
				if strings.Contains(item.Name, "hammer") {
					bestTarget = &item
					break // Take any hammer immediately
				}
			}
		}

		// 2. Look for Presents if hammer held (or no hammer found)
		if bestTarget == nil && hammerHeld {
			var newestTarget *TargetItem
			for _, item := range items {
				if strings.Contains(item.Name, "present") {
					if newestTarget == nil || item.AddedAt.After(newestTarget.AddedAt) {
						newestTarget = &item
					}
				}
			}
			bestTarget = newestTarget
		}

		if bestTarget != nil {
			a.mu.Lock()
			a.activeTargetID = bestTarget.ID
			a.mu.Unlock()
			go a.executePursuit(*bestTarget)
		}
	}
}

func (a *App) executePursuit(target TargetItem) {
	a.AddLog(fmt.Sprintf(">>> PURSUING %s (ID: %s) <<<", strings.ToUpper(target.Name), target.ID))
	
	// 1. Move
	a.UpdateStatus(fmt.Sprintf("MOVING TO %s", strings.ToUpper(target.Name)))
	a.MoveToLoc(target.Loc)
	
	// Wait for move (2.5 seconds for safety)
	time.Sleep(2500 * time.Millisecond)
	
	// Check if still valid
	a.mu.Lock()
	_, stillExists := a.roomItems[target.ID]
	currentActiveID := a.activeTargetID
	a.mu.Unlock()
	
	if !stillExists || currentActiveID != target.ID {
		a.AddLog(fmt.Sprintf("[ABORT] %s is no longer available.", target.Name))
		a.mu.Lock()
		a.activeTargetID = ""
		a.mu.Unlock()
		return
	}

	// 2. Interact
	a.UpdateStatus(fmt.Sprintf("INTERACTING WITH %s", strings.ToUpper(target.Name)))
	a.Interact(target.ID)
	
	// Give time for pickup (1.5 seconds)
	time.Sleep(1500 * time.Millisecond)

	a.mu.Lock()
	if strings.Contains(target.Name, "hammer") {
		// We don't mark hammerHeld here yet, we wait to see if it gets REMOVED or we assume success
		// For now, let's assume if we sent the packet and it's removed, it worked.
		// Actually, let's just mark it held if we finished the interaction.
		a.hammerHeld = true
		a.AddLog(">>> Hammer marked as held. <<<")
	}
	a.activeTargetID = ""
	a.mu.Unlock()
	
	a.UpdateStatus(a.getWaitingStatus())
}

func (a *App) getWaitingStatus() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.enabled { return "IDLE" }
	if a.hammerHeld { return "WAITING FOR PRESENTS" }
	return "WAITING FOR HAMMER"
}

func (a *App) handleObjectAdd(e *g.Intercept) {
	data := e.Packet.Data
	packetStr := string(data)
	
	isHammer := strings.Contains(packetStr, "toby_hammer")
	isPresent := strings.Contains(packetStr, "Manniv_present_gen")

	if !isHammer && !isPresent {
		return
	}

	id := a.extractID(packetStr)
	loc := a.extractLoc(packetStr)
	name := "item"
	if isHammer { name = "toby_hammer" } else { name = "present" }

	if id != "" && loc != "" {
		a.mu.Lock()
		a.roomItems[id] = TargetItem{
			ID:      id,
			Name:    name,
			Loc:     loc,
			AddedAt: time.Now(),
		}
		a.mu.Unlock()
		a.AddLog(fmt.Sprintf("[ROOM] Added %s (ID: %s) at Coords [%d, %d]", name, id, int(loc[0])-64, int(loc[2])-64))
	}
}

func (a *App) handleObjectRemove(e *g.Intercept) {
	packetStr := string(e.Packet.Data)
	
	// Extract ID from remove packet (usually just the ID string)
	id := ""
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

	if id != "" {
		a.mu.Lock()
		_, exists := a.roomItems[id]
		if exists {
			delete(a.roomItems, id)
			a.AddLog(fmt.Sprintf("[ROOM] Removed ID %s", id))
			if a.activeTargetID == id {
				a.activeTargetID = ""
				a.AddLog("Current target removed. Aborting pursuit.")
			}
		}
		a.mu.Unlock()
	}
}

func (a *App) extractID(packetStr string) string {
	for i := 0; i < len(packetStr)-8; i++ {
		isDigit := true
		for j := 0; j < 9; j++ {
			if packetStr[i+j] < '0' || packetStr[i+j] > '9' {
				isDigit = false
				break
			}
		}
		if isDigit {
			return packetStr[i : i+9]
		}
	}
	return ""
}

func (a *App) extractLoc(packetStr string) string {
	locIdx := -1
	if dotIdx := strings.LastIndex(packetStr, "1.0"); dotIdx != -1 {
		locIdx = dotIdx - 7
	} else if dotIdx := strings.LastIndex(packetStr, ".0"); dotIdx != -1 {
		locIdx = dotIdx - 8
	}
	if locIdx < 0 || locIdx+4 >= len(packetStr) {
		if iihIdx := strings.LastIndex(packetStr, "IIH"); iihIdx != -1 {
			locIdx = iihIdx - 4
		}
	}
	if locIdx >= 0 && locIdx+4 <= len(packetStr) {
		return packetStr[locIdx : locIdx+4]
	}
	return ""
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

func (a *App) MoveToLoc(locStr string) {
	if a.ext == nil {
		return
	}
	payload := []byte(locStr + "H")
	a.AddLog(fmt.Sprintf("Sending Move: %s (Hex: %s)", string(payload), hex.EncodeToString(payload)))
	a.ext.Send(g.Out.Id("ORIGINS_MOVE"), payload)
}

func (a *App) Interact(id string) {
	if a.ext == nil {
		return
	}
	payload := []byte("AJ @I" + id + "@A0")
	a.AddLog(fmt.Sprintf("Sending Interact: %s (Hex: %s)", string(payload), hex.EncodeToString(payload)))
	a.ext.Send(g.Out.Id("SETSTUFFDATA"), payload)
}

func (a *App) ToggleEvent(enabled bool) {
	a.mu.Lock()
	a.enabled = enabled
	a.activeTargetID = ""
	if !enabled {
		a.hammerHeld = false
		a.roomItems = make(map[string]TargetItem)
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
