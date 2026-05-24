package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	g "xabbo.b7c.io/goearth"
	gencoding "xabbo.b7c.io/goearth/encoding"
)

//go:embed all:frontend/dist
var assets embed.FS

const DB_URL = "postgresql://neondb_owner:npg_S9jFTYzdQx3l@ep-aged-king-a77p1t8b-pooler.ap-southeast-2.aws.neon.tech/neondb?sslmode=require&channel_binding=require"

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

	// Database pool
	dbPool *pgxpool.Pool

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

	// Scheduling state
	scheduleMu      sync.Mutex
	scheduleEnabled bool
	targetTime      time.Time
}

func NewApp() *App {
	return &App{
		logs:                 []string{"Pickup-Drop initialized..."},
		stripScanSeenItemIDs: make(map[int]struct{}),
		stripScanCounts:      make(map[string]int),
		stripScanItemIDs:     make(map[string][]int),
		ignoredItemIDs:       make(map[int]struct{}),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Initialize Database Pool
	dbCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(dbCtx, DB_URL)
	if err != nil {
		a.AddLog(fmt.Sprintf("CRITICAL ERROR: Failed to connect to DB: %v", err))
	} else {
		a.dbPool = pool
		a.AddLog("Connected to PostgreSQL successfully.")
	}

	a.ext = g.NewExt(g.ExtInfo{
		Title:       "Pickup-Drop",
		Description: "Pickup and Drop items automatically",
		Version:     "2.0.0",
		Author:      "Gemini CLI",
	})

	// Register headers
	a.ext.Headers().Add("PICK_ALL", g.Header{Dir: g.Out, Value: 401})
	a.ext.Headers().Add("GOTOFLAT", g.Header{Dir: g.Out, Value: 123})
	a.ext.Headers().Add("GETSTRIP", g.Header{Dir: g.Out, Value: 65})
	a.ext.Headers().Add("STRIPINFO_2", g.Header{Dir: g.In, Value: 140})
	a.ext.Headers().Add("PLACESTUFF", g.Header{Dir: g.Out, Value: 90})
	a.ext.Headers().Add("PLACEITEM", g.Header{Dir: g.Out, Value: 92})
	a.ext.Headers().Add("SHOUT", g.Header{Dir: g.Out, Value: 52})
	a.ext.Headers().Add("QUIT", g.Header{Dir: g.Out, Value: 53})

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

	// Start background schedule monitor
	go a.monitorSchedule()

	a.AddLog("Extension registered. Waiting for connection...")
	go a.ext.Run()
}

func (a *App) monitorSchedule() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		a.scheduleMu.Lock()
		if !a.scheduleEnabled {
			a.scheduleMu.Unlock()
			continue
		}

		if time.Now().After(a.targetTime) {
			a.scheduleEnabled = false
			a.AddLog(fmt.Sprintf(">>> SCHEDULE TRIGGERED at %s! <<<", time.Now().Format("15:04:05")))
			a.scheduleMu.Unlock()

			// Trigger full sequence with DB check
			go a.ExecuteCommands()
		} else {
			a.scheduleMu.Unlock()
		}
	}
}

func (a *App) SetSchedule(hours int, minutes int, enabled bool) {
	a.scheduleMu.Lock()
	defer a.scheduleMu.Unlock()

	if !enabled {
		a.scheduleEnabled = false
		a.AddLog("Schedule Disabled.")
		return
	}

	offset := time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute
	if offset <= 0 {
		a.AddLog("ERROR: Schedule time must be greater than 0.")
		return
	}

	a.targetTime = time.Now().Add(offset)
	a.scheduleEnabled = true
	a.AddLog(fmt.Sprintf("Schedule Enabled! Bot will trigger in %dh %dm (at %s)", hours, minutes, a.targetTime.Format("15:04:05")))
}

type ScheduleStatus struct {
	TargetUnix int64 `json:"targetUnix"`
	Enabled    bool  `json:"enabled"`
}

func (a *App) GetScheduleStatus() ScheduleStatus {
	a.scheduleMu.Lock()
	defer a.scheduleMu.Unlock()

	unix := int64(0)
	if !a.targetTime.IsZero() {
		unix = a.targetTime.Unix()
	}

	return ScheduleStatus{
		TargetUnix: unix,
		Enabled:    a.scheduleEnabled,
	}
}

// --- Pre-Closure DB Logic ---

func (a *App) checkActiveGames() ([]string, error) {
	if a.dbPool == nil {
		return nil, fmt.Errorf("database not connected")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := a.dbPool.Query(ctx, "SELECT DISTINCT player_name FROM public.banker_trades WHERE status != 'completed'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var players []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			players = append(players, name)
		}
	}
	return players, nil
}

func (a *App) Shout(msg string) {
	if a.ext == nil {
		return
	}
	// SHOUT format: [Header][Msg]
	a.ext.Send(g.Out.Id("SHOUT"), msg)
}

// --- Hand Scan Logic ---

func (a *App) handleStripPacket(e *g.Intercept) {
	a.stripScanMu.Lock()
	if !a.stripScanActive {
		a.stripScanMu.Unlock()
		return
	}
a.stripScanPageCount++
a.stripScanLastPacketAt = time.Now()
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
	if pageRecords == 0 || pageRepeated || pageLimitReached {
		a.stripScanActive = false
		a.finalizeHandScan(scanID)
		a.stripScanMu.Unlock()
		return
	}

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
			pos++
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
		readVL64()
		if pos < len(data) {
			pos++
		}
		skipUntilDelim()
		readVL64()
		readVL64()
		readVL64()
		start := pos
		for pos < len(data) && data[pos] != 0x02 {
			pos++
		}
		className := string(data[start:pos])
		if pos < len(data) {
			pos++
		}
		classQtys[className] += len(itemIDs)
		classItemIDs[className] = append(classItemIDs[className], itemIDs...)
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

// --- Internal Sequence Control ---

func (a *App) runAutoDropLogic() {
	currentX := 1
	currentY := 1
	wallPass := false

	for {
		a.stripScanMu.Lock()
		allItemIDs := []int{}
		for _, ids := range a.stripScanItemIDs {
			for _, id := range ids {
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
				if currentY > 26 {
					a.AddLog("[Pass] Floor grid is full. Switching to Wall Pass...")
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
				prefix := "@P"
				if (i/10)%2 == 1 {
					prefix = "@Q"
				}
				wallPos := fmt.Sprintf("%s:w=1,0 l=%d,%d r", prefix, (i%15)+5, (i/15)+20)
				a.AddLog(fmt.Sprintf("[%d/%d] Wall: Item %d at %s", i+1, len(allItemIDs), itemID, wallPos))
				idBuf := make([]byte, gencoding.VL64EncodeLen(itemID))
				gencoding.VL64Encode(idBuf, itemID)
				payload := append([]byte{}, idBuf...)
				payload = append(payload, []byte(wallPos)...)
				a.ext.Send(g.Out.Id("PLACEITEM"), payload)
			}
			time.Sleep(800 * time.Millisecond)
		}

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
			wallPass = true
		} else if remainingNewCount > 0 && wallPass {
			wallPass = false
			currentX = 1
			currentY = 1
		} else {
			a.AddLog(">>> SEQUENCE COMPLETED: Hand is empty! <<<")
			// Step 6: QUIT to Main Menu
			a.AddLog("Exiting room to Main Menu...")
			// Header 53 (QUIT), Data: @u (which is 0x40 0x75)
			a.ext.Send(g.Out.Id("QUIT"), []byte{0x40, 0x75})
			return
		}
		time.Sleep(1 * time.Second)
	}
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

		// --- DB CHECK LOOP ---
		a.AddLog(">>> [Pre-Flight] Checking for active games in database...")
		for {
			players, err := a.checkActiveGames()
			if err != nil {
				a.AddLog(fmt.Sprintf("DB ERROR: %v. Proceeding cautiously...", err))
				break
			}
			if len(players) == 0 {
				a.AddLog("No active games found. Safe to proceed.")
				break
			}
			// Active games found!
			playerList := strings.Join(players, ", ")
			msg := fmt.Sprintf("%s, casino is closing please finish up your games.", playerList)
			a.AddLog(fmt.Sprintf("[GUARD] Active players detected: %s. Shouting warning...", playerList))
			a.Shout(msg)
			
			a.AddLog("Waiting 30 seconds for games to finish...")
			time.Sleep(30 * time.Second)
		}

		// Final 30 second countdown
		a.AddLog(">>> [Pre-Flight] All games finished. Final 30 second warning...")
		a.Shout("Casino will be closing in 30 secs")
		time.Sleep(30 * time.Second)

		// 1. INITIAL SCAN
		a.AddLog(">>> [Step 1/6] Scanning hand to ignore existing items...")
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
		a.AddLog(fmt.Sprintf("Found %d items to ignore.", initialCount))

		// 2. PICK ALL
		a.AddLog(">>> [Step 2/6] Sending PICK_ALL...")
		a.ExecutePickAll()
		time.Sleep(10 * time.Second)

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
			a.AddLog("WARNING: No new items detected. Sequence stopping.")
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
