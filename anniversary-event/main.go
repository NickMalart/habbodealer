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
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Loc       string    `json:"loc"`
	AddedAt   time.Time `json:"-"`
	IsPresent bool      `json:"isPresent"`
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

	// Phase control
	currentPhase string
	phaseTimer   *time.Timer
}

func NewApp() *App {
	return &App{
		logs:      []string{"Anniversary Bot initialized..."},
		status:    "IDLE",
		roomItems: make(map[string]TargetItem),
		currentPhase: "IDLE",
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	a.ext = g.NewExt(g.ExtInfo{
		Title:       "Anniversary Bot",
		Description: "Automates Toby Hammer and Presents",
		Version:     "1.1.5",
		Author:      "Gemini CLI",
	})

	// Register headers for Habbo Origins (Shockwave)
	a.ext.Headers().Add("STATUS", g.Header{Dir: g.In, Value: 34})
	a.ext.Headers().Add("OBJECTS", g.Header{Dir: g.In, Value: 32})
	a.ext.Headers().Add("ACTIVEOBJECT_ADD", g.Header{Dir: g.In, Value: 93})
	a.ext.Headers().Add("ACTIVEOBJECT_REMOVE", g.Header{Dir: g.In, Value: 94})
	a.ext.Headers().Add("ACTIVEOBJECT_UPDATE", g.Header{Dir: g.In, Value: 95})
	a.ext.Headers().Add("BULLETIN", g.Header{Dir: g.In, Value: 680})
	
	a.ext.Headers().Add("SetStuffData", g.Header{Dir: g.Out, Value: 74})
	a.ext.Headers().Add("Move", g.Header{Dir: g.Out, Value: 1269})
	a.ext.Headers().Add("CarryItem", g.Header{Dir: g.Out, Value: 97})
	a.ext.Headers().Add("LookTo", g.Header{Dir: g.Out, Value: 79})
	a.ext.Headers().Add("Chat", g.Header{Dir: g.Out, Value: 52})

	a.ext.Activated(func() {
		a.ShowWindow()
		a.AddLog("Extension activated!")
	})

	// Intercept packets
	a.ext.Intercept(g.In.Id("OBJECTS")).With(a.handleObjects)
	a.ext.Intercept(g.In.Id("ACTIVEOBJECT_ADD")).With(a.handleObjectAdd)
	a.ext.Intercept(g.In.Id("ACTIVEOBJECT_UPDATE")).With(a.handleObjectAdd)
	a.ext.Intercept(g.In.Id("ACTIVEOBJECT_REMOVE")).With(a.handleObjectRemove)
	a.ext.Intercept(g.In.Id("STATUS")).With(a.handleStatus)
	a.ext.Intercept(g.In.Id("BULLETIN")).With(a.handleBulletin)

	// Start the logic loop
	go a.pursuitLoop()

	a.AddLog("Extension registered. Waiting for connection...")
	go a.ext.Run()
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

func (a *App) handleObjects(e *g.Intercept) {
	data := e.Packet.Data
	parts := bytes.Split(data, []byte{0x02})
	if len(parts) < 2 { return }
	
	for i := 1; i < len(parts)-3; i += 4 {
		id := string(parts[i])
		name := string(parts[i+1])
		loc := string(parts[i+2])
		a.addObject(id, name, loc)
	}
}

func (a *App) handleObjectAdd(e *g.Intercept) {
	data := e.Packet.Data
	parts := bytes.Split(data, []byte{0x02})
	if len(parts) < 3 { return }

	id := string(parts[0])
	numericID := ""
	for i := 0; i <= len(id)-1; i++ {
		if id[i] >= '0' && id[i] <= '9' {
			start := i
			for i < len(id) && id[i] >= '0' && id[i] <= '9' { i++ }
			numericID = id[start:i]
			break
		}
	}
	if numericID != "" { id = numericID }
	
	name := string(parts[1])
	locFull := string(parts[2]) 
	a.addObject(id, name, locFull)
}

func (a *App) addObject(id, name, locFull string) {
	nameLower := strings.ToLower(name)
	isHammer := strings.Contains(nameLower, "hammer")
	isPresent := strings.Contains(nameLower, "present")

	if !isHammer && !isPresent {
		return
	}

	loc := locFull
	if iihIdx := strings.Index(locFull, "IIH"); iihIdx != -1 {
		loc = locFull[:iihIdx]
	} else if dotIdx := strings.Index(locFull, "1.0"); dotIdx != -1 {
		loc = locFull[:dotIdx]
	}
	loc = strings.TrimSpace(strings.TrimRight(loc, "\x02"))

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

	a.AddLog(fmt.Sprintf("[ROOM] Detected %s (ID: %s) at %s", cleanName, id, loc))
}

func (a *App) handleObjectRemove(e *g.Intercept) {
	data := string(e.Packet.Data)
	id := ""
	for i := 0; i <= len(data)-9; i++ {
		sub := data[i : i+9]
		isDigit := true
		for _, c := range sub {
			if c < '0' || c > '9' { isDigit = false; break }
		}
		if isDigit { id = sub; break }
	}
	if id != "" { a.removeObject(id) }
}

func (a *App) removeObject(id string) {
	a.mu.Lock()
	if _, exists := a.roomItems[id]; exists {
		delete(a.roomItems, id)
		if a.activeTargetID == id { a.activeTargetID = "" }
		a.mu.Unlock()
		a.AddLog(fmt.Sprintf("[ROOM] Removed ID %s", id))
	} else {
		a.mu.Unlock()
	}
}

func (a *App) handleStatus(e *g.Intercept) {
	// Status tracking
}

func (a *App) handleBulletin(e *g.Intercept) {
	data := string(e.Packet.Data)
	if strings.Contains(data, "Anniversary Present") {
		if strings.Contains(data, "found nothing") {
			a.AddLog("[EVENT] Opened present: Found nothing.")
		} else {
			a.AddLog("[EVENT] Opened present: Item found!")
		}
	}
}

func (a *App) pursuitLoop() {
	a.AddLog("[CORE] Pursuit loop started.")
	lastHeartbeat := time.Now()
	
	for {
		time.Sleep(1000 * time.Millisecond)
		
		a.mu.Lock()
		enabled := a.enabled
		activeID := a.activeTargetID
		hammerHeld := a.hammerHeld
		
		items := make([]TargetItem, 0, len(a.roomItems))
		for _, it := range a.roomItems { items = append(items, it) }
		a.mu.Unlock()

		if !enabled {
			continue
		}

		// Heartbeat logging every 5 seconds when idle
		if time.Since(lastHeartbeat) > 5*time.Second {
			goal := "TOBY_HAMMER"
			if hammerHeld { goal = "PRESENTS" }
			status := "BUSY"
			if activeID == "" { status = "IDLE" }
			
			a.AddLog(fmt.Sprintf("[STATUS] State: %s | Goal: %s | Items in room: %d", status, goal, len(items)))
			lastHeartbeat = time.Now()
		}

		if activeID != "" {
			continue
		}

		var bestTarget *TargetItem
		if !hammerHeld {
			for i := range items {
				if items[i].Name == "toby_hammer" {
					bestTarget = &items[i]
					break
				}
			}
		} else {
			var newestTarget *TargetItem
			for i := range items {
				if items[i].IsPresent {
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
	
	if target.Name == "toby_hammer" {
		// Phase 1: Talk to Hammer (Use it)
		a.UpdateStatus("TALKING TO HAMMER")
		a.Interact(target.ID)
		
		// Phase 2: Wait 6s
		a.AddLog("Waiting 6 seconds before walking to tile...")
		time.Sleep(6 * time.Second)
		
		// Phase 3: Walk onto tile
		a.UpdateStatus("WALKING ONTO TILE")
		a.MoveToLoc(target.Loc, false)
		
		// Wait for pickup (hammer removed)
		for i := 0; i < 10; i++ {
			time.Sleep(1 * time.Second)
			a.mu.Lock()
			_, exists := a.roomItems[target.ID]
			a.mu.Unlock()
			if !exists {
				a.AddLog("Hammer picked up!")
				a.SetHammerHeld(true)
				break
			}
		}
		
		a.mu.Lock()
		a.activeTargetID = ""
		a.mu.Unlock()
		a.UpdateStatus(a.getWaitingStatus())
		return
	}

	if target.IsPresent {
		// Phase 5: Walk next to it
		a.UpdateStatus("MOVING TO PRESENT")
		a.MoveToLoc(target.Loc, true)
		
		// Phase 6: Wait 6s once near it
		a.AddLog("Waiting 6 seconds near present...")
		time.Sleep(6 * time.Second)
		
		// Phase 7: Open it
		a.mu.Lock()
		_, stillExists := a.roomItems[target.ID]
		if !stillExists {
			a.activeTargetID = ""
			a.mu.Unlock()
			a.AddLog("[ABORT] Present is gone.")
			a.UpdateStatus(a.getWaitingStatus())
			return
		}
		a.mu.Unlock()

		a.UpdateStatus("OPENING PRESENT")
		a.Interact(target.ID)
		time.Sleep(2000 * time.Millisecond)

		a.mu.Lock()
		a.activeTargetID = ""
		a.mu.Unlock()
		a.UpdateStatus(a.getWaitingStatus())
	}
}

func (a *App) MoveToLoc(locStr string, walkNextTo bool) {
	if a.ext == nil { return }
	
	// Strip 'Su' prefix if present
	if strings.HasPrefix(locStr, "Su") {
		locStr = locStr[2:]
	}

	coords := []byte(locStr)
	if len(coords) < 2 { return }

	// Origins coordinate packets should be exactly 2 chars (X, Y) + 'H'
	if len(coords) > 2 {
		coords = coords[:2]
	}

	if walkNextTo {
		if coords[0] > 65 { 
			coords[0]-- 
		} else if coords[1] > 65 {
			coords[1]--
		}
	}
	
	coords = append(coords, 'H')
	
	a.AddLog(fmt.Sprintf("Move: %s", string(coords)))
	a.ext.Send(g.Out.Id("Move"), coords)
	
	// Also send LookTo to face the tile
	if len(coords) >= 3 {
		lookCoords := coords[:len(coords)-1]
		a.ext.Send(g.Out.Id("LookTo"), lookCoords)
	}
}

func (a *App) Interact(id string) {
	if a.ext == nil { return }
	data := "@I" + id + "@A0"
	a.AddLog(fmt.Sprintf("Interact ID %s", id))
	a.ext.Send(g.Out.Id("SetStuffData"), []byte(data))
}

func (a *App) CarryItem(itemID string) {
	if a.ext == nil { return }
	a.ext.Send(g.Out.Id("CarryItem"), []byte(itemID))
}

func (a *App) getWaitingStatus() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.enabled { return "IDLE" }
	if a.hammerHeld { return "WAITING FOR PRESENTS" }
	return "WAITING FOR HAMMER"
}

func (a *App) ToggleEvent(enabled bool) {
	a.mu.Lock()
	a.enabled = enabled
	a.activeTargetID = ""
	a.consecutiveMisses = 0
	if !enabled {
		a.simulating = false
		a.roomItems = make(map[string]TargetItem)
		a.mu.Unlock()
		a.UpdateStatus("IDLE")
		a.AddLog("Bot DISABLED.")
	} else {
		held := a.hammerHeld
		a.mu.Unlock()
		if held { a.UpdateStatus("WAITING FOR PRESENTS") } else { a.UpdateStatus("WAITING FOR HAMMER") }
		a.AddLog("Bot ENABLED.")
	}
}

func (a *App) SimulateDrop() {
	a.mu.Lock()
	if a.simulating { a.mu.Unlock(); return }
	a.simulating = true
	a.hammerHeld = false
	a.mu.Unlock()

	a.AddLog("--- SIMULATION STARTED ---")
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "simulatingUpdate", true)
		runtime.EventsEmit(a.ctx, "hammerHeldUpdate", false)
	}
	
	go func() {
		defer func() {
			a.mu.Lock()
			a.simulating = false
			a.mu.Unlock()
			a.AddLog("--- SIMULATION ENDED ---")
			if a.ctx != nil { runtime.EventsEmit(a.ctx, "simulatingUpdate", false) }
		}()
		// Simulation using user's example coordinates
		// PBPC is the requested walking tile for the hammer
		a.addObject("sim_h", "toby_hammer", "PBPCIIH1.0")
		a.AddLog("[SIM] Hammer spawned at PBPC. Bot should Talk -> Wait 6s -> Walk.")
		
		// Wait for bot to "pick up"
		time.Sleep(15 * time.Second)
		
		a.SetHammerHeld(true)
		a.removeObject("sim_h")
		time.Sleep(2 * time.Second)
		
		locs := []string{"PAQC", "RASE"}
		for i, l := range locs {
			id := fmt.Sprintf("sim_p%d", i)
			a.addObject(id, "present", l+"IIH1.0")
			a.AddLog(fmt.Sprintf("[SIM] Present %d spawned at %s. Bot should Walk -> Wait 6s -> Open.", i, l))
			time.Sleep(15 * time.Second)
			a.removeObject(id)
			time.Sleep(1 * time.Second)
		}
	}()
}

func (a *App) StopSimulation() {
	a.mu.Lock()
	a.simulating = false
	a.mu.Unlock()
}

func (a *App) ResetHammer() { a.SetHammerHeld(false) }

func (a *App) SetHammerHeld(held bool) {
	a.mu.Lock()
	a.hammerHeld = held
	a.mu.Unlock()
	a.AddLog(fmt.Sprintf("Hammer state: %v", held))
	a.UpdateStatus(a.getWaitingStatus())
	if a.ctx != nil { runtime.EventsEmit(a.ctx, "hammerHeldUpdate", held) }
}

func (a *App) UpdateStatus(status string) {
	a.mu.Lock()
	a.status = status
	a.mu.Unlock()
	if a.ctx != nil { runtime.EventsEmit(a.ctx, "statusUpdate", status) }
}

func (a *App) GetStatus() string {
	a.mu.Lock(); defer a.mu.Unlock(); return a.status
}

func (a *App) GetHammerHeld() bool {
	a.mu.Lock(); defer a.mu.Unlock(); return a.hammerHeld
}

func (a *App) GetLogs() []string {
	a.mu.Lock(); defer a.mu.Unlock(); return a.logs
}

func (a *App) LogRoomState() {
	a.mu.Lock(); defer a.mu.Unlock()
	a.AddLog(fmt.Sprintf("--- ROOM STATE (%d) ---", len(a.roomItems)))
	for id, it := range a.roomItems { a.AddLog(fmt.Sprintf("[%s] %s @ %s", id, it.Name, it.Loc)) }
}

func (a *App) TestMove(coords string) {
	if a.ext == nil { return }
	
	// If input is hex (e.g. 53755042504348), inject it directly as-is
	if b, err := hex.DecodeString(coords); err == nil && len(b) >= 2 {
		a.AddLog(fmt.Sprintf("TestMove: Injecting TOTAL RAW hex %s", coords))
		// For Shockwave, header is encoded as (c1-64)*64 + (c2-64)
		headerValue := uint16(b[0]-64)*64 + uint16(b[1]-64)
		a.ext.SendPacket(&g.Packet{Header: g.Header{Dir: g.Out, Value: headerValue}, Data: b[2:]})
		return
	}
	
	// Otherwise, treat as coordinates and use MoveToLoc
	a.AddLog(fmt.Sprintf("TestMove: Testing location %s", coords))
	a.MoveToLoc(coords, false)
}

func (a *App) ShowWindow() {
	if a.ctx != nil { runtime.WindowShow(a.ctx) }
}

func main() {
	app := NewApp()
	err := wails.Run(&options.App{
		Title:  "Anniversary Bot",
		Width:  400,
		Height: 600,
		AssetServer: &assetserver.Options{ Assets: assets },
		OnStartup: app.startup,
		Bind: []interface{}{ app },
	})
	if err != nil { log.Fatal(err) }
}
