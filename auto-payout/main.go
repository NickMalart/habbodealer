package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	g "xabbo.b7c.io/goearth"
	gencoding "xabbo.b7c.io/goearth/encoding"
	"xabbo.b7c.io/goearth/shockwave/in"
)

//go:embed all:frontend/dist
var assets embed.FS

// Payout represents a single delivery task
type Payout struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ItemName  string `json:"itemName"`
	Quantity  int    `json:"quantity"`
	Status    string `json:"status"` // "Pending", "In Room", "Trading", "Completed", "Failed", "Disabled"
	CreatedAt string `json:"createdAt"`
}

type ParsedUsers28User struct {
	Username string `json:"username"`
	TradeID  int    `json:"trade_id"`
	ChatID   int    `json:"chat_id"`
	TokenHex string `json:"token_hex"`
}



type App struct {
	ctx     context.Context
	ext     *g.Ext
	pMu     sync.RWMutex
	payouts []Payout

	// Logs
	logs   []string
	logsMu sync.Mutex

	// Room state
	roomUsers   map[string]ParsedUsers28User
	roomUsersMu sync.RWMutex

	// Inventory (Strip) Scan state
	stripScanActive      bool
	stripScanSessionID   int
	stripScanSeenItemIDs map[int]struct{}
	stripScanItemIDs     map[string][]int
	stripScanMu          sync.Mutex

	// Inventory final state
	inventory   map[string][]int // name -> list of strip IDs
	inventoryMu sync.RWMutex

	// Trade state
	activeTradePartner string
	activeTradeTarget  int
	tradeActive        bool
	tradeAccepted      bool
	tradeMu            sync.Mutex

	// Config & DB
	pythonExec   string
	parserScript string
	db           *pgxpool.Pool
	dbConnString string
}

func NewApp() *App {
	return &App{
		payouts:      []Payout{},
		logs:         []string{"Bot initialized..."},
		roomUsers:    make(map[string]ParsedUsers28User),
		inventory:    make(map[string][]int),
		pythonExec:   "python",
		dbConnString: "postgresql://neondb_owner:npg_Jx8ERGzK6eog@ep-small-thunder-a7ceewoj-pooler.ap-southeast-2.aws.neon.tech/neondb?sslmode=require&channel_binding=require",
		stripScanSeenItemIDs: make(map[int]struct{}),
		stripScanItemIDs:     make(map[string][]int),
	}
}

func (a *App) AddLog(msg string) {
	fullMsg := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), msg)
	log.Println(fullMsg)
	a.logsMu.Lock()
	a.logs = append(a.logs, fullMsg)
	if len(a.logs) > 50 {
		a.logs = a.logs[len(a.logs)-50:]
	}
	a.logsMu.Unlock()
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "logsUpdate", a.GetLogs())
	}
}

func (a *App) GetLogs() []string {
	a.logsMu.Lock()
	defer a.logsMu.Unlock()
	return a.logs
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.initDatabase()
	a.loadPayoutsFromDB()
	a.initParser()

	a.ext = g.NewExt(g.ExtInfo{
		Title:       "Auto Payout Bot",
		Description: "Automatically trades items to people when they join the room",
		Version:     "1.0.0",
		Author:      "Gemini CLI",
	})

	// Register custom headers for this version of goearth
	// 140 and 98 are STRIPINFO for Origins/Shockwave
	a.ext.Headers().Add("STRIPINFO_IN", g.Header{Dir: g.In, Value: 140})
	a.ext.Headers().Add("STRIPINFO_98_IN", g.Header{Dir: g.In, Value: 98})
	a.ext.Headers().Add("TRADE_OPEN_IN", g.Header{Dir: g.In, Value: 104})
	a.ext.Headers().Add("TRADE_CLOSE_IN", g.Header{Dir: g.In, Value: 110})
	a.ext.Headers().Add("TRADE_COMPLETED_IN", g.Header{Dir: g.In, Value: 112})
	
	a.ext.Headers().Add("GETSTRIP_OUT", g.Header{Dir: g.Out, Value: 65})
	a.ext.Headers().Add("TRADE_OPEN_OUT", g.Header{Dir: g.Out, Value: 71})
	a.ext.Headers().Add("TRADE_ADDITEM_OUT", g.Header{Dir: g.Out, Value: 72})
	a.ext.Headers().Add("TRADE_ACCEPT_OUT", g.Header{Dir: g.Out, Value: 69})

	a.ext.Intercept(in.USERS, in.SPACENODEUSERS).With(a.handleRoomUsers)
	a.ext.Intercept(g.In.Id("STRIPINFO_IN"), g.In.Id("STRIPINFO_98_IN")).With(a.handleStripInfo)
	a.ext.Intercept(g.In.Id("TRADE_OPEN_IN")).With(a.handleTradeOpen)
	a.ext.Intercept(g.In.Id("TRADE_CLOSE_IN")).With(a.handleTradeClose)
	a.ext.Intercept(g.In.Id("TRADE_COMPLETED_IN")).With(a.handleTradeCompleted)

	a.ext.Activated(func() {
		a.ShowWindow()
	})

	a.AddLog("Extension registered. Waiting for connection...")
	go a.ext.Run()
	go a.payoutMonitor()
}

func (a *App) initDatabase() {
	a.AddLog("Connecting to database...")
	config, err := pgxpool.ParseConfig(a.dbConnString)
	if err != nil {
		a.AddLog("ERROR: Database config parse failed: " + err.Error())
		return
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		a.AddLog("ERROR: Database connection failed: " + err.Error())
		return
	}

	a.db = pool
	
	// Create table if not exists
	query := `CREATE TABLE IF NOT EXISTS auto_payouts (
		id TEXT PRIMARY KEY,
		player_name TEXT NOT NULL,
		item_name TEXT NOT NULL,
		quantity INTEGER NOT NULL,
		status TEXT NOT NULL,
		created_at TEXT NOT NULL
	);`
	_, err = a.db.Exec(context.Background(), query)
	if err != nil {
		a.AddLog("ERROR: Table creation failed: " + err.Error())
	} else {
		a.AddLog("Database connected and ready.")
	}
}

func (a *App) ShowWindow() {
	if a.ctx != nil {
		runtime.WindowShow(a.ctx)
	}
}

func (a *App) initParser() {
	if p, err := exec.LookPath("python3"); err == nil {
		a.pythonExec = p
	} else if p, err := exec.LookPath("python"); err == nil {
		a.pythonExec = p
	}

	candidates := []string{
		filepath.Join("scripts", "parse_users28.py"),
		filepath.Join("..", "scripts", "parse_users28.py"),
		"C:\\Users\\Dubbo\\habbodealer\\habbodealer\\scripts\\parse_users28.py",
	}

	for _, cand := range candidates {
		if _, err := os.Stat(cand); err != nil {
			continue
		}
		abs, _ := filepath.Abs(cand)
		a.parserScript = abs
		a.AddLog("Parser found: " + abs)
		return
	}
	a.AddLog("ERROR: parse_users28.py NOT FOUND. Detection will not work.")
}

// --- Wails Methods ---

func (a *App) GetPayouts() []Payout {
	a.pMu.RLock()
	defer a.pMu.RUnlock()
	return a.payouts
}

func (a *App) AddPayout(name, itemName string, qty int) {
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	createdAt := time.Now().Format("2006-01-02 15:04:05")
	p := Payout{
		ID:        id,
		Name:      strings.TrimSpace(name),
		ItemName:  strings.TrimSpace(strings.ToLower(itemName)),
		Quantity:  qty,
		Status:    "Pending",
		CreatedAt: createdAt,
	}

	a.AddLog(fmt.Sprintf("Adding payout: %s x %d %s", p.Name, p.Quantity, p.ItemName))

	if a.db != nil {
		_, err := a.db.Exec(context.Background(), 
			"INSERT INTO auto_payouts (id, player_name, item_name, quantity, status, created_at) VALUES ($1, $2, $3, $4, $5, $6)",
			p.ID, p.Name, p.ItemName, p.Quantity, p.Status, p.CreatedAt)
		if err != nil {
			a.AddLog("ERROR: DB Save failed: " + err.Error())
		}
	}

	a.pMu.Lock()
	a.payouts = append(a.payouts, p)
	a.pMu.Unlock()
	a.emitUpdate()
}

func (a *App) DeletePayout(id string) {
	if a.db != nil {
		_, err := a.db.Exec(context.Background(), "DELETE FROM auto_payouts WHERE id = $1", id)
		if err != nil {
			a.AddLog("ERROR: DB Delete failed: " + err.Error())
		}
	}

	a.pMu.Lock()
	newPayouts := []Payout{}
	found := false
	for _, p := range a.payouts {
		if p.ID != id {
			newPayouts = append(newPayouts, p)
		} else {
			found = true
		}
	}
	if found {
		a.payouts = newPayouts
		a.pMu.Unlock()
		a.emitUpdate()
		a.AddLog("Entry deleted.")
	} else {
		a.pMu.Unlock()
	}
}

func (a *App) TogglePayoutStatus(id string) {
	a.pMu.Lock()
	defer a.pMu.Unlock()
	
	for i, p := range a.payouts {
		if p.ID == id {
			newStatus := "Pending"
			if p.Status == "Pending" || p.Status == "In Room" {
				newStatus = "Disabled"
			}
			a.payouts[i].Status = newStatus
			
			if a.db != nil {
				a.db.Exec(context.Background(), "UPDATE auto_payouts SET status = $1 WHERE id = $2", newStatus, id)
			}
			break
		}
	}
	a.emitUpdate()
}

func (a *App) ClearCompleted() {
	if a.db != nil {
		a.db.Exec(context.Background(), "DELETE FROM auto_payouts WHERE status = 'Completed'")
	}

	a.pMu.Lock()
	newPayouts := []Payout{}
	for _, p := range a.payouts {
		if p.Status != "Completed" {
			newPayouts = append(newPayouts, p)
		}
	}
	a.payouts = newPayouts
	a.pMu.Unlock()
	a.emitUpdate()
	a.AddLog("Completed entries cleared.")
}

func (a *App) RefreshInventory() {
	if a.ext != nil {
		a.stripScanMu.Lock()
		a.stripScanActive = true
		sessionID := a.stripScanSessionID + 1
		a.stripScanSessionID = sessionID
		a.stripScanSeenItemIDs = make(map[int]struct{})
		a.stripScanItemIDs = make(map[string][]int)
		a.stripScanMu.Unlock()

		a.AddLog("Requesting hand inventory...")
		a.ext.Send(g.Out.Id("GETSTRIP_OUT"), "new")

		go func() {
			time.Sleep(2 * time.Second) // 2 second timeout for inventory scan
			a.finalizeStripScan(sessionID)
		}()
	}
}

func (a *App) ReturnAllToOwner(ownerName string) {
	a.inventoryMu.RLock()
	defer a.inventoryMu.RUnlock()

	count := 0
	for name, ids := range a.inventory {
		if len(ids) > 0 {
			a.AddPayout(ownerName, name, len(ids))
			count++
		}
	}
	if count > 0 {
		a.AddLog(fmt.Sprintf("Queued %d return tasks to %s", count, ownerName))
	} else {
		a.AddLog("Nothing to return - hand is empty.")
	}
}

// --- Internal Logic ---

func (a *App) loadPayoutsFromDB() {
	if a.db == nil {
		return
	}
	rows, err := a.db.Query(context.Background(), "SELECT id, player_name, item_name, quantity, status, created_at FROM auto_payouts")
	if err != nil {
		a.AddLog("ERROR: DB Load failed: " + err.Error())
		return
	}
	defer rows.Close()

	var payouts []Payout
	for rows.Next() {
		var p Payout
		err := rows.Scan(&p.ID, &p.Name, &p.ItemName, &p.Quantity, &p.Status, &p.CreatedAt)
		if err == nil {
			payouts = append(payouts, p)
		}
	}
	
	a.pMu.Lock()
	a.payouts = payouts
	a.pMu.Unlock()
	a.AddLog(fmt.Sprintf("Loaded %d payouts from database.", len(payouts)))
}

func (a *App) emitUpdate() {
	runtime.EventsEmit(a.ctx, "payoutsUpdate", a.GetPayouts())
}

func (a *App) handleRoomUsers(e *g.Intercept) {
	headerName := "USERS"
	if e.Packet.Header.Value != 28 {
		headerName = "SPACENODEUSERS"
	}
	a.AddLog(fmt.Sprintf("Intercepted %s packet (len: %d).", headerName, len(e.Packet.Data)))

	if a.parserScript == "" {
		a.AddLog("ERROR: Parser script path is empty.")
		return
	}

	tmpFile, err := os.CreateTemp("", "users28_*.bin")
	if err != nil {
		a.AddLog("ERROR: Failed to create temp file: " + err.Error())
		return
	}
	tmpPath := tmpFile.Name()

	_, err = tmpFile.Write(e.Packet.Data)
	tmpFile.Close()
	if err != nil {
		a.AddLog("ERROR: Failed to write to temp file: " + err.Error())
		os.Remove(tmpPath)
		return
	}

	go func(path string) {
		defer os.Remove(path)

		cmd := exec.Command(a.pythonExec, a.parserScript, "--input", path, "--json")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			a.AddLog("ERROR: Parser execution failed: " + err.Error())
			a.AddLog("Stderr: " + stderr.String())
			return
		}

		var users []ParsedUsers28User
		if err := json.Unmarshal(stdout.Bytes(), &users); err != nil {
			a.AddLog("ERROR: Failed to parse JSON from parser.")
			return
		}

		a.roomUsersMu.Lock()
		for _, u := range users {
			a.roomUsers[strings.ToLower(u.Username)] = u
		}
		a.roomUsersMu.Unlock()

		a.updatePayoutStatuses()
	}(tmpPath)
}

func (a *App) updatePayoutStatuses() {
	a.pMu.Lock()
	defer a.pMu.Unlock()
	a.roomUsersMu.RLock()
	defer a.roomUsersMu.RUnlock()

	changed := false
	for i, p := range a.payouts {
		if p.Status == "Completed" || p.Status == "Disabled" {
			continue
		}
		
		user, ok := a.roomUsers[strings.ToLower(p.Name)]
		if ok {
			if p.Status == "Pending" || p.Status == "Failed" {
				a.AddLog(fmt.Sprintf("Target detected: %s (TradeID: %d)", p.Name, user.TradeID))
				a.payouts[i].Status = "In Room"
				changed = true
			}
		} else {
			if p.Status == "In Room" {
				a.AddLog(fmt.Sprintf("Target left room: %s", p.Name))
				a.payouts[i].Status = "Pending"
				changed = true
			}
		}
	}
	if changed {
		a.emitUpdate()
	}
}

func (a *App) handleStripInfo(e *g.Intercept) {
	a.stripScanMu.Lock()
	if !a.stripScanActive {
		a.stripScanMu.Unlock()
		return
	}
	sessionID := a.stripScanSessionID
	data := e.Packet.Data
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
		a.stripScanMu.Unlock()
		return
	}

	pageRepeated := false
	for i := 0; i < count; i++ {
		if pos >= len(data) {
			break
		}

		mainID, ok := readVL64()
		if !ok { break }
		
		// If we've seen this item, we've wrapped around
		if i == 0 {
			if _, seen := a.stripScanSeenItemIDs[mainID]; seen {
				pageRepeated = true
			} else {
				a.stripScanSeenItemIDs[mainID] = struct{}{}
			}
		}

		extraCount, _ := readVL64()
		extraIDs := make([]int, 0, extraCount)
		for j := 0; j < extraCount; j++ {
			if extraID, ok := readVL64(); ok {
				extraIDs = append(extraIDs, extraID)
			}
		}

		readVL64() // Pos
		typeChar := data[pos]
		pos++
		skipUntilDelim()

		readVL64() // templateId
		readVL64() // extra field 0
		readVL64() // extra field 1

		classStart := pos
		for pos < len(data) && data[pos] != 0x02 {
			pos++
		}
		classRaw := strings.ToLower(string(data[classStart:pos]))
		if pos < len(data) {
			pos++
		}

		switch typeChar {
		case 'S':
			readVL64(); readVL64(); skipUntilDelim()
		case 'I':
			skipUntilDelim()
		default:
			skipUntilDelim()
		}

		if !pageRepeated {
			a.stripScanItemIDs[classRaw] = append(a.stripScanItemIDs[classRaw], mainID)
			a.stripScanItemIDs[classRaw] = append(a.stripScanItemIDs[classRaw], extraIDs...)
		}
	}
	a.stripScanMu.Unlock()

	if pageRepeated {
		a.finalizeStripScan(sessionID)
	} else {
		// Request next page asynchronously to avoid blocking the packet thread
		go func() {
			time.Sleep(200 * time.Millisecond)
			a.ext.Send(g.Out.Id("GETSTRIP_OUT"), "next")
		}()
	}
}

func (a *App) finalizeStripScan(sessionID int) {
	a.stripScanMu.Lock()
	if !a.stripScanActive || a.stripScanSessionID != sessionID {
		a.stripScanMu.Unlock()
		return
	}
	
	finalInventory := make(map[string][]int)
	for name, ids := range a.stripScanItemIDs {
		finalInventory[name] = ids
	}
	
	a.stripScanActive = false
	a.stripScanMu.Unlock()

	a.inventoryMu.Lock()
	a.inventory = finalInventory
	a.inventoryMu.Unlock()
	
	a.AddLog(fmt.Sprintf("Hand scanning complete. Found %d unique item types.", len(finalInventory)))
}

func (a *App) handleTradeOpen(e *g.Intercept) {
	a.tradeMu.Lock()
	a.tradeActive = true
	a.tradeAccepted = false
	a.tradeMu.Unlock()
	a.AddLog("Trade window opened.")
}

func (a *App) handleTradeClose(e *g.Intercept) {
	a.tradeMu.Lock()
	partner := a.activeTradePartner
	a.tradeActive = false
	a.activeTradePartner = ""
	a.tradeMu.Unlock()

	if partner != "" {
		a.pMu.Lock()
		for i, p := range a.payouts {
			if strings.EqualFold(p.Name, partner) && p.Status == "Trading" {
				a.payouts[i].Status = "In Room"
				a.AddLog("Trade closed prematurely. Re-queueing...")
			}
		}
		a.pMu.Unlock()
		a.emitUpdate()
	} else {
		a.AddLog("Trade closed.")
	}
}

func (a *App) handleTradeCompleted(e *g.Intercept) {
	a.tradeMu.Lock()
	partner := a.activeTradePartner
	a.tradeActive = false
	a.activeTradePartner = ""
	a.tradeMu.Unlock()

	if partner != "" {
		a.pMu.Lock()
		for i, p := range a.payouts {
			if strings.EqualFold(p.Name, partner) && p.Status == "Trading" {
				a.payouts[i].Status = "Completed"
				if a.db != nil {
					a.db.Exec(context.Background(), "UPDATE auto_payouts SET status = 'Completed' WHERE id = $1", p.ID)
				}
				a.AddLog(fmt.Sprintf("Payout for %s SUCCESSFUL.", partner))
			}
		}
		a.pMu.Unlock()
		a.emitUpdate()
	}
}

func (a *App) payoutMonitor() {
	for {
		time.Sleep(2 * time.Second)

		a.pMu.RLock()
		var target *Payout
		for _, p := range a.payouts {
			if p.Status == "In Room" {
				target = &p
				break
			}
		}
		a.pMu.RUnlock()

		if target != nil {
			a.tradeMu.Lock()
			if !a.tradeActive {
				a.roomUsersMu.RLock()
				user, ok := a.roomUsers[strings.ToLower(target.Name)]
				a.roomUsersMu.RUnlock()

				if ok && user.TradeID > 0 {
					a.activeTradePartner = target.Name
					a.activeTradeTarget = user.TradeID
					
					a.pMu.Lock()
					for i, p := range a.payouts {
						if p.ID == target.ID {
							a.payouts[i].Status = "Trading"
						}
					}
					a.pMu.Unlock()
					a.emitUpdate()

					a.AddLog(fmt.Sprintf("Initiating auto-trade for %s...", target.Name))
					a.ext.Send(g.Out.Id("TRADE_OPEN_OUT"), user.TradeID)
					
					go a.automateTrade(target)
				}
			}
			a.tradeMu.Unlock()
		}
	}
}

func (a *App) automateTrade(p *Payout) {
	time.Sleep(2500 * time.Millisecond) // Wait for trade window
	
	a.tradeMu.Lock()
	if !a.tradeActive { return }
	a.tradeMu.Unlock()

	a.inventoryMu.RLock()
	ids, ok := a.inventory[strings.ToLower(p.ItemName)]
	inventoryCount := len(ids)
	a.inventoryMu.RUnlock()

	if !ok || inventoryCount == 0 {
		a.AddLog(fmt.Sprintf("ERROR: Inventory shortage for '%s'. Refresh hand or restock.", p.ItemName))
		return
	}

	toAdd := p.Quantity
	if inventoryCount < toAdd {
		a.AddLog(fmt.Sprintf("WARNING: Only have %d of %s. Trading all.", inventoryCount, p.ItemName))
		toAdd = inventoryCount
	}

	a.AddLog(fmt.Sprintf("Adding %d x %s...", toAdd, p.ItemName))
	for i := 0; i < toAdd; i++ {
		a.tradeMu.Lock()
		if !a.tradeActive { return }
		a.tradeMu.Unlock()

		itemID := ids[i]
		a.ext.Send(g.Out.Id("TRADE_ADDITEM_OUT"), itemID)
		time.Sleep(600 * time.Millisecond)
	}

	a.AddLog("Finalizing trade...")
	time.Sleep(1000 * time.Millisecond)
	a.ext.Send(g.Out.Id("TRADE_ACCEPT_OUT"))
}

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:  "Auto Payout Bot",
		Width:  950,
		Height: 700,
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
