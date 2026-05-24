package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	g "xabbo.b7c.io/goearth"
	gencoding "xabbo.b7c.io/goearth/encoding"
)

//go:embed all:frontend/dist
var assets embed.FS

type TradeItem struct {
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
}

type App struct {
	ctx     context.Context
	ext     *g.Ext
	mu      sync.Mutex
	logs    []string
	running bool

	// Hand scan state
	stripScanMu           sync.Mutex
	stripScanActive       bool
	stripScanSessionID    int
	stripScanPageCount    int
	stripScanLastPacketAt time.Time
	stripScanSeenItemIDs  map[int]struct{}
	stripScanCounts       map[string]int
	stripScanItemIDs      map[string][]int
	handItems             []TradeItem

	// Filtering state
	ignoredItemIDs map[int]struct{}
}

func NewApp() *App {
	return &App{
		logs:                 []string{"Pickup-Drop initialized..."},
		stripScanSeenItemIDs: make(map[int]struct{}),
		stripScanCounts:      make(map[string]int),
		stripScanItemIDs:     make(map[string][]int),
		ignoredItemIDs:      make(map[int]struct{}),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	a.ext = g.NewExt(g.ExtInfo{
		Title:       "Pickup-Drop",
		Description: "Pickup and Drop items automatically",
		Version:     "1.4.0",
		Author:      "Gemini CLI",
	})

	// Register headers
	a.ext.Headers().Add("PICK_ALL", g.Header{Dir: g.Out, Value: 401})
	a.ext.Headers().Add("GOTOFLAT", g.Header{Dir: g.Out, Value: 123})
	a.ext.Headers().Add("GETSTRIP", g.Header{Dir: g.Out, Value: 65})
	a.ext.Headers().Add("STRIPINFO_2", g.Header{Dir: g.In, Value: 140})
	a.ext.Headers().Add("PLACESTUFF", g.Header{Dir: g.Out, Value: 90})
	a.ext.Headers().Add("PLACEITEM", g.Header{Dir: g.Out, Value: 92})

	a.ext.Activated(func() {
		a.ShowWindow()
		clientType := "Unknown"
		if a.ext != nil {
			clientType = fmt.Sprintf("%v", a.ext.Client())
		}
		a.AddLog(fmt.Sprintf("Extension activated! Connected to: %s", clientType))
	})

	// Intercept STRIPINFO_2 for hand scanning
	a.ext.Intercept(g.In.Id("STRIPINFO_2")).With(a.handleStripPacket)

	a.AddLog("Extension registered. Waiting for connection...")
	go a.ext.Run()
}

func (a *App) handleStripPacket(e *g.Intercept) {
	a.stripScanMu.Lock()
	if !a.stripScanActive {
		a.stripScanMu.Unlock()
		return
	}

	a.stripScanPageCount++
	a.stripScanLastPacketAt = time.Now()
	currentPage := a.stripScanPageCount
	scanID := a.stripScanSessionID

	// Parse the page
	firstMainID, pageRecords, classQtys, classItemIDs := a.parseStripInfoPageRaw(e.Packet.Data)

	pageRepeated := false
	if firstMainID != 0 {
		if _, seen := a.stripScanSeenItemIDs[firstMainID]; seen {
			pageRepeated = true
		} else {
			a.stripScanSeenItemIDs[firstMainID] = struct{}{}
		}
	}

	if !pageRepeated && pageRecords > 0 {
		for className, qty := range classQtys {
			a.stripScanCounts[className] += qty
		}
		for name, ids := range classItemIDs {
			a.stripScanItemIDs[name] = append(a.stripScanItemIDs[name], ids...)
		}
	}

	pageLimitReached := a.stripScanPageCount >= 25
	
	// STOP CONDITIONS: 0 records found, page repeated, or page limit reached
	if pageRecords == 0 || pageRepeated || pageLimitReached {
		a.stripScanActive = false
		a.finalizeHandScan(scanID)
		a.stripScanMu.Unlock()
		return
	}

	a.AddLog(fmt.Sprintf("[STRIP] Page %d: records=%d", currentPage, pageRecords))

	// Request next page
	a.stripScanMu.Unlock()
	go func() {
		time.Sleep(750 * time.Millisecond)
		a.ext.Send(g.Out.Id("GETSTRIP"), []byte("next"))
	}()
}

func (a *App) parseStripInfoPageRaw(data []byte) (firstMainID int, pageRecords int, classQtys map[string]int, classItemIDs map[string][]int) {
	classQtys = make(map[string]int)
	classItemIDs = make(map[string][]int)
	pos := 0

	readVL64 := func() (int, bool) {
		if pos >= len(data) {
			return 0, false
		}
		n := gencoding.VL64DecodeLen(data[pos])
		if n <= 0 || pos+n > len(data) {
			return 0, false
		}
		v := gencoding.VL64Decode(data[pos : pos+n])
		pos += n
		return v, true
	}

	skipUntilDelim := func() {
		for pos < len(data) && data[pos] != 0x02 {
			pos++
		}
		if pos < len(data) {
			pos++ // skip \x02
		}
	}

	count, ok := readVL64()
	if !ok {
		return
	}
	pageRecords = count

	for i := 0; i < count; i++ {
		if pos >= len(data) {
			break
		}

		// Field 1: IDs
		mainID, ok := readVL64()
		if !ok {
			break
		}
		if i == 0 {
			firstMainID = mainID
		}
		
		itemIDs := []int{mainID}
		extraCount, _ := readVL64()
		for j := 0; j < extraCount; j++ {
			if id, ok := readVL64(); ok {
				itemIDs = append(itemIDs, id)
			}
		}
		readVL64() // Pos
		// S|I
		if pos < len(data) {
			pos++
		}
		skipUntilDelim()

		// Field 2: Class Name
		readVL64() // templateId
		readVL64()
		readVL64()
		// String
		start := pos
		for pos < len(data) && data[pos] != 0x02 {
			pos++
		}
		className := string(data[start:pos])
		if pos < len(data) {
			pos++ // skip \x02
		}

		classQtys[className] += len(itemIDs)
		classItemIDs[className] = append(classItemIDs[className], itemIDs...)

		// Field 3: Dimensions/Props
		skipUntilDelim()
	}
	return
}

func (a *App) finalizeHandScan(scanID int) {
	a.mu.Lock()
	a.handItems = nil
	for name, qty := range a.stripScanCounts {
		a.handItems = append(a.handItems, TradeItem{Name: name, Quantity: qty})
	}
	sort.Slice(a.handItems, func(i, j int) bool {
		return a.handItems[i].Name < a.handItems[j].Name
	})
	items := a.handItems
	a.mu.Unlock()

	a.AddLog(fmt.Sprintf("[STRIP] Hand scan complete. Found %d item types.", len(items)))
	for _, it := range items {
		a.AddLog(fmt.Sprintf(" - %s x%d", it.itName(), it.Quantity))
	}
}

func (it TradeItem) itName() string {
	if it.Name == "" {
		return "Unknown"
	}
	return it.Name
}

func (a *App) requestHandScan() int {
	a.stripScanMu.Lock()
	a.stripScanActive = true
	a.stripScanSessionID++
	a.stripScanPageCount = 0
	a.stripScanLastPacketAt = time.Now()
	a.stripScanSeenItemIDs = make(map[int]struct{})
	a.stripScanCounts = make(map[string]int)
	a.stripScanItemIDs = make(map[string][]int)
	sid := a.stripScanSessionID
	a.stripScanMu.Unlock()

	a.AddLog(fmt.Sprintf("[STRIP] Starting hand scan (session %d)...", sid))
	a.ext.Send(g.Out.Id("GETSTRIP"), []byte("new"))
	return sid
}

// --- Modular Step Functions ---

func (a *App) ExecutePickAll() {
	if a.ext == nil {
		a.AddLog("ERROR: Extension not initialized")
		return
	}
	a.AddLog(">>> [Step] Sending PICK_ALL (Header 401)...")
	a.ext.Send(g.Out.Id("PICK_ALL"), []byte{0x61, 0x7b, 0x61, 0x4d, 0x4b})
}

func (a *App) ExecuteRoomRefresh() {
	if a.ext == nil {
		a.AddLog("ERROR: Extension not initialized")
		return
	}
	a.AddLog(">>> [Step] Sending GOTOFLAT (Header 123) for Room 221681...")
	a.ext.Send(g.Out.Id("GOTOFLAT"), []byte("221681"))
}

func (a *App) ExecuteHandScan() {
	if a.ext == nil {
		a.AddLog("ERROR: Extension not initialized")
		return
	}
	a.requestHandScan()
}

// Internal version that doesn't check 'a.running'
func (a *App) runAutoDropLogic() {
	currentX := 1
	currentY := 1
	wallPass := false

	for {
		a.stripScanMu.Lock()
		allItemIDs := []int{}
		for _, ids := range a.stripScanItemIDs {
			for _, id := range ids {
				// FILTER: Skip pre-existing items
				if _, ignored := a.ignoredItemIDs[id]; !ignored {
					allItemIDs = append(allItemIDs, id)
				}
			}
		}
		a.stripScanMu.Unlock()

		if len(allItemIDs) == 0 {
			a.AddLog(">>> SEQUENCE COMPLETED: Hand is empty (or only ignored items remain)! <<<")
			return
		}

		mode := "Floor"
		if wallPass {
			mode = "Wall"
		}
		a.AddLog(fmt.Sprintf(">>> [Auto-Drop] Starting %s pass for %d items...", mode, len(allItemIDs)))

		for i, itemID := range allItemIDs {
			if !wallPass {
				// --- FLOOR PLACEMENT ---
				if currentY > 26 {
					a.AddLog("[Pass] Floor grid is full. Items remain. Switching to Wall Pass...")
					wallPass = true
					break
				}

				a.AddLog(fmt.Sprintf("[%d/%d] Floor: Item %d at (%d, %d)", i+1, len(allItemIDs), itemID, currentX, currentY))
				
				idBuf := make([]byte, gencoding.VL64EncodeLen(itemID))
				gencoding.VL64Encode(idBuf, itemID)
				xBuf := make([]byte, gencoding.VL64EncodeLen(currentX))
				gencoding.VL64Encode(xBuf, currentX)
				yBuf := make([]byte, gencoding.VL64EncodeLen(currentY))
				gencoding.VL64Encode(yBuf, currentY)
				rotBuf := make([]byte, gencoding.VL64EncodeLen(0))
				gencoding.VL64Encode(rotBuf, 0)

				payload := append([]byte{}, idBuf...)
				payload = append(payload, xBuf...)
				payload = append(payload, yBuf...)
				payload = append(payload, rotBuf...)
				a.ext.Send(g.Out.Id("PLACESTUFF"), payload)

				currentX++
				if currentX > 16 {
					currentX = 1
					currentY++
				}
			} else {
				// --- WALL PLACEMENT ---
				prefix := "@P"
				if (i / 10) % 2 == 1 {
					prefix = "@Q"
				}
				wallPos := fmt.Sprintf("%s:w=1,0 l=%d,%d r", prefix, (i % 15) + 5, (i / 15) + 20)
				a.AddLog(fmt.Sprintf("[%d/%d] Wall: Item %d at %s", i+1, len(allItemIDs), itemID, wallPos))
				
				idBuf := make([]byte, gencoding.VL64EncodeLen(itemID))
				gencoding.VL64Encode(idBuf, itemID)
				
				payload := append([]byte{}, idBuf...)
				payload = append(payload, []byte(wallPos)...)
				
				a.ext.Send(g.Out.Id("PLACEITEM"), payload)
			}

			time.Sleep(800 * time.Millisecond)
		}

		// Verification Phase
		a.AddLog(">>> [Verify] Refreshing hand to check status...")
		time.Sleep(2 * time.Second)
		sid := a.requestHandScan()

		scanDeadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(scanDeadline) {
			a.stripScanMu.Lock()
			active := a.stripScanActive
			currentSID := a.stripScanSessionID
			a.stripScanMu.Unlock()
			if !active && currentSID >= sid {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}

		// Check what's left
		a.stripScanMu.Lock()
		remainingNewCount := 0
		for _, ids := range a.stripScanItemIDs {
			for _, id := range ids {
				if _, ignored := a.ignoredItemIDs[id]; !ignored {
					remainingNewCount++
				}
			}
		}
		a.stripScanMu.Unlock()

		if remainingNewCount > 0 && !wallPass {
			a.AddLog("[Verify] Floor items placed, but new items remain. Trying Wall pass next.")
			wallPass = true
		} else if remainingNewCount > 0 && wallPass {
			a.AddLog("[Verify] Wall items placed, but new items remain. Retrying Floor pass.")
			wallPass = false
			currentX = 1
			currentY = 1
		} else {
			a.AddLog(">>> SEQUENCE COMPLETED: Hand is empty (or only ignored items remain)! <<<")
			return
		}
		time.Sleep(1 * time.Second)
	}
}

func (a *App) ExecuteAutoDrop() {
	if a.ext == nil {
		a.AddLog("ERROR: Extension not initialized")
		return
	}

	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		a.AddLog("Drop sequence already in progress...")
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
		
		// Clear ignored list for manual trigger (assuming user wants to drop everything in hand now)
		a.ignoredItemIDs = make(map[int]struct{})
		
		a.runAutoDropLogic()
	}()
}

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

		startTime := time.Now()
		
		// 1. INITIAL SCAN (To identify ignored items)
		a.AddLog(">>> [Step 1/6] Scanning hand for pre-existing items (to ignore)...")
		sid1 := a.requestHandScan()
		scanDeadline1 := time.Now().Add(15 * time.Second)
		for time.Now().Before(scanDeadline1) {
			a.stripScanMu.Lock()
			active := a.stripScanActive
			currentSID := a.stripScanSessionID
			a.stripScanMu.Unlock()
			if !active && currentSID >= sid1 {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}

		// Store ignored IDs
		a.stripScanMu.Lock()
		a.ignoredItemIDs = make(map[int]struct{})
		initialCount := 0
		for _, ids := range a.stripScanItemIDs {
			for _, id := range ids {
				a.ignoredItemIDs[id] = struct{}{}
				initialCount++
			}
		}
		a.stripScanMu.Unlock()
		a.AddLog(fmt.Sprintf("Found %d existing items. These will be ignored during placement.", initialCount))

		// 2. PICK ALL
		a.AddLog(">>> [Step 2/6] Sending PICK_ALL...")
		a.ExecutePickAll()
		
		for i := 10; i > 0; i-- {
			a.AddLog(fmt.Sprintf("Waiting... %ds remaining", i))
			time.Sleep(1 * time.Second)
		}

		// 3. REFRESH ROOM
		a.AddLog(">>> [Step 3/6] Refreshing room...")
		a.ExecuteRoomRefresh()
		time.Sleep(3 * time.Second)
		
		// 4. POST-PICKUP SCAN
		a.AddLog(">>> [Step 4/6] Scanning hand for new items...")
		sid2 := a.requestHandScan()
		scanDeadline2 := time.Now().Add(15 * time.Second)
		for time.Now().Before(scanDeadline2) {
			a.stripScanMu.Lock()
			active := a.stripScanActive
			currentSID := a.stripScanSessionID
			a.stripScanMu.Unlock()
			if !active && currentSID >= sid2 {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}

		// Verify we actually picked something up
		a.stripScanMu.Lock()
		newItemsCount := 0
		for _, ids := range a.stripScanItemIDs {
			for _, id := range ids {
				if _, ignored := a.ignoredItemIDs[id]; !ignored {
					newItemsCount++
				}
			}
		}
		a.stripScanMu.Unlock()

		if newItemsCount == 0 {
			a.AddLog("WARNING: No new items detected in hand after PICK_ALL. Stopping sequence.")
			return
		}
		a.AddLog(fmt.Sprintf("Successfully picked up %d new items!", newItemsCount))

		// 5. RUN AUTO DROP
		a.runAutoDropLogic()
		
		duration := time.Since(startTime).Round(time.Millisecond)
		a.AddLog(fmt.Sprintf("Full sequence finished. Total time: %s", duration))
	}()
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
		Title:  "Pickup-Drop",
		Width:  400,
		Height: 600,
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
