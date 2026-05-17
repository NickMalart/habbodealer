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
	Status    string `json:"status"` // "Pending", "In Room", "Trading", "Completed", "Failed"
	CreatedAt string `json:"createdAt"`
}

type ParsedUsers28User struct {
	Username string `json:"username"`
	TradeID  int    `json:"trade_id"`
	ChatID   int    `json:"chat_id"`
	TokenHex string `json:"token_hex"`
}

type ParsedUsers28Result struct {
	Users []ParsedUsers28User `json:"users"`
}

type App struct {
	ctx     context.Context
	ext     *g.Ext
	payouts []Payout
	pMu     sync.RWMutex

	// Room state
	roomUsers   map[string]ParsedUsers28User
	roomUsersMu sync.RWMutex

	// Inventory state
	inventory   map[string][]int // name -> list of strip IDs
	inventoryMu sync.RWMutex

	// Trade state
	activeTradePartner string
	activeTradeTarget  int
	tradeActive        bool
	tradeAccepted      bool
	tradeMu            sync.Mutex

	// Config
	pythonExec   string
	parserScript string
}

func NewApp() *App {
	return &App{
		payouts:    []Payout{},
		roomUsers:  make(map[string]ParsedUsers28User),
		inventory:  make(map[string][]int),
		pythonExec: "python",
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.loadPayouts()
	a.initParser()

	a.ext = g.NewExt(g.ExtInfo{
		Title:       "Auto Payout Bot",
		Description: "Automatically trades items to people when they join the room",
		Version:     "1.0.0",
		Author:      "Gemini CLI",
	})

	// Register custom headers for this version of goearth
	a.ext.Headers().Add("STRIPINFO_IN", g.Header{Dir: g.In, Value: 98})
	a.ext.Headers().Add("TRADE_OPEN_IN", g.Header{Dir: g.In, Value: 104})
	a.ext.Headers().Add("TRADE_CLOSE_IN", g.Header{Dir: g.In, Value: 105})
	a.ext.Headers().Add("TRADE_COMPLETED_IN", g.Header{Dir: g.In, Value: 112})
	
	a.ext.Headers().Add("GETSTRIP_OUT", g.Header{Dir: g.Out, Value: 101})
	a.ext.Headers().Add("TRADE_OPEN_OUT", g.Header{Dir: g.Out, Value: 71})
	a.ext.Headers().Add("TRADE_ADDITEM_OUT", g.Header{Dir: g.Out, Value: 72})
	a.ext.Headers().Add("TRADE_ACCEPT_OUT", g.Header{Dir: g.Out, Value: 69})

	a.ext.Intercept(in.USERS).With(a.handleRoomUsers)
	a.ext.Intercept(g.In.Id("STRIPINFO_IN")).With(a.handleStripInfo)
	a.ext.Intercept(g.In.Id("TRADE_OPEN_IN")).With(a.handleTradeOpen)
	a.ext.Intercept(g.In.Id("TRADE_CLOSE_IN")).With(a.handleTradeClose)
	a.ext.Intercept(g.In.Id("TRADE_COMPLETED_IN")).With(a.handleTradeCompleted)

	a.ext.Activated(func() {
		a.ShowWindow()
	})

	go a.ext.Run()
	go a.payoutMonitor()
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

	script := filepath.Join("scripts", "parse_users28.py")
	if _, err := os.Stat(script); err != nil {
		script = filepath.Join("..", "scripts", "parse_users28.py")
	}
	if _, err := os.Stat(script); err != nil {
		abs, _ := filepath.Abs(script)
		a.parserScript = abs
	}
}

// --- Wails Methods ---

func (a *App) GetPayouts() []Payout {
	a.pMu.RLock()
	defer a.pMu.RUnlock()
	return a.payouts
}

func (a *App) AddPayout(name, itemName string, qty int) {
	log.Printf("[DEBUG] AddPayout called: name=%s, item=%s, qty=%d", name, itemName, qty)
	a.pMu.Lock()
	defer a.pMu.Unlock()

	p := Payout{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		Name:      strings.TrimSpace(name),
		ItemName:  strings.TrimSpace(strings.ToLower(itemName)),
		Quantity:  qty,
		Status:    "Pending",
		CreatedAt: time.Now().Format("2006-01-02 15:04:05"),
	}
	a.payouts = append(a.payouts, p)
	a.savePayouts()
	a.emitUpdate()
}

func (a *App) DeletePayout(id string) {
	a.pMu.Lock()
	defer a.pMu.Unlock()

	newPayouts := []Payout{}
	for _, p := range a.payouts {
		if p.ID != id {
			newPayouts = append(newPayouts, p)
		}
	}
	a.payouts = newPayouts
	a.savePayouts()
	a.emitUpdate()
}

func (a *App) ClearCompleted() {
	a.pMu.Lock()
	defer a.pMu.Unlock()

	newPayouts := []Payout{}
	for _, p := range a.payouts {
		if p.Status != "Completed" {
			newPayouts = append(newPayouts, p)
		}
	}
	a.payouts = newPayouts
	a.savePayouts()
	a.emitUpdate()
}

func (a *App) RefreshInventory() {
	if a.ext != nil {
		a.ext.Send(g.Out.Id("GETSTRIP_OUT"), "new")
	}
}

func (a *App) ReturnAllToOwner(ownerName string) {
	a.pMu.Lock()
	defer a.pMu.Unlock()

	a.inventoryMu.RLock()
	defer a.inventoryMu.RUnlock()

	for name, ids := range a.inventory {
		if len(ids) > 0 {
			a.payouts = append(a.payouts, Payout{
				ID:        fmt.Sprintf("return-%d", time.Now().UnixNano()),
				Name:      ownerName,
				ItemName:  name,
				Quantity:  len(ids),
				Status:    "Pending",
				CreatedAt: time.Now().Format("2006-01-02 15:04:05"),
			})
		}
	}
	a.savePayouts()
	a.emitUpdate()
}

// --- Internal Logic ---

func (a *App) emitUpdate() {
	runtime.EventsEmit(a.ctx, "payoutsUpdate", a.GetPayouts())
}

func (a *App) loadPayouts() {
	data, err := os.ReadFile("payouts.json")
	if err == nil {
		json.Unmarshal(data, &a.payouts)
	}
}

func (a *App) savePayouts() {
	data, _ := json.MarshalIndent(a.payouts, "", "  ")
	os.WriteFile("payouts.json", data, 0644)
}

func (a *App) handleRoomUsers(e *g.Intercept) {
	if a.parserScript == "" {
		return
	}

	tmpFile, err := os.CreateTemp("", "users28_*.bin")
	if err != nil {
		return
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	tmpFile.Write(e.Packet.Data)
	tmpFile.Close()

	cmd := exec.Command(a.pythonExec, a.parserScript, "--input", tmpPath, "--json")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return
	}

	var result ParsedUsers28Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return
	}

	a.roomUsersMu.Lock()
	for _, u := range result.Users {
		a.roomUsers[strings.ToLower(u.Username)] = u
	}
	a.roomUsersMu.Unlock()

	a.updatePayoutStatuses()
}

func (a *App) updatePayoutStatuses() {
	a.pMu.Lock()
	defer a.pMu.Unlock()
	a.roomUsersMu.RLock()
	defer a.roomUsersMu.RUnlock()

	changed := false
	for i, p := range a.payouts {
		if p.Status == "Completed" || p.Status == "Trading" {
			continue
		}
		if _, ok := a.roomUsers[strings.ToLower(p.Name)]; ok {
			if p.Status == "Pending" {
				a.payouts[i].Status = "In Room"
				changed = true
			}
		} else {
			if p.Status == "In Room" {
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
		return
	}

	newInventory := make(map[string][]int)

	for i := 0; i < count; i++ {
		if pos >= len(data) {
			break
		}

		mainID, ok := readVL64()
		if !ok {
			break
		}

		extraCount, ok := readVL64()
		if !ok {
			break
		}
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

		newInventory[classRaw] = append(newInventory[classRaw], mainID)
		newInventory[classRaw] = append(newInventory[classRaw], extraIDs...)
	}

	a.inventoryMu.Lock()
	a.inventory = newInventory
	a.inventoryMu.Unlock()
	log.Printf("Inventory updated: %d categories", len(newInventory))
}

func (a *App) handleTradeOpen(e *g.Intercept) {
	a.tradeMu.Lock()
	a.tradeActive = true
	a.tradeAccepted = false
	a.tradeMu.Unlock()
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
				// Revert to In Room so it can be retried
				a.payouts[i].Status = "In Room"
			}
		}
		a.pMu.Unlock()
		a.emitUpdate()
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
			}
		}
		a.pMu.Unlock()
		a.emitUpdate()
		a.savePayouts()
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

					log.Printf("Opening trade with %s (ID %d)", target.Name, user.TradeID)
					a.ext.Send(g.Out.Id("TRADE_OPEN_OUT"), user.TradeID)
					
					go a.automateTrade(target)
				}
			}
			a.tradeMu.Unlock()
		}
	}
}

func (a *App) automateTrade(p *Payout) {
	time.Sleep(1500 * time.Millisecond)
	
	a.tradeMu.Lock()
	if !a.tradeActive {
		a.tradeMu.Unlock()
		return
	}
	a.tradeMu.Unlock()

	a.inventoryMu.RLock()
	ids, ok := a.inventory[strings.ToLower(p.ItemName)]
	a.inventoryMu.RUnlock()

	if !ok || len(ids) < p.Quantity {
		log.Printf("Shortage for %s: need %d of %s, only have %d", p.Name, p.Quantity, p.ItemName, len(ids))
	}

	toAdd := p.Quantity
	if len(ids) < toAdd {
		toAdd = len(ids)
	}

	for i := 0; i < toAdd; i++ {
		a.tradeMu.Lock()
		if !a.tradeActive {
			a.tradeMu.Unlock()
			return
		}
		a.tradeMu.Unlock()

		itemID := ids[i]
		log.Printf("Adding item %d (%s) for %s", itemID, p.ItemName, p.Name)
		a.ext.Send(g.Out.Id("TRADE_ADDITEM_OUT"), itemID)
		time.Sleep(500 * time.Millisecond)
	}

	time.Sleep(1 * time.Second)
	a.ext.Send(g.Out.Id("TRADE_ACCEPT_OUT"))
}

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:  "Auto Payout Bot",
		Width:  800,
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
