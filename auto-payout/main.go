package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"database/sql"

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
	ID            string `json:"id"`
	Name          string `json:"name"`
	ItemName      string `json:"itemName"`
	Quantity      int    `json:"quantity"`
	Status        string `json:"status"` // "Pending", "In Room", "Trading", "Completed", "Failed", "Disabled"
	CreatedAt     string `json:"createdAt"`
	TradeID       int    `json:"playerTradeId,omitempty"`
	BankerTradeID int    `json:"bankerTradeId,omitempty"`
	Notified      bool   `json:"notified,omitempty"`
}

type StockedItem struct {
	ID            int    `json:"id"`
	RawName       string `json:"rawName"`
	CanonicalName string `json:"canonicalName"`
	DisplayName   string `json:"displayName"`
	IsActive      bool   `json:"isActive"`
}

type ParsedUsers28User struct {
	Username string `json:"username"`
	TradeID  int    `json:"trade_id"`
	ChatID   int    `json:"chat_id"`
	TokenHex string `json:"token_hex"`
}

type TradeItem struct {
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
	RawData  string `json:"raw_data,omitempty"`
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
	activeTradePartner     string
	activeTradeTarget      int
	tradeActive            bool
	tradeAccepted          bool
	payoutTradeSent        bool
	currentTradeItems      string
	lastTradeItems         []TradeItem
	lastTradePartner       string
	lastTradePartnerID     int
	lastTradePartnerChatID int
	allowedNamesCache      []string
	lastScreenshotPath     string
	tradeMu                sync.Mutex

	// Banker state
	bankerName   string
	bankerNameMu sync.RWMutex

	// Config & DB
	pythonExec     string
	parserScript   string
	db             *pgxpool.Pool
	dbConnString   string
	discordWebhook string

	// Room users request throttling
	lastUsersRequestAt time.Time
	usersReqMu         sync.Mutex

	// Track last reported inventory to DB to avoid redundant updates
	lastInventoryReport string
	lastInventoryMu     sync.Mutex

	// Inflight payouts being actively automated (name -> payout id)
	inflight   map[string]string
	inflightMu sync.RWMutex

	// Notified map to avoid duplicate failure webhooks in-memory
	notified   map[string]struct{}
	notifiedMu sync.RWMutex
}

func NewApp() *App {
	return &App{
		payouts:              []Payout{},
		logs:                 []string{"Bot initialized..."},
		roomUsers:            make(map[string]ParsedUsers28User),
		inventory:            make(map[string][]int),
		pythonExec:           "python",
		dbConnString:         "postgresql://neondb_owner:npg_S9jFTYzdQx3l@ep-aged-king-a77p1t8b-pooler.ap-southeast-2.aws.neon.tech/neondb?sslmode=require&channel_binding=require",
		discordWebhook:       "https://discordapp.com/api/webhooks/1505787297696583800/aUE_M4-quy6wkFs0qVySjHgZq3zYOze5watr67D89e6O1V9VwmjNy24HzN-X7TI5G5k3",
		stripScanSeenItemIDs: make(map[int]struct{}),
		stripScanItemIDs:     make(map[string][]int),
		inflight:             make(map[string]string),
		notified:             make(map[string]struct{}),
	}
}

func (a *App) takeScreenshot() string {
	ex, err := os.Executable()
	if err != nil {
		a.AddLog("ERROR: Could not get executable path: " + err.Error())
		return ""
	}
	appDir := filepath.Dir(ex)
	shotDir := filepath.Join(appDir, "screenshots")

	if _, err := os.Stat(shotDir); os.IsNotExist(err) {
		os.MkdirAll(shotDir, 0755)
	}

	path := filepath.Join(shotDir, fmt.Sprintf("payout_%s_%d.png", time.Now().Format("20060102_150405"), time.Now().UnixNano()%1000))

	// PowerShell command to capture the primary screen.
	psCommand := fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms, System.Drawing; $Screen = [System.Windows.Forms.Screen]::PrimaryScreen; $Bitmap = New-Object System.Drawing.Bitmap $Screen.Bounds.Width, $Screen.Bounds.Height; $Graphics = [System.Drawing.Graphics]::FromImage($Bitmap); $Graphics.CopyFromScreen($Screen.Bounds.X, $Screen.Bounds.Y, 0, 0, $Bitmap.Size); $Bitmap.Save('%s', [System.Drawing.Imaging.ImageFormat]::Png); $Graphics.Dispose(); $Bitmap.Dispose();`, path)

	cmd := exec.Command("powershell.exe", "-NoProfile", "-Command", psCommand)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Run(); err != nil {
		a.AddLog("ERROR: Screenshot failed: " + err.Error())
		return ""
	}
	return path
}

func (a *App) sendDiscordNotification(p Payout, screenshotPath string) {
	if a.discordWebhook == "" {
		return
	}

	go func() {
		// Embed construction
		payload := map[string]interface{}{
			"embeds": []map[string]interface{}{
				{
					"title": "✅ Payout Successful",
					"color": 0x00ff00, // Green
					"fields": []map[string]interface{}{
						{"name": "Player", "value": p.Name, "inline": true},
						{"name": "Items", "value": fmt.Sprintf("%d x %s", p.Quantity, p.ItemName), "inline": true},
						{"name": "Status", "value": "Delivered", "inline": true},
					},
					"timestamp": time.Now().Format(time.RFC3339),
					"footer": map[string]string{
						"text": "Auto Payout Bot • Proof of Delivery",
					},
				},
			},
		}

		if screenshotPath != "" {
			// Add image attachment reference to the embed
			payload["embeds"].([]map[string]interface{})[0]["image"] = map[string]string{
				"url": "attachment://screenshot.png",
			}
		}

		jsonPayload, _ := json.Marshal(payload)

		var cmd *exec.Cmd
		if screenshotPath != "" {
			// Using payload_json with file attachment
			cmd = exec.Command("curl", "-s", "-H", "Content-Type: multipart/form-data",
				"-F", fmt.Sprintf("payload_json=%s", string(jsonPayload)),
				"-F", fmt.Sprintf("screenshot.png=@%s", screenshotPath),
				a.discordWebhook)
		} else {
			// Standard JSON post
			cmd = exec.Command("curl", "-s", "-H", "Content-Type: application/json", "-X", "POST", "-d", string(jsonPayload), a.discordWebhook)
		}

		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if err := cmd.Run(); err != nil {
			a.AddLog("ERROR: Discord notification failed: " + err.Error())
		}
	}()
}

func (a *App) AddLog(msg string) {
	fullMsg := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), msg)
	log.Println(fullMsg)
	a.logsMu.Lock()
	a.logs = append(a.logs, fullMsg)
	if len(a.logs) > 50 {
		a.logs = a.logs[len(a.logs)-50:]
	}
	logsCopy := make([]string, len(a.logs))
	copy(logsCopy, a.logs)
	a.logsMu.Unlock()

	if a.ctx != nil {
		go runtime.EventsEmit(a.ctx, "logsUpdate", logsCopy)
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
	a.ext.Headers().Add("STRIPINFO_IN", g.Header{Dir: g.In, Value: 140})
	a.ext.Headers().Add("STRIPINFO_98_IN", g.Header{Dir: g.In, Value: 98})
	a.ext.Headers().Add("TRADE_OPEN_IN", g.Header{Dir: g.In, Value: 104})
	a.ext.Headers().Add("TRADE_ACCEPT_IN", g.Header{Dir: g.In, Value: 109})
	a.ext.Headers().Add("TRADE_CONFIRM_IN", g.Header{Dir: g.In, Value: 111})
	a.ext.Headers().Add("TRADE_CLOSE_IN", g.Header{Dir: g.In, Value: 110})
	a.ext.Headers().Add("TRADE_COMPLETED_IN", g.Header{Dir: g.In, Value: 112})
	a.ext.Headers().Add("TRADE_ITEMS_IN", g.Header{Dir: g.In, Value: 108})
	a.ext.Headers().Add("USER_OBJECT_IN", g.Header{Dir: g.In, Value: 5})

	a.ext.Headers().Add("GETSTRIP_OUT", g.Header{Dir: g.Out, Value: 65})
	// Outgoing room user requests
	a.ext.Headers().Add("G_USRS", g.Header{Dir: g.Out, Value: 61})
	a.ext.Headers().Add("GETSPACENODEUSERS", g.Header{Dir: g.Out, Value: 154})
	a.ext.Headers().Add("TRADE_OPEN_OUT", g.Header{Dir: g.Out, Value: 71})
	a.ext.Headers().Add("TRADE_CLOSE_OUT", g.Header{Dir: g.Out, Value: 70})
	a.ext.Headers().Add("TRADE_ADDITEM_OUT", g.Header{Dir: g.Out, Value: 72})
	a.ext.Headers().Add("TRADE_ACCEPT_OUT", g.Header{Dir: g.Out, Value: 69})
	a.ext.Headers().Add("TRADE_CONFIRM_ACCEPT_OUT", g.Header{Dir: g.Out, Value: 402})

	a.ext.Intercept(in.USERS, in.SPACENODEUSERS).With(a.handleRoomUsers)
	a.ext.Intercept(g.In.Id("USER_OBJECT_IN")).With(a.handleUserObject)
	a.ext.Intercept(g.In.Id("STRIPINFO_IN"), g.In.Id("STRIPINFO_98_IN")).With(a.handleStripInfo)
	a.ext.Intercept(g.In.Id("TRADE_OPEN_IN")).With(a.handleTradeOpen)
	a.ext.Intercept(g.In.Id("TRADE_ACCEPT_IN")).With(a.handlePartnerAccept)
	a.ext.Intercept(g.In.Id("TRADE_CONFIRM_IN")).With(a.handlePartnerConfirm)
	a.ext.Intercept(g.In.Id("TRADE_CLOSE_IN")).With(a.handleTradeClose)
	a.ext.Intercept(g.In.Id("TRADE_COMPLETED_IN")).With(a.handleTradeCompleted)
	a.ext.Intercept(g.In.Id("TRADE_ITEMS_IN")).With(a.handleTradeItems)

	// Selective packet sniffing for debugging: log TRADE_OPEN/TRADE_CLOSE/TRADE_ACCEPT/COMPLETED packets
	a.ext.InterceptAll(func(e *g.Intercept) {
		h := e.Packet.Header.Value
		// Interested headers: TRADE_OPEN_OUT(71), TRADE_OPEN_IN(104), TRADE_CLOSE_OUT(70), TRADE_ACCEPT_IN(109), TRADE_CONFIRM_IN(111), TRADE_COMPLETED_IN(112), TRADE_ITEMS_IN(108)
		switch h {
		case 71, 104, 70, 109, 111, 112, 108:
			dir := "out"
			if e.Packet.Header.Dir == g.In {
				dir = "in"
			}
			// Log short hex of payload to help debug open/response
			a.AddLog(fmt.Sprintf("PACKET_SNIFF header=%d dir=%s len=%d payload=% X", h, dir, len(e.Packet.Data), e.Packet.Data))
		default:
			// ignore
		}
	})

	a.ext.Activated(func() {
		a.ShowWindow()
	})

	a.AddLog("Extension registered. Waiting for connection...")
	go a.ext.Run()
	go a.payoutMonitor()
	go a.dbPoller()
	go a.inventoryRefreshLoop()
}

// dbPoller periodically reloads pending payouts from the database so new rows
// inserted by the dealer app are picked up automatically.
func (a *App) dbPoller() {
	if a.db == nil {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.loadPayoutsFromDB()
		case <-a.ctx.Done():
			return
		}
	}
}

func (a *App) inventoryRefreshLoop() {
	// Initial refresh on startup
	time.Sleep(5 * time.Second) // Wait for connection
	a.RefreshInventory()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			a.RefreshInventory()
		case <-a.ctx.Done():
			return
		}
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

	// Set search path and create tables with public qualification
	_, _ = a.db.Exec(context.Background(), "SET search_path TO public;")

	// Create table if not exists
	query := `CREATE TABLE IF NOT EXISTS public.auto_payouts (
		id TEXT PRIMARY KEY,
		player_name TEXT NOT NULL,
		item_name TEXT NOT NULL,
		quantity INTEGER NOT NULL,
		status TEXT NOT NULL,
		created_at TEXT NOT NULL,
		player_trade_id INTEGER NULL
	);`
	_, err = a.db.Exec(context.Background(), query)
	if err != nil {
		a.AddLog("ERROR: Table creation failed: " + err.Error())
	}

	// Ensure notified column exists for failure webhooks
	_, _ = a.db.Exec(context.Background(), "ALTER TABLE public.auto_payouts ADD COLUMN IF NOT EXISTS notified BOOLEAN DEFAULT FALSE;")

	// Create public.stocked_items table
	query = `CREATE TABLE IF NOT EXISTS public.stocked_items (
		id SERIAL PRIMARY KEY,
		raw_name TEXT NOT NULL,
		canonical_name TEXT NOT NULL,
		display_name TEXT NOT NULL,
		is_active BOOLEAN NOT NULL DEFAULT TRUE
	);`
	_, err = a.db.Exec(context.Background(), query)
	if err != nil {
		a.AddLog("ERROR: public.stocked_items table creation failed: " + err.Error())
	}

	// Create settings table for auto-payout configuration (key/value)
	query = `CREATE TABLE IF NOT EXISTS public.auto_payout_settings (
		setting_key TEXT PRIMARY KEY,
		setting_value TEXT NOT NULL
	);`
	_, err = a.db.Exec(context.Background(), query)
	if err != nil {
		a.AddLog("ERROR: auto_payout_settings table creation failed: " + err.Error())
	}

	a.AddLog("Database connected and ready.")

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

func normalizeTradeItemName(raw string) (string, bool) {
	name := strings.TrimSpace(strings.ToLower(raw))
	name = strings.Trim(name, "\x00\r\n\t")

	if name == "" {
		return "", false
	}

	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '*' || r == '-' || r == '.' {
			continue
		}
		return "", false
	}

	return name, true
}

func normalizeClassKeyWithVariant(raw string) (string, bool) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" || raw == "null" {
		return "", false
	}

	if star := strings.LastIndex(raw, "*"); star > 0 {
		suffix := raw[star+1:]
		if suffix == "" {
			return "", false
		}
		for _, r := range suffix {
			if r < '0' || r > '9' {
				return "", false
			}
		}

		base, ok := normalizeTradeItemName(raw[:star])
		if !ok {
			return "", false
		}
		return base + "*" + suffix, true
	}

	return normalizeTradeItemName(raw)
}

func (a *App) AddPayout(name, itemName string, qty int) {
	if qty <= 0 {
		a.AddLog("Ignoring AddPayout with non-positive quantity")
		return
	}

	normItem, ok := normalizeClassKeyWithVariant(itemName)
	if !ok {
		normItem = strings.TrimSpace(strings.ToLower(itemName))
	}

	player := normalizeName(name)

	// Load configured limits (defaults are applied when missing)
	maxQty := a.getIntSetting("max_qty_per_unique", 10)
	maxUnique := a.getIntSetting("max_unique_items", 6)

	// If DB present, enforce max unique items per player (only when adding a new unique)
	if a.db != nil {
		ctx := context.Background()
		var exists bool
		if err := a.db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM public.auto_payouts WHERE lower(player_name)=lower($1) AND lower(item_name)=lower($2) AND status != 'Completed')", player, normItem).Scan(&exists); err == nil {
			if !exists {
				var uniqueCount int
				if err := a.db.QueryRow(ctx, "SELECT COUNT(DISTINCT item_name) FROM public.auto_payouts WHERE lower(player_name)=lower($1) AND status != 'Completed'", player).Scan(&uniqueCount); err == nil {
					if uniqueCount >= maxUnique {
						a.AddLog(fmt.Sprintf("Player %s already has %d unique items queued (limit %d). Not adding %s", player, uniqueCount, maxUnique, normItem))
						return
					}
				}
			}
		}
	}

	// Resolve optional player_trade id to attach to new rows (do once)
	var tradeParam interface{}
	if a.db != nil {
		ctx := context.Background()
		var playerTrade sql.NullInt64
		err := a.db.QueryRow(ctx, "SELECT player_trade_id FROM public.banker_trades WHERE player_name = $1 AND status = 'paying' ORDER BY created_at DESC LIMIT 1", player).Scan(&playerTrade)
		if err != nil || !playerTrade.Valid {
			_ = a.db.QueryRow(ctx, "SELECT player_trade_id FROM public.banker_trades WHERE player_name = $1 ORDER BY created_at DESC LIMIT 1", player).Scan(&playerTrade)
		}
		if playerTrade.Valid {
			tradeParam = playerTrade.Int64
			a.AddLog(fmt.Sprintf("DB: attaching player_trade_id=%d to new auto_payout for %s", playerTrade.Int64, player))
		} else {
			tradeParam = nil
			a.AddLog(fmt.Sprintf("DB: no player_trade_id found for %s; inserting NULL", player))
		}
	}

	createdAt := time.Now().Format("2006-01-02 15:04:05")

	// If configured, split large quantities into multiple DB rows each capped by maxQty
	remain := qty
	created := make([]Payout, 0)
	chunkIdx := 0
	for remain > 0 {
		chunk := remain
		if maxQty > 0 && chunk > maxQty {
			chunk = maxQty
		}

		id := fmt.Sprintf("%d-%d", time.Now().UnixNano(), chunkIdx)
		p := Payout{
			ID:        id,
			Name:      player,
			ItemName:  normItem,
			Quantity:  chunk,
			Status:    "Pending",
			CreatedAt: createdAt,
		}

		a.AddLog(fmt.Sprintf("Adding payout: %s x %d %s", p.Name, p.Quantity, p.ItemName))

		if a.db != nil {
			ctx := context.Background()
			if _, err := a.db.Exec(ctx,
				"INSERT INTO public.auto_payouts (id, player_name, item_name, quantity, status, created_at, player_trade_id) VALUES ($1, $2, $3, $4, $5, $6, $7)",
				p.ID, p.Name, p.ItemName, p.Quantity, p.Status, p.CreatedAt, tradeParam); err != nil {
				a.AddLog("ERROR: DB Save failed: " + err.Error())
			}
		}

		created = append(created, p)
		remain -= chunk
		chunkIdx++
	}

	if len(created) > 0 {
		a.pMu.Lock()
		a.payouts = append(a.payouts, created...)
		a.pMu.Unlock()
		a.emitUpdate()
	}
}

// AddPayoutCheckResult describes the server-side validation outcome for a proposed AddPayout
type AddPayoutCheckResult struct {
	Allowed     bool   `json:"allowed"`
	Reason      string `json:"reason"`
	UniqueCount int    `json:"uniqueCount"`
	Exists      bool   `json:"exists"`
	MaxUnique   int    `json:"maxUnique"`
	MaxQty      int    `json:"maxQty"`
	Chunks      []int  `json:"chunks"`
	Player      string `json:"player"`
	Item        string `json:"item"`
}

// CheckAddPayout runs the same checks AddPayout uses but does not persist anything.
// Returns a structured result explaining whether the server would accept the add
// and any chunking that would occur.
func (a *App) CheckAddPayout(name, itemName string, qty int) (AddPayoutCheckResult, error) {
	res := AddPayoutCheckResult{Allowed: true, Reason: "", Player: name, Item: itemName}

	if qty <= 0 {
		res.Allowed = false
		res.Reason = "Quantity must be positive"
		return res, nil
	}

	normItem, ok := normalizeClassKeyWithVariant(itemName)
	if !ok {
		normItem = strings.TrimSpace(strings.ToLower(itemName))
	}

	player := normalizeName(name)

	maxQty := a.getIntSetting("max_qty_per_unique", 10)
	maxUnique := a.getIntSetting("max_unique_items", 6)
	res.MaxQty = maxQty
	res.MaxUnique = maxUnique
	res.Player = player
	res.Item = normItem

	if a.db != nil {
		ctx := context.Background()
		// Check if an existing non-completed row for this player+item exists
		var exists bool
		if err := a.db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM public.auto_payouts WHERE lower(player_name)=lower($1) AND lower(item_name)=lower($2) AND status != 'Completed')", player, normItem).Scan(&exists); err == nil {
			res.Exists = exists
		}

		if !res.Exists {
			var uniqueCount int
			if err := a.db.QueryRow(ctx, "SELECT COUNT(DISTINCT item_name) FROM public.auto_payouts WHERE lower(player_name)=lower($1) AND status != 'Completed'", player).Scan(&uniqueCount); err == nil {
				res.UniqueCount = uniqueCount
				if uniqueCount >= maxUnique {
					res.Allowed = false
					res.Reason = fmt.Sprintf("Player %s already has %d unique item(s) queued (limit %d)", player, uniqueCount, maxUnique)
				}
			}
		}
	}

	// Compute chunking plan
	remain := qty
	chunks := []int{}
	if maxQty <= 0 {
		chunks = append(chunks, remain)
	} else {
		for remain > 0 {
			take := remain
			if take > maxQty {
				take = maxQty
			}
			chunks = append(chunks, take)
			remain -= take
		}
	}
	res.Chunks = chunks

	// If not already rejected, set a descriptive reason
	if res.Allowed && len(chunks) > 1 {
		res.Reason = fmt.Sprintf("Will split quantity into %d chunk(s)", len(chunks))
	}

	return res, nil
}

func normalizeName(raw string) string {
	return strings.TrimSpace(raw)
}

func (a *App) DeletePayout(id string) {
	if a.db != nil {
		_, err := a.db.Exec(context.Background(), "DELETE FROM public.auto_payouts WHERE id = $1", id)
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
				a.db.Exec(context.Background(), "UPDATE public.auto_payouts SET status = $1 WHERE id = $2", newStatus, id)
			}
			break
		}
	}
	a.emitUpdate()
}

func (a *App) ClearCompleted() {
	if a.db != nil {
		a.db.Exec(context.Background(), "DELETE FROM public.auto_payouts WHERE status = 'Completed'")
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

func (a *App) RefreshQueue() {
	a.loadPayoutsFromDB()
	a.emitUpdate()
	a.AddLog("Payout queue refreshed from database.")
}

func (a *App) RefreshInventory() {
	if a.ext != nil {
		a.tradeMu.Lock()
		inTrade := a.tradeActive
		a.tradeMu.Unlock()

		if inTrade {
			return
		}

		a.stripScanMu.Lock()
		if a.stripScanActive {
			a.stripScanMu.Unlock()
			return
		}
		a.stripScanActive = true
		sessionID := a.stripScanSessionID + 1
		a.stripScanSessionID = sessionID
		a.stripScanSeenItemIDs = make(map[int]struct{})
		a.stripScanItemIDs = make(map[string][]int)
		a.stripScanMu.Unlock()

		a.AddLog("Requesting hand inventory...")
		a.ext.Send(g.Out.Id("GETSTRIP_OUT"), "new")

		go func() {
			time.Sleep(2500 * time.Millisecond) // Wait for scan
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

// --- Stocked Items Methods ---

func (a *App) GetStockedItems() []StockedItem {
	if a.db == nil {
		return []StockedItem{}
	}
	rows, err := a.db.Query(context.Background(), "SELECT id, raw_name, canonical_name, display_name, is_active FROM public.stocked_items ORDER BY display_name ASC")
	if err != nil {
		a.AddLog("ERROR: Failed to query public.stocked_items: " + err.Error())
		return []StockedItem{}
	}
	defer rows.Close()

	items := []StockedItem{}
	for rows.Next() {
		var i StockedItem
		if err := rows.Scan(&i.ID, &i.RawName, &i.CanonicalName, &i.DisplayName, &i.IsActive); err == nil {
			items = append(items, i)
		}
	}
	return items
}

func (a *App) AddStockedItem(rawName, displayName string) {
	if a.db == nil {
		return
	}
	canonical, ok := normalizeClassKeyWithVariant(rawName)
	if !ok {
		canonical = strings.ToLower(strings.TrimSpace(rawName))
	}
	_, err := a.db.Exec(context.Background(),
		"INSERT INTO public.stocked_items (raw_name, canonical_name, display_name, is_active) VALUES ($1, $2, $3, $4)",
		rawName, canonical, displayName, true)
	if err != nil {
		a.AddLog("ERROR: Failed to add stocked item: " + err.Error())
	} else {
		a.AddLog(fmt.Sprintf("Stocked item added: %s (%s)", displayName, rawName))
	}
}

func (a *App) DeleteStockedItem(id int) {
	if a.db == nil {
		return
	}
	_, err := a.db.Exec(context.Background(), "DELETE FROM public.stocked_items WHERE id = $1", id)
	if err != nil {
		a.AddLog("ERROR: Failed to delete stocked item: " + err.Error())
	}
}

func (a *App) ToggleStockedItem(id int) {
	if a.db == nil {
		return
	}
	_, err := a.db.Exec(context.Background(), "UPDATE public.stocked_items SET is_active = NOT is_active WHERE id = $1", id)
	if err != nil {
		a.AddLog("ERROR: Failed to toggle stocked item: " + err.Error())
	}
}

func (a *App) GetActiveStockedItemNames() []string {
	if a.db == nil {
		return []string{}
	}
	rows, err := a.db.Query(context.Background(), "SELECT raw_name FROM public.stocked_items WHERE is_active = TRUE")
	if err != nil {
		return []string{}
	}
	defer rows.Close()

	names := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err == nil {
			names = append(names, n)
		}
	}
	return names
}

// PayoutSettings holds persisted UI settings for auto-payout
type PayoutSettings struct {
	MaxUniqueItems  int `json:"maxUniqueItems"`
	MaxQtyPerUnique int `json:"maxQtyPerUnique"`
}

// getIntSetting reads an integer setting from the DB and falls back to def when missing
func (a *App) getIntSetting(key string, def int) int {
	if a.db == nil {
		return def
	}
	var v string
	ctx := context.Background()
	if err := a.db.QueryRow(ctx, "SELECT setting_value FROM public.auto_payout_settings WHERE setting_key = $1", key).Scan(&v); err == nil {
		if iv, err := strconv.Atoi(v); err == nil {
			return iv
		}
	}
	return def
}

// GetSettings returns current auto-payout settings (with defaults when missing)
func (a *App) GetSettings() PayoutSettings {
	s := PayoutSettings{MaxUniqueItems: 6, MaxQtyPerUnique: 10}
	if a.db == nil {
		return s
	}
	ctx := context.Background()
	var v string
	if err := a.db.QueryRow(ctx, "SELECT setting_value FROM public.auto_payout_settings WHERE setting_key = $1", "max_unique_items").Scan(&v); err == nil {
		if iv, err := strconv.Atoi(v); err == nil {
			s.MaxUniqueItems = iv
		}
	}
	if err := a.db.QueryRow(ctx, "SELECT setting_value FROM public.auto_payout_settings WHERE setting_key = $1", "max_qty_per_unique").Scan(&v); err == nil {
		if iv, err := strconv.Atoi(v); err == nil {
			s.MaxQtyPerUnique = iv
		}
	}
	return s
}

// SaveSettings persists provided settings to the DB.
func (a *App) SaveSettings(maxUnique int, maxQty int) error {
	if a.db == nil {
		return nil
	}
	ctx := context.Background()
	if _, err := a.db.Exec(ctx, `INSERT INTO public.auto_payout_settings (setting_key, setting_value) VALUES ($1, $2) ON CONFLICT (setting_key) DO UPDATE SET setting_value = EXCLUDED.setting_value`, "max_unique_items", fmt.Sprintf("%d", maxUnique)); err != nil {
		a.AddLog("ERROR: Failed to save max_unique_items: " + err.Error())
		return err
	}
	if _, err := a.db.Exec(ctx, `INSERT INTO public.auto_payout_settings (setting_key, setting_value) VALUES ($1, $2) ON CONFLICT (setting_key) DO UPDATE SET setting_value = EXCLUDED.setting_value`, "max_qty_per_unique", fmt.Sprintf("%d", maxQty)); err != nil {
		a.AddLog("ERROR: Failed to save max_qty_per_unique: " + err.Error())
		return err
	}
	a.AddLog(fmt.Sprintf("Settings saved: max_unique=%d, max_qty=%d", maxUnique, maxQty))
	return nil
}

// --- Internal Logic ---

func (a *App) loadPayoutsFromDB() {
	if a.db == nil {
		a.AddLog("ERROR: Database not connected. Cannot load payouts.")
		return
	}

	a.AddLog("Querying public.auto_payouts records...")
	payouts := []Payout{}
	count := 0

	// Manual entries from public.auto_payouts table
	rows, err := a.db.Query(context.Background(), "SELECT id, player_name, item_name, quantity, status, created_at, COALESCE(player_trade_id,0), COALESCE(banker_trade_id,0), COALESCE(notified,false) FROM public.auto_payouts WHERE status != 'Completed' ORDER BY created_at DESC")
	if err == nil {
		for rows.Next() {
			var p Payout
			if err := rows.Scan(&p.ID, &p.Name, &p.ItemName, &p.Quantity, &p.Status, &p.CreatedAt, &p.TradeID, &p.BankerTradeID, &p.Notified); err == nil {
				p.Name = normalizeName(p.Name)
				payouts = append(payouts, p)
				count++
			}
		}
		rows.Close()
	} else {
		a.AddLog("ERROR: DB Query failed: " + err.Error())
	}

	a.pMu.Lock()
	a.payouts = payouts
	a.pMu.Unlock()

	// Debug: log each loaded payout for easier tracing
	for _, p := range payouts {
		a.AddLog(fmt.Sprintf("[DB] loaded payout id=%s name=%s item=%s qty=%d status=%s tradeid=%d bankerid=%d",
			p.ID, p.Name, p.ItemName, p.Quantity, p.Status, p.TradeID, p.BankerTradeID))
	}

	a.AddLog(fmt.Sprintf("Sync complete. Found %d active records in public.auto_payouts.", count))
	a.emitUpdate()

	// If we have any pending payouts, request a room users refresh so parse28
	// can populate the current room map immediately.
	for _, p := range payouts {
		if p.Status == "Pending" {
			go a.requestRoomUsers()
			break
		}
	}
}

// requestRoomUsers asks the client to refresh the current room user list by
// sending G_USRS + GETSPACENODEUSERS. Calls are rate-limited to once per 5s.
func (a *App) requestRoomUsers() {
	a.usersReqMu.Lock()
	if time.Since(a.lastUsersRequestAt) < 5*time.Second {
		a.usersReqMu.Unlock()
		return
	}
	a.lastUsersRequestAt = time.Now()
	a.usersReqMu.Unlock()

	if a.ext == nil {
		a.AddLog("ERROR: Extension not connected; cannot request room users")
		return
	}

	a.AddLog("[ROOM] requesting current room users via G_USRS + GETSPACENODEUSERS")
	a.ext.Send(g.Out.Id("G_USRS"))
	a.ext.Send(g.Out.Id("GETSPACENODEUSERS"))
}

func (a *App) emitUpdate() {
	p := a.GetPayouts()
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "payoutsUpdate", p)
	}
}

func (a *App) persistPayoutStatus(id string, status string) {
	if a.db == nil {
		return
	}
	ctx := context.Background()
	if _, err := a.db.Exec(ctx, "UPDATE public.auto_payouts SET status = $1 WHERE id = $2", status, id); err != nil {
		a.AddLog("ERROR: Failed to persist payout status: " + err.Error())
	} else {
		a.AddLog(fmt.Sprintf("DB: set payout %s status=%s", id, status))
	}
}

func (a *App) markInflight(name, id string) {
	a.inflightMu.Lock()
	a.inflight[strings.ToLower(name)] = id
	a.inflightMu.Unlock()
}

func (a *App) unmarkInflight(name string) {
	a.inflightMu.Lock()
	delete(a.inflight, strings.ToLower(name))
	a.inflightMu.Unlock()
}

func (a *App) getInflight(name string) (string, bool) {
	a.inflightMu.RLock()
	id, ok := a.inflight[strings.ToLower(name)]
	a.inflightMu.RUnlock()
	return id, ok
}

// hasActiveBankerTrades checks whether there are any non-completed rows in
// public.banker_trades for this banker. When true, the banker should not
// accept incoming trades (outgoing opens for payouts are still allowed).
func (a *App) hasActiveBankerTrades() bool {
	if a.db == nil {
		return false
	}

	a.bankerNameMu.RLock()
	bname := strings.TrimSpace(a.bankerName)
	a.bankerNameMu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	var exists bool
	var err error
	if bname != "" {
		err = a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM public.banker_trades WHERE lower(banker_name) = lower($1) AND COALESCE(status,'') != 'completed')`, bname).Scan(&exists)
	} else {
		err = a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM public.banker_trades WHERE COALESCE(status,'') != 'completed')`).Scan(&exists)
	}
	if err != nil {
		a.AddLog("ERROR: hasActiveBankerTrades query failed: " + err.Error())
		return false
	}
	return exists
}

// isPayoutCompletedInDB returns true when the auto_payout row is marked
// as Completed or Disabled in the DB. Returns an error if the query fails.
func (a *App) isPayoutCompletedInDB(id string) (bool, error) {
	if a.db == nil {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var status string
	if err := a.db.QueryRow(ctx, "SELECT status FROM public.auto_payouts WHERE id = $1", id).Scan(&status); err != nil {
		return false, err
	}
	s := strings.TrimSpace(strings.ToLower(status))
	return s == "completed" || s == "disabled", nil
}

func (a *App) sendPayoutNotAcceptedWebhook(p Payout, attempts int) {
	// Non-blocking webhook notify about failed payout acceptance
	go func() {
		webhook := "https://discordapp.com/api/webhooks/1502209413065343086/lV-mzQvSRCqc-HkjKZWXOrmX0McP1HU47_fBjthixU2IdO0Bh18j-FBkIjGCDDjgAbo4"

		// In-memory dedupe
		a.notifiedMu.RLock()
		if _, ok := a.notified[p.ID]; ok {
			a.notifiedMu.RUnlock()
			a.AddLog("Skipping webhook: already sent in this session for " + p.ID)
			return
		}
		a.notifiedMu.RUnlock()

		// If DB reports already notified, skip
		if a.db != nil {
			var already bool
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := a.db.QueryRow(ctx, "SELECT COALESCE(notified,false) FROM public.auto_payouts WHERE id = $1", p.ID).Scan(&already); err == nil {
				if already {
					a.notifiedMu.Lock()
					a.notified[p.ID] = struct{}{}
					a.notifiedMu.Unlock()
					a.AddLog("Skipping webhook: already notified in DB for " + p.ID)
					return
				}
			}
		}

		embed := map[string]interface{}{
			"title": "⚠️ Payout Not Accepted",
			"color": 16711680, // red
			"fields": []map[string]interface{}{
				{"name": "Payout To", "value": p.Name, "inline": true},
				{"name": "Item", "value": p.ItemName, "inline": true},
				{"name": "Quantity", "value": fmt.Sprintf("%d", p.Quantity), "inline": true},
				{"name": "Attempts", "value": fmt.Sprintf("%d", attempts), "inline": true},
				{"name": "Note", "value": "Trade attempts exhausted; marking payout completed forcibly.", "inline": false},
			},
			"timestamp": time.Now().Format(time.RFC3339),
		}

		payload := map[string]interface{}{"embeds": []map[string]interface{}{embed}}
		body, _ := json.Marshal(payload)

		resp, err := http.Post(webhook, "application/json", bytes.NewReader(body))
		if err != nil {
			a.AddLog("ERROR: Failed to send failure webhook: " + err.Error())
			return
		}
		resp.Body.Close()

		// Mark in-memory and in DB
		a.notifiedMu.Lock()
		a.notified[p.ID] = struct{}{}
		a.notifiedMu.Unlock()

		if a.db != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := a.db.Exec(ctx, "UPDATE public.auto_payouts SET notified = TRUE WHERE id = $1", p.ID); err != nil {
				a.AddLog("ERROR: Failed to mark notified in DB: " + err.Error())
			}
		}

		a.AddLog("Sent failure webhook for payout " + p.ID)
	}()
}

// sendSimpleFailureWebhook posts a concise failure message (always) so the
// operator is notified when automation exhausts retries. It marks the DB
// notified flag and the in-memory map to avoid duplicates after sending.
func (a *App) sendSimpleFailureWebhook(p Payout) {
	go func() {
		webhook := "https://discordapp.com/api/webhooks/1502209413065343086/lV-mzQvSRCqc-HkjKZWXOrmX0McP1HU47_fBjthixU2IdO0Bh18j-FBkIjGCDDjgAbo4"

		embed := map[string]interface{}{
			"title": "⚠️ Payout — Issue",
			"color": 16711680,
			"fields": []map[string]interface{}{
				{"name": "Payout To", "value": p.Name, "inline": true},
				{"name": "Payout Decision", "value": "Keep", "inline": true},
				{"name": "Payout Items", "value": fmt.Sprintf("%s x%d", p.ItemName, p.Quantity), "inline": true},
				{"name": "Notes", "value": "Payout trade failed to open after all retry attempts", "inline": false},
				{"name": "Payout ID", "value": p.ID, "inline": false},
			},
			"timestamp": time.Now().Format(time.RFC3339),
			"footer":    map[string]string{"text": "Auto Payout Bot"},
		}

		payload := map[string]interface{}{"embeds": []map[string]interface{}{embed}}
		body, _ := json.Marshal(payload)

		resp, err := http.Post(webhook, "application/json", bytes.NewReader(body))
		if err != nil {
			a.AddLog("ERROR: Failed to send simple failure webhook: " + err.Error())
			return
		}
		resp.Body.Close()

		// Mark in-memory and DB as notified to avoid repeated alerts.
		a.notifiedMu.Lock()
		a.notified[p.ID] = struct{}{}
		a.notifiedMu.Unlock()

		if a.db != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := a.db.Exec(ctx, "UPDATE public.auto_payouts SET notified = TRUE WHERE id = $1", p.ID); err != nil {
				a.AddLog("ERROR: Failed to mark notified in DB: " + err.Error())
			}
		}

		a.AddLog("Sent failure webhook for payout " + p.ID)
	}()
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
			key := strings.ToLower(normalizeName(u.Username))
			a.roomUsers[key] = u
			a.AddLog(fmt.Sprintf("[ROOM] parsed user: username=%s tradeid=%d chatid=%d key=%s", u.Username, u.TradeID, u.ChatID, key))
		}
		a.roomUsersMu.Unlock()

		a.updatePayoutStatuses()
	}(tmpPath)
}

func (a *App) updatePayoutStatuses() {
	// We collect changed IDs and their new statuses while holding the in-memory lock,
	// then persist them after unlocking to avoid blocking the hot path.
	a.pMu.Lock()
	changed := false
	type changeRec struct {
		id     string
		status string
	}
	changes := make([]changeRec, 0)

	a.roomUsersMu.RLock()
	for i, p := range a.payouts {
		if p.Status == "Completed" || p.Status == "Disabled" {
			continue
		}

		// Use consistent lower-case keys for room lookup
		targetKey := strings.ToLower(normalizeName(p.Name))
		user, ok := a.roomUsers[targetKey]
		if ok {
			if p.Status == "Pending" || p.Status == "Failed" || p.Status == "Payout Pending" {
				a.AddLog(fmt.Sprintf("Target detected: %s (RoomIndex: %d)", p.Name, user.ChatID))
				a.payouts[i].Status = "In Room"
				changes = append(changes, changeRec{id: p.ID, status: "In Room"})
				changed = true
			}
		} else {
			if p.Status == "In Room" {
				a.AddLog(fmt.Sprintf("Target left room: %s", p.Name))
				a.payouts[i].Status = "Pending"
				changes = append(changes, changeRec{id: p.ID, status: "Pending"})
				changed = true
			} else {
				// Debug: target not present in current room users snapshot
				a.AddLog(fmt.Sprintf("Target not in room: %s (key='%s') known_room_users=%d", p.Name, targetKey, len(a.roomUsers)))
			}
		}
	}
	a.roomUsersMu.RUnlock()
	a.pMu.Unlock()

	if changed {
		// Persist status changes to DB to prevent the dbPoller from overwriting
		for _, c := range changes {
			go a.persistPayoutStatus(c.id, c.status)
		}
		go a.emitUpdate()
	}
}

func encodeVL64(value int) string {
	buf := make([]byte, gencoding.VL64EncodeLen(value))
	gencoding.VL64Encode(buf, value)
	return string(buf)
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
		if !ok {
			break
		}

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
			readVL64()
			readVL64()
			skipUntilDelim()
		case 'I':
			skipUntilDelim()
		default:
			skipUntilDelim()
		}

		if !pageRepeated {
			// Always use lower-case and trimmed name
			name := strings.TrimSpace(classRaw)
			a.stripScanItemIDs[name] = append(a.stripScanItemIDs[name], mainID)
			a.stripScanItemIDs[name] = append(a.stripScanItemIDs[name], extraIDs...)
		}
	}
	a.stripScanMu.Unlock()

	if pageRepeated {
		a.finalizeStripScan(sessionID)
	} else {
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
	totalItems := 0
	details := []string{}
	for name, ids := range a.stripScanItemIDs {
		finalInventory[name] = ids
		totalItems += len(ids)
		if len(ids) > 0 {
			details = append(details, fmt.Sprintf("%s:%d", name, len(ids)))
		}
	}

	a.stripScanActive = false
	a.stripScanMu.Unlock()

	// Only update if we actually got items OR if we are sure we aren't in a trade
	// (Prevents wiping inventory if scan is blocked by a trade window)
	a.tradeMu.Lock()
	inTrade := a.tradeActive
	a.tradeMu.Unlock()

	if totalItems == 0 && inTrade {
		a.AddLog("Hand scan returned 0 items while in trade (ignoring to prevent state loss).")
		return
	}

	a.inventoryMu.Lock()
	a.inventory = finalInventory
	a.inventoryMu.Unlock()

	if totalItems > 0 {
		a.AddLog(fmt.Sprintf("Hand scanning complete. Total: %d, Details: %s", totalItems, strings.Join(details, ", ")))
	} else {
		a.AddLog("Hand scanning complete. Hand is empty.")
	}

	// Report inventory to database
	if a.db != nil {
		// Create a string representation for change detection
		reportStr := strings.Join(details, "|")

		a.lastInventoryMu.Lock()
		changed := reportStr != a.lastInventoryReport
		a.lastInventoryMu.Unlock()

		if !changed {
			// Skip DB update if nothing changed
			return
		}

		a.bankerNameMu.Lock()
		bName := a.bankerName
		a.bankerNameMu.Unlock()

		if bName == "" {
			bName = "Auto Payout Bot" // fallback
		}

		go func(name, report string, inv map[string][]int) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			for itemName, ids := range inv {
				qty := len(ids)
				_, err := a.db.Exec(ctx, `
					INSERT INTO banker_inventory (banker_name, item_name, quantity, updated_at)
					VALUES ($1, $2, $3, NOW())
					ON CONFLICT (banker_name, item_name) 
					DO UPDATE SET quantity = EXCLUDED.quantity, updated_at = NOW()
				`, name, itemName, qty)
				if err != nil {
					log.Printf("[INVENTORY_DB] ERROR: failed to update %s for %s: %v", itemName, name, err)
				}
			}

			a.lastInventoryMu.Lock()
			a.lastInventoryReport = report
			a.lastInventoryMu.Unlock()

			log.Printf("[INVENTORY_DB] Successfully reported %d item types for %s", len(inv), name)
		}(bName, reportStr, finalInventory)
	}
}

func (a *App) handleUserObject(e *g.Intercept) {
	data := e.Packet.Data
	if len(data) < 2 {
		return
	}
	// ID is first VL64
	n := gencoding.VL64DecodeLen(data[0])
	if n <= 0 || len(data) < n {
		return
	}
	pos := n
	// Name is next string
	if pos >= len(data) {
		return
	}
	nameLen := int(data[pos])
	pos++
	if pos+nameLen > len(data) {
		return
	}
	name := string(data[pos : pos+nameLen])
	a.bankerNameMu.Lock()
	a.bankerName = name
	a.bankerNameMu.Unlock()
	a.AddLog(fmt.Sprintf("Banker identified: %s", name))
}

func (a *App) handleTradeOpen(e *g.Intercept) {
	// Determine incoming trader id (if any) so we can allow opens that match
	// an outgoing payout attempt while still blocking unsolicited incoming
	// trades when the banker has active banker_trades in the DB.

	if a.hasActiveBankerTrades() {
		// If we're currently driving an outgoing payout flow, allow the
		// incoming trade open so the payout can complete. Otherwise block
		// unsolicited incoming opens while banker_trades are active.
		a.tradeMu.Lock()
		allow := a.payoutTradeSent
		a.tradeMu.Unlock()
		if !allow {
			a.AddLog("Blocking incoming trade open: active banker_trades present")
			e.Block()
			if a.ext != nil {
				a.ext.Send(g.Out.Id("TRADE_CLOSE_OUT"))
			}
			return
		}
	}

	a.tradeMu.Lock()
	a.tradeActive = true
	a.tradeAccepted = false
	a.currentTradeItems = ""
	a.lastTradeItems = nil
	a.lastTradePartner = ""
	a.lastTradePartnerID = 0
	a.lastTradePartnerChatID = 0
	a.allowedNamesCache = []string{}

	if id, ok := decodeLeadingVL64(e.Packet.Data); ok {
		a.activeTradeTarget = id
		a.lastTradePartnerID = id

		// Try to resolve name immediately
		a.roomUsersMu.RLock()
		found := false
		for _, u := range a.roomUsers {
			if u.TradeID == id {
				a.lastTradePartner = u.Username
				a.lastTradePartnerChatID = u.ChatID
				a.AddLog(fmt.Sprintf("Trade window opened with %s (TradeID:%d, ChatID:%d).", u.Username, id, u.ChatID))
				found = true
				break
			}
		}
		a.roomUsersMu.RUnlock()

		if !found {
			a.AddLog(fmt.Sprintf("Trade window opened with TradeID %d (resolving name...). Note: user might not be in room map yet.", id))
		}
	}
	a.tradeMu.Unlock()

	// Fetch active stocked items immediately on trade open
	go func() {
		names := a.GetActiveStockedItemNames()
		a.tradeMu.Lock()
		a.allowedNamesCache = names
		a.tradeMu.Unlock()
		a.AddLog(fmt.Sprintf("[DEBUG] Stocked items cached for trade: %v", names))
	}()

	a.AddLog("Trade window opened.")
}

func (a *App) handleTradeItems(e *g.Intercept) {
	a.tradeMu.Lock()
	a.currentTradeItems = string(e.Packet.Data)
	allowedCache := a.allowedNamesCache
	a.lastTradeItems = a.parseTradeItems(e.Packet.Data, allowedCache)
	a.tradeMu.Unlock()
	a.AddLog(fmt.Sprintf("[DEBUG] TRADE_ITEMS updated, found %d items", len(a.lastTradeItems)))
}

func decodeLeadingVL64(data []byte) (int, bool) {
	if len(data) == 0 {
		return 0, false
	}
	n := gencoding.VL64DecodeLen(data[0])
	if n <= 0 || len(data) < n {
		return 0, false
	}
	return gencoding.VL64Decode(data[:n]), true
}

func (a *App) parseTradeItems(data []byte, allowedNamesCache []string) []TradeItem {
	counts := map[string]int{}
	fields := bytes.Split(data, []byte{0x02})

	allowed := make(map[string]bool)
	for _, n := range allowedNamesCache {
		allowed[strings.ToLower(strings.TrimSpace(n))] = true
	}

	for _, field := range fields {
		if len(field) == 0 {
			continue
		}
		s := strings.TrimSpace(string(field))
		if s == "" {
			continue
		}

		lowName := strings.ToLower(s)

		// Quick substring match against active stocked items (allowed map keys)
		best := ""
		for n := range allowed {
			if n == "" {
				continue
			}
			if strings.Contains(lowName, n) {
				if len(n) > len(best) {
					best = n
				}
			}
		}
		if best != "" {
			counts[best]++
			continue
		}

		// Filter out known packet fragments/metadata that are NOT physical items
		if lowName == "credit" || lowName == "pixel" || lowName == "shell" ||
			strings.HasPrefix(lowName, "ii") || strings.HasPrefix(lowName, "ih") ||
			len(lowName) < 3 {
			continue
		}

		isItem := false
		// 1. Check if it's in our allowed/stocked list
		if allowed[lowName] {
			isItem = true
		} else {
			// 2. Fallback heuristic for Habbo class names
			// Usually starts with cf_ (currency) or contains underscores and numbers
			if strings.HasPrefix(lowName, "cf_") || strings.Contains(lowName, "_") {
				isItem = true
			}
		}

		if isItem {
			counts[lowName]++
		}
	}

	var items []TradeItem
	for name, qty := range counts {
		items = append(items, TradeItem{Name: name, Quantity: qty})
	}
	return items
}

func (a *App) recordBankerTrade(playerName string, items []TradeItem, tradeID int, chatID int) {
	if a.db == nil {
		a.AddLog("ERROR: recordBankerTrade failed - Database not connected")
		return
	}

	if len(items) == 0 {
		a.AddLog(fmt.Sprintf("WARNING: recordBankerTrade skipped for %s - no items parsed", playerName))
		return
	}

	a.bankerNameMu.RLock()
	banker := a.bankerName
	a.bankerNameMu.RUnlock()
	if banker == "" {
		banker = "Auto Payout Bot"
	}

	type BankerBetItem struct {
		RawName string `json:"raw_name"`
		Qty     int    `json:"qty"`
	}

	bankerItems := make([]BankerBetItem, 0, len(items))
	for _, it := range items {
		bankerItems = append(bankerItems, BankerBetItem{
			RawName: it.Name,
			Qty:     it.Quantity,
		})
	}

	itemsJSON, err := json.Marshal(bankerItems)
	if err != nil {
		a.AddLog(fmt.Sprintf("ERROR: failed to marshal bet items: %v", err))
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_, err := a.db.Exec(ctx, `
			INSERT INTO public.banker_trades (player_name, bet_items, banker_name, status, created_at, player_trade_id, player_chat_id)
			VALUES ($1, $2, $3, $4, NOW(), $5, $6)
		`, playerName, itemsJSON, banker, "pending", tradeID, chatID)
		if err != nil {
			a.AddLog(fmt.Sprintf("ERROR: [BANKER][DB] failed to record trade: %v", err))
		} else {
			a.AddLog(fmt.Sprintf("SUCCESS: [BANKER] recorded trade from %s with %d item(s)", playerName, len(items)))
		}
	}()
}

func (a *App) handlePartnerAccept(e *g.Intercept) {
	a.AddLog("Partner accepted offer (Stage 1).")

	// Track partner acceptance for outgoing payout trades so automation can proceed
	a.tradeMu.Lock()
	if a.payoutTradeSent {
		a.tradeAccepted = true
	}
	a.tradeMu.Unlock()

	// Final validation for incoming trades
	if !a.payoutTradeSent {
		a.tradeMu.Lock()
		allowedNames := a.allowedNamesCache
		items := a.currentTradeItems
		partnerName := a.lastTradePartner
		partnerTradeID := a.lastTradePartnerID
		partnerChatID := a.lastTradePartnerChatID
		a.tradeMu.Unlock()

		// SECURITY: Ensure we have a valid identified partner before accepting any items
		// Note: Room index 0 is valid, so we only check if partnerName is resolved
		if partnerName == "" {
			a.AddLog("[SECURITY] Blocking trade: Partner identity (Name) could not be verified from room data.")
			e.Block()
			a.ext.Send(g.Out.Id("TRADE_CLOSE_OUT"))
			return
		}

		// Verbose debugging
		a.AddLog(fmt.Sprintf("[DEBUG] Validating trade for %s (ID:%d, ChatID:%d). Allowed items: %v", partnerName, partnerTradeID, partnerChatID, allowedNames))
		// Log a safe version of the packet data (printable chars only)
		safeItems := ""
		for _, b := range []byte(items) {
			if b >= 32 && b <= 126 {
				safeItems += string(b)
			} else {
				safeItems += "."
			}
		}
		a.AddLog(fmt.Sprintf("[DEBUG] Current trade packet data: %s", safeItems))

		if len(allowedNames) > 0 {
			// Build allowed name set
			allowedSet := make(map[string]bool)
			for _, n := range allowedNames {
				allowedSet[strings.ToLower(strings.TrimSpace(n))] = true
			}

			// Snapshot last parsed items (if any)
			a.tradeMu.Lock()
			lastItems := a.lastTradeItems
			a.tradeMu.Unlock()

			// Map of matched allowed item -> qty
			matchedItems := make(map[string]int)
			for _, it := range lastItems {
				low := strings.ToLower(strings.TrimSpace(it.Name))
				matchedKey := ""
				if allowedSet[low] {
					matchedKey = low
				} else {
					for k := range allowedSet {
						if k != "" && strings.Contains(low, k) {
							matchedKey = k
							break
						}
					}
				}
				if matchedKey != "" {
					matchedItems[matchedKey] += it.Quantity
				}
			}

			// If we couldn't parse items, fallback to previous substring check
			if len(matchedItems) == 0 {
				matched := false
				lowerItems := strings.ToLower(items)
				for _, name := range allowedNames {
					if strings.Contains(lowerItems, strings.ToLower(name)) {
						matched = true
						a.AddLog(fmt.Sprintf("[FILTER] Validated trade: matched stocked item '%s' (fallback)", name))
						break
					}
				}
				if !matched {
					a.AddLog("[FILTER] Blocking acceptance: no stocked items found in trade.")
					if a.ctx != nil {
						go runtime.EventsEmit(a.ctx, "debugEvent", map[string]interface{}{"ts": time.Now().Format(time.RFC3339), "type": "incoming-trade", "decision": "blocked", "reason": "no_stocked_items", "player": partnerName, "allowed": allowedNames})
					}
					e.Block()
					// Send TRADE_CLOSE to force the window shut for them
					a.ext.Send(g.Out.Id("TRADE_CLOSE_OUT"))
					return
				}

				// Accept by fallback since we can't determine counts
				go func() {
					time.Sleep(1500 * time.Millisecond)
					a.tradeMu.Lock()
					active := a.tradeActive
					a.tradeMu.Unlock()
					if active {
						a.AddLog("Automatically accepting trade (Stage 1 - fallback)...")
						a.ext.Send(g.Out.Id("TRADE_ACCEPT_OUT"))
						if a.ctx != nil {
							go runtime.EventsEmit(a.ctx, "debugEvent", map[string]interface{}{"ts": time.Now().Format(time.RFC3339), "type": "incoming-trade", "decision": "accepted", "reason": "fallback_no_counts", "player": partnerName, "allowed": allowedNames})
						}
					}
				}()
				return
			}

			// Enforce configured limits per-settings
			maxQty := a.getIntSetting("max_qty_per_unique", 10)
			maxUnique := a.getIntSetting("max_unique_items", 6)

			// Check per-item qty
			for itName, qty := range matchedItems {
				if maxQty > 0 && qty > maxQty {
					a.AddLog(fmt.Sprintf("[FILTER] Blocking acceptance: %s offered %d which exceeds max per-unique %d", itName, qty, maxQty))
					if a.ctx != nil {
						go runtime.EventsEmit(a.ctx, "debugEvent", map[string]interface{}{"ts": time.Now().Format(time.RFC3339), "type": "incoming-trade", "decision": "blocked", "reason": "max_qty_exceeded", "player": partnerName, "item": itName, "qty": qty, "max_qty": maxQty})
					}
					e.Block()
					a.ext.Send(g.Out.Id("TRADE_CLOSE_OUT"))
					return
				}
			}

			// Check unique count
			if maxUnique > 0 && len(matchedItems) > maxUnique {
				a.AddLog(fmt.Sprintf("[FILTER] Blocking acceptance: player offered %d unique stocked items (limit %d)", len(matchedItems), maxUnique))
				if a.ctx != nil {
					go runtime.EventsEmit(a.ctx, "debugEvent", map[string]interface{}{"ts": time.Now().Format(time.RFC3339), "type": "incoming-trade", "decision": "blocked", "reason": "max_unique_exceeded", "player": partnerName, "unique_offered": len(matchedItems), "max_unique": maxUnique, "items": matchedItems})
				}
				e.Block()
				a.ext.Send(g.Out.Id("TRADE_CLOSE_OUT"))
				return
			}

			// Allowed: accept and emit debug
			a.AddLog(fmt.Sprintf("[FILTER] Validated trade: matched items %v", matchedItems))
			if a.ctx != nil {
				go runtime.EventsEmit(a.ctx, "debugEvent", map[string]interface{}{"ts": time.Now().Format(time.RFC3339), "type": "incoming-trade", "decision": "accepted", "player": partnerName, "items": matchedItems, "max_qty": maxQty, "max_unique": maxUnique})
			}

			// Validated: Automatically accept the trade (Stage 1)
			go func() {
				time.Sleep(1500 * time.Millisecond) // Give the game a moment to process their accept
				a.tradeMu.Lock()
				active := a.tradeActive
				a.tradeMu.Unlock()
				if active {
					a.AddLog("Automatically accepting trade (Stage 1)...")
					a.ext.Send(g.Out.Id("TRADE_ACCEPT_OUT"))
				}
			}()

		} else {
			a.AddLog("[DEBUG] No active stocked items found in cache. This might be because the database fetch failed or no items are active. Allowing trade by default to prevent lockout.")
			// Automatically accept the trade if filter is essentially disabled
			go func() {
				time.Sleep(1500 * time.Millisecond)
				a.tradeMu.Lock()
				active := a.tradeActive
				a.tradeMu.Unlock()
				if active {
					a.AddLog("Automatically accepting trade (Stage 1 - No Filter)...")
					a.ext.Send(g.Out.Id("TRADE_ACCEPT_OUT"))
					if a.ctx != nil {
						go runtime.EventsEmit(a.ctx, "debugEvent", map[string]interface{}{"ts": time.Now().Format(time.RFC3339), "type": "incoming-trade", "decision": "accepted", "reason": "no_filter", "player": partnerName})
					}
				}
			}()
		}
	}
}

func (a *App) handlePartnerConfirm(e *g.Intercept) {
	a.AddLog("Partner confirmed trade (Stage 2).")

	if !a.payoutTradeSent {
		// Automatically confirm the trade after 4 seconds (Stage 2)
		go func() {
			time.Sleep(4000 * time.Millisecond)

			a.tradeMu.Lock()
			active := a.tradeActive
			a.tradeMu.Unlock()

			if active {
				a.AddLog("Automatically confirming trade (Stage 2)...")
				a.ext.Send(g.Out.Id("TRADE_CONFIRM_ACCEPT_OUT"))
			} else {
				a.AddLog("Stage 2 aborted: trade closed before automatic confirm.")
			}
		}()
	}
}

func (a *App) handleTradeClose(e *g.Intercept) {
	a.tradeMu.Lock()
	partner := a.activeTradePartner
	screenshotPath := a.lastScreenshotPath
	a.tradeActive = false
	// Do not immediately clear activeTradePartner/payoutTradeSent; let automation retry if inflight
	a.lastScreenshotPath = ""
	a.tradeMu.Unlock()

	if screenshotPath != "" {
		os.Remove(screenshotPath)
	}

	if partner != "" {
		// If there's an inflight automation for this partner, do not re-queue; let automation reopen
		if id, ok := a.getInflight(partner); ok {
			// Clear any stale activeTradeTarget (often set by inbound TRADE_OPEN) so retries
			// re-resolve the player's current room index (ChatID) from parse28.
			a.AddLog(fmt.Sprintf("Trade with %s closed during automation (inflight id=%s). Clearing active target, refreshing room users and will retry shortly.", partner, id))
			a.tradeMu.Lock()
			a.activeTradeTarget = 0
			a.payoutTradeSent = false
			a.tradeMu.Unlock()
			go a.requestRoomUsers()
			a.emitUpdate()
			return
		}

		a.pMu.Lock()
		changedIDs := []string{}
		for i, p := range a.payouts {
			if strings.EqualFold(normalizeName(p.Name), normalizeName(partner)) && p.Status == "Trading" {
				// Re-queue it. If they are still in room, the monitor will pick it up again in 2s.
				a.payouts[i].Status = "Pending"
				changedIDs = append(changedIDs, p.ID)
			}
		}
		a.pMu.Unlock()

		if len(changedIDs) > 0 {
			for _, id := range changedIDs {
				a.persistPayoutStatus(id, "Pending")
			}
			a.AddLog(fmt.Sprintf("Trade with %s closed without completing. Re-queueing for retry...", partner))
			// Trigger a status check immediately to see if they are still here
			a.updatePayoutStatuses()
		}
		a.emitUpdate()
	} else {
		a.AddLog("Trade closed.")
	}
}

func (a *App) handleTradeCompleted(e *g.Intercept) {
	a.AddLog("TRADE_COMPLETED packet intercepted.")
	a.tradeMu.Lock()
	partner := a.activeTradePartner
	screenshotPath := a.lastScreenshotPath
	payoutSent := a.payoutTradeSent

	lastPartner := a.lastTradePartner
	lastItems := a.lastTradeItems
	lastTradeID := a.lastTradePartnerID
	lastChatID := a.lastTradePartnerChatID

	a.tradeActive = false
	a.lastScreenshotPath = ""
	a.tradeMu.Unlock()

	// If this was an automated payout, clear inflight mapping for partner
	if partner != "" {
		a.unmarkInflight(partner)
	}

	// If it was a bet (incoming trade), record to banker_trades
	if !payoutSent && len(lastItems) > 0 {
		name := lastPartner
		if name == "" {
			name = "Unknown"
		}
		a.recordBankerTrade(name, lastItems, lastTradeID, lastChatID)
	}

	// Attempt to mark any matching payout(s) as completed. Prefer active partner name match,
	// but also fall back to matching by the last trade id or last partner name when available.
	foundCompleted := false

	a.pMu.Lock()
	for i, p := range a.payouts {
		// Skip entries that are already finished
		if p.Status == "Completed" || p.Status == "Disabled" {
			continue
		}

		match := false

		// 1) If we have an active partner name, match by that (preferred)
		if partner != "" && strings.EqualFold(normalizeName(p.Name), normalizeName(partner)) && p.Status == "Trading" {
			match = true
		}

		// 2) Fallback: match by lastTradeID if available
		if !match && lastTradeID > 0 && p.TradeID > 0 && p.TradeID == lastTradeID {
			match = true
		}

		// 3) Fallback: match by lastPartner name (case-insensitive)
		if !match && lastPartner != "" && strings.EqualFold(normalizeName(p.Name), normalizeName(lastPartner)) {
			match = true
		}

		if match {
			a.payouts[i].Status = "Completed"
			foundCompleted = true

			if a.db != nil {
				// Persist payout completion
				if _, err := a.db.Exec(context.Background(), "UPDATE public.auto_payouts SET status = 'Completed' WHERE id = $1", p.ID); err != nil {
					a.AddLog("ERROR: Failed to update DB status: " + err.Error())
				}
			}

			a.AddLog(fmt.Sprintf("Payout for %s SUCCESSFUL. Items delivered.", p.Name))
			a.sendDiscordNotification(a.payouts[i], screenshotPath)

			// Also mark any associated banker_trades as completed so the dealer bot re-opens
			if a.db != nil {
				var err error
				if p.BankerTradeID > 0 {
					_, err = a.db.Exec(context.Background(), "UPDATE public.banker_trades SET status = 'completed', risk_status = 'completed', risk_bank = 0 WHERE id = $1 AND status = 'paying'", p.BankerTradeID)
				} else if p.TradeID > 0 {
					_, err = a.db.Exec(context.Background(), "UPDATE public.banker_trades SET status = 'completed', risk_status = 'completed', risk_bank = 0 WHERE player_trade_id = $1 AND status = 'paying'", p.TradeID)
				} else {
					_, err = a.db.Exec(context.Background(), "UPDATE public.banker_trades SET status = 'completed', risk_status = 'completed', risk_bank = 0 WHERE player_name = $1 AND status = 'paying'", p.Name)
				}
				if err != nil {
					a.AddLog(fmt.Sprintf("ERROR: Failed to update banker_trades for %s/%d (banker_id=%d): %v", p.Name, p.TradeID, p.BankerTradeID, err))
				} else {
					a.AddLog(fmt.Sprintf("Marked banker_trades for %s/%d (banker_id=%d) as completed", p.Name, p.TradeID, p.BankerTradeID))
				}
			}
		}
	}
	a.pMu.Unlock()

	if foundCompleted {
		a.emitUpdate()
	}
}

func (a *App) payoutMonitor() {
	a.AddLog("Trade monitor loop started.")
	cycle := 0
	for {
		time.Sleep(2 * time.Second)
		cycle++

		a.pMu.RLock()
		var targets []Payout
		for _, p := range a.payouts {
			if p.Status == "In Room" {
				targets = append(targets, p)
			}
		}
		a.pMu.RUnlock()

		a.tradeMu.Lock()
		tradeActive := a.tradeActive
		partner := a.activeTradePartner
		a.tradeMu.Unlock()

		if tradeActive {
			continue
		}

		// Safety cleanup: If no trade is active and no partner is being tracked,
		// ensure no payouts are stuck in "Trading" status.
		if partner == "" {
			a.pMu.Lock()
			changedIDs := []string{}
			for i, p := range a.payouts {
				if p.Status == "Trading" {
					a.payouts[i].Status = "Pending"
					changedIDs = append(changedIDs, p.ID)
				}
			}
			a.pMu.Unlock()
			if len(changedIDs) > 0 {
				for _, id := range changedIDs {
					// Best-effort persist to DB
					a.persistPayoutStatus(id, "Pending")
				}
				a.AddLog("Safety: Reset stuck 'Trading' status for payouts.")
				go a.emitUpdate()
			}
		}

		if len(targets) == 0 {
			if cycle%30 == 0 {
				// a.AddLog("DEBUG: Trade monitor heartbeat (scanning, no targets in room).")
			}
			continue
		}

		target := targets[0]
		targetID := target.ID
		targetName := target.Name
		targetItem := target.ItemName

		// 1. Auto-refresh hand inventory before opening trade
		a.RefreshInventory()
		time.Sleep(3 * time.Second) // Wait for scan to finish

		a.roomUsersMu.RLock()
		roomCount := len(a.roomUsers)
		a.roomUsersMu.RUnlock()
		a.AddLog(fmt.Sprintf("DEBUG: roomUsers=%d looking up '%s'", roomCount, strings.ToLower(normalizeName(targetName))))

		a.roomUsersMu.RLock()
		user, ok := a.roomUsers[strings.ToLower(normalizeName(targetName))]
		a.roomUsersMu.RUnlock()

		if !ok {
			a.AddLog(fmt.Sprintf("DEBUG: %s not in room map; requesting fresh room scan before fallback", targetName))

			// Request an immediate room users refresh and wait briefly for parse28 to populate
			go a.requestRoomUsers()
			for i := 0; i < 8; i++ {
				time.Sleep(100 * time.Millisecond)
				a.roomUsersMu.RLock()
				u, ok2 := a.roomUsers[strings.ToLower(normalizeName(targetName))]
				a.roomUsersMu.RUnlock()
				if ok2 {
					user = u
					ok = true
					break
				}
			}

			if ok {
				// allow normal flow to continue using found user
			} else {
				// Fallback: if we have a player_trade_id from the DB, attempt to open a trade by that id
				if target.TradeID > 0 {
					a.AddLog(fmt.Sprintf("Attempting trade by player_trade_id=%d for %s", target.TradeID, targetName))

					// Set trade state to indicate we're attempting a trade
					a.tradeMu.Lock()
					a.activeTradePartner = targetName
					a.activeTradeTarget = target.TradeID
					a.payoutTradeSent = true
					a.tradeMu.Unlock()

					// Mark payout as Trading in the in-memory slice and persist to DB
					foundInSlice := false
					a.pMu.Lock()
					for i := range a.payouts {
						if a.payouts[i].ID == targetID {
							a.payouts[i].Status = "Trading"
							foundInSlice = true
							break
						}
					}
					a.pMu.Unlock()
					if foundInSlice {
						a.persistPayoutStatus(targetID, "Trading")
					}
					go a.emitUpdate()

					if !foundInSlice {
						a.AddLog("ERROR: Target record lost during trade setup (fallback)")
						a.tradeMu.Lock()
						a.activeTradePartner = ""
						a.tradeMu.Unlock()
						continue
					}

					a.AddLog(fmt.Sprintf("Initiating auto-trade by TradeID for %s (TradeID: %d) for %s...", targetName, target.TradeID, targetItem))
					a.AddLog(fmt.Sprintf("Sending TRADE_OPEN_OUT by TradeID %d (both forms)", target.TradeID))
					a.ext.Send(g.Out.Id("TRADE_OPEN_OUT"), target.TradeID)
					a.ext.Send(g.Out.Id("TRADE_OPEN_OUT"), []byte(encodeVL64(target.TradeID)))

					pCopy := target
					pCopy.Status = "Trading"
					go a.automateTrade(&pCopy)
					continue
				}
			}

			a.AddLog(fmt.Sprintf("DEBUG: Monitor waiting for %s to be re-indexed in room map...", targetName))
			continue
		}

		// Prefer trading by the player's room ChatID when they're present in-room.
		// Use TradeID only when ChatID is not available (player not in room).
		roomIndex := user.ChatID
		selectedTarget := roomIndex
		usingTradeID := false
		if roomIndex > 0 {
			// Use room index for outgoing trade opens (works reliably)
			selectedTarget = roomIndex
			usingTradeID = false
		} else if user.TradeID > 0 {
			// Fallback to TradeID when no room index is present
			selectedTarget = user.TradeID
			usingTradeID = true
		}
		if selectedTarget < 0 {
			a.AddLog(fmt.Sprintf("ERROR: %s has invalid target %d", targetName, selectedTarget))
			continue
		}

		a.tradeMu.Lock()
		a.activeTradePartner = targetName
		a.activeTradeTarget = selectedTarget
		a.payoutTradeSent = true
		a.tradeMu.Unlock()

		foundInSlice := false
		a.pMu.Lock()
		for i := range a.payouts {
			if a.payouts[i].ID == targetID {
				a.payouts[i].Status = "Trading"
				foundInSlice = true
				break
			}
		}
		a.pMu.Unlock()
		if foundInSlice {
			a.persistPayoutStatus(targetID, "Trading")
		}
		go a.emitUpdate()

		if !foundInSlice {
			a.AddLog("ERROR: Target record lost during trade setup")
			// Reset tracking
			a.tradeMu.Lock()
			a.activeTradePartner = ""
			a.tradeMu.Unlock()
			continue
		}

		if usingTradeID {
			a.AddLog(fmt.Sprintf("Initiating auto-trade for %s (TradeID: %d) for %s...", targetName, selectedTarget, targetItem))
			// Many servers reliably accept VL64-encoded TradeID opens. Try VL64-only first.
			vl := []byte(encodeVL64(selectedTarget))
			a.AddLog(fmt.Sprintf("Sending TRADE_OPEN_OUT by TradeID %d (vl=%x)", selectedTarget, vl))
			a.ext.Send(g.Out.Id("TRADE_OPEN_OUT"), vl)
			// small delay then try integer form as a fallback
			time.Sleep(150 * time.Millisecond)
			a.ext.Send(g.Out.Id("TRADE_OPEN_OUT"), selectedTarget)
		} else {
			a.AddLog(fmt.Sprintf("Initiating auto-trade for %s (RoomIndex: %d) for %s...", targetName, selectedTarget, targetItem))
			a.AddLog(fmt.Sprintf("Sending TRADE_OPEN_OUT to RoomIndex %d (both forms)", selectedTarget))
			a.ext.Send(g.Out.Id("TRADE_OPEN_OUT"), selectedTarget)
			a.ext.Send(g.Out.Id("TRADE_OPEN_OUT"), []byte(encodeVL64(selectedTarget)))
		}

		// Create a snapshot for the automation goroutine; ensure it has the most-recent trade id
		pCopy := target
		pCopy.Status = "Trading"
		if usingTradeID {
			pCopy.TradeID = selectedTarget
		}
		go a.automateTrade(&pCopy)
	}
}

func (a *App) automateTrade(p *Payout) {
	a.AddLog(fmt.Sprintf("Starting automation for %s: %d x %s (ID: %s)", p.Name, p.Quantity, p.ItemName, p.ID))

	// Mark this payout as inflight so other handlers know not to re-queue it
	a.markInflight(p.Name, p.ID)
	defer func() {
		a.unmarkInflight(p.Name)
		a.tradeMu.Lock()
		if a.activeTradePartner == p.Name {
			a.activeTradePartner = ""
		}
		a.payoutTradeSent = false
		a.tradeMu.Unlock()
	}()

	maxAttempts := 5

	// Capture initial target if set by monitor
	a.tradeMu.Lock()
	initialTarget := a.activeTradeTarget
	a.tradeMu.Unlock()

	for attemptNum := 1; attemptNum <= maxAttempts; attemptNum++ {
		a.AddLog(fmt.Sprintf("Automation attempt %d/%d for %s", attemptNum, maxAttempts, p.Name))

		// Check DB first: if the payout was marked Completed/Disabled there,
		// abort automation to avoid re-trading a finished entry.
		if a.db != nil {
			if completed, err := a.isPayoutCompletedInDB(p.ID); err == nil && completed {
				a.AddLog(fmt.Sprintf("Payout %s already completed in DB; aborting automation.", p.ID))
				a.pMu.Lock()
				for i := range a.payouts {
					if a.payouts[i].ID == p.ID {
						a.payouts[i].Status = "Completed"
						break
					}
				}
				a.pMu.Unlock()
				go a.emitUpdate()
				return
			}
		}

		// Ensure in-memory state shows Trading and persist it
		a.pMu.Lock()
		for i := range a.payouts {
			if a.payouts[i].ID == p.ID {
				a.payouts[i].Status = "Trading"
				break
			}
		}
		a.pMu.Unlock()
		a.persistPayoutStatus(p.ID, "Trading")
		a.emitUpdate()

		// Ensure active trade partner/target are set so reopen attempts work
		a.tradeMu.Lock()
		a.activeTradePartner = p.Name
		if a.activeTradeTarget == 0 {
			a.activeTradeTarget = initialTarget
		}
		// Mark that this is a payout trade
		a.payoutTradeSent = true
		openTarget := a.activeTradeTarget
		a.tradeMu.Unlock()

		// Re-resolve the player's current room index (ChatID) from the latest parse28
		// snapshot - prefer ChatID when available to avoid using stale inbound trade ids.
		a.roomUsersMu.RLock()
		if u, ok := a.roomUsers[strings.ToLower(normalizeName(p.Name))]; ok {
			if u.ChatID > 0 {
				openTarget = u.ChatID
				// also update the activeTradeTarget so future attempts use the room index
				a.tradeMu.Lock()
				a.activeTradeTarget = openTarget
				a.tradeMu.Unlock()
			} else if u.TradeID > 0 {
				openTarget = u.TradeID
				a.tradeMu.Lock()
				a.activeTradeTarget = openTarget
				a.tradeMu.Unlock()
			}
		}
		a.roomUsersMu.RUnlock()

		// If we don't have a target, fall back to DB trade id
		if openTarget == 0 && p.TradeID > 0 {
			openTarget = p.TradeID
		}

		// Attempt to open trade
		if openTarget != 0 {
			vl := []byte(encodeVL64(openTarget))
			// If this openTarget matches the DB TradeID for this payout, prefer VL64-first
			isTradeID := p.TradeID > 0 && openTarget == p.TradeID
			if isTradeID {
				a.AddLog(fmt.Sprintf("Initiating TRADE_OPEN (attempt %d) for %s TradeID=%d vl=%x (VL64-first)", attemptNum, p.Name, openTarget, vl))
				a.ext.Send(g.Out.Id("TRADE_OPEN_OUT"), vl)
				time.Sleep(150 * time.Millisecond)
				a.ext.Send(g.Out.Id("TRADE_OPEN_OUT"), openTarget)
			} else {
				a.AddLog(fmt.Sprintf("Initiating TRADE_OPEN (attempt %d) for %s target=%d vl=%x", attemptNum, p.Name, openTarget, vl))
				// Send both integer and VL64 forms; repeat a couple times to improve reliability
				for s := 0; s < 2; s++ {
					a.ext.Send(g.Out.Id("TRADE_OPEN_OUT"), openTarget)
					a.ext.Send(g.Out.Id("TRADE_OPEN_OUT"), vl)
					time.Sleep(120 * time.Millisecond)
				}
			}
		} else {
			// Try to resolve from room users
			a.roomUsersMu.RLock()
			u, ok := a.roomUsers[strings.ToLower(normalizeName(p.Name))]
			a.roomUsersMu.RUnlock()
			if !ok {
				a.AddLog("No known room target to open trade for " + p.Name + " - will retry shortly")
				time.Sleep(1 * time.Second)
				continue
			}
			a.tradeMu.Lock()
			a.activeTradeTarget = u.ChatID
			a.tradeMu.Unlock()
			a.AddLog(fmt.Sprintf("Initiating TRADE_OPEN to RoomIndex %d for %s", u.ChatID, p.Name))
			a.ext.Send(g.Out.Id("TRADE_OPEN_OUT"), u.ChatID)
			a.ext.Send(g.Out.Id("TRADE_OPEN_OUT"), []byte(encodeVL64(u.ChatID)))
		}

		// Wait up to 5s for the trade window to open
		tradeOpened := false
		for i := 0; i < 50; i++ {
			time.Sleep(100 * time.Millisecond)
			a.tradeMu.Lock()
			if a.tradeActive {
				tradeOpened = true
				a.tradeMu.Unlock()
				break
			}
			a.tradeMu.Unlock()
		}

		if !tradeOpened {
			a.AddLog("Trade did not open; will retry.")
			a.ext.Send(g.Out.Id("TRADE_CLOSE_OUT"))
			a.tradeMu.Lock()
			a.payoutTradeSent = false
			a.tradeMu.Unlock()
			// Refresh room user list before retrying so we can resolve ChatID again
			go a.requestRoomUsers()
			time.Sleep(2 * time.Second)
			continue
		}

		a.AddLog(fmt.Sprintf("Automation: trade window detected open for %s (attempt %d)", p.Name, attemptNum))

		// Refresh inventory snapshot for each attempt
		a.inventoryMu.RLock()
		ids, ok := a.inventory[strings.ToLower(p.ItemName)]
		inventoryCount := len(ids)
		a.inventoryMu.RUnlock()

		if !ok || inventoryCount == 0 {
			a.AddLog(fmt.Sprintf("ERROR: Inventory shortage for '%s'. Scanned hand has 0.", p.ItemName))
			// Re-queue as Pending
			a.pMu.Lock()
			for i, entry := range a.payouts {
				if entry.ID == p.ID && entry.Status == "Trading" {
					a.payouts[i].Status = "Pending"
				}
			}
			a.pMu.Unlock()
			a.persistPayoutStatus(p.ID, "Pending")
			a.emitUpdate()
			return
		}

		toAdd := p.Quantity
		if inventoryCount < toAdd {
			a.AddLog(fmt.Sprintf("WARNING: Requesting %d but only have %d of %s. Trading available amount.", toAdd, inventoryCount, p.ItemName))
			toAdd = inventoryCount
		}

		// Add items
		a.AddLog(fmt.Sprintf("Adding %d x %s (Total available: %d)...", toAdd, p.ItemName, inventoryCount))
		itemsAdded := 0
		aborted := false
		for i := 0; i < toAdd; i++ {
			a.tradeMu.Lock()
			if !a.tradeActive {
				a.tradeMu.Unlock()
				a.AddLog("Adding items aborted: trade closed unexpectedly.")
				aborted = true
				break
			}
			a.tradeMu.Unlock()

			itemID := ids[i]
			a.ext.Send(g.Out.Id("TRADE_ADDITEM_OUT"), itemID)
			a.AddLog(fmt.Sprintf("Sent TRADE_ADDITEM_OUT for ID: %d (%d/%d)", itemID, i+1, toAdd))
			itemsAdded++
			time.Sleep(750 * time.Millisecond)
		}

		if aborted {
			// Closed during item add; try again
			a.ext.Send(g.Out.Id("TRADE_CLOSE_OUT"))
			a.tradeMu.Lock()
			a.payoutTradeSent = false
			a.tradeMu.Unlock()
			// Let the room index refresh and give the client time to settle
			go a.requestRoomUsers()
			time.Sleep(1500 * time.Millisecond)
			continue
		}

		// Stage 1: accept offer (ours)
		a.AddLog("Finalizing stage 1 (Accept Offer)...")
		for acc := 1; acc <= 3; acc++ {
			time.Sleep(1500 * time.Millisecond)
			a.tradeMu.Lock()
			if !a.tradeActive {
				a.tradeMu.Unlock()
				break
			}
			a.tradeMu.Unlock()

			a.ext.Send(g.Out.Id("TRADE_ACCEPT_OUT"))
			a.AddLog(fmt.Sprintf("Sent TRADE_ACCEPT_OUT attempt %d/3", acc))
		}

		// Wait up to 30s for the partner to accept (Stage 1)
		a.tradeMu.Lock()
		a.tradeAccepted = false
		a.tradeMu.Unlock()

		accepted := false
		waitStart := time.Now()
		for time.Since(waitStart) < 30*time.Second {
			time.Sleep(500 * time.Millisecond)
			a.tradeMu.Lock()
			if !a.tradeActive {
				a.tradeMu.Unlock()
				break
			}
			if a.tradeAccepted {
				accepted = true
				a.tradeMu.Unlock()
				break
			}
			a.tradeMu.Unlock()
		}

		if !accepted {
			a.AddLog("Partner did not accept within 30s; closing trade and retrying...")
			a.ext.Send(g.Out.Id("TRADE_CLOSE_OUT"))
			a.tradeMu.Lock()
			a.payoutTradeSent = false
			a.tradeMu.Unlock()
			// Refresh room users and wait a short period before retrying
			go a.requestRoomUsers()
			time.Sleep(2 * time.Second)
			continue
		}

		// Partner accepted; proceed to confirmation stage and wait for completion
		a.AddLog("Partner accepted. Entering confirmation stage.")
		completed := false
		for confirmAttempt := 1; confirmAttempt <= 5; confirmAttempt++ {
			time.Sleep(4000 * time.Millisecond)

			// Quick DB check: if payout already Completed in DB, abort immediately
			if a.db != nil {
				if completedDB, err := a.isPayoutCompletedInDB(p.ID); err == nil && completedDB {
					a.AddLog(fmt.Sprintf("Payout %s already completed in DB; aborting confirm loop.", p.ID))
					return
				}
			}

			a.tradeMu.Lock()
			active := a.tradeActive
			a.tradeMu.Unlock()

			if !active {
				a.AddLog("Trade closed during confirmation stage.")
				// See if handler already marked Completed (check both memory and DB)
				a.pMu.RLock()
				for _, pp := range a.payouts {
					if pp.ID == p.ID && pp.Status == "Completed" {
						completed = true
						break
					}
				}
				a.pMu.RUnlock()
				if !completed && a.db != nil {
					if completedDB, err := a.isPayoutCompletedInDB(p.ID); err == nil && completedDB {
						completed = true
					}
				}
				break
			}

			if confirmAttempt == 1 {
				a.AddLog("Capturing trade confirmation screenshot...")
				go func() {
					path := a.takeScreenshot()
					a.tradeMu.Lock()
					a.lastScreenshotPath = path
					a.tradeMu.Unlock()
				}()
			}

			a.AddLog(fmt.Sprintf("Finalizing stage 2 (Confirm Trade) attempt %d/5...", confirmAttempt))

			// Before sending confirm, re-check DB to avoid racing with handler
			if a.db != nil {
				if completedDB, err := a.isPayoutCompletedInDB(p.ID); err == nil && completedDB {
					a.AddLog(fmt.Sprintf("Payout %s already completed in DB; skipping confirm send.", p.ID))
					return
				}
			}

			a.ext.Send(g.Out.Id("TRADE_CONFIRM_ACCEPT_OUT"))

			// Check if the payout was marked Completed by the intercepted handler (memory)
			a.pMu.RLock()
			for _, pp := range a.payouts {
				if pp.ID == p.ID && pp.Status == "Completed" {
					completed = true
					break
				}
			}
			a.pMu.RUnlock()

			// Also check DB after send in case completion happened concurrently
			if !completed && a.db != nil {
				if completedDB, err := a.isPayoutCompletedInDB(p.ID); err == nil && completedDB {
					completed = true
				}
			}

			if completed {
				a.AddLog("Payout marked completed by handler.")
				return
			}
		}

		if completed {
			return
		}

		a.AddLog(fmt.Sprintf("Attempt %d/%d did not complete; will retry.", attemptNum, maxAttempts))
		// Close and retry
		a.ext.Send(g.Out.Id("TRADE_CLOSE_OUT"))
		a.tradeMu.Lock()
		a.payoutTradeSent = false
		a.tradeMu.Unlock()
		// Refresh room users and wait before retrying to avoid immediate reopen failures
		go a.requestRoomUsers()
		time.Sleep(2 * time.Second)
	}

	// Exhausted attempts: force-complete and notify
	a.AddLog(fmt.Sprintf("Payout %s failed after %d attempts; forcing completion and notifying.", p.ID, maxAttempts))
	a.pMu.Lock()
	for i := range a.payouts {
		if a.payouts[i].ID == p.ID {
			a.payouts[i].Status = "Completed"
			break
		}
	}
	a.pMu.Unlock()
	a.persistPayoutStatus(p.ID, "Completed")
	a.emitUpdate()

	// Also mark banker_trades as completed to keep the flow consistent
	if a.db != nil {
		var err error
		if p.BankerTradeID > 0 {
			_, err = a.db.Exec(context.Background(), "UPDATE public.banker_trades SET status = 'completed', risk_status = 'completed', risk_bank = 0 WHERE id = $1 AND status = 'paying'", p.BankerTradeID)
		} else if p.TradeID > 0 {
			_, err = a.db.Exec(context.Background(), "UPDATE public.banker_trades SET status = 'completed', risk_status = 'completed', risk_bank = 0 WHERE player_trade_id = $1 AND status = 'paying'", p.TradeID)
		} else {
			_, err = a.db.Exec(context.Background(), "UPDATE public.banker_trades SET status = 'completed', risk_status = 'completed', risk_bank = 0 WHERE player_name = $1 AND status = 'paying'", p.Name)
		}
		if err != nil {
			a.AddLog(fmt.Sprintf("ERROR: Failed to update banker_trades for %s/%d (banker_id=%d): %v", p.Name, p.TradeID, p.BankerTradeID, err))
		} else {
			a.AddLog(fmt.Sprintf("Marked banker_trades for %s/%d (banker_id=%d) as completed", p.Name, p.TradeID, p.BankerTradeID))
		}
	}

	// Send concise webhook notifying of unaccepted payout (always attempts to send)
	a.sendSimpleFailureWebhook(*p)
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
