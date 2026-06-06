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
		Version:     "1.0.6",
		Author:      "Gemini CLI",
	})

	// Register headers for Habbo Origins (Shockwave)
	a.ext.Headers().Add("STATUS", g.Header{Dir: g.In, Value: 34})
	a.ext.Headers().Add("OBJECTS", g.Header{Dir: g.In, Value: 32})
	a.ext.Headers().Add("REMOVE_ITEM", g.Header{Dir: g.In, Value: 84})
	a.ext.Headers().Add("SetStuffData", g.Header{Dir: g.Out, Value: 74})
	a.ext.Headers().Add("Move", g.Header{Dir: g.Out, Value: 1269})
	a.ext.Headers().Add("CarryItem", g.Header{Dir: g.Out, Value: 97})
	a.ext.Headers().Add("Pong", g.Header{Dir: g.Out, Value: 196})

	a.ext.Activated(func() {
		a.ShowWindow()
		a.AddLog("Extension activated!")
	})

	// Intercept packets
	a.ext.Intercept(g.In.Id("OBJECTS")).With(a.handleObjects)
	a.ext.Intercept(g.In.Id("REMOVE_ITEM")).With(a.handleObjectRemove)
	a.ext.Intercept(g.In.Id("STATUS")).With(a.handleStatus)
	
	// Log all outgoing for debugging
	a.ext.Intercept(g.Out.Any).With(a.handleOutgoing)

	// Start the logic loop
	go a.pursuitLoop()

	a.AddLog("Extension registered. Waiting for connection...")
	go a.ext.Run()
}

func (a *App) LogRoomState() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.AddLog(fmt.Sprintf("--- ROOM STATE (%d Items) ---", len(a.roomItems)))
	for id, it := range a.roomItems {
		a.AddLog(fmt.Sprintf("ID: %s | Name: %s | Loc: %s", id, it.Name, it.Loc))
	}
	a.AddLog("---------------------------")
}

func (a *App) handleOutgoing(e *g.Intercept) {
	header := e.Packet.Header.Value
	name := e.Packet.Header.Name
	data := e.Packet.Data
	a.AddLog(fmt.Sprintf("[OUT] Header %d (%s): %s (Hex: %s)", header, name, string(data), hex.EncodeToString(data)))
}

func (a *App) TestMove(coords string) {
	if a.ext == nil { return }
	payload := append([]byte(coords), 'H')
	a.AddLog(fmt.Sprintf("[TEST] Sending Manual Move: %s", coords))
	a.ext.Send(g.Out.Id("Move"), payload)
}

func (a *App) handleObjects(e *g.Intercept) {
	data := e.Packet.Data
	// Shockwave OBJECTS (32) format: [numObjects][2]ID[2]Name[2]Loc[2]StuffData[2]...
	parts := bytes.Split(data, []byte{0x02})
	if len(parts) < 2 {
		return
	}

	// Skip the first part (number of objects)
	for i := 1; i < len(parts)-3; i += 4 {
		id := string(parts[i])
		name := string(parts[i+1])
		loc := string(parts[i+2])
		a.addObject(id, name, loc)
	}
}

func (a *App) handleStatus(e *g.Intercept) {
	// Status (34) can be used to track own position if needed
}

func (a *App) pursuitLoop() {
	for {
		time.Sleep(1000 * time.Millisecond)
		
		a.mu.Lock()
		enabled := a.enabled
		busy := a.activeTargetID != ""
		simulating := a.simulating
		hammerHeld := a.hammerHeld
		items := make([]TargetItem, 0, len(a.roomItems))
		for _, it := range a.roomItems {
			items = append(items, it)
		}
		a.mu.Unlock()

		if !enabled || busy || simulating {
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
		if a.activeTargetID == id {
			a.activeTargetID = ""
		}
		a.mu.Unlock()
		a.AddLog(fmt.Sprintf("[ROOM] Removed ID %s", id))
	} else {
		a.mu.Unlock()
	}
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
		a.AddLog(fmt.Sprintf("[SIM] 1/4 Hammer appearing at %s...", hammerLoc))
		a.addObject(hammerID, "toby_hammer", hammerLoc+"IIH0.0")
		
		a.AddLog("[SIM] 2/4 Walking to hammer spot...")
		a.MoveToLoc(TargetItem{ID: hammerID, Loc: hammerLoc, Name: "toby_hammer"})
		time.Sleep(4 * time.Second)

		a.AddLog("[SIM] 3/4 Picking up Toby Hammer...")
		a.Interact(hammerID)
		time.Sleep(1 * time.Second)
		
		a.AddLog("[SIM] 4/4 Simulating CarryItem packet...")
		a.CarryItem("4342") 
		
		a.mu.Lock()
		a.hammerHeld = true
		a.mu.Unlock()
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "hammerHeldUpdate", true)
		}
		a.removeObject(hammerID)
		
		a.AddLog("[SIM] Hammer sequence done. Waiting 2s for first present...")
		time.Sleep(2 * time.Second)

		// 2. Present Drops & Walks
		ids := []string{"sim_p1", "sim_p2"}
		locs := []string{"RASE", "KQA"}
		
		for i, id := range ids {
			a.mu.Lock()
			simActive := a.simulating
			a.mu.Unlock()
			if !simActive { 
				a.AddLog("[SIM] Simulation stopped early.")
				return 
			}

			a.AddLog(fmt.Sprintf("[SIM] Present %d/2 appeared at %s", i+1, locs[i]))
			a.addObject(id, "Manniv_present_gen", locs[i]+"IIH0.0")
			
			a.AddLog(fmt.Sprintf("[SIM] Walking next to present %d...", i+1))
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

func (a *App) handleStatus(e *g.Intercept) {
	data := string(e.Packet.Data)
	// Shockwave STATUS (34) format: @b[X][Y][Z][Height]...
	// e.g. @bIIKQA1.0
	if len(data) >= 4 {
		x := int(data[0]) - 64
		y := int(data[1]) - 64
		a.AddLog(fmt.Sprintf("[STATUS] Bot Position: (%d, %d) Raw: %s", x, y, data[:2]))
	}
}

func (a *App) MoveToLoc(target TargetItem) {
	if a.ext == nil {
		return
	}

	locStr := target.Loc
	if len(locStr) < 2 {
		return
	}

	// Shockwave Loc is usually X, Y, Z, Dir (Base64 encoded)
	// SAPB -> X=19, Y=1, Z=16, Dir=2
	// Move packet usually only needs X and Y
	xChar := locStr[0]
	yChar := locStr[1]

	if target.IsPresent {
		// Adjacency logic: step 1 tile away
		if xChar > 65 {
			xChar--
		} else if yChar > 65 {
			yChar--
		}
	}

	// Payload: [X Char][Y Char] + 'H'
	payload := []byte{xChar, yChar, 'H'}

	x := int(xChar) - 64
	y := int(yChar) - 64

	a.AddLog(fmt.Sprintf("Sending Move to (%d, %d) - Payload: %s (Hex: %s)", x, y, string(payload), hex.EncodeToString(payload)))
	a.ext.Send(g.Out.Id("Move"), payload)
}

func (a *App) Interact(id string) {
	if a.ext == nil {
		return
	}
	// Shockwave SetStuffData payload: @I + ID + @A0 (where ID is the string ID)
	payload := []byte("@I" + id + "@A0")
	
	a.AddLog(fmt.Sprintf("Sending Interact (Raw Bytes): %v (Hex: %s)", payload, hex.EncodeToString(payload)))
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
