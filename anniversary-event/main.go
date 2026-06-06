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
	myID           int
	myX, myY       int
	isWalking      bool
	targetID       string
	targetX, targetY int
	targetType     string // "hammer" or "present"
	hasHammer      bool
	presents       map[string]presentInfo
	hammerTimer    *time.Timer
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
		id := e.Packet.ReadInt()
		a.mu.Lock()
		a.myID = id
		a.mu.Unlock()
		a.addLog(fmt.Sprintf("Detected my player ID: %d", id))
	})

	// Intercept STATUS (Header 34) to track our position and hammer status
	ext.Intercept(g.In.Id("STATUS")).With(func(e *g.Intercept) {
		data := string(e.Packet.Data)
		entries := strings.Split(data, "\x02")
		for _, entry := range entries {
			if len(entry) < 6 {
				continue
			}
			// ID is 2 bytes
			id := decodePos(entry[0], entry[1])
			
			a.mu.Lock()
			if id == a.myID {
				a.myX = decodePos(entry[2], entry[3])
				a.myY = decodePos(entry[4], entry[5])
				
				// Detect hammer being held (e.g. /hmr 1/)
				if strings.Contains(entry, "hmr 1/") {
					if !a.hasHammer {
						a.addLog("Detected hammer in hand.")
						a.hasHammer = true
						a.startHammerTimer()
					}
				}

				// Check if we arrived at target
				if a.collectEnabled && a.isWalking && a.myX == a.targetX && a.myY == a.targetY {
					a.isWalking = false
					go a.handleArrival()
				}
			}
			a.mu.Unlock()
		}
	})

	// Intercept ACTIVEOBJECT_ADD (Header 93) to detect hammers and presents
	ext.Intercept(g.In.Id("ACTIVEOBJECT_ADD")).With(func(e *g.Intercept) {
		data := string(e.Packet.Data)
		fields := strings.Split(data, "\x02")
		if len(fields) < 3 {
			return
		}

		id := fields[0]
		name := fields[1]
		loc := fields[2]
		if len(loc) < 4 {
			return
		}
		ox := decodePos(loc[0], loc[1])
		oy := decodePos(loc[2], loc[3])

		a.mu.Lock()
		defer a.mu.Unlock()

		if strings.Contains(name, "hammer") {
			a.addLog(fmt.Sprintf("🔨 Hammer appeared! ID=%s at (%d, %d)", id, ox, oy))
			if !a.collectEnabled || a.hasHammer || a.isWalking {
				return
			}
			a.startWalking(id, "hammer", ox, oy, loc[:4])
		} else if strings.Contains(name, "present") {
			a.addLog(fmt.Sprintf("🎁 Present appeared! ID=%s at (%d, %d)", id, ox, oy))
			a.presents[id] = presentInfo{id: id, data: []byte(loc[:4]), x: ox, y: oy}
			
			if !a.collectEnabled || !a.hasHammer || a.isWalking {
				return
			}
			a.seekNextPresent()
		}
	})

	// Intercept ACTIVEOBJECT_REMOVE (Header 94) to clear targets
	ext.Intercept(g.In.Id("ACTIVEOBJECT_REMOVE")).With(func(e *g.Intercept) {
		id := string(e.Packet.Data)
		a.mu.Lock()
		delete(a.presents, id)
		if a.targetID == id {
			a.addLog(fmt.Sprintf("Target %s removed, stopping.", id))
			a.isWalking = false
			a.targetID = ""
		}
		a.mu.Unlock()
	})

	ext.Run()
}

func (a *App) startHammerTimer() {
	if a.hammerTimer != nil {
		a.hammerTimer.Stop()
	}
	a.hammerTimer = time.AfterFunc(5*time.Minute, func() {
		a.mu.Lock()
		a.hasHammer = false
		a.addLog("Hammer timer expired. Internal state reset.")
		a.mu.Unlock()
	})
}

func (a *App) ResetHammer() {
	a.mu.Lock()
	a.hasHammer = false
	if a.hammerTimer != nil {
		a.hammerTimer.Stop()
	}
	a.mu.Unlock()
	a.addLog("Hammer status reset manually.")
}

func (a *App) startWalking(id string, itemType string, ox, oy int, rawLoc string) {
	a.mu.Lock()
	mx, my := a.myX, a.myY
	a.mu.Unlock()

	tx, ty := ox, oy
	finalLoc := []byte(rawLoc)

	if itemType == "present" {
		// Walk to a neighbor
		tx, ty = a.getBestNeighbor(ox, oy, mx, my)
		b1, b2 := encodePos(tx)
		b3, b4 := encodePos(ty)
		finalLoc = []byte{b1, b2, b3, b4}
	}

	a.mu.Lock()
	a.targetID = id
	a.targetType = itemType
	a.targetX = tx
	a.targetY = ty
	a.isWalking = true
	a.mu.Unlock()

	a.addLog(fmt.Sprintf("Walking to %s... To(%d,%d)", itemType, tx, ty))
	// MOVE packet: Su + 4 bytes target + 6 bytes suffix
	ext.Send(g.Out.Id("MOVE"), append(finalLoc, []byte("pljSZM")...))
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
	if len(a.presents) == 0 {
		return
	}
	// Pick any present for now
	for id, p := range a.presents {
		a.startWalking(id, "present", p.x, p.y, string(p.data))
		break
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
		a.pickup(id)
		
		a.mu.Lock()
		a.hasHammer = true
		a.startHammerTimer()
		a.mu.Unlock()
	} else {
		a.addLog(fmt.Sprintf("Arrived next to present %s. Picking up...", id))
		a.pickup(id)
	}

	// Seek next if we have hammer
	a.mu.Lock()
	if a.hasHammer {
		a.seekNextPresent()
	}
	a.mu.Unlock()
}

func (a *App) pickup(id string) {
	// AJ @I ID @A0
	pickupData := []byte("@I" + id + "@A0")
	ext.Send(g.Out.Id("PICKUP"), pickupData)
}

func (a *App) ToggleCollect(enabled bool) {
	a.mu.Lock()
	a.collectEnabled = enabled
	a.mu.Unlock()
	a.addLog(fmt.Sprintf("Auto-Collect: %v", enabled))
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
func (a *App) SimulateDrop() { a.addLog("Simulate Drop stub") }
func (a *App) StopSimulation() { a.addLog("Stop Simulation stub") }
func (a *App) GetLogs() []string { return a.logs }
func (a *App) GetStatus() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.hasHammer {
		return "HOLDING HAMMER"
	}
	return "WAITING FOR HAMMER"
}
func (a *App) GetHammerHeld() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.hasHammer
}
func (a *App) LogRoomState() { a.addLog("Log Room State stub") }

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
