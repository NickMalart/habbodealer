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
	in "xabbo.b7c.io/goearth/shockwave/in"
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
	Timestamp      string `json:"timestamp"`
	PartnerName    string `json:"partnerName"`
	PartnerTradeID int    `json:"partnerTradeId"`
	PayloadHex     string `json:"payloadHex"`
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
}

type DBConfig struct {
	DatabaseURL string `json:"databaseUrl"`
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
	}
	db := a.db
	a.mu.Unlock()

	if db != nil && sessionID > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()

		var dbSessionID int64
		err := db.QueryRow(
			ctx,
			`INSERT INTO trade_sessions (started_at) VALUES ($1) RETURNING id`,
			now,
		).Scan(&dbSessionID)
		if err != nil {
			log.Printf("[DB] failed to create trade session: %v", err)
		} else {
			a.mu.Lock()
			if a.currentSession != nil && a.currentSession.ID == sessionID {
				a.currentSession.DBID = dbSessionID
			}
			a.mu.Unlock()
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
		}
	}
	db := a.db
	a.mu.Unlock()

	if db != nil && dbSessionID > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()

		if _, err := db.Exec(
			ctx,
			`UPDATE trade_sessions SET ended_at = $1 WHERE id = $2`,
			now,
			dbSessionID,
		); err != nil {
			log.Printf("[DB] failed to close trade session %d: %v", dbSessionID, err)
		}
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

func (a *App) handleIncomingTradeOpen(e *g.Intercept) {
	if e == nil || e.Packet == nil {
		return
	}
	if e.Packet.Header.Dir != g.In || e.Packet.Header.Value != 104 {
		return
	}

	tradeID, _ := decodeLeadingVL64(e.Packet.Data)

	a.mu.Lock()
	if !a.running || a.currentSession == nil {
		a.mu.Unlock()
		return
	}

	partnerName := "Unknown"
	if tradeID > 0 {
		if name, ok := a.usersByTradeID[tradeID]; ok && strings.TrimSpace(name) != "" {
			partnerName = name
		} else {
			partnerName = fmt.Sprintf("Unknown (#%d)", tradeID)
		}
	}

	entry := TradeEntry{
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
		PartnerName:    partnerName,
		PartnerTradeID: tradeID,
		PayloadHex:     fmt.Sprintf("% X", e.Packet.Data),
	}
	a.currentSession.Entries = append(a.currentSession.Entries, entry)
	dbSessionID := a.currentSession.DBID
	db := a.db
	a.mu.Unlock()

	if db != nil && dbSessionID > 0 {
		occurredAt, err := time.Parse(time.RFC3339, entry.Timestamp)
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			_, err = db.Exec(
				ctx,
				`INSERT INTO trade_entries (session_id, occurred_at, partner_name, partner_trade_id, payload_hex)
				 VALUES ($1, $2, $3, $4, $5)`,
				dbSessionID,
				occurredAt,
				entry.PartnerName,
				entry.PartnerTradeID,
				entry.PayloadHex,
			)
			cancel()
			if err != nil {
				log.Printf("[DB] failed to persist trade entry: %v", err)
			}
		} else {
			log.Printf("[DB] failed to parse entry timestamp %q: %v", entry.Timestamp, err)
		}
	}

	a.emitUpdate()
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
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Printf("[DB] connection setup failed: %v", err)
		return
	}

	if err := db.Ping(ctx); err != nil {
		log.Printf("[DB] ping failed: %v", err)
		db.Close()
		return
	}

	a.mu.Lock()
	a.db = db
	a.mu.Unlock()

	if err := a.ensureTables(); err != nil {
		log.Printf("[DB] migration failed: %v", err)
		return
	}

	if err := a.loadSessionsFromDB(); err != nil {
		log.Printf("[DB] failed to load sessions: %v", err)
	}
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

	queries := []string{
		`CREATE TABLE IF NOT EXISTS trade_sessions (
			id BIGSERIAL PRIMARY KEY,
			started_at TIMESTAMPTZ NOT NULL,
			ended_at TIMESTAMPTZ NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS trade_entries (
			id BIGSERIAL PRIMARY KEY,
			session_id BIGINT NOT NULL REFERENCES trade_sessions(id) ON DELETE CASCADE,
			occurred_at TIMESTAMPTZ NOT NULL,
			partner_name TEXT NOT NULL,
			partner_trade_id INTEGER NOT NULL,
			payload_hex TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_trade_entries_session_id ON trade_entries(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_trade_entries_occurred_at ON trade_entries(occurred_at)`,
	}

	for _, q := range queries {
		if _, err := db.Exec(ctx, q); err != nil {
			return err
		}
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
			e.payload_hex
		FROM trade_sessions s
		LEFT JOIN trade_entries e ON e.session_id = s.id
		ORDER BY s.id ASC, e.occurred_at ASC, e.id ASC
	`)
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
			s.Entries = append(s.Entries, TradeEntry{
				Timestamp:      r.entryOccurred.UTC().Format(time.RFC3339),
				PartnerName:    *r.partnerName,
				PartnerTradeID: *r.partnerTradeID,
				PayloadHex:     *r.payloadHex,
			})
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

	users, err := runUsers28PythonParser(e.Packet.Data)
	if err != nil {
		log.Printf("[USERS28] parser failed: %v", err)
		return
	}

	resolved := make(map[int]string)
	for _, u := range users {
		name := strings.TrimSpace(u.Username)
		if u.TradeID > 0 && name != "" {
			resolved[u.TradeID] = name
		}
	}
	if len(resolved) == 0 {
		return
	}

	a.mu.Lock()
	a.usersByTradeID = resolved
	a.mu.Unlock()
}

func runUsers28PythonParser(packetData []byte) ([]ParsedUsers28User, error) {
	scriptCandidates := []string{
		filepath.Join("..", "scripts", "parse_users28.py"),
		filepath.Join("scripts", "parse_users28.py"),
	}

	scriptPath := ""
	for _, c := range scriptCandidates {
		if _, err := os.Stat(c); err == nil {
			scriptPath = c
			break
		}
	}
	if scriptPath == "" {
		return nil, fmt.Errorf("parse_users28.py not found")
	}

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
