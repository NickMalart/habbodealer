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

var ext = g.NewExt(g.ExtInfo{
	Title:       "Anniversary Bot",
	Description: "Auto Hammer & Present Collector",
	Version:     "1.1.0",
	Author:      "Dubbo",
})

type App struct {
	ctx            context.Context
	mu             sync.Mutex
	logs           []string
	collectEnabled bool
	myID           string
	myX, myY       int
	isWalking      bool
	targetID       string
	targetX, targetY int
	targetType     string // "hammer" or "present"
	hasHammer      bool
	presents       map[string]presentInfo
	hammerTimer    *time.Timer
	isSimulating   bool
	simStop        chan struct{}
}

type presentInfo struct {
	id   string
	data []byte // The 4 bytes
	x, y int
}

func NewApp() *App {
	return &App{
		logs:     []string{},
		presents: make(map[string]presentInfo),
		myID:     "12345", // Default ID for simulation
		myX:      18,
		myY:      20,
	}
}

func decodePos(b1, b2 byte) int {
	return int(b1-64)*64 + int(b2-64)
}

func encodePos(v int) (byte, byte) {
	return byte(v/64 + 64), byte(v%64 + 64)
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	go a.runExt()
}

func (a *App) runExt() {
	ext.Activated(func() {
		if a.ctx != nil {
			runtime.WindowShow(a.ctx)
		}
	})

	// Register Headers
	ext.Headers().Add("USER_OBJECT", g.Header{Dir: g.In, Value: 5})
	ext.Headers().Add("STATUS", g.Header{Dir: g.In, Value: 34})
	ext.Headers().Add("ACTIVEOBJECT_ADD", g.Header{Dir: g.In, Value: 93})
	ext.Headers().Add("ACTIVEOBJECT_REMOVE", g.Header{Dir: g.In, Value: 94})
	ext.Headers().Add("PICKUP", g.Header{Dir: g.Out, Value: 74})
	ext.Headers().Add("MOVE", g.Header{Dir: g.Out, Value: 1269})

	// Intercept USER_OBJECT (Header 5) to get our own ID
	ext.Intercept(g.In.Id("USER_OBJECT")).With(func(e *g.Intercept) {
		id := e.Packet.ReadString()
		if id == "" {
			// Fallback if not a string
			id = fmt.Sprintf("%d", e.Packet.ReadInt())
		}
		a.mu.Lock()
		a.myID = id
		a.mu.Unlock()
		a.addLog(fmt.Sprintf("Detected my player ID: %s", id))
	})

	// Intercept STATUS (Header 34) to track our position and hammer status
	ext.Intercept(g.In.Id("STATUS")).With(func(e *g.Intercept) {
		a.handleStatusPacket(e.Packet.Data)
	})

	// Intercept ACTIVEOBJECT_ADD (Header 93) to detect hammers and presents
	ext.Intercept(g.In.Id("ACTIVEOBJECT_ADD")).With(func(e *g.Intercept) {
		a.handleActiveObjectAdd(e.Packet.Data)
	})

	// Intercept ACTIVEOBJECT_REMOVE (Header 94) to clear targets
	ext.Intercept(g.In.Id("ACTIVEOBJECT_REMOVE")).With(func(e *g.Intercept) {
		a.handleActiveObjectRemove(e.Packet.Data)
	})

	ext.Run()
}

func (a *App) handleStatusPacket(data []byte) {
	// Status entries are often separated by \x0D (13) or \x02
	entries := strings.Split(string(data), "\x0D")
	if len(entries) <= 1 {
		entries = strings.Split(string(data), "\x02")
	}

	for _, entry := range entries {
		if len(entry) < 4 {
			continue
		}
		fields := strings.Split(entry, " ")
		if len(fields) < 2 {
			continue
		}
		id := fields[0]
		
		a.mu.Lock()
		if id == a.myID {
			// Format: ID X Y Z Dir Status
			// X and Y are often single bytes in fields[1]
			loc := fields[1]
			if len(loc) >= 2 {
				a.myX = int(loc[0]) - 64
				a.myY = int(loc[1]) - 64
				
				// Detect hammer being held
				if strings.Contains(entry, "hmr 1/") {
					if !a.hasHammer {
						a.addLogInternal("Detected hammer in hand.")
						a.hasHammer = true
						a.startHammerTimerInternal()
					}
				}

				// Check if we arrived at target
				if a.collectEnabled && a.isWalking && a.myX == a.targetX && a.myY == a.targetY {
					a.isWalking = false
					go a.handleArrival()
				}
			}
		}
		a.mu.Unlock()
	}
	a.emitLogs()
}

func (a *App) handleActiveObjectAdd(data []byte) {
	strData := string(data)
	fields := strings.Split(strData, "\x02")
	if len(fields) < 3 {
		return
	}

	id := fields[0]
	name := fields[1]
	loc := fields[2]
	if len(loc) < 4 {
		return
	}

	// Coordinates for objects use specific offsets depending on type
	ox, oy := 0, 0
	if strings.Contains(name, "hammer") {
		ox = int(loc[0]) - 65
		oy = int(loc[1]) - 45
	} else if strings.Contains(name, "present") {
		ox = int(loc[0]) - 63
		oy = int(loc[1]) - 42
	} else {
		// Default fallback
		ox = int(loc[0]) - 63
		oy = int(loc[1]) - 45
	}

	a.mu.Lock()
	doWalk := false
	targetType := ""
	if strings.Contains(name, "hammer") {
		a.addLogInternal(fmt.Sprintf("🔨 Hammer appeared! ID=%s at (%d, %d)", id, ox, oy))
		if a.collectEnabled && !a.hasHammer && !a.isWalking {
			doWalk = true
			targetType = "hammer"
		}
	} else if strings.Contains(name, "present") {
		a.addLogInternal(fmt.Sprintf("🎁 Present appeared! ID=%s at (%d, %d)", id, ox, oy))
		a.presents[id] = presentInfo{id: id, data: []byte(loc[:4]), x: ox, y: oy}
		if a.collectEnabled && a.hasHammer && !a.isWalking {
			doWalk = true
			targetType = "present"
		}
	}
	a.mu.Unlock()
	a.emitLogs()

	if doWalk {
		if targetType == "hammer" {
			a.startWalking(id, "hammer", ox, oy, loc[:4])
		} else {
			a.startWalking(id, "present", ox, oy, loc[:4])
		}
	}
}

func (a *App) handleActiveObjectRemove(data []byte) {
	id := string(data)
	a.mu.Lock()
	delete(a.presents, id)
	if a.targetID == id {
		a.addLogInternal(fmt.Sprintf("Target %s removed, stopping.", id))
		a.isWalking = false
		a.targetID = ""
	}
	a.mu.Unlock()
	a.emitLogs()
}

func (a *App) startHammerTimerInternal() {
	// Assumes lock is held
	if a.hammerTimer != nil {
		a.hammerTimer.Stop()
	}
	a.hammerTimer = time.AfterFunc(5*time.Minute, func() {
		a.mu.Lock()
		a.hasHammer = false
		a.addLogInternal("Hammer timer expired. Internal state reset.")
		a.mu.Unlock()
		a.emitLogs()
	})
}

func (a *App) ResetHammer() {
	a.mu.Lock()
	a.hasHammer = false
	if a.hammerTimer != nil {
		a.hammerTimer.Stop()
	}
	a.addLogInternal("Hammer status reset manually.")
	a.mu.Unlock()
	a.emitLogs()
}

func (a *App) startWalking(id string, itemType string, ox, oy int, rawLoc string) {
	a.mu.Lock()
	if a.isWalking {
		a.mu.Unlock()
		return
	}
	mx, my := a.myX, a.myY
	sim := a.isSimulating
	a.mu.Unlock()

	// Always walk to a neighbor for both hammer and present
	tx, ty := a.getBestNeighbor(ox, oy, mx, my)

	a.mu.Lock()
	a.targetID = id
	a.targetType = itemType
	a.targetX = tx
	a.targetY = ty
	a.isWalking = true
	a.mu.Unlock()

	// Construct MOVE packet (Header 1269 / Su)
	// Format derived from working hex SuQBSEH:
	// [TargetX+63][TargetY+45][OriginX+65][OriginY+49] + H
	moveData := []byte{
		byte(tx + 63),
		byte(ty + 45),
		byte(mx + 65),
		byte(my + 49),
		'H',
	}

	a.addLog(fmt.Sprintf("🚶 Walking to %s (%d, %d) from (%d, %d). Packet Hex: 5375%x", itemType, tx, ty, mx, my, moveData))
	ext.Send(g.Out.Id("MOVE"), moveData)

	if sim {
		// In simulation, we also move the character locally towards the target
		go a.simulateMovement(tx, ty)
	}
}

func (a *App) TestWalk(tx, ty, mx, my int) {
	moveData := []byte{
		byte(tx + 63),
		byte(ty + 45),
		byte(mx + 65),
		byte(my + 49),
		'H',
	}
	a.addLog(fmt.Sprintf("TestWalk to (%d, %d) from (%d, %d). Packet Hex: %x", tx, ty, mx, my, moveData))
	ext.Send(g.Out.Id("MOVE"), moveData)
}

func (a *App) SetMyPos(x, y int) {
	a.mu.Lock()
	a.myX = x
	a.myY = y
	a.mu.Unlock()
	a.addLog(fmt.Sprintf("Local position manually set to (%d, %d)", x, y))
}

func (a *App) simulateMovement(tx, ty int) {
	for {
		a.mu.Lock()
		mx, my := a.myX, a.myY
		id := a.myID
		sim := a.isSimulating
		isWalking := a.isWalking
		a.mu.Unlock()

		if !sim || !isWalking {
			return
		}

		if mx == tx && my == ty {
			a.addLog(fmt.Sprintf("[SIM] Arrived at (%d, %d)", mx, my))
			return
		}

		// Move one tile
		if mx < tx {
			mx++
		} else if mx > tx {
			mx--
		}
		if my < ty {
			my++
		} else if my > ty {
			my--
		}

		a.addLog(fmt.Sprintf("[SIM] Moving to (%d, %d)...", mx, my))

		// Emit simulated status packet locally to advance the simulation
		// Format: ID X_byte Y_byte Z_byte /flat/0/0/0/
		statusData := fmt.Sprintf("%s %c%c%c/flat/0/0/0", id, byte(mx+64), byte(my+64), byte(64))
		
		// If holding hammer, add it
		a.mu.Lock()
		if a.hasHammer {
			statusData += "/hmr 1"
		}
		a.mu.Unlock()
		statusData += "/"

		a.handleStatusPacket([]byte(statusData))

		time.Sleep(800 * time.Millisecond) // Walking speed simulation
	}
}

func (a *App) getBestNeighbor(px, py, mx, my int) (int, int) {
	neighbors := [][2]int{
		{px + 1, py}, {px - 1, py}, {px, py + 1}, {px, py - 1},
	}
	bestX, bestY := px+1, py
	minDist := 999999
	for _, n := range neighbors {
		dist := abs(n[0]-mx) + abs(n[1]-my)
		if dist < minDist {
			minDist = dist
			bestX, bestY = n[0], n[1]
		}
	}
	return bestX, bestY
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (a *App) seekNextPresent() {
	a.mu.Lock()
	var nextP *presentInfo
	if len(a.presents) > 0 {
		for id := range a.presents {
			p := a.presents[id]
			nextP = &p
			break
		}
	}
	a.mu.Unlock()

	if nextP != nil {
		a.startWalking(nextP.id, "present", nextP.x, nextP.y, string(nextP.data))
	}
}

func (a *App) handleArrival() {
	a.mu.Lock()
	id := a.targetID
	itemType := a.targetType
	enabled := a.collectEnabled
	a.mu.Unlock()
	
	if !enabled || id == "" {
		return
	}

	if itemType == "hammer" {
		a.addLog("Arrived at hammer. Waiting 6s...")
		time.Sleep(6 * time.Second)
		a.addLog(fmt.Sprintf("Picking up hammer %s...", id))
		
		a.mu.Lock()
		a.hasHammer = true
		a.startHammerTimerInternal()
		a.mu.Unlock()
		a.emitLogs()
		
		a.pickup(id)
	} else {
		a.addLog(fmt.Sprintf("Arrived next to present %s. Picking up...", id))
		a.pickup(id)
	}

	// Seek next if we have hammer
	a.mu.Lock()
	hasHammer := a.hasHammer
	a.mu.Unlock()

	if hasHammer {
		a.seekNextPresent()
	}
}

func (a *App) pickup(id string) {
	// ALWAYS send the packet
	pickupData := []byte("@I" + id + "@A0")
	ext.Send(g.Out.Id("PICKUP"), pickupData)

	a.mu.Lock()
	sim := a.isSimulating
	a.mu.Unlock()

	if sim {
		a.addLog(fmt.Sprintf("[SIM] Local cleanup of picked ID %s", id))
		// Simulate object removal locally
		a.handleActiveObjectRemove([]byte(id))
	}
}

func (a *App) ToggleCollect(enabled bool) {
	a.mu.Lock()
	a.collectEnabled = enabled
	a.mu.Unlock()
	a.addLog(fmt.Sprintf("Auto-Collect: %v", enabled))
}

func (a *App) addLog(msg string) {
	a.mu.Lock()
	a.addLogInternal(msg)
	a.mu.Unlock()
	a.emitLogs()
}

func (a *App) addLogInternal(msg string) {
	a.logs = append(a.logs, msg)
	if len(a.logs) > 100 {
		a.logs = a.logs[1:]
	}
}

func (a *App) emitLogs() {
	if a.ctx != nil {
		a.mu.Lock()
		logsCopy := make([]string, len(a.logs))
		copy(logsCopy, a.logs)
		a.mu.Unlock()
		runtime.EventsEmit(a.ctx, "logsUpdate", logsCopy)
	}
}

func (a *App) TestMove(input string) {
	data, err := hex.DecodeString(input)
	if err != nil || len(data) < 2 {
		a.addLog(fmt.Sprintf("Invalid or too short hex: %s", input))
		return
	}
	headerVal := uint16(int(data[1]-64) + int(data[0]-64)*64)
	a.addLog(fmt.Sprintf("Sending packet: Header=%d, DataHex=%x", headerVal, data[2:]))
	ext.Headers().Add("TEST_MOVE", g.Header{Dir: g.Out, Value: headerVal})
	ext.Send(g.Out.Id("TEST_MOVE"), data[2:])
}

func (a *App) ToggleEvent(enabled bool) { a.addLog(fmt.Sprintf("Toggle Event: %v", enabled)) }
func (a *App) SetHammerHeld(held bool) {
	a.mu.Lock()
	a.hasHammer = held
	a.mu.Unlock()
	a.addLog(fmt.Sprintf("Hammer held set to: %v", held))
}

func (a *App) SimulatePacket() {
	packetHex := "415d393030303030303032024d746f62795f68616d6d65720252425044494948312e3002302c302c300202483002484d4d"
	data, err := hex.DecodeString(packetHex)
	if err != nil {
		a.addLog(fmt.Sprintf("Error decoding simulation packet: %v", err))
		return
	}

	a.mu.Lock()
	a.collectEnabled = true
	a.mu.Unlock()

	a.addLog("📥 Sending Simulation Packet: ACTIVEOBJECT_ADD (Hammer)")
	// Send to Incoming (server -> client) stream to trigger interceptors
	ext.Send(g.In.Id("ACTIVEOBJECT_ADD"), data[2:])
}

func (a *App) SimulateDrop() {
	a.mu.Lock()
	if a.isSimulating {
		a.mu.Unlock()
		return
	}
	a.isSimulating = true
	a.collectEnabled = true
	a.simStop = make(chan struct{})
	a.mu.Unlock()
	
	a.addLog("🚀 Starting full simulation run...")
	
	go func() {
		stop := a.simStop
		// Initial position
		a.mu.Lock()
		a.myX, a.myY = 18, 20
		a.hasHammer = false
		a.presents = make(map[string]presentInfo)
		a.isWalking = false
		a.mu.Unlock()
		
		a.addLog(fmt.Sprintf("[SIM] Bot brain initialized at (%d, %d)", 18, 20))
		
		// 1. Hammer drops at (18, 22)
		time.Sleep(2 * time.Second)
		a.addLog("[SIM] Brain detected Hammer at (18, 22). Deciding path...")
		a.handleActiveObjectAdd([]byte("900000005\x02Mtoby_hammer\x02SCREIIH1.0"))
		
		// Wait for pickup
		for {
			select {
			case <-stop: return
			case <-time.After(1 * time.Second):
				a.mu.Lock()
				has := a.hasHammer
				a.mu.Unlock()
				if has {
					goto gotHammer
				}
			}
		}
		
	gotHammer:
		a.addLog("✅ [SIM] Brain confirmed Hammer acquired. Watching for Present...")
		time.Sleep(3 * time.Second)
		
		// 2. Present spawns at (18, 25)
		a.addLog("[SIM] Brain detected Present at (18, 25). Calculating neighbor tile...")
		a.handleActiveObjectAdd([]byte("900000006\x02Manniv_present_gen3\x02QCRBIIH0.0"))
		
		// Wait for present to be picked up
		for {
			select {
			case <-stop: return
			case <-time.After(1 * time.Second):
				a.mu.Lock()
				targets := len(a.presents)
				a.mu.Unlock()
				if targets == 0 {
					a.addLog("🏁 [SIM] Brain confirmed Present collected. Simulation successful.")
					a.mu.Lock()
					a.isSimulating = false
					a.mu.Unlock()
					return
				}
			}
		}
	}()
}

func (a *App) StopSimulation() {
	a.mu.Lock()
	if !a.isSimulating {
		a.mu.Unlock()
		return
	}
	a.isSimulating = false
	if a.simStop != nil {
		close(a.simStop)
	}
	a.mu.Unlock()
	a.addLog("Simulation stopped.")
}

func (a *App) GetLogs() []string { return a.logs }
func (a *App) GetStatus() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	sim := ""
	if a.isSimulating {
		sim = "[SIM] "
	}
	if a.hasHammer {
		return sim + "HOLDING HAMMER"
	}
	return sim + "WAITING FOR HAMMER"
}
func (a *App) GetHammerHeld() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.hasHammer
}
func (a *App) LogRoomState() { a.addLog("Log Room State stub") }
func (a *App) GetIsSimulating() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.isSimulating
}

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:  "Anniversary Bot",
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
