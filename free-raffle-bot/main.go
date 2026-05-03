package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
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
	in "xabbo.b7c.io/goearth/shockwave/in"
)

//go:embed all:frontend/dist
var assets embed.FS

var ext = g.NewExt(g.ExtInfo{
	Title:       "Free Raffle Bot",
	Description: "Tracks eligible raffle entries from completed Neon bets",
	Version:     "1.0.0",
	Author:      "Dubbo",
})

type DBConfig struct {
	DatabaseURL string `json:"databaseUrl"`
	OwnerKey    string `json:"ownerKey,omitempty"`
}

type RaffleParticipant struct {
	Username    string `json:"username"`
	BetCount    int    `json:"betCount"`
	Tickets     int    `json:"tickets"`
	FirstBet    string `json:"firstBet"`
	LastBet     string `json:"lastBet"`
	UsernameKey string `json:"-"`
}

type RaffleSession struct {
	ID             int                 `json:"id"`
	StartedAt      string              `json:"startedAt"`
	ScheduledEndAt string              `json:"scheduledEndAt,omitempty"`
	EndedAt        string              `json:"endedAt,omitempty"`
	BonusEvery     int                 `json:"bonusEvery"`
	Participants   []RaffleParticipant `json:"participants"`
	DBID           int64               `json:"-"`
	CursorAt       time.Time           `json:"-"`
	CursorEntry    string              `json:"-"`
}

type RaffleState struct {
	Connected      bool            `json:"connected"`
	InRoom         bool            `json:"inRoom"`
	Enabled        bool            `json:"enabled"`
	BonusEvery     int             `json:"bonusEvery"`
	CurrentSession *RaffleSession  `json:"currentSession,omitempty"`
	Sessions       []RaffleSession `json:"sessions"`
}

type App struct {
	ctx context.Context

	mu sync.Mutex

	connected      bool
	inRoom         bool
	enabled        bool
	bonusEvery     int
	nextSessionID  int
	currentSession *RaffleSession
	sessions       []RaffleSession
	db             *pgxpool.Pool
	ownerKey       string
	pollCancel     context.CancelFunc
}

func NewApp() *App {
	return &App{bonusEvery: 5}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.initDatabase()
	a.startPoller()
	go a.runExt()
}

func (a *App) shutdown(context.Context) {
	a.mu.Lock()
	cancel := a.pollCancel
	a.pollCancel = nil
	db := a.db
	a.db = nil
	a.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if db != nil {
		db.Close()
	}
}

func (a *App) runExt() {
	ext.Run()
}

func (a *App) logDebug(format string, args ...interface{}) {
	log.Printf("[FREE_RAFFLE_DEBUG] "+format, args...)
}

func normalizeUsername(raw string) string {
	return strings.TrimSpace(raw)
}

func normalizeUsernameKey(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func ticketsForBetCount(bets int, bonusEvery int) int {
	if bets <= 0 {
		return 0
	}
	if bonusEvery <= 0 {
		bonusEvery = 5
	}
	return 1 + (bets / bonusEvery)
}

func copySession(s *RaffleSession) *RaffleSession {
	if s == nil {
		return nil
	}
	out := &RaffleSession{
		ID:             s.ID,
		StartedAt:      s.StartedAt,
		ScheduledEndAt: s.ScheduledEndAt,
		EndedAt:        s.EndedAt,
		BonusEvery:     s.BonusEvery,
		DBID:           s.DBID,
		CursorAt:       s.CursorAt,
		CursorEntry:    s.CursorEntry,
		Participants:   make([]RaffleParticipant, len(s.Participants)),
	}
	copy(out.Participants, s.Participants)
	return out
}

func (a *App) GetState() RaffleState {
	a.mu.Lock()
	defer a.mu.Unlock()

	state := RaffleState{
		Connected:  a.connected,
		InRoom:     a.inRoom,
		Enabled:    a.enabled,
		BonusEvery: a.bonusEvery,
		Sessions:   make([]RaffleSession, len(a.sessions)),
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
	runtime.EventsEmit(a.ctx, "raffleStateUpdate", a.GetState())
}

func (a *App) SetEnabled(v bool) RaffleState {
	a.mu.Lock()
	a.enabled = v
	a.mu.Unlock()
	a.emitUpdate()
	return a.GetState()
}

func (a *App) SetBonusEvery(v int) RaffleState {
	if v <= 0 {
		v = 5
	}
	a.mu.Lock()
	a.bonusEvery = v
	a.mu.Unlock()
	a.emitUpdate()
	return a.GetState()
}

func parseOptionalRFC3339(raw string) (time.Time, bool, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return time.Time{}, false, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, false, err
	}
	return t.UTC(), true, nil
}

func (a *App) StartRaffle() RaffleState {
	state, err := a.StartRaffleWithWindow("", "")
	if err != nil {
		a.logDebug("StartRaffle fallback failed: %v", err)
	}
	return state
}

func (a *App) StartRaffleWithWindow(startAtRFC3339 string, endAtRFC3339 string) (RaffleState, error) {
	now := time.Now().UTC()
	startAt, hasStart, err := parseOptionalRFC3339(startAtRFC3339)
	if err != nil {
		return a.GetState(), fmt.Errorf("invalid start datetime: %w", err)
	}
	if !hasStart {
		startAt = now
	}

	endAt, hasEnd, err := parseOptionalRFC3339(endAtRFC3339)
	if err != nil {
		return a.GetState(), fmt.Errorf("invalid end datetime: %w", err)
	}
	if hasEnd && !endAt.After(startAt) {
		return a.GetState(), fmt.Errorf("end datetime must be later than start datetime")
	}

	var db *pgxpool.Pool
	var owner string
	var sessionID int
	var startedAt string
	var scheduledEndAt string
	var bonusEvery int

	a.mu.Lock()
	if a.currentSession == nil {
		a.nextSessionID++
		sessionID = a.nextSessionID
		startedAt = startAt.Format(time.RFC3339)
		if hasEnd {
			scheduledEndAt = endAt.Format(time.RFC3339)
		}
		bonusEvery = a.bonusEvery
		if bonusEvery <= 0 {
			bonusEvery = 5
		}
		a.currentSession = &RaffleSession{
			ID:             sessionID,
			StartedAt:      startedAt,
			ScheduledEndAt: scheduledEndAt,
			BonusEvery:     bonusEvery,
			Participants:   []RaffleParticipant{},
			CursorAt:       startAt,
		}
	} else {
		a.mu.Unlock()
		return a.GetState(), fmt.Errorf("a raffle session is already active")
	}
	db = a.db
	owner = a.ownerKey
	a.enabled = true
	a.mu.Unlock()

	if sessionID > 0 && db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()

		var dbSessionID int64
		err := db.QueryRow(ctx,
			`INSERT INTO raffle_sessions (started_at, scheduled_end_at, owner_key, bonus_every, last_seen_created_at, last_seen_entry_id)
			 VALUES ($1,$2,$3,$4,$5,$6)
			 RETURNING id`,
			startAt,
			func() interface{} {
				if hasEnd {
					return endAt
				}
				return nil
			}(),
			owner,
			bonusEvery,
			startAt,
			"",
		).Scan(&dbSessionID)
		if err != nil {
			a.logDebug("start session insert failed: %v", err)
			return a.GetState(), err
		} else {
			a.mu.Lock()
			if a.currentSession != nil && a.currentSession.ID == sessionID {
				a.currentSession.DBID = dbSessionID
			}
			a.mu.Unlock()
		}
	}

	a.emitUpdate()
	return a.GetState(), nil
}

func (a *App) StopRaffle() RaffleState {
	now := time.Now().UTC()
	var stopped *RaffleSession
	var db *pgxpool.Pool
	var owner string

	a.mu.Lock()
	if a.currentSession != nil {
		a.currentSession.EndedAt = now.Format(time.RFC3339)
		stopped = copySession(a.currentSession)
		a.sessions = append(a.sessions, *a.currentSession)
		a.currentSession = nil
	}
	db = a.db
	owner = a.ownerKey
	a.mu.Unlock()

	if stopped != nil && db != nil && stopped.DBID > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_, err := db.Exec(ctx,
			`UPDATE raffle_sessions
			 SET ended_at = $1, last_seen_created_at = $2, last_seen_entry_id = $3
			 WHERE id = $4 AND owner_key = $5`,
			now,
			stopped.CursorAt,
			stopped.CursorEntry,
			stopped.DBID,
			owner,
		)
		if err != nil {
			a.logDebug("stop session update failed: %v", err)
		}
	}

	a.emitUpdate()
	return a.GetState()
}

func (a *App) ClearSessions() RaffleState {
	a.mu.Lock()
	a.sessions = nil
	if a.currentSession != nil {
		a.currentSession.Participants = nil
	}
	a.mu.Unlock()
	a.emitUpdate()
	return a.GetState()
}

func (a *App) startPoller() {
	a.mu.Lock()
	if a.pollCancel != nil {
		a.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.pollCancel = cancel
	a.mu.Unlock()

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			a.processNewBets()
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (a *App) processNewBets() {
	a.mu.Lock()
	if !a.enabled || a.currentSession == nil || a.db == nil {
		a.mu.Unlock()
		return
	}
	now := time.Now().UTC()
	startAt, err := time.Parse(time.RFC3339, a.currentSession.StartedAt)
	if err != nil {
		a.mu.Unlock()
		return
	}
	if now.Before(startAt.UTC()) {
		a.mu.Unlock()
		return
	}
	if strings.TrimSpace(a.currentSession.ScheduledEndAt) != "" {
		endAt, err := time.Parse(time.RFC3339, a.currentSession.ScheduledEndAt)
		if err == nil && (now.Equal(endAt.UTC()) || now.After(endAt.UTC())) {
			a.mu.Unlock()
			a.StopRaffle()
			return
		}
	}
	sessionDBID := a.currentSession.DBID
	if sessionDBID <= 0 {
		a.mu.Unlock()
		return
	}
	owner := a.ownerKey
	sessionStartedAt := startAt.UTC()
	cursorAt := a.currentSession.CursorAt
	cursorEntry := a.currentSession.CursorEntry
	db := a.db
	a.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	rows, err := db.Query(ctx, `
		SELECT e.id, e.player_name, e.created_at
		FROM game_history_entries e
		WHERE e.owner_key = $1
		  AND e.created_at >= $2
		  AND (e.created_at > $3 OR (e.created_at = $3 AND e.id > $4))
		  AND EXISTS (
			SELECT 1
			FROM game_history_items i
			WHERE i.owner_key = e.owner_key
			  AND i.entry_id = e.id
			  AND i.item_type = 'bet'
		  )
		ORDER BY e.created_at ASC, e.id ASC
		LIMIT 200
	`, owner, sessionStartedAt, cursorAt, cursorEntry)
	if err != nil {
		a.logDebug("poll query failed: %v", err)
		return
	}
	defer rows.Close()

	type betRow struct {
		EntryID   string
		Player    string
		CreatedAt time.Time
	}
	batch := make([]betRow, 0)
	for rows.Next() {
		var r betRow
		if err := rows.Scan(&r.EntryID, &r.Player, &r.CreatedAt); err != nil {
			a.logDebug("poll scan failed: %v", err)
			return
		}
		batch = append(batch, r)
	}
	if err := rows.Err(); err != nil {
		a.logDebug("poll rows error: %v", err)
		return
	}
	if len(batch) == 0 {
		return
	}

	upserts := make([]RaffleParticipant, 0, len(batch))
	lastCursorAt := cursorAt
	lastCursorEntry := cursorEntry

	a.mu.Lock()
	if a.currentSession == nil || a.currentSession.DBID != sessionDBID {
		a.mu.Unlock()
		return
	}

	for _, row := range batch {
		name := normalizeUsername(row.Player)
		key := normalizeUsernameKey(name)
		if key == "" {
			lastCursorAt = row.CreatedAt.UTC()
			lastCursorEntry = row.EntryID
			continue
		}

		idx := -1
		for i := range a.currentSession.Participants {
			if a.currentSession.Participants[i].UsernameKey == key {
				idx = i
				break
			}
		}
		if idx == -1 {
			p := RaffleParticipant{
				Username:    name,
				UsernameKey: key,
				BetCount:    1,
				Tickets:     ticketsForBetCount(1, a.currentSession.BonusEvery),
				FirstBet:    row.CreatedAt.UTC().Format(time.RFC3339),
				LastBet:     row.CreatedAt.UTC().Format(time.RFC3339),
			}
			a.currentSession.Participants = append(a.currentSession.Participants, p)
			upserts = append(upserts, p)
		} else {
			p := &a.currentSession.Participants[idx]
			p.BetCount++
			p.Tickets = ticketsForBetCount(p.BetCount, a.currentSession.BonusEvery)
			p.LastBet = row.CreatedAt.UTC().Format(time.RFC3339)
			upserts = append(upserts, *p)
		}

		lastCursorAt = row.CreatedAt.UTC()
		lastCursorEntry = row.EntryID
	}

	sort.Slice(a.currentSession.Participants, func(i, j int) bool {
		if a.currentSession.Participants[i].Tickets != a.currentSession.Participants[j].Tickets {
			return a.currentSession.Participants[i].Tickets > a.currentSession.Participants[j].Tickets
		}
		return strings.ToLower(a.currentSession.Participants[i].Username) < strings.ToLower(a.currentSession.Participants[j].Username)
	})

	a.currentSession.CursorAt = lastCursorAt
	a.currentSession.CursorEntry = lastCursorEntry
	bonusEvery := a.currentSession.BonusEvery
	a.mu.Unlock()

	if len(upserts) > 0 {
		if err := a.persistParticipants(sessionDBID, upserts); err != nil {
			a.logDebug("participant upsert failed: %v", err)
		}
		a.logDebug("processed %d new bet rows (bonusEvery=%d)", len(batch), bonusEvery)
	}

	if err := a.persistSessionCursor(sessionDBID, lastCursorAt, lastCursorEntry); err != nil {
		a.logDebug("cursor persist failed: %v", err)
	}

	a.emitUpdate()
}

func (a *App) persistSessionCursor(sessionDBID int64, at time.Time, entryID string) error {
	a.mu.Lock()
	db := a.db
	owner := a.ownerKey
	a.mu.Unlock()
	if db == nil || sessionDBID <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := db.Exec(ctx,
		`UPDATE raffle_sessions
		 SET last_seen_created_at = $1, last_seen_entry_id = $2
		 WHERE id = $3 AND owner_key = $4`,
		at,
		entryID,
		sessionDBID,
		owner,
	)
	return err
}

func (a *App) persistParticipants(sessionDBID int64, participants []RaffleParticipant) error {
	a.mu.Lock()
	db := a.db
	owner := a.ownerKey
	a.mu.Unlock()
	if db == nil {
		return fmt.Errorf("db not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	for _, p := range participants {
		firstAt, _ := time.Parse(time.RFC3339, p.FirstBet)
		lastAt, _ := time.Parse(time.RFC3339, p.LastBet)
		if _, err := tx.Exec(ctx, `
			INSERT INTO raffle_participants (
				session_id, owner_key, username, username_key,
				bet_count, ticket_count, first_bet_at, last_bet_at, updated_at
			) VALUES (
				$1,$2,$3,$4,
				$5,$6,$7,$8,NOW()
			)
			ON CONFLICT (session_id, owner_key, username_key) DO UPDATE SET
				username = EXCLUDED.username,
				bet_count = EXCLUDED.bet_count,
				ticket_count = EXCLUDED.ticket_count,
				first_bet_at = LEAST(raffle_participants.first_bet_at, EXCLUDED.first_bet_at),
				last_bet_at = GREATEST(raffle_participants.last_bet_at, EXCLUDED.last_bet_at),
				updated_at = NOW()
		`, sessionDBID, owner, p.Username, p.UsernameKey, p.BetCount, p.Tickets, firstAt, lastAt); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func loadDBConfig() (*DBConfig, error) {
	if envURL := strings.TrimSpace(os.Getenv("ROLL_ORIGINS_DB_URL")); envURL != "" {
		owner := strings.TrimSpace(os.Getenv("ROLL_ORIGINS_OWNER_KEY"))
		if owner == "" {
			owner = strings.TrimSpace(os.Getenv("TRADE_TRACKER_OWNER_KEY"))
		}
		return &DBConfig{DatabaseURL: envURL, OwnerKey: owner}, nil
	}

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
		a.logDebug("db config failed: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		a.logDebug("db pool create failed: %v", err)
		return
	}
	if err := db.Ping(ctx); err != nil {
		a.logDebug("db ping failed: %v", err)
		db.Close()
		return
	}

	owner := strings.TrimSpace(os.Getenv("ROLL_ORIGINS_OWNER_KEY"))
	if owner == "" {
		owner = strings.TrimSpace(os.Getenv("TRADE_TRACKER_OWNER_KEY"))
	}
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

	if err := a.ensureTables(); err != nil {
		a.logDebug("ensureTables failed: %v", err)
		return
	}
	if err := a.loadSessionsFromDB(); err != nil {
		a.logDebug("load sessions failed: %v", err)
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
		`CREATE TABLE IF NOT EXISTS raffle_sessions (
			id BIGSERIAL PRIMARY KEY,
			started_at TIMESTAMPTZ NOT NULL,
			scheduled_end_at TIMESTAMPTZ NULL,
			ended_at TIMESTAMPTZ NULL,
			owner_key TEXT NOT NULL DEFAULT '',
			bonus_every INTEGER NOT NULL DEFAULT 5,
			last_seen_created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_seen_entry_id TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS raffle_participants (
			id BIGSERIAL PRIMARY KEY,
			session_id BIGINT NOT NULL REFERENCES raffle_sessions(id) ON DELETE CASCADE,
			owner_key TEXT NOT NULL DEFAULT '',
			username TEXT NOT NULL,
			username_key TEXT NOT NULL,
			bet_count INTEGER NOT NULL DEFAULT 1,
			ticket_count INTEGER NOT NULL DEFAULT 1,
			first_bet_at TIMESTAMPTZ NOT NULL,
			last_bet_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (session_id, owner_key, username_key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_raffle_sessions_owner ON raffle_sessions(owner_key)`,
		`CREATE INDEX IF NOT EXISTS idx_raffle_sessions_started ON raffle_sessions(started_at)`,
		`CREATE INDEX IF NOT EXISTS idx_raffle_participants_session ON raffle_participants(session_id, owner_key)`,
		`CREATE INDEX IF NOT EXISTS idx_raffle_participants_tickets ON raffle_participants(session_id, ticket_count DESC)`,
	}

	for _, q := range queries {
		if _, err := db.Exec(ctx, q); err != nil {
			return err
		}
	}

	alterQueries := []string{
		`ALTER TABLE raffle_sessions ADD COLUMN IF NOT EXISTS scheduled_end_at TIMESTAMPTZ NULL`,
		`ALTER TABLE raffle_sessions ADD COLUMN IF NOT EXISTS bonus_every INTEGER NOT NULL DEFAULT 5`,
		`ALTER TABLE raffle_sessions ADD COLUMN IF NOT EXISTS last_seen_created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`,
		`ALTER TABLE raffle_sessions ADD COLUMN IF NOT EXISTS last_seen_entry_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE raffle_participants ADD COLUMN IF NOT EXISTS username_key TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE raffle_participants ADD COLUMN IF NOT EXISTS bet_count INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE raffle_participants ADD COLUMN IF NOT EXISTS ticket_count INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE raffle_participants ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`,
	}
	for _, q := range alterQueries {
		if _, err := db.Exec(ctx, q); err != nil {
			return err
		}
	}

	return nil
}

func (a *App) loadSessionsFromDB() error {
	a.mu.Lock()
	db := a.db
	owner := a.ownerKey
	a.mu.Unlock()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	sRows, err := db.Query(ctx, `
		SELECT id, started_at, scheduled_end_at, ended_at, bonus_every, last_seen_created_at, last_seen_entry_id
		FROM raffle_sessions
		WHERE owner_key = $1
		ORDER BY id ASC
	`, owner)
	if err != nil {
		return err
	}
	defer sRows.Close()

	type dbSession struct {
		id             int64
		startedAt      time.Time
		scheduledEndAt *time.Time
		endedAt        *time.Time
		bonusEvery     int
		cursorAt       time.Time
		cursorID       string
	}
	sessionsByID := map[int64]*RaffleSession{}
	orderedIDs := make([]int64, 0)
	maxID := 0
	var current *RaffleSession

	for sRows.Next() {
		var s dbSession
		if err := sRows.Scan(&s.id, &s.startedAt, &s.scheduledEndAt, &s.endedAt, &s.bonusEvery, &s.cursorAt, &s.cursorID); err != nil {
			return err
		}
		rs := &RaffleSession{
			ID:           int(s.id),
			StartedAt:    s.startedAt.UTC().Format(time.RFC3339),
			BonusEvery:   s.bonusEvery,
			Participants: []RaffleParticipant{},
			DBID:         s.id,
			CursorAt:     s.cursorAt.UTC(),
			CursorEntry:  s.cursorID,
		}
		if s.scheduledEndAt != nil {
			rs.ScheduledEndAt = s.scheduledEndAt.UTC().Format(time.RFC3339)
		}
		if rs.BonusEvery <= 0 {
			rs.BonusEvery = 5
		}
		if s.endedAt != nil {
			rs.EndedAt = s.endedAt.UTC().Format(time.RFC3339)
		}
		sessionsByID[s.id] = rs
		orderedIDs = append(orderedIDs, s.id)
		if int(s.id) > maxID {
			maxID = int(s.id)
		}
	}
	if err := sRows.Err(); err != nil {
		return err
	}

	pRows, err := db.Query(ctx, `
		SELECT session_id, username, username_key, bet_count, ticket_count, first_bet_at, last_bet_at
		FROM raffle_participants
		WHERE owner_key = $1
		ORDER BY session_id ASC, ticket_count DESC, username ASC
	`, owner)
	if err != nil {
		return err
	}
	defer pRows.Close()

	for pRows.Next() {
		var sessionID int64
		var p RaffleParticipant
		var firstAt time.Time
		var lastAt time.Time
		if err := pRows.Scan(&sessionID, &p.Username, &p.UsernameKey, &p.BetCount, &p.Tickets, &firstAt, &lastAt); err != nil {
			return err
		}
		p.FirstBet = firstAt.UTC().Format(time.RFC3339)
		p.LastBet = lastAt.UTC().Format(time.RFC3339)
		if s := sessionsByID[sessionID]; s != nil {
			s.Participants = append(s.Participants, p)
		}
	}
	if err := pRows.Err(); err != nil {
		return err
	}

	closed := make([]RaffleSession, 0)
	for _, id := range orderedIDs {
		s := sessionsByID[id]
		if s == nil {
			continue
		}
		if s.EndedAt == "" && current == nil {
			current = s
			continue
		}
		closed = append(closed, *s)
	}

	a.mu.Lock()
	a.sessions = closed
	a.currentSession = current
	if a.nextSessionID < maxID {
		a.nextSessionID = maxID
	}
	a.mu.Unlock()

	a.emitUpdate()
	return nil
}

func setupExt(a *App) {
	ext.Activated(func() {
		if a.ctx != nil {
			runtime.WindowShow(a.ctx)
		}
	})

	ext.Connected(func(g.ConnectArgs) {
		a.mu.Lock()
		a.connected = true
		a.mu.Unlock()
		a.emitUpdate()
	})

	ext.Disconnected(func() {
		a.mu.Lock()
		a.connected = false
		a.inRoom = false
		a.mu.Unlock()
		a.emitUpdate()
	})

	ext.Intercept(in.ROOM_READY).With(func(e *g.Intercept) {
		a.mu.Lock()
		a.inRoom = true
		a.mu.Unlock()
		a.emitUpdate()
	})
}

func main() {
	app := NewApp()
	setupExt(app)

	err := wails.Run(&options.App{
		Title:             "Free Raffle Bot",
		Width:             720,
		Height:            540,
		MinWidth:          640,
		MinHeight:         500,
		StartHidden:       false,
		HideWindowOnClose: true,
		OnStartup:         app.startup,
		OnShutdown:        app.shutdown,
		Bind:              []interface{}{app},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 18, G: 22, B: 28, A: 1},
	})

	if err != nil {
		log.Fatal(err)
	}
}
