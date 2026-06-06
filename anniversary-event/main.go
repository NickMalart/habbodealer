package main

import (
	"bytes"
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
	IsPresent bool
}

type App struct {
	ctx     context.Context
	ext     *g.Ext
	mu      sync.Mutex
	logs    []string
	enabled bool
	status  string

	// Simulation state
	simulating bool

	// Event State
	hammerHeld        bool
	activeTargetID    string
	consecutiveMisses int
	
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
		Version:     "1.0.5",
		Author:      "Gemini CLI",
	})

	// Register headers
	a.ext.Headers().Add("ACTIVEOBJECT_ADD", g.Header{Dir: g.In, Value: 93})
	a.ext.Headers().Add("ACTIVEOBJECT_REMOVE", g.Header{Dir: g.In, Value: 94})
	a.ext.Headers().Add("SetStuffData", g.Header{Dir: g.Out, Value: 74})
	a.ext.Headers().Add("Move", g.Header{Dir: g.Out, Value: 75})
	a.ext.Headers().Add("CarryItem", g.Header{Dir: g.Out, Value: 97})
	a.ext.Headers().Add("TREASURE_MAP", g.Header{Dir: g.In, Value: 3601})
	a.ext.Headers().Add("BACKPACK_UPDATE", g.Header{Dir: g.In, Value: 1241})
	a.ext.Headers().Add("NOTIFICATION", g.Header{Dir: g.In, Value: 680})

	a.ext.Activated(func() {
		a.ShowWindow()
		a.AddLog("Extension activated!")
	})

	// Intercept packets
	a.ext.Intercept(g.In.Id("ACTIVEOBJECT_ADD")).With(a.handleObjectAdd)
	a.ext.Intercept(g.In.Id("ACTIVEOBJECT_REMOVE")).With(a.handleObjectRemove)
	a.ext.Intercept(g.In.Id("TREASURE_MAP")).With(a.handleReward)
	a.ext.Intercept(g.In.Id("BACKPACK_UPDATE")).With(a.handleReward)
	a.ext.Intercept(g.In.Id("NOTIFICATION")).With(a.handleReward)

	// Start the logic loop
	go a.pursuitLoop()

	a.AddLog("Extension registered. Waiting for connection...")
	go a.ext.Run()
}

func (a *App) pursuitLoop() {
	for {
		time.Sleep(1000 * time.Millisecond)
		
		a.mu.Lock()
		enabled := a.enabled
		busy := a.activeTargetID != ""
		hammerHeld := a.hammerHeld
		items := make([]TargetItem, 0, len(a.roomItems))
		for _, it := range a.roomItems {
			items = append(items, it)
		}
		a.mu.Unlock()

		if !enabled || busy {
			continue
		}

		var bestTarget *TargetItem
		
		// 1. Look for Hammer if not held
		if !hammerHeld {
			for _, item := range items {
				if strings.Contains(item.Name, "hammer") {
					target := item
					bestTarget = &target
					break
				}
			}
		}

		// 2. Look for Presents if hammer held
		if bestTarget == nil && hammerHeld {
			var newestTarget *TargetItem
			for i := range items {
				if strings.Contains(items[i].Name, "present") {
					if newestTarget == nil || items[i].AddedAt.After(newestTarget.AddedAt) {
						newestTarget = &items[i]
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
	a.MoveToLoc(target)
	
	// Wait for move (5 seconds for safety)
	time.Sleep(5000 * time.Millisecond)
	
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
		a.UpdateStatus(a.getWaitingStatus())
		return
	}

	// 2. Interact
	a.UpdateStatus(fmt.Sprintf("INTERACTING WITH %s", strings.ToUpper(target.Name)))
	a.Interact(target.ID)
	
	// Give time for pickup (2 seconds)
	time.Sleep(2000 * time.Millisecond)

	a.mu.Lock()
	if strings.Contains(target.Name, "hammer") {
		a.hammerHeld = true
		a.AddLog(">>> Hammer marked as held. <<<")
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "hammerHeldUpdate", true)
		}
	} else if strings.Contains(target.Name, "present") {
		// Recovery logic: if present still exists, increment miss count
		_, stillExists := a.roomItems[target.ID]
		if stillExists {
			a.consecutiveMisses++
			a.AddLog(fmt.Sprintf("[MISS] Present %s still exists. Miss count: %d", target.ID, a.consecutiveMisses))
			if a.consecutiveMisses >= 3 {
				a.hammerHeld = false
				a.consecutiveMisses = 0
				a.AddLog(">>> [RECOVERY] Too many misses. Resetting hammer state. <<<")
				if a.ctx != nil {
					runtime.EventsEmit(a.ctx, "hammerHeldUpdate", false)
				}
			}
		} else {
			a.consecutiveMisses = 0
		}
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
	// Split by the STX delimiter (0x02)
	parts := bytes.Split(data, []byte{0x02})
	if len(parts) < 3 {
		return
	}

	id := string(parts[0])
	name := string(parts[1])
	locFull := string(parts[2]) // e.g. "SAPBIIH0.0"

	a.addObject(id, name, locFull)
}

func (a *App) addObject(id, name, locFull string) {
	isHammer := strings.Contains(name, "toby_hammer")
	isPresent := strings.Contains(name, "Manniv_present_gen")

	if !isHammer && !isPresent {
		return
	}

	// Extract coordinate part before IIH
	loc := locFull
	if iihIdx := strings.Index(locFull, "IIH"); iihIdx != -1 {
		loc = locFull[:iihIdx]
	}

	cleanName := "present"
	if isHammer {
		cleanName = "toby_hammer"
	}

	a.mu.Lock()
	a.roomItems[id] = TargetItem{
		ID:        id,
		Name:      cleanName,
		Loc:       loc,
		AddedAt:   time.Now(),
		IsPresent: isPresent,
	}
	a.mu.Unlock()

	a.AddLog(fmt.Sprintf("[ROOM] Added %s (ID: %s) at EncodedLoc: %s", cleanName, id, loc))
}

func (a *App) removeObject(id string) {
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

func (a *App) CarryItem(itemID string) {
	if a.ext == nil {
		return
	}
	// Manual payload for CarryItem (usually item ID as string)
	payload := []byte(itemID)
	a.AddLog(fmt.Sprintf("Sending CarryItem: %s", itemID))
	a.ext.Send(g.Out.Id("CarryItem"), payload)
}

func (a *App) SimulateDrop() {
	a.mu.Lock()
	if a.simulating {
		a.mu.Unlock()
		return
	}
	a.simulating = true
	// Ensure we start with no hammer in sim
	a.hammerHeld = false
	a.consecutiveMisses = 0
	a.mu.Unlock()

	a.AddLog("--- DETAILED SIMULATION STARTED ---")
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "simulatingUpdate", true)
		runtime.EventsEmit(a.ctx, "hammerHeldUpdate", false)
	}
	
	go func() {
		defer func() {
			a.mu.Lock()
			a.simulating = false
			a.mu.Unlock()
			a.AddLog("--- SIMULATION COMPLETED ---")
			if a.ctx != nil {
				runtime.EventsEmit(a.ctx, "simulatingUpdate", false)
			}
		}()

		// 1. Hammer Drop & Walk
		hammerID := "sim_hammer"
		hammerLoc := "SAPB"
		a.AddLog(fmt.Sprintf("[SIM] Hammer appeared at %s. Walking to spot...", hammerLoc))
		a.addObject(hammerID, "toby_hammer", hammerLoc+"IIH0.0")
		
		// Explicit move in simulation
		a.MoveToLoc(TargetItem{ID: hammerID, Loc: hammerLoc, Name: "toby_hammer"})
		time.Sleep(4 * time.Second)

		// Pickup
		a.AddLog("[SIM] Picking up Toby Hammer...")
		a.Interact(hammerID)
		time.Sleep(1 * time.Second)
		
		// Simulate holding
		a.AddLog("[SIM] Now holding Toby Hammer (simulating hand object)...")
		a.CarryItem("4342") // Item ID for Toby Hammer
		
		a.mu.Lock()
		a.hammerHeld = true
		a.mu.Unlock()
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "hammerHeldUpdate", true)
		}
		a.removeObject(hammerID)
		time.Sleep(2 * time.Second)

		// 2. Present Drops & Walks
		ids := []string{"sim_p1", "sim_p2"}
		locs := []string{"RASE", "KQA"}
		
		for i, id := range ids {
			a.mu.Lock()
			if !a.simulating { a.mu.Unlock(); return }
			a.mu.Unlock()

			a.AddLog(fmt.Sprintf("[SIM] Present %d appeared at %s", i+1, locs[i]))
			a.addObject(id, "Manniv_present_gen", locs[i]+"IIH0.0")
			
			a.AddLog(fmt.Sprintf("[SIM] Walking next to present %d...", i+1))
			// Use IsPresent=true to trigger adjacency walk
			a.MoveToLoc(TargetItem{ID: id, Loc: locs[i], Name: "present", IsPresent: true})
			time.Sleep(4 * time.Second)

			a.AddLog(fmt.Sprintf("[SIM] Opening present %d...", i+1))
			a.Interact(id)
			time.Sleep(2 * time.Second)
			
			a.removeObject(id)
			time.Sleep(1 * time.Second)
		}
	}()
}

func (a *App) StopSimulation() {
	a.mu.Lock()
	a.simulating = false
	a.mu.Unlock()
	a.AddLog("Simulation stopped manually.")
}

func (a *App) handleObjectRemove(e *g.Intercept) {
	packetStr := string(e.Packet.Data)
	id := ""
	for i := 0; i < len(packetStr)-8; i++ {
		if packetStr[i] >= '0' && packetStr[i] <= '9' {
			isDigit := true
			for j := 1; j < 9; j++ {
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
	}

	if id != "" {
		a.removeObject(id)
	}
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

func (a *App) MoveToLoc(target TargetItem) {
	if a.ext == nil {
		return
	}
	
	locStr := target.Loc
	targetLoc := locStr
	if target.IsPresent && len(locStr) > 0 {
		// Try to walk to an adjacent tile for presents
		// Decrement the first char to step one tile away
		firstChar := locStr[0]
		if firstChar > 35 { 
			targetLoc = string(firstChar-1) + locStr[1:]
		} else if len(locStr) > 1 {
			// If first char is too small, try changing second
			targetLoc = string(firstChar) + string(locStr[1]+1) + locStr[2:]
		}
	}

	// Manual prefix Su and suffix H as seen in working Origins packets
	payload := append([]byte("Su"), []byte(targetLoc)...)
	payload = append(payload, 'H')
	
	a.AddLog(fmt.Sprintf("Sending Move: %s (Hex: %s)", string(payload), hex.EncodeToString(payload)))
	a.ext.Send(g.Out.Id("Move"), payload)
}

func (a *App) Interact(id string) {
	if a.ext == nil {
		return
	}
	// Manual prefix AJ as seen in verified Hex: 41 4a 40 49...
	payload := []byte("AJ@I" + id + "@A0")
	a.AddLog(fmt.Sprintf("Sending Interact: %s (Hex: %s)", string(payload), hex.EncodeToString(payload)))
	a.ext.Send(g.Out.Id("SetStuffData"), payload)
}

func (a *App) handleReward(e *g.Intercept) {
	a.AddLog(fmt.Sprintf("[REWARD] Received packet (ID %d): %s", e.Packet.Header.Value, string(e.Packet.Data)))
}

func (a *App) ToggleEvent(enabled bool) {
	a.mu.Lock()
	a.enabled = enabled
	a.activeTargetID = ""
	a.consecutiveMisses = 0
	if !enabled {
		a.simulating = false // Force stop simulation
		a.hammerHeld = false
		a.roomItems = make(map[string]TargetItem)
		a.mu.Unlock()
		a.UpdateStatus("IDLE")
		a.AddLog("Bot DISABLED. Simulation and pursuit stopped.")
	} else {
		held := a.hammerHeld
		a.mu.Unlock()
		if held {
			a.UpdateStatus("WAITING FOR PRESENTS")
		} else {
			a.UpdateStatus("WAITING FOR HAMMER")
		}
		a.AddLog("Bot ENABLED.")
	}
}

func (a *App) ResetHammer() {
	a.mu.Lock()
	a.hammerHeld = false
	a.activeTargetID = ""
	enabled := a.enabled
	a.mu.Unlock()
	a.AddLog("Hammer state reset manually.")
	if enabled {
		a.UpdateStatus("WAITING FOR HAMMER")
	}
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "hammerHeldUpdate", false)
	}
}

func (a *App) SetHammerHeld(held bool) {
	a.mu.Lock()
	a.hammerHeld = held
	enabled := a.enabled
	a.mu.Unlock()
	
	msg := "Hammer marked as NOT HELD."
	if held {
		msg = "Hammer marked as HELD manually."
	}
	a.AddLog(msg)
	
	if enabled {
		a.UpdateStatus(a.getWaitingStatus())
	}
	
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "hammerHeldUpdate", held)
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
