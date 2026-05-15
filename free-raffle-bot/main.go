package main

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"math/big"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
	out "xabbo.b7c.io/goearth/shockwave/out"
)

//go:embed all:frontend/dist
var assets embed.FS

var ext = g.NewExt(g.ExtInfo{
	Title:       "Free Raffle Bot",
	Description: "Tracks eligible raffle entries from completed Neon bets",
	Version:     "1.0.0",
	Author:      "Dubbo",
})

const hardcodedRaffleWebhookURL = "https://discordapp.com/api/webhooks/1499651607800057926/SLvv8HU_yG2vyW04MaXkZ2eVd_10qpddkJjBWDQ6GmB7LzUhJu7yZAgQInsg0eLOogJ9"

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
	ManualDelta int    `json:"-"`
	UsernameKey string `json:"-"`
}

type RaffleSession struct {
	ID                 int                 `json:"id"`
	StartedAt          string              `json:"startedAt"`
	ScheduledEndAt     string              `json:"scheduledEndAt,omitempty"`
	EndedAt            string              `json:"endedAt,omitempty"`
	RaffleName         string              `json:"raffleName,omitempty"`
	PrizeName          string              `json:"prizeName,omitempty"`
	PrizeQty           int                 `json:"prizeQty,omitempty"`
	BonusEvery         int                 `json:"bonusEvery"`
	WebhookMessageID   string              `json:"webhookMessageId,omitempty"`
	Participants       []RaffleParticipant `json:"participants"`
	WinnerName         string              `json:"winnerName,omitempty"`
	WinnerTickets      int                 `json:"winnerTickets,omitempty"`
	WinnerOdds         string              `json:"winnerOdds,omitempty"`
	WinnerDrawnAt      string              `json:"winnerDrawnAt,omitempty"`
	WinnerMethod       string              `json:"winnerMethod,omitempty"`
	WinnerSummary      string              `json:"winnerSummary,omitempty"`
	WinnerProofURL     string              `json:"winnerProofUrl,omitempty"`
	WinnerProofID      string              `json:"-"`
	WinnerProofFile    string              `json:"-"`
	HeroImageURL       string              `json:"-"`
	HeroAttachmentID   string              `json:"-"`
	HeroAttachmentFile string              `json:"-"`
	DBID               int64               `json:"-"`
	CursorAt           time.Time           `json:"-"`
	CursorEntry        string              `json:"-"`
	ResumedAt          time.Time           `json:"-"` // zero if never resumed; bets before this time skip shouts

	SponsorEnabled  bool   `json:"sponsorEnabled"`
	SponsorName     string `json:"sponsorName,omitempty"`
	SponsorRoomName string `json:"sponsorRoomName,omitempty"`
}

type RaffleSessionSummary struct {
	ID             int    `json:"id"`
	StartedAt      string `json:"startedAt"`
	ScheduledEndAt string `json:"scheduledEndAt,omitempty"`
	EndedAt        string `json:"endedAt,omitempty"`
	RaffleName     string `json:"raffleName,omitempty"`
	PrizeName      string `json:"prizeName,omitempty"`
	PrizeQty       int    `json:"prizeQty,omitempty"`
	WinnerName     string `json:"winnerName,omitempty"`
	DBID           int64  `json:"dbId"`
}

type RaffleState struct {
	Connected             bool                   `json:"connected"`
	InRoom                bool                   `json:"inRoom"`
	Enabled               bool                   `json:"enabled"`
	BonusEvery            int                    `json:"bonusEvery"`
	TicketAnnounceEnabled bool                   `json:"ticketAnnounceEnabled"`
	TicketProgressEnabled bool                   `json:"ticketProgressEnabled"`
	RaffleName            string                 `json:"raffleName"`
	RafflePrizeName       string                 `json:"rafflePrizeName"`
	RafflePrizeQty        int                    `json:"rafflePrizeQty"`
	RaffleHeroImageName   string                 `json:"raffleHeroImageName"`
	RaffleAutoUpdate      bool                   `json:"raffleAutoUpdate"`
	RaffleMessageID       string                 `json:"raffleMessageId"`
	HypeShoutEnabled      bool                   `json:"hypeShoutEnabled"`
	HypeShoutPhrase       string                 `json:"hypeShoutPhrase"`
	HypeShoutMinutes      int                    `json:"hypeShoutMinutes"`
	NextHypeShoutAt       string                 `json:"nextHypeShoutAt,omitempty"`
	AutoMsgEnabled        bool                   `json:"autoMsgEnabled"`
	AutoMsgPhrase         string                 `json:"autoMsgPhrase"`
	AutoMsgMinutes        int                    `json:"autoMsgMinutes"`
	NextAutoMsgAt         string                 `json:"nextAutoMsgAt,omitempty"`
	CurrentSession        *RaffleSession         `json:"currentSession,omitempty"`
	Sessions              []RaffleSessionSummary `json:"sessions"`

	SponsorEnabled  bool   `json:"sponsorEnabled"`
	SponsorName     string `json:"sponsorName"`
	SponsorRoomName string `json:"sponsorRoomName"`
}

type App struct {
	ctx context.Context

	mu        sync.Mutex
	processMu sync.Mutex
	debugMu   sync.Mutex

	connected             bool
	inRoom                bool
	enabled               bool
	bonusEvery            int
	ticketAnnounceEnabled bool
	ticketProgressEnabled bool
	nextSessionID         int
	currentSession        *RaffleSession
	sessions              []RaffleSession
	db                    *pgxpool.Pool
	ownerKey              string
	pollCancel            context.CancelFunc
	listenCancel          context.CancelFunc
	debugLines            []string
	lastNotifyAt          string
	lastNotifyRaw         string
	lastQueryRows         int
	lastShout             string

	raffleName               string
	rafflePrizeName          string
	rafflePrizeQty           int
	raffleHeroDataURL        string
	raffleHeroFileName       string
	raffleHeroImageURL       string
	raffleHeroAttachmentID   string
	raffleHeroAttachmentFile string
	raffleAutoUpdate         bool
	raffleMessageID          string

	hypeShoutEnabled  bool
	hypeShoutPhrase   string
	hypeShoutMinutes  int
	nextHypeShoutAt   time.Time
	hypeShoutStopChan chan struct{}
	hypeShoutMu       sync.Mutex

	autoMsgEnabled  bool
	autoMsgPhrase   string
	autoMsgMinutes  int
	nextAutoMsgAt   time.Time
	autoMsgStopChan chan struct{}
	autoMsgMu       sync.Mutex

	sponsorEnabled  bool
	sponsorName     string
	sponsorRoomName string

	pendingProofBytes    []byte
	pendingProofFileName string
}

func NewApp() *App {
	return &App{bonusEvery: 5, raffleAutoUpdate: true, raffleName: "Flame Raffle", rafflePrizeName: "Purple Dragon Lamp", rafflePrizeQty: 1}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.initDatabase()
	a.startPoller()
	a.startRealtimeWatcher()
	go a.runExt()
}

func (a *App) shutdown(context.Context) {
	a.mu.Lock()
	pollCancel := a.pollCancel
	a.pollCancel = nil
	listenCancel := a.listenCancel
	a.listenCancel = nil
	db := a.db
	a.db = nil
	a.mu.Unlock()

	if pollCancel != nil {
		pollCancel()
	}
	if listenCancel != nil {
		listenCancel()
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
	msg := fmt.Sprintf(format, args...)
	a.debugMu.Lock()
	line := fmt.Sprintf("%s %s", time.Now().UTC().Format(time.RFC3339), msg)
	a.debugLines = append(a.debugLines, line)
	if len(a.debugLines) > 300 {
		a.debugLines = a.debugLines[len(a.debugLines)-300:]
	}
	a.debugMu.Unlock()
	a.emitDebugUpdate()
}

func (a *App) emitDebugUpdate() {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "raffleDebugUpdate", a.GetDebugSnapshot())
}

func (a *App) GetDebugSnapshot() string {
	a.debugMu.Lock()
	defer a.debugMu.Unlock()

	lines := make([]string, 0, len(a.debugLines)+8)
	lines = append(lines, "=== Free Raffle Debug Snapshot ===")
	lines = append(lines, fmt.Sprintf("time_utc: %s", time.Now().UTC().Format(time.RFC3339)))
	lines = append(lines, fmt.Sprintf("last_notify_at: %s", strings.TrimSpace(a.lastNotifyAt)))
	lines = append(lines, fmt.Sprintf("last_notify_payload: %s", strings.TrimSpace(a.lastNotifyRaw)))
	lines = append(lines, fmt.Sprintf("last_query_rows: %d", a.lastQueryRows))
	lines = append(lines, fmt.Sprintf("last_shout: %s", strings.TrimSpace(a.lastShout)))
	a.mu.Lock()
	lines = append(lines, fmt.Sprintf("connected: %t  inRoom: %t  ticketAnnounce: %t  ticketProgress: %t", a.connected, a.inRoom, a.ticketAnnounceEnabled, a.ticketProgressEnabled))
	a.mu.Unlock()
	lines = append(lines, "")
	lines = append(lines, "--- Recent Logs ---")
	lines = append(lines, a.debugLines...)
	return strings.Join(lines, "\n")
}

func (a *App) ClearDebugSnapshot() {
	a.debugMu.Lock()
	a.debugLines = nil
	a.lastNotifyAt = ""
	a.lastNotifyRaw = ""
	a.lastQueryRows = 0
	a.lastShout = ""
	a.debugMu.Unlock()
	a.emitDebugUpdate()
}

func normalizeUsername(raw string) string {
	return strings.TrimSpace(raw)
}

func normalizeUsernameKey(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func isAfterCursor(eventAt time.Time, entryID string, cursorAt time.Time, cursorEntry string) bool {
	if eventAt.After(cursorAt) {
		return true
	}
	if eventAt.Before(cursorAt) {
		return false
	}
	if strings.TrimSpace(cursorEntry) == "" {
		return true
	}

	entryNum, errEntry := strconv.ParseInt(strings.TrimSpace(entryID), 10, 64)
	cursorNum, errCursor := strconv.ParseInt(strings.TrimSpace(cursorEntry), 10, 64)
	if errEntry == nil && errCursor == nil {
		return entryNum > cursorNum
	}

	return entryID > cursorEntry
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

func effectiveTicketsForParticipant(betCount int, bonusEvery int, manualDelta int) int {
	tickets := ticketsForBetCount(betCount, bonusEvery) + manualDelta
	if tickets < 0 {
		return 0
	}
	return tickets
}

func sortParticipants(participants []RaffleParticipant) {
	sort.Slice(participants, func(i, j int) bool {
		if participants[i].Tickets != participants[j].Tickets {
			return participants[i].Tickets > participants[j].Tickets
		}
		return strings.ToLower(participants[i].Username) < strings.ToLower(participants[j].Username)
	})
}

func copySession(s *RaffleSession) *RaffleSession {
	if s == nil {
		return nil
	}
	out := &RaffleSession{
		ID:                 s.ID,
		StartedAt:          s.StartedAt,
		ScheduledEndAt:     s.ScheduledEndAt,
		EndedAt:            s.EndedAt,
		RaffleName:         s.RaffleName,
		PrizeName:          s.PrizeName,
		PrizeQty:           s.PrizeQty,
		BonusEvery:         s.BonusEvery,
		WebhookMessageID:   s.WebhookMessageID,
		WinnerName:         s.WinnerName,
		WinnerTickets:      s.WinnerTickets,
		WinnerOdds:         s.WinnerOdds,
		WinnerDrawnAt:      s.WinnerDrawnAt,
		WinnerMethod:       s.WinnerMethod,
		WinnerSummary:      s.WinnerSummary,
		WinnerProofURL:     s.WinnerProofURL,
		WinnerProofID:      s.WinnerProofID,
		WinnerProofFile:    s.WinnerProofFile,
		HeroImageURL:       s.HeroImageURL,
		HeroAttachmentID:   s.HeroAttachmentID,
		HeroAttachmentFile: s.HeroAttachmentFile,
		DBID:               s.DBID,
		CursorAt:           s.CursorAt,
		CursorEntry:        s.CursorEntry,
		SponsorEnabled:     s.SponsorEnabled,
		SponsorName:        s.SponsorName,
		SponsorRoomName:    s.SponsorRoomName,
		Participants:       make([]RaffleParticipant, len(s.Participants)),
	}
	copy(out.Participants, s.Participants)
	return out
}

func (a *App) GetState() RaffleState {
	a.mu.Lock()
	defer a.mu.Unlock()

	state := RaffleState{
		Connected:             a.connected,
		InRoom:                a.inRoom,
		Enabled:               a.enabled,
		BonusEvery:            a.bonusEvery,
		TicketAnnounceEnabled: a.ticketAnnounceEnabled,
		TicketProgressEnabled: a.ticketProgressEnabled,
		RaffleName:            a.raffleName,
		RafflePrizeName:       a.rafflePrizeName,
		RafflePrizeQty:        a.rafflePrizeQty,
		RaffleHeroImageName:   a.raffleHeroFileName,
		RaffleAutoUpdate:      a.raffleAutoUpdate,
		RaffleMessageID:       a.raffleMessageID,
		HypeShoutEnabled:      a.hypeShoutEnabled,
		HypeShoutPhrase:       a.hypeShoutPhrase,
		HypeShoutMinutes:      a.hypeShoutMinutes,
		NextHypeShoutAt: func() string {
			a.hypeShoutMu.Lock()
			defer a.hypeShoutMu.Unlock()
			if a.nextHypeShoutAt.IsZero() {
				return ""
			}
			return a.nextHypeShoutAt.Format(time.RFC3339)
		}(),
		AutoMsgEnabled: a.autoMsgEnabled,
		AutoMsgPhrase:  a.autoMsgPhrase,
		AutoMsgMinutes: a.autoMsgMinutes,
		NextAutoMsgAt: func() string {
			a.autoMsgMu.Lock()
			defer a.autoMsgMu.Unlock()
			if a.nextAutoMsgAt.IsZero() {
				return ""
			}
			return a.nextAutoMsgAt.Format(time.RFC3339)
		}(),
		SponsorEnabled: a.sponsorEnabled,
		SponsorName:           a.sponsorName,
		SponsorRoomName:       a.sponsorRoomName,
		Sessions:              make([]RaffleSessionSummary, len(a.sessions)),
	}
	for i, s := range a.sessions {
		state.Sessions[i] = RaffleSessionSummary{
			ID:             s.ID,
			StartedAt:      s.StartedAt,
			ScheduledEndAt: s.ScheduledEndAt,
			EndedAt:        s.EndedAt,
			RaffleName:     s.RaffleName,
			PrizeName:      s.PrizeName,
			PrizeQty:       s.PrizeQty,
			WinnerName:     s.WinnerName,
			DBID:           s.DBID,
		}
	}
	state.CurrentSession = copySession(a.currentSession)
	return state
}

func (a *App) SetHypeShoutConfig(phrase string, minutes int) RaffleState {
	a.hypeShoutMu.Lock()
	a.hypeShoutPhrase = strings.TrimSpace(phrase)
	if minutes < 1 {
		minutes = 1
	}
	a.hypeShoutMinutes = minutes
	wasEnabled := a.hypeShoutEnabled
	a.hypeShoutMu.Unlock()

	if wasEnabled {
		a.ToggleHypeShout(false)
		return a.ToggleHypeShout(true)
	}

	a.emitUpdate()
	return a.GetState()
}

func (a *App) ToggleHypeShout(enabled bool) RaffleState {
	a.hypeShoutMu.Lock()
	a.hypeShoutEnabled = enabled
	if a.hypeShoutStopChan != nil {
		close(a.hypeShoutStopChan)
		a.hypeShoutStopChan = nil
	}

	if enabled {
		a.hypeShoutStopChan = make(chan struct{})
		stopChan := a.hypeShoutStopChan
		phrase := a.hypeShoutPhrase
		minutes := a.hypeShoutMinutes
		go a.runHypeShoutLoop(stopChan, phrase, minutes)
	}
	a.hypeShoutMu.Unlock()

	a.emitUpdate()
	return a.GetState()
}

func (a *App) runHypeShoutLoop(stopChan chan struct{}, phrase string, minutes int) {
	a.logDebug("[HYPE_SHOUT] started: ~every %d mins -> %q", minutes, phrase)
	base := time.Duration(minutes) * time.Minute

	for {
		// ±20% jitter
		jitter := time.Duration(rand.Int63n(int64(base/5)*2) - int64(base/5))
		wait := base + jitter
		
		a.hypeShoutMu.Lock()
		a.nextHypeShoutAt = time.Now().Add(wait)
		a.hypeShoutMu.Unlock()
		a.emitUpdate()

		nextAt := a.nextHypeShoutAt.Format("15:04:05")
		a.logDebug("[HYPE_SHOUT] next shout in %v (at %s)", wait.Truncate(time.Second), nextAt)

		select {
		case <-stopChan:
			a.hypeShoutMu.Lock()
			a.nextHypeShoutAt = time.Time{}
			a.hypeShoutMu.Unlock()
			a.logDebug("[HYPE_SHOUT] stopped")
			return
		case <-time.After(wait):
		}

		a.hypeShoutMu.Lock()
		enabled := a.hypeShoutEnabled
		currentPhrase := strings.TrimSpace(a.hypeShoutPhrase)
		a.hypeShoutMu.Unlock()

		if !enabled || currentPhrase == "" {
			continue
		}

		a.mu.Lock()
		prize := a.rafflePrizeName
		qty := a.rafflePrizeQty
		if a.currentSession != nil && strings.TrimSpace(a.currentSession.PrizeName) != "" {
			prize = a.currentSession.PrizeName
			qty = a.currentSession.PrizeQty
		}
		a.mu.Unlock()

		prizeDisplay := fmt.Sprintf("%s x%d", prize, qty)
		msg := strings.ReplaceAll(currentPhrase, "[prize]", prizeDisplay)
		ext.Send(out.SHOUT, msg)
		a.debugMu.Lock()
		a.lastShout = msg
		a.debugMu.Unlock()
	}
}

func (a *App) SetAutoMsgConfig(phrase string, minutes int) RaffleState {
	a.autoMsgMu.Lock()
	a.autoMsgPhrase = strings.TrimSpace(phrase)
	if minutes < 1 {
		minutes = 1
	}
	a.autoMsgMinutes = minutes
	wasEnabled := a.autoMsgEnabled
	a.autoMsgMu.Unlock()

	if wasEnabled {
		a.ToggleAutoMsg(false)
		return a.ToggleAutoMsg(true)
	}

	a.emitUpdate()
	return a.GetState()
}

func (a *App) ToggleAutoMsg(enabled bool) RaffleState {
	a.autoMsgMu.Lock()
	a.autoMsgEnabled = enabled
	if a.autoMsgStopChan != nil {
		close(a.autoMsgStopChan)
		a.autoMsgStopChan = nil
	}

	if enabled {
		a.autoMsgStopChan = make(chan struct{})
		stopChan := a.autoMsgStopChan
		phrase := a.autoMsgPhrase
		minutes := a.autoMsgMinutes
		go a.runAutoMsgLoop(stopChan, phrase, minutes)
	}
	a.autoMsgMu.Unlock()

	a.emitUpdate()
	return a.GetState()
}

func (a *App) runAutoMsgLoop(stopChan chan struct{}, phrase string, minutes int) {
	a.logDebug("[AUTO_MSG] started: ~every %d mins -> %q", minutes, phrase)
	base := time.Duration(minutes) * time.Minute

	for {
		// ±20% jitter
		jitter := time.Duration(rand.Int63n(int64(base/5)*2) - int64(base/5))
		wait := base + jitter
		
		a.autoMsgMu.Lock()
		a.nextAutoMsgAt = time.Now().Add(wait)
		a.autoMsgMu.Unlock()
		a.emitUpdate()

		nextAt := a.nextAutoMsgAt.Format("15:04:05")
		a.logDebug("[AUTO_MSG] next shout in %v (at %s)", wait.Truncate(time.Second), nextAt)

		select {
		case <-stopChan:
			a.autoMsgMu.Lock()
			a.nextAutoMsgAt = time.Time{}
			a.autoMsgMu.Unlock()
			a.logDebug("[AUTO_MSG] stopped")
			return
		case <-time.After(wait):
		}

		a.autoMsgMu.Lock()
		enabled := a.autoMsgEnabled
		currentPhrase := strings.TrimSpace(a.autoMsgPhrase)
		a.autoMsgMu.Unlock()

		if !enabled || currentPhrase == "" {
			continue
		}

		a.mu.Lock()
		prize := a.rafflePrizeName
		qty := a.rafflePrizeQty
		if a.currentSession != nil && strings.TrimSpace(a.currentSession.PrizeName) != "" {
			prize = a.currentSession.PrizeName
			qty = a.currentSession.PrizeQty
		}
		a.mu.Unlock()

		prizeDisplay := fmt.Sprintf("%s x%d", prize, qty)
		msg := strings.ReplaceAll(currentPhrase, "[prize]", prizeDisplay)
		ext.Send(out.SHOUT, msg)
		a.debugMu.Lock()
		a.lastShout = msg
		a.debugMu.Unlock()
	}
}

func (a *App) SetRaffleSponsorConfig(enabled bool, name string, roomName string) RaffleState {
	a.mu.Lock()
	a.sponsorEnabled = enabled
	a.sponsorName = strings.TrimSpace(name)
	a.sponsorRoomName = strings.TrimSpace(roomName)
	var sessionDBID int64
	if a.currentSession != nil {
		a.currentSession.SponsorEnabled = a.sponsorEnabled
		a.currentSession.SponsorName = a.sponsorName
		a.currentSession.SponsorRoomName = a.sponsorRoomName
		sessionDBID = a.currentSession.DBID
	}
	a.mu.Unlock()
	a.emitUpdate()
	if sessionDBID > 0 {
		if err := a.saveSessionMeta(sessionDBID); err != nil {
			a.logDebug("SetRaffleSponsorConfig save failed: %v", err)
		}
	}
	return a.GetState()
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

func (a *App) SetTicketAnnounceEnabled(v bool) RaffleState {
	a.mu.Lock()
	a.ticketAnnounceEnabled = v
	a.mu.Unlock()
	a.emitUpdate()
	return a.GetState()
}

func (a *App) GetTicketAnnounceEnabled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ticketAnnounceEnabled
}

func (a *App) SetTicketProgressEnabled(v bool) RaffleState {
	a.mu.Lock()
	a.ticketProgressEnabled = v
	a.mu.Unlock()
	a.emitUpdate()
	return a.GetState()
}

func (a *App) GetTicketProgressEnabled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ticketProgressEnabled
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

func parseHistoryStartedAt(raw string) (time.Time, bool) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return time.Time{}, false
	}

	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC(), true
		}
	}

	return time.Time{}, false
}

func formatDateDDMMYYYYGMTPlus10(raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" || strings.EqualFold(v, "tbd") {
		return "TBD (GMT +10)"
	}

	var t time.Time
	if parsed, err := time.Parse(time.RFC3339, v); err == nil {
		t = parsed.UTC()
	} else if parsed, ok := parseHistoryStartedAt(v); ok {
		t = parsed.UTC()
	} else {
		return v + " (GMT +10)"
	}

	loc := time.FixedZone("GMT+10", 10*60*60)
	return t.In(loc).Format("02/01/2006") + " GMT +10"
}

func decodeImageDataURL(dataURL string) ([]byte, string, error) {
	s := strings.TrimSpace(dataURL)
	if !strings.HasPrefix(s, "data:") {
		return nil, "", fmt.Errorf("missing data URL prefix")
	}

	parts := strings.SplitN(s, ",", 2)
	if len(parts) != 2 {
		return nil, "", fmt.Errorf("invalid data URL")
	}

	meta := strings.TrimPrefix(parts[0], "data:")
	b64 := parts[1]
	mimeType := "image/png"
	metaParts := strings.Split(meta, ";")
	if len(metaParts) > 0 {
		if mt := strings.TrimSpace(metaParts[0]); mt != "" {
			mimeType = mt
		}
	}

	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, "", err
	}

	return raw, mimeType, nil
}

func clampEmbedText(v string, max int) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "-"
	}
	if max <= 0 {
		max = 1024
	}
	if len(v) > max {
		return v[:max-1] + "..."
	}
	return v
}

func buildTicketBoard(participants []RaffleParticipant) string {
	if len(participants) == 0 {
		return "🎟️ No ticket purchases yet. Be the first to jump in!"
	}

	maxRows := 18
	if len(participants) < maxRows {
		maxRows = len(participants)
	}

	lines := make([]string, 0, maxRows+1)
	for i := 0; i < maxRows; i++ {
		p := participants[i]
		name := strings.TrimSpace(p.Username)
		if name == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("🎫 %d. %s — %d ticket(s)", i+1, name, p.Tickets))
	}

	if remaining := len(participants) - maxRows; remaining > 0 {
		lines = append(lines, fmt.Sprintf("...and %d more player(s)", remaining))
	}

	board := "📢 Live ticket board\n" + strings.Join(lines, "\n")
	if len(board) > 1000 {
		board = board[:1000] + "..."
	}
	return board
}

func sanitizeTrackerEmbedFields(fields []map[string]interface{}) []map[string]interface{} {
	// Keep this noisy field out even if someone accidentally adds it in future edits.
	filtered := make([]map[string]interface{}, 0, len(fields))
	for _, field := range fields {
		name, _ := field["name"].(string)
		if strings.Contains(strings.ToLower(strings.TrimSpace(name)), "ticket activity") {
			continue
		}
		filtered = append(filtered, field)
	}
	return filtered
}

func (a *App) SetRaffleDiscordConfig(
	raffleName string,
	rafflePrizeName string,
	rafflePrizeQty int,
	heroImageDataURL string,
	heroImageFileName string,
	autoUpdate bool,
) RaffleState {
	a.mu.Lock()
	a.raffleName = strings.TrimSpace(raffleName)
	a.rafflePrizeName = strings.TrimSpace(rafflePrizeName)
	if rafflePrizeQty <= 0 {
		rafflePrizeQty = 1
	}
	a.rafflePrizeQty = rafflePrizeQty
	a.raffleHeroDataURL = strings.TrimSpace(heroImageDataURL)
	a.raffleHeroFileName = strings.TrimSpace(heroImageFileName)
	if a.raffleHeroDataURL != "" {
		// New image file selected — clear stale Discord attachment IDs.
		// They will be replaced with fresh IDs when the next POST uploads the file.
		a.raffleHeroImageURL = ""
		a.raffleHeroAttachmentID = ""
		a.raffleHeroAttachmentFile = ""
	}
	// If no new image is provided, preserve existing attachment state so
	// every subsequent PATCH still carries the hero image reference.
	a.raffleAutoUpdate = autoUpdate
	var sessionDBID int64
	if a.currentSession != nil {
		// Freeze display metadata once a message exists for this session.
		// Later auto-updates should only update mutable tracker content.
		if strings.TrimSpace(a.currentSession.WebhookMessageID) == "" {
			a.currentSession.RaffleName = a.raffleName
			a.currentSession.PrizeName = a.rafflePrizeName
			a.currentSession.PrizeQty = a.rafflePrizeQty
			a.currentSession.HeroImageURL = a.raffleHeroImageURL
			a.currentSession.HeroAttachmentID = a.raffleHeroAttachmentID
			a.currentSession.HeroAttachmentFile = a.raffleHeroAttachmentFile
		}
		sessionDBID = a.currentSession.DBID
	}
	a.mu.Unlock()
	a.emitUpdate()
	if sessionDBID > 0 {
		if err := a.saveSessionMeta(sessionDBID); err != nil {
			a.logDebug("saveSessionMeta failed: %v", err)
		}
	}
	return a.GetState()
}

func (a *App) CreateRaffle(name, prize string, qty int, heroDataUrl, heroFileName string, startAt, endAt string) (RaffleState, error) {
	a.mu.Lock()
	a.raffleName = name
	a.rafflePrizeName = prize
	if qty > 0 {
		a.rafflePrizeQty = qty
	} else {
		a.rafflePrizeQty = 1
	}

	if heroDataUrl != "" {
		a.raffleHeroDataURL = heroDataUrl
		a.raffleHeroFileName = heroFileName
		// New image file selected — clear stale Discord attachment IDs.
		a.raffleHeroImageURL = ""
		a.raffleHeroAttachmentID = ""
		a.raffleHeroAttachmentFile = ""
	}
	// Let's assume auto-update should be on for new raffles.
	a.raffleAutoUpdate = true
	a.mu.Unlock()

	a.emitUpdate()

	// 2. Start the session.
	return a.StartRaffleWithWindow(startAt, endAt)
}

func (a *App) PostOrUpdateRaffleWebhook() string {
	if err := a.postOrUpdateRaffleWebhook(nil, true, "manual"); err != nil {
		a.logDebug("raffle webhook manual sync failed: %v", err)
		return err.Error()
	}
	return "ok"
}

func (a *App) RepostRaffleWebhook() string {
	a.mu.Lock()
	a.raffleMessageID = ""
	if a.currentSession != nil {
		// Repost defines a new baseline for static session metadata.
		a.currentSession.RaffleName = strings.TrimSpace(a.raffleName)
		a.currentSession.PrizeName = strings.TrimSpace(a.rafflePrizeName)
		a.currentSession.PrizeQty = a.rafflePrizeQty
		if a.currentSession.PrizeQty <= 0 {
			a.currentSession.PrizeQty = 1
		}
		a.currentSession.HeroImageURL = strings.TrimSpace(a.raffleHeroImageURL)
		a.currentSession.HeroAttachmentID = strings.TrimSpace(a.raffleHeroAttachmentID)
		a.currentSession.HeroAttachmentFile = strings.TrimSpace(a.raffleHeroAttachmentFile)
		a.currentSession.WebhookMessageID = ""
	}
	a.mu.Unlock()

	if err := a.postOrUpdateRaffleWebhook(nil, true, "repost"); err != nil {
		a.logDebug("raffle webhook repost failed: %v", err)
		return err.Error()
	}
	return "ok"
}

func (a *App) UpsertManualParticipant(username string, betCount int, tickets int) (RaffleState, error) {
	name := normalizeUsername(username)
	key := normalizeUsernameKey(name)
	if key == "" {
		return a.GetState(), fmt.Errorf("username is required")
	}
	if betCount < 0 {
		betCount = 0
	}
	if tickets < 0 {
		tickets = 0
	}

	now := time.Now().UTC().Format(time.RFC3339)

	a.mu.Lock()
	if a.currentSession == nil {
		a.mu.Unlock()
		return a.GetState(), fmt.Errorf("start or resume a raffle session first")
	}

	sessionDBID := a.currentSession.DBID
	bonusEvery := a.currentSession.BonusEvery
	if bonusEvery <= 0 {
		bonusEvery = 5
	}

	idx := -1
	for i := range a.currentSession.Participants {
		if a.currentSession.Participants[i].UsernameKey == key {
			idx = i
			break
		}
	}

	manualDelta := tickets - ticketsForBetCount(betCount, bonusEvery)
	var persisted RaffleParticipant
	if idx == -1 {
		persisted = RaffleParticipant{
			Username:    name,
			UsernameKey: key,
			BetCount:    betCount,
			Tickets:     effectiveTicketsForParticipant(betCount, bonusEvery, manualDelta),
			ManualDelta: manualDelta,
			FirstBet:    now,
			LastBet:     now,
		}
		a.currentSession.Participants = append(a.currentSession.Participants, persisted)
	} else {
		p := &a.currentSession.Participants[idx]
		if p.FirstBet == "" {
			p.FirstBet = now
		}
		p.Username = name
		p.UsernameKey = key
		p.BetCount = betCount
		p.ManualDelta = manualDelta
		p.Tickets = effectiveTicketsForParticipant(betCount, bonusEvery, manualDelta)
		p.LastBet = now
		persisted = *p
	}

	sortParticipants(a.currentSession.Participants)
	a.mu.Unlock()

	if sessionDBID <= 0 {
		return a.GetState(), fmt.Errorf("current raffle session is not persisted yet")
	}
	if err := a.persistParticipants(sessionDBID, []RaffleParticipant{persisted}); err != nil {
		a.logDebug("manual participant persist failed for %s: %v", name, err)
		return a.GetState(), err
	}

	a.logDebug("manual participant saved: %s bets=%d tickets=%d delta=%d", name, persisted.BetCount, persisted.Tickets, persisted.ManualDelta)
	a.emitUpdate()
	go func() {
		if err := a.postOrUpdateRaffleWebhook(nil, true, "manual-participant"); err != nil {
			a.logDebug("auto-patch manual participant failed: %v", err)
		}
	}()

	return a.GetState(), nil
}

func (a *App) RemoveParticipantFromCurrentSession(username string) (RaffleState, error) {
	name := normalizeUsername(username)
	key := normalizeUsernameKey(name)
	if key == "" {
		return a.GetState(), fmt.Errorf("username is required")
	}

	a.mu.Lock()
	if a.currentSession == nil {
		a.mu.Unlock()
		return a.GetState(), fmt.Errorf("start or resume a raffle session first")
	}

	sessionDBID := a.currentSession.DBID
	idx := -1
	for i := range a.currentSession.Participants {
		if a.currentSession.Participants[i].UsernameKey == key {
			idx = i
			break
		}
	}
	if idx == -1 {
		a.mu.Unlock()
		return a.GetState(), fmt.Errorf("participant %s not found in active session", name)
	}

	removedName := a.currentSession.Participants[idx].Username
	a.currentSession.Participants = append(a.currentSession.Participants[:idx], a.currentSession.Participants[idx+1:]...)
	a.mu.Unlock()

	if sessionDBID <= 0 {
		return a.GetState(), fmt.Errorf("current raffle session is not persisted yet")
	}
	if err := a.deleteParticipant(sessionDBID, key); err != nil {
		a.logDebug("participant delete failed for %s: %v", removedName, err)
		return a.GetState(), err
	}

	a.logDebug("participant removed from session: %s", removedName)
	a.emitUpdate()
	go func() {
		if err := a.postOrUpdateRaffleWebhook(nil, true, "remove-participant"); err != nil {
			a.logDebug("auto-patch remove participant failed: %v", err)
		}
	}()

	return a.GetState(), nil
}

func (a *App) DrawWinnerForCurrentSession() string {
	a.mu.Lock()
	if a.currentSession == nil {
		a.mu.Unlock()
		return "start or resume a raffle session first"
	}
	s := a.currentSession
	if len(s.Participants) == 0 {
		a.mu.Unlock()
		return "no participants with tickets yet"
	}

	totalTickets := 0
	for _, p := range s.Participants {
		if p.Tickets > 0 {
			totalTickets += p.Tickets
		}
	}
	if totalTickets <= 0 {
		a.mu.Unlock()
		return "no valid tickets to draw from"
	}

	rnd, err := crand.Int(crand.Reader, big.NewInt(int64(totalTickets)))
	if err != nil {
		a.mu.Unlock()
		a.logDebug("winner draw failed: %v", err)
		return "failed to draw winner securely"
	}

	pick := int(rnd.Int64()) + 1
	cumulative := 0
	var winner RaffleParticipant
	found := false
	for _, p := range s.Participants {
		if p.Tickets <= 0 {
			continue
		}
		cumulative += p.Tickets
		if pick <= cumulative {
			winner = p
			found = true
			break
		}
	}
	if !found {
		a.mu.Unlock()
		return "failed to resolve winner from ticket pool"
	}

	oddsPct := (float64(winner.Tickets) / float64(totalTickets)) * 100.0
	oddsText := fmt.Sprintf("%d/%d (%.2f%%)", winner.Tickets, totalTickets, oddsPct)
	drawnAt := time.Now().UTC().Format(time.RFC3339)
	method := "crypto/rand weighted ticket draw"
	summary := fmt.Sprintf("Winner %s selected with secure weighted draw: pick=%d over %d total tickets; %s", winner.Username, pick, totalTickets, oddsText)

	s.WinnerName = winner.Username
	s.WinnerTickets = winner.Tickets
	s.WinnerOdds = oddsText
	s.WinnerDrawnAt = drawnAt
	s.WinnerMethod = method
	s.WinnerSummary = summary

	a.mu.Unlock()

	a.logDebug("winner drawn: %s", summary)
	a.emitUpdate()
	go func() {
		if err := a.postOrUpdateRaffleWebhook(nil, true, "winner-draw"); err != nil {
			a.logDebug("auto-patch winner draw failed: %v", err)
		}
	}()
	return fmt.Sprintf("Winner: %s | Odds: %s | Method: %s", winner.Username, oddsText, method)
}

func (a *App) PostWinnerProofForCurrentSession(imageDataURL string, imageFileName string) string {
	dataURL := strings.TrimSpace(imageDataURL)
	if dataURL == "" {
		return "winner proof image is required"
	}

	a.mu.Lock()
	if a.currentSession == nil {
		a.mu.Unlock()
		return "start or resume a raffle session first"
	}
	if a.currentSession.WinnerName == "" {
		a.mu.Unlock()
		return "draw a winner first"
	}
	messageID := strings.TrimSpace(a.currentSession.WebhookMessageID)
	if messageID == "" {
		messageID = strings.TrimSpace(a.raffleMessageID)
	}
	if messageID == "" {
		a.mu.Unlock()
		return "post the raffle to Discord first"
	}
	a.mu.Unlock()

	raw, mimeType, err := decodeImageDataURL(dataURL)
	if err != nil {
		return "proof image decode error: " + err.Error()
	}

	fileName := strings.TrimSpace(imageFileName)
	if fileName == "" {
		switch mimeType {
		case "image/jpeg":
			fileName = "winner-proof.jpg"
		case "image/gif":
			fileName = "winner-proof.gif"
		case "image/webp":
			fileName = "winner-proof.webp"
		default:
			fileName = "winner-proof.png"
		}
	}

	a.mu.Lock()
	a.pendingProofBytes = raw
	a.pendingProofFileName = fileName
	a.mu.Unlock()

	if err := a.postOrUpdateRaffleWebhook(nil, true, "winner-proof"); err != nil {
		a.mu.Lock()
		a.pendingProofBytes = nil
		a.pendingProofFileName = ""
		a.mu.Unlock()
		a.logDebug("winner proof patch failed: %v", err)
		return err.Error()
	}
	a.logDebug("winner proof patched into raffle message")
	return "ok"
}

func withWebhookWait(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Set("wait", "true")
	u.RawQuery = q.Encode()
	return u.String()
}

func webhookMessageEndpoint(raw string, messageID string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	u.RawQuery = ""
	u.Path = strings.TrimRight(u.Path, "/") + "/messages/" + strings.TrimSpace(messageID)
	return u.String(), nil
}

func (a *App) postOrUpdateRaffleWebhook(sessionOverride *RaffleSession, allowManual bool, reason string) error {
	a.mu.Lock()
	webhookURL := strings.TrimSpace(hardcodedRaffleWebhookURL)
	autoUpdate := a.raffleAutoUpdate
	
	// Default fallbacks (internal constants or state-defaults)
	raffleName := "Flame Raffle"
	prizeName := "Purple Dragon Lamp"
	prizeQty := 1
	heroDataURL := ""
	heroFileName := ""
	heroImageURL := ""
	heroAttachmentID := ""
	heroAttachmentFile := ""
	messageID := ""
	sponsorEnabled := false
	sponsorName := ""
	sponsorRoomName := ""

	// Determine which session we are working with
	var session *RaffleSession
	if sessionOverride != nil {
		session = copySession(sessionOverride)
	} else if a.currentSession != nil {
		session = copySession(a.currentSession)
	}

	if session != nil {
		// Strictly use session's own metadata
		if n := strings.TrimSpace(session.RaffleName); n != "" {
			raffleName = n
		} else if sessionOverride == nil {
			// Fallback to bot-global ONLY if we are processing the current session
			if gName := strings.TrimSpace(a.raffleName); gName != "" {
				raffleName = gName
			}
		}

		if n := strings.TrimSpace(session.PrizeName); n != "" {
			prizeName = n
		} else if sessionOverride == nil {
			if gPrize := strings.TrimSpace(a.rafflePrizeName); gPrize != "" {
				prizeName = gPrize
			}
		}

		if session.PrizeQty > 0 {
			prizeQty = session.PrizeQty
		} else if sessionOverride == nil {
			if a.rafflePrizeQty > 0 {
				prizeQty = a.rafflePrizeQty
			}
		}

		heroImageURL = strings.TrimSpace(session.HeroImageURL)
		heroAttachmentID = strings.TrimSpace(session.HeroAttachmentID)
		heroAttachmentFile = strings.TrimSpace(session.HeroAttachmentFile)
		
		// If this is the current session, we might have unsaved/pending hero data in the bot state
		if sessionOverride == nil || (a.currentSession != nil && session.DBID == a.currentSession.DBID) {
			heroDataURL = strings.TrimSpace(a.raffleHeroDataURL)
			heroFileName = strings.TrimSpace(a.raffleHeroFileName)
			if heroImageURL == "" {
				heroImageURL = strings.TrimSpace(a.raffleHeroImageURL)
			}
			if heroAttachmentID == "" {
				heroAttachmentID = strings.TrimSpace(a.raffleHeroAttachmentID)
			}
			if heroAttachmentFile == "" {
				heroAttachmentFile = strings.TrimSpace(a.raffleHeroAttachmentFile)
			}
		}

		messageID = strings.TrimSpace(session.WebhookMessageID)
		if messageID == "" && sessionOverride == nil {
			messageID = strings.TrimSpace(a.raffleMessageID)
		}

		sponsorEnabled = session.SponsorEnabled
		sponsorName = strings.TrimSpace(session.SponsorName)
		sponsorRoomName = strings.TrimSpace(session.SponsorRoomName)
		
		if !sponsorEnabled && sessionOverride == nil {
			sponsorEnabled = a.sponsorEnabled
			sponsorName = strings.TrimSpace(a.sponsorName)
			sponsorRoomName = strings.TrimSpace(a.sponsorRoomName)
		}
	}
	a.mu.Unlock()

	if webhookURL == "" {
		return fmt.Errorf("discord webhook URL is empty")
	}
	if !autoUpdate && !allowManual {
		return nil
	}
	if session == nil {
		if allowManual {
			return fmt.Errorf("start or resume a live raffle before posting to discord")
		}
		return fmt.Errorf("no raffle session available")
	}

	if raffleName == "" {
		raffleName = "Flame Raffle"
	}
	if prizeName == "" {
		prizeName = "Purple Dragon Lamp"
	}
	if prizeQty <= 0 {
		prizeQty = 1
	}
	prizeDisplay := fmt.Sprintf("%s x%d", prizeName, prizeQty)

	now := time.Now().UTC()
	participants := session.Participants
	totalTickets := 0
	for _, p := range participants {
		if p.Tickets > 0 {
			totalTickets += p.Tickets
		}
	}

	statusText := "started"
	if strings.TrimSpace(session.EndedAt) != "" {
		statusText = "ended"
	}

	startLine := session.StartedAt
	if startLine == "" {
		startLine = now.Format(time.RFC3339)
	}

	endLine := session.ScheduledEndAt
	if strings.TrimSpace(session.EndedAt) != "" {
		endLine = session.EndedAt
	}
	if strings.TrimSpace(endLine) == "" {
		endLine = "TBD"
	}

	ticketBoard := buildTicketBoard(participants)
	headerLine := fmt.Sprintf("🎉 NEW RAFFLE! 🎉 %s | 🎁 %s | 🎟️ %d total tickets", raffleName, prizeDisplay, totalTickets)
	if sponsorEnabled && sponsorName != "" {
		headerLine = fmt.Sprintf("🎉 NEW RAFFLE! 🎉 %s | 🎁 %s | 💎 Sponsored by %s", raffleName, prizeDisplay, sponsorName)
	}

	promoEmbedFields := []map[string]interface{}{
		{"name": "🔥 How To Enter", "value": clampEmbedText("Find a live dealer at rollorigins.club and place a bet for your chance to win.", 1000), "inline": false},
		{"name": "🎟️ Ticket Rules", "value": "1st bet = 1 Ticket + Every 5th = 1 FREE Ticket.", "inline": false},
		{"name": "📣 Heads Up", "value": "Ticket board below updates automatically whenever someone earns tickets.", "inline": false},
	}
	if sponsorEnabled && sponsorName != "" {
		sponsorVal := sponsorName
		if sponsorRoomName != "" {
			sponsorVal = fmt.Sprintf("**%s**\n📍 Room: *%s*", sponsorName, sponsorRoomName)
		}
		promoEmbedFields = append([]map[string]interface{}{
			{"name": "💎 Sponsored By", "value": sponsorVal, "inline": false},
		}, promoEmbedFields...)
	}

	promoEmbed := map[string]interface{}{
		"title":       fmt.Sprintf("🎁 WHAT'S UP FOR GRABS 🎁"),
		"description": fmt.Sprintf("🎁 **%s**\n🔥 **%s**", prizeDisplay, raffleName),
		"color":       0xF1C40F,
		"fields":      promoEmbedFields,
		"footer":      map[string]interface{}{"text": "✨ Roll Origins Free Raffle"},
		"timestamp":   now.Format(time.RFC3339),
	}

	trackerFields := []map[string]interface{}{
		{"name": "📌 Status", "value": "🟢 " + statusText, "inline": true},
		{"name": "👥 Participants", "value": strconv.Itoa(len(participants)), "inline": true},
		{"name": "🎟️ Total Tickets", "value": strconv.Itoa(totalTickets), "inline": true},
		{"name": "🧾 Raffle ID", "value": strconv.FormatInt(session.DBID, 10), "inline": false},
		{"name": "⏰ Planned End", "value": clampEmbedText(formatDateDDMMYYYYGMTPlus10(endLine), 1000), "inline": false},
		{"name": "🏆 Winner", "value": func() string {
			if strings.TrimSpace(session.WinnerName) == "" {
				return "TBD"
			}
			return clampEmbedText(session.WinnerName, 200)
		}(), "inline": true},
		{"name": "🔐 Draw Method", "value": func() string {
			if strings.TrimSpace(session.WinnerMethod) == "" {
				return "Pending draw"
			}
			return clampEmbedText(session.WinnerMethod, 200)
		}(), "inline": true},
		{"name": "📊 Winner Odds", "value": func() string {
			if strings.TrimSpace(session.WinnerOdds) == "" {
				return "Pending draw"
			}
			return clampEmbedText(session.WinnerOdds, 200)
		}(), "inline": true},
		{"name": "🕒 Created", "value": clampEmbedText(startLine, 1000), "inline": true},
		{"name": "🧠 Winner Explain", "value": func() string {
			if strings.TrimSpace(session.WinnerSummary) == "" {
				return "Draw not completed yet."
			}
			return clampEmbedText(session.WinnerSummary, 1000)
		}(), "inline": false},
		{"name": "📣 Ticket Board", "value": clampEmbedText(ticketBoard, 1000), "inline": false},
	}

	trackerEmbed := map[string]interface{}{
		"title":       "🎲 Roll Origins Raffle Tracker 🎲",
		"description": fmt.Sprintf("🎉 **NEW RAFFLE!** 🎉 %s\n🎁 Prize: **%s**", raffleName, prizeDisplay),
		"color":       0x2ECC71,
		"fields":      sanitizeTrackerEmbedFields(trackerFields),
		"footer":      map[string]interface{}{"text": "✨ Auto-updated on every ticket buy and winner draw ✨"},
		"timestamp":   now.Format(time.RFC3339),
	}

	if strings.TrimSpace(session.WinnerProofID) != "" && strings.TrimSpace(session.WinnerProofFile) != "" {
		trackerEmbed["image"] = map[string]interface{}{"url": "attachment://" + strings.TrimSpace(session.WinnerProofFile)}
	} else if strings.TrimSpace(session.WinnerProofURL) != "" {
		trackerEmbed["image"] = map[string]interface{}{"url": strings.TrimSpace(session.WinnerProofURL)}
	}

	if heroAttachmentID != "" && heroAttachmentFile != "" {
		promoEmbed["image"] = map[string]interface{}{"url": "attachment://" + heroAttachmentFile}
	} else if heroImageURL != "" {
		promoEmbed["image"] = map[string]interface{}{"url": heroImageURL}
	}

	payload := map[string]interface{}{
		"username": "Roll Origins Raffles",
		"content":  headerLine,
		"embeds":   []interface{}{promoEmbed, trackerEmbed},
		"allowed_mentions": map[string]interface{}{
			"parse": []string{},
		},
	}

	a.mu.Lock()
	proofBytes := a.pendingProofBytes
	proofFileName := strings.TrimSpace(a.pendingProofFileName)
	existingProofAttachmentID := ""
	existingProofFileName := ""
	if session != nil {
		existingProofAttachmentID = strings.TrimSpace(session.WinnerProofID)
		existingProofFileName = strings.TrimSpace(session.WinnerProofFile)
	}
	a.mu.Unlock()

	if proofBytes != nil && proofFileName != "" {
		trackerEmbed["image"] = map[string]interface{}{"url": "attachment://" + proofFileName}
	}

	client := &http.Client{Timeout: 10 * time.Second}

	if messageID != "" {
		endpoint, err := webhookMessageEndpoint(webhookURL, messageID)
		if err != nil {
			return err
		}

		var patchReq *http.Request
		if (proofBytes != nil && proofFileName != "") || (heroDataURL != "" && heroFileName != "") {
			// Multipart PATCH to upload new files (proof and/or hero).
			attachmentsList := []map[string]interface{}{}
			nextFileIdx := 0

			// 1. Existing Hero (if not being replaced)
			if heroDataURL == "" && heroAttachmentID != "" && heroAttachmentFile != "" {
				attachmentsList = append(attachmentsList, map[string]interface{}{"id": heroAttachmentID, "filename": heroAttachmentFile})
			}

			// 2. New Hero upload
			heroUploadIdx := -1
			var heroBytes []byte
			actualHeroName := heroFileName
			if heroDataURL != "" {
				raw, mimeType, err := decodeImageDataURL(heroDataURL)
				if err == nil {
					heroBytes = raw
					if actualHeroName == "" {
						switch mimeType {
						case "image/jpeg":
							actualHeroName = "raffle-hero.jpg"
						case "image/gif":
							actualHeroName = "raffle-hero.gif"
						case "image/webp":
							actualHeroName = "raffle-hero.webp"
						default:
							actualHeroName = "raffle-hero.png"
						}
					}
					heroUploadIdx = nextFileIdx
					promoEmbed["image"] = map[string]interface{}{"url": "attachment://" + actualHeroName}
					attachmentsList = append(attachmentsList, map[string]interface{}{"id": strconv.Itoa(heroUploadIdx), "filename": actualHeroName})
					nextFileIdx++
				}
			}

			// 3. Existing Proof (if not being replaced)
			if proofBytes == nil && existingProofAttachmentID != "" && existingProofFileName != "" {
				attachmentsList = append(attachmentsList, map[string]interface{}{"id": existingProofAttachmentID, "filename": existingProofFileName})
			}

			// 4. New Proof upload
			proofUploadIdx := -1
			if proofBytes != nil && proofFileName != "" {
				proofUploadIdx = nextFileIdx
				trackerEmbed["image"] = map[string]interface{}{"url": "attachment://" + proofFileName}
				attachmentsList = append(attachmentsList, map[string]interface{}{"id": strconv.Itoa(proofUploadIdx), "filename": proofFileName})
				nextFileIdx++
			}

			payload["attachments"] = attachmentsList

			jb, err := json.Marshal(payload)
			if err != nil {
				return err
			}
			var mpBody bytes.Buffer
			mpWriter := multipart.NewWriter(&mpBody)
			if err := mpWriter.WriteField("payload_json", string(jb)); err != nil {
				return err
			}

			if heroUploadIdx >= 0 && len(heroBytes) > 0 {
				part, err := mpWriter.CreateFormFile(fmt.Sprintf("files[%d]", heroUploadIdx), actualHeroName)
				if err != nil {
					return err
				}
				if _, err := part.Write(heroBytes); err != nil {
					return err
				}
			}

			if proofUploadIdx >= 0 && len(proofBytes) > 0 {
				part, err := mpWriter.CreateFormFile(fmt.Sprintf("files[%d]", proofUploadIdx), proofFileName)
				if err != nil {
					return err
				}
				if _, err := part.Write(proofBytes); err != nil {
					return err
				}
			}

			if err := mpWriter.Close(); err != nil {
				return err
			}
			patchReq, err = http.NewRequest("PATCH", endpoint, &mpBody)
			if err != nil {
				return err
			}
			patchReq.Header.Set("Content-Type", mpWriter.FormDataContentType())
		} else {
			attachmentsList := []map[string]interface{}{}
			// Always keep existing attachments that are referenced by attachment:// in embeds.
			if heroAttachmentID != "" && heroAttachmentFile != "" {
				attachmentsList = append(attachmentsList, map[string]interface{}{"id": heroAttachmentID, "filename": heroAttachmentFile})
			}
			if existingProofAttachmentID != "" && existingProofFileName != "" {
				attachmentsList = append(attachmentsList, map[string]interface{}{"id": existingProofAttachmentID, "filename": existingProofFileName})
			}
			payload["attachments"] = attachmentsList
			jb, err := json.Marshal(payload)
			if err != nil {
				return err
			}
			patchReq, err = http.NewRequest("PATCH", endpoint, bytes.NewReader(jb))
			if err != nil {
				return err
			}
			patchReq.Header.Set("Content-Type", "application/json")
		}

		resp, err := client.Do(patchReq)
		if err == nil {
			defer resp.Body.Close()
			patchBody, _ := io.ReadAll(resp.Body)
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				// Always parse all attachments from the PATCH response so we can
				// refresh both the hero CDN URL/ID and the proof URL/ID. This
				// ensures the hero image never disappears from the embed if Discord
				// renews attachment IDs between patches.
				var patchResp struct {
					Attachments []struct {
						ID       string `json:"id"`
						Filename string `json:"filename"`
						URL      string `json:"url"`
					} `json:"attachments"`
				}
				_ = json.Unmarshal(patchBody, &patchResp)

				// Extract updated hero attachment state from response.
				newHeroID := ""
				newHeroFile := ""
				newHeroURL := ""

				heroFilenameToFind := heroAttachmentFile
				if heroDataURL != "" {
					// Replicate logic to find the name of the file that was just uploaded
					if heroFileName != "" {
						heroFilenameToFind = heroFileName
					} else {
						_, mimeType, err := decodeImageDataURL(heroDataURL)
						if err == nil {
							switch mimeType {
							case "image/jpeg":
								heroFilenameToFind = "raffle-hero.jpg"
							case "image/gif":
								heroFilenameToFind = "raffle-hero.gif"
							case "image/webp":
								heroFilenameToFind = "raffle-hero.webp"
							default:
								heroFilenameToFind = "raffle-hero.png"
							}
						}
					}
				}

				if heroFilenameToFind != "" {
					for _, att := range patchResp.Attachments {
						if strings.EqualFold(att.Filename, heroFilenameToFind) {
							newHeroID = strings.TrimSpace(att.ID)
							newHeroFile = strings.TrimSpace(att.Filename)
							newHeroURL = strings.TrimSpace(att.URL)
							break
						}
					}
				}

				// Extract proof attachment state from response.
				cdnURL := ""
				proofAttachmentID := ""
				proofAttachmentFile := ""
				if proofBytes != nil || existingProofAttachmentID != "" {
					for _, att := range patchResp.Attachments {
						if (proofFileName != "" && strings.EqualFold(att.Filename, proofFileName)) || (proofFileName == "" && existingProofFileName != "" && strings.EqualFold(att.Filename, existingProofFileName)) {
							cdnURL = strings.TrimSpace(att.URL)
							proofAttachmentID = strings.TrimSpace(att.ID)
							proofAttachmentFile = strings.TrimSpace(att.Filename)
							break
						}
					}
					if cdnURL == "" && proofBytes != nil && len(patchResp.Attachments) > 0 {
						last := patchResp.Attachments[len(patchResp.Attachments)-1]
						cdnURL = strings.TrimSpace(last.URL)
						proofAttachmentID = strings.TrimSpace(last.ID)
						proofAttachmentFile = strings.TrimSpace(last.Filename)
					}
				}

				heroMetaChanged := false
				a.mu.Lock()
				// Clear the data URL so we don't try to re-upload it on every auto-update.
				a.raffleHeroDataURL = ""
				a.raffleHeroFileName = ""

				// Update hero attachment state so future PATCHes use the fresh ID/URL.
				if newHeroURL != "" && newHeroURL != a.raffleHeroImageURL {
					a.raffleHeroImageURL = newHeroURL
					if a.currentSession != nil && session != nil && a.currentSession.DBID == session.DBID {
						a.currentSession.HeroImageURL = newHeroURL
					}
					heroMetaChanged = true
				}
				if newHeroID != "" && newHeroID != a.raffleHeroAttachmentID {
					a.raffleHeroAttachmentID = newHeroID
					if a.currentSession != nil && session != nil && a.currentSession.DBID == session.DBID {
						a.currentSession.HeroAttachmentID = newHeroID
					}
					heroMetaChanged = true
				}
				if newHeroFile != "" && newHeroFile != a.raffleHeroAttachmentFile {
					a.raffleHeroAttachmentFile = newHeroFile
					if a.currentSession != nil && session != nil && a.currentSession.DBID == session.DBID {
						a.currentSession.HeroAttachmentFile = newHeroFile
					}
					heroMetaChanged = true
				}
				// Update proof attachment state.
				proofMetaChanged := false
				if cdnURL != "" && a.currentSession != nil && session != nil && a.currentSession.DBID == session.DBID {
					if cdnURL != a.currentSession.WinnerProofURL || proofAttachmentID != a.currentSession.WinnerProofID {
						proofMetaChanged = true
					}
					a.currentSession.WinnerProofURL = cdnURL
					a.currentSession.WinnerProofID = proofAttachmentID
					a.currentSession.WinnerProofFile = proofAttachmentFile
				}
				a.pendingProofBytes = nil
				a.pendingProofFileName = ""
				var metaSessionDBID int64
				if (heroMetaChanged || proofMetaChanged || reason == "winner-draw" || reason == "winner-proof") && session != nil {
					metaSessionDBID = session.DBID
				}
				a.mu.Unlock()

				if session != nil && session.DBID > 0 && strings.TrimSpace(messageID) != "" {
					if err := a.persistSessionWebhookMessageID(session.DBID, strings.TrimSpace(messageID)); err != nil {
						a.logDebug("failed to persist webhook message id after patch session=%d id=%s err=%v", session.DBID, strings.TrimSpace(messageID), err)
					}
				}
				if metaSessionDBID > 0 {
					if err := a.saveSessionMeta(metaSessionDBID); err != nil {
						a.logDebug("saveSessionMeta after patch metadata refresh failed: %v", err)
					}
				}
				a.logDebug("raffle webhook updated message id=%s reason=%s heroRefreshed=%v", messageID, reason, heroMetaChanged)
				return nil
			}
			a.logDebug("raffle webhook update failed status=%d body=%s; falling back to create", resp.StatusCode, strings.TrimSpace(string(patchBody)))
		} else {
			a.logDebug("raffle webhook update request failed: %v; falling back to create", err)
		}
	}

	type uploadFile struct {
		Field string
		Name  string
		Bytes []byte
	}
	uploads := make([]uploadFile, 0, 1)
	createWithHeroUpload := false
	if heroDataURL != "" {
		raw, mimeType, err := decodeImageDataURL(heroDataURL)
		if err != nil {
			return fmt.Errorf("hero image decode error: %w", err)
		}
		fileName := strings.TrimSpace(heroFileName)
		if fileName == "" {
			switch mimeType {
			case "image/jpeg":
				fileName = "raffle-hero.jpg"
			case "image/gif":
				fileName = "raffle-hero.gif"
			case "image/webp":
				fileName = "raffle-hero.webp"
			default:
				createWithHeroUpload = true
				fileName = "raffle-hero.png"

				// Fallback creates must not reference attachment:// images unless those files are uploaded in this request.
				if !createWithHeroUpload {
					if heroImageURL != "" {
						promoEmbed["image"] = map[string]interface{}{"url": heroImageURL}
					} else {
						delete(promoEmbed, "image")
					}
				}
				if proofBytes == nil || proofFileName == "" {
					if strings.TrimSpace(session.WinnerProofURL) != "" {
						trackerEmbed["image"] = map[string]interface{}{"url": strings.TrimSpace(session.WinnerProofURL)}
					} else if strings.TrimSpace(session.WinnerProofID) != "" && strings.TrimSpace(session.WinnerProofFile) != "" {
						delete(trackerEmbed, "image")
					}
				}
			}
		}
		promoEmbed["image"] = map[string]interface{}{"url": "attachment://" + fileName}
		uploads = append(uploads, uploadFile{Field: "files[0]", Name: fileName, Bytes: raw})
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	postURL := withWebhookWait(webhookURL)
	var req *http.Request

	if len(uploads) == 0 {
		req, err = http.NewRequest("POST", postURL, bytes.NewReader(payloadJSON))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
	} else {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		if err := writer.WriteField("payload_json", string(payloadJSON)); err != nil {
			return err
		}
		for _, f := range uploads {
			part, err := writer.CreateFormFile(f.Field, f.Name)
			if err != nil {
				return err
			}
			if _, err := part.Write(f.Bytes); err != nil {
				return err
			}
		}
		if err := writer.Close(); err != nil {
			return err
		}

		req, err = http.NewRequest("POST", postURL, &body)
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var respPayload struct {
		ID          string `json:"id"`
		Attachments []struct {
			ID       string `json:"id"`
			Filename string `json:"filename"`
			URL      string `json:"url"`
		} `json:"attachments"`
	}
	_ = json.Unmarshal(body, &respPayload)

	a.mu.Lock()
	a.raffleHeroDataURL = ""
	a.raffleHeroFileName = ""

	if strings.TrimSpace(respPayload.ID) != "" {
		a.raffleMessageID = strings.TrimSpace(respPayload.ID)
		if a.currentSession != nil && session != nil && a.currentSession.DBID == session.DBID {
			a.currentSession.WebhookMessageID = strings.TrimSpace(respPayload.ID)
		}
	}
	heroAttachmentUpdated := false
	if len(respPayload.Attachments) > 0 {
		if strings.TrimSpace(respPayload.Attachments[0].URL) != "" {
			a.raffleHeroImageURL = strings.TrimSpace(respPayload.Attachments[0].URL)
			if a.currentSession != nil && session != nil && a.currentSession.DBID == session.DBID {
				a.currentSession.HeroImageURL = strings.TrimSpace(respPayload.Attachments[0].URL)
			}
			heroAttachmentUpdated = true
		}
		if strings.TrimSpace(respPayload.Attachments[0].ID) != "" {
			a.raffleHeroAttachmentID = strings.TrimSpace(respPayload.Attachments[0].ID)
			if a.currentSession != nil && session != nil && a.currentSession.DBID == session.DBID {
				a.currentSession.HeroAttachmentID = strings.TrimSpace(respPayload.Attachments[0].ID)
			}
			heroAttachmentUpdated = true
		}
		if strings.TrimSpace(respPayload.Attachments[0].Filename) != "" {
			a.raffleHeroAttachmentFile = strings.TrimSpace(respPayload.Attachments[0].Filename)
			if a.currentSession != nil && session != nil && a.currentSession.DBID == session.DBID {
				a.currentSession.HeroAttachmentFile = strings.TrimSpace(respPayload.Attachments[0].Filename)
			}
			heroAttachmentUpdated = true
		}
	}
	var metaSessionDBID int64
	if session != nil && heroAttachmentUpdated {
		metaSessionDBID = session.DBID
	}
	a.mu.Unlock()

	if session != nil && session.DBID > 0 && strings.TrimSpace(respPayload.ID) != "" {
		if err := a.persistSessionWebhookMessageID(session.DBID, strings.TrimSpace(respPayload.ID)); err != nil {
			a.logDebug("failed to persist webhook message id session=%d id=%s err=%v", session.DBID, strings.TrimSpace(respPayload.ID), err)
		}
	}
	if metaSessionDBID > 0 {
		if err := a.saveSessionMeta(metaSessionDBID); err != nil {
			a.logDebug("saveSessionMeta after hero attachment failed: %v", err)
		}
	}

	a.logDebug("raffle webhook posted message id=%s reason=%s", strings.TrimSpace(respPayload.ID), reason)
	a.emitUpdate()
	return nil
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
	var raffleName string
	var rafflePrizeName string
	var rafflePrizeQty int
	var heroImageURL string
	var heroAttachmentID string
	var heroAttachmentFile string
	var sponsorEnabled bool
	var sponsorName string
	var sponsorRoomName string

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
		raffleName = strings.TrimSpace(a.raffleName)
		if raffleName == "" {
			raffleName = "Flame Raffle"
		}
		rafflePrizeName = strings.TrimSpace(a.rafflePrizeName)
		if rafflePrizeName == "" {
			rafflePrizeName = "Purple Dragon Lamp"
		}
		rafflePrizeQty = a.rafflePrizeQty
		if rafflePrizeQty <= 0 {
			rafflePrizeQty = 1
		}
		heroImageURL = strings.TrimSpace(a.raffleHeroImageURL)
		heroAttachmentID = strings.TrimSpace(a.raffleHeroAttachmentID)
		heroAttachmentFile = strings.TrimSpace(a.raffleHeroAttachmentFile)
		sponsorEnabled = a.sponsorEnabled
		sponsorName = strings.TrimSpace(a.sponsorName)
		sponsorRoomName = strings.TrimSpace(a.sponsorRoomName)

		a.currentSession = &RaffleSession{
			ID:                 sessionID,
			StartedAt:          startedAt,
			ScheduledEndAt:     scheduledEndAt,
			RaffleName:         raffleName,
			PrizeName:          rafflePrizeName,
			PrizeQty:           rafflePrizeQty,
			BonusEvery:         bonusEvery,
			Participants:       []RaffleParticipant{},
			CursorAt:           startAt,
			HeroImageURL:       heroImageURL,
			HeroAttachmentID:   heroAttachmentID,
			HeroAttachmentFile: heroAttachmentFile,
			SponsorEnabled:     sponsorEnabled,
			SponsorName:        sponsorName,
			SponsorRoomName:    sponsorRoomName,
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
			`INSERT INTO raffle_sessions (started_at, scheduled_end_at, owner_key, bonus_every, last_seen_created_at, last_seen_entry_id, webhook_message_id,
			                              raffle_name, prize_name, prize_qty, hero_image_url, hero_attachment_id, hero_attachment_file,
			                              sponsor_enabled, sponsor_name, sponsor_room_name)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
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
			"",
			raffleName,
			rafflePrizeName,
			rafflePrizeQty,
			heroImageURL,
			heroAttachmentID,
			heroAttachmentFile,
			sponsorEnabled,
			sponsorName,
			sponsorRoomName,
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
	go a.processNewBets()
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

func (a *App) ResumeSession(dbID int64) (RaffleState, error) {
	a.mu.Lock()
        if a.currentSession != nil {
                a.mu.Unlock()
                a.StopRaffle()
                a.mu.Lock()
        }
	idx := -1
	for i := range a.sessions {
		if a.sessions[i].DBID == dbID {
			idx = i
			break
		}
	}
	if idx == -1 {
		a.mu.Unlock()
		return a.GetState(), fmt.Errorf("session %d not found", dbID)
	}
	// Move it out of closed list and make it current (clear EndedAt so it's live)
	s := a.sessions[idx]
	a.sessions = append(a.sessions[:idx], a.sessions[idx+1:]...)
	s.EndedAt = ""
	s.ResumedAt = time.Now().UTC()
	a.currentSession = &s
	db := a.db
	owner := a.ownerKey
	a.enabled = true
	a.mu.Unlock()

	// Reopen in DB (clear ended_at)
	if db != nil && dbID > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if _, err := db.Exec(ctx,
			`UPDATE raffle_sessions SET ended_at = NULL WHERE id = $1 AND owner_key = $2`,
			dbID, owner,
		); err != nil {
			a.logDebug("resume session db update failed: %v", err)
		}
	}

	a.mu.Lock()
	if a.currentSession != nil && a.currentSession.DBID == dbID {
		a.raffleMessageID = strings.TrimSpace(a.currentSession.WebhookMessageID)
	}
	a.mu.Unlock()

	a.logDebug("resumed session dbID=%d participants=%d", dbID, len(s.Participants))
	a.emitUpdate()
	go a.processNewBets()
	return a.GetState(), nil
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

type TallyItem struct {
	Name    string `json:"name"`
	WonQty  int    `json:"wonQty"`
	LostQty int    `json:"lostQty"`
	NetQty  int    `json:"netQty"`
}

type SessionTally struct {
	SessionDBID int64       `json:"sessionDbId"`
	StartedAt   string      `json:"startedAt"`
	EndedAt     string      `json:"endedAt"`
	Games       int         `json:"games"`
	Items       []TallyItem `json:"items"`
}

// GetSessionTally fetches item win/loss totals from game_history_items for the given session's time window.
func (a *App) GetSessionTally(dbID int64) (SessionTally, error) {
	a.mu.Lock()
	db := a.db
	owner := a.ownerKey
	// Find session (current or closed)
	var startedAt, endedAt string
	if a.currentSession != nil && a.currentSession.DBID == dbID {
		startedAt = a.currentSession.StartedAt
		endedAt = a.currentSession.EndedAt
	} else {
		for _, s := range a.sessions {
			if s.DBID == dbID {
				startedAt = s.StartedAt
				endedAt = s.EndedAt
				break
			}
		}
	}
	a.mu.Unlock()

	tally := SessionTally{SessionDBID: dbID, StartedAt: startedAt, EndedAt: endedAt}

	if db == nil || startedAt == "" {
		return tally, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// started_at is TEXT with mixed formats — parse via parseHistoryStartedAt and cast in SQL
	startTime, ok := parseHistoryStartedAt(startedAt)
	if !ok {
		return tally, fmt.Errorf("invalid startedAt: %q", startedAt)
	}
	var endClause string
	var args []interface{}
	args = append(args, owner, startTime)
	if endedAt != "" {
		if endTime, ok := parseHistoryStartedAt(endedAt); ok {
			args = append(args, endTime)
			endClause = fmt.Sprintf("AND to_timestamp(trim(e.started_at), 'YYYY-MM-DD\"T\"HH24:MI:SS') AT TIME ZONE 'UTC' <= $%d", len(args))
		}
	}

	// Sum bet items (won by dealer = came in) and payout items (lost by dealer = went out)
	// Prioritize raffle_session_id, but fall back to time window for games with ID 0 (unassigned)
	query := fmt.Sprintf(`
		SELECT
			i.item_name,
			SUM(CASE WHEN i.item_type = 'bet'    THEN i.quantity ELSE 0 END) AS won_qty,
			SUM(CASE WHEN i.item_type = 'payout' THEN i.quantity ELSE 0 END) AS lost_qty,
			COUNT(DISTINCT e.id) AS games
		FROM game_history_entries e
		JOIN game_history_items i ON i.entry_id = e.id AND i.owner_key = e.owner_key
		WHERE e.owner_key = $1
		  AND (
			e.raffle_session_id = $2
			OR (e.raffle_session_id = 0 AND trim(COALESCE(e.started_at, '')) <> '' AND to_timestamp(trim(e.started_at), 'YYYY-MM-DD"T"HH24:MI:SS') AT TIME ZONE 'UTC' >= $3 %s)
		  )
		  AND e.status = 'Completed'
		GROUP BY i.item_name
		ORDER BY (SUM(CASE WHEN i.item_type = 'bet' THEN i.quantity ELSE 0 END) -
		          SUM(CASE WHEN i.item_type = 'payout' THEN i.quantity ELSE 0 END)) DESC,
		         i.item_name
	`, endClause)

	rows, err := db.Query(ctx, query, append([]interface{}{owner, dbID}, args[1:]...)...)
	if err != nil {
		return tally, err
	}
	defer rows.Close()

	totalGames := 0
	items := make([]TallyItem, 0)
	for rows.Next() {
		var t TallyItem
		var g int
		if err := rows.Scan(&t.Name, &t.WonQty, &t.LostQty, &g); err != nil {
			return tally, err
		}
		t.NetQty = t.WonQty - t.LostQty
		items = append(items, t)
		if g > totalGames {
			totalGames = g
		}
	}
	if err := rows.Err(); err != nil {
		return tally, err
	}

	// Fetch total distinct games in window separately (items query only counts games that had items)
	var totalGamesRow int
	_ = db.QueryRow(ctx, fmt.Sprintf(`
		SELECT COUNT(*) FROM game_history_entries
		WHERE owner_key = $1
		  AND (
			raffle_session_id = $2
			OR (raffle_session_id = 0 AND trim(COALESCE(started_at, '')) <> '' AND to_timestamp(trim(started_at), 'YYYY-MM-DD"T"HH24:MI:SS') AT TIME ZONE 'UTC' >= $3 %s)
		  )
		  AND status = 'Completed'
	`, endClause), append([]interface{}{owner, dbID}, args[1:]...)...).Scan(&totalGamesRow)
	if totalGamesRow > totalGames {
		totalGames = totalGamesRow
	}

	tally.Games = totalGames
	tally.Items = items
	return tally, nil
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

func (a *App) startRealtimeWatcher() {
	a.mu.Lock()
	if a.listenCancel != nil {
		a.mu.Unlock()
		return
	}
	if a.db == nil {
		a.mu.Unlock()
		a.logDebug("realtime watcher not started: db is nil")
		return
	}
	db := a.db
	ctx, cancel := context.WithCancel(context.Background())
	a.listenCancel = cancel
	a.mu.Unlock()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			conn, err := db.Acquire(ctx)
			if err != nil {
				a.logDebug("realtime watcher acquire failed: %v", err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
				continue
			}

			if _, err := conn.Conn().Exec(ctx, `LISTEN raffle_game_finished`); err != nil {
				a.logDebug("realtime watcher LISTEN failed: %v", err)
				conn.Release()
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
				continue
			}
			a.logDebug("realtime watcher connected on channel raffle_game_finished")

			for {
				n, err := conn.Conn().WaitForNotification(ctx)
				if err != nil {
					if ctx.Err() == nil {
						a.logDebug("realtime watcher notification error: %v", err)
					}
					break
				}

				payload := ""
				if n != nil {
					payload = strings.TrimSpace(n.Payload)
				}
				a.debugMu.Lock()
				a.lastNotifyAt = time.Now().UTC().Format(time.RFC3339)
				a.lastNotifyRaw = payload
				a.debugMu.Unlock()
				a.logDebug("realtime notify received: %s", payload)
				a.processNewBets()
			}

			conn.Release()
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
		}
	}()
}

func (a *App) processNewBets() {
	a.processMu.Lock()
	defer a.processMu.Unlock()

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

	var cursorEntryNum int64
	if strings.TrimSpace(cursorEntry) != "" {
		cursorEntryNum, _ = strconv.ParseInt(strings.TrimSpace(cursorEntry), 10, 64)
	}

	rows, err := db.Query(ctx, `
		SELECT
			e.id::text,
			e.player_name,
			e.started_at
		FROM game_history_entries e
		WHERE e.owner_key = $1
		  AND trim(COALESCE(e.started_at, '')) <> ''
		  AND (e.raffle_session_id = $2 OR e.raffle_session_id = 0)
		  AND e.raffle_session_id <> -1
		  AND lower(e.game) <> 'bandit'
		  AND (
			e.started_at::timestamptz > $3
			OR (e.started_at::timestamptz = $3 AND e.id::bigint > $4)
		  )
		ORDER BY e.started_at::timestamptz ASC, e.id ASC
		LIMIT 500
	`, owner, sessionDBID, cursorAt, cursorEntryNum)
	if err != nil {
		a.logDebug("poll query failed: %v", err)
		return
	}
	defer rows.Close()

	type dbBetRow struct {
		EntryID   string
		Player    string
		StartedAt string
	}
	type betRow struct {
		EntryID string
		Player  string
		EventAt time.Time
	}
	scannedCount := 0
	skippedInvalidTime := 0
	latestSeenAt := time.Time{}
	latestSeenEntry := ""
	batch := make([]betRow, 0)
	for rows.Next() {
		var raw dbBetRow
		if err := rows.Scan(&raw.EntryID, &raw.Player, &raw.StartedAt); err != nil {
			a.logDebug("poll scan failed: %v", err)
			return
		}
		scannedCount++

		eventAt, ok := parseHistoryStartedAt(raw.StartedAt)
		if !ok {
			skippedInvalidTime++
			continue
		}
		if eventAt.Before(sessionStartedAt) {
			continue
		}

		if latestSeenAt.IsZero() || eventAt.After(latestSeenAt) || (eventAt.Equal(latestSeenAt) && raw.EntryID > latestSeenEntry) {
			latestSeenAt = eventAt
			latestSeenEntry = raw.EntryID
		}

		batch = append(batch, betRow{
			EntryID: raw.EntryID,
			Player:  raw.Player,
			EventAt: eventAt,
		})
	}
	if err := rows.Err(); err != nil {
		a.logDebug("poll rows error: %v", err)
		return
	}
	latestSeenAtText := ""
	if !latestSeenAt.IsZero() {
		latestSeenAtText = latestSeenAt.Format(time.RFC3339)
	}
	a.logDebug(
		"poll inspected=%d accepted=%d skippedInvalidTime=%d sessionStart=%s cursorAt=%s cursorEntry=%q latestSeenAt=%s latestSeenEntry=%q",
		scannedCount,
		len(batch),
		skippedInvalidTime,
		sessionStartedAt.Format(time.RFC3339),
		cursorAt.Format(time.RFC3339),
		cursorEntry,
		latestSeenAtText,
		latestSeenEntry,
	)
	if len(batch) == 0 {
		a.debugMu.Lock()
		a.lastQueryRows = 0
		a.debugMu.Unlock()
		return
	}
	a.debugMu.Lock()
	a.lastQueryRows = len(batch)
	a.debugMu.Unlock()

	upserts := make([]RaffleParticipant, 0, len(batch))
	type ticketAnnounce struct {
		name      string
		tickets   int
		isNew     bool
		gamesAway int // if > 0, it's a progress shout
	}
	ticketAnnounces := make([]ticketAnnounce, 0, len(batch))
	lastCursorAt := cursorAt
	lastCursorEntry := cursorEntry

	a.mu.Lock()
	if a.currentSession == nil || a.currentSession.DBID != sessionDBID {
		a.mu.Unlock()
		return
	}

	announceEnabled := a.ticketAnnounceEnabled
	progressEnabled := a.ticketProgressEnabled
	resumedAt := a.currentSession.ResumedAt
	bonusEvery := a.currentSession.BonusEvery
	prizeName := strings.TrimSpace(a.currentSession.PrizeName)
	if prizeName == "" {
		prizeName = "Raffle"
	}
	prizeQty := a.currentSession.PrizeQty
	if prizeQty <= 0 {
		prizeQty = 1
	}
	prize := fmt.Sprintf("%s x%d", prizeName, prizeQty)

	for _, row := range batch {
		name := normalizeUsername(row.Player)
		key := normalizeUsernameKey(name)
		if key == "" {
			lastCursorAt = row.EventAt.UTC()
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
				Tickets:     effectiveTicketsForParticipant(1, a.currentSession.BonusEvery, 0),
				FirstBet:    row.EventAt.UTC().Format(time.RFC3339),
				LastBet:     row.EventAt.UTC().Format(time.RFC3339),
			}
			a.currentSession.Participants = append(a.currentSession.Participants, p)
			upserts = append(upserts, p)
			// New entrant: use combined shout, but suppress if this bet predates a resume
			if resumedAt.IsZero() || row.EventAt.UTC().After(resumedAt) {
				ticketAnnounces = append(ticketAnnounces, ticketAnnounce{name: p.Username, tickets: p.Tickets, isNew: true})
			}
		} else {
			p := &a.currentSession.Participants[idx]
			oldTickets := p.Tickets
			p.BetCount++
			p.Tickets = effectiveTicketsForParticipant(p.BetCount, a.currentSession.BonusEvery, p.ManualDelta)
			p.LastBet = row.EventAt.UTC().Format(time.RFC3339)
			upserts = append(upserts, *p)

			if resumedAt.IsZero() || row.EventAt.UTC().After(resumedAt) {
				if p.Tickets > oldTickets {
					if announceEnabled {
						ticketAnnounces = append(ticketAnnounces, ticketAnnounce{name: p.Username, tickets: p.Tickets})
					}
				} else if progressEnabled {
					gamesAway := bonusEvery - (p.BetCount % bonusEvery)
					if gamesAway > 0 && gamesAway <= 2 { // Only shout when close to avoid spam
						ticketAnnounces = append(ticketAnnounces, ticketAnnounce{name: p.Username, gamesAway: gamesAway})
					}
				}
			}
		}

		lastCursorAt = row.EventAt.UTC()
		lastCursorEntry = row.EntryID
	}

	sortParticipants(a.currentSession.Participants)

	a.currentSession.CursorAt = lastCursorAt
	a.currentSession.CursorEntry = lastCursorEntry
	a.mu.Unlock()

	for _, ta := range ticketAnnounces {
		if ta.isNew {
			a.shoutEntrant(ta.name, ta.tickets, prize)
		} else if ta.gamesAway > 0 {
			a.shoutTicketProgress(ta.name, ta.gamesAway, prize)
		} else if announceEnabled {
			a.shoutTicketCount(ta.name, ta.tickets, prize)
		}
	}

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

	if len(upserts) > 0 {
		go func() {
			if err := a.postOrUpdateRaffleWebhook(nil, true, "ticket-gain"); err != nil {
				a.logDebug("auto-patch ticket gain failed: %v", err)
			}
		}()
	}
}

func (a *App) shoutTicketProgress(name string, gamesAway int, prize string) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return
	}
	var msg string
	if gamesAway == 1 {
		msg = fmt.Sprintf("%s! Only 1 more game for another %s ticket! 🎟️", trimmed, prize)
	} else {
		msg = fmt.Sprintf("%s! Only %d more games for another %s ticket! 🎟️", trimmed, gamesAway, prize)
	}
	ext.Send(out.SHOUT, msg)
	a.debugMu.Lock()
	a.lastShout = msg
	a.debugMu.Unlock()
	a.logDebug("progress shout sent: %s", msg)
}

func (a *App) shoutEntrant(name string, entries int, prize string) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return
	}
	msg := fmt.Sprintf("Congrats %s! Entered raffle for %s! See Discord for the draw!", trimmed, prize)
	ext.Send(out.SHOUT, msg)
	a.debugMu.Lock()
	a.lastShout = msg
	a.debugMu.Unlock()
	a.logDebug("shout sent: %s", msg)
}

func (a *App) shoutTicketCount(name string, entries int, prize string) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return
	}
	msg := fmt.Sprintf("%s now has %d entries for %s! See Discord!", trimmed, entries, prize)
	ext.Send(out.SHOUT, msg)
	a.debugMu.Lock()
	a.lastShout = msg
	a.debugMu.Unlock()
	a.logDebug("ticket shout sent: %s", msg)
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

func (a *App) persistSessionWebhookMessageID(sessionDBID int64, messageID string) error {
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
		 SET webhook_message_id = $1
		 WHERE id = $2 AND owner_key = $3`,
		strings.TrimSpace(messageID),
		sessionDBID,
		owner,
	)
	return err
}

func (a *App) saveSessionMeta(sessionDBID int64) error {
	a.mu.Lock()
	db := a.db
	owner := a.ownerKey

	// Start with global defaults
	raffleName := strings.TrimSpace(a.raffleName)
	prizeName := strings.TrimSpace(a.rafflePrizeName)
	prizeQty := a.rafflePrizeQty
	heroImageURL := strings.TrimSpace(a.raffleHeroImageURL)
	heroAttachmentID := strings.TrimSpace(a.raffleHeroAttachmentID)
	heroAttachmentFile := strings.TrimSpace(a.raffleHeroAttachmentFile)

	winnerName := ""
	winnerTickets := 0
	winnerOdds := ""
	var winnerDrawnAt *time.Time
	winnerMethod := ""
	winnerSummary := ""
	winnerProofURL := ""
	winnerProofID := ""
	winnerProofFile := ""
	sponsorEnabled := false
	sponsorName := ""
	sponsorRoomName := ""

	// Find the session to save metadata for
	var session *RaffleSession
	if a.currentSession != nil && a.currentSession.DBID == sessionDBID {
		session = a.currentSession
	} else {
		for i := range a.sessions {
			if a.sessions[i].DBID == sessionDBID {
				session = &a.sessions[i]
				break
			}
		}
	}

	if session != nil {
		if n := strings.TrimSpace(session.RaffleName); n != "" {
			raffleName = n
		}
		if n := strings.TrimSpace(session.PrizeName); n != "" {
			prizeName = n
		}
		if session.PrizeQty > 0 {
			prizeQty = session.PrizeQty
		}
		heroImageURL = strings.TrimSpace(session.HeroImageURL)
		heroAttachmentID = strings.TrimSpace(session.HeroAttachmentID)
		heroAttachmentFile = strings.TrimSpace(session.HeroAttachmentFile)

		winnerName = strings.TrimSpace(session.WinnerName)
		winnerTickets = session.WinnerTickets
		winnerOdds = strings.TrimSpace(session.WinnerOdds)
		if session.WinnerDrawnAt != "" {
			if t, err := time.Parse(time.RFC3339, session.WinnerDrawnAt); err == nil {
				ut := t.UTC()
				winnerDrawnAt = &ut
			}
		}
		winnerMethod = strings.TrimSpace(session.WinnerMethod)
		winnerSummary = strings.TrimSpace(session.WinnerSummary)
		winnerProofURL = strings.TrimSpace(session.WinnerProofURL)
		winnerProofID = strings.TrimSpace(session.WinnerProofID)
		winnerProofFile = strings.TrimSpace(session.WinnerProofFile)
		sponsorEnabled = session.SponsorEnabled
		sponsorName = strings.TrimSpace(session.SponsorName)
		sponsorRoomName = strings.TrimSpace(session.SponsorRoomName)
	}
	a.mu.Unlock()

	if db == nil || sessionDBID <= 0 {
		return nil
	}
	if raffleName == "" {
		raffleName = "Flame Raffle"
	}
	if prizeName == "" {
		prizeName = "Purple Dragon Lamp"
	}
	if prizeQty <= 0 {
		prizeQty = 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := db.Exec(ctx,
		`UPDATE raffle_sessions
		 SET raffle_name = $1, prize_name = $2, prize_qty = $3,
		     hero_image_url = $4, hero_attachment_id = $5, hero_attachment_file = $6,
		     winner_name = $7, winner_tickets = $8, winner_odds = $9, winner_drawn_at = $10,
		     winner_method = $11, winner_summary = $12, winner_proof_url = $13,
		     winner_proof_id = $14, winner_proof_file = $15,
		     sponsor_enabled = $16, sponsor_name = $17, sponsor_room_name = $18
		 WHERE id = $19 AND owner_key = $20`,
		raffleName, prizeName, prizeQty,
		heroImageURL, heroAttachmentID, heroAttachmentFile,
		winnerName, winnerTickets, winnerOdds, winnerDrawnAt,
		winnerMethod, winnerSummary, winnerProofURL,
		winnerProofID, winnerProofFile,
		sponsorEnabled, sponsorName, sponsorRoomName,
		sessionDBID, owner,
	)
	return err
}

func (a *App) deleteParticipant(sessionDBID int64, usernameKey string) error {
	a.mu.Lock()
	db := a.db
	owner := a.ownerKey
	a.mu.Unlock()
	if db == nil {
		return fmt.Errorf("db not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := db.Exec(ctx, `
		DELETE FROM raffle_participants
		WHERE session_id = $1 AND owner_key = $2 AND username_key = $3
	`, sessionDBID, owner, strings.TrimSpace(usernameKey))
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
				bet_count, ticket_count, manual_ticket_delta, first_bet_at, last_bet_at, updated_at
			) VALUES (
				$1,$2,$3,$4,
				$5,$6,$7,$8,$9,NOW()
			)
			ON CONFLICT (session_id, owner_key, username_key) DO UPDATE SET
				username = EXCLUDED.username,
				bet_count = EXCLUDED.bet_count,
				ticket_count = EXCLUDED.ticket_count,
				manual_ticket_delta = EXCLUDED.manual_ticket_delta,
				first_bet_at = LEAST(raffle_participants.first_bet_at, EXCLUDED.first_bet_at),
				last_bet_at = GREATEST(raffle_participants.last_bet_at, EXCLUDED.last_bet_at),
				updated_at = NOW()
		`, sessionDBID, owner, p.Username, p.UsernameKey, p.BetCount, p.Tickets, p.ManualDelta, firstAt, lastAt); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}


func (a *App) DeleteSession(dbID int64) (RaffleState, error) {
        a.mu.Lock()
        db := a.db
        owner := a.ownerKey
        if a.currentSession != nil && a.currentSession.DBID == dbID {
                a.currentSession = nil
                a.raffleMessageID = ""
        }
        idx := -1
        for i, s := range a.sessions {
                if s.DBID == dbID {
                        idx = i
                        break
                }
        }
        if idx != -1 {
                a.sessions = append(a.sessions[:idx], a.sessions[idx+1:]...)
        }
        a.mu.Unlock()

        if db != nil && dbID > 0 {
                ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
                defer cancel()
                _, err := db.Exec(ctx, `DELETE FROM raffle_sessions WHERE id = $1 AND owner_key = $2`, dbID, owner)
                if err != nil {
                        return a.GetState(), err
                }
        }
        a.emitUpdate()
        return a.GetState(), nil
}


func (a *App) PostWinnerProofForSession(dbID int64, imageDataURL string, imageFileName string) (string, error) {
	dataURL := strings.TrimSpace(imageDataURL)
	if dataURL == "" {
		return "", fmt.Errorf("winner proof image is required")
	}

	a.mu.Lock()
	var session *RaffleSession
	if a.currentSession != nil && a.currentSession.DBID == dbID {
		session = a.currentSession
	} else {
		for i := range a.sessions {
			if a.sessions[i].DBID == dbID {
				session = &a.sessions[i]
				break
			}
		}
	}
	if session == nil {
		a.mu.Unlock()
		return "", fmt.Errorf("session %d not found", dbID)
	}
	if session.WinnerName == "" {
		a.mu.Unlock()
		return "", fmt.Errorf("draw a winner first")
	}
	a.mu.Unlock()

	raw, mimeType, err := decodeImageDataURL(dataURL)
	if err != nil {
		return "", fmt.Errorf("proof image decode error: %w", err)
	}

	fileName := strings.TrimSpace(imageFileName)
	if fileName == "" {
		switch mimeType {
		case "image/jpeg":
			fileName = "winner-proof.jpg"
		case "image/gif":
			fileName = "winner-proof.gif"
		case "image/webp":
			fileName = "winner-proof.webp"
		default:
			fileName = "winner-proof.png"
		}
	}

	a.mu.Lock()
	a.pendingProofBytes = raw
	a.pendingProofFileName = fileName
	a.mu.Unlock()

	if err := a.postOrUpdateRaffleWebhook(session, true, "winner-proof"); err != nil {
		a.mu.Lock()
		a.pendingProofBytes = nil
		a.pendingProofFileName = ""
		a.mu.Unlock()
		return "", err
	}

	// Update DB with proof info if possible
	a.mu.Lock()
	proofURL := session.WinnerProofURL
	proofID := session.WinnerProofID
	proofFile := session.WinnerProofFile
	db := a.db
	owner := a.ownerKey
	a.mu.Unlock()

	if db != nil && dbID > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_, _ = db.Exec(ctx,
			`UPDATE raffle_sessions SET winner_proof_url = $1, winner_proof_id = $2, winner_proof_file = $3 WHERE id = $4 AND owner_key = $5`,
			proofURL, proofID, proofFile, dbID, owner,
		)
	}

	return "ok", nil
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
			webhook_message_id TEXT NOT NULL DEFAULT '',
			raffle_name TEXT NOT NULL DEFAULT 'Flame Raffle',
			prize_name TEXT NOT NULL DEFAULT 'Purple Dragon Lamp',
			prize_qty INTEGER NOT NULL DEFAULT 1,
			hero_image_url TEXT NOT NULL DEFAULT '',
			hero_attachment_id TEXT NOT NULL DEFAULT '',
			hero_attachment_file TEXT NOT NULL DEFAULT '',
			sponsor_enabled BOOLEAN NOT NULL DEFAULT FALSE,
			sponsor_name TEXT NOT NULL DEFAULT '',
			sponsor_room_name TEXT NOT NULL DEFAULT '',
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
			manual_ticket_delta INTEGER NOT NULL DEFAULT 0,
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
		`CREATE OR REPLACE FUNCTION notify_raffle_game_finished_from_entries()
		RETURNS TRIGGER AS $$
		BEGIN
			IF lower(trim(COALESCE(NEW.status, ''))) IN ('completed','issue')
			   AND trim(COALESCE(NEW.completed_at, '')) <> '' THEN
				PERFORM pg_notify(
					'raffle_game_finished',
					json_build_object(
						'owner_key', NEW.owner_key,
						'entry_id', NEW.id,
						'source', 'entries'
					)::text
				);
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql`,
		`CREATE OR REPLACE FUNCTION notify_raffle_trade_entry_insert()
		RETURNS TRIGGER AS $$
		BEGIN
			PERFORM pg_notify(
				'raffle_game_finished',
				json_build_object(
					'owner_key', NEW.owner_key,
					'entry_id', NEW.id,
					'source', 'trade_entries'
				)::text
			);
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql`,
		`DO $$
		BEGIN
			IF to_regclass('public.game_history_entries') IS NOT NULL THEN
				DROP TRIGGER IF EXISTS trg_raffle_game_finished_entries ON game_history_entries;
				CREATE TRIGGER trg_raffle_game_finished_entries
				AFTER INSERT OR UPDATE ON game_history_entries
				FOR EACH ROW
				EXECUTE FUNCTION notify_raffle_game_finished_from_entries();
			END IF;
		END $$`,
		`DO $$
		BEGIN
			IF to_regclass('public.trade_entries') IS NOT NULL THEN
				DROP TRIGGER IF EXISTS trg_raffle_trade_entries_insert ON trade_entries;
				CREATE TRIGGER trg_raffle_trade_entries_insert
				AFTER INSERT ON trade_entries
				FOR EACH ROW
				EXECUTE FUNCTION notify_raffle_trade_entry_insert();
			END IF;
		END $$`,
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
		`ALTER TABLE raffle_sessions ADD COLUMN IF NOT EXISTS webhook_message_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE raffle_sessions ADD COLUMN IF NOT EXISTS raffle_name TEXT NOT NULL DEFAULT 'Flame Raffle'`,
		`ALTER TABLE raffle_sessions ADD COLUMN IF NOT EXISTS prize_name TEXT NOT NULL DEFAULT 'Purple Dragon Lamp'`,
		`ALTER TABLE raffle_sessions ADD COLUMN IF NOT EXISTS prize_qty INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE raffle_sessions ADD COLUMN IF NOT EXISTS hero_image_url TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE raffle_sessions ADD COLUMN IF NOT EXISTS hero_attachment_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE raffle_sessions ADD COLUMN IF NOT EXISTS hero_attachment_file TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE raffle_sessions ADD COLUMN IF NOT EXISTS winner_name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE raffle_sessions ADD COLUMN IF NOT EXISTS winner_tickets INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE raffle_sessions ADD COLUMN IF NOT EXISTS winner_odds TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE raffle_sessions ADD COLUMN IF NOT EXISTS winner_drawn_at TIMESTAMPTZ NULL`,
		`ALTER TABLE raffle_sessions ADD COLUMN IF NOT EXISTS winner_method TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE raffle_sessions ADD COLUMN IF NOT EXISTS winner_summary TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE raffle_sessions ADD COLUMN IF NOT EXISTS winner_proof_url TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE raffle_sessions ADD COLUMN IF NOT EXISTS winner_proof_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE raffle_sessions ADD COLUMN IF NOT EXISTS winner_proof_file TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE raffle_sessions ADD COLUMN IF NOT EXISTS sponsor_enabled BOOLEAN NOT NULL DEFAULT FALSE`,
		`ALTER TABLE raffle_sessions ADD COLUMN IF NOT EXISTS sponsor_name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE raffle_sessions ADD COLUMN IF NOT EXISTS sponsor_room_name TEXT NOT NULL DEFAULT ''`,
		// Fix existing rows that still have the old hardcoded defaults
		`UPDATE raffle_sessions SET raffle_name = 'Flame Raffle' WHERE raffle_name = 'Weekend Raffle'`,
		`UPDATE raffle_sessions SET prize_name = 'Purple Dragon Lamp' WHERE prize_name = 'Mystery Prize'`,
		`UPDATE raffle_sessions SET prize_qty = 1 WHERE prize_qty <= 0`,
		`ALTER TABLE raffle_participants ADD COLUMN IF NOT EXISTS username_key TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE raffle_participants ADD COLUMN IF NOT EXISTS bet_count INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE raffle_participants ADD COLUMN IF NOT EXISTS ticket_count INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE raffle_participants ADD COLUMN IF NOT EXISTS manual_ticket_delta INTEGER NOT NULL DEFAULT 0`,
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
		SELECT id, started_at, scheduled_end_at, ended_at, bonus_every, last_seen_created_at, last_seen_entry_id, webhook_message_id,
		       raffle_name, prize_name, prize_qty, hero_image_url, hero_attachment_id, hero_attachment_file,
		       winner_name, winner_tickets, winner_odds, winner_drawn_at, winner_method, winner_summary,
		       winner_proof_url, winner_proof_id, winner_proof_file, sponsor_enabled, sponsor_name, sponsor_room_name
		FROM raffle_sessions
		WHERE owner_key = $1
		ORDER BY id ASC
	`, owner)
	if err != nil {
		return err
	}
	defer sRows.Close()

	type dbSession struct {
		id                 int64
		startedAt          time.Time
		scheduledEndAt     *time.Time
		endedAt            *time.Time
		bonusEvery         int
		cursorAt           time.Time
		cursorID           string
		webhookMessageID   string
		raffleName         string
		prizeName          string
		prizeQty           int
		heroImageURL       string
		heroAttachmentID   string
		heroAttachmentFile string
		winnerName         string
		winnerTickets      int
		winnerOdds         string
		winnerDrawnAt      *time.Time
		winnerMethod       string
		winnerSummary      string
		winnerProofURL     string
		winnerProofID      string
		winnerProofFile    string
		sponsorEnabled     bool
		sponsorName        string
		sponsorRoomName    string
	}
	sessionsByID := map[int64]*RaffleSession{}
	orderedIDs := make([]int64, 0)
	maxID := 0
	var current *RaffleSession

	type sessionMeta struct {
		raffleName         string
		prizeName          string
		prizeQty           int
		heroImageURL       string
		heroAttachmentID   string
		heroAttachmentFile string
		winnerName         string
		winnerTickets      int
		winnerOdds         string
		winnerDrawnAt      string
		winnerMethod       string
		winnerSummary      string
		winnerProofURL     string
		winnerProofID      string
		winnerProofFile    string
		sponsorEnabled     bool
		sponsorName        string
		sponsorRoomName    string
	}
	metaByID := map[int64]sessionMeta{}

	for sRows.Next() {
		var s dbSession
		if err := sRows.Scan(&s.id, &s.startedAt, &s.scheduledEndAt, &s.endedAt, &s.bonusEvery, &s.cursorAt, &s.cursorID, &s.webhookMessageID,
			&s.raffleName, &s.prizeName, &s.prizeQty, &s.heroImageURL, &s.heroAttachmentID, &s.heroAttachmentFile,
			&s.winnerName, &s.winnerTickets, &s.winnerOdds, &s.winnerDrawnAt, &s.winnerMethod, &s.winnerSummary,
			&s.winnerProofURL, &s.winnerProofID, &s.winnerProofFile, &s.sponsorEnabled, &s.sponsorName, &s.sponsorRoomName); err != nil {
			return err
		}
		rs := &RaffleSession{
			ID:                 int(s.id),
			StartedAt:          s.startedAt.UTC().Format(time.RFC3339),
			RaffleName:         strings.TrimSpace(s.raffleName),
			PrizeName:          strings.TrimSpace(s.prizeName),
			PrizeQty:           s.prizeQty,
			BonusEvery:         s.bonusEvery,
			WebhookMessageID:   strings.TrimSpace(s.webhookMessageID),
			Participants:       []RaffleParticipant{},
			DBID:               s.id,
			CursorAt:           s.cursorAt.UTC(),
			CursorEntry:        s.cursorID,
			HeroImageURL:       strings.TrimSpace(s.heroImageURL),
			HeroAttachmentID:   strings.TrimSpace(s.heroAttachmentID),
			HeroAttachmentFile: strings.TrimSpace(s.heroAttachmentFile),
			WinnerName:         strings.TrimSpace(s.winnerName),
			WinnerTickets:      s.winnerTickets,
			WinnerOdds:         strings.TrimSpace(s.winnerOdds),
			WinnerMethod:       strings.TrimSpace(s.winnerMethod),
			WinnerSummary:      strings.TrimSpace(s.winnerSummary),
			WinnerProofURL:     strings.TrimSpace(s.winnerProofURL),
			WinnerProofID:      strings.TrimSpace(s.winnerProofID),
			WinnerProofFile:    strings.TrimSpace(s.winnerProofFile),
			SponsorEnabled:     s.sponsorEnabled,
			SponsorName:        strings.TrimSpace(s.sponsorName),
			SponsorRoomName:    strings.TrimSpace(s.sponsorRoomName),
		}
		if s.winnerDrawnAt != nil {
			rs.WinnerDrawnAt = s.winnerDrawnAt.UTC().Format(time.RFC3339)
		}
		metaByID[s.id] = sessionMeta{
			raffleName:         strings.TrimSpace(s.raffleName),
			prizeName:          strings.TrimSpace(s.prizeName),
			prizeQty:           s.prizeQty,
			heroImageURL:       strings.TrimSpace(s.heroImageURL),
			heroAttachmentID:   strings.TrimSpace(s.heroAttachmentID),
			heroAttachmentFile: strings.TrimSpace(s.heroAttachmentFile),
			winnerName:         strings.TrimSpace(s.winnerName),
			winnerTickets:      s.winnerTickets,
			winnerOdds:         strings.TrimSpace(s.winnerOdds),
			winnerDrawnAt:      rs.WinnerDrawnAt,
			winnerMethod:       strings.TrimSpace(s.winnerMethod),
			winnerSummary:      strings.TrimSpace(s.winnerSummary),
			winnerProofURL:     strings.TrimSpace(s.winnerProofURL),
			winnerProofID:      strings.TrimSpace(s.winnerProofID),
			winnerProofFile:    strings.TrimSpace(s.winnerProofFile),
			sponsorEnabled:     s.sponsorEnabled,
			sponsorName:        strings.TrimSpace(s.sponsorName),
			sponsorRoomName:    strings.TrimSpace(s.sponsorRoomName),
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
		SELECT session_id, username, username_key, bet_count, ticket_count, manual_ticket_delta, first_bet_at, last_bet_at
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
		if err := pRows.Scan(&sessionID, &p.Username, &p.UsernameKey, &p.BetCount, &p.Tickets, &p.ManualDelta, &firstAt, &lastAt); err != nil {
			return err
		}
		if p.Tickets != effectiveTicketsForParticipant(p.BetCount, sessionsByID[sessionID].BonusEvery, p.ManualDelta) {
			p.Tickets = effectiveTicketsForParticipant(p.BetCount, sessionsByID[sessionID].BonusEvery, p.ManualDelta)
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
		sortParticipants(s.Participants)
		if s.EndedAt == "" && current == nil {
			current = s
			continue
		}
		closed = append(closed, *s)
	}

	a.mu.Lock()
	a.sessions = closed
	a.currentSession = current
	if a.currentSession != nil {
		a.raffleMessageID = strings.TrimSpace(a.currentSession.WebhookMessageID)
	} else {
		a.raffleMessageID = ""
	}
	if current != nil {
		if m, ok := metaByID[current.DBID]; ok {
			if m.raffleName != "" {
				a.raffleName = m.raffleName
				current.RaffleName = m.raffleName
			}
			if m.prizeName != "" {
				a.rafflePrizeName = m.prizeName
				current.PrizeName = m.prizeName
			}
			if m.prizeQty > 0 {
				a.rafflePrizeQty = m.prizeQty
				current.PrizeQty = m.prizeQty
			}
			a.raffleHeroImageURL = m.heroImageURL
			a.raffleHeroAttachmentID = m.heroAttachmentID
			a.raffleHeroAttachmentFile = m.heroAttachmentFile
			current.HeroImageURL = m.heroImageURL
			current.HeroAttachmentID = m.heroAttachmentID
			current.HeroAttachmentFile = m.heroAttachmentFile
			a.sponsorEnabled = m.sponsorEnabled
			a.sponsorName = m.sponsorName
			a.sponsorRoomName = m.sponsorRoomName
			current.SponsorEnabled = m.sponsorEnabled
			current.SponsorName = m.sponsorName
			current.SponsorRoomName = m.sponsorRoomName
		}
	}
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

	// Set inRoom from USERS packets too — fires even if already in a room when the ext opens
	ext.Intercept(in.USERS, in.SPACENODEUSERS).With(func(e *g.Intercept) {
		a.mu.Lock()
		if !a.inRoom {
			a.inRoom = true
			a.mu.Unlock()
			a.logDebug("inRoom set via USERS packet")
			a.emitUpdate()
		} else {
			a.mu.Unlock()
		}
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
