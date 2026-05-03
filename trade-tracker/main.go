package main

import (
	"bufio"
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
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	g "xabbo.b7c.io/goearth"
	gencoding "xabbo.b7c.io/goearth/encoding"
	in "xabbo.b7c.io/goearth/shockwave/in"
	out "xabbo.b7c.io/goearth/shockwave/out"
)

//go:embed all:frontend/dist
var assets embed.FS

var ext = g.NewExt(g.ExtInfo{
	Title:       "Trade Tracker",
	Description: "Track incoming trades in timestamped sessions",
	Version:     "1.0.0",
	Author:      "Dubbo",
})

type ParsedUsers28User struct {
	Username string `json:"username"`
	TradeID  int    `json:"trade_id"`
}

type TradeEntry struct {
	Timestamp      string      `json:"timestamp"`
	PartnerName    string      `json:"partnerName"`
	PartnerTradeID int         `json:"partnerTradeId"`
	PayloadHex     string      `json:"payloadHex"`
	FurniItems     []TradeItem `json:"furniItems,omitempty"`
}

type TradeSession struct {
	ID        int          `json:"id"`
	StartedAt string       `json:"startedAt"`
	EndedAt   string       `json:"endedAt,omitempty"`
	Entries   []TradeEntry `json:"entries"`
	DBID      int64        `json:"-"`
}

type TrackerState struct {
	Connected      bool           `json:"connected"`
	Running        bool           `json:"running"`
	CurrentSession *TradeSession  `json:"currentSession,omitempty"`
	Sessions       []TradeSession `json:"sessions"`
}

type App struct {
	ctx context.Context

	mu sync.Mutex

	connected      bool
	running        bool
	nextSessionID  int
	currentSession *TradeSession
	sessions       []TradeSession
	usersByTradeID map[int]string
	db             *pgxpool.Pool
	ownerKey       string

	// Trade lifecycle tracking (per-active-trade)
	currentTradePartnerID   int
	currentTradePartnerName string
	lastAllTradeItems       []TradeItem
	ownTradeItems           []TradeItem // items we have added to the trade
	partnerAcceptedSnapshot []TradeItem
	partnerAccepted         bool
	ourAccepted             bool
	tradeRecorded           bool
}

// lastAddWasOurs is set atomically when we send TRADE_ADDITEM (outgoing #72)
// so TRADE_ITEMS parsing can attribute the new items to the correct side.
var lastAddWasOurs int32 // 1 = our add, 0 = partner add

var itemClassMu sync.RWMutex
var knownItemClasses = map[string]struct{}{}
var knownItemClassList []string

func hasKnownItemClasses() bool {
	itemClassMu.RLock()
	n := len(knownItemClasses)
	itemClassMu.RUnlock()
	return n > 0
}

func isKnownItemClass(name string) bool {
	itemClassMu.RLock()
	_, ok := knownItemClasses[strings.ToLower(strings.TrimSpace(name))]
	itemClassMu.RUnlock()
	return ok
}

func setKnownItemClasses(next map[string]struct{}) {
	list := make([]string, 0, len(next))
	for k := range next {
		list = append(list, k)
	}
	sort.Slice(list, func(i, j int) bool {
		return len(list[i]) > len(list[j])
	})

	itemClassMu.Lock()
	knownItemClasses = next
	knownItemClassList = list
	itemClassMu.Unlock()
}

func loadExternalTexts(gameHost string) error {
	url := "https://origins-gamedata.habbo.com/external_texts/1"
	switch strings.ToLower(strings.TrimSpace(gameHost)) {
	case "game-obr.habbo.com":
		url = "https://origins-gamedata.habbo.com.br/external_texts/1"
	case "game-oes.habbo.com":
		url = "https://origins-gamedata.habbo.es/external_texts/1"
	}

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("external_texts http status %d", resp.StatusCode)
	}

	classes := map[string]struct{}{}
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))

		if strings.HasPrefix(key, "furni_") && strings.HasSuffix(key, "_name") {
			class := strings.TrimSuffix(strings.TrimPrefix(key, "furni_"), "_name")
			if class != "" {
				classes[class] = struct{}{}
			}
			continue
		}

		if strings.HasPrefix(key, "wallitem_") && strings.HasSuffix(key, "_name") {
			class := strings.TrimSuffix(strings.TrimPrefix(key, "wallitem_"), "_name")
			if class != "" {
				classes[class] = struct{}{}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	if len(classes) == 0 {
		return fmt.Errorf("external_texts parsed with zero known item classes")
	}

	setKnownItemClasses(classes)
	log.Printf("[TRADE_TRACKER_DEBUG] loaded %d known item classes from external_texts", len(classes))
	return nil
}

type DBConfig struct {
	DatabaseURL string `json:"databaseUrl"`
	OwnerKey    string `json:"ownerKey,omitempty"`
}

func NewApp() *App {
	return &App{
		usersByTradeID: make(map[int]string),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.initDatabase()
	go a.runExt()
}

func (a *App) shutdown(context.Context) {
	a.mu.Lock()
	a.running = false
	db := a.db
	a.db = nil
	a.mu.Unlock()
	if db != nil {
		db.Close()
	}
}

func (a *App) runExt() {
	ext.Run()
}

func (a *App) logDebug(format string, args ...interface{}) {
	log.Printf("[TRADE_TRACKER_DEBUG] "+format, args...)
}

// normalizeUsername strips known parser artefacts (e.g. "adfAmaver1995" → "Amaver1995").
func normalizeUsername(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) >= 4 {
		maxPrefix := 4
		if len(raw)-3 < maxPrefix {
			maxPrefix = len(raw) - 3
		}
		for i := 1; i <= maxPrefix; i++ {
			prefixOK := true
			for j := 0; j < i; j++ {
				if raw[j] < 'a' || raw[j] > 'z' {
					prefixOK = false
					break
				}
			}
			if !prefixOK {
				continue
			}
			if raw[i] < 'A' || raw[i] > 'Z' {
				continue
			}
			if i+1 < len(raw) && (raw[i+1] < 'a' || raw[i+1] > 'z') {
				continue
			}
			return raw[i:]
		}
	}
	return raw
}

// diffItems subtracts own items from the combined TRADE_ITEMS list to get partner-only items.
func diffItems(all []TradeItem, subtract []TradeItem) []TradeItem {
	subtractQty := make(map[string]int, len(subtract))
	for _, item := range subtract {
		subtractQty[item.Name] += item.Quantity
	}
	qtys := make(map[string]int, len(all))
	rawByName := make(map[string]string, len(all))
	names := make([]string, 0, len(all))
	for _, item := range all {
		if qtys[item.Name] == 0 {
			names = append(names, item.Name)
		}
		qtys[item.Name] += item.Quantity
		if rawByName[item.Name] == "" {
			rawByName[item.Name] = item.Raw
		}
	}
	sort.Strings(names)
	result := make([]TradeItem, 0)
	for _, name := range names {
		remaining := qtys[name] - subtractQty[name]
		if remaining > 0 {
			result = append(result, TradeItem{Name: name, Quantity: remaining, Raw: rawByName[name]})
		}
	}
	return result
}

func copySession(s *TradeSession) *TradeSession {
	if s == nil {
		return nil
	}
	out := &TradeSession{
		ID:        s.ID,
		StartedAt: s.StartedAt,
		EndedAt:   s.EndedAt,
		Entries:   make([]TradeEntry, len(s.Entries)),
	}
	copy(out.Entries, s.Entries)
	return out
}

func (a *App) GetState() TrackerState {
	a.mu.Lock()
	defer a.mu.Unlock()

	state := TrackerState{
		Connected: a.connected,
		Running:   a.running,
		Sessions:  make([]TradeSession, len(a.sessions)),
	}

	for i := range a.sessions {
		state.Sessions[i] = *copySession(&a.sessions[i])
	}
	state.CurrentSession = copySession(a.currentSession)

	return state
}

func (a *App) emitUpdate() {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "trackerStateUpdate", a.GetState())
}

func (a *App) StartTracking() TrackerState {
	now := time.Now().UTC()
	var sessionID int
	var sessionStartedAt string

	a.mu.Lock()
	if !a.running {
		a.running = true
		a.nextSessionID++
		sessionID = a.nextSessionID
		sessionStartedAt = now.Format(time.RFC3339)
		a.currentSession = &TradeSession{
			ID:        sessionID,
			StartedAt: sessionStartedAt,
			Entries:   []TradeEntry{},
		}
		a.logDebug("start requested: opened in-memory session id=%d startedAt=%s", sessionID, sessionStartedAt)
	}
	db := a.db
	a.mu.Unlock()

	if sessionID == 0 {
		a.logDebug("start requested while already running: no new session created")
	}

	if db == nil {
		a.logDebug("start requested with db=nil: Neon writes disabled until DB init succeeds")
	}

	if db != nil && sessionID > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()

		var dbSessionID int64
		err := db.QueryRow(
			ctx,
			`INSERT INTO trade_sessions (started_at, owner_key) VALUES ($1, $2) RETURNING id`,
			now,
			a.ownerKey,
		).Scan(&dbSessionID)
		if err != nil {
			log.Printf("[DB] failed to create trade session: %v", err)
			a.logDebug("session insert failed: sessionID=%d ownerKey=%q err=%v", sessionID, a.ownerKey, err)
		} else {
			a.mu.Lock()
			if a.currentSession != nil && a.currentSession.ID == sessionID {
				a.currentSession.DBID = dbSessionID
			}
			a.mu.Unlock()
			a.logDebug("session insert ok: sessionID=%d dbSessionID=%d ownerKey=%q", sessionID, dbSessionID, a.ownerKey)
		}
	}

	a.emitUpdate()
	return a.GetState()
}

func (a *App) StopTracking() TrackerState {
	now := time.Now().UTC()
	var dbSessionID int64

	a.mu.Lock()
	if a.running {
		a.running = false
		if a.currentSession != nil {
			a.currentSession.EndedAt = now.Format(time.RFC3339)
			dbSessionID = a.currentSession.DBID
			a.sessions = append(a.sessions, *a.currentSession)
			a.currentSession = nil
			a.logDebug("stop requested: closed current session dbSessionID=%d", dbSessionID)
		}
	}
	db := a.db
	a.mu.Unlock()

	if db != nil && dbSessionID > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()

		if _, err := db.Exec(
			ctx,
			`UPDATE trade_sessions SET ended_at = $1 WHERE id = $2 AND owner_key = $3`,
			now,
			dbSessionID,
			a.ownerKey,
		); err != nil {
			log.Printf("[DB] failed to close trade session %d: %v", dbSessionID, err)
			a.logDebug("session close update failed: dbSessionID=%d ownerKey=%q err=%v", dbSessionID, a.ownerKey, err)
		} else {
			a.logDebug("session close update ok: dbSessionID=%d ownerKey=%q", dbSessionID, a.ownerKey)
		}
	} else {
		a.logDebug("stop requested without DB close update: dbNil=%t dbSessionID=%d", db == nil, dbSessionID)
	}

	a.emitUpdate()
	return a.GetState()
}

func (a *App) ClearSessions() TrackerState {
	a.mu.Lock()
	a.sessions = nil
	if a.currentSession != nil {
		a.currentSession.Entries = nil
	}
	a.mu.Unlock()
	a.emitUpdate()
	return a.GetState()
}

func decodeLeadingVL64(data []byte) (int, bool) {
	if len(data) == 0 {
		return 0, false
	}
	vlen := gencoding.VL64DecodeLen(data[0])
	if vlen <= 0 || vlen > 6 || vlen > len(data) {
		return 0, false
	}
	v := gencoding.VL64Decode(data[:vlen])
	if v <= 0 {
		return 0, false
	}
	return v, true
}

type TradeItem struct {
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
	Raw      string `json:"raw,omitempty"`
}

// Accept candidate tokens containing lowercase letters, digits and underscores.
var itemClassRe = regexp.MustCompile(`[a-z0-9_]+`)

func normalizeTradeClassToken(raw string) string {
	token := strings.ToLower(strings.TrimSpace(raw))
	if token == "" {
		return ""
	}

	matches := itemClassRe.FindAllString(token, -1)
	if len(matches) == 0 {
		return ""
	}

	itemClassMu.RLock()
	list := append([]string(nil), knownItemClassList...)
	itemClassMu.RUnlock()

	// Prefer known classes first (same spirit as tracker external_texts resolution).
	for _, m := range matches {
		if isKnownItemClass(m) {
			return m
		}
	}

	// Some packets prepend noisy prefixes to the class. Try suffix/underscore-joined matches.
	for _, m := range matches {
		for _, cls := range list {
			if m == cls || strings.HasSuffix(m, cls) || strings.Contains(m, "_"+cls) {
				return cls
			}
		}
	}

	// If no known classes loaded (network issue), fallback to previous behavior.
	if !hasKnownItemClasses() {
		best := ""
		for _, m := range matches {
			if len(m) > len(best) {
				best = m
			}
		}
		return best
	}

	return ""
}

func (a *App) parseTradeItemsSimple(data []byte) []TradeItem {
	counts := map[string]int{}
	rawByName := map[string]string{}

	fields := bytes.Split(data, []byte{0x02})
	for _, field := range fields {
		if len(field) == 0 {
			continue
		}
		s := strings.TrimSpace(string(field))
		raw := s

		var cand string
		if idx := strings.Index(s, "{"); idx != -1 {
			cand = s[idx+1:]
		} else if idx := strings.LastIndex(s, "|"); idx != -1 {
			cand = s[idx+1:]
		} else {
			cand = s
		}

		qty := 1
		if star := strings.LastIndex(cand, "*"); star != -1 {
			num := cand[star+1:]
			if n, err := strconv.Atoi(num); err == nil && n > 0 {
				qty = n
				cand = cand[:star]
			}
		}

		cand = strings.TrimSpace(cand)
		if cand == "" {
			continue
		}
		name := normalizeTradeClassToken(cand)
		if name == "" {
			continue
		}
		counts[name] += qty
		if _, ok := rawByName[name]; !ok {
			rawByName[name] = raw
		}
	}

	if len(counts) == 0 {
		return []TradeItem{}
	}

	names := make([]string, 0, len(counts))
	for n := range counts {
		names = append(names, n)
	}
	sort.Strings(names)

	items := make([]TradeItem, 0, len(names))
	for _, n := range names {
		items = append(items, TradeItem{Name: n, Quantity: counts[n], Raw: rawByName[n]})
	}

	return items
}

func (a *App) handleIncomingTradeOpen(e *g.Intercept) {
	if e == nil || e.Packet == nil {
		return
	}
	// Handle relevant trade headers: 104 (open), 108 (items), 109 (partner accept), 69 (our accept outgoing)
	hdr := e.Packet.Header
	if hdr.Value == 104 || hdr.Value == 108 || hdr.Value == 109 || hdr.Value == 69 {
		a.mu.Lock()
		running := a.running
		hasSession := a.currentSession != nil
		dbReady := a.db != nil
		sessionDBID := int64(0)
		if a.currentSession != nil {
			sessionDBID = a.currentSession.DBID
		}
		a.mu.Unlock()
		a.logDebug("packet seen: dir=%v header=%d running=%t hasSession=%t dbReady=%t sessionDBID=%d", hdr.Dir, hdr.Value, running, hasSession, dbReady, sessionDBID)
	}

	// TRADE_OPEN incoming 104: record partner id/name in-memory for the upcoming trade
	if hdr.Dir == g.In && hdr.Value == 104 {
		tradeID, _ := decodeLeadingVL64(e.Packet.Data)

		a.mu.Lock()
		if !a.running || a.currentSession == nil {
			a.mu.Unlock()
			return
		}

		a.currentTradePartnerID = tradeID
		if tradeID > 0 {
			if name, ok := a.usersByTradeID[tradeID]; ok && strings.TrimSpace(name) != "" {
				a.currentTradePartnerName = name
			} else {
				a.currentTradePartnerName = fmt.Sprintf("Unknown (#%d)", tradeID)
			}
		} else {
			a.currentTradePartnerName = "Unknown"
		}
		// Try to resolve name late in case USERS packet arrived after this trade open
		if strings.HasPrefix(a.currentTradePartnerName, "Unknown") && tradeID > 0 {
			if name, ok := a.usersByTradeID[tradeID]; ok && strings.TrimSpace(name) != "" {
				a.currentTradePartnerName = normalizeUsername(name)
			}
		}
		// Reset per-trade state
		a.partnerAccepted = false
		a.ourAccepted = false
		a.tradeRecorded = false
		a.partnerAcceptedSnapshot = nil
		a.lastAllTradeItems = nil
		a.ownTradeItems = nil
		a.mu.Unlock()

		// Request a fresh USERS packet from the server, then wait for the
		// partner's name to arrive before giving up.
		if tradeID > 0 {
			go func(tid int) {
				a.logDebug("requesting room users to resolve partner tradeID=%d", tid)
				ext.Send(out.G_USRS)
				ext.Send(out.GETSPACENODEUSERS)
				deadline := time.Now().Add(1500 * time.Millisecond)
				for time.Now().Before(deadline) {
					time.Sleep(75 * time.Millisecond)
					a.mu.Lock()
					name, ok := a.usersByTradeID[tid]
					a.mu.Unlock()
					if ok && strings.TrimSpace(name) != "" {
						a.mu.Lock()
						// Only update if the trade is still the same one
						if a.currentTradePartnerID == tid && strings.HasPrefix(a.currentTradePartnerName, "Unknown") {
							a.currentTradePartnerName = normalizeUsername(name)
							a.logDebug("resolved partner name via wait-poll: tradeID=%d name=%q", tid, a.currentTradePartnerName)
							a.mu.Unlock()
							a.emitUpdate()
						} else {
							a.mu.Unlock()
						}
						return
					}
				}
				a.logDebug("partner name still unresolved after 1500ms: tradeID=%d", tid)
			}(tradeID)
		}

		a.emitUpdate()
		return
	}

	// TRADE_ADDITEM outgoing 72: we added an item, flag so next TRADE_ITEMS is attributed to us.
	if hdr.Dir == g.Out && hdr.Value == 72 {
		atomic.StoreInt32(&lastAddWasOurs, 1)
		return
	}

	// TRADE_ITEMS incoming 108: parse full snapshot and track own vs partner items.
	if hdr.Dir == g.In && hdr.Value == 108 {
		allItems := a.parseTradeItemsSimple(e.Packet.Data)
		wasOurs := atomic.SwapInt32(&lastAddWasOurs, 0) == 1
		a.logDebug("TRADE_ITEMS parsed: total=%d wasOurs=%t", len(allItems), wasOurs)
		for _, it := range allItems {
			a.logDebug("  item: name=%q qty=%d", it.Name, it.Quantity)
		}

		a.mu.Lock()
		prevAll := make(map[string]int, len(a.lastAllTradeItems))
		for _, it := range a.lastAllTradeItems {
			prevAll[it.Name] += it.Quantity
		}
		allMap := make(map[string]int, len(allItems))
		for _, it := range allItems {
			allMap[it.Name] += it.Quantity
		}
		// Compute added quantities in this packet vs last
		for name, q := range allMap {
			delta := q - prevAll[name]
			if delta > 0 && wasOurs {
				// Find or create own entry
				found := false
				for i := range a.ownTradeItems {
					if a.ownTradeItems[i].Name == name {
						a.ownTradeItems[i].Quantity += delta
						found = true
						break
					}
				}
				if !found {
					a.ownTradeItems = append(a.ownTradeItems, TradeItem{Name: name, Quantity: delta})
				}
			}
		}
		// Clamp own items to actual totals in case items were removed
		for i := range a.ownTradeItems {
			if total, ok := allMap[a.ownTradeItems[i].Name]; ok {
				if a.ownTradeItems[i].Quantity > total {
					a.ownTradeItems[i].Quantity = total
				}
			} else {
				a.ownTradeItems[i].Quantity = 0
			}
		}
		a.lastAllTradeItems = allItems
		a.mu.Unlock()
		return
	}

	// TRADE_ACCEPT incoming 109: partner accepted current state. Snapshot partner side.
	if hdr.Dir == g.In && hdr.Value == 109 {
		a.mu.Lock()
		a.partnerAccepted = true
		if len(a.lastAllTradeItems) > 0 {
			// Snapshot only partner-side items (all minus our own adds)
			partnerOnly := diffItems(a.lastAllTradeItems, a.ownTradeItems)
			a.partnerAcceptedSnapshot = partnerOnly
		} else {
			a.partnerAcceptedSnapshot = nil
		}
		// Late-resolve name in case USERS packet arrived after TRADE_OPEN
		if strings.HasPrefix(a.currentTradePartnerName, "Unknown") && a.currentTradePartnerID > 0 {
			if name, ok := a.usersByTradeID[a.currentTradePartnerID]; ok && strings.TrimSpace(name) != "" {
				a.currentTradePartnerName = normalizeUsername(name)
			}
		}
		ourAccepted := a.ourAccepted
		recorded := a.tradeRecorded
		db := a.db
		dbSessionID := int64(0)
		if a.currentSession != nil {
			dbSessionID = a.currentSession.DBID
		}
		partnerName := a.currentTradePartnerName
		partnerTradeID := a.currentTradePartnerID
		a.mu.Unlock()
		for _, it := range a.partnerAcceptedSnapshot {
			a.logDebug("partner snapshot item: name=%q qty=%d", it.Name, it.Quantity)
		}
		a.logDebug("incoming partner accept: ourAccepted=%t recorded=%t dbReady=%t dbSessionID=%d partner=%q tradeID=%d items=%d", ourAccepted, recorded, db != nil, dbSessionID, partnerName, partnerTradeID, len(a.partnerAcceptedSnapshot))

		if db != nil && dbSessionID > 0 && ourAccepted && !recorded {
			if err := a.persistTradeEntry(dbSessionID, partnerName, partnerTradeID, a.partnerAcceptedSnapshot, fmt.Sprintf("% X", e.Packet.Data)); err != nil {
				log.Printf("[DB] failed to persist final trade entry: %v", err)
				a.logDebug("persist attempt from partner accept failed: dbSessionID=%d err=%v", dbSessionID, err)
			} else {
				a.mu.Lock()
				a.tradeRecorded = true
				a.partnerAccepted = false
				a.ourAccepted = false
				a.partnerAcceptedSnapshot = nil
				a.lastAllTradeItems = nil
				a.mu.Unlock()
				a.logDebug("persist attempt from partner accept succeeded: dbSessionID=%d", dbSessionID)
				a.emitUpdate()
			}
		} else {
			a.logDebug("persist skipped on partner accept: dbReady=%t dbSessionID=%d ourAccepted=%t recorded=%t", db != nil, dbSessionID, ourAccepted, recorded)
			// Fallback: persist on partner accept only, for cases where outgoing accept packet is not intercepted.
			if db != nil && dbSessionID > 0 && !recorded {
				if err := a.persistTradeEntry(dbSessionID, partnerName, partnerTradeID, a.partnerAcceptedSnapshot, "PARTNER_ACCEPT_ONLY "+fmt.Sprintf("% X", e.Packet.Data)); err != nil {
					a.logDebug("fallback persist (partner accept only) failed: dbSessionID=%d err=%v", dbSessionID, err)
				} else {
					a.mu.Lock()
					a.tradeRecorded = true
					a.partnerAccepted = false
					a.ourAccepted = false
					a.partnerAcceptedSnapshot = nil
					a.lastAllTradeItems = nil
					a.mu.Unlock()
					a.logDebug("fallback persist (partner accept only) succeeded: dbSessionID=%d", dbSessionID)
					a.emitUpdate()
				}
			}
		}
		return
	}

	// Our outgoing TRADE_ACCEPT is header 69 (observe outgoing packets)
	if hdr.Dir == g.Out && hdr.Value == 69 {
		a.mu.Lock()
		a.ourAccepted = true
		// Late-resolve name in case USERS packet arrived after TRADE_OPEN
		if strings.HasPrefix(a.currentTradePartnerName, "Unknown") && a.currentTradePartnerID > 0 {
			if name, ok := a.usersByTradeID[a.currentTradePartnerID]; ok && strings.TrimSpace(name) != "" {
				a.currentTradePartnerName = normalizeUsername(name)
			}
		}
		partnerAccepted := a.partnerAccepted
		recorded := a.tradeRecorded
		db := a.db
		dbSessionID := int64(0)
		if a.currentSession != nil {
			dbSessionID = a.currentSession.DBID
		}
		partnerName := a.currentTradePartnerName
		partnerTradeID := a.currentTradePartnerID
		partnerSnap := a.partnerAcceptedSnapshot
		a.mu.Unlock()
		a.logDebug("outgoing our accept: partnerAccepted=%t recorded=%t dbReady=%t dbSessionID=%d partner=%q tradeID=%d items=%d", partnerAccepted, recorded, db != nil, dbSessionID, partnerName, partnerTradeID, len(partnerSnap))

		if db != nil && dbSessionID > 0 && partnerAccepted && !recorded {
			if err := a.persistTradeEntry(dbSessionID, partnerName, partnerTradeID, partnerSnap, fmt.Sprintf("% X", e.Packet.Data)); err != nil {
				log.Printf("[DB] failed to persist final trade entry: %v", err)
				a.logDebug("persist attempt from outgoing accept failed: dbSessionID=%d err=%v", dbSessionID, err)
			} else {
				a.mu.Lock()
				a.tradeRecorded = true
				a.partnerAccepted = false
				a.ourAccepted = false
				a.partnerAcceptedSnapshot = nil
				a.lastAllTradeItems = nil
				a.mu.Unlock()
				a.logDebug("persist attempt from outgoing accept succeeded: dbSessionID=%d", dbSessionID)
				a.emitUpdate()
			}
		} else {
			a.logDebug("persist skipped on outgoing accept: dbReady=%t dbSessionID=%d partnerAccepted=%t recorded=%t", db != nil, dbSessionID, partnerAccepted, recorded)
			// Fallback: persist on our accept only, for cases where incoming partner accept packet is missed.
			if db != nil && dbSessionID > 0 && !recorded {
				if err := a.persistTradeEntry(dbSessionID, partnerName, partnerTradeID, partnerSnap, "OUTGOING_ACCEPT_ONLY "+fmt.Sprintf("% X", e.Packet.Data)); err != nil {
					a.logDebug("fallback persist (outgoing accept only) failed: dbSessionID=%d err=%v", dbSessionID, err)
				} else {
					a.mu.Lock()
					a.tradeRecorded = true
					a.partnerAccepted = false
					a.ourAccepted = false
					a.partnerAcceptedSnapshot = nil
					a.lastAllTradeItems = nil
					a.mu.Unlock()
					a.logDebug("fallback persist (outgoing accept only) succeeded: dbSessionID=%d", dbSessionID)
					a.emitUpdate()
				}
			}
		}
		return
	}
}

func loadDBConfig() (*DBConfig, error) {
	searchDirs := []string{}
	if cwd, err := os.Getwd(); err == nil {
		searchDirs = append(searchDirs, cwd)
	}
	if exePath, err := os.Executable(); err == nil {
		searchDirs = append(searchDirs, filepath.Dir(exePath))
	}

	seenDirs := map[string]struct{}{}
	candidates := []string{}

	for _, dir := range searchDirs {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			continue
		}

		for {
			if _, ok := seenDirs[abs]; !ok {
				seenDirs[abs] = struct{}{}
				candidates = append(candidates, filepath.Join(abs, "db.local.json"))
			}
			parent := filepath.Dir(abs)
			if parent == abs {
				break
			}
			abs = parent
		}
	}

	// Legacy app-local fallback path.
	candidates = append(candidates, filepath.Join("trade-tracker", "db.local.json"))

	for _, candidate := range candidates {
		data, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}

		var cfg DBConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", candidate, err)
		}
		if strings.TrimSpace(cfg.DatabaseURL) == "" {
			return nil, fmt.Errorf("databaseUrl is empty in %s", candidate)
		}
		return &cfg, nil
	}

	return nil, fmt.Errorf("db.local.json not found in cwd/exe parent paths")
}

func (a *App) initDatabase() {
	cfg, err := loadDBConfig()
	if err != nil {
		log.Printf("[DB] config not loaded: %v", err)
		a.logDebug("db init failed at config load: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Printf("[DB] connection setup failed: %v", err)
		a.logDebug("db init failed at pool creation: %v", err)
		return
	}

	if err := db.Ping(ctx); err != nil {
		log.Printf("[DB] ping failed: %v", err)
		a.logDebug("db init failed at ping: %v", err)
		db.Close()
		return
	}

	// Determine owner key: env -> config -> hostname fallback
	owner := strings.TrimSpace(os.Getenv("TRADE_TRACKER_OWNER_KEY"))
	if owner == "" {
		owner = strings.TrimSpace(cfg.OwnerKey)
	}
	if owner == "" {
		if h, err := os.Hostname(); err == nil {
			owner = h
		} else {
			owner = "local"
		}
	}

	a.mu.Lock()
	a.db = db
	a.ownerKey = owner
	a.mu.Unlock()
	a.logDebug("db init connected: ownerKey=%q", owner)

	if err := a.ensureTables(); err != nil {
		log.Printf("[DB] migration failed: %v", err)
		a.logDebug("db init failed at ensureTables: %v", err)
		return
	}
	a.logDebug("db tables ensured")

	if err := a.loadSessionsFromDB(); err != nil {
		log.Printf("[DB] failed to load sessions: %v", err)
		a.logDebug("db session preload failed: %v", err)
	} else {
		a.logDebug("db session preload completed")
	}
}

func (a *App) persistTradeEntry(dbSessionID int64, partnerName string, partnerTradeID int, items []TradeItem, payload string) error {
	if a.db == nil {
		a.logDebug("persist aborted: db not initialized")
		return fmt.Errorf("db not initialized")
	}

	occurredAt := time.Now().UTC()
	itemsJSON, _ := json.Marshal(items)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	a.logDebug("persist insert attempt: dbSessionID=%d partner=%q tradeID=%d items=%d ownerKey=%q", dbSessionID, partnerName, partnerTradeID, len(items), a.ownerKey)

	tx, err := a.db.Begin(ctx)
	if err != nil {
		a.logDebug("persist begin tx failed: dbSessionID=%d err=%v", dbSessionID, err)
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var tradeEntryID int64
	if err := tx.QueryRow(ctx, `INSERT INTO trade_entries (session_id, occurred_at, partner_name, partner_trade_id, payload_hex, furni_items, owner_key) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		dbSessionID,
		occurredAt,
		partnerName,
		partnerTradeID,
		payload,
		itemsJSON,
		a.ownerKey,
	).Scan(&tradeEntryID); err != nil {
		a.logDebug("persist insert failed: dbSessionID=%d err=%v", dbSessionID, err)
		return err
	}

	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		qty := item.Quantity
		if qty <= 0 {
			qty = 1
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO trade_entry_items (trade_entry_id, item_name, quantity, raw_data, owner_key)
			VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (trade_entry_id, item_name)
			DO UPDATE SET quantity = EXCLUDED.quantity, raw_data = EXCLUDED.raw_data
		`, tradeEntryID, name, qty, item.Raw, a.ownerKey); err != nil {
			a.logDebug("persist item row failed: tradeEntryID=%d item=%q qty=%d err=%v", tradeEntryID, name, qty, err)
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		a.logDebug("persist commit failed: tradeEntryID=%d err=%v", tradeEntryID, err)
		return err
	}
	a.logDebug("persist insert ok: dbSessionID=%d", dbSessionID)

	// Append to in-memory session entries when possible
	a.mu.Lock()
	defer a.mu.Unlock()
	te := TradeEntry{
		Timestamp:      occurredAt.Format(time.RFC3339),
		PartnerName:    partnerName,
		PartnerTradeID: partnerTradeID,
		PayloadHex:     payload,
		FurniItems:     items,
	}
	if a.currentSession != nil && a.currentSession.DBID == dbSessionID {
		a.currentSession.Entries = append(a.currentSession.Entries, te)
		return nil
	}
	for i := range a.sessions {
		if a.sessions[i].DBID == dbSessionID {
			a.sessions[i].Entries = append(a.sessions[i].Entries, te)
			return nil
		}
	}

	return nil
}

func (a *App) ensureTables() error {
	a.mu.Lock()
	db := a.db
	a.mu.Unlock()

	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	createQueries := []string{
		`CREATE TABLE IF NOT EXISTS trade_sessions (
			id BIGSERIAL PRIMARY KEY,
			started_at TIMESTAMPTZ NOT NULL,
			ended_at TIMESTAMPTZ NULL,
			owner_key TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS trade_entries (
			id BIGSERIAL PRIMARY KEY,
			session_id BIGINT NOT NULL REFERENCES trade_sessions(id) ON DELETE CASCADE,
			occurred_at TIMESTAMPTZ NOT NULL,
			partner_name TEXT NOT NULL,
			partner_trade_id INTEGER NOT NULL,
			payload_hex TEXT NOT NULL,
			furni_items JSONB DEFAULT '[]'::jsonb,
			owner_key TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS trade_entry_items (
			id BIGSERIAL PRIMARY KEY,
			trade_entry_id BIGINT NOT NULL REFERENCES trade_entries(id) ON DELETE CASCADE,
			item_name TEXT NOT NULL,
			quantity INTEGER NOT NULL DEFAULT 1,
			raw_data TEXT NOT NULL DEFAULT '',
			owner_key TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (trade_entry_id, item_name)
		)`,
	}

	for _, q := range createQueries {
		if _, err := db.Exec(ctx, q); err != nil {
			return err
		}
	}

	// Ensure older tables get new columns if they existed before this version
	alterQueries := []string{
		`ALTER TABLE trade_sessions ADD COLUMN IF NOT EXISTS owner_key TEXT DEFAULT ''`,
		`ALTER TABLE trade_entries ADD COLUMN IF NOT EXISTS owner_key TEXT DEFAULT ''`,
		`ALTER TABLE trade_entries ADD COLUMN IF NOT EXISTS furni_items JSONB DEFAULT '[]'::jsonb`,
		`ALTER TABLE trade_entry_items ADD COLUMN IF NOT EXISTS quantity INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE trade_entry_items ADD COLUMN IF NOT EXISTS raw_data TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE trade_entry_items ADD COLUMN IF NOT EXISTS owner_key TEXT DEFAULT ''`,
	}
	for _, q := range alterQueries {
		if _, err := db.Exec(ctx, q); err != nil {
			return err
		}
	}

	indexQueries := []string{
		`CREATE INDEX IF NOT EXISTS idx_trade_entries_session_id ON trade_entries(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_trade_entries_occurred_at ON trade_entries(occurred_at)`,
		`CREATE INDEX IF NOT EXISTS idx_trade_sessions_owner_key ON trade_sessions(owner_key)`,
		`CREATE INDEX IF NOT EXISTS idx_trade_entries_owner_key ON trade_entries(owner_key)`,
		`CREATE INDEX IF NOT EXISTS idx_trade_entry_items_entry_id ON trade_entry_items(trade_entry_id)`,
		`CREATE INDEX IF NOT EXISTS idx_trade_entry_items_owner_key ON trade_entry_items(owner_key)`,
		`CREATE INDEX IF NOT EXISTS idx_trade_entry_items_name ON trade_entry_items(item_name)`,
	}
	for _, q := range indexQueries {
		if _, err := db.Exec(ctx, q); err != nil {
			return err
		}
	}

	// Backfill normalized rows from existing JSON payloads.
	if _, err := db.Exec(ctx, `
		INSERT INTO trade_entry_items (trade_entry_id, item_name, quantity, raw_data, owner_key)
		SELECT
			e.id,
			COALESCE(elem->>'name', ''),
			GREATEST(COALESCE((elem->>'quantity')::int, 1), 1),
			COALESCE(elem->>'raw', ''),
			e.owner_key
		FROM trade_entries e
		CROSS JOIN LATERAL jsonb_array_elements(COALESCE(e.furni_items, '[]'::jsonb)) elem
		WHERE COALESCE(elem->>'name', '') <> ''
		ON CONFLICT (trade_entry_id, item_name) DO NOTHING
	`); err != nil {
		return err
	}

	return nil
}

func (a *App) loadSessionsFromDB() error {
	a.mu.Lock()
	db := a.db
	a.mu.Unlock()

	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	rows, err := db.Query(ctx, `
		SELECT
			s.id,
			s.started_at,
			s.ended_at,
			e.occurred_at,
			e.partner_name,
			e.partner_trade_id,
			e.payload_hex,
			e.furni_items
		FROM trade_sessions s
		LEFT JOIN trade_entries e ON e.session_id = s.id
		WHERE s.owner_key = $1
		ORDER BY s.id ASC, e.occurred_at ASC, e.id ASC
	`, a.ownerKey)
	if err != nil {
		return err
	}
	defer rows.Close()

	type sessionRow struct {
		sessionID      int64
		startedAt      time.Time
		endedAt        *time.Time
		entryOccurred  *time.Time
		partnerName    *string
		partnerTradeID *int
		payloadHex     *string
		furniJSON      *string
	}

	orderedIDs := make([]int64, 0)
	sessionsByID := make(map[int64]*TradeSession)
	maxSessionID := 0

	for rows.Next() {
		var r sessionRow
		if err := rows.Scan(
			&r.sessionID,
			&r.startedAt,
			&r.endedAt,
			&r.entryOccurred,
			&r.partnerName,
			&r.partnerTradeID,
			&r.payloadHex,
			&r.furniJSON,
		); err != nil {
			return err
		}

		s, exists := sessionsByID[r.sessionID]
		if !exists {
			s = &TradeSession{
				ID:        int(r.sessionID),
				StartedAt: r.startedAt.UTC().Format(time.RFC3339),
				Entries:   []TradeEntry{},
				DBID:      r.sessionID,
			}
			if r.endedAt != nil {
				s.EndedAt = r.endedAt.UTC().Format(time.RFC3339)
			}
			sessionsByID[r.sessionID] = s
			orderedIDs = append(orderedIDs, r.sessionID)
			if int(r.sessionID) > maxSessionID {
				maxSessionID = int(r.sessionID)
			}
		}

		if r.entryOccurred != nil && r.partnerName != nil && r.partnerTradeID != nil && r.payloadHex != nil {
			te := TradeEntry{
				Timestamp:      r.entryOccurred.UTC().Format(time.RFC3339),
				PartnerName:    *r.partnerName,
				PartnerTradeID: *r.partnerTradeID,
				PayloadHex:     *r.payloadHex,
			}
			if r.furniJSON != nil && strings.TrimSpace(*r.furniJSON) != "" {
				var items []TradeItem
				if err := json.Unmarshal([]byte(*r.furniJSON), &items); err == nil {
					te.FurniItems = items
				}
			}
			s.Entries = append(s.Entries, te)
		}
	}

	if err := rows.Err(); err != nil {
		return err
	}

	loaded := make([]TradeSession, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		loaded = append(loaded, *sessionsByID[id])
	}

	a.mu.Lock()
	a.sessions = loaded
	if a.nextSessionID < maxSessionID {
		a.nextSessionID = maxSessionID
	}
	a.mu.Unlock()

	a.emitUpdate()
	return nil
}

func (a *App) handleUsersPacket(e *g.Intercept) {
	if e == nil || e.Packet == nil || len(e.Packet.Data) == 0 {
		return
	}
	a.logDebug("USERS packet received: %d bytes", len(e.Packet.Data))

	users, err := runUsers28PythonParser(e.Packet.Data)
	if err != nil {
		log.Printf("[USERS28] parser failed: %v", err)
		return
	}

	a.mu.Lock()
	for _, u := range users {
		name := normalizeUsername(strings.TrimSpace(u.Username))
		if u.TradeID > 0 && name != "" {
			a.usersByTradeID[u.TradeID] = name
		}
	}
	// If we have an active trade partner still marked Unknown, try to resolve now
	if strings.HasPrefix(a.currentTradePartnerName, "Unknown") && a.currentTradePartnerID > 0 {
		if name, ok := a.usersByTradeID[a.currentTradePartnerID]; ok && name != "" {
			a.currentTradePartnerName = name
			a.logDebug("late-resolved partner name from USERS packet: tradeID=%d name=%q", a.currentTradePartnerID, name)
		}
	}
	a.mu.Unlock()
}

func runUsers28PythonParser(packetData []byte) ([]ParsedUsers28User, error) {
	// Resolve relative to the executable so the script is always found
	// regardless of working directory (e.g. when launched by app-launcher).
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)

	scriptCandidates := []string{
		// Primary: exe is at trade-tracker/build/bin/ → 3 levels up = workspace root
		filepath.Join(exeDir, "..", "..", "..", "scripts", "parse_users28.py"),
		// Fallbacks for go run / dev workflows
		filepath.Join("..", "scripts", "parse_users28.py"),
		filepath.Join("scripts", "parse_users28.py"),
	}

	scriptPath := ""
	for _, c := range scriptCandidates {
		abs, _ := filepath.Abs(c)
		if _, err := os.Stat(abs); err == nil {
			scriptPath = abs
			break
		}
	}
	if scriptPath == "" {
		attempted := make([]string, len(scriptCandidates))
		for i, c := range scriptCandidates {
			attempted[i], _ = filepath.Abs(c)
		}
		return nil, fmt.Errorf("parse_users28.py not found; tried: %v", attempted)
	}
	log.Printf("[USERS28] using script: %s", scriptPath)

	pythonExec := ""
	if p, err := exec.LookPath("python3"); err == nil {
		pythonExec = p
	} else if p, err := exec.LookPath("python"); err == nil {
		pythonExec = p
	} else {
		return nil, fmt.Errorf("python not found in PATH")
	}

	tmpFile, err := os.CreateTemp("", "users28_*.bin")
	if err != nil {
		return nil, err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(packetData); err != nil {
		tmpFile.Close()
		return nil, err
	}
	if err := tmpFile.Close(); err != nil {
		return nil, err
	}

	cmd := exec.Command(pythonExec, scriptPath, "--input", tmpPath, "--json")
	cmd.Dir = "."
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("python parser failed: %w stderr=%s", err, strings.TrimSpace(stderr.String()))
	}

	var users []ParsedUsers28User
	if err := json.Unmarshal(stdout.Bytes(), &users); err != nil {
		return nil, fmt.Errorf("failed to decode parser json: %w", err)
	}

	return users, nil
}

func setupExt(a *App) {
	ext.Initialized(func(e g.InitArgs) {
		log.Printf("initialized (connected=%t)", e.Connected)
	})

	ext.Activated(func() {
		log.Printf("activated")
		if a.ctx != nil {
			runtime.WindowShow(a.ctx)
		}
	})

	ext.Connected(func(e g.ConnectArgs) {
		log.Printf("connected (%s:%d)", e.Host, e.Port)
		go func(host string) {
			if err := loadExternalTexts(host); err != nil {
				log.Printf("[TRADE_TRACKER_DEBUG] external_texts load failed: host=%s err=%v", host, err)
			}
		}(e.Host)
		a.mu.Lock()
		a.connected = true
		a.mu.Unlock()
		a.emitUpdate()
	})

	ext.Disconnected(func() {
		log.Printf("disconnected")
		a.mu.Lock()
		a.connected = false
		a.mu.Unlock()
		a.emitUpdate()
	})

	ext.Intercept(in.USERS, in.SPACENODEUSERS).With(a.handleUsersPacket)
	ext.InterceptAll(a.handleIncomingTradeOpen)
}

func main() {
	app := NewApp()
	setupExt(app)

	err := wails.Run(&options.App{
		Title:             "Trade Tracker",
		Width:             640,
		Height:            520,
		MinWidth:          620,
		MinHeight:         460,
		StartHidden:       true,
		HideWindowOnClose: true,
		OnStartup:         app.startup,
		OnShutdown:        app.shutdown,
		Bind:              []interface{}{app},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 19, G: 26, B: 34, A: 1},
	})

	if err != nil {
		log.Fatal(err)
	}
}
