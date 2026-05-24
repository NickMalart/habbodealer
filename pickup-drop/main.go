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
	handItems             []TradeItem
}

func NewApp() *App {
	return &App{
		logs:                 []string{"Pickup-Drop initialized..."},
		stripScanSeenItemIDs: make(map[int]struct{}),
		stripScanCounts:      make(map[string]int),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	a.ext = g.NewExt(g.ExtInfo{
		Title:       "Pickup-Drop",
		Description: "Pickup and Drop items automatically",
		Version:     "1.1.0",
		Author:      "Gemini CLI",
	})

	// Register headers
	a.ext.Headers().Add("PICK_ALL", g.Header{Dir: g.Out, Value: 401})
	a.ext.Headers().Add("GOTOFLAT", g.Header{Dir: g.Out, Value: 123})
	a.ext.Headers().Add("GETSTRIP", g.Header{Dir: g.Out, Value: 65})
	a.ext.Headers().Add("STRIPINFO_2", g.Header{Dir: g.In, Value: 140})

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
	firstMainID, pageRecords, classQtys := a.parseStripInfoPageRaw(e.Packet.Data)

	pageRepeated := false
	if firstMainID != 0 {
		if _, seen := a.stripScanSeenItemIDs[firstMainID]; seen {
			pageRepeated = true
		} else {
			a.stripScanSeenItemIDs[firstMainID] = struct{}{}
		}
	}

	if !pageRepeated {
		for className, qty := range classQtys {
			a.stripScanCounts[className] += qty
		}
	}

	pageLimitReached := a.stripScanPageCount >= 25
	a.AddLog(fmt.Sprintf("[STRIP] Page %d: records=%d repeated=%t limit=%t", currentPage, pageRecords, pageRepeated, pageLimitReached))

	if pageRepeated || pageLimitReached {
		a.stripScanActive = false
		a.finalizeHandScan(scanID)
		a.stripScanMu.Unlock()
		return
	}

	// Request next page
	a.stripScanMu.Unlock()
	go func() {
		time.Sleep(750 * time.Millisecond)
		a.ext.Send(g.Out.Id("GETSTRIP"), []byte("next"))
	}()
}

func (a *App) parseStripInfoPageRaw(data []byte) (firstMainID int, pageRecords int, classQtys map[string]int) {
	classQtys = make(map[string]int)
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
		extraCount, _ := readVL64()
		for j := 0; j < extraCount; j++ {
			readVL64()
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

		classQtys[className] += (1 + extraCount)

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
	sid := a.stripScanSessionID
	a.stripScanMu.Unlock()

	a.AddLog(fmt.Sprintf("[STRIP] Starting hand scan (session %d)...", sid))
	a.ext.Send(g.Out.Id("GETSTRIP"), []byte("new"))
	return sid
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

// ExecuteCommands is the placeholder for the button action
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
		startTime := time.Now()
		defer func() {
			a.mu.Lock()
			a.running = false
			a.mu.Unlock()
			duration := time.Since(startTime).Round(time.Millisecond)
			a.AddLog(fmt.Sprintf("Sequence finished. Total time: %s", duration))
		}()

		a.AddLog(">>> Starting sequence...")

		if a.ext == nil {
			a.AddLog("ERROR: Extension not initialized")
			return
		}

		// Step 1: PICK_ALL (FQa[123]aMK)
		a.AddLog("[Step 1/3] Sending PICK_ALL (Header 401)...")
		a.ext.Send(g.Out.Id("PICK_ALL"), []byte{0x61, 0x7b, 0x61, 0x4d, 0x4b})

		// Step 2: 10s Delay
		for i := 10; i > 0; i-- {
			a.AddLog(fmt.Sprintf("[Step 2/3] Waiting... %ds remaining", i))
			time.Sleep(1 * time.Second)
		}

		// Step 3: GOTOFLAT (@[123]221681)
		a.AddLog("[Step 3/3] Sending GOTOFLAT (Header 123) for Room 221681...")
		a.ext.Send(g.Out.Id("GOTOFLAT"), []byte("221681"))
		a.AddLog("GOTOFLAT packet dispatched.")

		// Step 4: Hand Scan
		a.AddLog("[Step 4/3] Waiting for room entry (3s)...")
		time.Sleep(3 * time.Second)
		a.requestHandScan()

		// Wait for scan to complete (simple poll)
		a.AddLog("Waiting for hand scan completion...")
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			a.stripScanMu.Lock()
			active := a.stripScanActive
			a.stripScanMu.Unlock()
			if !active {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
	}()
}

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:  "Pickup-Drop",
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
