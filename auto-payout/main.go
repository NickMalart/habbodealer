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
	"regexp"
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

//go:embed scripts/parse_users28.py
var users28Parser []byte

var itemRegex = regexp.MustCompile(`(?:CF_\d+_[a-z][a-z0-9_.-]*|[a-z][a-z0-9_.-]+_[a-z0-9_.-]+)(?:\*\d+)?`)

var figurePrefixes = []string{
	"hd-", "hr-", "ch-", "lg-", "sh-",
	"ha-", "he-", "ea-", "fa-", "ca-",
	"cc-", "wa-", "cp-",
}

func isUsersPacket(data []byte) bool {
	s := strings.ToLower(string(data))
	for _, p := range figurePrefixes {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

const (
	maxOpenTradeDuration = 2 * time.Minute
	banDuration          = 5 * time.Minute
	banMonitorInterval   = 5 * time.Second
)

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
	Name           string `json:"name"`
	Quantity       int    `json:"quantity"`
	IsUnrecognized bool   `json:"is_unrecognized,omitempty"`
	RawData        string `json:"raw_data,omitempty"`
}

// TradeState represents brief information about the currently active trade
// including elapsed/remaining seconds so the UI can render a live countdown.
type TradeState struct {
	Active             bool   `json:"active"`
	Partner            string `json:"partner"`
	PartnerID          int    `json:"partnerId"`
	ElapsedSeconds     int64  `json:"elapsedSeconds"`
	RemainingSeconds   int64  `json:"remainingSeconds"`
	MaxOpenSeconds     int64  `json:"maxOpenSeconds"`
	BanDurationSeconds int64  `json:"banDurationSeconds"`
}

// BanEntry is a UI-friendly representation of an active ban key.
type BanEntry struct {
	Key              string `json:"key"`
	Label            string `json:"label"`
	ExpiresAt        string `json:"expiresAt"`
	RemainingSeconds int64  `json:"remainingSeconds"`
	Message          string `json:"message"`
	IsActive         bool   `json:"isActive"`
}

type BanInfo struct {
	ExpiresAt time.Time `json:"expiresAt"`
	Message   string    `json:"message"`
	IsActive  bool      `json:"isActive"`
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
	activeTradePartner       string
	activeTradeTarget        int
	tradeActive              bool
	tradeAccepted            bool
	payoutTradeSent          bool
	currentTradeItems        string
	lastTradeItems           []TradeItem
	lastTradePartner         string
	lastTradePartnerID       int
	lastTradePartnerChatID   int
	allowedNamesCache        []string
	allowedDisplayNamesCache []string
	lastScreenshotPath       string
	tradeMu                  sync.Mutex
	tradeStartedAt           time.Time

	// Banker state
	bankerName   string
	bankerNameMu sync.RWMutex
	// When true, do not perform player hand/strip scans (banker/split mode)
	skipStripScan bool

	// Config & DB
	pythonExec     string
	pythonArgs     []string
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

	// In-memory ban list to block abusive partners (keyed by name:lower or tradeid:<id>)
	banList *BanList

	// Notified map to avoid duplicate failure webhooks in-memory
	notified   map[string]struct{}
	notifiedMu sync.RWMutex

	// Cross-bot shout queue state
	lastShoutTime map[string]time.Time
	shoutMu       sync.Mutex
}

func NewApp() *App {
	return &App{
		payouts:              []Payout{},
		logs:                 []string{"Bot initialized..."},
		roomUsers:            make(map[string]ParsedUsers28User),
		inventory:            make(map[string][]int),
		pythonExec:           "python",
		dbConnString:         "postgresql://neondb_owner:npg_z45TVuirPAvO@ep-steep-silence-a7kpyt3u-pooler.ap-southeast-2.aws.neon.tech/neondb?sslmode=require&channel_binding=require",
		discordWebhook:       "https://discord.com/api/webhooks/1519096496400760924/dsDuT5QTEahQ3l4BQ3L41h5lMu73iXpKAI2L6uCnNVfQJ4R6XJ-moQcqHwDFs53UaHR1",
		stripScanSeenItemIDs: make(map[int]struct{}),
		stripScanItemIDs:     make(map[string][]int),
		inflight:             make(map[string]string),
		notified:             make(map[string]struct{}),
		lastShoutTime:        make(map[string]time.Time),
		banList:              NewBanList(),
	}
}

func (a *App) queueShout(playerName, message string) {
	if a.db == nil || playerName == "" {
		return
	}

	a.shoutMu.Lock()
	if a.lastShoutTime == nil {
		a.lastShoutTime = make(map[string]time.Time)
	}
	lastTime, exists := a.lastShoutTime[strings.ToLower(playerName)]
	// 10 second cooldown per player to avoid spamming the Dealer shout queue
	if exists && time.Since(lastTime) < 10*time.Second {
		a.shoutMu.Unlock()
		return
	}
	a.lastShoutTime[strings.ToLower(playerName)] = time.Now()
	a.shoutMu.Unlock()

	owner := a.getOwnerKey()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := a.db.Exec(ctx, "INSERT INTO public.dealer_shouts (owner_key, target_player, message, shout_type, status, created_at) VALUES ($1, $2, $3, $4, 'pending', NOW())", owner, playerName, message, "error")
		if err != nil {
			a.AddLog("ERROR: Failed to insert shout: " + err.Error())
		} else {
			a.AddLog(fmt.Sprintf("DB: Queued shout for %s: %s (owner=%s)", playerName, message, owner))
		}
	}()
}

// BanList is a simple in-memory ban store keyed by arbitrary string keys
// (we use "name:<lower>" and "tradeid:<id>"). Expirations are stored as time.Time.
type BanList struct {
	mu   sync.RWMutex
	bans map[string]BanInfo
}

func NewBanList() *BanList {
	return &BanList{bans: make(map[string]BanInfo)}
}

func (b *BanList) Add(key string, d time.Duration, message string) BanInfo {
	b.mu.Lock()
	defer b.mu.Unlock()
	var expiry time.Time
	if d == 0 {
		expiry = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
	} else {
		expiry = time.Now().Add(d)
	}
	info := BanInfo{
		ExpiresAt: expiry,
		Message:   message,
		IsActive:  true,
	}
	b.bans[key] = info
	return info
}

// AddBan adds a ban to memory and DB.
func (a *App) AddBan(key string, d time.Duration, message string) {
	info := a.banList.Add(key, d, message)
	a.syncBanToDB(key, info)
}

// RemoveBan removes a ban from memory and DB.
func (a *App) RemoveBan(key string) {
	a.banList.Remove(key)
	a.removeBanFromDB(key)
}

// syncBanToDB persists a ban to the database.
func (a *App) syncBanToDB(key string, info BanInfo) {
	if a.db == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		query := `INSERT INTO public.banned_players (ban_key, expires_at, message, is_active) 
		          VALUES ($1, $2, $3, TRUE) 
		          ON CONFLICT (ban_key) DO UPDATE SET expires_at = EXCLUDED.expires_at, message = EXCLUDED.message, is_active = TRUE`
		_, err := a.db.Exec(ctx, query, key, info.ExpiresAt, info.Message)
		if err != nil {
			a.AddLog("ERROR: syncBanToDB failed: " + err.Error())
		}
	}()
}

// removeBanFromDB marks a ban as inactive in the database.
func (a *App) removeBanFromDB(key string) {
	if a.db == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := a.db.Exec(ctx, "UPDATE public.banned_players SET is_active = FALSE WHERE ban_key = $1", key)
		if err != nil {
			a.AddLog("ERROR: removeBanFromDB failed: " + err.Error())
		}
	}()
}

func (b *BanList) IsBanned(key string) bool {
	info, ok := b.Get(key)
	if !ok {
		return false
	}
	if time.Now().After(info.ExpiresAt) {
		b.Remove(key)
		return false
	}
	return true
}

func (b *BanList) Get(key string) (BanInfo, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	info, ok := b.bans[key]
	return info, ok
}

// Remove deletes a ban key immediately.
func (b *BanList) Remove(key string) {
	b.mu.Lock()
	delete(b.bans, key)
	b.mu.Unlock()
}

// Clear removes all bans from the memory map.
func (b *BanList) Clear() {
	b.mu.Lock()
	b.bans = make(map[string]BanInfo)
	b.mu.Unlock()
}

// PurgeExpired removes all expired bans from the in-memory map.
// Returns true if any bans were removed.
func (b *BanList) PurgeExpired() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	removed := false
	for k, info := range b.bans {
		if now.After(info.ExpiresAt) {
			delete(b.bans, k)
			removed = true
		}
	}
	return removed
}

// List returns a snapshot copy of the ban map.
func (b *BanList) List() map[string]BanInfo {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make(map[string]BanInfo, len(b.bans))
	for k, v := range b.bans {
		out[k] = v
	}
	return out
}

// getBanInfo checks both name and trade-id variants for an active ban and returns the info.
func (a *App) getBanInfo(name string, tradeID int) (BanInfo, bool) {
	if a.banList == nil {
		return BanInfo{}, false
	}
	if name != "" {
		norm := strings.ToLower(normalizeName(name))
		key := "name:" + norm
		if info, ok := a.banList.Get(key); ok {
			if time.Now().Before(info.ExpiresAt) {
				a.AddLog(fmt.Sprintf("[BAN_CHECK] Match by name: %s (key=%s)", name, key))
				return info, true
			}
			a.AddLog(fmt.Sprintf("[BAN_CHECK] Expired ban for %s removed", name))
			a.banList.Remove(key)
		}
	}
	if tradeID > 0 {
		key := fmt.Sprintf("tradeid:%d", tradeID)
		if info, ok := a.banList.Get(key); ok {
			if time.Now().Before(info.ExpiresAt) {
				a.AddLog(fmt.Sprintf("[BAN_CHECK] Match by TradeID: %d (key=%s)", tradeID, key))
				return info, true
			}
			a.AddLog(fmt.Sprintf("[BAN_CHECK] Expired ban for TradeID %d removed", tradeID))
			a.banList.Remove(key)
		}
	}
	return BanInfo{}, false
}

func formatRemainingTime(expiresAt time.Time) string {
	if expiresAt.Year() > 3000 {
		return "lifetime"
	}
	rem := time.Until(expiresAt)
	if rem <= 0 {
		return "0 mins"
	}

	days := int(rem.Hours() / 24)
	hours := int(rem.Hours())
	mins := int(rem.Minutes())

	if days > 1 {
		return fmt.Sprintf("%d days", days)
	} else if days == 1 {
		return "1 day"
	} else if hours > 1 {
		return fmt.Sprintf("%d hours", hours)
	} else if hours == 1 {
		return "1 hour"
	} else if mins > 1 {
		return fmt.Sprintf("%d mins", mins)
	} else {
		return "1 min"
	}
}

// isPartnerBanned checks both name and trade-id variants for an active ban.
func (a *App) isPartnerBanned(name string, tradeID int) bool {
	_, banned := a.getBanInfo(name, tradeID)
	return banned
}

// banMonitor watches the currently active trade and bans the partner if the
// trade remains open longer than maxOpenTradeDuration. It will attempt to
// close the trade and reset local state when banning occurs.
func (a *App) banMonitor() {
	a.AddLog("Ban monitor started.")
	ticker := time.NewTicker(banMonitorInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.tradeMu.Lock()
			active := a.tradeActive
			start := a.tradeStartedAt
			name := a.lastTradePartner
			id := a.lastTradePartnerID
			target := a.activeTradeTarget
			a.tradeMu.Unlock()

			if !active {
				continue
			}

			if id == 0 && target > 0 {
				id = target
			}

			if start.IsZero() {
				// If we didn't record a start time for some reason, set it now
				a.tradeMu.Lock()
				if a.tradeStartedAt.IsZero() {
					a.tradeStartedAt = time.Now()
				}
				a.tradeMu.Unlock()
				continue
			}

			if time.Since(start) > maxOpenTradeDuration {
				a.AddLog(fmt.Sprintf("[BAN] Trade with %s (id=%d) open > %s — banning for %s", name, id, maxOpenTradeDuration, banDuration))
				msg := fmt.Sprintf("%s you are banned from placing a bet", name)
				if name != "" {
					a.AddBan("name:"+strings.ToLower(normalizeName(name)), banDuration, msg)
				}
				if id > 0 {
					a.AddBan(fmt.Sprintf("tradeid:%d", id), banDuration, msg)
				}

				// Notify frontend of updated banlist
				if a.ctx != nil {
					go runtime.EventsEmit(a.ctx, "banListUpdate", a.GetBanList())
				}
				// Attempt to close the trade window immediately
				if a.ext != nil {
					a.ext.Send(g.Out.Id("TRADE_CLOSE_OUT"))
				}
				// Reset local trade state conservatively
				a.tradeMu.Lock()
				a.tradeActive = false
				a.payoutTradeSent = false
				a.activeTradePartner = ""
				a.activeTradeTarget = 0
				a.lastTradePartner = ""
				a.lastTradePartnerID = 0
				a.tradeStartedAt = time.Time{}
				a.tradeMu.Unlock()
				continue
			}

			// Also check if the current partner is ALREADY banned (e.g. manual ban via UI while trade active)
			if info, banned := a.getBanInfo(name, id); banned {
				a.AddLog(fmt.Sprintf("[BAN] Closing active trade with already banned partner %s (id=%d)", name, id))
				if name != "" {
					a.queueShout(name, fmt.Sprintf("%s - Remaining: %s", info.Message, formatRemainingTime(info.ExpiresAt)))
				}
				if a.ext != nil {
					a.ext.Send(g.Out.Id("TRADE_CLOSE_OUT"))
				}
				// Reset local trade state
				a.tradeMu.Lock()
				a.tradeActive = false
				a.payoutTradeSent = false
				a.activeTradePartner = ""
				a.activeTradeTarget = 0
				a.lastTradePartner = ""
				a.lastTradePartnerID = 0
				a.tradeStartedAt = time.Time{}
				a.tradeMu.Unlock()
			}
		case <-a.ctx.Done():
			return
		}
	}
}

func (a *App) loadBansFromDB() {
	if a.db == nil {
		return
	}
	a.AddLog("Loading active bans from database...")
	rows, err := a.db.Query(context.Background(), "SELECT ban_key, expires_at, message FROM public.banned_players WHERE is_active = TRUE AND expires_at > NOW()")
	if err != nil {
		a.AddLog("ERROR: loadBansFromDB failed: " + err.Error())
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var key, msg string
		var exp time.Time
		if err := rows.Scan(&key, &exp, &msg); err == nil {
			a.AddLog(fmt.Sprintf("[DB_LOAD] Loading active ban: %s (expires: %s)", key, exp.Format(time.RFC3339)))
			a.banList.Add(key, exp.Sub(time.Now()), msg)
			count++
		}
	}
	a.AddLog(fmt.Sprintf("Loaded %d active bans from DB.", count))
}

func (a *App) cleanupBans() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			removed := false
			if a.db != nil {
				_, err := a.db.Exec(context.Background(), "UPDATE public.banned_players SET is_active = FALSE WHERE expires_at <= NOW() AND is_active = TRUE")
				if err != nil {
					a.AddLog("ERROR: cleanupBans failed: " + err.Error())
				}
			}
			if a.banList != nil {
				if a.banList.PurgeExpired() {
					removed = true
				}
			}
			if removed && a.ctx != nil {
				go runtime.EventsEmit(a.ctx, "banListUpdate", a.GetBanList())
			}
		case <-a.ctx.Done():
			return
		}
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
	a.loadBansFromDB()
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
	go a.banMonitor()
	go a.cleanupBans()
	go a.tradeTicker()
	go a.payoutMonitor()
	go a.dbPoller()
	go a.inventoryRefreshLoop()
}

// tradeTicker emits a compact trade state every second so the frontend can
// render a smooth countdown without polling.
func (a *App) tradeTicker() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if a.ctx == nil {
				continue
			}
			// Emit current trade info
			info := a.GetTradeInfo()
			go runtime.EventsEmit(a.ctx, "tradeUpdate", info)
		case <-a.ctx.Done():
			return
		}
	}
}

// GetTradeInfo returns a snapshot of the current trade state for the UI.
func (a *App) GetTradeInfo() TradeState {
	a.tradeMu.Lock()
	defer a.tradeMu.Unlock()
	ts := TradeState{Active: a.tradeActive, Partner: a.lastTradePartner, PartnerID: a.lastTradePartnerID}
	if a.tradeStartedAt.IsZero() || !a.tradeActive {
		ts.ElapsedSeconds = 0
		ts.RemainingSeconds = int64(maxOpenTradeDuration.Seconds())
	} else {
		elapsed := int64(time.Since(a.tradeStartedAt).Seconds())
		maxSec := int64(maxOpenTradeDuration.Seconds())
		rem := maxSec - elapsed
		if rem < 0 {
			rem = 0
		}
		ts.ElapsedSeconds = elapsed
		ts.RemainingSeconds = rem
	}
	ts.MaxOpenSeconds = int64(maxOpenTradeDuration.Seconds())
	ts.BanDurationSeconds = int64(banDuration.Seconds())
	return ts
}

// GetBanList returns a UI-friendly list of active bans.
func (a *App) GetBanList() []BanEntry {
	if a.banList == nil {
		return []BanEntry{}
	}
	now := time.Now()
	snapshot := a.banList.List()
	out := make([]BanEntry, 0, len(snapshot))
	for k, info := range snapshot {
		if now.After(info.ExpiresAt) {
			continue
		}
		rem := int64(info.ExpiresAt.Sub(now).Seconds())
		if rem < 0 {
			rem = 0
		}
		label := k
		if strings.HasPrefix(k, "name:") {
			label = "Name: " + strings.TrimPrefix(k, "name:")
		} else if strings.HasPrefix(k, "tradeid:") {
			label = "TradeID: " + strings.TrimPrefix(k, "tradeid:")
		}

		expiresAtStr := info.ExpiresAt.Format(time.RFC3339)
		if info.ExpiresAt.Year() > 3000 {
			expiresAtStr = "Lifetime"
			rem = 3153600000 // approx 100 years
		}

		out = append(out, BanEntry{
			Key:              k,
			Label:            label,
			ExpiresAt:        expiresAtStr,
			RemainingSeconds: rem,
			Message:          info.Message,
			IsActive:         info.IsActive,
		})
	}
	return out
}

// ClearBan removes a ban by key (exact key as returned by GetBanList).
func (a *App) ClearBan(key string) error {
	if a.banList == nil {
		return nil
	}
	a.RemoveBan(key)
	a.AddLog(fmt.Sprintf("Ban cleared: %s", key))
	if a.ctx != nil {
		go runtime.EventsEmit(a.ctx, "banListUpdate", a.GetBanList())
	}
	return nil
}

func (a *App) ReloadBans() error {
	if a.banList != nil {
		a.banList.Clear()
	}
	a.loadBansFromDB()
	if a.ctx != nil {
		go runtime.EventsEmit(a.ctx, "banListUpdate", a.GetBanList())
	}
	return nil
}

// BanPlayer manually adds a name to the ban list with a specified duration and message.
func (a *App) BanPlayer(name string, duration string, message string) error {
	if a.banList == nil {
		return fmt.Errorf("ban list not initialized")
	}

	var d time.Duration
	switch duration {
	case "1h":
		d = 1 * time.Hour
	case "5h":
		d = 5 * time.Hour
	case "24h":
		d = 24 * time.Hour
	case "1w":
		d = 7 * 24 * time.Hour
	case "1m":
		d = 30 * 24 * time.Hour
	case "lifetime":
		d = 0 // Handled as lifetime in BanList.Add
	default:
		return fmt.Errorf("invalid duration: %s", duration)
	}

	if message == "" {
		message = fmt.Sprintf("%s you are banned from placing a bet", name)
	}

	key := "name:" + strings.ToLower(normalizeName(name))
	a.AddBan(key, d, message)

	a.AddLog(fmt.Sprintf("Player banned: %s for %s. Message: %s", name, duration, message))

	if a.ctx != nil {
		go runtime.EventsEmit(a.ctx, "banListUpdate", a.GetBanList())
	}
	return nil
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

func autoPayoutSchemaStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS public.auto_payouts (
			id TEXT PRIMARY KEY,
			player_name TEXT NOT NULL,
			item_name TEXT NOT NULL,
			quantity INTEGER NOT NULL,
			status TEXT NOT NULL,
			created_at TEXT NOT NULL,
			player_trade_id INTEGER NULL,
			banker_trade_id INTEGER NULL,
			notified BOOLEAN DEFAULT FALSE
		);`,
		`ALTER TABLE public.auto_payouts ADD COLUMN IF NOT EXISTS player_name TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE public.auto_payouts ADD COLUMN IF NOT EXISTS item_name TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE public.auto_payouts ADD COLUMN IF NOT EXISTS quantity INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE public.auto_payouts ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'pending';`,
		`ALTER TABLE public.auto_payouts ADD COLUMN IF NOT EXISTS created_at TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE public.auto_payouts ADD COLUMN IF NOT EXISTS player_trade_id INTEGER NULL;`,
		`ALTER TABLE public.auto_payouts ADD COLUMN IF NOT EXISTS banker_trade_id INTEGER NULL;`,
		`ALTER TABLE public.auto_payouts ADD COLUMN IF NOT EXISTS notified BOOLEAN DEFAULT FALSE;`,
	}
}

func bannedPlayersSchemaStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS public.banned_players (
			ban_key TEXT PRIMARY KEY,
			expires_at TIMESTAMP NOT NULL,
			message TEXT NOT NULL,
			is_active BOOLEAN DEFAULT TRUE,
			created_at TIMESTAMP DEFAULT NOW()
		);`,
		`ALTER TABLE public.banned_players ADD COLUMN IF NOT EXISTS ban_key TEXT;`,
		`ALTER TABLE public.banned_players ADD COLUMN IF NOT EXISTS expires_at TIMESTAMP;`,
		`ALTER TABLE public.banned_players ADD COLUMN IF NOT EXISTS message TEXT;`,
		`ALTER TABLE public.banned_players ADD COLUMN IF NOT EXISTS is_active BOOLEAN DEFAULT TRUE;`,
		`ALTER TABLE public.banned_players ADD COLUMN IF NOT EXISTS created_at TIMESTAMP DEFAULT NOW();`,
		`ALTER TABLE public.banned_players ADD COLUMN IF NOT EXISTS username TEXT;`,
		`UPDATE public.banned_players SET ban_key = username WHERE ban_key IS NULL AND username IS NOT NULL AND TRIM(username) <> '';`,
		`DELETE FROM public.banned_players a USING public.banned_players b WHERE a.ctid < b.ctid AND a.ban_key = b.ban_key AND a.ban_key IS NOT NULL;`,
		`DROP INDEX IF EXISTS idx_banned_players_ban_key;`,
		`CREATE UNIQUE INDEX idx_banned_players_ban_key ON public.banned_players (ban_key) WHERE ban_key IS NOT NULL;`,
		`UPDATE public.banned_players SET is_active = TRUE WHERE is_active IS NULL;`,
	}
}

func autoPayoutSettingsSchemaStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS public.auto_payout_settings (
			setting_key TEXT PRIMARY KEY,
			setting_value TEXT NOT NULL
		);`,
		`ALTER TABLE public.auto_payout_settings ADD COLUMN IF NOT EXISTS setting_key TEXT;`,
		`ALTER TABLE public.auto_payout_settings ADD COLUMN IF NOT EXISTS setting_value TEXT;`,
		`DELETE FROM public.auto_payout_settings a USING public.auto_payout_settings b WHERE a.ctid < b.ctid AND a.setting_key = b.setting_key AND a.setting_key IS NOT NULL;`,
		`DROP INDEX IF EXISTS idx_auto_payout_settings_key;`,
		`CREATE UNIQUE INDEX idx_auto_payout_settings_key ON public.auto_payout_settings (setting_key) WHERE setting_key IS NOT NULL;`,
	}
}

func (a *App) ensureAutoPayoutSettingsUniqueIndex() error {
	if a.db == nil {
		return nil
	}
	ctx := context.Background()
	_, _ = a.db.Exec(ctx, "ALTER TABLE public.auto_payout_settings ADD COLUMN IF NOT EXISTS setting_key TEXT;")
	_, _ = a.db.Exec(ctx, "ALTER TABLE public.auto_payout_settings ADD COLUMN IF NOT EXISTS setting_value TEXT;")
	_, _ = a.db.Exec(ctx, `DELETE FROM public.auto_payout_settings a USING public.auto_payout_settings b WHERE a.ctid < b.ctid AND a.setting_key = b.setting_key AND a.setting_key IS NOT NULL;`)
	_, _ = a.db.Exec(ctx, `DROP INDEX IF EXISTS idx_auto_payout_settings_key;`)
	_, err := a.db.Exec(ctx, `CREATE UNIQUE INDEX idx_auto_payout_settings_key ON public.auto_payout_settings (setting_key) WHERE setting_key IS NOT NULL;`)
	return err
}

func bankerTradeSchemaStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS public.banker_trades (
			id BIGSERIAL PRIMARY KEY,
			player_name TEXT NOT NULL,
			bet_items JSONB NOT NULL DEFAULT '[]'::jsonb,
			banker_name TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			player_trade_id INTEGER NULL,
			player_chat_id INTEGER NULL,
			owner_key TEXT NOT NULL DEFAULT '',
			bet_amount INTEGER DEFAULT 0,
			risk_bank INTEGER DEFAULT 0,
			risk_status TEXT DEFAULT 'idle'
		);`,
		`ALTER TABLE public.banker_trades ADD COLUMN IF NOT EXISTS player_name TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE public.banker_trades ADD COLUMN IF NOT EXISTS bet_items JSONB NOT NULL DEFAULT '[]'::jsonb;`,
		`ALTER TABLE public.banker_trades ADD COLUMN IF NOT EXISTS banker_name TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE public.banker_trades ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'pending';`,
		`ALTER TABLE public.banker_trades ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();`,
		`ALTER TABLE public.banker_trades ADD COLUMN IF NOT EXISTS player_trade_id INTEGER NULL;`,
		`ALTER TABLE public.banker_trades ADD COLUMN IF NOT EXISTS player_chat_id INTEGER NULL;`,
		`ALTER TABLE public.banker_trades ADD COLUMN IF NOT EXISTS owner_key TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE public.banker_trades ADD COLUMN IF NOT EXISTS bet_amount INTEGER DEFAULT 0;`,
		`ALTER TABLE public.banker_trades ADD COLUMN IF NOT EXISTS risk_bank INTEGER DEFAULT 0;`,
		`ALTER TABLE public.banker_trades ADD COLUMN IF NOT EXISTS risk_status TEXT DEFAULT 'idle';`,
	}
}

func bankerInventorySchemaStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS public.banker_inventory (
			banker_name TEXT NOT NULL DEFAULT '',
			item_name TEXT NOT NULL DEFAULT '',
			quantity INTEGER NOT NULL DEFAULT 0,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (banker_name, item_name)
		);`,
		`ALTER TABLE public.banker_inventory ADD COLUMN IF NOT EXISTS banker_name TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE public.banker_inventory ADD COLUMN IF NOT EXISTS item_name TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE public.banker_inventory ADD COLUMN IF NOT EXISTS quantity INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE public.banker_inventory ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();`,
	}
}

func dealerShoutSchemaStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS public.dealer_shouts (
			id SERIAL PRIMARY KEY,
			owner_key TEXT NOT NULL DEFAULT '',
			target_player TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			shout_type TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			completed_at TIMESTAMPTZ NULL
		);`,
		`ALTER TABLE public.dealer_shouts ADD COLUMN IF NOT EXISTS owner_key TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE public.dealer_shouts ADD COLUMN IF NOT EXISTS target_player TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE public.dealer_shouts ADD COLUMN IF NOT EXISTS message TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE public.dealer_shouts ADD COLUMN IF NOT EXISTS shout_type TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE public.dealer_shouts ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'pending';`,
		`ALTER TABLE public.dealer_shouts ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();`,
		`ALTER TABLE public.dealer_shouts ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ NULL;`,
		`CREATE INDEX IF NOT EXISTS idx_dealer_shouts_status_owner ON public.dealer_shouts(status, owner_key);`,
	}
}

func newPGXPoolConfig(connString string) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, err
	}
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = make(map[string]string)
	}
	cfg.ConnConfig.RuntimeParams["application_name"] = "auto-payout"
	cfg.ConnConfig.RuntimeParams["default_query_exec_mode"] = "simple_protocol"
	return cfg, nil
}

func (a *App) initDatabase() {
	a.AddLog("Connecting to database...")

	cfg, err := newPGXPoolConfig(a.dbConnString)
	if err != nil {
		a.AddLog("ERROR: Database connection config failed: " + err.Error())
		return
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		a.AddLog("ERROR: Database connection failed: " + err.Error())
		return
	}

	a.db = pool

	// Set search path and create tables with public qualification
	_, _ = a.db.Exec(context.Background(), "SET search_path TO public;")

	for _, stmt := range autoPayoutSchemaStatements() {
		if _, err := a.db.Exec(context.Background(), stmt); err != nil {
			a.AddLog("ERROR: auto_payouts schema migration failed: " + err.Error())
		}
	}

	for _, stmt := range dealerShoutSchemaStatements() {
		if _, err := a.db.Exec(context.Background(), stmt); err != nil {
			a.AddLog("ERROR: dealer_shouts schema migration failed: " + err.Error())
		}
	}

	// Ensure notified column exists for failure webhooks
	_, _ = a.db.Exec(context.Background(), "ALTER TABLE public.auto_payouts ADD COLUMN IF NOT EXISTS notified BOOLEAN DEFAULT FALSE;")

	for _, stmt := range bankerInventorySchemaStatements() {
		if _, err := a.db.Exec(context.Background(), stmt); err != nil {
			a.AddLog("ERROR: banker_inventory schema migration failed: " + err.Error())
		}
	}

	for _, stmt := range bankerTradeSchemaStatements() {
		if _, err := a.db.Exec(context.Background(), stmt); err != nil {
			a.AddLog("ERROR: banker_trades schema migration failed: " + err.Error())
		}
	}

	for _, stmt := range bannedPlayersSchemaStatements() {
		if _, err := a.db.Exec(context.Background(), stmt); err != nil {
			a.AddLog("ERROR: banned_players schema migration failed: " + err.Error())
		}
	}

	// Create the canonical public.stocked_items table layout used by the app.
	query := `CREATE TABLE IF NOT EXISTS public.stocked_items (
		id SERIAL PRIMARY KEY,
		owner_key TEXT NOT NULL DEFAULT '',
		raw_name TEXT NOT NULL DEFAULT '',
		canonical_name TEXT NOT NULL DEFAULT '',
		display_name TEXT NOT NULL DEFAULT '',
		is_active BOOLEAN NOT NULL DEFAULT TRUE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);`
	_, err = a.db.Exec(context.Background(), query)
	if err != nil {
		a.AddLog("ERROR: public.stocked_items table creation failed: " + err.Error())
	}
	_, _ = a.db.Exec(context.Background(), "ALTER TABLE public.stocked_items ADD COLUMN IF NOT EXISTS owner_key TEXT NOT NULL DEFAULT '';")
	_, _ = a.db.Exec(context.Background(), "ALTER TABLE public.stocked_items ADD COLUMN IF NOT EXISTS raw_name TEXT NOT NULL DEFAULT '';")
	_, _ = a.db.Exec(context.Background(), "ALTER TABLE public.stocked_items ADD COLUMN IF NOT EXISTS canonical_name TEXT NOT NULL DEFAULT '';")
	_, _ = a.db.Exec(context.Background(), "ALTER TABLE public.stocked_items ADD COLUMN IF NOT EXISTS display_name TEXT NOT NULL DEFAULT '';")
	_, _ = a.db.Exec(context.Background(), "ALTER TABLE public.stocked_items ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE;")
	_, _ = a.db.Exec(context.Background(), "ALTER TABLE public.stocked_items ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();")
	_, _ = a.db.Exec(context.Background(), `DELETE FROM public.stocked_items a USING public.stocked_items b WHERE a.ctid < b.ctid AND a.owner_key = b.owner_key AND a.raw_name = b.raw_name AND a.raw_name <> '';`)
	_, _ = a.db.Exec(context.Background(), `CREATE UNIQUE INDEX IF NOT EXISTS idx_stocked_items_owner_raw ON public.stocked_items(owner_key, raw_name) WHERE raw_name <> '';`)

	for _, stmt := range autoPayoutSettingsSchemaStatements() {
		if _, err := a.db.Exec(context.Background(), stmt); err != nil {
			a.AddLog("ERROR: auto_payout_settings schema migration failed: " + err.Error())
		}
	}
	if err := a.ensureAutoPayoutSettingsUniqueIndex(); err != nil {
		a.AddLog("ERROR: auto_payout_settings index repair failed: " + err.Error())
	}

	a.AddLog("Database connected and ready.")
}

func (a *App) ShowWindow() {
	if a.ctx != nil {
		runtime.WindowShow(a.ctx)
	}
}

func (a *App) initParser() {
	if p, err := exec.LookPath("py"); err == nil {
		a.pythonExec = p
		a.pythonArgs = []string{"-3"}
	} else if p, err := exec.LookPath("python3"); err == nil {
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
	// If not found on disk, try to use an embedded copy (packaged builds).
	if len(users28Parser) > 0 {
		ex, err := os.Executable()
		dest := ""
		if err == nil {
			dest = filepath.Join(filepath.Dir(ex), "parse_users28.py")
		} else {
			dest = filepath.Join(os.TempDir(), "parse_users28.py")
		}
		// Write embedded parser to dest (overwrite if necessary)
		if err := os.WriteFile(dest, users28Parser, 0644); err != nil {
			a.AddLog("ERROR: Failed to write embedded parser: " + err.Error())
		} else {
			a.parserScript = dest
			a.AddLog("Using embedded parser at: " + dest)
			return
		}
	}
	a.AddLog("ERROR: parse_users28.py NOT FOUND. Detection will not work.")
}

func (a *App) getOwnerKey() string {
	if v := strings.TrimSpace(os.Getenv("TRADE_TRACKER_OWNER_KEY")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("ROLL_ORIGINS_OWNER_KEY")); v != "" {
		return v
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "local"
}

func (a *App) recordTradeLedger(partnerName string, tradeType string, items []TradeItem) {
	if a.db == nil {
		return
	}

	totalQty := 0
	for _, it := range items {
		if it.Quantity > 0 {
			totalQty += it.Quantity
		}
	}
	if totalQty <= 0 && len(items) > 0 {
		totalQty = len(items)
	}
	if totalQty == 0 {
		return
	}

	type LedgerItem struct {
		Name     string `json:"name"`
		Quantity int    `json:"quantity"`
		RawName  string `json:"raw_name,omitempty"`
		Qty      int    `json:"qty,omitempty"`
	}

	ledgerItems := make([]LedgerItem, 0, len(items))
	for _, it := range items {
		ledgerItems = append(ledgerItems, LedgerItem{
			Name:     it.Name,
			Quantity: it.Quantity,
			RawName:  it.Name,
			Qty:      it.Quantity,
		})
	}

	itemsJSON, _ := json.Marshal(ledgerItems)
	owner := a.getOwnerKey()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if _, err := a.db.Exec(ctx, `INSERT INTO public.trade_ledger (owner_key, partner_name, trade_type, total_quantity, items) VALUES ($1, $2, $3, $4, $5)`, owner, partnerName, tradeType, totalQty, itemsJSON); err != nil {
			a.AddLog(fmt.Sprintf("ERROR: failed to write trade_ledger: %v", err))
		} else {
			a.AddLog(fmt.Sprintf("LEDGER: recorded %s for %s (%d)", tradeType, partnerName, totalQty))
		}
	}()
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
	minQty := a.getIntSetting("min_qty_per_unique", 1)

	if minQty > 0 && qty < minQty {
		a.AddLog(fmt.Sprintf("Ignoring AddPayout: quantity %d is below minimum limit %d", qty, minQty))
		return
	}

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
	MinQty      int    `json:"minQty"`
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
	minQty := a.getIntSetting("min_qty_per_unique", 1)
	res.MaxQty = maxQty
	res.MaxUnique = maxUnique
	res.MinQty = minQty
	res.Player = player
	res.Item = normItem

	if minQty > 0 && qty < minQty {
		res.Allowed = false
		res.Reason = fmt.Sprintf("Quantity must be at least %d", minQty)
		return res, nil
	}

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
		if a.skipStripScan {
			a.stripScanMu.Unlock()
			a.AddLog("Skipping hand inventory scan (banker/split mode).")
			return
		}
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

// SetSkipStripScan enables/disables skipping player hand scans (banker/split mode).
func (a *App) SetSkipStripScan(enabled bool) {
	a.stripScanMu.Lock()
	a.skipStripScan = enabled
	a.stripScanMu.Unlock()
	a.AddLog(fmt.Sprintf("[CONFIG] SkipStripScan = %t", enabled))
	a.emitUpdate()
}

// GetSkipStripScan returns whether strip scans are skipped.
func (a *App) GetSkipStripScan() bool {
	a.stripScanMu.Lock()
	v := a.skipStripScan
	a.stripScanMu.Unlock()
	return v
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
	owner := a.getOwnerKey()
	rows, err := a.db.Query(context.Background(), "SELECT id, raw_name, canonical_name, display_name, is_active FROM public.stocked_items WHERE ($1 = '' OR owner_key = $1) ORDER BY display_name ASC", owner)
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
	owner := a.getOwnerKey()
	_, err := a.db.Exec(context.Background(), `
		INSERT INTO public.stocked_items (owner_key, raw_name, canonical_name, display_name, is_active)
		VALUES ($1, $2, $3, $4, TRUE)
		ON CONFLICT (owner_key, raw_name) DO UPDATE SET
			canonical_name = EXCLUDED.canonical_name,
			display_name = EXCLUDED.display_name,
			is_active = TRUE
	`, owner, rawName, canonical, displayName)
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

func (a *App) GetActiveStockedItems() []StockedItem {
	if a.db == nil {
		return []StockedItem{}
	}
	rows, err := a.db.Query(context.Background(), "SELECT id, raw_name, canonical_name, display_name, is_active FROM public.stocked_items WHERE is_active = TRUE ORDER BY display_name ASC")
	if err != nil {
		a.AddLog("ERROR: Failed to query active stocked_items: " + err.Error())
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

// PayoutSettings holds persisted UI settings for auto-payout
type PayoutSettings struct {
	MaxUniqueItems  int `json:"maxUniqueItems"`
	MaxQtyPerUnique int `json:"maxQtyPerUnique"`
	MinQtyPerUnique int `json:"minQtyPerUnique"`
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
	s := PayoutSettings{MaxUniqueItems: 6, MaxQtyPerUnique: 10, MinQtyPerUnique: 1}
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
	if err := a.db.QueryRow(ctx, "SELECT setting_value FROM public.auto_payout_settings WHERE setting_key = $1", "min_qty_per_unique").Scan(&v); err == nil {
		if iv, err := strconv.Atoi(v); err == nil {
			s.MinQtyPerUnique = iv
		}
	}
	return s
}

// SaveSettings persists provided settings to the DB.
func (a *App) SaveSettings(maxUnique int, maxQty int, minQty int) error {
	if a.db == nil {
		return nil
	}
	if err := a.ensureAutoPayoutSettingsUniqueIndex(); err != nil {
		a.AddLog("ERROR: Settings table repair failed before save: " + err.Error())
		return err
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
	if _, err := a.db.Exec(ctx, `INSERT INTO public.auto_payout_settings (setting_key, setting_value) VALUES ($1, $2) ON CONFLICT (setting_key) DO UPDATE SET setting_value = EXCLUDED.setting_value`, "min_qty_per_unique", fmt.Sprintf("%d", minQty)); err != nil {
		a.AddLog("ERROR: Failed to save min_qty_per_unique: " + err.Error())
		return err
	}
	a.AddLog(fmt.Sprintf("Settings saved: max_unique=%d, max_qty=%d, min_qty=%d", maxUnique, maxQty, minQty))
	return nil
}

// --- Internal Logic ---

func (a *App) loadPayoutsFromDB() {
	if a.db == nil {
		a.AddLog("ERROR: Database not connected. Cannot load payouts.")
		return
	}

	a.AddLog("Querying public.auto_payouts records...")
	dbPayouts := []Payout{}
	count := 0

	// Manual entries from public.auto_payouts table. Filter out Completed/Disabled status (case-insensitive).
	rows, err := a.db.Query(context.Background(), "SELECT id, player_name, item_name, quantity, status, created_at, COALESCE(player_trade_id,0), COALESCE(banker_trade_id,0), COALESCE(notified,false) FROM public.auto_payouts WHERE LOWER(status) NOT IN ('completed', 'disabled') ORDER BY created_at DESC")
	if err == nil {
		for rows.Next() {
			var p Payout
			if err := rows.Scan(&p.ID, &p.Name, &p.ItemName, &p.Quantity, &p.Status, &p.CreatedAt, &p.TradeID, &p.BankerTradeID, &p.Notified); err == nil {
				p.Name = normalizeName(p.Name)
				dbPayouts = append(dbPayouts, p)
				count++
			}
		}
		rows.Close()
	} else {
		a.AddLog("ERROR: DB Query failed: " + err.Error())
	}

	a.pMu.Lock()
	// Create a map of DB results for quick lookup during merge
	dbMap := make(map[string]Payout)
	for _, p := range dbPayouts {
		dbMap[p.ID] = p
	}

	merged := make([]Payout, 0)
	// Process existing in-memory payouts
	for _, oldP := range a.payouts {
		if dbP, ok := dbMap[oldP.ID]; ok {
			// Found in DB. Preserve 'Trading' or terminal statuses if they haven't synced yet.
			if oldP.Status == "Trading" || oldP.Status == "Completed" || oldP.Status == "Disabled" {
				merged = append(merged, oldP)
			} else {
				merged = append(merged, dbP)
			}
			delete(dbMap, oldP.ID)
		} else {
			// Not in DB results (meaning it's likely Completed or Disabled in the DB now).
			// Keep it in memory if it's already marked terminal or Trading.
			if oldP.Status == "Trading" || oldP.Status == "Completed" || oldP.Status == "Disabled" {
				merged = append(merged, oldP)
			}
		}
	}
	// Add any remaining new payouts from DB
	for _, newP := range dbMap {
		merged = append(merged, newP)
	}

	a.payouts = merged
	a.pMu.Unlock()

	// Debug: log each loaded payout for easier tracing
	for _, p := range merged {
		a.AddLog(fmt.Sprintf("[DB_SYNC] id=%s name=%s item=%s qty=%d status=%s tradeid=%d bankerid=%d",
			p.ID, p.Name, p.ItemName, p.Quantity, p.Status, p.TradeID, p.BankerTradeID))
	}

	a.AddLog(fmt.Sprintf("Sync complete. Current in-memory queue: %d records.", len(merged)))
	a.emitUpdate()

	// If we have any pending payouts, request a room users refresh so parse28
	// can populate the current room map immediately.
	for _, p := range merged {
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

// waitForRoomUserByTradeOrChat attempts to resolve a room user by tradeID or chatID
// within the provided timeout by polling the in-memory `roomUsers` map.
func (a *App) waitForRoomUserByTradeOrChat(id int, timeout time.Duration) (ParsedUsers28User, bool) {
	if id <= 0 {
		return ParsedUsers28User{}, false
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		a.roomUsersMu.RLock()
		for _, u := range a.roomUsers {
			if u.TradeID == id || u.ChatID == id {
				a.roomUsersMu.RUnlock()
				return u, true
			}
		}
		a.roomUsersMu.RUnlock()
		time.Sleep(75 * time.Millisecond)
	}
	return ParsedUsers28User{}, false
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

	// Use a conditional update to ensure we don't overwrite a terminal status (Completed/Disabled)
	// with a non-terminal one (e.g., Trading, Pending) due to a race condition.
	query := "UPDATE public.auto_payouts SET status = $1 WHERE id = $2"
	isTerminal := strings.EqualFold(status, "Completed") || strings.EqualFold(status, "Disabled")
	if !isTerminal {
		query += " AND LOWER(status) NOT IN ('completed', 'disabled')"
	}

	if _, err := a.db.Exec(ctx, query, status, id); err != nil {
		a.AddLog("ERROR: Failed to persist payout status: " + err.Error())
	} else {
		// Log the status change for debugging
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

		// Attempt to resolve any pending active trade target to avoid a race where
		// a TRADE_OPEN arrives before the USERS/SPACENODEUSERS packet is parsed.
		a.tradeMu.Lock()
		activeTarget := a.activeTradeTarget
		needResolve := a.lastTradePartner == ""
		a.tradeMu.Unlock()

		if activeTarget != 0 {
			a.roomUsersMu.RLock()
			var matchedUser *ParsedUsers28User
			for _, u := range a.roomUsers {
				if u.TradeID == activeTarget || u.ChatID == activeTarget {
					matchedUser = &u
					break
				}
			}
			a.roomUsersMu.RUnlock()

			if matchedUser != nil {
				if needResolve {
					a.tradeMu.Lock()
					a.lastTradePartner = matchedUser.Username
					a.lastTradePartnerID = matchedUser.TradeID
					a.lastTradePartnerChatID = matchedUser.ChatID
					if a.activeTradePartner == "" {
						a.activeTradePartner = matchedUser.Username
					}
					a.tradeMu.Unlock()
					a.AddLog(fmt.Sprintf("[ROOM] Resolved active trade target %d -> %s (chat=%d trade=%d)", activeTarget, matchedUser.Username, matchedUser.ChatID, matchedUser.TradeID))
				}

				// SECURITY: If this resolved user is banned, close the trade immediately.
				if info, banned := a.getBanInfo(matchedUser.Username, matchedUser.TradeID); banned {
					a.AddLog(fmt.Sprintf("[BAN] Closing active trade with newly-identified banned partner %s (id=%d)", matchedUser.Username, matchedUser.TradeID))
					a.queueShout(matchedUser.Username, fmt.Sprintf("%s - Remaining: %s", info.Message, formatRemainingTime(info.ExpiresAt)))
					if a.ext != nil {
						a.ext.Send(g.Out.Id("TRADE_CLOSE_OUT"))
					}
					// Reset local trade state
					a.tradeMu.Lock()
					a.tradeActive = false
					a.payoutTradeSent = false
					a.activeTradePartner = ""
					a.activeTradeTarget = 0
					a.lastTradePartner = ""
					a.lastTradePartnerID = 0
					a.tradeStartedAt = time.Time{}
					a.tradeMu.Unlock()
				}
			}
		}

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
			a.stripScanMu.Lock()
			skip := a.skipStripScan
			a.stripScanMu.Unlock()
			if skip {
				a.AddLog("Strip scan continuing skipped (banker/split mode).")
				return
			}
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
	id, ok := decodeLeadingVL64(e.Packet.Data)
	if !ok {
		a.AddLog("ERROR: Could not decode ID from TRADE_OPEN_IN")
		return
	}

	a.tradeMu.Lock()
	a.tradeActive = true
	a.tradeAccepted = false
	a.currentTradeItems = ""
	a.lastTradeItems = nil
	a.lastTradePartner = ""
	a.lastTradePartnerID = id
	a.lastTradePartnerChatID = 0
	a.activeTradeTarget = id
	a.tradeStartedAt = time.Now()

	// Try to resolve name from room map
	a.roomUsersMu.RLock()
	found := false
	for _, u := range a.roomUsers {
		if u.TradeID == id || u.ChatID == id {
			a.lastTradePartner = u.Username
			a.lastTradePartnerChatID = u.ChatID
			a.lastTradePartnerID = u.TradeID // ensure we use the canonical TradeID
			a.activeTradeTarget = u.TradeID
			found = true
			break
		}
	}
	a.roomUsersMu.RUnlock()

	partnerName := a.lastTradePartner
	partnerID := a.lastTradePartnerID
	a.tradeMu.Unlock()

	if found {
		a.AddLog(fmt.Sprintf("Trade window opened with %s (ID:%d).", partnerName, partnerID))
	} else {
		a.AddLog(fmt.Sprintf("Trade window opened with ID %d (name not yet resolved).", id))
		go a.requestRoomUsers()
	}

	// Check for active banker_trades blockade
	if a.hasActiveBankerTrades() {
		a.tradeMu.Lock()
		allow := a.payoutTradeSent
		a.tradeMu.Unlock()
		if !allow {
			a.AddLog(fmt.Sprintf("[BLOCK] Blocking incoming trade from %s (id=%d): active banker_trades present", partnerName, id))
			if partnerName != "" {
				a.queueShout(partnerName, fmt.Sprintf("%s, hold on! A game is in progress. Trades are paused until it finishes.", partnerName))
			}
			e.Block()
			if a.ext != nil {
				a.ext.Send(g.Out.Id("TRADE_CLOSE_OUT"))
			}
			return
		}
	}

	// Immediate ban check
	if info, banned := a.getBanInfo(partnerName, partnerID); banned {
		a.AddLog(fmt.Sprintf("[BAN] Blocking incoming trade from banned partner %s (id=%d)", partnerName, partnerID))
		if partnerName != "" {
			a.queueShout(partnerName, fmt.Sprintf("%s - Remaining: %s", info.Message, formatRemainingTime(info.ExpiresAt)))
		}
		e.Block()
		if a.ext != nil {
			a.ext.Send(g.Out.Id("TRADE_CLOSE_OUT"))
		}
		return
	}

	// Fetch active stocked items immediately on trade open (synchronously to avoid race with parseTradeItems)
	activeItems := a.GetActiveStockedItems()
	var names []string
	var displayNames []string
	for _, it := range activeItems {
		names = append(names, it.RawName)
		displayNames = append(displayNames, it.DisplayName)
	}

	a.tradeMu.Lock()
	a.allowedNamesCache = names
	a.allowedDisplayNamesCache = displayNames
	a.tradeMu.Unlock()
	a.AddLog(fmt.Sprintf("[DEBUG] Stocked items cached for trade: %v (display: %v)", names, displayNames))

	a.AddLog("Trade window opened.")
}

func (a *App) handleTradeItems(e *g.Intercept) {
	if isUsersPacket(e.Packet.Data) {
		return
	}

	a.tradeMu.Lock()
	a.currentTradeItems = string(e.Packet.Data)
	allowedCache := a.allowedNamesCache
	partner := a.lastTradePartner
	a.tradeMu.Unlock()

	a.bankerNameMu.RLock()
	banker := a.bankerName
	a.bankerNameMu.RUnlock()

	a.roomUsersMu.RLock()
	roomUsers := make(map[string]bool)
	for name := range a.roomUsers {
		roomUsers[name] = true
	}
	a.roomUsersMu.RUnlock()

	items := a.parseTradeItems(e.Packet.Data, allowedCache, partner, banker, roomUsers)

	a.tradeMu.Lock()
	a.lastTradeItems = items
	a.tradeMu.Unlock()

	a.AddLog(fmt.Sprintf("[DEBUG] TRADE_ITEMS updated, found %d items", len(items)))
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

func (a *App) parseTradeItems(data []byte, allowedNamesCache []string, partner string, banker string, roomUsers map[string]bool) []TradeItem {
	counts := map[string]int{}
	unrecognized := map[string]int{}
	fields := bytes.Split(data, []byte{0x02})

	type allowedItem struct {
		lower    string
		baseName string
	}
	var activeItems []allowedItem
	for _, n := range allowedNamesCache {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		low := strings.ToLower(n)
		base := low
		if idx := strings.LastIndex(base, "*"); idx != -1 {
			base = base[:idx]
		}
		activeItems = append(activeItems, allowedItem{
			lower:    low,
			baseName: base,
		})
	}

	used := make(map[int]bool)
	for i := 0; i < len(fields); i++ {
		field := string(fields[i])

		isFloor := false
		isWall := false
		nameIdx := -1

		// Floor Detection: 'II' marker at index i, name is at i-2
		if strings.HasPrefix(field, "II") && i >= 2 {
			isFloor = true
			nameIdx = i - 2
		}

		// Wall Detection: 'wall_' marker at index i, name is at i-1
		if !isFloor && strings.HasPrefix(field, "wall_") && i >= 1 {
			isWall = true
			nameIdx = i - 1
		}

		if (isFloor || isWall) && nameIdx >= 0 && !used[nameIdx] {
			lowNameField := strings.ToLower(string(fields[nameIdx]))
			used[nameIdx] = true

			matched := false
			matchDetail := ""
			// 1. Check if it contains any of our authorized items (substring match)
			for j := range activeItems {
				it := &activeItems[j]
				if strings.Contains(lowNameField, it.lower) || strings.Contains(lowNameField, it.baseName) {
					counts[it.baseName]++
					matched = true
					matchDetail = fmt.Sprintf("Matched %s", it.baseName)
					break
				}
			}

			if !matched {
				// 2. If no authorized match, only treat as 'unrecognized' if it looks like a real item.
				// This prevents metadata like '0,0,0' or numbers from triggering rejections.
				if itemRegex.MatchString(lowNameField) {
					unrecognized[lowNameField]++
					matchDetail = "UNRECOGNIZED FURNITURE"
				} else {
					matchDetail = "METADATA/NOISE (Ignored)"
				}
			}

			a.AddLog(fmt.Sprintf("[DEBUG-TRADE] Found %s candidate at field[%d]: '%s' | %s",
				func() string {
					if isFloor {
						return "Floor"
					}
					return "Wall"
				}(),
				nameIdx, lowNameField, matchDetail))
		}
	}

	var items []TradeItem
	for name, qty := range counts {
		items = append(items, TradeItem{Name: name, Quantity: qty})
	}
	for name, qty := range unrecognized {
		items = append(items, TradeItem{Name: name, Quantity: qty, IsUnrecognized: true})
	}
	return items
}

func (a *App) recordBankerTrade(playerName string, items []TradeItem, tradeID int, chatID int) {
	totalQty := 0
	for _, it := range items {
		totalQty += it.Quantity
	}

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

	owner := a.getOwnerKey()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_, err := a.db.Exec(ctx, `
			INSERT INTO public.banker_trades (player_name, bet_items, banker_name, status, created_at, player_trade_id, player_chat_id, owner_key, risk_bank, bet_amount)
			VALUES ($1, $2, $3, $4, NOW(), $5, $6, $7, 0, $8)
		`, playerName, itemsJSON, banker, "pending", tradeID, chatID, owner, totalQty)
		if err != nil {
			a.AddLog(fmt.Sprintf("ERROR: [BANKER][DB] failed to record trade: %v", err))
		} else {
			a.AddLog(fmt.Sprintf("SUCCESS: [BANKER] recorded trade from %s with %d item(s)", playerName, len(items)))
			// Also record to global trade_ledger as an IN (banker received items)
			a.recordTradeLedger(playerName, "IN", items)
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

		// If this partner is currently banned, block the acceptance immediately
		if info, banned := a.getBanInfo(partnerName, partnerTradeID); banned {
			a.AddLog(fmt.Sprintf("[BAN] Blocking acceptance from banned partner %s (id=%d)", partnerName, partnerTradeID))
			if partnerName != "" {
				a.queueShout(partnerName, fmt.Sprintf("%s - Remaining: %s", info.Message, formatRemainingTime(info.ExpiresAt)))
			}
			e.Block()
			if a.ext != nil {
				a.ext.Send(g.Out.Id("TRADE_CLOSE_OUT"))
			}
			return
		}

		// SECURITY: Ensure we have a valid identified partner before accepting any items
		// Note: Room index 0 is valid, so we only check if partnerName is resolved.
		// Try to resolve briefly by requesting room users if the name is unknown
		if partnerName == "" {
			if partnerTradeID > 0 {
				a.AddLog("[SECURITY] Partner name unknown. Requesting room users and attempting quick resolve...")
				// Request a fresh room users packet (rate-limited inside)
				a.requestRoomUsers()
				if u, ok := a.waitForRoomUserByTradeOrChat(partnerTradeID, 800*time.Millisecond); ok {
					a.tradeMu.Lock()
					a.lastTradePartner = u.Username
					a.lastTradePartnerID = u.TradeID
					a.lastTradePartnerChatID = u.ChatID
					partnerName = u.Username
					a.tradeMu.Unlock()
					a.AddLog(fmt.Sprintf("[ROOM] Resolved partner %d -> %s (accept-time)", partnerTradeID, partnerName))
				}
			}

			if partnerName == "" {
				a.AddLog("[SECURITY] Blocking trade: Partner identity (Name) could not be verified from room data.")
				e.Block()
				a.ext.Send(g.Out.Id("TRADE_CLOSE_OUT"))
				return
			}
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
			// Snapshot last parsed items (if any)
			a.tradeMu.Lock()
			lastItems := a.lastTradeItems
			a.tradeMu.Unlock()

			// Map of matched allowed item -> qty
			matchedItems := make(map[string]int)
			unrecognizedItems := make(map[string]int)
			for _, it := range lastItems {
				low := strings.ToLower(strings.TrimSpace(it.Name))
				if it.IsUnrecognized {
					unrecognizedItems[low] += it.Quantity
				} else {
					matchedItems[low] += it.Quantity
				}
			}

			// SECURITY CHECK: If there are ANY real furniture items in the trade (identified
			// by the II/wall markers in parseTradeItems) that are NOT on our authorized list,
			// we must block the trade.
			if len(unrecognizedItems) > 0 {
				var unrecognized []string
				for n := range unrecognizedItems {
					unrecognized = append(unrecognized, n)
				}
				a.AddLog(fmt.Sprintf("[SECURITY] Blocking trade: unauthorized furniture detected: %v", unrecognized))

				a.tradeMu.Lock()
				displayNames := a.allowedDisplayNamesCache
				a.tradeMu.Unlock()

				msg := fmt.Sprintf("%s, trade rejected: unauthorized items detected.", partnerName)
				if len(displayNames) > 0 {
					msg = fmt.Sprintf("%s, trade rejected: unauthorized items detected. We only accept: %s", partnerName, strings.Join(displayNames, ", "))
				}
				a.queueShout(partnerName, msg)

				if a.ctx != nil {
					go runtime.EventsEmit(a.ctx, "debugEvent", map[string]interface{}{"ts": time.Now().Format(time.RFC3339), "type": "incoming-trade", "decision": "blocked", "reason": "unauthorized_items_detected", "player": partnerName, "unrecognized": unrecognized})
				}
				e.Block()
				a.ext.Send(g.Out.Id("TRADE_CLOSE_OUT"))
				return
			}

			if len(matchedItems) == 0 {
				a.AddLog("[FILTER] Blocking acceptance: no authorized items found in trade.")
				a.queueShout(partnerName, fmt.Sprintf("%s, trade rejected: no authorized items found.", partnerName))
				e.Block()
				a.ext.Send(g.Out.Id("TRADE_CLOSE_OUT"))
				return
			}

			// 2. Check payout coverage: ensure dealer hand + incoming items can cover
			// the required payout assuming a 2x multiplier. If not, block the trade.
			// Skip this check if in split-banker mode (strip scan skipped).
			if !a.GetSkipStripScan() {
				mult := 2
				// Snapshot last parsed items (incoming counts)
				a.tradeMu.Lock()
				lastItemsCopy := make([]TradeItem, len(a.lastTradeItems))
				copy(lastItemsCopy, a.lastTradeItems)
				a.tradeMu.Unlock()

				// Build hand map from current inventory (base name -> qty)
				handMap := make(map[string]int)
				a.inventoryMu.RLock()
				for name, ids := range a.inventory {
					base := name
					if star := strings.LastIndex(name, "*"); star > 0 {
						base = name[:star]
					}
					handMap[base] += len(ids)
				}
				a.inventoryMu.RUnlock()

				// Build incoming map from observed partner items
				incomingMap := make(map[string]int)
				for _, it := range lastItemsCopy {
					n := strings.ToLower(strings.TrimSpace(it.Name))
					base := n
					if star := strings.LastIndex(n, "*"); star > 0 {
						base = n[:star]
					}
					incomingMap[base] += it.Quantity
				}

				// Compute required payouts from matchedItems
				required := make(map[string]int)
				for k, q := range matchedItems {
					base := k
					if star := strings.LastIndex(k, "*"); star > 0 {
						base = k[:star]
					}
					required[base] += q * mult
				}

				// Detect shortages
				short := false
				for name, need := range required {
					have := handMap[name]
					inc := incomingMap[name]
					available := have + inc
					if available < need {
						short = true
						a.AddLog(fmt.Sprintf("[FILTER] Blocking acceptance: insufficient payout stock for %s required=%d available=%d (hand=%d incoming=%d)", name, need, available, have, inc))
						a.queueShout(partnerName, fmt.Sprintf("%s, trade cancelled. Please wait for the Banker to restock before betting.", partnerName))
						break
					}
				}
				if short {
					e.Block()
					a.ext.Send(g.Out.Id("TRADE_CLOSE_OUT"))
					return
				}
			}

			// Enforce configured limits per-settings
			maxQty := a.getIntSetting("max_qty_per_unique", 10)
			maxUnique := a.getIntSetting("max_unique_items", 6)
			minQty := a.getIntSetting("min_qty_per_unique", 1)

			// Check per-item qty
			for itName, qty := range matchedItems {
				if maxQty > 0 && qty > maxQty {
					a.AddLog(fmt.Sprintf("[FILTER] Blocking acceptance: %s offered %d which exceeds max per-unique %d", itName, qty, maxQty))
					a.queueShout(partnerName, fmt.Sprintf("%s, trade cancelled: quantity exceeds the limit of %d.", partnerName, maxQty))
					e.Block()
					a.ext.Send(g.Out.Id("TRADE_CLOSE_OUT"))
					return
				}
				if minQty > 0 && qty < minQty {
					a.AddLog(fmt.Sprintf("[FILTER] Blocking acceptance: %s offered %d which is below min per-unique %d", itName, qty, minQty))
					a.queueShout(partnerName, fmt.Sprintf("%s, trade cancelled: quantity is below the minimum limit of %d.", partnerName, minQty))
					e.Block()
					a.ext.Send(g.Out.Id("TRADE_CLOSE_OUT"))
					return
				}
			}

			// Check unique count (total items found in trade, matched or not)
			if maxUnique > 0 && len(matchedItems) > maxUnique {
				a.AddLog(fmt.Sprintf("[FILTER] Blocking acceptance: player offered %d unique items (limit %d)", len(matchedItems), maxUnique))
				a.queueShout(partnerName, fmt.Sprintf("%s, please consolidate your trade. I can only process %d unique item types at once.", partnerName, maxUnique))
				e.Block()
				a.ext.Send(g.Out.Id("TRADE_CLOSE_OUT"))
				return
			}

			// Allowed: accept and emit debug
			a.AddLog(fmt.Sprintf("[FILTER] Validated trade: matched items %v", matchedItems))
			if a.ctx != nil {
				go runtime.EventsEmit(a.ctx, "debugEvent", map[string]interface{}{"ts": time.Now().Format(time.RFC3339), "type": "incoming-trade", "decision": "accepted", "player": partnerName, "items": matchedItems, "max_qty": maxQty, "max_unique": maxUnique, "min_qty": minQty})
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

		}
	}
}

func (a *App) handlePartnerConfirm(e *g.Intercept) {
	a.AddLog("Partner confirmed trade (Stage 2).")

	// Always schedule an automatic confirm after 4 seconds (Stage 2).
	// This mirrors the behavior in the main app so outgoing payout flows
	// are confirmed reliably even when the partner confirms quickly.
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

func (a *App) handleTradeClose(e *g.Intercept) {
	a.tradeMu.Lock()
	partner := a.activeTradePartner
	screenshotPath := a.lastScreenshotPath
	a.tradeActive = false
	a.tradeStartedAt = time.Time{}
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
	a.tradeStartedAt = time.Time{}
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

			// Record payout to trade_ledger as OUT. Prefer the intercepted trade items when available,
			// otherwise fall back to the queued payout item/quantity.
			var outItems []TradeItem
			if len(lastItems) > 0 {
				outItems = lastItems
			} else {
				outItems = []TradeItem{{Name: p.ItemName, Quantity: p.Quantity}}
			}
			a.recordTradeLedger(p.Name, "OUT", outItems)

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

			break // Ensure only one payout record is consumed per completion packet
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
				// Avoid picking up payouts that are already being actively automated
				if _, ok := a.getInflight(p.Name); ok {
					continue
				}
				targets = append(targets, p)
			}
		}
		a.pMu.RUnlock()

		// Filter out banned targets so we do not attempt trades with them
		filtered := make([]Payout, 0, len(targets))
		for _, t := range targets {
			if a.isPartnerBanned(t.Name, t.TradeID) {
				a.AddLog(fmt.Sprintf("[BAN] Skipping target %s (tradeid=%d) due to active ban", t.Name, t.TradeID))
				continue
			}
			filtered = append(filtered, t)
		}
		if len(filtered) == 0 {
			// No non-banned targets this cycle
			continue
		}
		targets = filtered

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

	// Abort early if partner is currently banned
	if a.isPartnerBanned(p.Name, p.TradeID) {
		a.AddLog(fmt.Sprintf("Aborting automation for %s: partner currently banned", p.Name))
		a.pMu.Lock()
		for i := range a.payouts {
			if a.payouts[i].ID == p.ID {
				a.payouts[i].Status = "Pending"
				break
			}
		}
		a.pMu.Unlock()
		a.persistPayoutStatus(p.ID, "Pending")
		a.emitUpdate()
		return
	}

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

		// Step 3: Inventory Verification (Dropped Packet Floor)
		// If the confirm loop exhausted without completion, explicitly refresh inventory
		// and check if the items are gone. If they are, the trade likely succeeded.
		a.AddLog(fmt.Sprintf("Confirm stage timed out for %s; performing inventory verification...", p.Name))
		a.RefreshInventory()

		// Wait for scan to complete (max 5s)
		scanTimedOut := true
		for s := 0; s < 50; s++ {
			time.Sleep(100 * time.Millisecond)
			a.stripScanMu.Lock()
			active := a.stripScanActive
			a.stripScanMu.Unlock()
			if !active {
				scanTimedOut = false
				break
			}
		}

		if !scanTimedOut {
			// Check if the items we tried to trade are still there
			a.inventoryMu.RLock()
			currentIDs, _ := a.inventory[strings.ToLower(p.ItemName)]
			a.inventoryMu.RUnlock()

			// Simple heuristic: if our count for this item is now less than what we
			// tried to trade (or if specific IDs are gone), assume success.
			// Using IDs set from earlier in this attempt (ids[0:toAdd])
			stillHave := false
			for _, idToFind := range ids[:toAdd] {
				found := false
				for _, curID := range currentIDs {
					if curID == idToFind {
						found = true
						break
					}
				}
				if found {
					stillHave = true
					break
				}
			}

			if !stillHave {
				a.AddLog(fmt.Sprintf("INVENTORY_VERIFY: Items are GONE from hand. Deducing successful trade for %s.", p.Name))
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

				// Record reliable ledger OUT and mark banker trades
				if a.db != nil {
					if p.BankerTradeID > 0 {
						a.db.Exec(context.Background(), "UPDATE public.banker_trades SET status = 'completed', risk_status = 'completed', risk_bank = 0 WHERE id = $1 AND status = 'paying'", p.BankerTradeID)
					} else if p.TradeID > 0 {
						a.db.Exec(context.Background(), "UPDATE public.banker_trades SET status = 'completed', risk_status = 'completed', risk_bank = 0 WHERE player_trade_id = $1 AND status = 'paying'", p.TradeID)
					} else {
						a.db.Exec(context.Background(), "UPDATE public.banker_trades SET status = 'completed', risk_status = 'completed', risk_bank = 0 WHERE player_name = $1 AND status = 'paying'", p.Name)
					}
				}
				a.recordTradeLedger(p.Name, "OUT", []TradeItem{{Name: p.ItemName, Quantity: p.Quantity}})
				a.AddLog(fmt.Sprintf("Payout for %s COMPLETED via inventory verification.", p.Name))
				return
			} else {
				a.AddLog("INVENTORY_VERIFY: Items still in hand. Trade definitely failed.")
			}
		} else {
			a.AddLog("INVENTORY_VERIFY: Scan timed out; proceeding with normal retry.")
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

			// Record the payout to trade_ledger as an OUT entry
			a.recordTradeLedger(p.Name, "OUT", []TradeItem{{Name: p.ItemName, Quantity: p.Quantity}})
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
