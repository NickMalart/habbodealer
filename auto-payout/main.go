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

type Payout struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	ItemName       string `json:"itemName"`
	Quantity       int    `json:"quantity"`
	Status         string `json:"status"`
	CreatedAt      string `json:"createdAt"`
	BankerTradeID  int    `json:"bankerTradeId"`
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

	logs   []string
	logsMu sync.Mutex

	roomUsers   map[string]ParsedUsers28User
	roomUsersMu sync.RWMutex

	stripScanActive      bool
	stripScanSessionID   int
	stripScanSeenItemIDs map[int]struct{}
	stripScanItemIDs     map[string][]int
	stripScanMu          sync.Mutex

	inventory   map[string][]int
	inventoryMu sync.RWMutex

	activeTradePartner string
	lastActiveTradePartner string
	activeTradeTarget  int
	tradeActive        bool
	tradeAccepted      bool
	tradeCompleted     bool
	payoutPending      bool
	lastPayoutTime     time.Time
	lastScreenshotPath string
	tradeMu            sync.Mutex

	pythonExec     string
	parserScript   string
	db             *pgxpool.Pool
	dbConnString   string
	discordWebhook string

	recentOutgoingMu sync.Mutex
	recentOutgoing   []int

	botName    string
	ownChatID  int
	ownTradeID int
}

func NewApp() *App {
	return &App{
		payouts:      []Payout{},
		logs:         []string{"Bot initialized..."},
		roomUsers:    make(map[string]ParsedUsers28User),
		inventory:    make(map[string][]int),
		pythonExec:   "python",
		dbConnString: "postgresql://neondb_owner:npg_Jx8ERGzK6eog@ep-small-thunder-a7ceewoj-pooler.ap-southeast-2.aws.neon.tech/neondb?sslmode=require&channel_binding=require",
		discordWebhook: "https://discordapp.com/api/webhooks/1505787297696583800/aUE_M4-quy6wkFs0qVySjHgZq3zYOze5watr67D89e6O1V9VwmjNy24HzN-X7TI5G5k3",
		stripScanSeenItemIDs: make(map[int]struct{}),
		stripScanItemIDs:     make(map[string][]int),
	}
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
		Author:      "Dubbo",
	})

	a.ext.Headers().Add("STRIPINFO_IN", g.Header{Dir: g.In, Value: 140})
	a.ext.Headers().Add("STRIPINFO_98_IN", g.Header{Dir: g.In, Value: 98})
	a.ext.Headers().Add("TRADE_OPEN_IN", g.Header{Dir: g.In, Value: 104})
	a.ext.Headers().Add("TRADE_ACCEPT_IN", g.Header{Dir: g.In, Value: 109})
	a.ext.Headers().Add("TRADE_CONFIRM_IN", g.Header{Dir: g.In, Value: 111})
	a.ext.Headers().Add("TRADE_CLOSE_IN", g.Header{Dir: g.In, Value: 110})
	a.ext.Headers().Add("TRADE_COMPLETED_IN", g.Header{Dir: g.In, Value: 112})
	
	a.ext.Headers().Add("GETSTRIP_OUT", g.Header{Dir: g.Out, Value: 65})
	a.ext.Headers().Add("TRADE_OPEN_OUT", g.Header{Dir: g.Out, Value: 71})
	a.ext.Headers().Add("TRADE_ADDITEM_OUT", g.Header{Dir: g.Out, Value: 72})
	a.ext.Headers().Add("TRADE_ACCEPT_OUT", g.Header{Dir: g.Out, Value: 69})
	a.ext.Headers().Add("TRADE_CONFIRM_ACCEPT_OUT", g.Header{Dir: g.Out, Value: 402})
	a.ext.Headers().Add("G_USRS_OUT", g.Header{Dir: g.Out, Value: 61})
	a.ext.Headers().Add("GETSPACENODEUSERS_OUT", g.Header{Dir: g.Out, Value: 126})

	a.ext.Intercept(in.USERS, in.SPACENODEUSERS).With(a.handleRoomUsers)
	a.ext.Intercept(g.In.Id("STRIPINFO_IN"), g.In.Id("STRIPINFO_98_IN")).With(a.handleStripInfo)
	a.ext.Intercept(g.In.Id("TRADE_OPEN_IN")).With(a.handleTradeOpen)
	a.ext.Intercept(g.In.Id("TRADE_ACCEPT_IN")).With(a.handlePartnerAccept)
	a.ext.Intercept(g.In.Id("TRADE_CONFIRM_IN")).With(a.handlePartnerConfirm)
	a.ext.Intercept(g.In.Id("TRADE_CLOSE_IN")).With(a.handleTradeClose)
	a.ext.Intercept(g.In.Id("TRADE_COMPLETED_IN")).With(a.handleTradeCompleted)

	a.ext.Activated(func() {
		a.AddLog("Extension activated via G-Earth.")
		a.ShowWindow()
	})

	a.AddLog("Extension registered. Waiting for connection...")
	go a.ext.Run()
	go a.payoutMonitor()
	go a.dbMonitor()
	
	// Ensure window is shown initially
	go func() {
		time.Sleep(2 * time.Second)
		a.ShowWindow()
	}()
}

// --- Wails Methods ---

func (a *App) Ping() string {
	a.AddLog("UI Ping: pong")
	return "Pong"
}

func (a *App) GetPayouts() []Payout {
	a.pMu.RLock()
	defer a.pMu.RUnlock()
	return a.payouts
}

func (a *App) GetLogs() []string {
	a.logsMu.Lock()
	defer a.logsMu.Unlock()
	return a.logs
}

func normalizeTradeItemName(raw string) (string, bool) {
	// Shockwave protocol heuristic: the actual item class name usually 
	// follows the last 'H' in the raw descriptor string.
	if idx := strings.LastIndex(raw, "H"); idx != -1 {
		raw = raw[idx+1:]
	}

	name := strings.TrimSpace(strings.ToLower(raw))
	name = strings.Trim(name, "\x00\r\n\t")

	if name == "" {
		return "", false
	}
	return name, true
}

func normalizeClassKeyWithVariant(raw string) (string, bool) {
	return normalizeTradeItemName(raw)
}

func (a *App) AddPayout(name, itemName string, qty int) {
	a.AddLog(fmt.Sprintf("UI AddPayout: %s x %d %s", name, qty, itemName))
	
	normItem, ok := normalizeClassKeyWithVariant(itemName)
	if !ok {
		normItem = strings.TrimSpace(strings.ToLower(itemName))
	}
	
	p := Payout{ID: fmt.Sprintf("%d", time.Now().UnixNano()), Name: strings.TrimSpace(name), ItemName: normItem, Quantity: qty, Status: "Pending", CreatedAt: time.Now().Format("2006-01-02 15:04:05")}
	if a.db != nil { 
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := a.db.Exec(ctx, "INSERT INTO auto_payouts (id, player_name, item_name, quantity, status, created_at) VALUES ($1, $2, $3, $4, $5, $6)", p.ID, p.Name, p.ItemName, p.Quantity, p.Status, p.CreatedAt) 
		if err != nil { a.AddLog("ERROR: DB insert failed: " + err.Error()) }
	}
	a.pMu.Lock()
	a.payouts = append(a.payouts, p)
	a.pMu.Unlock()
	a.emitUpdate()
}

func (a *App) DeletePayout(id string) {
	a.AddLog("UI DeletePayout: " + id)
	if a.db != nil { 
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		a.db.Exec(ctx, "DELETE FROM auto_payouts WHERE id = $1", id) 
	}
	a.pMu.Lock()
	newPayouts := []Payout{}
	for _, p := range a.payouts { if p.ID != id { newPayouts = append(newPayouts, p) } }
	a.payouts = newPayouts
	a.pMu.Unlock()
	a.emitUpdate()
}

func (a *App) TogglePayoutStatus(id string) {
	a.AddLog("UI ToggleStatus: " + id)
	a.pMu.Lock()
	defer a.pMu.Unlock()
	for i, p := range a.payouts {
		if p.ID == id {
			newStatus := "Pending"
			if p.Status != "Disabled" { newStatus = "Disabled" }
			a.payouts[i].Status = newStatus
			if a.db != nil { 
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				a.db.Exec(ctx, "UPDATE auto_payouts SET status = $1 WHERE id = $2", newStatus, id) 
			}
			break
		}
	}
	a.emitUpdate()
}

func (a *App) ClearCompleted() {
	a.AddLog("UI ClearCompleted")
	if a.db != nil { 
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		a.db.Exec(ctx, "DELETE FROM auto_payouts WHERE status = 'Completed'") 
	}
	a.pMu.Lock()
	newPayouts := []Payout{}
	for _, p := range a.payouts { if p.Status != "Completed" { newPayouts = append(newPayouts, p) } }
	a.payouts = newPayouts
	a.pMu.Unlock()
	a.emitUpdate()
}

func (a *App) RefreshQueue() {
	a.AddLog("UI RefreshQueue")
	a.loadPayoutsFromDB()
}

func (a *App) RefreshInventory() {
	a.AddLog("UI RefreshInventory")
	if a.ext != nil {
		a.stripScanMu.Lock()
		a.stripScanActive = true
		sessionID := a.stripScanSessionID + 1
		a.stripScanSessionID = sessionID
		a.stripScanSeenItemIDs = make(map[int]struct{})
		a.stripScanItemIDs = make(map[string][]int)
		a.stripScanMu.Unlock()
		a.AddLog("Requesting hand inventory scan...")
		a.ext.Send(g.Out.Id("GETSTRIP_OUT"), "new")
		go func() {
			time.Sleep(2 * time.Second)
			a.finalizeStripScan(sessionID)
		}()
	} else {
		a.AddLog("ERROR: Bot not connected, cannot scan.")
	}
}

func (a *App) ReturnAllToOwner(ownerName string) {
	a.AddLog("UI ReturnAllToOwner: " + ownerName)
	a.inventoryMu.RLock()
	defer a.inventoryMu.RUnlock()
	count := 0
	for name, ids := range a.inventory {
		if len(ids) > 0 { 
			a.AddPayout(ownerName, name, len(ids))
			count++
		}
	}
	a.AddLog(fmt.Sprintf("Queued %d items for return.", count))
}

// --- Internal Logic ---

func (a *App) ShowWindow() {
	if a.ctx != nil {
		runtime.WindowShow(a.ctx)
	}
}

func (a *App) initDatabase() {
	a.AddLog("Connecting to database...")
	pool, err := pgxpool.New(context.Background(), a.dbConnString)
	if err != nil {
		a.AddLog("ERROR: Database connection failed: " + err.Error())
		return
	}
	a.db = pool
	a.AddLog("Database connected.")
}

func (a *App) initParser() {
	if p, err := exec.LookPath("python3"); err == nil { a.pythonExec = p } else { a.pythonExec, _ = exec.LookPath("python") }
	candidates := []string{
		filepath.Join("scripts", "parse_users28.py"),
		filepath.Join("..", "scripts", "parse_users28.py"),
		"C:\\Users\\Dubbo\\habbodealer\\habbodealer\\scripts\\parse_users28.py",
	}
	for _, cand := range candidates {
		if _, err := os.Stat(cand); err == nil {
			abs, _ := filepath.Abs(cand)
			a.parserScript = abs
			a.AddLog("Parser found: " + abs)
			return
		}
	}
}

func (a *App) loadPayoutsFromDB() {
	if a.db == nil { return }
	payouts := []Payout{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := a.db.Query(ctx, "SELECT id, player_name, item_name, quantity, status, created_at, banker_trade_id FROM auto_payouts WHERE status != 'Completed' ORDER BY created_at DESC")
	if err == nil {
		count := 0
		for rows.Next() {
			var p Payout
			if err := rows.Scan(&p.ID, &p.Name, &p.ItemName, &p.Quantity, &p.Status, &p.CreatedAt, &p.BankerTradeID); err == nil { 
				p.Name = strings.TrimSpace(p.Name)
				payouts = append(payouts, p)
				count++
			}
		}
		rows.Close()
		a.AddLog(fmt.Sprintf("DB Sync: Found %d active record(s).", count))
	} else {
		a.AddLog("ERROR: DB query failed: " + err.Error())
	}
	a.pMu.Lock()
	existing := make(map[string]string)
	for _, p := range a.payouts {
		if p.Status == "Trading" || p.Status == "In Room" {
			existing[p.ID] = p.Status
		}
	}
	for i, p := range payouts {
		if st, ok := existing[p.ID]; ok {
			payouts[i].Status = st
		}
	}
	a.payouts = payouts
	a.pMu.Unlock()
	a.emitUpdate()
}

func (a *App) emitUpdate() {
	a.pMu.RLock()
	p := a.payouts
	a.pMu.RUnlock()
	if a.ctx != nil { runtime.EventsEmit(a.ctx, "payoutsUpdate", p) }
}

func (a *App) AddLog(msg string) {
	fullMsg := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), msg)
	log.Println(fullMsg)
	a.logsMu.Lock()
	a.logs = append(a.logs, fullMsg)
	if len(a.logs) > 100 { a.logs = a.logs[len(a.logs)-100:] }
	a.logsMu.Unlock()
	if a.ctx != nil { runtime.EventsEmit(a.ctx, "logsUpdate", a.logs) }
}

func (a *App) handleRoomUsers(e *g.Intercept) {
	tmpFile, err := os.CreateTemp("", "users28_*.bin")
	if err != nil { return }
	tmpPath := tmpFile.Name()
	tmpFile.Write(e.Packet.Data)
	tmpFile.Close()
	go func(path string) {
		defer os.Remove(path)
		if a.parserScript == "" { return }
		cmd := exec.Command(a.pythonExec, a.parserScript, "--input", path, "--json")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		if err := cmd.Run(); err != nil { return }
		var users []ParsedUsers28User
		if err := json.Unmarshal(stdout.Bytes(), &users); err != nil { return }
		a.roomUsersMu.Lock()
		for _, u := range users { a.roomUsers[strings.ToLower(u.Username)] = u }
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
	needsUserScan := false

	for i, p := range a.payouts {
		if p.Status == "Completed" || p.Status == "Disabled" || p.Status == "Trading" { continue }
		if _, ok := a.roomUsers[strings.ToLower(p.Name)]; ok {
			if p.Status == "Pending" || p.Status == "Failed" { a.payouts[i].Status = "In Room"; changed = true }
		} else {
			if p.Status == "In Room" { a.payouts[i].Status = "Pending"; changed = true }
			if p.Status == "Pending" { needsUserScan = true }
		}
	}
	if changed { go a.emitUpdate() }
	if needsUserScan { go a.requestRoomUsers() }
}

func (a *App) requestRoomUsers() {
	if a.ext != nil {
		a.ext.Send(g.Out.Id("G_USRS_OUT"))
		a.ext.Send(g.Out.Id("GETSPACENODEUSERS_OUT"))
	}
}

func (a *App) registerOutgoing(roomIndex int) {
	a.recentOutgoingMu.Lock()
	defer a.recentOutgoingMu.Unlock()
	a.recentOutgoing = append(a.recentOutgoing, roomIndex)
	if len(a.recentOutgoing) > 5 { a.recentOutgoing = a.recentOutgoing[len(a.recentOutgoing)-5:] }
}

func (a *App) matchesRecentOutgoing(data []byte) (int, bool) {
	a.recentOutgoingMu.Lock()
	defer a.recentOutgoingMu.Unlock()
	
	pos := 0
	for pos < len(data) {
		vlen := gencoding.VL64DecodeLen(data[pos])
		if vlen <= 0 || pos+vlen > len(data) {
			break
		}
		id := gencoding.VL64Decode(data[pos : pos+vlen])
		pos += vlen
		
		for i, val := range a.recentOutgoing {
			if val == id {
				a.recentOutgoing = append(a.recentOutgoing[:i], a.recentOutgoing[i+1:]...)
				return id, true
			}
		}
	}
	return 0, false
}

func (a *App) handleStripInfo(e *g.Intercept) {
	a.stripScanMu.Lock()
	if !a.stripScanActive { a.stripScanMu.Unlock(); return }
	sessionID := a.stripScanSessionID
	data := e.Packet.Data
	pos := 0
	readVL64 := func() (int, bool) {
		if pos >= len(data) { return 0, false }
		n := gencoding.VL64DecodeLen(data[pos])
		if n <= 0 || pos+n > len(data) { return 0, false }
		v := gencoding.VL64Decode(data[pos : pos+n])
		pos += n
		return v, true
	}
	skipUntilDelim := func() {
		for pos < len(data) && data[pos] != 0x02 { pos++ }
		if pos < len(data) { pos++ }
	}
	count, _ := readVL64()
	pageRepeated := false
	for i := 0; i < count; i++ {
		mainID, _ := readVL64()
		if i == 0 { if _, seen := a.stripScanSeenItemIDs[mainID]; seen { pageRepeated = true } else { a.stripScanSeenItemIDs[mainID] = struct{}{} } }
		extraCount, _ := readVL64()
		extraIDs := make([]int, 0, extraCount)
		for j := 0; j < extraCount; j++ { id, _ := readVL64(); extraIDs = append(extraIDs, id) }
		readVL64(); typeChar := data[pos]; pos++; skipUntilDelim(); readVL64(); readVL64(); readVL64()
		classStart := pos
		for pos < len(data) && data[pos] != 0x02 { pos++ }
		classRaw := string(data[classStart:pos])
		if pos < len(data) { pos++ }
		switch typeChar { case 'S': readVL64(); readVL64(); skipUntilDelim(); case 'I': skipUntilDelim(); default: skipUntilDelim() }
		
		normName, ok := normalizeClassKeyWithVariant(classRaw)
		if !ok {
			normName = strings.TrimSpace(strings.ToLower(classRaw))
		}

		if !pageRepeated { a.stripScanItemIDs[normName] = append(a.stripScanItemIDs[normName], mainID); a.stripScanItemIDs[normName] = append(a.stripScanItemIDs[normName], extraIDs...) }
	}
	a.stripScanMu.Unlock()
	if pageRepeated { a.finalizeStripScan(sessionID) } else { go func() { time.Sleep(200 * time.Millisecond); a.ext.Send(g.Out.Id("GETSTRIP_OUT"), "next") }() }
}

func (a *App) finalizeStripScan(sessionID int) {
	a.stripScanMu.Lock()
	if !a.stripScanActive || a.stripScanSessionID != sessionID { a.stripScanMu.Unlock(); return }
	finalInventory := make(map[string][]int)
	details := []string{}
	for name, ids := range a.stripScanItemIDs { 
		finalInventory[name] = ids 
		if len(ids) > 0 { details = append(details, fmt.Sprintf("%s:%d", name, len(ids))) }
	}
	a.stripScanActive = false
	a.stripScanMu.Unlock()
	a.inventoryMu.Lock()
	a.inventory = finalInventory
	a.inventoryMu.Unlock()
	a.AddLog(fmt.Sprintf("Hand scan complete: %s", strings.Join(details, ", ")))
}

func (a *App) lookupNameByID(id int) string {
	a.roomUsersMu.RLock()
	defer a.roomUsersMu.RUnlock()
	for _, u := range a.roomUsers {
		if u.TradeID == id || u.ChatID == id {
			return u.Username
		}
	}
	return ""
}

func (a *App) handleTradeOpen(e *g.Intercept) {
	// In Shockwave, TRADE_OPEN (104) can contain multiple VL64 IDs.
	// We scan for the first one that matches a known user.
	partnerID := 0
	partnerName := ""
	
	pos := 0
	data := e.Packet.Data
	for pos < len(data) {
		vlen := gencoding.VL64DecodeLen(data[pos])
		if vlen <= 0 || pos+vlen > len(data) {
			break
		}
		id := gencoding.VL64Decode(data[pos : pos+vlen])
		pos += vlen
		
		if name := a.lookupNameByID(id); name != "" {
			partnerID = id
			partnerName = name
			break
		}
	}
	
	// Fallback to first ID if no name found
	if partnerID == 0 && len(data) > 0 {
		partnerID = gencoding.VL64Decode(data)
	}

	// Authorize if we just opened this, or if the person has a pending payout
	matchedID, matched := a.matchesRecentOutgoing(e.Packet.Data)
	authorized := matched

	// Auto-discovery of our own ID
	if matched {
		pos := 0
		data := e.Packet.Data
		for pos < len(data) {
			vlen := gencoding.VL64DecodeLen(data[pos])
			if vlen > 0 && pos+vlen <= len(data) {
				id := gencoding.VL64Decode(data[pos : pos+vlen])
				if id != matchedID {
					a.ownChatID = id
					if name := a.lookupNameByID(id); name != "" {
						if a.botName == "" {
							a.botName = name
							a.AddLog(fmt.Sprintf("Auto-discovered bot identity: %s (id:%d)", a.botName, id))
						}
					}
					break
				}
			}
			pos += vlen
		}
	}

	// Involved Check: Ignore trades that don't involve us
	if a.ownChatID > 0 {
		involved := false
		pos := 0
		data := e.Packet.Data
		for pos < len(data) {
			vlen := gencoding.VL64DecodeLen(data[pos])
			if vlen > 0 && pos+vlen <= len(data) {
				id := gencoding.VL64Decode(data[pos : pos+vlen])
				if id == a.ownChatID || (a.ownTradeID > 0 && id == a.ownTradeID) {
					involved = true
					break
				}
			}
			pos += vlen
		}
		if !involved && !matched {
			// This trade is between two other people. Ignore it.
			return
		}
	}

	a.tradeMu.Lock()
	pPending := a.payoutPending
	pPartner := a.activeTradePartner
	a.tradeMu.Unlock()

	if authorized {
		partnerID = matchedID
		partnerName = a.lookupNameByID(matchedID)
	}

	if !authorized && partnerName != "" {
		a.pMu.RLock()
		for _, p := range a.payouts {
			if strings.EqualFold(p.Name, partnerName) && (p.Status == "In Room" || p.Status == "Pending" || p.Status == "Trading") {
				authorized = true
				break
			}
		}
		a.pMu.RUnlock()
	}
	
	// Extra leniency: If we are actively waiting for a payout partner to open a trade
	if !authorized && pPending && pPartner != "" {
		if strings.EqualFold(partnerName, pPartner) {
			authorized = true
		} else if partnerName == "" {
			// If unidentified but we are expecting someone, allow it and we'll check items later.
			// This handles cases where the TradeID in the packet doesn't match our room cache.
			authorized = true
			partnerName = pPartner
			a.AddLog(fmt.Sprintf("Allowing unidentified trade request (id:%d) during payout session for %s.", partnerID, pPartner))
		}
	}

	if !authorized {
		a.AddLog(fmt.Sprintf("Blocked unauthorized incoming trade request from %s (id:%d).", partnerName, partnerID))
		e.Block()
		// Do NOT send TRADE_CLOSE_IN here, as it will close an existing active trade
		return
	}

	a.tradeMu.Lock()
	if a.tradeActive {
		a.tradeMu.Unlock()
		a.AddLog(fmt.Sprintf("Ignored trade request from %s (id:%d) because a trade is already active.", partnerName, partnerID))
		e.Block()
		return
	}
	a.tradeActive = true
	a.payoutPending = false // Reset pending flag as trade is now officially open
	a.tradeAccepted = false
	a.tradeCompleted = false // Reset for new trade
	a.activeTradePartner = partnerName
	a.lastActiveTradePartner = partnerName
	a.activeTradeTarget = partnerID
	a.tradeMu.Unlock()

	// Update payout status to Trading if we found a match
	a.pMu.Lock()
	for i, p := range a.payouts {
		if strings.EqualFold(p.Name, partnerName) && (p.Status == "In Room" || p.Status == "Pending") {
			a.payouts[i].Status = "Trading"
		}
	}
	a.pMu.Unlock()
	a.emitUpdate()

	a.AddLog(fmt.Sprintf("Trade window opened with %s.", partnerName))
}

func (a *App) handlePartnerAccept(e *g.Intercept) { a.AddLog("Partner accepted offer.") }
func (a *App) handlePartnerConfirm(e *g.Intercept) { a.AddLog("Partner confirmed trade.") }
func (a *App) handleTradeClose(e *g.Intercept) {
	// Small delay to allow TRADE_COMPLETED (112) to arrive first if they are out of order
	go func() {
		time.Sleep(500 * time.Millisecond)
		a.tradeMu.Lock()
		partner := a.activeTradePartner
		wasCompleted := a.tradeCompleted
		a.tradeActive = false
		a.payoutPending = false // Reset pending flag on close
		a.activeTradePartner = ""
		// Do NOT reset a.tradeCompleted here, otherwise it might be cleared before we check it
		a.tradeMu.Unlock()

		if partner != "" && !wasCompleted {
			a.pMu.Lock()
			for i, p := range a.payouts {
				if strings.EqualFold(p.Name, partner) && p.Status == "Trading" {
					a.payouts[i].Status = "Pending"
				}
			}
			a.pMu.Unlock()
			a.emitUpdate()
			a.AddLog(fmt.Sprintf("Trade with %s closed without completing. Re-queued.", partner))
		} else if partner != "" {
			a.AddLog("Trade closed (completed).")
		} else {
			a.AddLog("Trade closed.")
		}
	}()
}

func (a *App) handleTradeCompleted(e *g.Intercept) {
	a.tradeMu.Lock()
	partner := a.activeTradePartner
	if partner == "" {
		partner = a.lastActiveTradePartner
	}
	a.tradeCompleted = true
	a.tradeActive = false
	a.tradeMu.Unlock()

	if partner != "" {
		a.pMu.Lock()
		for i, p := range a.payouts {
			if strings.EqualFold(p.Name, partner) && p.Status == "Trading" {
				a.payouts[i].Status = "Completed"
				if a.db != nil {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					// Update auto_payouts table
					a.db.Exec(ctx, "UPDATE auto_payouts SET status = 'Completed' WHERE id = $1", p.ID)
					// Update banker_trades table: resolve by ID if available, otherwise by name + status 'paying'
					if p.BankerTradeID > 0 { 
						a.db.Exec(ctx, "UPDATE banker_trades SET status = 'completed' WHERE id = $1", p.BankerTradeID) 
					} else { 
						a.db.Exec(ctx, "UPDATE banker_trades SET status = 'completed' WHERE LOWER(player_name) = LOWER($1) AND status = 'paying'", p.Name) 
					}
				}
				a.AddLog(fmt.Sprintf("Payout for %s COMPLETED.", partner))
			}
		}
		a.pMu.Unlock()
		a.emitUpdate()
	}
}

func (a *App) dbMonitor() {
	ticker := time.NewTicker(1 * time.Second)
	for range ticker.C {
		a.loadPayoutsFromDB()
		a.updatePayoutStatuses()
	}
}

func (a *App) payoutMonitor() {
	for {
		time.Sleep(1 * time.Second)
		a.pMu.RLock()
		var targets []Payout
		for _, p := range a.payouts {
			if p.Status == "In Room" {
				targets = append(targets, p)
			}
		}
		a.pMu.RUnlock()

		a.tradeMu.Lock()
		active := a.tradeActive
		pending := a.payoutPending
		lastTime := a.lastPayoutTime
		a.tradeMu.Unlock()

		// Skip if already in a trade, if we just sent a request (pending),
		// or if we've attempted a trade in the last 8 seconds (cooldown).
		if active || pending || len(targets) == 0 || time.Since(lastTime) < 8*time.Second {
			continue
		}

		target := targets[0]
		a.roomUsersMu.RLock()
		user, ok := a.roomUsers[strings.ToLower(target.Name)]
		a.roomUsersMu.RUnlock()

		if !ok {
			continue
		}

		// Pre-flight delay to allow Habbo client/server to settle and refresh inventory
		a.AddLog(fmt.Sprintf("Preparing payout for %s (initiating hand scan)...", target.Name))
		a.RefreshInventory()
		
		// Wait for scan to complete (up to 5 seconds)
		scanDone := false
		for i := 0; i < 25; i++ {
			time.Sleep(200 * time.Millisecond)
			a.stripScanMu.Lock()
			active := a.stripScanActive
			a.stripScanMu.Unlock()
			if !active {
				scanDone = true
				break
			}
		}
		
		if !scanDone {
			a.AddLog("ERROR: Hand scan timed out. Retrying next cycle.")
			continue
		}

		// Verify inventory AFTER the scan but BEFORE opening the trade
		a.inventoryMu.RLock()
		ids, ok := a.inventory[strings.ToLower(target.ItemName)]
		inventoryCount := len(ids)
		a.inventoryMu.RUnlock()

		if !ok || inventoryCount == 0 {
			a.AddLog(fmt.Sprintf("ERROR: Inventory shortage for '%s' (0 in hand). Waiting for next cycle.", target.ItemName))
			continue
		}

		// Re-check state after delay
		a.tradeMu.Lock()
		if a.tradeActive || a.payoutPending {
			a.tradeMu.Unlock()
			continue
		}
		a.payoutPending = true
		a.lastPayoutTime = time.Now()
		a.activeTradePartner = target.Name
		a.activeTradeTarget = user.ChatID
		a.tradeMu.Unlock()

		a.pMu.Lock()
		for i := range a.payouts {
			if a.payouts[i].ID == target.ID {
				a.payouts[i].Status = "Trading"
				break
			}
		}
		a.pMu.Unlock()
		a.emitUpdate()

		a.registerOutgoing(user.ChatID)
		a.AddLog(fmt.Sprintf("Initiating payout trade for %s...", target.Name))
		a.ext.Send(g.Out.Id("TRADE_OPEN_OUT"), user.ChatID)

		// Start a timeout monitor to clear 'pending' if the trade never opens
		go func(id string, expectedPartner string) {
			time.Sleep(10 * time.Second)
			a.tradeMu.Lock()
			if a.payoutPending && !a.tradeActive && a.activeTradePartner == expectedPartner {
				a.AddLog("Payout trade request timed out. Resetting state.")
				a.payoutPending = false
				a.activeTradePartner = "" // Clear the expected partner since we failed
				a.tradeMu.Unlock()
				a.pMu.Lock()
				for i, p := range a.payouts {
					if p.ID == id && p.Status == "Trading" {
						a.payouts[i].Status = "In Room"
					}
				}
				a.pMu.Unlock()
				a.emitUpdate()
				return
			}
			a.tradeMu.Unlock()
		}(target.ID, target.Name)

		go a.automateTrade(&target)
	}
}

func (a *App) automateTrade(p *Payout) {
	time.Sleep(2 * time.Second)
	a.tradeMu.Lock(); active := a.tradeActive; a.tradeMu.Unlock()
	if !active { return }
	a.inventoryMu.RLock(); ids, ok := a.inventory[strings.ToLower(p.ItemName)]; a.inventoryMu.RUnlock()
	if !ok || len(ids) == 0 { a.AddLog(fmt.Sprintf("ERROR: No inventory for %s.", p.ItemName)); return }
	toAdd := p.Quantity
	if len(ids) < toAdd { toAdd = len(ids) }
	a.AddLog(fmt.Sprintf("Adding %d x %s to trade...", toAdd, p.ItemName))
	for i := 0; i < toAdd; i++ { a.ext.Send(g.Out.Id("TRADE_ADDITEM_OUT"), ids[i]); time.Sleep(750 * time.Millisecond) }
	for i := 0; i < 3; i++ { a.ext.Send(g.Out.Id("TRADE_ACCEPT_OUT")); time.Sleep(1500 * time.Millisecond) }
	time.Sleep(4 * time.Second)
	a.ext.Send(g.Out.Id("TRADE_CONFIRM_ACCEPT_OUT"))
}

func main() {
	app := NewApp()
	wails.Run(&options.App{Title: "Auto Payout Bot", Width: 950, Height: 700, AssetServer: &assetserver.Options{Assets: assets}, OnStartup: app.startup, Bind: []interface{}{app}})
}
