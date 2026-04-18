package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	g "xabbo.b7c.io/goearth"
	gencoding "xabbo.b7c.io/goearth/encoding"
	"xabbo.b7c.io/goearth/shockwave/in"
	"xabbo.b7c.io/goearth/shockwave/out"
	room "xabbo.b7c.io/goearth/shockwave/room"
)

// Global variables for dice management, rolling state, mutex, and wait group
var (
	diceList                             []*Dice
	mutedDuration                        int
	isMuted                              bool
	currentSum                           int
	awaitingTradeOpen                    bool
	dealerTradeWindowOpen                bool
	dealerAcceptingTrades                bool
	tradeOpenCount                       int
	tradeCloseCount                      int
	lastTradePartnerID                   int
	lastTradePartnerName                 string
	lastTradePartnerToken                string
	stableTradePartnerID                 int
	stableTradePartnerName               string
	stableTradePartnerToken              string
	tradeStarterTradeID                  int
	tradeStarterChatID                   int
	tradeStarterName                     string
	tradeStarterToken                    string
	tradeStarterLocked                   bool
	tradeAutoFlowID                      int
	tradeAutoAccepted                    bool
	tradeAutoConfirmed                   bool
	tradeAutoAcceptPending               bool
	tradeAutoConfirmPending              bool
	tradeCompleted                       bool
	tradeCloseAnnounced                  bool
	suppressNextTradeCloseAnnouncement   bool
	ignoreNextGuardCloseRecovery         bool
	hiddenBlockedTradeCleanupPending     bool
	lastTradeCoverageNotice              string
	lastTradeBlockNotice                 string
	awaitingGameChoice                   bool
	awaitingGameChoicePartnerID          int
	awaitingGameChoicePartnerName        string
	awaitingBlackjackDecision            bool
	awaitingBlackjackDecisionPartnerID   int
	awaitingBlackjackDecisionPartnerName string
	blackjackRoundActive                 bool
	blackjackPlayerTurn                  bool
	blackjackPlayerTotal                 int
	blackjackDealerTotal                 int
	blackjackPlayerName                  string
	pokerSequenceStage                   int
	blackjackHitInFlight                 bool
	blackjackNextHitIndex                int
	// 13-game state
	awaiting13Decision            bool
	awaiting13DecisionPartnerID   int
	awaiting13DecisionPartnerName string
	thirteenRoundActive           bool
	thirteenPlayerTurn            bool
	thirteenPlayerTotal           int
	thirteenDealerTotal           int
	thirteenPlayerName            string
	thirteenHitInFlight           bool
	thirteenNextHitIndex          int
	// Tri (High / Low) state
	awaitingTriChoice            bool
	awaitingTriChoicePartnerID   int
	awaitingTriChoicePartnerName string
	triRoundActive               bool
	triPlayerTurn                bool
	triMode                      string // "high" or "low"
	triPlayerTotal               int
	triDealerTotal               int
	triPlayerName                string
	pokerSequencePlayerName      string
	pokerSequencePlayerResult    PokerHandResult
	pokerSequencePlayerHand      string
	payoutActive                 bool
	payoutTradeActive            bool
	payoutTargetID               int
	payoutTargetName             string
	payoutAttempts               int
	payoutSessionID              int
	payoutTradeSent              bool
	payoutExpectedAddCount       int
	payoutActualAddCount         int
	lastPayoutCancelNoticeAt     time.Time

	// Payout retry/monitor state
	payoutResponseTimeoutMonitorID int
	payoutResponseTimeoutActive    bool
	payoutResponseTimeoutAttempts  int
	payoutCancelCount              int
	underfundedTradeMonitorID      int
	underfundedTradeMonitorNotice  string
	shortageMonitorID              int
	shortageMonitorActive          bool
	shortageMonitorDeadline        time.Time
	// Trade limit monitor state (gives partner time to correct invalid offer)
	tradeLimitMonitorID       int
	tradeLimitMonitorActive   bool
	tradeLimitMonitorDeadline time.Time
	tradeLimitGracePeriod     = 30 * time.Second
	lastTradeLimitNotice      string
	// Rate-limiting for public shouts triggered by trade coverage/limit
	lastTradeCoverageShoutAt time.Time
	lastTradeLimitShoutAt    time.Time
	tradeShoutCooldown       = 45 * time.Second
	// Whether the partner has accepted during the current open trade.
	// Keep this sticky until the trade closes so we can re-arm auto-accept
	// after temporary limit violations are corrected without forcing the
	// player to toggle accept again.
	partnerTradeAccepted bool
	// Whether a trade-limit warning was previously active (used to detect
	// transitions from invalid -> valid and to shout a one-time "now valid"
	// message).
	tradeLimitWasActive    bool
	lastTradeOpenData      string
	lastTradeOpen          string
	tradeOpen              bool
	messageQueue           []string
	isPokerRolling         bool
	isTriRolling           bool
	isBJRolling            bool
	is13Rolling            bool
	is13Hitting            bool
	isHitting              bool
	isClosing              bool
	ChatIsDisabled         bool
	mutex                  sync.Mutex
	resultsWaitGroup       sync.WaitGroup
	rollDelay              = 550 * time.Millisecond
	stripNextDelay         = 2250 * time.Millisecond
	stripGetNewPayload     = "new"
	stripGetNextPayload    = "next"
	tradeUserPattern       = regexp.MustCompile(`\[(\d+)\]`)
	stripItemNameRe        = regexp.MustCompile(`(?:CF_\d+_[a-z][a-z_]*|[a-z][a-z0-9_]*_[a-z0-9_]+)(?:\*\d+)?`)
	gameChoiceCleanupRe    = regexp.MustCompile(`[^a-z0-9]+`)
	roomEntities           = map[int]room.Entity{}
	roomMu                 sync.Mutex
	lastRoomUsersRequestAt time.Time
	roomUsersReqMu         sync.Mutex
	roomReadySeen          bool
	// Canonical USERS28 registry: source of truth is the Python parser only.
	users28Canonical          = map[string]ParsedUsers28User{}
	users28ByToken            = map[string]ParsedUsers28User{}
	users28ByIndex            = map[int]ParsedUsers28User{} // parsed chat_id -> user
	users28ByTradeID          = map[int]ParsedUsers28User{} // parsed trade_id -> user
	recentTradePartnerByToken = map[string]string{}
	recentTradePartnerSeenAt  = map[string]time.Time{}
	recentTradePartnerMu      sync.Mutex
	users28Mu                 sync.Mutex
	headerSniffUntil          time.Time
	headerSniffSeen           = map[uint16]bool{}
	headerSniffMu             sync.Mutex
	currentTradeItems         []TradeItem
	currentOwnTradeItems      []TradeItem
	tradeItemsMu              sync.Mutex
	lastAddItemWasOurs        bool
	// lastAddItemByUsAt records when we observed an outgoing TRADE_ADDITEM
	// packet. Use this timestamp in debugging to detect races between the
	// outgoing add and the subsequent server TRADE_ITEMS update.
	lastAddItemByUsAt      time.Time
	addItemMu              sync.Mutex
	currentHandItems       []TradeItem
	currentHandItemIDs     map[string][]int
	tradeHandSnapshot      []TradeItem
	tradeHandSnapshotReady bool
	// When true, the dealer will only refresh the frozen trade-hand
	// snapshot at controlled points: once before announcing Dealer Open
	// and after a game completes. Mid-trade strip scans will not update
	// the frozen snapshot while this policy is active.
	strictTradeSnapshotLifecycle bool = true
	handItemsMu                  sync.Mutex
	// lastAllTradeItems stores the last full TRADE_ITEMS (all items) packet
	// so we can compute deltas between successive full-state packets. This
	// helps reliably attribute the first added item to the correct side.
	lastAllTradeItems []TradeItem
	// partnerAcceptedSnapshot holds a copy of the trade full-state the partner
	// accepted (set when incoming header 109 is received). Used to determine
	// whether a subsequent TRADE_ITEMS packet actually changes the accepted
	// contents or merely restores them; allows re-arming auto-accept on
	// invalid->valid transitions when appropriate.
	partnerAcceptedSnapshot    []TradeItem
	gameBetItems               []TradeItem
	stripScanMu                sync.Mutex
	stripScanActive            bool
	stripScanSessionID         = 0
	stripScanPageCount         = 0
	stripScanLastPacketAt      time.Time
	stripScanStartedAt         time.Time
	stripScanSeenItemIDs       = map[int]struct{}{}
	stripScanCounts            = map[string]int{}
	stripScanItemIDs           = map[string][]int{}
	knownDiceIDs               = map[int]struct{}{}
	fakeDiceTestingMode        bool
	dealerOpenHeartbeatID      int
	dealerOpenHeartbeatActive  bool
	gameChoiceTimeoutMonitorID int
	gameChoiceTimeoutActive    bool
	gameChoiceUnreadableWarned bool
	dealerResyncInProgress     bool
	// When true, the UI has enabled dice setup mode and incoming dice IDs
	// should be recorded for the bot setup. Must be enabled by the Start Casino
	// button in the frontend.
	diceSetupActive bool
	// When true the casino frontend has started — kept for UI state only.
	casinoActive bool
	// When true the casino has a valid completed dice setup and the bot can run.
	casinoReady                 bool
	lastOutgoingTradeOpenID     int
	lastOutgoingTradeOpenAt     time.Time
	tradeOpenStateMu            sync.Mutex
	tradeWindowTimeoutMonitorID int
	tradeWindowOpenedAt         time.Time
	tradeWindowDeadline         time.Time
	tradeWindowTimeoutActive    bool
	blockAllTrades              = true
	dealerAnnouncementsEnabled  = true
	// Auto shout configuration
	autoShoutEnabled bool
	autoShoutPhrase  string
	autoShoutSeconds int = 30

	// Block recommended-rooms incoming packet configuration
	blockRecommendedRooms bool
	blockRecommendedMu    sync.Mutex

	// Block slide-object-bundle incoming packet configuration
	blockSlideObjectBundle bool
	blockSlideObjectMu     sync.Mutex

	// Additional incoming packet block flags
	blockStatusEffects      bool
	blockStatusEffectsMu    sync.Mutex
	blockRemoveBuddy        bool
	blockRemoveBuddyMu      sync.Mutex
	blockFriendListUpdate   bool
	blockFriendListUpdateMu sync.Mutex

	// Poll event eligibility outgoing block flag
	blockPollEventEligibilityOutgoing   bool
	blockPollEventEligibilityOutgoingMu sync.Mutex

	// Block recommended-rooms outgoing packet configuration
	blockOutgoingRecommendedRooms bool
	blockOutgoingRecommendedMu    sync.Mutex

	// Block slide-object-bundle outgoing packet configuration
	blockSlideObjectOutgoing   bool
	blockSlideObjectOutgoingMu sync.Mutex

	// Incoming trade limits (configured at startup)
	maxTradeUniqueItems     int = 5
	maxTradeQuantityPerItem int = 50

	autoShoutStopChan chan struct{}
	autoShoutMu       sync.Mutex
)

type TradeItem struct {
	Name     string
	Quantity int
	RawData  string // Store raw field for debugging
}

// LiveGameSummary is an anonymized, frontend-friendly summary of a completed
// game. It intentionally does not expose player names — `Winner` is mapped
// to "Player"/"Dealer"/"Unknown".
type LiveGameSummary struct {
	ID          string      `json:"id"`
	Game        string      `json:"game"`
	Winner      string      `json:"winner"`
	Outcome     string      `json:"outcome"`
	StartedAt   string      `json:"startedAt,omitempty"`
	CompletedAt string      `json:"completedAt,omitempty"`
	BetItems    []TradeItem `json:"betItems,omitempty"`
	PayoutItems []TradeItem `json:"payoutItems,omitempty"`
}

type LiveDealerStatusPayload struct {
	LastSeenAt         string            `json:"lastSeenAt"`
	DealerOpen         bool              `json:"dealerOpen"`
	TradeOpen          bool              `json:"tradeOpen"`
	GameActive         bool              `json:"gameActive"`
	SnapshotReady      bool              `json:"snapshotReady"`
	DealerName         string            `json:"dealerName"`
	RoomName           string            `json:"roomName"`
	MaxUniqueItems     int               `json:"maxUniqueItems"`
	MaxQuantityPerItem int               `json:"maxQuantityPerItem"`
	Snapshot           []TradeItem       `json:"snapshot,omitempty"`
	RecentGames        []LiveGameSummary `json:"recentGames,omitempty"`
}

type tradeLimitViolation struct {
	TooManyUniqueItems bool
	TooMuchQuantity    bool
	UniqueCount        int
	MaxUnique          int
	MaxPerItem         int
	OverLimitItems     []TradeItem
}

// getTradeLimitViolation inspects the parsed list of trade items and returns
// a violation struct if limits are exceeded, or nil otherwise.
func getTradeLimitViolation(items []TradeItem) *tradeLimitViolation {
	if len(items) == 0 {
		return nil
	}
	v := &tradeLimitViolation{
		UniqueCount: len(items),
		MaxUnique:   maxTradeUniqueItems,
		MaxPerItem:  maxTradeQuantityPerItem,
	}
	if v.UniqueCount > v.MaxUnique {
		v.TooManyUniqueItems = true
	}
	for _, it := range items {
		over := it.Quantity > v.MaxPerItem
		log.Printf("[TRADE_LIMIT_DEBUG] check item=%q qty=%d max=%d over=%t", it.Name, it.Quantity, v.MaxPerItem, over)
		if over {
			v.OverLimitItems = append(v.OverLimitItems, it)
		}
	}
	if len(v.OverLimitItems) > 0 {
		v.TooMuchQuantity = true
	}
	log.Printf("[TRADE_LIMIT_DEBUG] unique=%d maxUnique=%d tooManyUnique=%t tooMuchQuantity=%t", v.UniqueCount, v.MaxUnique, v.TooManyUniqueItems, v.TooMuchQuantity)
	if !v.TooManyUniqueItems && !v.TooMuchQuantity {
		return nil
	}
	return v
}

func formatTradeLimitViolationMessage(v *tradeLimitViolation) string {
	if v == nil {
		return ""
	}
	parts := []string{}

	if v.TooManyUniqueItems {
		parts = append(parts, fmt.Sprintf("max %d types", v.MaxUnique))
	}
	if v.TooMuchQuantity {
		parts = append(parts, fmt.Sprintf("max %d each", v.MaxPerItem))
	}

	base := fmt.Sprintf("Trade over limit — remove items within %ds", int(tradeLimitGracePeriod.Seconds()))
	if len(parts) > 0 {
		return base + ": " + strings.Join(parts, ", ")
	}
	return base
}

// equalTradeItemLists compares two slices of TradeItem for equality by
// canonicalizing to a name->quantity map and comparing maps. Order is
// ignored. Useful for detecting whether a full TRADE_ITEMS packet matches
// a previously recorded snapshot (e.g., partnerAcceptedSnapshot).
func equalTradeItemLists(a, b []TradeItem) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	ma := make(map[string]int)
	mb := make(map[string]int)
	for _, it := range a {
		ma[strings.ToLower(strings.TrimSpace(it.Name))] += it.Quantity
	}
	for _, it := range b {
		mb[strings.ToLower(strings.TrimSpace(it.Name))] += it.Quantity
	}
	if len(ma) != len(mb) {
		return false
	}
	for k, v := range ma {
		if mb[k] != v {
			return false
		}
	}
	return true
}

// rejectTradeForLimitViolation announces the reason, closes the trade and reopens the dealer.
func (a *App) rejectTradeForLimitViolation(v *tradeLimitViolation) {
	if v == nil {
		return
	}
	msg := formatTradeLimitViolationMessage(v)
	a.AddLogMsg("[TRADE_LIMIT] " + msg)
	// Decide whether to shout. Prefer to dedupe by message text but also
	// enforce a cooldown so repeated partner changes don't spam public chat
	// and trigger self-mute. We still log the event regardless.
	changed := msg != lastTradeLimitNotice
	lastTradeLimitNotice = msg

	now := time.Now()
	allowed := lastTradeLimitShoutAt.IsZero() || now.Sub(lastTradeLimitShoutAt) > tradeShoutCooldown
	if (v.TooMuchQuantity || changed) && allowed {
		lastTradeLimitShoutAt = now
		go func(m string) {
			time.Sleep(350 * time.Millisecond)
			ext.Send(out.SHOUT, m)
		}(msg)
	} else {
		a.AddLogMsg("[TRADE_LIMIT] shout suppressed by cooldown/suppression")
	}

	// Start a short-lived grace timer instead of closing immediately so the
	// partner has a chance to remove offending items (similar to shortage flow).
	// Mark that a trade-limit warning is active so we can detect when it
	// transitions back to a valid state.
	tradeLimitWasActive = true
	startTradeLimitMonitor(a, tradeLimitGracePeriod)
}

func normalizeUsers28Name(name string, tokenHex string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return name
	}
	if tokenHex != "" {
		if tokenBytes, err := hex.DecodeString(tokenHex); err == nil {
			tokenText := string(tokenBytes)
			if strings.HasPrefix(name, tokenText) && len(name) > len(tokenText) {
				return strings.TrimSpace(name[len(tokenText):])
			}
		}
	}
	return name
}

type RoomIdentityEntry struct {
	Name      string `json:"name"`
	Short     string `json:"short"`
	ChatIndex int    `json:"chatIndex"`
	RoomIndex int    `json:"roomIndex"`
	Token     string `json:"token"`
	TradeID   string `json:"tradeId,omitempty"`
	EntityID  string `json:"entityId,omitempty"`
}

// ParsedUsers28Result is the exact structured result returned by the Python helper.
type ParsedUsers28Result struct {
	Users []ParsedUsers28User `json:"users"`
}

type ParsedUsers28User struct {
	Username     string `json:"username"`
	TradeID      int    `json:"trade_id"`
	TradeIDRaw   string `json:"trade_id_raw"`
	ChatID       int    `json:"chat_id"`
	ChatIDRaw    string `json:"chat_id_raw"`
	EntityID     string `json:"entity_id,omitempty"`
	Figure       string `json:"figure,omitempty"`
	Sex          string `json:"sex,omitempty"`
	Motto        string `json:"motto,omitempty"`
	TokenHex     string `json:"token_hex,omitempty"`
	RawNameBlock string `json:"raw_name_block,omitempty"`
}

type ParsedUsers28Trade struct {
	Item       string `json:"item"`
	Colors     string `json:"colors"`
	FieldIndex int    `json:"field_index"`
}

type GameHistoryEntry struct {
	ID           string      `json:"id"`
	PlayerName   string      `json:"playerName"`
	StartedAt    string      `json:"startedAt"`
	UpdatedAt    string      `json:"updatedAt"`
	CompletedAt  string      `json:"completedAt,omitempty"`
	Game         string      `json:"game"`
	Winner       string      `json:"winner"`
	Status       string      `json:"status"`
	Issue        bool        `json:"issue"`
	IssueReason  string      `json:"issueReason"`
	PlayerResult string      `json:"playerResult"`
	DealerResult string      `json:"dealerResult"`
	BetItems     []TradeItem `json:"betItems"`
	PayoutItems  []TradeItem `json:"payoutItems"`
	Notes        []string    `json:"notes"`
}

type tradeShortage struct {
	Name        string
	Required    int
	Have        int
	PayoutTotal int
	HaveHand    int
	Incoming    int
}

type App struct {
	ext                  *g.Ext
	assets               embed.FS
	log                  []string
	debugLog             []string
	logMu                sync.Mutex
	chatLog              []string
	chatLogMu            sync.Mutex
	gameHistory          []GameHistoryEntry
	gameHistoryMu        sync.Mutex
	currentGameHistoryID string
	ctx                  context.Context
	currentDealerName    string
	currentRoomName      string
	users28PythonExec    string
	users28ParserScript  string
}

type PokerDisplayConfig struct {
	FiveOfAKind  string `json:"five_of_a_kind"`
	FourOfAKind  string `json:"four_of_a_kind"`
	FullHouse    string `json:"full_house"`
	HighStraight string `json:"high_straight"`
	LowStraight  string `json:"low_straight"`
	ThreeOfAKind string `json:"three_of_a_kind"`
	TwoPair      string `json:"two_pair"`
	OnePair      string `json:"one_pair"`
	Nothing      string `json:"nothing"`
}

// AutoShoutConfig holds frontend-friendly auto shout settings.
type AutoShoutConfig struct {
	Enabled bool   `json:"enabled"`
	Phrase  string `json:"phrase"`
	Seconds int    `json:"seconds"`
}

func NewApp(ext *g.Ext, assets embed.FS) *App {
	a := &App{
		ext:    ext,
		assets: assets,
	}
	a.initUsers28ParserCommand()
	return a
}

// getCurrentDealerName returns the configured dealer name, preferring an
// explicitly set value on the App instance, then environment overrides,
// then a sensible default.

func (a *App) initUsers28ParserCommand() {
	if a == nil {
		return
	}
	if strings.TrimSpace(a.users28ParserScript) == "" {
		a.users28ParserScript = filepath.Join("scripts", "parse_users28.py")
	}
	if strings.TrimSpace(a.users28PythonExec) != "" {
		return
	}
	if p, err := exec.LookPath("python3"); err == nil {
		a.users28PythonExec = p
		return
	}
	if p, err := exec.LookPath("python"); err == nil {
		a.users28PythonExec = p
		return
	}
	// leave empty and let the runner report a clear error later
}

func (a *App) getCurrentDealerName() string {
	if a != nil {
		if n := strings.TrimSpace(a.currentDealerName); n != "" {
			return n
		}
	}
	if v := os.Getenv("LIVE_SYNC_DEALER_NAME"); v != "" {
		return v
	}
	if v := os.Getenv("USERNAME"); v != "" {
		return v
	}
	return "Dealer"
}

// getCurrentRoomName returns the configured room name, preferring an
// explicitly set value on the App instance, then an optional environment
// override, otherwise empty string.
func (a *App) getCurrentRoomName() string {
	if a != nil {
		if n := strings.TrimSpace(a.currentRoomName); n != "" {
			return n
		}
	}
	if v := os.Getenv("LIVE_SYNC_ROOM_NAME"); v != "" {
		return v
	}
	return ""
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.loadGameHistory()
	a.setupExt()
	go func() {
		a.runExt()
	}()
	go func() {
		time.Sleep(1200 * time.Millisecond)
		requestRoomUsers(a)

		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			requestRoomUsers(a)
		}
	}()
	go func() {
		deadline := time.Now().Add(12 * time.Second)
		for time.Now().Before(deadline) {
			roomMu.Lock()
			ready := roomReadySeen
			roomMu.Unlock()
			if ready {
				break
			}
			time.Sleep(250 * time.Millisecond)
		}

		scanID := a.requestPlayerStrip(true)
		if ok := waitForStripScanCompletion(scanID, 20*time.Second); ok {
			a.AddLogMsg(fmt.Sprintf("[STRIP] initial hand sync complete (session=%d)", scanID))
			if !strictTradeSnapshotLifecycle || !tradeHandSnapshotReady {
				a.captureTradeHandSnapshot()
			} else {
				a.AddLogMsg("[STRIP] strict snapshot lifecycle active and snapshot already ready; skipping initial capture")
			}
		} else {
			a.AddLogMsg(fmt.Sprintf("[STRIP] initial hand sync timeout (session=%d)", scanID))
			a.finalizeStripScan(scanID, "startup timeout")
		}

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if !casinoActive && !dealerAcceptingTrades && !tradeOpen {
				continue
			}
			a.requestPlayerStrip(false)
		}
	}()

	// Send an initial heartbeat so external dashboards receive immediate status
	a.sendLiveDealerStatus(dealerAcceptingTrades, a.getCurrentDealerName())

	// Start a periodic heartbeat to keep the website's lastSeenAt fresh.
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			a.sendLiveDealerStatus(dealerAcceptingTrades, a.getCurrentDealerName())
		}
	}()

}

func (a *App) LoadConfig() *PokerDisplayConfig {
	configFilePath := getConfigFilePath()

	file, err := os.Open(configFilePath)
	if err != nil {
		a.AddLogMsg("Config file not found, loading default values")
		return &PokerDisplayConfig{
			FiveOfAKind:  "Five of a kind: %s",
			FourOfAKind:  "Four of a kind: %s",
			FullHouse:    "Full House: %s",
			HighStraight: "High Str8",
			LowStraight:  "Low Str8",
			ThreeOfAKind: "Three of a kind: %s",
			TwoPair:      "Two Pair: %ss",
			OnePair:      "One Pair: %ss",
			Nothing:      "Nothing",
		}
	}
	defer file.Close()

	var config PokerDisplayConfig
	if err := json.NewDecoder(file).Decode(&config); err != nil {
		a.AddLogMsg("Error decoding config file: " + err.Error())
		return nil
	}

	// Config file loaded successfully
	return &config
}

func (a *App) SaveConfig(config *PokerDisplayConfig) {
	configFilePath := getConfigFilePath()

	file, err := os.Create(configFilePath)
	if err != nil {
		a.AddLogMsg("Error creating config file: " + err.Error())
		return
	}
	defer file.Close()
	defer file.Close()

	if err := json.NewEncoder(file).Encode(config); err != nil {
		a.AddLogMsg("Error encoding config file: " + err.Error())
		return
	}

	a.AddLogMsg("Config file saved successfully")
}

// GetAutoShoutConfig returns the current auto-shout configuration.
func (a *App) GetAutoShoutConfig() AutoShoutConfig {
	autoShoutMu.Lock()
	defer autoShoutMu.Unlock()

	return AutoShoutConfig{
		Enabled: autoShoutEnabled,
		Phrase:  autoShoutPhrase,
		Seconds: autoShoutSeconds,
	}
}

// SaveAutoShoutConfig updates phrase and seconds. If auto-shout is
// currently enabled, the loop is restarted to apply interval changes.
func (a *App) SaveAutoShoutConfig(phrase string, seconds int) AutoShoutConfig {
	autoShoutMu.Lock()
	autoShoutPhrase = strings.TrimSpace(phrase)
	if seconds < 1 {
		seconds = 1
	}
	autoShoutSeconds = seconds
	wasEnabled := autoShoutEnabled
	autoShoutMu.Unlock()

	// If enabled, restart loop so interval changes apply immediately.
	if wasEnabled {
		// Toggle off then on to restart
		a.ToggleAutoShout(false)
		return a.ToggleAutoShout(true)
	}

	cfg := AutoShoutConfig{
		Enabled: wasEnabled,
		Phrase:  autoShoutPhrase,
		Seconds: autoShoutSeconds,
	}

	if a.ctx != nil {
		b, _ := json.Marshal(cfg)
		runtime.EventsEmit(a.ctx, "autoShoutUpdate", string(b))
	}

	return cfg
}

// BlockRecommendedConfig holds frontend-friendly block-recommend config.
type BlockRecommendedConfig struct {
	Enabled bool `json:"enabled"`
}

// GetBlockRecommendedRoomsConfig returns current blockRecommendedRooms setting.
func (a *App) GetBlockRecommendedRoomsConfig() BlockRecommendedConfig {
	blockRecommendedMu.Lock()
	defer blockRecommendedMu.Unlock()
	return BlockRecommendedConfig{Enabled: blockRecommendedRooms}
}

// ToggleBlockRecommendedRooms enables/disables blocking of recommended-room packets.
func (a *App) ToggleBlockRecommendedRooms(enabled bool) BlockRecommendedConfig {
	blockRecommendedMu.Lock()
	blockRecommendedRooms = enabled
	blockRecommendedMu.Unlock()

	cfg := BlockRecommendedConfig{Enabled: blockRecommendedRooms}
	if a.ctx != nil {
		b, _ := json.Marshal(cfg)
		runtime.EventsEmit(a.ctx, "blockRecommendedUpdate", string(b))
	}

	return cfg
}

// BlockSlideObjectConfig holds frontend-friendly block config for the
// SLIDEOBJECTBUNDLE (header 230).
type BlockSlideObjectConfig struct {
	Enabled bool `json:"enabled"`
}

// GetBlockSlideObjectBundleConfig returns current blockSlideObjectBundle setting.
func (a *App) GetBlockSlideObjectBundleConfig() BlockSlideObjectConfig {
	blockSlideObjectMu.Lock()
	defer blockSlideObjectMu.Unlock()
	return BlockSlideObjectConfig{Enabled: blockSlideObjectBundle}
}

// ToggleBlockSlideObjectBundle enables/disables blocking of slide-object-bundle packets.
func (a *App) ToggleBlockSlideObjectBundle(enabled bool) BlockSlideObjectConfig {
	blockSlideObjectMu.Lock()
	blockSlideObjectBundle = enabled
	blockSlideObjectMu.Unlock()

	cfg := BlockSlideObjectConfig{Enabled: blockSlideObjectBundle}
	if a.ctx != nil {
		b, _ := json.Marshal(cfg)
		runtime.EventsEmit(a.ctx, "blockSlideObjectBundleUpdate", string(b))
	}

	return cfg
}

// GetBlockStatusEffectsConfig returns current block setting for incoming STATUS_EFFECTS (1242).
func (a *App) GetBlockStatusEffectsConfig() BlockSlideObjectConfig {
	blockStatusEffectsMu.Lock()
	defer blockStatusEffectsMu.Unlock()
	return BlockSlideObjectConfig{Enabled: blockStatusEffects}
}

// ToggleBlockStatusEffects toggles blocking of incoming STATUS_EFFECTS packets.
func (a *App) ToggleBlockStatusEffects(enabled bool) BlockSlideObjectConfig {
	blockStatusEffectsMu.Lock()
	blockStatusEffects = enabled
	blockStatusEffectsMu.Unlock()

	cfg := BlockSlideObjectConfig{Enabled: blockStatusEffects}
	if a.ctx != nil {
		b, _ := json.Marshal(cfg)
		runtime.EventsEmit(a.ctx, "blockStatusEffectsUpdate", string(b))
	}

	return cfg
}

// GetBlockRemoveBuddyConfig returns current block setting for incoming REMOVE_BUDDY (138).
func (a *App) GetBlockRemoveBuddyConfig() BlockSlideObjectConfig {
	blockRemoveBuddyMu.Lock()
	defer blockRemoveBuddyMu.Unlock()
	return BlockSlideObjectConfig{Enabled: blockRemoveBuddy}
}

// ToggleBlockRemoveBuddy toggles blocking of incoming REMOVE_BUDDY packets.
func (a *App) ToggleBlockRemoveBuddy(enabled bool) BlockSlideObjectConfig {
	blockRemoveBuddyMu.Lock()
	blockRemoveBuddy = enabled
	blockRemoveBuddyMu.Unlock()

	cfg := BlockSlideObjectConfig{Enabled: blockRemoveBuddy}
	if a.ctx != nil {
		b, _ := json.Marshal(cfg)
		runtime.EventsEmit(a.ctx, "blockRemoveBuddyUpdate", string(b))
	}

	return cfg
}

// GetBlockFriendListUpdateConfig returns current block setting for incoming FRIEND_LIST_UPDATE (13).
func (a *App) GetBlockFriendListUpdateConfig() BlockSlideObjectConfig {
	blockFriendListUpdateMu.Lock()
	defer blockFriendListUpdateMu.Unlock()
	return BlockSlideObjectConfig{Enabled: blockFriendListUpdate}
}

// ToggleBlockFriendListUpdate toggles blocking of incoming FRIEND_LIST_UPDATE packets.
func (a *App) ToggleBlockFriendListUpdate(enabled bool) BlockSlideObjectConfig {
	blockFriendListUpdateMu.Lock()
	blockFriendListUpdate = enabled
	blockFriendListUpdateMu.Unlock()

	cfg := BlockSlideObjectConfig{Enabled: blockFriendListUpdate}
	if a.ctx != nil {
		b, _ := json.Marshal(cfg)
		runtime.EventsEmit(a.ctx, "blockFriendListUpdateUpdate", string(b))
	}

	return cfg
}

// GetBlockPollEventEligibilityOutgoingConfig returns current outgoing poll-event config (1120).
func (a *App) GetBlockPollEventEligibilityOutgoingConfig() BlockSlideObjectConfig {
	blockPollEventEligibilityOutgoingMu.Lock()
	defer blockPollEventEligibilityOutgoingMu.Unlock()
	return BlockSlideObjectConfig{Enabled: blockPollEventEligibilityOutgoing}
}

// ToggleBlockPollEventEligibilityOutgoing toggles blocking of outgoing POLL_EVENT_ELIGIBILITY (1120).
func (a *App) ToggleBlockPollEventEligibilityOutgoing(enabled bool) BlockSlideObjectConfig {
	blockPollEventEligibilityOutgoingMu.Lock()
	blockPollEventEligibilityOutgoing = enabled
	blockPollEventEligibilityOutgoingMu.Unlock()

	cfg := BlockSlideObjectConfig{Enabled: blockPollEventEligibilityOutgoing}
	if a.ctx != nil {
		b, _ := json.Marshal(cfg)
		runtime.EventsEmit(a.ctx, "blockPollEventEligibilityOutgoingUpdate", string(b))
	}

	return cfg
}

// GetBlockRecommendedRoomsOutgoingConfig returns current setting for outgoing recommended-rooms.
func (a *App) GetBlockRecommendedRoomsOutgoingConfig() BlockRecommendedConfig {
	blockOutgoingRecommendedMu.Lock()
	defer blockOutgoingRecommendedMu.Unlock()
	return BlockRecommendedConfig{Enabled: blockOutgoingRecommendedRooms}
}

// ToggleBlockRecommendedRoomsOutgoing toggles blocking of outgoing recommended-rooms requests.
func (a *App) ToggleBlockRecommendedRoomsOutgoing(enabled bool) BlockRecommendedConfig {
	blockOutgoingRecommendedMu.Lock()
	blockOutgoingRecommendedRooms = enabled
	blockOutgoingRecommendedMu.Unlock()

	cfg := BlockRecommendedConfig{Enabled: blockOutgoingRecommendedRooms}
	if a.ctx != nil {
		b, _ := json.Marshal(cfg)
		runtime.EventsEmit(a.ctx, "blockRecommendedOutgoingUpdate", string(b))
	}

	return cfg
}

// GetBlockSlideObjectBundleOutgoingConfig returns current setting for outgoing slide-object-bundle.
func (a *App) GetBlockSlideObjectBundleOutgoingConfig() BlockSlideObjectConfig {
	blockSlideObjectOutgoingMu.Lock()
	defer blockSlideObjectOutgoingMu.Unlock()
	return BlockSlideObjectConfig{Enabled: blockSlideObjectOutgoing}
}

// ToggleBlockSlideObjectBundleOutgoing toggles blocking of outgoing slide-object-bundle packets.
func (a *App) ToggleBlockSlideObjectBundleOutgoing(enabled bool) BlockSlideObjectConfig {
	blockSlideObjectOutgoingMu.Lock()
	blockSlideObjectOutgoing = enabled
	blockSlideObjectOutgoingMu.Unlock()

	cfg := BlockSlideObjectConfig{Enabled: blockSlideObjectOutgoing}
	if a.ctx != nil {
		b, _ := json.Marshal(cfg)
		runtime.EventsEmit(a.ctx, "blockSlideObjectBundleOutgoingUpdate", string(b))
	}

	return cfg
}

// ToggleAutoShout enables or disables the auto shout loop and returns
// the current configuration.
func (a *App) ToggleAutoShout(enabled bool) AutoShoutConfig {
	autoShoutMu.Lock()

	autoShoutEnabled = enabled

	if autoShoutStopChan != nil {
		close(autoShoutStopChan)
		autoShoutStopChan = nil
	}

	if enabled {
		autoShoutStopChan = make(chan struct{})
		stopChan := autoShoutStopChan
		phrase := autoShoutPhrase
		seconds := autoShoutSeconds
		if seconds < 1 {
			seconds = 1
			autoShoutSeconds = 1
		}

		go a.runAutoShoutLoop(stopChan, phrase, seconds)
	}

	cfg := AutoShoutConfig{
		Enabled: autoShoutEnabled,
		Phrase:  autoShoutPhrase,
		Seconds: autoShoutSeconds,
	}
	autoShoutMu.Unlock()

	if a.ctx != nil {
		b, _ := json.Marshal(cfg)
		runtime.EventsEmit(a.ctx, "autoShoutUpdate", string(b))
	}

	return cfg
}

// runAutoShoutLoop runs the ticker that shouts the configured phrase.
func (a *App) runAutoShoutLoop(stopChan chan struct{}, phrase string, seconds int) {
	ticker := time.NewTicker(time.Duration(seconds) * time.Second)
	defer ticker.Stop()

	a.AddLogMsg(fmt.Sprintf("[AUTO_SHOUT] started: every %ds -> %q", seconds, phrase))

	for {
		select {
		case <-stopChan:
			a.AddLogMsg("[AUTO_SHOUT] stopped")
			return

		case <-ticker.C:
			autoShoutMu.Lock()
			enabled := autoShoutEnabled
			currentPhrase := strings.TrimSpace(autoShoutPhrase)
			autoShoutMu.Unlock()

			if !enabled || currentPhrase == "" {
				continue
			}

			if ChatIsDisabled {
				a.AddLogMsg("[AUTO_SHOUT] skipped because chat is disabled")
				continue
			}

			if isMuted {
				a.AddLogMsg("[AUTO_SHOUT] skipped because muted")
				continue
			}

			sendMessageWithDelay(currentPhrase)
		}
	}
}

func (a *App) dealerOpenMessage() string {
	return "Dealer Open, See Whats in My Hand - rollorigins.club"
}

func dealerGameActive() bool {
	return awaitingGameChoice ||
		awaitingBlackjackDecision ||
		awaiting13Decision ||
		awaitingTriChoice ||
		blackjackRoundActive ||
		thirteenRoundActive ||
		triRoundActive ||
		pokerSequenceStage > 0 ||
		isPokerRolling ||
		isTriRolling ||
		isBJRolling ||
		is13Rolling ||
		isHitting ||
		is13Hitting ||
		isClosing
}

func dealerReadyForNewTrade() bool {
	return dealerAcceptingTrades &&
		!dealerGameActive() &&
		!dealerResyncInProgress &&
		dealerSnapshotReady()
}

// dealerSnapshotReady reports whether a frozen trade-hand snapshot is present
// and ready for validating incoming trades. It locks the hand-items mutex
// to read the shared flag safely.
func dealerSnapshotReady() bool {
	handItemsMu.Lock()
	defer handItemsMu.Unlock()
	return tradeHandSnapshotReady
}

func dealerDiceReadyLocked() bool {
	return fakeDiceTestingMode || len(diceList) >= 5
}

func dealerDiceReady() bool {
	mutex.Lock()
	defer mutex.Unlock()
	return dealerDiceReadyLocked()
}

func canAnnounceDealerOpenLocked() bool {
	return !isMuted && dealerDiceReadyLocked()
}

func canAnnounceDealerOpen() bool {
	return !isMuted && dealerDiceReady()
}

func shouldAnnounceDealerOpen() bool {
	return dealerAnnouncementsEnabled && canAnnounceDealerOpen()
}

func getConfigFilePath() string {
	configDir, _ := os.UserConfigDir()
	configPath := filepath.Join(configDir, "Gamba-Suite")
	os.MkdirAll(configPath, 0700)
	return filepath.Join(configPath, "poker_display_config.json")
}

func getGameHistoryFilePath() string {
	configDir, _ := os.UserConfigDir()
	configPath := filepath.Join(configDir, "Gamba-Suite")
	os.MkdirAll(configPath, 0700)
	return filepath.Join(configPath, "game_history.json")
}

func cloneTradeItems(items []TradeItem) []TradeItem {
	copyItems := make([]TradeItem, len(items))
	copy(copyItems, items)
	return copyItems
}

// getRecentGameSummaries returns the last n completed games as anonymized
// summaries suitable for public status APIs. Player names are not exposed —
// winners are mapped to "Player"/"Dealer"/"Unknown".
func (a *App) getRecentGameSummaries(n int) []LiveGameSummary {
	a.gameHistoryMu.Lock()
	defer a.gameHistoryMu.Unlock()

	out := make([]LiveGameSummary, 0, n)
	count := 0
	dealerName := strings.TrimSpace(a.getCurrentDealerName())

	for i := 0; i < len(a.gameHistory) && count < n; i++ {
		entry := a.gameHistory[i]
		if strings.TrimSpace(entry.CompletedAt) == "" {
			continue
		}

		winner := strings.TrimSpace(entry.Winner)
		publicWinner := "Unknown"
		if winner != "" {
			// Treat either the configured dealer name or the literal
			// "Dealer" (case-insensitive) as a dealer win.
			if strings.EqualFold(winner, dealerName) || strings.EqualFold(winner, "Dealer") {
				publicWinner = "Dealer"
			} else {
				publicWinner = "Player"
			}
		}

		// If the dealer won, omit payout items (no payout to player).
		var payoutItems []TradeItem
		if publicWinner == "Dealer" {
			payoutItems = nil
		} else {
			payoutItems = cloneTradeItems(entry.PayoutItems)
		}

		gs := LiveGameSummary{
			ID:          entry.ID,
			Game:        entry.Game,
			Winner:      publicWinner,
			Outcome:     entry.Status,
			StartedAt:   entry.StartedAt,
			CompletedAt: entry.CompletedAt,
			BetItems:    cloneTradeItems(entry.BetItems),
			PayoutItems: payoutItems,
		}
		out = append(out, gs)
		count++
	}

	return out
}

func gameHistoryTimestamp() string {
	return time.Now().Format(time.RFC3339)
}

func (a *App) loadGameHistory() {
	path := getGameHistoryFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			a.AddLogMsg(fmt.Sprintf("[GAME_HISTORY] failed to read history file: %v", err))
		}
		return
	}

	var entries []GameHistoryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		a.AddLogMsg(fmt.Sprintf("[GAME_HISTORY] failed to decode history file: %v", err))
		return
	}

	// Normalize stored Winner fields so they contain either the player's
	// name (when the player won) or the literal "Dealer". Historically the
	// dealer's real username (e.g. "Gymbox") could be stored; convert those
	// to the canonical "Dealer" value so frontends and exports are stable.
	modified := 0
	for i := range entries {
		w := strings.TrimSpace(entries[i].Winner)
		if w == "" {
			continue
		}
		// Already normalized
		if strings.EqualFold(w, "Dealer") {
			continue
		}
		// If the winner string matches the recorded player name, keep it as
		// the player's name (player win). Otherwise treat as dealer.
		if entries[i].PlayerName != "" && strings.EqualFold(w, entries[i].PlayerName) {
			entries[i].Winner = entries[i].PlayerName
			continue
		}
		entries[i].Winner = "Dealer"
		modified++
	}

	a.gameHistoryMu.Lock()
	a.gameHistory = entries
	if modified > 0 {
		a.AddLogMsg(fmt.Sprintf("[GAME_HISTORY] normalized %d winner fields to 'Dealer'", modified))
		// Persist migrated history back to disk while we hold the lock.
		a.saveGameHistoryLocked()
	}
	a.gameHistoryMu.Unlock()
	a.emitGameHistoryUpdate()
}

func (a *App) GetGameHistoryJSON() string {
	a.gameHistoryMu.Lock()
	entries := make([]GameHistoryEntry, len(a.gameHistory))
	copy(entries, a.gameHistory)
	a.gameHistoryMu.Unlock()

	jsonData, err := json.Marshal(entries)
	if err != nil {
		return "[]"
	}
	return string(jsonData)
}

func (a *App) ClearGameHistory() {
	a.gameHistoryMu.Lock()
	a.gameHistory = nil
	a.currentGameHistoryID = ""
	a.gameHistoryMu.Unlock()

	_ = os.Remove(getGameHistoryFilePath())
	a.emitGameHistoryUpdate()
	a.AddLogMsg("[GAME_HISTORY] cleared all saved game history")
}

func (a *App) saveGameHistoryLocked() {
	jsonData, err := json.MarshalIndent(a.gameHistory, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(getGameHistoryFilePath(), jsonData, 0600)
}

func (a *App) emitGameHistoryUpdate() {
	a.gameHistoryMu.Lock()
	entries := make([]GameHistoryEntry, len(a.gameHistory))
	copy(entries, a.gameHistory)
	a.gameHistoryMu.Unlock()

	jsonData, err := json.Marshal(entries)
	if err != nil {
		return
	}
	runtime.EventsEmit(a.ctx, "gameHistoryUpdate", string(jsonData))
}

func (a *App) syncGameHistory() {
	a.AddLogMsg("[GAME_HISTORY] syncGameHistory start")

	// Save a copy of the history to disk without holding the mutex while
	// performing heavier work (like emitting stats which may re-lock).
	a.gameHistoryMu.Lock()
	jsonData, err := json.MarshalIndent(a.gameHistory, "", "  ")
	a.gameHistoryMu.Unlock()
	if err == nil {
		_ = os.WriteFile(getGameHistoryFilePath(), jsonData, 0600)
	}

	if a.ctx == nil {
		a.AddLogMsg("[GAME_HISTORY] syncGameHistory no runtime context, done")
		return
	}

	a.gameHistoryMu.Lock()
	historyJSON, err := json.Marshal(a.gameHistory)
	a.gameHistoryMu.Unlock()
	if err == nil {
		runtime.EventsEmit(a.ctx, "gameHistoryUpdate", string(historyJSON))
		a.AddLogMsg("[GAME_HISTORY] syncGameHistory emitted history update")
	}

	// Emit computed casino stats for UI convenience. These functions lock
	// gameHistoryMu internally, so ensure we are not holding it here.
	if stats := a.GetCasinoStatsJSON("all_time"); stats != "{}" {
		runtime.EventsEmit(a.ctx, "casinoStatsUpdate", stats)
		a.AddLogMsg("[GAME_HISTORY] syncGameHistory emitted stats update")
	}
	if statsT := a.GetCasinoStatsJSON("today"); statsT != "{}" {
		runtime.EventsEmit(a.ctx, "casinoStatsUpdateToday", statsT)
	}
}

func (a *App) findCurrentGameHistoryIndexLocked() int {
	if strings.TrimSpace(a.currentGameHistoryID) == "" {
		return -1
	}
	for i := range a.gameHistory {
		if a.gameHistory[i].ID == a.currentGameHistoryID {
			return i
		}
	}
	return -1
}

func (a *App) updateCurrentGameHistoryLocked(update func(entry *GameHistoryEntry)) bool {
	idx := a.findCurrentGameHistoryIndexLocked()
	if idx < 0 {
		return false
	}
	update(&a.gameHistory[idx])
	a.gameHistory[idx].UpdatedAt = gameHistoryTimestamp()
	return true
}

func (a *App) beginGameHistory(playerName string, betItems []TradeItem) {
	a.AddLogMsg("[GAME_HISTORY] beginGameHistory start")
	a.gameHistoryMu.Lock()

	if a.updateCurrentGameHistoryLocked(func(entry *GameHistoryEntry) {
		if entry.CompletedAt == "" {
			entry.Status = "Issue"
			entry.Issue = true
			entry.IssueReason = "Round was replaced before it fully finished"
			entry.CompletedAt = gameHistoryTimestamp()
			entry.Notes = append(entry.Notes, "New round started before previous round was fully resolved")
		}
	}) {
		a.currentGameHistoryID = ""
	}

	startedAt := gameHistoryTimestamp()
	entry := GameHistoryEntry{
		ID:         fmt.Sprintf("%d", time.Now().UnixNano()),
		PlayerName: strings.TrimSpace(playerName),
		StartedAt:  startedAt,
		UpdatedAt:  startedAt,
		Game:       "Waiting For Choice",
		Winner:     "",
		Status:     "Awaiting Game Choice",
		BetItems:   cloneTradeItems(betItems),
		Notes:      []string{"Trade completed and bet recorded"},
	}
	if entry.PlayerName == "" {
		entry.PlayerName = "Unknown"
	}

	a.gameHistory = append([]GameHistoryEntry{entry}, a.gameHistory...)
	a.currentGameHistoryID = entry.ID
	a.AddLogMsg("[GAME_HISTORY] beginGameHistory mutation complete")
	a.gameHistoryMu.Unlock()
	a.AddLogMsg("[GAME_HISTORY] beginGameHistory unlocked, syncing")
	a.syncGameHistory()
}

func (a *App) noteCurrentGameHistory(note string) {
	a.AddLogMsg("[GAME_HISTORY] noteCurrentGameHistory start")
	a.gameHistoryMu.Lock()
	if !a.updateCurrentGameHistoryLocked(func(entry *GameHistoryEntry) {
		entry.Notes = append(entry.Notes, note)
	}) {
		a.gameHistoryMu.Unlock()
		return
	}
	a.AddLogMsg("[GAME_HISTORY] noteCurrentGameHistory mutation complete")
	a.gameHistoryMu.Unlock()
	a.AddLogMsg("[GAME_HISTORY] noteCurrentGameHistory unlocked, syncing")
	a.syncGameHistory()
}

func (a *App) setCurrentGameHistoryGame(game string) {
	a.AddLogMsg("[GAME_HISTORY] setCurrentGameHistoryGame start")
	a.gameHistoryMu.Lock()
	if !a.updateCurrentGameHistoryLocked(func(entry *GameHistoryEntry) {
		changed := !strings.EqualFold(strings.TrimSpace(entry.Game), strings.TrimSpace(game))
		entry.Game = game
		entry.Status = "In Progress"
		if changed {
			entry.Notes = append(entry.Notes, fmt.Sprintf("Game selected: %s", game))
		}
	}) {
		a.gameHistoryMu.Unlock()
		return
	}
	a.AddLogMsg("[GAME_HISTORY] setCurrentGameHistoryGame mutation complete")
	a.gameHistoryMu.Unlock()
	a.AddLogMsg("[GAME_HISTORY] setCurrentGameHistoryGame unlocked, syncing")
	a.syncGameHistory()
}

func (a *App) setCurrentGameHistoryResults(playerResult string, dealerResult string, winner string, status string, complete bool) {
	a.AddLogMsg("[GAME_HISTORY] setCurrentGameHistoryResults start")
	a.gameHistoryMu.Lock()
	if !a.updateCurrentGameHistoryLocked(func(entry *GameHistoryEntry) {
		if strings.TrimSpace(playerResult) != "" {
			entry.PlayerResult = playerResult
		}
		if strings.TrimSpace(dealerResult) != "" {
			entry.DealerResult = dealerResult
		}
		if strings.TrimSpace(winner) != "" {
			norm := strings.TrimSpace(winner)
			// If the supplied winner matches the recorded player name, store
			// the player's name. Otherwise treat it as a dealer win and
			// canonicalize to "Dealer" (this covers stored dealer usernames).
			if entry.PlayerName != "" && strings.EqualFold(norm, entry.PlayerName) {
				entry.Winner = entry.PlayerName
			} else if strings.EqualFold(norm, "Dealer") || strings.EqualFold(norm, a.getCurrentDealerName()) {
				entry.Winner = "Dealer"
			} else {
				// Unknown non-player name — assume dealer and normalize.
				entry.Winner = "Dealer"
			}
		}
		if strings.TrimSpace(status) != "" {
			entry.Status = status
		}
		if complete {
			entry.CompletedAt = gameHistoryTimestamp()
		}
	}) {
		a.gameHistoryMu.Unlock()
		return
	}
	if complete {
		a.currentGameHistoryID = ""
	}
	a.AddLogMsg("[GAME_HISTORY] setCurrentGameHistoryResults mutation complete")
	a.gameHistoryMu.Unlock()
	a.AddLogMsg("[GAME_HISTORY] setCurrentGameHistoryResults unlocked, syncing")
	a.syncGameHistory()
	if complete {
		a.sendLiveDealerGames(5)
	}
}

func (a *App) markCurrentGameHistoryIssue(reason string, complete bool) {
	a.AddLogMsg("[GAME_HISTORY] markCurrentGameHistoryIssue start")
	a.gameHistoryMu.Lock()
	if !a.updateCurrentGameHistoryLocked(func(entry *GameHistoryEntry) {
		entry.Issue = true
		entry.IssueReason = reason
		entry.Status = "Issue"
		entry.Notes = append(entry.Notes, reason)
		if complete {
			entry.CompletedAt = gameHistoryTimestamp()
		}
	}) {
		a.gameHistoryMu.Unlock()
		return
	}
	if complete {
		a.currentGameHistoryID = ""
	}
	a.AddLogMsg("[GAME_HISTORY] markCurrentGameHistoryIssue mutation complete")
	a.gameHistoryMu.Unlock()
	a.AddLogMsg("[GAME_HISTORY] markCurrentGameHistoryIssue unlocked, syncing")
	a.syncGameHistory()
}

func (a *App) captureCurrentGameHistoryPayoutItems(items []TradeItem, note string, complete bool) {
	a.AddLogMsg("[GAME_HISTORY] captureCurrentGameHistoryPayoutItems start")
	a.gameHistoryMu.Lock()
	if !a.updateCurrentGameHistoryLocked(func(entry *GameHistoryEntry) {
		// If explicit payout items provided, use them. Otherwise, attempt
		// to infer payout from the recorded bet items (2x each) so history
		// isn't left with an empty payout list when the trade echo is delayed.
		if len(items) > 0 {
			entry.PayoutItems = cloneTradeItems(items)
		} else if len(entry.BetItems) > 0 {
			inferred := make([]TradeItem, 0, len(entry.BetItems))
			for _, b := range entry.BetItems {
				if b.Quantity <= 0 {
					continue
				}
				inferred = append(inferred, TradeItem{Name: b.Name, Quantity: b.Quantity * 2, RawData: b.RawData})
			}
			entry.PayoutItems = inferred
			if len(inferred) > 0 {
				entry.Notes = append(entry.Notes, "Predicted payout (2x bet)")
			}
		} else {
			entry.PayoutItems = cloneTradeItems(items)
		}

		if strings.TrimSpace(note) != "" {
			entry.Notes = append(entry.Notes, note)
		}
		if complete {
			entry.Status = "Completed"
			entry.CompletedAt = gameHistoryTimestamp()
		}
	}) {
		a.gameHistoryMu.Unlock()
		return
	}
	if complete {
		a.currentGameHistoryID = ""
	}
	a.AddLogMsg("[GAME_HISTORY] captureCurrentGameHistoryPayoutItems mutation complete")
	a.gameHistoryMu.Unlock()
	a.AddLogMsg("[GAME_HISTORY] captureCurrentGameHistoryPayoutItems unlocked, syncing")
	a.syncGameHistory()
}

func (a *App) setupExt() {
	registerCustomTradeHeaders(a)

	a.ext.Intercept(out.CHAT, out.SHOUT, out.WHISPER).With(a.onChatMessage)
	a.ext.Intercept(out.GETSTRIP).With(a.handleOutgoingGetStrip)
	a.ext.Intercept(out.THROW_DICE).With(a.handleThrowDice)
	a.ext.Intercept(out.DICE_OFF).With(a.handleDiceOff)
	a.ext.Intercept(in.DICE_VALUE).With(a.handleDiceResult)
	a.ext.Intercept(in.CHAT, in.CHAT_2, in.CHAT_3).With(a.handleIncomingChat)
	a.ext.Intercept(in.ROOM_READY).With(a.handleRoomReady)
	a.ext.Intercept(in.USERS).With(a.handleRoomUsers)
	a.ext.Intercept(in.SPACENODEUSERS).With(a.handleRoomUsers)
	a.ext.Intercept(out.CHAT).With(a.handleTalk)
	a.ext.Intercept(out.SHOUT).With(a.handleTalk)
	a.ext.InterceptAll(func(e *g.Intercept) {
		handleMutePacket(e)
		handleRoomResetPacket(a, e)
		handleTradePacket(a, e)
		handleUsers28Packet(a, e)
		handleIncomingHeaderSniff(a, e)
		handleOutgoingHeaderSniff(a, e)
		handleStripPacket(a, e)
	})
}

func registerCustomTradeHeaders(a *App) {
	confirmID := g.Out.Id("TRADE_CONFIRM_ACCEPT")
	if _, ok := a.ext.Headers().TryGet(confirmID); !ok {
		a.ext.Headers().Add("TRADE_CONFIRM_ACCEPT", g.Header{Dir: g.Out, Value: 402})
		a.AddLogMsg("[TRADE_HEADERS] registered outgoing TRADE_CONFIRM_ACCEPT -> 402")
	}
}

func (a *App) runExt() {
	defer os.Exit(0)
	a.ext.Run()
}

func (a *App) ShowWindow() {
	runtime.WindowShow(a.ctx)
}

func startMuteTimer(duration int) {
	for duration > 0 {
		log.Printf("Remaining mute time: %d seconds", duration)
		time.Sleep(1 * time.Second) // Sleep for 1 second
		duration--
	}

	// Mute duration finished
	handleMuteEnd()
}

func handleMuteEnd() {
	isMuted = false
	log.Println("Mute finished, sending queued messages...")

	// If any messages were queued while muted, send them now.
	if len(messageQueue) > 0 {
		log.Printf("[MUTE_QUEUE] sending %d queued messages", len(messageQueue))

		dealerOpenQueued := false
		dealerOpenMsg := (&App{}).dealerOpenMessage()
		for _, message := range messageQueue {
			if strings.TrimSpace(message) == dealerOpenMsg {
				dealerOpenQueued = true
				break
			}
		}

		// Ensure the dealer open flags reflect that we'll be announcing now.
		if dealerOpenQueued {
			awaitingTradeOpen = true
			dealerAcceptingTrades = true
			if shouldAnnounceDealerOpen() {
				dealerTradeWindowOpen = true
				startDealerOpenHeartbeat(nil)
			}
		}

		// Send queued messages with small spacing so Habbo's flood control is less likely to trigger.
		for _, message := range messageQueue {
			go sendMessageWithDelay(message)
			time.Sleep(150 * time.Millisecond)
		}

		// Clear the queue
		messageQueue = nil
	}
}

// waitForUnmute blocks until the mute clears or max duration elapses.
// Use before any critical chat message (winner announce etc.) so it is not
// silently swallowed by Habbo's flood-control mute.
func waitForUnmute(max time.Duration) {
	if !isMuted {
		return
	}
	deadline := time.Now().Add(max)
	for isMuted && time.Now().Before(deadline) {
		time.Sleep(300 * time.Millisecond)
	}
	if isMuted {
		log.Printf("[MUTE_GUARD] still muted after %s wait, sending anyway", max)
	} else {
		log.Printf("[MUTE_GUARD] mute cleared, proceeding with message")
	}
}

// Mute detection logic (called within InterceptAll)
func handleMutePacket(e *g.Intercept) {
	// Check for the "first muted" packet with header 4069
	if e.Packet.Header.Value == 4069 {
		mutedDuration = e.Packet.ReadInt() // Read the mute duration in seconds
		log.Printf("You are muted for %d seconds.", mutedDuration)
		isMuted = true
		go startMuteTimer(mutedDuration) // Start the mute timer
	}

	// Check for the "trying to chat while muted" packet with header 3285
	if e.Packet.Header.Value == 3285 {
		remainingMuteDuration := e.Packet.ReadInt() // Read the remaining mute duration
		log.Printf("Mute still active, remaining time: %d seconds.", remainingMuteDuration)
	}
}

// Trade detection logic (called within InterceptAll)
func handleTradePacket(a *App, e *g.Intercept) {
	defer func() {
		if r := recover(); r != nil {
			a.AddLogMsg(fmt.Sprintf("[TRADE] recovered while handling header %d: %v", e.Packet.Header.Value, r))
		}
	}()

	// Only process trade-related logic when casino setup is complete.
	if !casinoReady {
		return
	}

	// NOTE: payout coverage guard removed — proceed with outgoing accept/confirm.

	// TRADE_OPEN outgoing 71 - remember recent target so matching incoming 104 isn't blocked by dealer guard.
	if e.Packet.Header.Dir == g.Out && e.Packet.Header.Value == 71 {
		if targetID, ok := decodeLeadingVL64(e.Packet.Data); ok {
			rememberOutgoingTradeOpenTarget(targetID)
			a.AddLogMsg(fmt.Sprintf("[TRADE_OPEN #71] remembered outgoing target id %d", targetID))
		} else {
			a.AddLogMsg(fmt.Sprintf("[TRADE_OPEN #71] outgoing payload decode failed: %q", string(e.Packet.Data)))
		}
	}

	// TRADE_ADDITEM outgoing 72 - we are adding an item; flag next TRADE_ITEMS as ours
	if e.Packet.Header.Dir == g.Out && e.Packet.Header.Value == 72 {
		addItemMu.Lock()
		lastAddItemWasOurs = true
		lastAddItemByUsAt = time.Now()
		addItemMu.Unlock()
		// Debug log outgoing add with timestamp
		a.AddLogMsg(fmt.Sprintf("[TRADE_ADDITEM_DEBUG] outgoing TRADE_ADDITEM payload=%q at=%s", string(e.Packet.Data), lastAddItemByUsAt.Format(time.RFC3339Nano)))
		if payoutTradeActive {
			payoutActualAddCount++
			a.AddLogMsg(fmt.Sprintf("[PAYOUT_DEBUG] observed outgoing TRADE_ADDITEM count %d/%d", payoutActualAddCount, payoutExpectedAddCount))
		}
		a.AddLogMsg("[TRADE_ADDITEM #72] outgoing: next TRADE_ITEMS belongs to us")
		return
	}

	if e.Packet.Header.Dir == g.In && hiddenBlockedTradeCleanupPending {
		switch e.Packet.Header.Value {
		case 105, 108, 109, 111, 112:
			a.AddLogMsg(fmt.Sprintf("[TRADE_GUARD] suppressing incoming trade packet %d during blocked-trade cleanup", e.Packet.Header.Value))
			e.Block()
			return
		}
	}

	// TRADE_ACCEPT incoming 109 - wait 2 seconds then send TRADE_ACCEPT (69)
	if e.Packet.Header.Value == 109 {
		// Partner accepted the current trade state. Record this so a later
		// TRA_ITEMS update will treat it as stale.
		partnerTradeAccepted = true
		// Snapshot the full-state the partner accepted so we can detect
		// whether later TRADE_ITEMS actually change the accepted contents.
		tradeItemsMu.Lock()
		if len(lastAllTradeItems) > 0 {
			partnerAcceptedSnapshot = make([]TradeItem, len(lastAllTradeItems))
			copy(partnerAcceptedSnapshot, lastAllTradeItems)
		} else {
			// Fallback: snapshot currentTradeItems when lastAllTradeItems is empty
			partnerAcceptedSnapshot = make([]TradeItem, len(currentTradeItems))
			copy(partnerAcceptedSnapshot, currentTradeItems)
		}
		tradeItemsMu.Unlock()
		a.AddLogMsg("[TRADE_ACCEPT] partner accepted current trade state")
		scheduleAutoTradeAccept(a, string(e.Packet.Data))
		return
	}

	// TRADE_CONFIRM incoming 111 - wait 4 seconds then send TRADE_CONFIRM_ACCEPT (402)
	if e.Packet.Header.Value == 111 {
		scheduleAutoTradeConfirm(a, string(e.Packet.Data))
		return
	}

	// TRADE_ITEMS header 108 - incoming server echo of full trade state (always incoming)
	// Two modes:
	// - Non-payout mode: treat the incoming list as the partner's offer directly
	//   (simpler and avoids racey diff logic that can hide the first add).
	// - Payout mode: keep delta attribution so automated payout adds by the
	//   dealer are assigned to our own offer correctly.
	if e.Packet.Header.Value == 108 {
		allItems := a.parseTradeItemsPacket(e.Packet.Data)

		if !payoutTradeActive {
			// Non-payout: maintain both sides of the trade window as items are
			// added so the dealer side appears in the UI too. We still validate
			// only the partner side against trade limits.

			// Snapshot outgoing-add state and timestamp for debugging/race detection.
			addItemMu.Lock()
			wasOurs := lastAddItemWasOurs
			lastAddItemWasOurs = false
			lastAddAt := lastAddItemByUsAt
			addItemMu.Unlock()

			prevAllLen := 0
			tradeItemsMu.Lock()
			prevAllLen = len(lastAllTradeItems)
			prevAllCopy := make([]TradeItem, len(lastAllTradeItems))
			copy(prevAllCopy, lastAllTradeItems)
			partnerMap := map[string]int{}
			for _, it := range currentTradeItems {
				partnerMap[it.Name] = it.Quantity
			}
			ownMap := map[string]int{}
			for _, it := range currentOwnTradeItems {
				ownMap[it.Name] = it.Quantity
			}
			acceptedSnap := make([]TradeItem, len(partnerAcceptedSnapshot))
			copy(acceptedSnap, partnerAcceptedSnapshot)
			tradeItemsMu.Unlock()

			a.AddLogMsg(fmt.Sprintf("[TRADE_ITEMS_DEBUG] incoming TRADE_ITEMS all=%d prevAll=%d wasOursFlag=%t lastAddAt=%s (non-payout)", len(allItems), prevAllLen, wasOurs, lastAddAt.Format(time.RFC3339Nano)))

			if len(allItems) == 0 {
				a.AddLogMsg(fmt.Sprintf("[TRADE_ITEMS_DEBUG] zero parsed items, raw=%q", string(e.Packet.Data)))
			}
			for i, item := range allItems {
				a.AddLogMsg(fmt.Sprintf("[TRADE_ITEMS #108] raw[%d] name=%q quantity=%d", i, item.Name, item.Quantity))
			}

			if len(e.Packet.Data) > 0 {
				extendTradeWindowTimeoutForPartnerActivity(a)
			}

			prevAll := map[string]int{}
			for _, it := range prevAllCopy {
				prevAll[it.Name] = it.Quantity
			}
			allMap := map[string]int{}
			for _, it := range allItems {
				allMap[it.Name] += it.Quantity
			}

			// Attribute positive deltas to the side that most recently sent
			// TRADE_ADDITEM. First packet with no previous baseline defaults to
			// the partner side.
			added := map[string]int{}
			for name, q := range allMap {
				if q > prevAll[name] {
					added[name] = q - prevAll[name]
				}
			}
			if len(prevAllCopy) == 0 && len(allItems) > 0 {
				for name, q := range allMap {
					partnerMap[name] = q
				}
			} else if len(added) > 0 {
				if wasOurs {
					for name, q := range added {
						ownMap[name] += q
					}
				} else {
					for name, q := range added {
						partnerMap[name] += q
					}
				}
			}

			// Keep the UI consistent when items are removed by clamping each side
			// back down to the current server total if our attributed totals drift.
			for name, total := range allMap {
				combined := partnerMap[name] + ownMap[name]
				if combined > total {
					over := combined - total
					if ownMap[name] >= over {
						ownMap[name] -= over
					} else {
						over -= ownMap[name]
						ownMap[name] = 0
						if partnerMap[name] >= over {
							partnerMap[name] -= over
						} else {
							partnerMap[name] = 0
						}
					}
				}
			}
			for name := range partnerMap {
				if _, ok := allMap[name]; !ok {
					partnerMap[name] = 0
				}
			}
			for name := range ownMap {
				if _, ok := allMap[name]; !ok {
					ownMap[name] = 0
				}
			}

			mapToItems := func(m map[string]int) []TradeItem {
				names := make([]string, 0, len(m))
				for n := range m {
					names = append(names, n)
				}
				sort.Strings(names)
				out := make([]TradeItem, 0, len(names))
				for _, n := range names {
					if m[n] <= 0 {
						continue
					}
					out = append(out, TradeItem{Name: n, Quantity: m[n]})
				}
				return out
			}

			currentPartnerItems := mapToItems(partnerMap)
			currentOwnItems := mapToItems(ownMap)

			tradeItemsMu.Lock()
			currentTradeItems = currentPartnerItems
			currentOwnTradeItems = currentOwnItems
			lastAllTradeItems = make([]TradeItem, len(allItems))
			copy(lastAllTradeItems, allItems)
			tradeItemsMu.Unlock()

			a.AddLogMsg(fmt.Sprintf("[TRADE_ITEMS #108] partner=%d own=%d all=%d wasOurs=%t (non-payout)", len(currentPartnerItems), len(currentOwnItems), len(allItems), wasOurs))
			a.emitTradeItemsUpdate("both")

			wasValid := true
			if len(prevAllCopy) > 0 {
				wasValid = getTradeLimitViolation(prevAllCopy) == nil
			}

			itemsCopy := make([]TradeItem, len(currentPartnerItems))
			copy(itemsCopy, currentPartnerItems)

			_ = acceptedSnap
			a.AddLogMsg(fmt.Sprintf("[TRADE_LIMIT_DEBUG] validating %d parsed trade items", len(itemsCopy)))
			for i, it := range itemsCopy {
				a.AddLogMsg(fmt.Sprintf("[TRADE_LIMIT_DEBUG] parsed[%d] name=%q qty=%d", i, it.Name, it.Quantity))
			}
			violation := getTradeLimitViolation(itemsCopy)
			isValid := violation == nil
			if violation != nil {
				tradeLimitWasActive = true
				a.rejectTradeForLimitViolation(violation)
				return
			}

			if tradeLimitWasActive || tradeLimitMonitorActive {
				stopTradeLimitMonitor()
				lastTradeLimitNotice = ""
				tradeLimitWasActive = false
				a.AddLogMsg("[TRADE_LIMIT] violation resolved; trade is valid again")
				ext.Send(out.SHOUT, "Trade is back within limits, accept again if needed")
			}

			if !wasValid && isValid {
				a.AddLogMsg(fmt.Sprintf("[TRADE_LIMIT] transition invalid->valid detected prevPartnerAccepted=%t", partnerTradeAccepted))
				if partnerTradeAccepted && !tradeAutoAccepted && !tradeAutoAcceptPending {
					a.AddLogMsg("[TRADE_ACCEPT] re-arming auto-accept after invalid->valid transition")
					scheduleAutoTradeAccept(a, "rearmed-after-limit-fix")
				}
			}

			handItemsMu.Lock()
			ready := tradeHandSnapshotReady
			handItemsMu.Unlock()

			if !ready {
				a.AddLogMsg("[TRADE_COVERAGE] snapshot not ready yet, skipping live trade check")
				return
			}

			a.notifyTradeQuantityCoverage()
			return
		}

		// Payout mode: compute delta and attribute to the side that sent TRADE_ADDITEM.
		// Snapshot outgoing-add state and timestamp for debugging/race detection
		addItemMu.Lock()
		wasOurs := lastAddItemWasOurs
		lastAddItemWasOurs = false
		lastAddAt := lastAddItemByUsAt
		addItemMu.Unlock()

		// Prev full-state length for debug
		prevAllLen := 0
		tradeItemsMu.Lock()
		prevAllLen = len(lastAllTradeItems)
		tradeItemsMu.Unlock()

		a.AddLogMsg(fmt.Sprintf("[TRADE_ITEMS_DEBUG] incoming TRADE_ITEMS all=%d prevAll=%d wasOursFlag=%t lastAddAt=%s", len(allItems), prevAllLen, wasOurs, lastAddAt.Format(time.RFC3339Nano)))

		// When parsing produced zero items, record the raw payload to help
		// diagnose brittle parser behavior that treats header/token fields as items.
		if len(allItems) == 0 {
			a.AddLogMsg(fmt.Sprintf("[TRADE_ITEMS_DEBUG] zero parsed items (payout-mode), raw=%q", string(e.Packet.Data)))
		}

		// Build maps of previous full-state and current tracked sides
		prevAll := map[string]int{}
		tradeItemsMu.Lock()
		for _, it := range lastAllTradeItems {
			prevAll[it.Name] = it.Quantity
		}
		partnerMap := map[string]int{}
		for _, it := range currentTradeItems {
			partnerMap[it.Name] = it.Quantity
		}
		ownMap := map[string]int{}
		for _, it := range currentOwnTradeItems {
			ownMap[it.Name] = it.Quantity
		}
		tradeItemsMu.Unlock()

		allMap := map[string]int{}
		for _, it := range allItems {
			allMap[it.Name] += it.Quantity
		}

		// Compute additions (positive deltas) vs previous full-state
		added := map[string]int{}
		for name, q := range allMap {
			if q > prevAll[name] {
				added[name] = q - prevAll[name]
			}
		}

		// Merge added items into the appropriate side.
		tradeItemsMu.Lock()
		if wasOurs {
			for name, q := range added {
				ownMap[name] += q
			}
		} else {
			for name, q := range added {
				partnerMap[name] += q
			}
		}

		// Helper: convert map -> sorted slice
		mapToItems := func(m map[string]int) []TradeItem {
			names := make([]string, 0, len(m))
			for n := range m {
				names = append(names, n)
			}
			sort.Strings(names)
			out := make([]TradeItem, 0, len(names))
			for _, n := range names {
				if m[n] <= 0 {
					continue
				}
				out = append(out, TradeItem{Name: n, Quantity: m[n]})
			}
			return out
		}

		currentTradeItems = mapToItems(partnerMap)
		currentOwnTradeItems = mapToItems(ownMap)

		// Save the new full-state for the next delta calculation
		lastAllTradeItems = make([]TradeItem, len(allItems))
		copy(lastAllTradeItems, allItems)
		tradeItemsMu.Unlock()

		a.AddLogMsg(fmt.Sprintf("[TRADE_ITEMS #108] partner=%d own=%d all=%d wasOurs=%t (payout)", len(currentTradeItems), len(currentOwnTradeItems), len(allItems), wasOurs))

		for i, item := range allItems {
			a.AddLogMsg(fmt.Sprintf("[TRADE_ITEMS #108] raw[%d] name=%q quantity=%d", i, item.Name, item.Quantity))
		}

		a.emitTradeItemsUpdate("both")

		// Extend timeout on any incoming TRADE_ITEMS payload (debug-safe)
		if len(e.Packet.Data) > 0 {
			extendTradeWindowTimeoutForPartnerActivity(a)
		}

		a.AddLogMsg("[TRADE_COVERAGE] skipped shortage enforcement during payout trade")
		return
	}

	// TRADE_COMPLETED header 112 - send chat message with the traded items
	if e.Packet.Header.Value == 112 {
		tradeCompleted = true
		tradeAutoConfirmed = true
		tradeAutoConfirmPending = false
		// Clear any partner accept / trade-limit state after a completed trade
		partnerTradeAccepted = false
		partnerAcceptedSnapshot = nil
		tradeLimitWasActive = false
		lastTradeLimitNotice = ""
		stopTradeLimitMonitor()
		if payoutTradeActive {
			stopPayoutResponseTimeoutMonitor()
			resetPayoutRetryState()
			tradeItemsMu.Lock()
			payoutItems := cloneTradeItems(currentOwnTradeItems)
			tradeItemsMu.Unlock()
			partnerName := normalizeUsername(strings.TrimSpace(lastTradePartnerName))
			if partnerName == "" || partnerName == "Unknown" {
				partnerName = strings.TrimSpace(payoutTargetName)
			}
			if partnerName == "" {
				partnerName = "Unknown"
			}

			a.AddLogMsg("[TRADE_COMPLETED #112] payout trade completed")
			a.captureCurrentGameHistoryPayoutItems(payoutItems, "Payout trade completed successfully", true)

			completeMsg := fmt.Sprintf("Trade Completed: \"%s\"", partnerName)
			a.AddLogMsg(fmt.Sprintf("[TRADE_COMPLETED] shouting: %q", completeMsg))
			ext.Send(out.SHOUT, completeMsg)
		} else {
			a.AddLogMsg("[TRADE_COMPLETED #112] trade completed, sending trade summary")

			// Send the trade items summary to chat
			a.sendTradeCompletionMessage()

			// Record predicted payout items for history as 2x the bet items
			// This ensures the frontend shows a sensible payout count even when
			// an explicit payout trade flow was not used.
			payoutPred := make([]TradeItem, 0, len(gameBetItems))
			for _, it := range gameBetItems {
				if it.Quantity <= 0 {
					continue
				}
				payoutPred = append(payoutPred, TradeItem{Name: it.Name, Quantity: it.Quantity * 2, RawData: it.RawData})
			}
			if len(payoutPred) > 0 {
				// Keep the round open after the bet trade completes. At this point the
				// player still has to choose a game and the app still needs to record the
				// actual game, winner and results. We only persist a predicted payout so
				// the history modal can show the expected return while the round is live.
				a.captureCurrentGameHistoryPayoutItems(payoutPred, "Predicted payout (2x bet)", false)
			} else {
				// Do not complete the round here. A missing prediction should not clear
				// currentGameHistoryID before the game result is recorded.
				a.captureCurrentGameHistoryPayoutItems([]TradeItem{}, "No payout items recorded yet", false)
			}
			go func() {
				if ok := a.forceRefreshHandSnapshot("trade completed"); ok {
					a.AddLogMsg("[TRADE_COMPLETED] forced hand refresh complete after trade")
				} else {
					a.AddLogMsg("[TRADE_COMPLETED] forced hand refresh failed after trade")
				}
			}()
		}
		return
	}

	if e.Packet.Header.Value == 104 {
		// Manual block-all-trades toggle — skip if we just sent our own payout trade open
		if blockAllTrades && !payoutTradeSent && !matchesRecentOutgoingFunc(e.Packet.Data) {
			activeRound := awaitingGameChoice || dealerGameActive() || payoutActive || payoutTradeActive
			allowed := false

			if activeRound {
				// Strict trade-id validation: build a set of expected trade IDs
				// derived from the current game state (starter, stable copy,
				// last partner, payout target and any resolved awaiting partner).
				incomingTraderID := 0
				if id, ok := decodeLeadingVL64(e.Packet.Data); ok {
					incomingTraderID = id
				}

				expectedIDs := map[int]struct{}{}
				if tradeStarterTradeID > 0 {
					expectedIDs[tradeStarterTradeID] = struct{}{}
				}
				if stableTradePartnerID > 0 {
					expectedIDs[stableTradePartnerID] = struct{}{}
				}
				if lastTradePartnerID > 0 {
					expectedIDs[lastTradePartnerID] = struct{}{}
				}
				if payoutTargetID > 0 {
					expectedIDs[payoutTargetID] = struct{}{}
				}

				// If we have an awaiting partner name for the current choice,
				// try to resolve its trade_id too.
				awaitingName := strings.TrimSpace(awaitingGameChoicePartnerName)
				if awaitingName != "" {
					if id, ok := lookupUsers28TradeIDByName(awaitingName); ok {
						expectedIDs[id] = struct{}{}
					}
				}

				partnerName := strings.TrimSpace(lastTradePartnerName)
				// Also include partnerName-derived id for backwards compatibility
				if partnerName != "" && !strings.EqualFold(partnerName, "Unknown") {
					if id, ok := lookupUsers28TradeIDByName(partnerName); ok {
						expectedIDs[id] = struct{}{}
					}
				}

				// Diagnostic log of the check
				a.AddLogMsg(fmt.Sprintf("[TRADE_BLOCK_DEBUG] incomingChatID=%d expectedIDs=%v partner=%q activeRound=%t", incomingTraderID, expectedIDs, partnerName, activeRound))

				// If we resolved any expected IDs, require an exact match.
				matched := false
				if len(expectedIDs) > 0 {
					if incomingTraderID > 0 {
						if _, ok := expectedIDs[incomingTraderID]; ok {
							matched = true
							a.AddLogMsg(fmt.Sprintf("[TRADE_BLOCK] allowing trade by exact trade_id match (%d)", incomingTraderID))
							allowed = true
						}
					}
				}

				// Fallback: keep previous behavior when no expected IDs were
				// resolvable (best-effort name->trade_id match).
				if !matched && len(expectedIDs) == 0 {
					if partnerName != "" && !strings.EqualFold(partnerName, "Unknown") {
						if expectedID, ok := lookupUsers28TradeIDByName(partnerName); ok {
							a.AddLogMsg(fmt.Sprintf("[TRADE_BLOCK_DEBUG] fallback active partner=%q expectedChatID=%d incomingChatID=%d", partnerName, expectedID, incomingTraderID))
							if incomingTraderID > 0 && incomingTraderID == expectedID {
								a.AddLogMsg(fmt.Sprintf("[TRADE_BLOCK] allowing trade from active partner %q by fallback parsed chat_id match", partnerName))
								allowed = true
							}
						}
					}
				}

				if !allowed {
					a.AddLogMsg("[TRADE_BLOCK] incoming trade blocked during active round (trade_id mismatch)")

					// Detailed guard state for diagnostics
					a.AddLogMsg(fmt.Sprintf("[TRADE_GUARD] block reason=%s awaitingTradeOpen=%t dealerTradeWindowOpen=%t dealerGameActive=%t dealerResyncInProgress=%t", "incoming blocked during active round", awaitingTradeOpen, dealerTradeWindowOpen, dealerGameActive(), dealerResyncInProgress))
					hiddenBlockedTradeCleanupPending = true
					ignoreNextGuardCloseRecovery = true
					suppressNextTradeCloseAnnouncement = true
					e.Block()
					ext.Send(out.TRADE_CLOSE)
					return
				}
			}
		}

		a.ShowWindow()
		stopUnderfundedTradeMonitor()
		lastTradeCoverageNotice = ""
		lastTradeBlockNotice = ""
		openedDuringDealerWindow := awaitingTradeOpen && dealerTradeWindowOpen

		incomingTraderID := 0
		if id, ok := decodeLeadingVL64(e.Packet.Data); ok {
			incomingTraderID = id
		}
		recentTargetID, matchedRecentOutgoing := matchesRecentOutgoingTradeOpen(e.Packet.Data, incomingTraderID)

		// During payout mode, someone else opened a trade with us — close it and let the payout loop retry
		isPayoutTradeOpen := false
		if payoutActive {
			incomingTraderID := 0
			if id, ok := decodeLeadingVL64(e.Packet.Data); ok {
				incomingTraderID = id
			}
			expectedID, _ := lookupUsers28TradeIDByName(payoutTargetName)
			a.AddLogMsg(fmt.Sprintf("[PAYOUT_DEBUG] incoming trade-open while payout active: sent=%t target=%q targetID=%d expectedChatID=%d incomingChatID=%d matchedRecentOutgoing=%t", payoutTradeSent, payoutTargetName, payoutTargetID, expectedID, incomingTraderID, matchedRecentOutgoing))
			if payoutTradeSent {
				// Our outgoing TRADE_OPEN was accepted — this is the payout trade opening successfully
				savedPayoutTargetID := payoutTargetID
				savedPayoutTargetName := payoutTargetName
				stopPayout() // kills retry goroutine
				payoutTradeActive = true
				payoutTargetID = savedPayoutTargetID
				payoutTargetName = savedPayoutTargetName
				isPayoutTradeOpen = true
				a.AddLogMsg(fmt.Sprintf("[PAYOUT] trade opened successfully with %s, proceeding", savedPayoutTargetName))
				// Fall through to normal trade-open handling below
				go a.autoAddPayoutItems()
				// Don't start the payout response timeout at trade-open; start it
				// only after the dealer (us) actually accepts the payout so we
				// don't time out while auto-adding many items.
			} else {
				// Someone else opened a trade with us during payout — block it
				a.AddLogMsg(fmt.Sprintf("[PAYOUT] incoming trade blocked during payout to %s, closing", payoutTargetName))

				// Detailed guard state for diagnostics
				a.AddLogMsg(fmt.Sprintf("[TRADE_GUARD] block reason=%s awaitingTradeOpen=%t dealerTradeWindowOpen=%t dealerGameActive=%t dealerResyncInProgress=%t", "incoming blocked during payout", awaitingTradeOpen, dealerTradeWindowOpen, dealerGameActive(), dealerResyncInProgress))
				hiddenBlockedTradeCleanupPending = true
				ignoreNextGuardCloseRecovery = true
				suppressNextTradeCloseAnnouncement = true
				e.Block()
				ext.Send(out.TRADE_CLOSE)
				return
			}
		}

		if !isPayoutTradeOpen && !dealerReadyForNewTrade() && !matchedRecentOutgoing {
			reason := "dealer not open"
			if dealerGameActive() {
				reason = "dealer busy in active game"
			} else if dealerResyncInProgress {
				reason = "dealer syncing hand"
			} else if !dealerAcceptingTrades {
				reason = "dealer not accepting trades"
			}

			a.AddLogMsg(fmt.Sprintf("[TRADE_GUARD] blocking incoming trade open: %s", reason))

			// Detailed guard state for diagnostics
			a.AddLogMsg(fmt.Sprintf("[TRADE_GUARD] block reason=%s awaitingTradeOpen=%t dealerTradeWindowOpen=%t dealerGameActive=%t dealerResyncInProgress=%t", reason, awaitingTradeOpen, dealerTradeWindowOpen, dealerGameActive(), dealerResyncInProgress))
			hiddenBlockedTradeCleanupPending = true
			ignoreNextGuardCloseRecovery = true
			suppressNextTradeCloseAnnouncement = true
			e.Block()
			ext.Send(out.TRADE_CLOSE)
			return
		}

		if matchedRecentOutgoing {
			a.AddLogMsg(fmt.Sprintf("[TRADE_GUARD] allowing incoming trade open because it matches recent outgoing target %d", recentTargetID))
		}

		// Defensive guard: do not allow incoming trade to proceed if we don't
		// yet have a ready frozen hand snapshot. This prevents the race where
		// ClearTradeItems() wiped the snapshot and the dealer reopens immediately.
		if !isPayoutTradeOpen && !matchedRecentOutgoing && !dealerSnapshotReady() {
			a.AddLogMsg("[TRADE_GUARD] blocking incoming trade open: hand snapshot not ready")
			hiddenBlockedTradeCleanupPending = true
			ignoreNextGuardCloseRecovery = true
			suppressNextTradeCloseAnnouncement = true
			e.Block()
			ext.Send(out.TRADE_CLOSE)
			return
		}

		awaitingGameChoice = false
		gameChoiceUnreadableWarned = false
		awaitingGameChoicePartnerID = 0
		awaitingGameChoicePartnerName = ""
		pokerSequenceStage = 0
		pokerSequencePlayerName = ""
		stopDealerOpenHeartbeat()
		if !isPayoutTradeOpen && openedDuringDealerWindow {
			startTradeWindowTimeoutMonitor(a)
		}

		resetTradeAutoFlow()

		// Only clear lastTradePartnerName/ID/Token if not a payout trade.
		// This preserves the winner's name for the payout trade open message.
		if !isPayoutTradeOpen {
			lastTradePartnerName = ""
			lastTradePartnerID = 0
			lastTradePartnerToken = ""
			tradeStarterTradeID = 0
			tradeStarterChatID = 0
			tradeStarterName = ""
			tradeStarterToken = ""
			tradeStarterLocked = false
		}

		for _, decodeLine := range decodeTradeOpenPacket(e.Packet) {
			a.AddLogMsg("[TRADE_OPEN_DECODE] " + decodeLine)
		}

		for _, candidateLine := range describeTradeRoomCandidates(a.ext) {
			a.AddLogMsg("[TRADE_ROOM] " + candidateLine)
		}

		if len(e.Packet.Data) >= 1 {
			go requestRoomUsers(a)

			tradeToken := strings.TrimSpace(extractTradeTokenFromPacket(e.Packet.Data))
			if tradeToken != "" {
				lastTradePartnerToken = tradeToken
				tradeStarterToken = tradeToken
				a.AddLogMsg(fmt.Sprintf("[TRADE_OPEN] extracted token=%q payload_hex=% X", tradeToken, e.Packet.Data))
				if !isLikelyToken(tradeToken) {
					a.AddLogMsg(fmt.Sprintf("[TRADE_OPEN] extracted token looks suspicious: %q", tradeToken))
				}
			} else {
				a.AddLogMsg(fmt.Sprintf("[TRADE_OPEN] no 4-byte token found in payload (raw=% X)", e.Packet.Data))
			}

			if id, ok := decodeLeadingVL64(e.Packet.Data); ok {
				lastTradePartnerID = id
				tradeStarterTradeID = id
			} else {
				a.AddLogMsg("[TRADE_OPEN] unable to decode incoming trade-open trade_id")
			}

			resolved := false
			if tradeStarterToken != "" {
				if user, ok := lookupUsers28UserByToken(tradeStarterToken); ok {
					tradeStarterName = strings.TrimSpace(user.Username)
					tradeStarterChatID = user.ChatID
					if user.TradeID > 0 {
						tradeStarterTradeID = user.TradeID
						lastTradePartnerID = user.TradeID
					}
					if strings.TrimSpace(user.TokenHex) != "" {
						tradeStarterToken = strings.TrimSpace(user.TokenHex)
						lastTradePartnerToken = tradeStarterToken
					}
					lastTradePartnerName = tradeStarterName
					tradeStarterLocked = tradeStarterName != ""
					resolved = tradeStarterLocked
					a.AddLogMsg(fmt.Sprintf("[TRADE_OPEN] resolved starter from trade token %q -> name=%q chat_id=%d trade_id=%d", tradeStarterToken, tradeStarterName, tradeStarterChatID, tradeStarterTradeID))
				}
			}

			if !resolved && tradeStarterTradeID > 0 {
				if user, ok := lookupUsers28UserByTradeID(tradeStarterTradeID); ok {
					tradeStarterName = strings.TrimSpace(user.Username)
					tradeStarterChatID = user.ChatID
					if strings.TrimSpace(user.TokenHex) != "" {
						tradeStarterToken = strings.TrimSpace(user.TokenHex)
						lastTradePartnerToken = tradeStarterToken
					}
					lastTradePartnerName = tradeStarterName
					tradeStarterLocked = tradeStarterName != ""
					resolved = tradeStarterLocked
					a.AddLogMsg(fmt.Sprintf("[TRADE_OPEN] resolved starter from parsed USERS28 trade_id %d -> name=%q chat_id=%d", tradeStarterTradeID, tradeStarterName, tradeStarterChatID))
				} else if name, ok := waitForUsers28TradeIDName(tradeStarterTradeID, 1200*time.Millisecond); ok {
					tradeStarterName = strings.TrimSpace(name)
					lastTradePartnerName = tradeStarterName
					if chatIdx, ok := waitForUsers28RoomIndexByName(tradeStarterName, 900*time.Millisecond); ok {
						tradeStarterChatID = chatIdx
					}
					tradeStarterLocked = tradeStarterName != ""
					resolved = tradeStarterLocked
					a.AddLogMsg(fmt.Sprintf("[TRADE_OPEN] resolved starter after wait from parsed USERS28 trade_id %d -> name=%q chat_id=%d", tradeStarterTradeID, tradeStarterName, tradeStarterChatID))
				} else {
					a.AddLogMsg(fmt.Sprintf("[TRADE_OPEN] parsed USERS28 has no current user for trade_id %d", tradeStarterTradeID))
				}
			}

			if tradeStarterLocked && tradeStarterChatID <= 0 && tradeStarterName != "" {
				if chatIdx, ok := lookupRoomEntityIndexByName(tradeStarterName); ok && chatIdx > 0 {
					tradeStarterChatID = chatIdx
				} else if chatIdx, ok := waitForUsers28RoomIndexByName(tradeStarterName, 900*time.Millisecond); ok && chatIdx > 0 {
					tradeStarterChatID = chatIdx
				} else if chatIdx, ok := lookupUsers28RoomIndexByName(tradeStarterName); ok && chatIdx > 0 {
					tradeStarterChatID = chatIdx
				}
			}

			a.AddLogMsg(fmt.Sprintf("[TRADE_STARTER] locked=%t name=%q trade_id=%d chat_id=%d token=%q", tradeStarterLocked, tradeStarterName, tradeStarterTradeID, tradeStarterChatID, tradeStarterToken))
		}

		tradePayload := strings.TrimSpace(string(e.Packet.Data))
		if tradePayload == "" {
			tradePayload = "(empty payload)"
		}

		tradeOpenCount++
		lastTradeOpenData = string(e.Packet.Data)
		lastTradeOpen = fmt.Sprintf("Incoming[%d] -> %s", e.Packet.Header.Value, tradePayload)
		if lastTradePartnerID > 0 {
			partnerID := lastTradePartnerID
			outPreview := string(ext.NewPacket(out.TRADE_OPEN, partnerID).Data)
			a.AddLogMsg(fmt.Sprintf("[TRADE_OPEN #%d] derived outgoing[%d] -> %s (partner id %d)", tradeOpenCount, 71, outPreview, partnerID))
		} else {
			a.AddLogMsg(fmt.Sprintf("[TRADE_OPEN #%d] unresolved reopen target from strict parsed USERS28 state", tradeOpenCount))
		}
		awaitingTradeOpen = false
		dealerAcceptingTrades = false
		dealerTradeWindowOpen = false
		log.Printf("[TRADE_OPEN #%d] %s", tradeOpenCount, lastTradeOpen)
		a.AddLogMsg(fmt.Sprintf("[TRADE_OPEN #%d] %s", tradeOpenCount, lastTradeOpen))
		stableTradePartnerID = tradeStarterTradeID
		if stableTradePartnerID <= 0 {
			stableTradePartnerID = lastTradePartnerID
		}
		stableTradePartnerName = strings.TrimSpace(tradeStarterName)
		if stableTradePartnerName == "" {
			stableTradePartnerName = strings.TrimSpace(lastTradePartnerName)
		}
		stableTradePartnerToken = strings.TrimSpace(tradeStarterToken)
		if stableTradePartnerToken == "" {
			stableTradePartnerToken = strings.TrimSpace(lastTradePartnerToken)
		}
		a.AddLogMsg(fmt.Sprintf("[TRADE_OPEN_STABLE] name=%q id=%d token=%q chat_id=%d", stableTradePartnerName, stableTradePartnerID, stableTradePartnerToken, tradeStarterChatID))

		partnerName := strings.TrimSpace(lastTradePartnerName)
		if partnerName == "" {
			partnerName = "Unknown"
		}
		if !isPayoutTradeOpen && !matchedRecentOutgoing {
			if partnerName == "" || strings.EqualFold(partnerName, "Unknown") {
				notify := "Sorry can't identify you from the current room-user state, please rejoin room and try again"
				a.AddLogMsg(fmt.Sprintf("[TRADE_OPEN] unresolved partner from strict parsed USERS28 state, cancelling trade: %s", notify))
				e.Block()
				ext.Send(out.TRADE_CLOSE)
				ext.Send(out.SHOUT, notify)
				return
			}
		}
		openMsg := fmt.Sprintf("Trade Opened: \"%s\"", partnerName)
		shouldAnnounceTradeOpen := true
		if payoutActive || payoutTradeSent || payoutTradeActive {
			shouldAnnounceTradeOpen = false
		}
		if shouldAnnounceTradeOpen {
			a.AddLogMsg(fmt.Sprintf("[TRADE_OPEN] shouting: %q", openMsg))
			ext.Send(out.SHOUT, openMsg)
		} else {
			a.AddLogMsg(fmt.Sprintf("[TRADE_OPEN] suppressed public shout during payout flow: %q", openMsg))
		}

		handItemsMu.Lock()
		// Under strict lifecycle we normally keep the snapshot captured before
		// Dealer Open. However, once the trade actually opens we must stop using
		// that old frozen view immediately so coverage checks cannot race against
		// stale inventory while the forced strip refresh is starting.
		if !strictTradeSnapshotLifecycle {
			tradeHandSnapshot = []TradeItem{}
			tradeHandSnapshotReady = false
			// Diagnostic: explicit log when non-strict lifecycle clears snapshot
			a.AddLogMsg("[TRADE_HAND_SNAPSHOT] cleared snapshot due to non-strict lifecycle on TRADE_OPEN")
		}
		handItemsMu.Unlock()

		// Always invalidate immediately on incoming trade open before the async
		// forced refresh goroutine starts. This prevents TRADE_ITEMS coverage
		// checks from using the previous round snapshot for even a single packet.
		a.invalidateTradeHandSnapshot("incoming trade open: awaiting forced refresh")

		// Mark trade as open so background hand rescans are skipped while
		// a frozen trade snapshot is being prepared.
		tradeOpen = true

		// Ensure any previous trade state is cleared so the first TRADE_ITEMS
		// packet for this new trade is interpreted correctly.
		tradeItemsMu.Lock()
		currentTradeItems = []TradeItem{}
		currentOwnTradeItems = []TradeItem{}
		tradeItemsMu.Unlock()

		// Reset last full-state so the next TRADE_ITEMS packet is treated as a
		// fresh baseline for delta calculations.
		tradeItemsMu.Lock()
		lastAllTradeItems = nil
		partnerAcceptedSnapshot = nil
		tradeItemsMu.Unlock()

		addItemMu.Lock()
		lastAddItemWasOurs = false
		addItemMu.Unlock()

		// Reset partner accept and any lingering trade-limit state for new trade
		partnerTradeAccepted = false
		tradeLimitWasActive = false
		lastTradeLimitNotice = ""
		stopTradeLimitMonitor()

		go func() {
			if ok := a.forceRefreshHandSnapshot("incoming trade open"); ok {
				a.notifyTradeQuantityCoverage()
			} else {
				a.AddLogMsg("[TRADE_HAND_SNAPSHOT] forced refresh failed on incoming trade open")
			}
		}()
		return
	}

	// TRADE_CLOSE appears as incoming header 110 in your client logs.
	if e.Packet.Header.Value == 110 {
		if hiddenBlockedTradeCleanupPending {
			hiddenBlockedTradeCleanupPending = false
		}
		if ignoreNextGuardCloseRecovery {
			ignoreNextGuardCloseRecovery = false
			a.AddLogMsg("[TRADE_GUARD] ignoring trade-close recovery for blocked foreign trade during active round")
			e.Block()
			return
		}

		stopUnderfundedTradeMonitor()
		stopTradeWindowTimeoutMonitor()
		wasCompleted := tradeCompleted
		tradeAutoConfirmed = true
		tradeAutoConfirmPending = false
		partnerName := strings.TrimSpace(lastTradePartnerName)
		if partnerName == "" {
			partnerName = "Unknown"
		}

		suppressCloseAnnouncement := suppressNextTradeCloseAnnouncement
		if payoutTradeActive && !wasCompleted {
			suppressCloseAnnouncement = true
		}
		suppressNextTradeCloseAnnouncement = false

		if !tradeCompleted && !tradeCloseAnnounced && !suppressCloseAnnouncement {
			closeMsg := fmt.Sprintf("Trade Closed: \"%s\"", partnerName)
			a.AddLogMsg(fmt.Sprintf("[TRADE_CLOSE] shouting: %q", closeMsg))
			ext.Send(out.SHOUT, closeMsg)
			tradeCloseAnnounced = true
		} else if suppressCloseAnnouncement {
			a.AddLogMsg("[TRADE_GUARD] suppressed trade closed announcement for forced guard-close")
		}

		tradeClosePayload := strings.TrimSpace(string(e.Packet.Data))
		if tradeClosePayload == "" {
			tradeClosePayload = "(empty payload)"
		}

		tradeCloseCount++
		closeLog := fmt.Sprintf("Incoming[%d] -> %s", e.Packet.Header.Value, tradeClosePayload)
		log.Printf("[TRADE_CLOSE #%d] %s", tradeCloseCount, closeLog)
		a.AddLogMsg(fmt.Sprintf("[TRADE_CLOSE #%d] %s", tradeCloseCount, closeLog))
		resetTradeAutoFlow()
		lastTradePartnerToken = ""

		// Clear trade items when trade closes. Ensure client/server trade
		// window state is cleared too.
		a.ClearTradeItems()

		// If a trade was open, we previously sent a delayed outgoing
		// TRADE_CLOSE to ensure UI cleared. That can race with a new
		// incoming trade-open; avoid sending a stray delayed close here.
		// The code paths that intentionally block trades (hidden/guard)
		// already send an immediate outgoing TRADE_CLOSE when needed.

		if !wasCompleted {
			// If this was part of a payout flow, retry the payout when either
			// the payout trade was active or we had recently attempted an
			// outgoing payout open (payoutActive && payoutTradeSent). The
			// latter case covers quick partner cancels where an incoming
			// TRADE_OPEN packet never arrived.
			if payoutTradeActive || (payoutActive && payoutTradeSent) {
				retryTargetID := payoutTargetID
				retryTargetName := payoutTargetName
				payoutTradeActive = false
				stopPayoutResponseTimeoutMonitor()

				payoutCancelCount++

				a.AddLogMsg(fmt.Sprintf("[PAYOUT] payout trade cancelled by %s, cancel count %d/5", retryTargetName, payoutCancelCount))
				a.noteCurrentGameHistory(fmt.Sprintf("Payout trade closed before completion; retry %d/5", payoutCancelCount))

				playerName := strings.TrimSpace(retryTargetName)
				if playerName == "" {
					playerName = "Player"
				}

				// Public notice at most once every 45 seconds
				if canAnnouncePayoutCancelNotice() {
					msg := fmt.Sprintf("%q closed trade", playerName)
					ext.Send(out.SHOUT, msg)
					markPayoutCancelNoticeSent()
				}

				if payoutCancelCount >= 5 {
					stopPayoutResponseTimeoutMonitor()
					stopPayout()
					resetPayoutRetryState()
					resetTradeAutoFlow()

					flagMsg := "User have cancelled trade too many times, flagged issue please go to rollorigins.club."
					ext.Send(out.SHOUT, flagMsg)

					a.markCurrentGameHistoryIssue(
						fmt.Sprintf("Payout trade cancelled too many times by %s", retryTargetName),
						true,
					)

					go a.reopenDealerIdle("payout issue: too many payout cancellations")
					return
				}

				// Reset the "sent" flag so the retry loop will resend the open
				// cleanly, then start a fresh payout attempt.
				payoutTradeSent = false
				startPayout(a, retryTargetID, retryTargetName)
				return
			} else {
				a.AddLogMsg("[TRADE_REOPEN] trade closed before completion, reopening dealer (idle recovery)")
				go a.reopenDealerIdle("incomplete trade close")
			}
		} else if payoutTradeActive {
			// Payout trade completed normally — clear active flag
			payoutTradeActive = false
			a.AddLogMsg("[PAYOUT] payout trade completed successfully")
			stopPayoutResponseTimeoutMonitor()
			resetPayoutRetryState()
			a.noteCurrentGameHistory("Dealer payout flow finished successfully")

			// Resync hand before reopening dealer trades.
			go a.resyncHandThenOpenDealer()
		}

		partnerID := lastTradePartnerID
		if partnerID <= 0 {
			requestRoomUsers(a)
			a.AddLogMsg("[TRADE_REOPEN] not ready: no last trade target, requested room users")
			return
		}

		a.AddLogMsg(fmt.Sprintf("[TRADE_REOPEN] ready for manual reopen -> %s (%d)", lastTradePartnerName, partnerID))
	}
}

func resetTradeAutoFlow() {
	tradeAutoFlowID++
	tradeAutoAccepted = false
	tradeAutoConfirmed = false
	tradeAutoAcceptPending = false
	tradeAutoConfirmPending = false
	tradeCompleted = false
	tradeCloseAnnounced = false
}

func resetPokerSequence() {
	pokerSequenceStage = 0
	pokerSequencePlayerName = ""
	pokerSequencePlayerResult = PokerHandResult{}
	pokerSequencePlayerHand = ""
}

func resetBlackjackSequence() {
	awaitingBlackjackDecision = false
	awaitingBlackjackDecisionPartnerID = 0
	awaitingBlackjackDecisionPartnerName = ""
	blackjackRoundActive = false
	blackjackPlayerTurn = false
	blackjackPlayerTotal = 0
	blackjackDealerTotal = 0
	blackjackPlayerName = ""
	blackjackHitInFlight = false
	blackjackNextHitIndex = 3
}

func reset13Sequence() {
	awaiting13Decision = false
	awaiting13DecisionPartnerID = 0
	awaiting13DecisionPartnerName = ""
	thirteenRoundActive = false
	thirteenPlayerTurn = false
	thirteenPlayerTotal = 0
	thirteenDealerTotal = 0
	thirteenPlayerName = ""
	thirteenHitInFlight = false
	thirteenNextHitIndex = 2
}

func resetTriSequence() {
	awaitingTriChoice = false
	awaitingTriChoicePartnerID = 0
	awaitingTriChoicePartnerName = ""
	triRoundActive = false
	triPlayerTurn = false
	triMode = ""
	triPlayerTotal = 0
	triDealerTotal = 0
	triPlayerName = ""
}

func stopPayout() {
	payoutActive = false
	payoutTradeActive = false
	payoutTargetID = 0
	payoutTargetName = ""
	payoutAttempts = 0
	payoutSessionID++
	payoutTradeSent = false
	payoutExpectedAddCount = 0
	payoutActualAddCount = 0
}

func stopPayoutResponseTimeoutMonitor() {
	payoutResponseTimeoutMonitorID++
	payoutResponseTimeoutActive = false
}

func resetPayoutRetryState() {
	stopPayoutResponseTimeoutMonitor()
	payoutResponseTimeoutAttempts = 0
	payoutCancelCount = 0
	lastPayoutCancelNoticeAt = time.Time{}
}

func canAnnouncePayoutCancelNotice() bool {
	return time.Since(lastPayoutCancelNoticeAt) >= 45*time.Second
}

func markPayoutCancelNoticeSent() {
	lastPayoutCancelNoticeAt = time.Now()
}

func resumeDealerAfterPayoutIssue(a *App, reason string) {
	a.AddLogMsg(fmt.Sprintf("[PAYOUT] resuming dealer after payout issue: %s", reason))

	// Clear payout state and retry counters first.
	stopPayout()
	resetPayoutRetryState()
	resetTradeAutoFlow()

	// Use the safe reopen path which performs the required resync/snapshot
	// refresh before announcing dealer open. reopenDealerIdle already stops
	// heartbeats/monitors, clears trade state and forces a hand snapshot.
	go a.reopenDealerIdle("payout issue: " + reason)
}

func (a *App) startPayoutResponseTimeoutMonitor(playerName string, targetID int, targetName string) {
	stopPayoutResponseTimeoutMonitor()

	payoutResponseTimeoutMonitorID++
	monitorID := payoutResponseTimeoutMonitorID
	payoutResponseTimeoutActive = true

	go func(id int, player string, retryTargetID int, retryTargetName string) {
		time.Sleep(45 * time.Second)

		if id != payoutResponseTimeoutMonitorID || !payoutResponseTimeoutActive || !payoutTradeActive {
			return
		}

		payoutResponseTimeoutActive = false
		payoutResponseTimeoutAttempts++

		a.AddLogMsg(fmt.Sprintf("[PAYOUT_TIMEOUT] payout response timeout %d/3 for %s", payoutResponseTimeoutAttempts, player))

		timeoutMsg := fmt.Sprintf("%q did not accept trade", player)
		ext.Send(out.SHOUT, timeoutMsg)

		time.Sleep(1200 * time.Millisecond)
		// Mark this as a forced/local close so incoming TRADE_CLOSE isn't
		// treated as a player cancel and no false "closed trade" shout
		// is emitted by the incoming handler.
		hiddenBlockedTradeCleanupPending = true
		ignoreNextGuardCloseRecovery = true
		suppressNextTradeCloseAnnouncement = true
		ext.Send(out.TRADE_CLOSE)

		if payoutResponseTimeoutAttempts >= 3 {
			flagMsg := "We have flagged the issues, Please go to rollorigins.club to resolve."
			time.Sleep(1200 * time.Millisecond)
			ext.Send(out.SHOUT, flagMsg)

			a.markCurrentGameHistoryIssue(
				fmt.Sprintf("Payout trade timed out 3 times waiting for %s to accept", player),
				true,
			)

			resumeDealerAfterPayoutIssue(a, "payout response timeout")
			return
		}

		// Retry payout open again
		time.Sleep(1500 * time.Millisecond)
		payoutTradeActive = false
		startPayout(a, retryTargetID, retryTargetName)
	}(monitorID, playerName, targetID, targetName)
}

func waitForHiddenBlockedTradeCleanup(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !hiddenBlockedTradeCleanupPending {
			return true
		}
		time.Sleep(75 * time.Millisecond)
	}
	return !hiddenBlockedTradeCleanupPending
}

func startPayout(a *App, targetID int, targetName string) {
	stopPayout()
	payoutActive = true
	payoutTargetID = targetID
	payoutTargetName = targetName
	payoutSessionID++
	sessionID := payoutSessionID
	a.noteCurrentGameHistory(fmt.Sprintf("Payout started for %s", targetName))

	go func() {
		// Small delay so the winner shout clears Habbo's rate limiter first
		time.Sleep(1200 * time.Millisecond)

		for attempt := 1; attempt <= 5; attempt++ {
			if sessionID != payoutSessionID {
				return
			}
			payoutAttempts = attempt

			if hiddenBlockedTradeCleanupPending {
				a.AddLogMsg("[PAYOUT_DEBUG] waiting for blocked-trade cleanup before opening payout trade")
				ext.Send(out.TRADE_CLOSE)
				waitForHiddenBlockedTradeCleanup(2 * time.Second)
			}

			if strings.TrimSpace(targetName) != "" {
				expectedToken, _ := lookupTokenByName(targetName)
				a.AddLogMsg(fmt.Sprintf("[PAYOUT_DEBUG] attempt %d target=%q currentTargetID=%d expectedToken=%q", attempt, targetName, targetID, expectedToken))
				go requestRoomUsers(a)
				if resolvedID, ok := waitForRoomEntityIndexByName(targetName, 900*time.Millisecond); ok && resolvedID > 0 && resolvedID != targetID {
					a.AddLogMsg(fmt.Sprintf("[PAYOUT] refreshed %s target from ROOM_USERS index %d -> %d", targetName, targetID, resolvedID))
					targetID = resolvedID
					payoutTargetID = resolvedID
				} else if resolvedID, ok := waitForUsers28TradeIDByName(targetName, 700*time.Millisecond); ok && resolvedID > 0 && resolvedID != targetID {
					a.AddLogMsg(fmt.Sprintf("[PAYOUT] refreshed %s target from USERS28 trade id %d -> %d", targetName, targetID, resolvedID))
					targetID = resolvedID
					payoutTargetID = resolvedID
				} else if resolvedID, ok := waitForUsers28NameIndex(targetName, 700*time.Millisecond); ok {
					a.AddLogMsg(fmt.Sprintf("[PAYOUT_DEBUG] generic USERS28 name->index fallback produced %d for %s (current target %d)", resolvedID, targetName, targetID))
					// Use the resolved room/users28 index as the outgoing target
					targetID = resolvedID
					payoutTargetID = resolvedID
				}
			}

			a.AddLogMsg(fmt.Sprintf("[PAYOUT] opening trade with %s (%d), attempt %d/5", targetName, targetID, attempt))
			payoutTradeSent = true
			rememberOutgoingTradeOpenTarget(targetID)
			outPreview := string(ext.NewPacket(out.TRADE_OPEN, targetID).Data)
			a.AddLogMsg(fmt.Sprintf("[PAYOUT] outgoing[71] payload=%q", outPreview))
			ext.Send(out.TRADE_OPEN, targetID)

			// Fallback: send the raw VL64 payload form as well. Some sessions are picky about payload composition.
			rawPayload := encodeVL64(targetID)
			a.AddLogMsg(fmt.Sprintf("[PAYOUT] outgoing[71] raw payload fallback=%q", rawPayload))
			ext.Send(g.Out.Id("TRADE_OPEN"), []byte(rawPayload))

			if attempt > 1 {
				a.AddLogMsg(fmt.Sprintf("[PAYOUT] retry trade open attempt %d/5 for %s", attempt, targetName))
			}

			// Wait up to 5 seconds for the trade to open (header 104 will call stopPayout)
			for i := 0; i < 50; i++ {
				time.Sleep(100 * time.Millisecond)
				if sessionID != payoutSessionID {
					// Trade opened (or externally cancelled) — done
					return
				}
			}

			// Trade didn't open after 5s, loop for next attempt
		}

		// All 5 attempts exhausted
		if sessionID == payoutSessionID {
			msg := "Recorded game history and flagged"
			a.AddLogMsg(fmt.Sprintf("[PAYOUT] all attempts exhausted, shouting: %q", msg))
			a.markCurrentGameHistoryIssue("Payout trade failed to open after all retry attempts", true)
			sendMessageWithDelay(msg)
			stopPayout()
			// Resume normal dealer-open cycle
			awaitingTradeOpen = true
			dealerAcceptingTrades = true
			if shouldAnnounceDealerOpen() {
				dealerTradeWindowOpen = true
				go sendMessageWithDelay(a.dealerOpenMessage())
			}
			startDealerOpenHeartbeat(a)
		}
	}()
}

func (a *App) autoAddPayoutItems() {
	time.Sleep(600 * time.Millisecond) // settle time after trade opens

	// Always begin payout selection from a fresh completed strip scan so we do
	// not choose ids from a stale or partial hand snapshot from the previous
	// round. This is the critical guard for cases where the dealer just lost or
	// re-added the same item type and the live hand cache has not caught up yet.
	if ok := a.forceRefreshHandSnapshot("payout auto-add start"); !ok {
		a.AddLogMsg("[PAYOUT] aborting auto-add because forced hand refresh failed at payout start")
		return
	}

	betItems := gameBetItems
	if len(betItems) == 0 {
		a.AddLogMsg("[PAYOUT] no bet items recorded, skipping auto-add")
		return
	}

	a.AddLogMsg(fmt.Sprintf("[PAYOUT_DEBUG] auto-add start: bet item types=%d", len(betItems)))
	for i, betItem := range betItems {
		a.AddLogMsg(fmt.Sprintf("[PAYOUT_DEBUG] bet[%d] name=%q qty=%d payoutTarget=%d", i, betItem.Name, betItem.Quantity, betItem.Quantity*2))
	}

	required := payoutRequirementsFromBetItems(betItems)
	// Compute total required items up-front so outgoing intercept can
	// track progress against this expected count while we send adds.
	requiredTotal := 0
	for _, need := range required {
		requiredTotal += need
	}
	payoutExpectedAddCount = requiredTotal
	payoutActualAddCount = 0
	selectedByName := map[string][]int{}
	usedIDs := map[int]struct{}{}

	// Try current hand first, then rescan hand pages if we are still short.
	for attempt := 1; attempt <= 3; attempt++ {
		handSnapshot := snapshotHandItemIDs()
		for name, needQty := range required {
			if needQty <= 0 {
				continue
			}

			already := len(selectedByName[name])
			if already >= needQty {
				continue
			}

			candidates := uniqueInts(handSnapshot[name])
			for _, id := range candidates {
				if _, seen := usedIDs[id]; seen {
					continue
				}
				selectedByName[name] = append(selectedByName[name], id)
				usedIDs[id] = struct{}{}
				if len(selectedByName[name]) >= needQty {
					break
				}
			}
		}

		missing := payoutMissingCounts(required, selectedByName)
		if len(missing) == 0 {
			break
		}

		if attempt < 3 {
			a.AddLogMsg(fmt.Sprintf("[PAYOUT_DEBUG] payout still short after hand scan attempt %d, forcing full rescan: %s", attempt, formatMissingCounts(missing)))
			if ok := a.forceRefreshHandSnapshot(fmt.Sprintf("payout auto-add retry %d", attempt+1)); !ok {
				a.AddLogMsg(fmt.Sprintf("[PAYOUT] forced hand refresh failed during payout retry %d", attempt+1))
				break
			}
		} else {
			a.AddLogMsg(fmt.Sprintf("[PAYOUT_DEBUG] payout still short after final hand scan attempt: %s", formatMissingCounts(missing)))
		}
	}

	total := 0
	plannedIDs := make([]int, 0)
	for _, betItem := range betItems {
		needed := required[betItem.Name]
		toAdd := selectedByName[betItem.Name]
		a.AddLogMsg(fmt.Sprintf("[PAYOUT_DEBUG] selected ids for %s: selected=%d needed=%d", betItem.Name, len(toAdd), needed))
		if len(toAdd) < needed {
			a.AddLogMsg(fmt.Sprintf("[PAYOUT] warning: need %d of %s but only found %d unique item ids", needed, betItem.Name, len(toAdd)))
		}
		for _, itemID := range toAdd {
			if !payoutTradeActive {
				a.AddLogMsg("[PAYOUT] trade closed mid-add, stopping")
				return
			}
			time.Sleep(550 * time.Millisecond)
			ext.Send(out.TRADE_ADDITEM, -itemID)
			plannedIDs = append(plannedIDs, itemID)
			total++
			payload := string(ext.NewPacket(out.TRADE_ADDITEM, -itemID).Data)
			a.AddLogMsg(fmt.Sprintf("[PAYOUT] added item %d (%s) %d/%d payload=%q", itemID, betItem.Name, total, needed, payload))
			a.AddLogMsg(fmt.Sprintf("[PAYOUT_DEBUG] added id %d (planned so far %d/%d)", itemID, len(plannedIDs), payoutExpectedAddCount))
		}
	}
	a.AddLogMsg(fmt.Sprintf("[PAYOUT] auto-add complete: %d item(s) offered", total))

	// Accept only after full payout placement has been queued.
	fullyPlanned := true
	for name, need := range required {
		if len(selectedByName[name]) < need {
			fullyPlanned = false
		}
	}

	if payoutTradeActive && !tradeAutoAccepted {
		if fullyPlanned && total >= requiredTotal && payoutActualAddCount >= requiredTotal {
			time.Sleep(450 * time.Millisecond)
			ext.Send(out.TRADE_ACCEPT)
			tradeAutoAccepted = true
			tradeAutoAcceptPending = false
			a.AddLogMsg(fmt.Sprintf("[PAYOUT] auto-accept sent after full payout placement (%d/%d sent=%d)", total, requiredTotal, payoutActualAddCount))

			// Start the payout response timeout monitor only after we've
			// accepted the payout. This avoids the 30s partner-accept timeout
			// from running while we are still auto-adding potentially large
			// numbers of items.
			if payoutTradeActive && !payoutResponseTimeoutActive {
				a.AddLogMsg("[PAYOUT] starting payout response timeout monitor after dealer auto-accept")
				a.startPayoutResponseTimeoutMonitor(payoutTargetName, payoutTargetID, payoutTargetName)
			}
		} else {
			a.AddLogMsg(fmt.Sprintf("[PAYOUT_DEBUG] accept deferred: planned=%d/%d sent=%d/%d", total, requiredTotal, payoutActualAddCount, requiredTotal))
		}
	}

	go a.verifyAndRetryPayoutAdds(plannedIDs)
}

func snapshotHandItemIDs() map[string][]int {
	handItemsMu.Lock()
	defer handItemsMu.Unlock()

	snapshot := make(map[string][]int, len(currentHandItemIDs))
	for name, ids := range currentHandItemIDs {
		copyIDs := make([]int, len(ids))
		copy(copyIDs, ids)
		snapshot[name] = copyIDs
	}
	return snapshot
}

func uniqueInts(ids []int) []int {
	if len(ids) == 0 {
		return nil
	}
	seen := map[int]struct{}{}
	unique := make([]int, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		key := absInt(id)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, id)
	}
	return unique
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func payoutMissingCounts(required map[string]int, selected map[string][]int) map[string]int {
	missing := map[string]int{}
	for name, need := range required {
		have := len(selected[name])
		if have < need {
			missing[name] = need - have
		}
	}
	return missing
}

func formatMissingCounts(missing map[string]int) string {
	if len(missing) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(missing))
	for name, qty := range missing {
		parts = append(parts, fmt.Sprintf("%s:%d", name, qty))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func ownTradeOfferTotal() int {
	tradeItemsMu.Lock()
	defer tradeItemsMu.Unlock()
	total := 0
	for _, item := range currentOwnTradeItems {
		total += item.Quantity
	}
	return total
}

func payoutRequirementsFromBetItems(betItems []TradeItem) map[string]int {
	required := map[string]int{}
	for _, item := range betItems {
		if item.Quantity <= 0 {
			continue
		}
		required[item.Name] += item.Quantity * 2
	}
	return required
}

func ownTradeOfferCounts() map[string]int {
	tradeItemsMu.Lock()
	defer tradeItemsMu.Unlock()
	counts := map[string]int{}
	for _, item := range currentOwnTradeItems {
		counts[item.Name] += item.Quantity
	}
	return counts
}

func (a *App) ownTradeHasRequiredPayoutOffer(required map[string]int) (bool, string) {
	have := ownTradeOfferCounts()
	for name, qty := range required {
		if have[name] < qty {
			return false, fmt.Sprintf("%s have=%d need=%d", name, have[name], qty)
		}
	}
	return true, ""
}

func (a *App) tryAcceptPayoutTrade(required map[string]int, source string) bool {
	if !payoutTradeActive {
		return false
	}
	if tradeAutoAccepted {
		return true
	}
	ok, detail := a.ownTradeHasRequiredPayoutOffer(required)
	if !ok {
		a.AddLogMsg(fmt.Sprintf("[PAYOUT_DEBUG] not accepting yet (%s): %s", source, detail))
		return false
	}

	ext.Send(out.TRADE_ACCEPT)
	tradeAutoAccepted = true
	tradeAutoAcceptPending = false
	a.AddLogMsg(fmt.Sprintf("[PAYOUT] auto-accept sent after verifying payout items (%s)", source))

	// Start response timeout monitor after dealer (us) accepted.
	if payoutTradeActive && !payoutResponseTimeoutActive {
		a.AddLogMsg("[PAYOUT] starting payout response timeout monitor after dealer accept (verified)")
		a.startPayoutResponseTimeoutMonitor(payoutTargetName, payoutTargetID, payoutTargetName)
	}
	return true
}

func (a *App) verifyAndRetryPayoutAdds(plannedIDs []int) {
	if len(plannedIDs) == 0 {
		return
	}

	required := payoutRequirementsFromBetItems(gameBetItems)
	if len(required) == 0 {
		a.AddLogMsg("[PAYOUT_DEBUG] no payout requirements found while verifying add")
		return
	}
	requiredTotal := 0
	for _, need := range required {
		requiredTotal += need
	}

	// Give the server time to echo TRADE_ITEMS updates.
	time.Sleep(2500 * time.Millisecond)
	if !payoutTradeActive {
		return
	}

	ownTotal := ownTradeOfferTotal()
	a.AddLogMsg(fmt.Sprintf("[PAYOUT_DEBUG] post-add own offer total=%d planned=%d", ownTotal, len(plannedIDs)))
	if a.tryAcceptPayoutTrade(required, "after-negative-add") {
		return
	}

	// Fallback for sessions where TRADE_ADDITEM expects a positive item id.
	a.AddLogMsg("[PAYOUT_DEBUG] own offer still empty after auto-add, retrying with positive item IDs")
	for _, itemID := range plannedIDs {
		if !payoutTradeActive {
			return
		}
		time.Sleep(450 * time.Millisecond)
		ext.Send(out.TRADE_ADDITEM, itemID)
		if payoutTradeActive {
			payoutActualAddCount++
		}
		payload := string(ext.NewPacket(out.TRADE_ADDITEM, itemID).Data)
		a.AddLogMsg(fmt.Sprintf("[PAYOUT_DEBUG] retry add item +%d payload=%q", itemID, payload))
		a.AddLogMsg(fmt.Sprintf("[PAYOUT_DEBUG] sent count now %d/%d", payoutActualAddCount, payoutExpectedAddCount))
	}

	// Wait again for trade echo, then only accept if exact payout offer is present.
	time.Sleep(2200 * time.Millisecond)
	if a.tryAcceptPayoutTrade(required, "after-positive-retry") {
		return
	}

	if payoutTradeActive && !tradeAutoAccepted && len(plannedIDs) >= requiredTotal && payoutActualAddCount >= requiredTotal {
		// Attribution can be unreliable in this direction; once full payout has been queued,
		// accept without waiting for the player to accept first.
		ext.Send(out.TRADE_ACCEPT)
		tradeAutoAccepted = true
		tradeAutoAcceptPending = false
		a.AddLogMsg(fmt.Sprintf("[PAYOUT] auto-accept fallback after queued full payout (%d/%d sent=%d)", len(plannedIDs), requiredTotal, payoutActualAddCount))

		// Start response timeout monitor after dealer accept (fallback path).
		if payoutTradeActive && !payoutResponseTimeoutActive {
			a.AddLogMsg("[PAYOUT] starting payout response timeout monitor after dealer auto-accept (fallback)")
			a.startPayoutResponseTimeoutMonitor(payoutTargetName, payoutTargetID, payoutTargetName)
		}
		return
	}

	a.AddLogMsg("[PAYOUT_DEBUG] payout items still not fully reflected in own trade offer; waiting for manual intervention")
	a.noteCurrentGameHistory("Payout items did not fully reflect in trade offer; manual review may be needed")
}

func stopUnderfundedTradeMonitor() {
	// Disabled underfunded trade monitor per user request.
	underfundedTradeMonitorID++
	underfundedTradeMonitorNotice = ""
}

func startTradeWindowTimeoutMonitor(a *App) {
	tradeWindowTimeoutMonitorID++
	monitorID := tradeWindowTimeoutMonitorID
	tradeWindowOpenedAt = time.Now()
	tradeWindowDeadline = tradeWindowOpenedAt.Add(45 * time.Second)
	tradeWindowTimeoutActive = true

	go func(id int) {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			if id != tradeWindowTimeoutMonitorID {
				return
			}

			if !tradeWindowTimeoutActive {
				return
			}

			if time.Now().Before(tradeWindowDeadline) {
				continue
			}

			tradeWindowTimeoutActive = false
			msg := "closing trade window opened for too long"
			a.AddLogMsg("[TRADE_TIMEOUT] " + msg)
			a.markCurrentGameHistoryIssue("Trade window stayed open too long and was force closed", true)
			ext.Send(out.SHOUT, msg)
			ext.Send(out.TRADE_CLOSE)
			return
		}
	}(monitorID)
}

func extendTradeWindowTimeoutForPartnerActivity(a *App) {
	if !tradeWindowTimeoutActive {
		return
	}

	now := time.Now()
	maxDeadline := tradeWindowOpenedAt.Add(90 * time.Second)
	if now.After(maxDeadline) {
		return
	}

	extendedDeadline := now.Add(20 * time.Second)
	if extendedDeadline.After(maxDeadline) {
		extendedDeadline = maxDeadline
	}

	if extendedDeadline.After(tradeWindowDeadline) {
		tradeWindowDeadline = extendedDeadline
		seconds := int(tradeWindowDeadline.Sub(tradeWindowOpenedAt).Seconds())
		a.AddLogMsg(fmt.Sprintf("[TRADE_TIMEOUT] partner adding items, extending window to %ds max", seconds))
	}
}

func stopTradeWindowTimeoutMonitor() {
	if !tradeWindowTimeoutActive {
		return
	}
	tradeWindowTimeoutMonitorID++
	tradeWindowTimeoutActive = false
}

func stopGameChoiceTimeoutMonitor() {
	gameChoiceTimeoutMonitorID++
	gameChoiceTimeoutActive = false
}

func (a *App) startGameChoiceTimeoutMonitor() {
	stopGameChoiceTimeoutMonitor()

	gameChoiceTimeoutMonitorID++
	monitorID := gameChoiceTimeoutMonitorID
	gameChoiceTimeoutActive = true

	playerName := strings.TrimSpace(awaitingGameChoicePartnerName)
	if playerName == "" {
		playerName = strings.TrimSpace(lastTradePartnerName)
	}
	if playerName == "" {
		playerName = "Player"
	}

	go func(id int, player string) {
		// First 30 seconds
		time.Sleep(30 * time.Second)

		if id != gameChoiceTimeoutMonitorID || !gameChoiceTimeoutActive || !awaitingGameChoice {
			return
		}

		reminder := "Shout pkr, 21, 13, Tri"
		a.AddLogMsg(fmt.Sprintf("[GAME_CHOICE_TIMEOUT] 30s no response, repeating prompt for %s", player))
		ext.Send(out.SHOUT, reminder)

		// Another 30 seconds
		time.Sleep(30 * time.Second)

		if id != gameChoiceTimeoutMonitorID || !gameChoiceTimeoutActive || !awaitingGameChoice {
			return
		}

		// Final timeout hit
		awaitingGameChoice = false
		gameChoiceUnreadableWarned = false
		awaitingGameChoicePartnerID = 0
		awaitingGameChoicePartnerName = ""
		gameChoiceTimeoutActive = false

		closeMsg := fmt.Sprintf("Closing trade no response from %q", player)
		flagMsg := "We have flagged this game, please advise us on rollorigins.club"

		a.AddLogMsg(fmt.Sprintf("[GAME_CHOICE_TIMEOUT] final timeout for %s", player))

		ext.Send(out.SHOUT, closeMsg)
		time.Sleep(1200 * time.Millisecond)

		ext.Send(out.SHOUT, flagMsg)

		a.markCurrentGameHistoryIssue(
			fmt.Sprintf("No game choice response from %s after 60 seconds", player),
			true,
		)

		time.Sleep(1200 * time.Millisecond)
		ext.Send(out.TRADE_CLOSE)

		time.Sleep(1500 * time.Millisecond)
		go a.reopenDealerIdle("game choice timeout")
	}(monitorID, playerName)
}

func startUnderfundedTradeMonitor(a *App, notice string) {
	// Underfunded trade monitor disabled per user request.
	underfundedTradeMonitorID++
	underfundedTradeMonitorNotice = ""
}

// startShortageMonitor begins a short-lived monitor that will force-close
// a trade if reported shortages remain unresolved for the given timeout.
func startShortageMonitor(a *App, timeout time.Duration) {
	shortageMonitorID++
	id := shortageMonitorID
	shortageMonitorActive = true
	shortageMonitorDeadline = time.Now().Add(timeout)

	go func(monitor int) {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			if monitor != shortageMonitorID {
				return
			}
			if !shortageMonitorActive {
				return
			}

			// If trade closed externally, stop the monitor.
			if !tradeOpen {
				stopShortageMonitor()
				return
			}

			// If shortages resolved, stop the monitor.
			if len(a.getTradeCoverageShortages()) == 0 {
				stopShortageMonitor()
				return
			}

			// If deadline passed, close the trade to free the booth.
			if time.Now().After(shortageMonitorDeadline) {
				partnerName := strings.TrimSpace(lastTradePartnerName)
				if partnerName == "" {
					partnerName = "Player"
				}
				a.AddLogMsg(fmt.Sprintf("[TRADE_COVERAGE] shortage unresolved; force-closing trade with %s", partnerName))
				ext.Send(out.SHOUT, "Sorry none avabile to see my hand - rollorigins.club")
				ext.Send(out.TRADE_CLOSE)
				stopShortageMonitor()
				return
			}
		}
	}(id)
}

func stopShortageMonitor() {
	shortageMonitorID++
	shortageMonitorActive = false
	shortageMonitorDeadline = time.Time{}
}

// startTradeLimitMonitor begins a short-lived monitor that will force-close
// a trade if a trade-limit violation remains unresolved for the given timeout.
func startTradeLimitMonitor(a *App, timeout time.Duration) {
	tradeLimitMonitorID++
	id := tradeLimitMonitorID
	tradeLimitMonitorActive = true
	tradeLimitMonitorDeadline = time.Now().Add(timeout)

	go func(monitor int) {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			if monitor != tradeLimitMonitorID {
				return
			}
			if !tradeLimitMonitorActive {
				return
			}

			// If trade closed externally, stop the monitor.
			if !tradeOpen {
				stopTradeLimitMonitor()
				return
			}

			// If violation resolved, stop the monitor.
			tradeItemsMu.Lock()
			itemsCopy := make([]TradeItem, len(currentTradeItems))
			copy(itemsCopy, currentTradeItems)
			tradeItemsMu.Unlock()
			if getTradeLimitViolation(itemsCopy) == nil {
				stopTradeLimitMonitor()
				return
			}

			// If deadline passed, close the trade to free the booth.
			if time.Now().After(tradeLimitMonitorDeadline) {
				partnerName := strings.TrimSpace(lastTradePartnerName)
				if partnerName == "" {
					partnerName = "Player"
				}
				a.AddLogMsg(fmt.Sprintf("[TRADE_LIMIT] unresolved; force-closing trade with %s", partnerName))
				ext.Send(out.SHOUT, "Trade still over limit; closing now.")
				ext.Send(out.TRADE_CLOSE)
				stopTradeLimitMonitor()
				return
			}
		}
	}(id)
}

func stopTradeLimitMonitor() {
	tradeLimitMonitorID++
	tradeLimitMonitorActive = false
	tradeLimitMonitorDeadline = time.Time{}
}

func startDealerOpenHeartbeat(a *App) {
	dealerOpenHeartbeatID++
	id := dealerOpenHeartbeatID
	dealerOpenHeartbeatActive = true
	dealerOpenMsg := "Dealer Open, See Whats in My Hand - rollorigins.club"
	if a != nil {
		dealerOpenMsg = a.dealerOpenMessage()
	}
	addLog := func(msg string) {
		if a != nil {
			a.AddLogMsg(msg)
		}
	}

	go func(id int, openMsg string) {
		// Initial 15s delay for the first re-announcement.
		timer := time.NewTimer(15 * time.Second)
		defer timer.Stop()

		select {
		case <-timer.C:
			if id != dealerOpenHeartbeatID {
				dealerOpenHeartbeatActive = false
				return
			}
			if !awaitingTradeOpen || !dealerTradeWindowOpen {
				dealerOpenHeartbeatActive = false
				return
			}
			if !dealerDiceReady() {
				addLog("[TRADE_REOPEN] dice not ready; stopping reopen heartbeat (initial)")
				log.Printf("[TRADE_REOPEN] dice not ready; stopping reopen heartbeat (initial)")
				dealerTradeWindowOpen = false
				dealerOpenHeartbeatActive = false
				return
			}
			// First shout: only if not muted.
			if isMuted {
				addLog("[TRADE_REOPEN] initial 15s announcer skipped due to mute")
				log.Printf("[TRADE_REOPEN] initial 15s announcer skipped due to mute")
			} else {
				addLog("[TRADE_REOPEN] initial 15s re-announcing dealer open")
				log.Printf("[TRADE_REOPEN] initial 15s re-announcing dealer open")
				sendMessageWithDelay(openMsg)
			}
		}

		// After the first attempt, run a steady 30s announcer that fires for everyone.
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			if id != dealerOpenHeartbeatID {
				dealerOpenHeartbeatActive = false
				return
			}
			if !awaitingTradeOpen || !dealerTradeWindowOpen {
				dealerOpenHeartbeatActive = false
				return
			}
			if !dealerDiceReady() {
				addLog("[TRADE_REOPEN] dice not ready; stopping 45s announcer")
				log.Printf("[TRADE_REOPEN] dice not ready; stopping 45s announcer")
				dealerTradeWindowOpen = false
				dealerOpenHeartbeatActive = false
				return
			}

			addLog("[TRADE_REOPEN] 30s periodic dealer-open announcer firing")
			log.Printf("[TRADE_REOPEN] 30s periodic dealer-open announcer firing")
			sendMessageWithDelay(openMsg)
		}
	}(id, dealerOpenMsg)
}

func stopDealerOpenHeartbeat() {
	if !dealerOpenHeartbeatActive {
		return
	}
	dealerOpenHeartbeatID++
	dealerOpenHeartbeatActive = false
}

func scheduleAutoTradeAccept(a *App, payload string) {
	if strings.TrimSpace(lastTradePartnerToken) == "" {
		return
	}
	if tradeAutoAccepted || tradeAutoAcceptPending {
		return
	}
	if !payoutTradeActive {
		if s := a.getTradeCoverageShortages(); s != nil && len(s) > 0 {
			a.AddLogMsg("[TRADE_ACCEPT] skipped auto-accept due to insufficient payout stock")
			return
		}

		// Also ensure the incoming partner offer respects configured trade limits
		tradeItemsMu.Lock()
		itemsCopy := make([]TradeItem, len(currentTradeItems))
		copy(itemsCopy, currentTradeItems)
		tradeItemsMu.Unlock()
		if v := getTradeLimitViolation(itemsCopy); v != nil {
			a.AddLogMsg("[TRADE_ACCEPT] skipped auto-accept due to active trade limit violation")
			return
		}
		// If snapshot not ready (s == nil) allow scheduling and perform a
		// short wait inside the confirm loop before sending each confirm.
	}

	tradeAutoAcceptPending = true
	flowID := tradeAutoFlowID
	a.AddLogMsg(fmt.Sprintf("[TRADE_ACCEPT #109] detected (%q), auto-accept in 2s", payload))

	go func(flow int) {
		time.Sleep(2 * time.Second)

		if flow != tradeAutoFlowID || strings.TrimSpace(lastTradePartnerToken) == "" {
			tradeAutoAcceptPending = false
			return
		}

		// If not in payout mode, ensure we have a recent hand snapshot and
		// that there are no coverage shortages before accepting. Treat a
		// nil result from getTradeCoverageShortages() as "snapshot not
		// ready" and wait briefly for it to become available.
		if !payoutTradeActive {
			deadline := time.Now().Add(1 * time.Second)
			for {
				if flow != tradeAutoFlowID || strings.TrimSpace(lastTradePartnerToken) == "" {
					tradeAutoAcceptPending = false
					return
				}
				s := a.getTradeCoverageShortages()
				if s == nil {
					if time.Now().After(deadline) {
						tradeAutoAcceptPending = false
						a.AddLogMsg("[TRADE_ACCEPT] canceled auto-accept: hand snapshot unavailable")
						return
					}
					time.Sleep(100 * time.Millisecond)
					continue
				}
				if len(s) > 0 {
					tradeAutoAcceptPending = false
					a.AddLogMsg("[TRADE_ACCEPT] canceled auto-accept due to insufficient payout stock")
					return
				}
				// Re-check trade-limits immediately before accepting in case the
				// player modified the offered items during the wait.
				tradeItemsMu.Lock()
				itemsCopy := make([]TradeItem, len(currentTradeItems))
				copy(itemsCopy, currentTradeItems)
				tradeItemsMu.Unlock()
				if v := getTradeLimitViolation(itemsCopy); v != nil {
					tradeAutoAcceptPending = false
					a.AddLogMsg("[TRADE_ACCEPT] canceled auto-accept because trade became invalid during wait")
					return
				}
				break
			}
		}

		ext.Send(out.TRADE_ACCEPT)
		tradeAutoAcceptPending = false
		tradeAutoAccepted = true
		a.AddLogMsg("[TRADE_ACCEPT] sent outgoing[69]")
		// If this was a payout flow, start the payout response timeout monitor
		// now that the dealer has accepted and we're waiting for the partner.
		if payoutTradeActive && !payoutResponseTimeoutActive {
			a.AddLogMsg("[PAYOUT] starting payout response timeout monitor after dealer accept")
			a.startPayoutResponseTimeoutMonitor(payoutTargetName, payoutTargetID, payoutTargetName)
		}
	}(flowID)
}

func scheduleAutoTradeConfirm(a *App, payload string) {
	if tradeAutoConfirmed || tradeAutoConfirmPending {
		return
	}
	if !payoutTradeActive {
		if s := a.getTradeCoverageShortages(); s != nil && len(s) > 0 {
			a.AddLogMsg("[TRADE_CONFIRM_ACCEPT] skipped auto-confirm due to insufficient payout stock")
			return
		}
		// If snapshot not ready (s == nil) allow scheduling and perform a
		// short wait inside the confirm loop before sending each confirm.
	}

	tradeAutoConfirmPending = true
	flowID := tradeAutoFlowID
	a.AddLogMsg(fmt.Sprintf("[TRADE_CONFIRM #111] detected (%q), auto-confirm starts in 4s with up to 10 attempts", payload))

	go func(flow int) {
		defer func() {
			if r := recover(); r != nil {
				tradeAutoConfirmPending = false
				a.AddLogMsg(fmt.Sprintf("[TRADE_CONFIRM_ACCEPT] recovered from panic: %v", r))
			}
		}()

		for attempt := 1; attempt <= 10; attempt++ {
			time.Sleep(4 * time.Second)

			if flow != tradeAutoFlowID || tradeAutoConfirmed {
				tradeAutoConfirmPending = false
				return
			}

			if !payoutTradeActive {
				// Wait briefly for a valid hand snapshot (up to 1s). If still
				// unavailable or shortages exist, cancel auto-confirm.
				deadline := time.Now().Add(1 * time.Second)
				for {
					if flow != tradeAutoFlowID || tradeAutoConfirmed {
						tradeAutoConfirmPending = false
						return
					}
					s := a.getTradeCoverageShortages()
					if s == nil {
						if time.Now().After(deadline) {
							tradeAutoConfirmPending = false
							a.AddLogMsg("[TRADE_CONFIRM_ACCEPT] canceled auto-confirm: hand snapshot unavailable")
							return
						}
						time.Sleep(100 * time.Millisecond)
						continue
					}
					if len(s) > 0 {
						tradeAutoConfirmPending = false
						a.AddLogMsg("[TRADE_CONFIRM_ACCEPT] canceled auto-confirm due to insufficient payout stock")
						return
					}
					break
				}
			}

			ext.Send(g.Out.Id("TRADE_CONFIRM_ACCEPT"))
			a.AddLogMsg(fmt.Sprintf("[TRADE_CONFIRM_ACCEPT] sent outgoing[402] attempt %d/10", attempt))
		}

		tradeAutoConfirmPending = false
		if flow == tradeAutoFlowID && !tradeAutoConfirmed {
			handleTradeConfirmTimeout(a)
		}
	}(flowID)
}

func handleTradeConfirmTimeout(a *App) {
	partnerName := strings.TrimSpace(lastTradePartnerName)
	if partnerName == "" {
		partnerName = "Unknown"
	}

	a.AddLogMsg("[TRADE_CONFIRM_ACCEPT] max attempts reached; sending trade close")
	a.markCurrentGameHistoryIssue("Trade confirm timed out and trade was force closed", true)
	ext.Send(out.TRADE_CLOSE)

	closeMsg := fmt.Sprintf("Trade Closed: \"%s\"", partnerName)
	if shouldAnnounceDealerOpen() {
		tradeCloseAnnounced = true
		sendMessageWithDelay(closeMsg)
		sendMessageWithDelay(a.dealerOpenMessage())
		dealerTradeWindowOpen = true
	} else {
		dealerTradeWindowOpen = false
		a.AddLogMsg("[TRADE_CONFIRM_ACCEPT] user muted; skipped timeout close announcement")
	}

	awaitingTradeOpen = true
	tradeAutoConfirmed = true
}

func extractTradePartnerID(payload string) (int, bool) {
	matches := tradeUserPattern.FindStringSubmatch(payload)
	if len(matches) < 2 {
		return 0, false
	}

	partnerID, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, false
	}

	return partnerID, true
}

func decodeTradeOpenPacket(pkt *g.Packet) []string {
	copyPacket := func() *g.Packet {
		return &g.Packet{
			Client: pkt.Client,
			Header: pkt.Header,
			Data:   append([]byte(nil), pkt.Data...),
			Pos:    0,
		}
	}

	lines := []string{
		fmt.Sprintf("raw=%q", string(pkt.Data)),
		fmt.Sprintf("hex=% X", pkt.Data),
		fmt.Sprintf("len=%d", len(pkt.Data)),
	}

	if v, pos, ok := tryReadInt(copyPacket()); ok {
		lines = append(lines, fmt.Sprintf("layout int -> id=%d (pos=%d)", v, pos))
	}

	if id, s, pos, ok := tryReadIntString(copyPacket()); ok {
		lines = append(lines, fmt.Sprintf("layout int,string -> id=%d text=%q (pos=%d)", id, s, pos))
	}

	if s, pos, ok := tryReadString(copyPacket()); ok {
		lines = append(lines, fmt.Sprintf("layout string -> text=%q (pos=%d)", s, pos))
	}

	if a, b, pos, ok := tryReadIntInt(copyPacket()); ok {
		lines = append(lines, fmt.Sprintf("layout int,int -> a=%d b=%d (pos=%d)", a, b, pos))
	}

	lines = append(lines, scanTradeOpenFields(pkt.Data)...)

	return lines
}

func scanTradeOpenFields(data []byte) []string {
	lines := []string{}

	for offset := 0; offset < len(data); offset++ {
		remaining := len(data) - offset

		if remaining >= 2 {
			chunk := data[offset : offset+2]
			lines = append(lines, fmt.Sprintf("scan b64_2 @%d -> %q = %d", offset, string(chunk), gencoding.B64Decode(chunk)))
		}

		if remaining >= 3 {
			chunk := data[offset : offset+3]
			lines = append(lines, fmt.Sprintf("scan b64_3 @%d -> %q = %d", offset, string(chunk), gencoding.B64Decode(chunk)))
		}

		vl64Len := gencoding.VL64DecodeLen(data[offset])
		if vl64Len > 0 && vl64Len <= 6 && remaining >= vl64Len {
			chunk := data[offset : offset+vl64Len]
			lines = append(lines, fmt.Sprintf("scan vl64 @%d len=%d -> %q = %d", offset, vl64Len, string(chunk), gencoding.VL64Decode(chunk)))
		}
	}

	return lines
}

func users28UserEqual(a ParsedUsers28User, b ParsedUsers28User) bool {
	// Compare username strictly, but treat missing/zero numeric IDs as unknown
	if strings.TrimSpace(a.Username) != strings.TrimSpace(b.Username) {
		return false
	}

	// If both sides have a non-zero ChatID and they differ, it's a real change.
	if a.ChatID > 0 && b.ChatID > 0 && a.ChatID != b.ChatID {
		return false
	}

	// If both sides have a non-zero TradeID and they differ, it's a real change.
	if a.TradeID > 0 && b.TradeID > 0 && a.TradeID != b.TradeID {
		return false
	}

	// If both have non-empty EntityID and they differ, treat as change.
	if strings.TrimSpace(a.EntityID) != "" && strings.TrimSpace(b.EntityID) != "" &&
		strings.TrimSpace(a.EntityID) != strings.TrimSpace(b.EntityID) {
		return false
	}

	if strings.TrimSpace(a.Figure) != strings.TrimSpace(b.Figure) {
		return false
	}

	if strings.TrimSpace(a.Sex) != strings.TrimSpace(b.Sex) {
		return false
	}

	// If both tokens present and differ, it's a change.
	at := strings.TrimSpace(a.TokenHex)
	bt := strings.TrimSpace(b.TokenHex)
	if at != "" && bt != "" && at != bt {
		return false
	}

	// Ignore raw VL64/text fragments such as chat_id_raw/trade_id_raw and motto differences.
	return true
}

func parseUsers28Int(raw string) int {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	return v
}

func handleUsers28Packet(a *App, e *g.Intercept) {
	if e.Packet.Header.Dir != g.In || e.Packet.Header.Value != 28 {
		return
	}

	a.AddLogMsg(fmt.Sprintf("[ROOM_USERS] received packet 28 len=%d", len(e.Packet.Data)))

	parsed, err := runUsers28PythonParser(a, e.Packet.Data)
	if err != nil {
		a.AddLogMsg(fmt.Sprintf("[USERS28_PY] parser failed: %v", err))
		return
	}
	if parsed == nil || len(parsed.Users) == 0 {
		a.AddLogMsg(fmt.Sprintf("[USERS28_PY] parser returned no users for len=%d", len(e.Packet.Data)))
		return
	}

	joined := make([]string, 0)
	changed := make([]string, 0)

	users28Mu.Lock()
	for _, u := range parsed.Users {
		username := strings.TrimSpace(u.Username)
		if username == "" {
			continue
		}

		// Compute a canonical key by stripping known token prefixes where possible.
		canonical := strings.ToLower(strings.TrimSpace(normalizeUsers28Name(u.Username, u.TokenHex)))
		if canonical == "" {
			canonical = strings.ToLower(username)
		}

		oldUser, exists := users28Canonical[canonical]
		if !exists {
			// New canonical entry
			users28Canonical[canonical] = u
			if token := strings.TrimSpace(u.TokenHex); token != "" {
				users28ByToken[token] = u
			}
			if u.ChatID > 0 {
				users28ByIndex[u.ChatID] = u
			}
			if u.TradeID > 0 {
				users28ByTradeID[u.TradeID] = u
			}
			joined = append(joined, u.Username)
			a.AddLogMsg(fmt.Sprintf("[ROOM_USERS_DEBUG] added user=%s canonical=%s raw=%s chat_id=%d chat_raw=%s trade_id=%d trade_raw=%s",
				u.Username, canonical, u.RawNameBlock, u.ChatID, u.ChatIDRaw, u.TradeID, u.TradeIDRaw))
			continue
		}

		if users28UserEqual(oldUser, u) {
			// No meaningful change; skip rewriting maps to avoid noisy "replaced" logs.
			a.AddLogMsg(fmt.Sprintf("[ROOM_USERS_DEBUG] unchanged user=%s canonical=%s raw=%s",
				u.Username, canonical, u.RawNameBlock))
			continue
		}

		// Significant change: update canonical record and associated indexes.
		users28Canonical[canonical] = u
		if token := strings.TrimSpace(u.TokenHex); token != "" {
			users28ByToken[token] = u
		}
		if u.ChatID > 0 {
			users28ByIndex[u.ChatID] = u
		}
		if u.TradeID > 0 {
			users28ByTradeID[u.TradeID] = u
		}
		changed = append(changed, fmt.Sprintf("%s(chat:%d->%d trade:%s->%s)",
			u.Username, oldUser.ChatID, u.ChatID, oldUser.TradeIDRaw, u.TradeIDRaw))
		a.AddLogMsg(fmt.Sprintf("[ROOM_USERS_DEBUG] updated user=%s canonical=%s raw=%s chat_id=%d chat_raw=%s trade_id=%d trade_raw=%s",
			u.Username, canonical, u.RawNameBlock, u.ChatID, u.ChatIDRaw, u.TradeID, u.TradeIDRaw))
	}
	users28Mu.Unlock()

	sort.Strings(joined)
	sort.Strings(changed)
	if len(joined) > 0 {
		a.AddLogMsg(fmt.Sprintf("[ROOM_USERS] joined: %s", strings.Join(joined, ", ")))
	}
	if len(changed) > 0 {
		a.AddLogMsg(fmt.Sprintf("[ROOM_USERS] replaced: %s", strings.Join(changed, ", ")))
	}

	a.emitRoomIdentityUpdate()
}

func clearRoomUserCaches(a *App) {
	roomMu.Lock()
	clear(roomEntities)
	roomMu.Unlock()

	users28Mu.Lock()
	clear(users28Canonical)
	clear(users28ByToken)
	clear(users28ByIndex)
	clear(users28ByTradeID)
	users28Mu.Unlock()

	a.emitRoomIdentityUpdate()
}

func handleRoomResetPacket(a *App, e *g.Intercept) {
	if e.Packet.Header.Dir != g.In || e.Packet.Header.Value != 54 {
		return
	}
	clearRoomUserCaches(a)
	a.AddLogMsg("[ROOM_USERS] cleared cached room users due to FLATINFO[54]")
}

func (a *App) resetDealerSessionState(reason string) {
	a.markCurrentGameHistoryIssue(fmt.Sprintf("Dealer session reset before round fully resolved (%s)", reason), true)

	// Stop any active game-choice timeout when resetting the dealer session.
	stopGameChoiceTimeoutMonitor()

	awaitingTradeOpen = false
	dealerAcceptingTrades = false
	dealerTradeWindowOpen = false
	awaitingGameChoice = false
	gameChoiceUnreadableWarned = false
	awaitingGameChoicePartnerID = 0
	awaitingGameChoicePartnerName = ""
	lastTradePartnerID = 0
	lastTradePartnerName = ""
	lastTradePartnerToken = ""
	tradeStarterTradeID = 0
	tradeStarterChatID = 0
	tradeStarterName = ""
	tradeStarterToken = ""
	tradeStarterLocked = false
	stableTradePartnerID = 0
	stableTradePartnerName = ""
	stableTradePartnerToken = ""
	gameBetItems = nil
	lastAddItemWasOurs = false
	lastTradeCoverageNotice = ""
	lastTradeBlockNotice = ""

	stopTradeWindowTimeoutMonitor()
	stopUnderfundedTradeMonitor()
	stopDealerOpenHeartbeat()
	resetTradeAutoFlow()
	resetPokerSequence()
	resetBlackjackSequence()
	stopPayout()
	a.ClearTradeItems()

	handItemsMu.Lock()
	currentHandItems = nil
	currentHandItemIDs = map[string][]int{}
	handItemsMu.Unlock()
	a.emitHandItemsUpdate()
	a.emitActiveGameBetItemsUpdate()

	clearRoomUserCaches(a)
	a.AddLogMsg(fmt.Sprintf("[DEALER_RESET] session reset (%s)", reason))
}

func lookupUsers28Token(token string) (string, bool) {
	users28Mu.Lock()
	defer users28Mu.Unlock()
	u, ok := users28ByToken[strings.TrimSpace(token)]
	if !ok || strings.TrimSpace(u.Username) == "" {
		return "", false
	}
	return u.Username, true
}

func lookupUsers28Index(index int) (string, bool) {
	users28Mu.Lock()
	defer users28Mu.Unlock()
	u, ok := users28ByIndex[index]
	if !ok || strings.TrimSpace(u.Username) == "" {
		return "", false
	}
	return u.Username, true
}

func lookupUsers28TradeID(id int) (string, bool) {
	users28Mu.Lock()
	defer users28Mu.Unlock()
	u, ok := users28ByTradeID[id]
	if !ok || strings.TrimSpace(u.Username) == "" {
		return "", false
	}
	return u.Username, true
}

func lookupUsers28TradeIDByName(name string) (int, bool) {
	needle := strings.ToLower(strings.TrimSpace(name))
	if needle == "" {
		return 0, false
	}
	users28Mu.Lock()
	defer users28Mu.Unlock()
	for id, user := range users28ByTradeID {
		if strings.ToLower(strings.TrimSpace(user.Username)) == needle && id > 0 {
			return id, true
		}
	}
	return 0, false
}

func lookupUsers28UserByTradeID(id int) (ParsedUsers28User, bool) {
	users28Mu.Lock()
	defer users28Mu.Unlock()
	u, ok := users28ByTradeID[id]
	if !ok {
		return ParsedUsers28User{}, false
	}
	return u, true
}

func lookupUsers28UserByToken(token string) (ParsedUsers28User, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return ParsedUsers28User{}, false
	}
	users28Mu.Lock()
	defer users28Mu.Unlock()
	u, ok := users28ByToken[token]
	if !ok {
		return ParsedUsers28User{}, false
	}
	return u, true
}

func waitForUsers28TradeIDName(id int, timeout time.Duration) (string, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if name, ok := lookupUsers28TradeID(id); ok {
			return name, true
		}
		time.Sleep(75 * time.Millisecond)
	}
	return "", false
}

func waitForUsers28TradeIDByName(name string, timeout time.Duration) (int, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if id, ok := lookupUsers28TradeIDByName(name); ok {
			return id, true
		}
		time.Sleep(75 * time.Millisecond)
	}
	return 0, false
}

func waitForUsers28IndexName(index int, timeout time.Duration) (string, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if name, ok := lookupUsers28Index(index); ok {
			return name, true
		}
		time.Sleep(75 * time.Millisecond)
	}
	return "", false
}

func waitForUsers28TokenName(token string, timeout time.Duration) (string, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if name, ok := lookupUsers28Token(token); ok {
			return name, true
		}
		time.Sleep(75 * time.Millisecond)
	}
	return "", false
}

func rememberRecentTradePartner(token string, name string) {
	token = strings.TrimSpace(token)
	name = strings.TrimSpace(name)
	if token == "" || name == "" {
		return
	}
	recentTradePartnerMu.Lock()
	recentTradePartnerByToken[token] = name
	recentTradePartnerSeenAt[token] = time.Now()
	recentTradePartnerMu.Unlock()
}

func lookupRecentTradePartner(token string, maxAge time.Duration) (string, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", false
	}
	recentTradePartnerMu.Lock()
	defer recentTradePartnerMu.Unlock()
	name, ok := recentTradePartnerByToken[token]
	if !ok {
		return "", false
	}
	seenAt := recentTradePartnerSeenAt[token]
	if maxAge > 0 && !seenAt.IsZero() && time.Since(seenAt) > maxAge {
		delete(recentTradePartnerByToken, token)
		delete(recentTradePartnerSeenAt, token)
		return "", false
	}
	if strings.TrimSpace(name) == "" {
		return "", false
	}
	return name, true
}

func (a *App) recoverUnknownTradePartner(packetData []byte, timeout time.Duration) (string, int, bool) {
	return "", 0, false
}

func isPlausibleTradeRoomIndex(index int) bool {
	return index > 0 && index <= 512
}

func isPlausibleUsers28RoomIndex(index int) bool {
	return index > 0 && index <= 512
}

func lookupUsers28NameIndex(name string) (int, bool) {
	needle := strings.ToLower(strings.TrimSpace(name))
	if needle == "" {
		return 0, false
	}

	users28Mu.Lock()
	defer users28Mu.Unlock()
	for idx, user := range users28ByIndex {
		if strings.ToLower(strings.TrimSpace(user.Username)) == needle && isPlausibleUsers28RoomIndex(idx) {
			return idx, true
		}
	}
	return 0, false
}

func lookupUsers28RoomIndexByName(name string) (int, bool) {
	return lookupUsers28NameIndex(name)
}

func waitForUsers28RoomIndexByName(name string, timeout time.Duration) (int, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if idx, ok := lookupUsers28RoomIndexByName(name); ok {
			return idx, true
		}
		time.Sleep(75 * time.Millisecond)
	}
	return 0, false
}

func waitForUsers28NameIndex(name string, timeout time.Duration) (int, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if idx, ok := lookupUsers28NameIndex(name); ok {
			return idx, true
		}
		time.Sleep(75 * time.Millisecond)
	}
	return 0, false
}

func shortTokenFromIndex(index int) string {
	if index <= 0 {
		return ""
	}
	return encodeB64(index, 2)
}

func isLikelyChatToken(s string) bool {
	if len(s) != 2 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 32 || s[i] > 126 {
			return false
		}
	}
	return true
}

func normalizeUsername(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}

	// Fix parser artefacts like "adfAmaver1995" or "fAmaver1995" without
	// mangling normal lowercase usernames. Only strip a short leading run of
	// lowercase junk when it is immediately followed by an uppercase-led name.
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
			if raw[i+1] < 'a' || raw[i+1] > 'z' {
				continue
			}
			return raw[i:]
		}
	}

	return raw
}

func decodeShortChatToken(token string) (idx int, ok bool) {
	if !isLikelyChatToken(token) {
		return 0, false
	}
	defer func() {
		if recover() != nil {
			idx = 0
			ok = false
		}
	}()
	idx = gencoding.B64Decode([]byte(token))
	if idx <= 0 {
		return 0, false
	}
	return idx, true
}

func chatIndexFromShortToken(token string) (idx int, ok bool) {
	return decodeShortChatToken(token)
}

func lookupRoomIdentityByChatIndex(index int) (string, bool) {
	return lookupUsers28Index(index)
}

func shortTokenCandidates(token string) []string {
	if len(token) < 2 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	for i := 0; i+2 <= len(token); i++ {
		cand := token[i : i+2]
		if !isLikelyChatToken(cand) {
			continue
		}
		if _, ok := seen[cand]; ok {
			continue
		}
		seen[cand] = struct{}{}
		out = append(out, cand)
	}
	return out
}

// runUsers28PythonParser writes the exact raw packet bytes to a temporary file,
// invokes the Python parser with --input <tmp> --json, and unmarshals the JSON.
func runUsers28PythonParser(a *App, packetData []byte) (*ParsedUsers28Result, error) {
	scriptPath := filepath.Join("scripts", "parse_users28.py")
	py := ""

	if a != nil {
		a.initUsers28ParserCommand()
		if strings.TrimSpace(a.users28ParserScript) != "" {
			scriptPath = a.users28ParserScript
		}
		py = strings.TrimSpace(a.users28PythonExec)
	}

	if _, err := os.Stat(scriptPath); err != nil {
		return nil, fmt.Errorf("users28 parser unavailable: %w", err)
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

	if py == "" {
		if p, err := exec.LookPath("python3"); err == nil {
			py = p
		} else if p, err := exec.LookPath("python"); err == nil {
			py = p
		} else {
			return nil, fmt.Errorf("python not found in PATH")
		}
		if a != nil {
			a.users28PythonExec = py
		}
	}

	cmd := exec.Command(py, scriptPath, "--input", tmpPath, "--json")
	cmd.Dir = "."
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if a != nil {
			a.AddLogMsg(fmt.Sprintf("[USERS28_PY] python parser failed: %v stderr=%s", err, strings.TrimSpace(stderr.String())))
		}
		return nil, fmt.Errorf("python parser failed: %w stderr=%s", err, stderr.String())
	}

	var users []ParsedUsers28User
	if err := json.Unmarshal(stdout.Bytes(), &users); err != nil {
		return nil, fmt.Errorf("failed to decode parser json: %w output=%s", err, stdout.String())
	}

	return &ParsedUsers28Result{Users: users}, nil
}

func runParseUsersScript(a *App, data []byte) (map[string]map[string]interface{}, error) {
	parsed, err := runUsers28PythonParser(a, data)
	if err != nil {
		return nil, err
	}

	res := map[string]map[string]interface{}{}
	for _, u := range parsed.Users {
		key := strings.TrimSpace(u.TokenHex)
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(u.Username))
		}
		res[key] = map[string]interface{}{
			"username":  u.Username,
			"trade_id":  u.TradeID,
			"chat_id":   u.ChatID,
			"entity_id": u.EntityID,
			"figure":    u.Figure,
			"sex":       u.Sex,
			"motto":     u.Motto,
			"token_hex": u.TokenHex,
		}
	}
	return res, nil
}

func lookupRoomEntityIndexByName(name string) (int, bool) {
	needle := strings.ToLower(normalizeUsername(name))
	if needle == "" {
		return 0, false
	}

	roomMu.Lock()
	defer roomMu.Unlock()
	for _, entity := range roomEntities {
		entityName := strings.TrimSpace(entity.Name)
		if _, clean, ok := splitTokenAndName(entityName); ok {
			entityName = clean
		}
		if strings.ToLower(entityName) == needle {
			return entity.Index, true
		}
	}

	return 0, false
}

func waitForRoomEntityIndexByName(name string, timeout time.Duration) (int, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if idx, ok := lookupRoomEntityIndexByName(name); ok {
			return idx, true
		}
		time.Sleep(75 * time.Millisecond)
	}
	return 0, false
}

func lookupRoomEntityNameByIndex(index int) (string, bool) {
	if index <= 0 {
		return "", false
	}

	roomMu.Lock()
	defer roomMu.Unlock()
	entity, ok := roomEntities[index]
	if !ok {
		return "", false
	}

	name := normalizeUsername(strings.TrimSpace(entity.Name))
	if _, clean, hasToken := splitTokenAndName(name); hasToken {
		name = normalizeUsername(strings.TrimSpace(clean))
	}
	if name == "" {
		return "", false
	}

	return name, true
}

// parseTradeItemsPacket extracts trade items from TRADE_ITEMS packet (header 108)
// Items are separated by \x02 bytes and may contain item names and quantities.
func (a *App) parseTradeItemsPacket(data []byte) []TradeItem {
	counts := map[string]int{}
	rawByName := map[string]string{}

	// Split on \x02 separator byte.
	fields := bytes.Split(data, []byte{0x02})
	for _, field := range fields {
		if len(field) == 0 {
			continue
		}

		fieldStr := strings.TrimSpace(string(field))
		// Log the raw field to aid debugging of parsing failures.
		a.AddLogMsg(fmt.Sprintf("[TRADE_PARSE_DEBUG] field=%q", fieldStr))

		itemName, qty, ok := a.extractTradeItemAndQuantity(fieldStr)
		if !ok {
			a.AddLogMsg(fmt.Sprintf("[TRADE_PARSE_DEBUG] skipped field=%q", fieldStr))
			// Preserve unknown raw field in logs for later inspection.
			a.AddLogMsg(fmt.Sprintf("[TRADE_UNKNOWN_FIELD] raw=%q", fieldStr))
			continue
		}
		if qty <= 0 {
			qty = 1
		}

		// Successful parse: record and log.
		a.AddLogMsg(fmt.Sprintf("[TRADE_PARSE_DEBUG] parsed field=%q -> name=%q qty=%d", fieldStr, itemName, qty))

		counts[itemName] += qty
		if _, exists := rawByName[itemName]; !exists {
			rawByName[itemName] = fieldStr
		}
	}

	if len(counts) == 0 {
		return []TradeItem{}
	}

	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)

	items := make([]TradeItem, 0, len(names))
	for _, name := range names {
		items = append(items, TradeItem{
			Name:     name,
			Quantity: counts[name],
			RawData:  rawByName[name],
		})
	}

	return items
}

func (a *App) extractTradeItemAndQuantity(field string) (string, int, bool) {
	// Try legacy format first (e.g. "itkoHP|club_sofa")
	a.AddLogMsg(fmt.Sprintf("[TRADE_PARSE_DEBUG] try legacy branch field=%q", field))
	if strings.Contains(field, "|") {
		parts := strings.Split(field, "|")
		if len(parts) >= 2 {
			cand := strings.TrimSpace(parts[len(parts)-1])
			// Reject obvious non-item header/token candidates early: require
			// either an underscore (typical furni class), a quantity suffix,
			// or a strict furni regex match before attempting normalization.
			if !(strings.Contains(cand, "_") || strings.Contains(cand, "*") || stripItemNameRe.MatchString(cand)) {
				a.AddLogMsg(fmt.Sprintf("[TRADE_PARSE_DEBUG] legacy candidate lacks item-like structure, skipping=%q", cand))
			} else {
				a.AddLogMsg(fmt.Sprintf("[TRADE_PARSE_DEBUG] legacy candidate=%q", cand))
				if name, qty, ok := a.normalizeTradeFieldClassWithQty(cand); ok {
					a.AddLogMsg(fmt.Sprintf("[TRADE_PARSE_DEBUG] legacy parsed %q -> %q", cand, name))
					return name, qty, true
				}
				a.AddLogMsg(fmt.Sprintf("[TRADE_PARSE_DEBUG] legacy failed to parse candidate=%q", cand))
			}
		}
	}

	// Try current format (e.g. "irbUAXb{chair_plasty*2")
	a.AddLogMsg(fmt.Sprintf("[TRADE_PARSE_DEBUG] try current branch field=%q", field))
	if strings.Contains(field, "{") {
		parts := strings.SplitN(field, "{", 2)
		if len(parts) == 2 {
			cand := strings.TrimSpace(parts[1])
			// Ensure the payload inside the brace looks like a furni-class or
			// quantity suffix before trying to normalise. This avoids treating
			// token-like headers (e.g. "m{MHcizMH") as item classes.
			if !(strings.Contains(cand, "_") || strings.Contains(cand, "*") || strings.HasPrefix(strings.ToLower(cand), "cf_") || stripItemNameRe.MatchString(cand)) {
				a.AddLogMsg(fmt.Sprintf("[TRADE_PARSE_DEBUG] current candidate looks invalid, skipping: %q", cand))
			} else {
				a.AddLogMsg(fmt.Sprintf("[TRADE_PARSE_DEBUG] current candidate=%q", cand))
				if name, qty, ok := a.normalizeTradeFieldClassWithQty(cand); ok {
					a.AddLogMsg(fmt.Sprintf("[TRADE_PARSE_DEBUG] current parsed %q -> %q", cand, name))
					return name, qty, true
				}
				a.AddLogMsg(fmt.Sprintf("[TRADE_PARSE_DEBUG] current failed to parse candidate=%q", cand))
			}
		}
	}

	// Fallback: try strict strip regex first, then try looser token candidates.
	a.AddLogMsg(fmt.Sprintf("[TRADE_PARSE_DEBUG] try fallback for field=%q", field))

	if match := stripItemNameRe.FindString(field); match != "" {
		a.AddLogMsg(fmt.Sprintf("[TRADE_PARSE_DEBUG] fallback strict matched=%q inside=%q", match, field))
		// Try to accept a strict pattern match only if it verifies against the
		// known sources (catalog or frozen/live hand). This prevents accepting
		// token-like headers that resemble class names but are not present in
		// the snapshot we send to the API (the authoritative view).
		if normalized, ok := normalizeClassKeyWithVariant(match); ok {
			// Prefer the verified path when available (catalog/hand/snapshot check).
			if name, qty, ok2 := a.normalizeTradeFieldClassWithQty(match); ok2 {
				a.AddLogMsg(fmt.Sprintf("[TRADE_ITEMS_PARSE_FALLBACK] matched %q inside %q (verified)", match, field))
				return name, qty, true
			}
			// Verification failed: do not accept unverified normalized classes.
			a.AddLogMsg(fmt.Sprintf("[TRADE_ITEMS_PARSE_FALLBACK] strict fallback found %q inside %q but verification failed; skipping", normalized, field))
		}
		a.AddLogMsg(fmt.Sprintf("[TRADE_PARSE_DEBUG] fallback strict match %q rejected by normalisation/raw-check", match))
	}

	// Looser candidate scanning: find word-like tokens and try each one.
	candidateRe := regexp.MustCompile(`[A-Za-z][A-Za-z0-9_]*(?:\*\d+)?`)
	matches := candidateRe.FindAllString(field, -1)
	if len(matches) > 0 {
		a.AddLogMsg(fmt.Sprintf("[TRADE_PARSE_DEBUG] fallback candidates=%q", matches))
		for _, cand := range matches {
			a.AddLogMsg(fmt.Sprintf("[TRADE_PARSE_DEBUG] fallback trying candidate=%q", cand))
			if name, qty, ok := a.normalizeTradeFieldClassWithQty(cand); ok {
				a.AddLogMsg(fmt.Sprintf("[TRADE_PARSE_DEBUG] fallback accepted candidate=%q -> %q", cand, name))
				return name, qty, true
			}
			a.AddLogMsg(fmt.Sprintf("[TRADE_PARSE_DEBUG] fallback candidate rejected=%q", cand))
		}
	} else {
		a.AddLogMsg(fmt.Sprintf("[TRADE_PARSE_DEBUG] fallback found no candidates in %q", field))
	}

	// Relaxed fallback: some server payloads include recognizable single-word
	// class names that don't match the strict furni regex (no underscore)
	// but are still meaningful (examples: "giftflowers", "hologram").
	// Prefer the longest matching hand-item name found as a substring of the
	// raw field payload to avoid choosing shorter overlapping names (e.g.
	// prefer "redhologram" over "hologram").
	low := strings.ToLower(field)
	handItemsMu.Lock()
	best := ""
	// Prefer the frozen trade snapshot when available so relaxed parsing
	// only accepts items that were actually present in the snapshot used
	// for coverage checks. Fall back to the live hand when no snapshot.
	itemsToCheck := currentHandItems
	if tradeHandSnapshotReady && len(tradeHandSnapshot) > 0 {
		itemsToCheck = tradeHandSnapshot
	}
	for _, it := range itemsToCheck {
		name := strings.ToLower(it.Name)
		if name == "" || len(name) < 3 {
			continue
		}
		if strings.Contains(low, name) {
			if len(name) > len(best) {
				best = name
			}
		}
	}
	if best != "" {
		if normalized, ok := normalizeClassKeyWithVariant(best); ok {
			a.AddLogMsg(fmt.Sprintf("[TRADE_ITEMS_PARSE_RELAXED] accepted %q inside %q (best match)", normalized, field))
			handItemsMu.Unlock()
			return normalized, 1, true
		}
	}
	handItemsMu.Unlock()

	return "", 0, false
}

func (a *App) normalizeTradeFieldClassWithQty(raw string) (string, int, bool) {
	normalized, ok := normalizeClassKeyWithVariant(raw)
	if !ok {
		return "", 0, false
	}

	if !isKnownTradeClassName(a, normalized) {
		return "", 0, false
	}

	// Trade quantity is represented by repeated item entries, not by the *n variant suffix.
	return normalized, 1, true
}

func isKnownTradeClassName(a *App, name string) bool {
	// Normalize input
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return false
	}

	// Prefer explicit catalog membership to avoid false-positives from
	// protocol/header tokens that happen to match the furni-name pattern.
	catalogSet := a.GetCatalogNameSet()
	if _, ok := catalogSet[name]; ok {
		return true
	}

	// Also accept items observed in the dealer's scanned hand (single-word
	// items or uncatalogued classes). Prefer the frozen trade snapshot when
	// available so parsing uses the same authoritative view we send to the
	// live-dealer API. Fall back to the live hand when no snapshot.
	handItemsMu.Lock()
	itemsToCheck := currentHandItems
	if tradeHandSnapshotReady && len(tradeHandSnapshot) > 0 {
		itemsToCheck = tradeHandSnapshot
	}
	for _, item := range itemsToCheck {
		if strings.ToLower(strings.TrimSpace(item.Name)) == name {
			handItemsMu.Unlock()
			return true
		}
	}
	handItemsMu.Unlock()

	return false
}

func normalizeTradeItemName(raw string) (string, bool) {
	name := strings.TrimSpace(strings.ToLower(raw))
	if name == "" || isCoordinatePattern(name) {
		return "", false
	}

	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
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

// isCoordinatePattern checks if a string looks like coordinates (e.g., "0,0,0")
func isCoordinatePattern(s string) bool {
	if !strings.Contains(s, ",") {
		return false
	}

	parts := strings.Split(s, ",")
	for _, part := range parts {
		if _, err := strconv.Atoi(strings.TrimSpace(part)); err != nil {
			return false
		}
	}

	return len(parts) >= 2
}

func (a *App) OpenLastTrade() {
	if strings.TrimSpace(lastTradePartnerName) != "" && lastTradePartnerName != "Unknown" {
		if idx, ok := lookupRoomEntityIndexByName(lastTradePartnerName); ok && idx > 0 && idx != lastTradePartnerID {
			a.AddLogMsg(fmt.Sprintf("Open Last Trade refreshed partner index from ROOM_USERS: %s (%d -> %d)", lastTradePartnerName, lastTradePartnerID, idx))
			lastTradePartnerID = idx
		} else if idx, ok := lookupUsers28TradeIDByName(lastTradePartnerName); ok && idx > 0 && idx != lastTradePartnerID {
			a.AddLogMsg(fmt.Sprintf("Open Last Trade refreshed partner trade id from USERS28: %s (%d -> %d)", lastTradePartnerName, lastTradePartnerID, idx))
			lastTradePartnerID = idx
		}
	}

	if lastTradePartnerID <= 0 {
		a.AddLogMsg("Open Last Trade failed: no last trader cached yet")
		go requestRoomUsers(a)
		return
	}

	ext.Send(out.TRADE_OPEN, lastTradePartnerID)
	rememberOutgoingTradeOpenTarget(lastTradePartnerID)
	outPreview := string(ext.NewPacket(out.TRADE_OPEN, lastTradePartnerID).Data)
	a.AddLogMsg(fmt.Sprintf("Open Last Trade sent -> %s (%d), Outgoing[71] %q", lastTradePartnerName, lastTradePartnerID, outPreview))
}

func (a *App) GetLastTradePartnerName() string {
	if lastTradePartnerID <= 0 {
		return "None"
	}
	if strings.TrimSpace(lastTradePartnerName) == "" {
		return "Unknown"
	}
	return lastTradePartnerName
}

// GetCurrentTradeItems returns the list of items currently in the trade
func (a *App) GetCurrentTradeItems() []TradeItem {
	tradeItemsMu.Lock()
	defer tradeItemsMu.Unlock()

	// Return a copy to prevent external modifications
	itemsCopy := make([]TradeItem, len(currentTradeItems))
	copy(itemsCopy, currentTradeItems)
	return itemsCopy
}

// GetTradeItemsJSON returns the current trade items as a JSON string for the frontend
func (a *App) GetTradeItemsJSON() string {
	items := a.GetCurrentTradeItems()
	jsonData, err := json.Marshal(items)
	if err != nil {
		a.AddLogMsg(fmt.Sprintf("[ERROR] failed to marshal trade items: %v", err))
		return "[]"
	}
	return string(jsonData)
}

// ClearTradeItems removes all current trade items
func (a *App) ClearTradeItems() {
	tradeItemsMu.Lock()
	currentTradeItems = []TradeItem{}
	currentOwnTradeItems = []TradeItem{}
	lastAllTradeItems = nil
	partnerAcceptedSnapshot = nil
	tradeItemsMu.Unlock()

	handItemsMu.Lock()
	// Keep the last frozen hand snapshot alive. The trade-open guard
	// requires a dealer snapshot to be present while reopening dealer.
	// Only mark the live trade as closed; do NOT clear
	// tradeHandSnapshot or tradeHandSnapshotReady here to avoid a race
	// where a quick TRADE_OPEN arrives before a fresh snapshot is built.
	tradeOpen = false
	handItemsMu.Unlock()

	stopUnderfundedTradeMonitor()
	stopShortageMonitor()
	// Ensure any trade-limit state is cleared when clearing trade items.
	stopTradeLimitMonitor()
	lastTradeLimitNotice = ""
	tradeLimitWasActive = false
	partnerTradeAccepted = false
	lastTradeCoverageNotice = ""
	lastTradeBlockNotice = ""
	a.AddLogMsg("[TRADE_ITEMS] cleared partner and own trade items")
	a.emitTradeItemsUpdate("both")
}

// emitTradeItemsUpdate pushes the current trade items to the frontend via an event
func (a *App) emitTradeItemsUpdate(side string) {
	tradeItemsMu.Lock()
	partnerItems := make([]TradeItem, len(currentTradeItems))
	copy(partnerItems, currentTradeItems)
	ownItems := make([]TradeItem, len(currentOwnTradeItems))
	copy(ownItems, currentOwnTradeItems)
	tradeItemsMu.Unlock()

	if side == "partner" || side == "both" {
		jsonData, err := json.Marshal(partnerItems)
		if err == nil {
			runtime.EventsEmit(a.ctx, "tradeItemsUpdate", string(jsonData))
		}
	}

	if side == "yours" || side == "both" {
		jsonData, err := json.Marshal(ownItems)
		if err == nil {
			runtime.EventsEmit(a.ctx, "ownTradeItemsUpdate", string(jsonData))
		}
	}
}

func (a *App) emitActiveGameBetItemsUpdate() {
	items := make([]TradeItem, len(gameBetItems))
	copy(items, gameBetItems)

	jsonData, err := json.Marshal(items)
	if err != nil {
		return
	}
	runtime.EventsEmit(a.ctx, "activeGameBetItemsUpdate", string(jsonData))
}

// requestPlayerStrip sends GETSTRIP[65] to refresh the player's hand inventory.
// If force is true, it will always start a brand new full scan even if a frozen
// snapshot already exists.
func (a *App) requestPlayerStrip(force bool) int {
	handItemsMu.Lock()
	snapshotReady := tradeHandSnapshotReady
	handItemsMu.Unlock()

	if !force && tradeOpen && snapshotReady {
		a.AddLogMsg("[STRIP_DEBUG] skipped hand scan because trade snapshot is already frozen")

		stripScanMu.Lock()
		sid := stripScanSessionID
		stripScanMu.Unlock()
		return sid
	}

	stripScanMu.Lock()
	stripScanActive = true
	stripScanSessionID++
	stripScanPageCount = 0
	stripScanStartedAt = time.Now()
	stripScanLastPacketAt = stripScanStartedAt
	stripScanSeenItemIDs = map[int]struct{}{}
	stripScanCounts = map[string]int{}
	stripScanItemIDs = map[string][]int{}
	sid := stripScanSessionID
	stripScanMu.Unlock()

	a.AddLogMsg(fmt.Sprintf("[STRIP_DEBUG] start scan session=%d force=%t", sid, force))

	ext.Send(out.GETSTRIP, "new")
	a.AddLogMsg("[STRIP_DEBUG] raw GETSTRIP send payload=\"new\"")
	a.AddLogMsg("[STRIP] requested player hand scan (GETSTRIP new)")

	return sid
}

func waitForStripScanCompletion(sessionID int, timeout time.Duration) bool {
	log.Printf("[STRIP_WAIT] waiting for strip scan completion session=%d timeout=%s", sessionID, timeout)
	deadline := time.Now().Add(timeout)
	inactivityLimit := stripNextDelay + 1500*time.Millisecond
	for time.Now().Before(deadline) {
		stripScanMu.Lock()
		active := stripScanActive
		current := stripScanSessionID
		lastPacketAt := stripScanLastPacketAt
		startedAt := stripScanStartedAt
		pages := stripScanPageCount
		stripScanMu.Unlock()

		if current > sessionID {
			log.Printf("[STRIP_WAIT] session=%d complete: scan advanced current=%d", sessionID, current)
			return true
		}
		if current == sessionID && !active {
			log.Printf("[STRIP_WAIT] session=%d complete: scan finished (active=false)", sessionID)
			return true
		}
		if current == sessionID && active && pages > 0 && !lastPacketAt.IsZero() && time.Since(lastPacketAt) > inactivityLimit {
			log.Printf("[STRIP_WAIT] session=%d inactive for %s after %d page(s); forcing finalize", sessionID, time.Since(lastPacketAt), pages)
			return false
		}
		if current == sessionID && active && pages == 0 && !startedAt.IsZero() && time.Since(startedAt) > timeout {
			log.Printf("[STRIP_WAIT] session=%d saw no strip pages within %s", sessionID, timeout)
			return false
		}

		time.Sleep(150 * time.Millisecond)
	}

	stripScanMu.Lock()
	active := stripScanActive
	current := stripScanSessionID
	pages := stripScanPageCount
	stripScanMu.Unlock()
	log.Printf("[STRIP_WAIT] timeout waiting for session=%d (current=%d active=%t pages=%d)", sessionID, current, active, pages)
	return false
}

func (a *App) resyncHandThenOpenDealer() {
	// Preserve dice across this full reset path to avoid losing assigned dice.
	snap := snapshotDice()
	a.resetDealerSessionState("reopen")
	if snap != nil {
		restoreDice(snap)
	}

	dealerResyncInProgress = true

	a.AddLogMsg("[TRADE_REOPEN] full reset complete; syncing room users and hand before reopening trades")
	requestRoomUsers(a)

	ok := a.forceRefreshHandSnapshot("resyncHandThenOpenDealer")

	dealerResyncInProgress = false
	if shouldRefreshRoomUsers() {
		requestRoomUsers(a)
	}

	if !ok {
		a.AddLogMsg("[TRADE_REOPEN] refusing to announce dealer open because forced hand refresh failed")
		return
	}

	awaitingTradeOpen = true
	dealerAcceptingTrades = true
	if shouldAnnounceDealerOpen() {
		dealerTradeWindowOpen = true
		openMsg := a.dealerOpenMessage()
		a.AddLogMsg(fmt.Sprintf("[TRADE_REOPEN] shouting: %q", openMsg))
		go sendMessageWithDelay(openMsg)
	} else {
		dealerTradeWindowOpen = false
		log.Printf("Dealer announcement skipped; incoming trades remain allowed.")
	}
	startDealerOpenHeartbeat(a)
}

// reopenDealerIdle performs a lightweight reopen after an ordinary failed trade
// without performing a full destructive dealer reset. It preserves assigned
// dice and restarts the dealer-open heartbeat safely.
func (a *App) reopenDealerIdle(reason string) {
	stopDealerOpenHeartbeat()
	// Ensure any pending game-choice timer is stopped when reopening dealer.
	stopGameChoiceTimeoutMonitor()

	stopTradeWindowTimeoutMonitor()
	stopShortageMonitor()
	stopUnderfundedTradeMonitor()

	// Ensure trade-limit monitoring state is cleared when reopening dealer.
	stopTradeLimitMonitor()
	tradeLimitWasActive = false
	lastTradeLimitNotice = ""
	partnerTradeAccepted = false
	partnerAcceptedSnapshot = nil

	resetTradeAutoFlow()
	a.ClearTradeItems()

	awaitingGameChoice = false
	gameChoiceUnreadableWarned = false
	awaitingGameChoicePartnerID = 0
	awaitingGameChoicePartnerName = ""
	lastTradePartnerID = 0
	lastTradePartnerName = ""
	lastTradePartnerToken = ""
	tradeStarterTradeID = 0
	tradeStarterChatID = 0
	tradeStarterName = ""
	tradeStarterToken = ""
	tradeStarterLocked = false
	stableTradePartnerID = 0
	stableTradePartnerName = ""
	stableTradePartnerToken = ""
	gameBetItems = nil
	a.emitActiveGameBetItemsUpdate()

	// Ensure we rebuild the frozen trade-hand snapshot before announcing
	// the dealer open. This matches the sync-first pattern used elsewhere
	// and prevents incoming trades from opening before inventory is ready.
	dealerResyncInProgress = true
	requestRoomUsers(a)

	ok := a.forceRefreshHandSnapshot("reopenDealerIdle")

	dealerResyncInProgress = false
	if shouldRefreshRoomUsers() {
		requestRoomUsers(a)
	}

	if !ok {
		awaitingTradeOpen = false
		dealerAcceptingTrades = false
		dealerTradeWindowOpen = false
		a.AddLogMsg(fmt.Sprintf("[TRADE_REOPEN] refusing to announce dealer open because forced hand refresh failed (%s)", reason))
		return
	}

	awaitingTradeOpen = true
	dealerAcceptingTrades = true
	if shouldAnnounceDealerOpen() {
		dealerTradeWindowOpen = true
		openMsg := a.dealerOpenMessage()
		a.AddLogMsg(fmt.Sprintf("[TRADE_REOPEN] shouting: %q (%s)", openMsg, reason))
		go sendMessageWithDelay(openMsg)
	} else {
		dealerTradeWindowOpen = false
	}

	startDealerOpenHeartbeat(a)
	a.AddLogMsg(fmt.Sprintf("[TRADE_REOPEN] reopened idle dealer (%s)", reason))
}

// snapshotDice returns a shallow copy of the current diceList for safe
// preservation across destructive resets.
func snapshotDice() []*Dice {
	mutex.Lock()
	defer mutex.Unlock()
	if len(diceList) == 0 {
		return nil
	}
	snap := make([]*Dice, 0, len(diceList))
	for _, d := range diceList {
		if d == nil {
			snap = append(snap, nil)
			continue
		}
		cp := *d
		snap = append(snap, &cp)
	}
	return snap
}

// restoreDice restores a previously captured dice snapshot into diceList.
func restoreDice(snap []*Dice) {
	if snap == nil {
		return
	}
	mutex.Lock()
	defer mutex.Unlock()
	diceList = make([]*Dice, 0, len(snap))
	for _, d := range snap {
		if d == nil {
			diceList = append(diceList, nil)
			continue
		}
		cp := *d
		diceList = append(diceList, &cp)
	}
}

// openDealerAfterRound syncs the hand and reopens dealer trades after a clean game result.
// Unlike resyncHandThenOpenDealer it does NOT mark a game-history issue.
func (a *App) openDealerAfterRound() {
	// Stop background monitors and heartbeats first so reopen runs on a
	// clean slate.
	stopDealerOpenHeartbeat()
	stopGameChoiceTimeoutMonitor()
	stopTradeWindowTimeoutMonitor()
	stopShortageMonitor()
	stopUnderfundedTradeMonitor()

	// Reset all auto-flow and per-game sequences so dealerGameActive()
	// will reliably report inactive.
	resetTradeAutoFlow()
	resetPokerSequence()
	resetBlackjackSequence()
	reset13Sequence()
	resetTriSequence()

	// Clear any incoming game-choice state.
	awaitingGameChoice = false
	gameChoiceUnreadableWarned = false
	awaitingGameChoicePartnerID = 0
	awaitingGameChoicePartnerName = ""

	// Optional: clear visible trade items (but keep frozen snapshot).
	a.ClearTradeItems()

	dealerResyncInProgress = true
	requestRoomUsers(a)

	// Clear stale partner identity from the previous round so the next trader
	// is always freshly identified when their TRADE_OPEN arrives.
	lastTradePartnerName = ""
	lastTradePartnerID = 0
	lastTradePartnerToken = ""
	tradeStarterTradeID = 0
	tradeStarterChatID = 0
	tradeStarterName = ""
	tradeStarterToken = ""
	tradeStarterLocked = false

	ok := a.forceRefreshHandSnapshot("openDealerAfterRound")

	dealerResyncInProgress = false
	if shouldRefreshRoomUsers() {
		requestRoomUsers(a)
	}

	if !ok {
		a.AddLogMsg("[DEALER_REOPEN] refusing to announce dealer open because forced hand refresh failed")
		return
	}

	awaitingTradeOpen = true
	dealerAcceptingTrades = true
	if shouldAnnounceDealerOpen() {
		dealerTradeWindowOpen = true
		openMsg := a.dealerOpenMessage()
		a.AddLogMsg(fmt.Sprintf("[DEALER_REOPEN] shouting: %q", openMsg))
		go sendMessageWithDelay(openMsg)
	} else {
		dealerTradeWindowOpen = false
		log.Printf("[DEALER_REOPEN] dealer open skipped (muted or no dice)")
	}
	startDealerOpenHeartbeat(a)
}

// handleStripPacket parses STRIPINFO_2 [140] to track items in the player's hand.
func handleStripPacket(a *App, e *g.Intercept) {
	if e.Packet.Header.Dir != g.In || e.Packet.Header.Value != 140 {
		return
	}

	rawData := append([]byte(nil), e.Packet.Data...)

	stripScanMu.Lock()
	active := stripScanActive
	if !active {
		a.AddLogMsg("[STRIP_DEBUG] ignored strip packet because no scan is active")
		stripScanMu.Unlock()
		return
	}
	scanID := stripScanSessionID
	stripScanPageCount++
	stripScanLastPacketAt = time.Now()
	currentPage := stripScanPageCount

	firstMainID, pageRecords, classQtys, classItemIDs := parseStripInfoPageRaw(rawData)

	pageRepeated := false
	if firstMainID != 0 {
		if _, seen := stripScanSeenItemIDs[firstMainID]; seen {
			pageRepeated = true
		} else {
			stripScanSeenItemIDs[firstMainID] = struct{}{}
		}
	}

	if !pageRepeated {
		for className, qty := range classQtys {
			stripScanCounts[className] += qty
		}
		for name, ids := range classItemIDs {
			stripScanItemIDs[name] = append(stripScanItemIDs[name], ids...)
		}
	}

	pageLimitReached := stripScanPageCount >= 25
	a.AddLogMsg(fmt.Sprintf("[STRIP_DEBUG] session=%d page=%d items=%d repeated=%t pageLimit=%t", scanID, currentPage, pageRecords, pageRepeated, pageLimitReached))

	stripScanMu.Unlock()

	if pageRepeated || pageLimitReached {
		reason := "wrapped"
		if pageRepeated {
			reason = "repeated page"
		} else if pageLimitReached {
			reason = "page limit"
		}
		a.finalizeStripScan(scanID, reason)
		return
	}

	go func() {
		time.Sleep(stripNextDelay)
		sendGetStripRaw(a, stripGetNextPayload)
	}()

	a.AddLogMsg(fmt.Sprintf("[STRIP] continuing scan: page %d had %d item(s), requesting next after %s", currentPage, pageRecords, stripNextDelay))
	return
}

func (a *App) finalizeStripScan(sessionID int, reason string) {
	stripScanMu.Lock()
	if !stripScanActive || stripScanSessionID != sessionID {
		a.AddLogMsg(fmt.Sprintf("[STRIP_DEBUG] finalize skipped session=%d active=%t currentSession=%d", sessionID, stripScanActive, stripScanSessionID))
		stripScanMu.Unlock()
		return
	}

	items := buildStripScanItems()
	itemIDs := stripScanItemIDs
	pagesScanned := stripScanPageCount
	stripScanActive = false
	stripScanLastPacketAt = time.Time{}
	stripScanStartedAt = time.Time{}
	stripScanMu.Unlock()
	a.AddLogMsg(fmt.Sprintf("[STRIP_DEBUG] finalize session=%d reason=%s pages=%d", sessionID, reason, pagesScanned))

	handItemsMu.Lock()
	currentHandItems = items
	currentHandItemIDs = itemIDs
	handItemsMu.Unlock()

	a.AddLogMsg(fmt.Sprintf("[STRIP] scan complete after %d page(s), reason=%s; hand item types=%d", pagesScanned, reason, len(items)))
	for i, item := range items {
		a.AddLogMsg(fmt.Sprintf("[STRIP] hand[%d] name=%q qty=%d", i, item.Name, item.Quantity))
	}
	a.emitHandItemsUpdate()

	// If a trade is open, refresh the frozen hand snapshot so coverage
	// checks reflect recent hand removals/additions, then re-run coverage.
	if tradeOpen {
		// Under the strict lifecycle policy, avoid updating the frozen
		// snapshot mid-trade — only update when no snapshot exists.
		if !strictTradeSnapshotLifecycle || !tradeHandSnapshotReady {
			// captureTradeHandSnapshot will copy currentHandItems into tradeHandSnapshot
			// and mark the snapshot ready for coverage comparisons.
			go func() {
				a.captureTradeHandSnapshot()
				// small delay to ensure snapshot processed before coverage check
				time.Sleep(50 * time.Millisecond)
				a.notifyTradeQuantityCoverage()
			}()
		} else {
			a.AddLogMsg("[STRIP_DEBUG] trade open and strict snapshot lifecycle active; skipping snapshot refresh")
		}
	}
}

func (a *App) handleOutgoingGetStrip(e *g.Intercept) {
	if e.Packet.Header.Dir != g.Out {
		return
	}
	payload := strings.TrimSpace(string(e.Packet.Data))
	a.AddLogMsg(fmt.Sprintf("[STRIP_DEBUG] outgoing GETSTRIP[65] payload=%q", payload))
}

func sendGetStripRaw(a *App, payload string) {
	trimmed := strings.TrimSpace(payload)
	ext.Send(g.Out.Id("GETSTRIP"), []byte(trimmed))
	a.AddLogMsg(fmt.Sprintf("[STRIP_DEBUG] raw GETSTRIP send payload=%q", trimmed))
}

func buildStripScanItems() []TradeItem {
	if len(stripScanCounts) == 0 {
		return []TradeItem{}
	}

	names := make([]string, 0, len(stripScanCounts))
	for name := range stripScanCounts {
		names = append(names, name)
	}
	sort.Strings(names)

	items := make([]TradeItem, 0, len(names))
	for _, name := range names {
		items = append(items, TradeItem{
			Name:     name,
			Quantity: stripScanCounts[name],
		})
	}

	return items
}

// diffItems returns the items in `all` that exceed the quantities in `subtract`.

// parseStripInfoPageRaw decodes a STRIPINFO_2 packet body (e.Packet.Data) using
// the real Shockwave grouped format.
// Each record groups all physical items of the same class:
//
//	Field 1 (until \x02): [mainItemId VL64][extraCount VL64]([extraItemId VL64]×N)[Pos VL64][S|I]
//	Field 2 (until \x02): [templateId VL64][VL64][VL64][className string]
//	Field 3 (until \x02): for "S": [DimX VL64][DimY VL64][Colors string]
//	                       for "I": [Props string]
//
// Quantity per record = 1 + extraCount.
// Returns: firstMainID (for wrap detection), record count, className→quantity map.
func parseStripInfoPageRaw(data []byte) (firstMainID int, pageRecords int, classQtys map[string]int, classItemIDs map[string][]int) {
	classQtys = map[string]int{}
	classItemIDs = map[string][]int{}
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
			pos++ // skip \x02
		}
	}

	// record count
	count, ok := readVL64()
	if !ok {
		return
	}
	pageRecords = count

	for i := 0; i < count; i++ {
		if pos >= len(data) {
			break
		}

		// --- Field 1 ---
		mainID, ok := readVL64()
		if !ok {
			break
		}
		if i == 0 {
			firstMainID = mainID
		}

		extraCount, ok := readVL64()
		if !ok {
			break
		}
		extraIDs := make([]int, 0, extraCount)
		for j := 0; j < extraCount; j++ {
			if extraID, ok := readVL64(); ok {
				extraIDs = append(extraIDs, extraID)
			} else {
				break
			}
		}

		if _, ok := readVL64(); !ok { // Pos
			break
		}

		if pos >= len(data) {
			break
		}
		typeChar := data[pos]
		pos++
		skipUntilDelim() // eat remainder of field 1

		// --- Field 2 ---
		if _, ok := readVL64(); !ok { // templateId
			break
		}
		if _, ok := readVL64(); !ok { // extra field 0
			break
		}
		if _, ok := readVL64(); !ok { // extra field 1
			break
		}

		classStart := pos
		for pos < len(data) && data[pos] != 0x02 {
			pos++
		}
		classRaw := strings.ToLower(string(data[classStart:pos]))
		if pos < len(data) {
			pos++ // skip \x02
		}

		// --- Field 3 ---
		switch typeChar {
		case 'S':
			readVL64()       // DimX
			readVL64()       // DimY
			skipUntilDelim() // Colors
		case 'I':
			skipUntilDelim() // Props
		default:
			skipUntilDelim()
		}

		normalizedClass, ok := normalizeClassKeyWithVariant(classRaw)
		if !ok {
			continue
		}

		classQtys[normalizedClass] += 1 + extraCount
		classItemIDs[normalizedClass] = append(classItemIDs[normalizedClass], mainID)
		classItemIDs[normalizedClass] = append(classItemIDs[normalizedClass], extraIDs...)
	}
	return
}

// Used to compute one trader's items from the combined TRADE_ITEMS packet.
func diffItems(all []TradeItem, subtract []TradeItem) []TradeItem {
	subtractQty := make(map[string]int, len(subtract))
	for _, item := range subtract {
		subtractQty[item.Name] += item.Quantity
	}

	allQty := make(map[string]int, len(all))
	rawByName := make(map[string]string, len(all))
	names := make([]string, 0, len(all))
	for _, item := range all {
		if allQty[item.Name] == 0 {
			names = append(names, item.Name)
		}
		allQty[item.Name] += item.Quantity
		if rawByName[item.Name] == "" {
			rawByName[item.Name] = item.RawData
		}
	}
	sort.Strings(names)

	result := make([]TradeItem, 0)
	for _, name := range names {
		remaining := allQty[name] - subtractQty[name]
		if remaining > 0 {
			result = append(result, TradeItem{
				Name:     name,
				Quantity: remaining,
				RawData:  rawByName[name],
			})
		}
	}
	return result
}

// emitHandItemsUpdate pushes the player's current hand items to the frontend.
func (a *App) emitHandItemsUpdate() {
	handItemsMu.Lock()
	items := make([]TradeItem, len(currentHandItems))
	copy(items, currentHandItems)
	handItemsMu.Unlock()

	jsonData, err := json.Marshal(items)
	if err != nil {
		return
	}
	runtime.EventsEmit(a.ctx, "handItemsUpdate", string(jsonData))
	// Also notify the configured live-dealer webhook so external dashboards
	// remain in sync whenever the frontend receives a hand update.
	a.sendLiveDealerSnapshot(items)
}

// sendLiveDealerSnapshot posts a hand snapshot to the configured live-dealer webhook.
// Runs asynchronously and logs status via `AddLogMsg`.
func (a *App) sendLiveDealerSnapshot(items []TradeItem) {
	go func(snapshot []TradeItem) {
		payload := LiveDealerStatusPayload{
			LastSeenAt:         time.Now().UTC().Format(time.RFC3339),
			DealerOpen:         dealerAcceptingTrades,
			TradeOpen:          tradeOpen,
			GameActive:         dealerGameActive(),
			SnapshotReady:      tradeHandSnapshotReady,
			DealerName:         a.getCurrentDealerName(),
			RoomName:           a.getCurrentRoomName(),
			MaxUniqueItems:     maxTradeUniqueItems,
			MaxQuantityPerItem: maxTradeQuantityPerItem,
			Snapshot:           snapshot,
		}

		jb, err := json.Marshal(payload)
		if err != nil {
			a.AddLogMsg("[TRADE_HAND_SNAPSHOT] webhook marshal error: " + err.Error())
			return
		}

		url := os.Getenv("LIVE_SYNC_URL")
		if url == "" {
			url = "http://rollorigins.club/api/live-dealer"
		}

		req, err := http.NewRequest("POST", url, bytes.NewReader(jb))
		if err != nil {
			a.AddLogMsg("[TRADE_HAND_SNAPSHOT] webhook request error: " + err.Error())
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer s3cUr3-r4nd0m_v4lu3-6f2b8a")

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			a.AddLogMsg("[TRADE_HAND_SNAPSHOT] webhook POST error: " + err.Error())
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			a.AddLogMsg(fmt.Sprintf("[TRADE_HAND_SNAPSHOT] webhook responded: %s %d", url, resp.StatusCode))
		} else {
			a.AddLogMsg("[TRADE_HAND_SNAPSHOT] webhook sent to " + url)
		}
	}(items)
}

// sendLiveDealerStatus posts a compact status update (dealer open, dealer name)
// to the configured live-dealer webhook. Runs asynchronously and logs status.
func (a *App) sendLiveDealerStatus(open bool, dealerName string) {
	go func(dealerOpen bool, name string) {
		payload := LiveDealerStatusPayload{
			LastSeenAt:         time.Now().UTC().Format(time.RFC3339),
			DealerOpen:         dealerOpen,
			TradeOpen:          tradeOpen,
			GameActive:         dealerGameActive(),
			SnapshotReady:      tradeHandSnapshotReady,
			DealerName:         strings.TrimSpace(name),
			RoomName:           a.getCurrentRoomName(),
			MaxUniqueItems:     maxTradeUniqueItems,
			MaxQuantityPerItem: maxTradeQuantityPerItem,
			RecentGames:        a.getRecentGameSummaries(5),
		}

		jb, err := json.Marshal(payload)
		if err != nil {
			a.AddLogMsg("[LIVE_DEALER_STATUS] webhook marshal error: " + err.Error())
			return
		}

		url := os.Getenv("LIVE_SYNC_URL")
		if url == "" {
			url = "http://rollorigins.club/api/live-dealer"
		}

		req, err := http.NewRequest("POST", url, bytes.NewReader(jb))
		if err != nil {
			a.AddLogMsg("[LIVE_DEALER_STATUS] webhook request error: " + err.Error())
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer s3cUr3-r4nd0m_v4lu3-6f2b8a")

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			a.AddLogMsg("[LIVE_DEALER_STATUS] webhook POST error: " + err.Error())
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			a.AddLogMsg(fmt.Sprintf("[LIVE_DEALER_STATUS] webhook responded: %s %d", url, resp.StatusCode))
		} else {
			a.AddLogMsg("[LIVE_DEALER_STATUS] webhook sent to " + url)
		}
	}(open, dealerName)
}

// sendLiveDealerGames posts an anonymized summary of the last N completed games
// to the configured live-dealer webhook. Never exposes the player name; winner
// is mapped to "Player"/"Dealer"/"Unknown".
func (a *App) sendLiveDealerGames(last int) {
	go func(n int) {
		type GameSummary struct {
			ID          string      `json:"id"`
			Game        string      `json:"game"`
			Winner      string      `json:"winner"`
			Outcome     string      `json:"outcome"`
			StartedAt   string      `json:"startedAt,omitempty"`
			CompletedAt string      `json:"completedAt,omitempty"`
			BetItems    []TradeItem `json:"betItems,omitempty"`
			PayoutItems []TradeItem `json:"payoutItems,omitempty"`
		}
		payload := struct {
			LastSeenAt string        `json:"lastSeenAt"`
			DealerName string        `json:"dealerName"`
			RoomName   string        `json:"roomName"`
			Games      []GameSummary `json:"games"`
		}{}
		payload.LastSeenAt = time.Now().UTC().Format(time.RFC3339)
		payload.DealerName = a.getCurrentDealerName()
		payload.RoomName = a.getCurrentRoomName()

		a.gameHistoryMu.Lock()
		// iterate newest-first; a.gameHistory is prepended on beginGameHistory
		count := 0
		for i := 0; i < len(a.gameHistory) && count < n; i++ {
			entry := a.gameHistory[i]
			if strings.TrimSpace(entry.CompletedAt) == "" {
				continue
			}

			winner := strings.TrimSpace(entry.Winner)
			publicWinner := "Unknown"
			dealerName := strings.TrimSpace(a.getCurrentDealerName())
			if winner != "" {
				// Treat configured dealer name or the literal "Dealer" as dealer win
				if strings.EqualFold(winner, dealerName) || strings.EqualFold(winner, "Dealer") {
					publicWinner = "Dealer"
				} else {
					publicWinner = "Player"
				}
			}

			// If the dealer won, omit payout items (no payout to player)
			var payoutItems []TradeItem
			if publicWinner == "Dealer" {
				payoutItems = nil
			} else {
				payoutItems = cloneTradeItems(entry.PayoutItems)
			}

			gs := GameSummary{
				ID:          entry.ID,
				Game:        entry.Game,
				Winner:      publicWinner,
				Outcome:     entry.Status,
				StartedAt:   entry.StartedAt,
				CompletedAt: entry.CompletedAt,
				BetItems:    cloneTradeItems(entry.BetItems),
				PayoutItems: payoutItems,
			}
			payload.Games = append(payload.Games, gs)
			count++
		}
		a.gameHistoryMu.Unlock()

		if len(payload.Games) == 0 {
			a.AddLogMsg("[LIVE_DEALER_GAMES] no completed games to send")
			return
		}

		jb, err := json.Marshal(payload)
		if err != nil {
			a.AddLogMsg("[LIVE_DEALER_GAMES] marshal error: " + err.Error())
			return
		}

		url := os.Getenv("LIVE_SYNC_URL")
		if url == "" {
			url = "http://rollorigins.club/api/live-dealer"
		}

		req, err := http.NewRequest("POST", url, bytes.NewReader(jb))
		if err != nil {
			a.AddLogMsg("[LIVE_DEALER_GAMES] request error: " + err.Error())
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer s3cUr3-r4nd0m_v4lu3-6f2b8a")

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			a.AddLogMsg("[LIVE_DEALER_GAMES] POST error: " + err.Error())
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			a.AddLogMsg(fmt.Sprintf("[LIVE_DEALER_GAMES] webhook responded: %s %d", url, resp.StatusCode))
		} else {
			a.AddLogMsg("[LIVE_DEALER_GAMES] webhook sent to " + url)
		}
	}(last)
}

func (a *App) captureTradeHandSnapshot() {
	handItemsMu.Lock()
	tradeHandSnapshot = make([]TradeItem, len(currentHandItems))
	copy(tradeHandSnapshot, currentHandItems)
	tradeHandSnapshotReady = true
	snapshot := make([]TradeItem, len(tradeHandSnapshot))
	copy(snapshot, tradeHandSnapshot)
	handItemsMu.Unlock()

	parts := make([]string, 0, len(snapshot))
	for _, item := range snapshot {
		parts = append(parts, fmt.Sprintf("%s=%d", item.Name, item.Quantity))
	}

	joined := strings.Join(parts, ",")
	a.AddLogMsg("[TRADE_HAND_SNAPSHOT] captured: " + joined)
	a.AddLogMsg(fmt.Sprintf("[TRADE_HAND_SNAPSHOT] ready=true (items=%d)", len(snapshot)))
	// Send snapshot to configured live-dealer webhook (non-blocking)
	a.sendLiveDealerSnapshot(snapshot)

	// If the partner already accepted while we were refreshing the hand,
	// attempt an immediate auto-accept now the frozen snapshot is ready.
	go a.maybeAutoAcceptOnSnapshotReady("capture")
}

func (a *App) invalidateTradeHandSnapshot(reason string) {
	handItemsMu.Lock()
	tradeHandSnapshot = nil
	tradeHandSnapshotReady = false
	handItemsMu.Unlock()

	a.AddLogMsg(fmt.Sprintf("[TRADE_HAND_SNAPSHOT] invalidated: %s", reason))
	a.AddLogMsg("[TRADE_HAND_SNAPSHOT] ready=false")
}

func (a *App) forceRefreshHandSnapshot(reason string) bool {
	a.invalidateTradeHandSnapshot(reason)

	scanID := a.requestPlayerStrip(true)
	a.AddLogMsg(fmt.Sprintf("[STRIP_DEBUG] forced strip requested session=%d reason=%s", scanID, reason))
	if ok := waitForStripScanCompletion(scanID, 20*time.Second); !ok {
		a.AddLogMsg(fmt.Sprintf("[TRADE_HAND_SNAPSHOT] forced hand sync timeout (session=%d reason=%s)", scanID, reason))
		a.finalizeStripScan(scanID, "timeout fallback")

		handItemsMu.Lock()
		haveHand := len(currentHandItems) > 0
		handItemsMu.Unlock()
		if !haveHand {
			a.AddLogMsg(fmt.Sprintf("[TRADE_HAND_SNAPSHOT] no hand items available after timeout (session=%d reason=%s); proceeding with empty snapshot", scanID, reason))
			handItemsMu.Lock()
			currentHandItems = []TradeItem{}
			currentHandItemIDs = map[string][]int{}
			handItemsMu.Unlock()
		} else {
			a.AddLogMsg(fmt.Sprintf("[TRADE_HAND_SNAPSHOT] using timeout fallback hand snapshot (session=%d reason=%s)", scanID, reason))
		}
	}

	// Always overwrite with the latest current hand after a forced scan.
	a.captureTradeHandSnapshot()
	a.AddLogMsg(fmt.Sprintf("[TRADE_HAND_SNAPSHOT] forced refresh complete (session=%d reason=%s)", scanID, reason))
	return true
}

func (a *App) notifyTradeQuantityCoverage() {
	// Compute shortages and notify partner if we cannot cover payout
	shortages := a.getTradeCoverageShortages()
	if len(shortages) == 0 {
		lastTradeCoverageNotice = ""
		lastTradeBlockNotice = ""
		a.AddLogMsg("[TRADE_COVERAGE] sufficient stock for payout")

		// Cancel any active shortage monitor since coverage is sufficient now.
		stopShortageMonitor()

		// If the partner had already accepted while we were resyncing the
		// hand, attempt an immediate accept now the snapshot and coverage
		// checks are clear.
		go a.maybeAutoAcceptOnSnapshotReady("coverage")
		return
	}

	// During an active payout flow we skip force-closing here; caller
	// (payout logic) handles shortages differently.
	if payoutTradeActive {
		a.AddLogMsg("[TRADE_COVERAGE] shortages detected but skipping enforcement during payout trade")
		return
	}

	// No grace timer now — close immediately with fixed message.
	stopShortageMonitor()

	msg := "Sorry none avabile to see my hand - rollorigins.club"
	changed := msg != lastTradeCoverageNotice
	lastTradeCoverageNotice = msg
	lastTradeBlockNotice = msg

	// Rate-limit public shouts so repeated incoming updates don't spam chat.
	now := time.Now()
	shouldShout := changed && (lastTradeCoverageShoutAt.IsZero() || now.Sub(lastTradeCoverageShoutAt) > tradeShoutCooldown)
	if shouldShout {
		lastTradeCoverageShoutAt = now
	} else {
		a.AddLogMsg("[TRADE_COVERAGE] shout suppressed by cooldown")
	}

	partnerName := strings.TrimSpace(lastTradePartnerName)
	if partnerName == "" {
		partnerName = "Player"
	}

	a.AddLogMsg(fmt.Sprintf("[TRADE_COVERAGE] immediate shortage close with %s: %s", partnerName, msg))

	go func(m string, shout bool) {
		if shout {
			time.Sleep(350 * time.Millisecond)
			ext.Send(out.SHOUT, m)
		}

		time.Sleep(1200 * time.Millisecond)
		ext.Send(out.TRADE_CLOSE)

		time.Sleep(1500 * time.Millisecond)
		a.reopenDealerIdle("insufficient hand stock")
	}(msg, shouldShout)

	a.noteCurrentGameHistory("Trade closed immediately because dealer hand could not cover payout")
}

func (a *App) getTradeCoverageShortages() []tradeShortage {
	// Ensure we have a frozen hand snapshot to compare against.
	handItemsMu.Lock()
	ready := tradeHandSnapshotReady
	if !ready {
		handItemsMu.Unlock()
		return nil
	}
	// copy snapshot
	handSnapshot := make([]TradeItem, len(tradeHandSnapshot))
	copy(handSnapshot, tradeHandSnapshot)
	handItemsMu.Unlock()

	// copy current partner trade items
	tradeItemsMu.Lock()
	partnerItems := make([]TradeItem, len(currentTradeItems))
	copy(partnerItems, currentTradeItems)
	tradeItemsMu.Unlock()

	if len(partnerItems) == 0 {
		return nil
	}

	// required payout quantities (uses existing logic: bet*2)
	required := payoutRequirementsFromBetItems(partnerItems)
	if len(required) == 0 {
		return nil
	}

	// Build canonical maps using normalizeClassKeyWithVariant so
	// variant suffixes (e.g. *4) are treated as part of the name.
	handMap := map[string]int{}
	for _, it := range handSnapshot {
		key := strings.ToLower(strings.TrimSpace(it.Name))
		if k, ok := normalizeClassKeyWithVariant(it.Name); ok {
			key = k
		}
		handMap[key] += it.Quantity
	}

	incomingMap := map[string]int{}
	for _, it := range partnerItems {
		key := strings.ToLower(strings.TrimSpace(it.Name))
		if k, ok := normalizeClassKeyWithVariant(it.Name); ok {
			key = k
		}
		incomingMap[key] += it.Quantity
	}

	// Recompute required payouts using canonical keys to match above maps.
	requiredCanon := map[string]int{}
	for _, it := range partnerItems {
		key := strings.ToLower(strings.TrimSpace(it.Name))
		if k, ok := normalizeClassKeyWithVariant(it.Name); ok {
			key = k
		}
		if it.Quantity <= 0 {
			continue
		}
		requiredCanon[key] += it.Quantity * 2
	}

	shortages := make([]tradeShortage, 0)
	for name, req := range requiredCanon {
		haveHand := handMap[name]
		incoming := incomingMap[name]
		available := haveHand + incoming
		if available < req {
			shortages = append(shortages, tradeShortage{
				Name:        name,
				Required:    req,
				Have:        available,
				PayoutTotal: req,
				HaveHand:    haveHand,
				Incoming:    incoming,
			})
		}
	}

	// If shortages present, log debug view of maps to aid diagnosis.
	if len(shortages) > 0 {
		// build readable lists
		handKeys := make([]string, 0, len(handMap))
		for k := range handMap {
			handKeys = append(handKeys, fmt.Sprintf("%s=%d", k, handMap[k]))
		}
		sort.Strings(handKeys)
		incomingKeys := make([]string, 0, len(incomingMap))
		for k := range incomingMap {
			incomingKeys = append(incomingKeys, fmt.Sprintf("%s=%d", k, incomingMap[k]))
		}
		sort.Strings(incomingKeys)
		reqKeys := make([]string, 0, len(requiredCanon))
		for k := range requiredCanon {
			reqKeys = append(reqKeys, fmt.Sprintf("%s=%d", k, requiredCanon[k]))
		}
		sort.Strings(reqKeys)
		a.AddLogMsg(fmt.Sprintf("[TRADE_COVERAGE_DEBUG] hand=%s incoming=%s required=%s", strings.Join(handKeys, ","), strings.Join(incomingKeys, ","), strings.Join(reqKeys, ",")))
	}

	return shortages
}

// maybeAutoAcceptOnSnapshotReady attempts to auto-accept the trade immediately
// when a frozen hand snapshot becomes available and the partner has already
// signalled acceptance. This helps avoid the race where TRADE_ACCEPT arrives
// before the hand snapshot is ready and the scheduled auto-accept times out.
func (a *App) maybeAutoAcceptOnSnapshotReady(context string) {
	if !partnerTradeAccepted || tradeAutoAccepted || tradeAutoAcceptPending {
		return
	}

	handItemsMu.Lock()
	ready := tradeHandSnapshotReady
	handItemsMu.Unlock()
	if !ready {
		return
	}

	// Check coverage shortages (should be non-nil since snapshot is ready)
	shortages := a.getTradeCoverageShortages()
	if shortages == nil {
		return
	}
	if len(shortages) > 0 {
		a.AddLogMsg("[TRADE_ACCEPT] not auto-accepting: shortages detected on snapshot ready")
		return
	}

	// Validate trade limits before auto-accepting
	tradeItemsMu.Lock()
	itemsCopy := make([]TradeItem, len(currentTradeItems))
	copy(itemsCopy, currentTradeItems)
	tradeItemsMu.Unlock()
	if v := getTradeLimitViolation(itemsCopy); v != nil {
		a.AddLogMsg("[TRADE_ACCEPT] not auto-accepting: trade limit violation on snapshot ready")
		return
	}

	// All checks passed — accept the trade now and mark as auto-accepted.
	ext.Send(out.TRADE_ACCEPT)
	tradeAutoAccepted = true
	tradeAutoAcceptPending = false
	a.AddLogMsg(fmt.Sprintf("[TRADE_ACCEPT] sent outgoing[69] (snapshot-ready %s)", context))
	// If this is a payout, start the payout response timeout monitor now that
	// we've accepted and are waiting for the partner to confirm.
	if payoutTradeActive && !payoutResponseTimeoutActive {
		a.AddLogMsg("[PAYOUT] starting payout response timeout monitor after snapshot-ready accept")
		a.startPayoutResponseTimeoutMonitor(payoutTargetName, payoutTargetID, payoutTargetName)
	}
}

func formatTradeShortages(shortages []tradeShortage) string {
	parts := make([]string, 0, len(shortages))
	for _, shortage := range shortages {
		parts = append(parts, fmt.Sprintf(
			"%s hand %d traded %d",
			formatTradeItemName(shortage.Name),
			shortage.HaveHand,
			shortage.Required,
		))
	}
	return strings.Join(parts, ", ")
}

// sendTradeCompletionMessage sends the post-trade game prompt sequence.
func (a *App) sendTradeCompletionMessage() {
	tradeItemsMu.Lock()
	gameBetItems = make([]TradeItem, len(currentTradeItems))
	copy(gameBetItems, currentTradeItems)
	tradeItemsMu.Unlock()
	a.emitActiveGameBetItemsUpdate()

	if len(gameBetItems) == 0 {
		a.AddLogMsg("[TRADE_MESSAGE] no items detected in trade, continuing anyway")
	} else {
		a.AddLogMsg(fmt.Sprintf("[TRADE_MESSAGE] recorded %d bet item type(s) for payout", len(gameBetItems)))
	}

	partnerName := normalizeUsername(strings.TrimSpace(lastTradePartnerName))
	partnerID := lastTradePartnerID
	partnerToken := strings.TrimSpace(lastTradePartnerToken)

	if partnerID <= 0 && stableTradePartnerID > 0 {
		partnerID = stableTradePartnerID
	}
	if partnerName == "" || strings.EqualFold(partnerName, "Unknown") {
		if stableName := normalizeUsername(strings.TrimSpace(stableTradePartnerName)); stableName != "" {
			partnerName = stableName
		}
	}
	if partnerToken == "" {
		partnerToken = strings.TrimSpace(stableTradePartnerToken)
	}

	if partnerName == "" || strings.EqualFold(partnerName, "Unknown") {
		if resolved, ok := lookupUsers28TradeID(partnerID); ok {
			partnerName = normalizeUsername(strings.TrimSpace(resolved))
			lastTradePartnerName = partnerName
		} else if resolved, ok := lookupUsers28Token(partnerToken); ok {
			partnerName = normalizeUsername(strings.TrimSpace(resolved))
			lastTradePartnerName = partnerName
		} else if resolved, ok := lookupRoomEntityNameByIndex(partnerID); ok {
			partnerName = normalizeUsername(strings.TrimSpace(resolved))
			lastTradePartnerName = partnerName
		}
	}
	if partnerName == "" || strings.EqualFold(partnerName, "Unknown") {
		partnerName = "Player"
	}
	lastTradePartnerID = partnerID
	if strings.TrimSpace(lastTradePartnerToken) == "" {
		lastTradePartnerToken = partnerToken
	}

	// Keep the stable copy alive for payout and post-round reopen.
	if stableTradePartnerID <= 0 && partnerID > 0 {
		stableTradePartnerID = partnerID
	}
	if strings.TrimSpace(stableTradePartnerName) == "" || strings.EqualFold(stableTradePartnerName, "Unknown") {
		stableTradePartnerName = partnerName
	}
	if strings.TrimSpace(stableTradePartnerToken) == "" {
		stableTradePartnerToken = partnerToken
	}

	a.AddLogMsg("[TRADE_FLOW] calling beginGameHistory")
	a.beginGameHistory(partnerName, gameBetItems)
	a.AddLogMsg("[TRADE_FLOW] beginGameHistory returned")

	first := fmt.Sprintf("%s what game do you want to play?", partnerName)
	second := "Shout pkr, 21, 13, TriH, TriL"
	awaitingGameChoice = true
	gameChoiceUnreadableWarned = false
	awaitingGameChoicePartnerName = normalizeUsername(strings.TrimSpace(tradeStarterName))
	if awaitingGameChoicePartnerName == "" || strings.EqualFold(awaitingGameChoicePartnerName, "Unknown") {
		awaitingGameChoicePartnerName = normalizeUsername(strings.TrimSpace(lastTradePartnerName))
	}
	if awaitingGameChoicePartnerName == "" || strings.EqualFold(awaitingGameChoicePartnerName, "Unknown") {
		awaitingGameChoicePartnerName = normalizeUsername(strings.TrimSpace(stableTradePartnerName))
	}

	awaitingGameChoicePartnerID = 0
	if tradeStarterChatID > 0 {
		awaitingGameChoicePartnerID = tradeStarterChatID
	}

	if awaitingGameChoicePartnerID <= 0 && awaitingGameChoicePartnerName != "" {
		if chatIdx, ok := lookupRoomEntityIndexByName(awaitingGameChoicePartnerName); ok && chatIdx > 0 {
			awaitingGameChoicePartnerID = chatIdx
		} else if chatIdx, ok := waitForUsers28RoomIndexByName(awaitingGameChoicePartnerName, 900*time.Millisecond); ok && chatIdx > 0 {
			awaitingGameChoicePartnerID = chatIdx
		} else if chatIdx, ok := lookupUsers28RoomIndexByName(awaitingGameChoicePartnerName); ok && chatIdx > 0 {
			awaitingGameChoicePartnerID = chatIdx
		}
	}

	a.AddLogMsg(fmt.Sprintf("[TRADE_MESSAGE_DEBUG] starter=%q starterTradeID=%d starterChatID=%d awaitingGameChoicePartnerID=%d stableTradePartnerID=%d",
		awaitingGameChoicePartnerName, tradeStarterTradeID, tradeStarterChatID, awaitingGameChoicePartnerID, stableTradePartnerID))

	a.startGameChoiceTimeoutMonitor()

	a.AddLogMsg(fmt.Sprintf("[TRADE_MESSAGE] shouting: %q", first))
	ext.Send(out.SHOUT, first)

	go func(msg string) {
		time.Sleep(1750 * time.Millisecond)
		a.AddLogMsg(fmt.Sprintf("[TRADE_MESSAGE] shouting: %q", msg))
		ext.Send(out.SHOUT, msg)
	}(second)
}

func formatTradeItemName(name string) string {
	parts := strings.Split(name, "_")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

type user28Entry struct {
	Token      string
	ShortToken string
	Name       string
	RoomIndex  int
}

var users28FigurePrefixes = []string{
	"hd-", "hr-", "ch-", "lg-", "sh-",
	"ha-", "he-", "ea-", "fa-", "ca-",
	"cc-", "wa-", "cp-",
}

func hasUsers28FigurePrefix(s string) bool {
	for _, prefix := range users28FigurePrefixes {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

func extractUsers28Entries(raw string) []user28Entry {
	b := []byte(raw)
	entries := make([]user28Entry, 0)
	seen := map[string]struct{}{}

	for i := 0; i < len(b)-4; i++ {
		if b[i] != 0x02 {
			continue
		}
		if !hasUsers28FigurePrefix(raw[i+1:]) {
			continue
		}

		// Figure field delimiter found; name is immediately before this delimiter.
		nameEnd := i
		nameStart := nameEnd
		for nameStart > 0 && isLikelyNameChar(b[nameStart-1]) {
			nameStart--
		}
		if nameEnd-nameStart < 2 {
			continue
		}

		// Shockwave often prefixes names with a length marker (e.g. MWebsedit).
		// If first two chars are uppercase, drop the first byte as the marker.
		rawNameStart := nameStart
		adjNameStart := rawNameStart
		if nameEnd-nameStart >= 3 && b[nameStart] >= 'A' && b[nameStart] <= 'Z' && b[nameStart+1] >= 'A' && b[nameStart+1] <= 'Z' {
			adjNameStart = rawNameStart + 1
		}

		name := strings.TrimSpace(string(b[adjNameStart:nameEnd]))
		if len(name) < 2 {
			continue
		}

		if adjNameStart < 4 {
			continue
		}
		// Token is directly before the actual name start (after optional marker trim).
		tokenStart := adjNameStart - 4

		roomIndex := 0
		scanStart := tokenStart - 6
		if scanStart < 0 {
			scanStart = 0
		}
		for startOff := scanStart; startOff < tokenStart; startOff++ {
			vlen := gencoding.VL64DecodeLen(b[startOff])
			if vlen > 0 && vlen <= 6 && startOff+vlen == tokenStart {
				v := gencoding.VL64Decode(b[startOff:tokenStart])
				if v > 0 {
					roomIndex = v
					break
				}
			}
		}
		token := string(b[tokenStart:adjNameStart])
		if roomIndex <= 0 && !isLikelyToken(token) {
			continue
		}

		shortToken := ""
		if tokenStart >= 2 {
			shortCandidate := string(b[tokenStart-2 : tokenStart])
			if isLikelyChatToken(shortCandidate) {
				shortToken = shortCandidate
			}
		}

		key := fmt.Sprintf("%d|%s|%s|%s", roomIndex, strings.ToLower(name), shortToken, token)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		entries = append(entries, user28Entry{
			Token:      token,
			ShortToken: shortToken,
			Name:       name,
			RoomIndex:  roomIndex,
		})
	}

	return entries
}

func isLikelyNameChar(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '_' || b == '-'
}

func isLikelyToken(s string) bool {
	if len(s) != 4 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 32 || s[i] > 126 {
			return false
		}
	}
	return true
}

func (a *App) handleRoomReady(e *g.Intercept) {
	roomMu.Lock()
	roomReadySeen = true
	roomMu.Unlock()
	clearRoomUserCaches(a)
	a.AddLogMsg("[ROOM_READY] room ready received")
	a.AddLogMsg("[ROOM_USERS] cleared cached room users")
	go requestRoomUsers(a)
}

func (a *App) handleRoomUsers(e *g.Intercept) {
	defer func() {
		if r := recover(); r != nil {
			a.AddLogMsg(fmt.Sprintf("[ROOM_USERS] failed to parse packet %d: %v", e.Packet.Header.Value, r))
		}
	}()

	a.AddLogMsg(fmt.Sprintf("[ROOM_USERS] received packet %d len=%d", e.Packet.Header.Value, len(e.Packet.Data)))

	// In this client, header 28 often uses raw USERS28 layout, not room.Entity wire format.
	// That payload is handled by handleUsers28Packet; skip structured decode here.
	if e.Packet.Header.Value == 28 {
		a.AddLogMsg("[ROOM_USERS] header 28 uses raw USERS28 layout; skipping room.Entity decode")
		return
	}

	count := e.Packet.ReadInt()
	parsedUsers := map[int]room.Entity{}

	for range count {
		var entity room.Entity
		e.Packet.Read(&entity)
		if entity.Type == room.User {
			parsedUsers[entity.Index] = entity
		}
	}

	// Do not populate USERS28 caches from structured room.Entity packets.
	// USERS[28] parsed by Python is the only room-user source of truth.

	roomMu.Lock()
	for index, entity := range parsedUsers {
		roomEntities[index] = entity
	}
	roomMu.Unlock()

	for _, line := range summarizeRoomUsers() {
		a.AddLogMsg("[ROOM_USERS] " + line)
	}
}

func requestRoomUsers(a *App) {
	defer func() {
		if recover() != nil {
			a.AddLogMsg("[ROOM_USERS] request failed")
		}
	}()

	roomUsersReqMu.Lock()
	lastRoomUsersRequestAt = time.Now()
	roomUsersReqMu.Unlock()

	// G_USRS is the packet this client uses to request the in-room USERS list (header 61).
	a.ext.Send(out.G_USRS)
	// Keep legacy request as a secondary path in case the server expects both in some sessions.
	a.ext.Send(out.GETSPACENODEUSERS)
	startIncomingHeaderSniff(8 * time.Second)
	a.AddLogMsg("[ROOM_USERS] requested current room users via G_USRS + GETSPACENODEUSERS")
}

func shouldRefreshRoomUsers() bool {
	roomUsersReqMu.Lock()
	last := lastRoomUsersRequestAt
	roomUsersReqMu.Unlock()
	return time.Since(last) > 5*time.Second
}

func (a *App) emitRoomIdentityUpdate() {
	users28Mu.Lock()
	entries := make([]RoomIdentityEntry, 0, len(users28Canonical))
	for _, user := range users28Canonical {
		entries = append(entries, RoomIdentityEntry{
			Name:      strings.TrimSpace(user.Username),
			Short:     "",
			ChatIndex: user.ChatID,
			RoomIndex: user.TradeID,
			Token:     strings.TrimSpace(user.TokenHex),
			TradeID:   strings.TrimSpace(user.TradeIDRaw),
			EntityID:  strings.TrimSpace(user.EntityID),
		})
	}
	users28Mu.Unlock()

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].ChatIndex == entries[j].ChatIndex {
			return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
		}
		return entries[i].ChatIndex < entries[j].ChatIndex
	})

	jsonData, err := json.Marshal(entries)
	if err != nil {
		a.AddLogMsg(fmt.Sprintf("[ROOM_USERS] failed to marshal room identity update: %v", err))
		return
	}
	runtime.EventsEmit(a.ctx, "roomIdentityUpdate", string(jsonData))
}

func resolveChatSenderName(index int) (string, bool) {
	if index <= 0 {
		return "", false
	}
	if name, ok := lookupUsers28Index(index); ok {
		return name, true
	}
	return lookupRoomEntityNameByIndex(index)
}

func startIncomingHeaderSniff(duration time.Duration) {
	headerSniffMu.Lock()
	defer headerSniffMu.Unlock()
	headerSniffUntil = time.Now().Add(duration)
	headerSniffSeen = map[uint16]bool{}
}

func handleIncomingHeaderSniff(a *App, e *g.Intercept) {
	if e.Packet.Header.Dir != g.In {
		return
	}

	// If configured, block incoming recommended-room list packets.
	blockRecommendedMu.Lock()
	block := blockRecommendedRooms
	blockRecommendedMu.Unlock()
	if block {
		name := ext.Headers().Name(e.Packet.Header)
		if strings.Contains(strings.ToLower(name), "recommend") || e.Packet.Header.Value == 351 {
			a.AddLogMsg(fmt.Sprintf("[BLOCK] blocking incoming recommended rooms packet [%d:%s]", e.Packet.Header.Value, name))
			e.Block()
			return
		}
	}

	// If configured, block incoming slide-object-bundle packets (header 230).
	blockSlideObjectMu.Lock()
	blockSlide := blockSlideObjectBundle
	blockSlideObjectMu.Unlock()
	if blockSlide {
		name := ext.Headers().Name(e.Packet.Header)
		if strings.Contains(strings.ToLower(name), "slideobjectbundle") || e.Packet.Header.Value == 230 {
			a.AddLogMsg(fmt.Sprintf("[BLOCK] blocking incoming slide-object-bundle packet [%d:%s]", e.Packet.Header.Value, name))
			e.Block()
			return
		}
	}

	// If configured, block incoming STATUS_EFFECTS (header 1242).
	blockStatusEffectsMu.Lock()
	blockStatus := blockStatusEffects
	blockStatusEffectsMu.Unlock()
	if blockStatus {
		name := ext.Headers().Name(e.Packet.Header)
		if strings.Contains(strings.ToLower(name), "status_effect") || e.Packet.Header.Value == 1242 {
			a.AddLogMsg(fmt.Sprintf("[BLOCK] blocking incoming status-effects packet [%d:%s]", e.Packet.Header.Value, name))
			e.Block()
			return
		}
	}

	// If configured, block incoming REMOVE_BUDDY (header 138).
	blockRemoveBuddyMu.Lock()
	blockRemove := blockRemoveBuddy
	blockRemoveBuddyMu.Unlock()
	if blockRemove {
		name := ext.Headers().Name(e.Packet.Header)
		if strings.Contains(strings.ToLower(name), "remove_buddy") || e.Packet.Header.Value == 138 {
			a.AddLogMsg(fmt.Sprintf("[BLOCK] blocking incoming remove-buddy packet [%d:%s]", e.Packet.Header.Value, name))
			e.Block()
			return
		}
	}

	// If configured, block incoming FRIEND_LIST_UPDATE (header 13).
	blockFriendListUpdateMu.Lock()
	blockFriend := blockFriendListUpdate
	blockFriendListUpdateMu.Unlock()
	if blockFriend {
		name := ext.Headers().Name(e.Packet.Header)
		if strings.Contains(strings.ToLower(name), "friend_list_update") || e.Packet.Header.Value == 13 {
			a.AddLogMsg(fmt.Sprintf("[BLOCK] blocking incoming friend-list-update packet [%d:%s]", e.Packet.Header.Value, name))
			e.Block()
			return
		}
	}

	headerSniffMu.Lock()
	active := time.Now().Before(headerSniffUntil)
	if !active {
		headerSniffMu.Unlock()
		return
	}

	header := e.Packet.Header.Value
	if headerSniffSeen[header] {
		headerSniffMu.Unlock()
		return
	}
	headerSniffSeen[header] = true
	headerSniffMu.Unlock()

	preview := string(e.Packet.Data)
	if len(preview) > 32 {
		preview = preview[:32]
	}
	name := ext.Headers().Name(e.Packet.Header)
	a.AddLogMsg(fmt.Sprintf("[HEADER_SNIFF] incoming[%d:%s] len=%d preview=%q", header, name, len(e.Packet.Data), preview))
}

// handleOutgoingHeaderSniff inspects outgoing headers and blocks them when configured.
func handleOutgoingHeaderSniff(a *App, e *g.Intercept) {
	if e.Packet.Header.Dir != g.Out {
		return
	}

	// Block outgoing recommended-rooms request (e.g., GET_RECOMMENDED_ROOMS header 264)
	blockOutgoingRecommendedMu.Lock()
	outBlock := blockOutgoingRecommendedRooms
	blockOutgoingRecommendedMu.Unlock()
	if outBlock {
		name := ext.Headers().Name(e.Packet.Header)
		if strings.Contains(strings.ToLower(name), "recommend") || e.Packet.Header.Value == 264 {
			a.AddLogMsg(fmt.Sprintf("[BLOCK] blocking outgoing recommended rooms packet [%d:%s]", e.Packet.Header.Value, name))
			e.Block()
			return
		}
	}

	// Block outgoing slide-object-bundle (header 230)
	blockSlideObjectOutgoingMu.Lock()
	outSlide := blockSlideObjectOutgoing
	blockSlideObjectOutgoingMu.Unlock()
	if outSlide {
		name := ext.Headers().Name(e.Packet.Header)
		if strings.Contains(strings.ToLower(name), "slideobjectbundle") || e.Packet.Header.Value == 230 {
			a.AddLogMsg(fmt.Sprintf("[BLOCK] blocking outgoing slide-object-bundle packet [%d:%s]", e.Packet.Header.Value, name))
			e.Block()
			return
		}
	}

	// If configured, block outgoing POLL_EVENT_ELIGIBILITY (header 1120).
	blockPollEventEligibilityOutgoingMu.Lock()
	outPoll := blockPollEventEligibilityOutgoing
	blockPollEventEligibilityOutgoingMu.Unlock()
	if outPoll {
		name := ext.Headers().Name(e.Packet.Header)
		if strings.Contains(strings.ToLower(name), "poll_event_eligibility") || e.Packet.Header.Value == 1120 {
			a.AddLogMsg(fmt.Sprintf("[BLOCK] blocking outgoing poll-event-eligibility packet [%d:%s]", e.Packet.Header.Value, name))
			e.Block()
			return
		}
	}
}

func summarizeRoomUsers() []string {
	roomMu.Lock()
	defer roomMu.Unlock()

	if len(roomEntities) == 0 {
		return []string{"no cached room users"}
	}

	indices := make([]int, 0, len(roomEntities))
	for index := range roomEntities {
		indices = append(indices, index)
	}
	sort.Ints(indices)

	entries := make([]string, 0, len(indices))
	for _, index := range indices {
		entity := roomEntities[index]
		_, cleanedName, ok := splitTokenAndName(entity.Name)
		if ok {
			entries = append(entries, fmt.Sprintf("%s(%d)", cleanedName, entity.Index))
		} else {
			entries = append(entries, fmt.Sprintf("%s(%d)", entity.Name, entity.Index))
		}
	}

	return []string{fmt.Sprintf("cached %d room user(s): %s", len(entries), strings.Join(entries, ", "))}
}

func describeTradeRoomCandidates(ext *g.Ext) []string {
	roomMu.Lock()
	defer roomMu.Unlock()

	if len(roomEntities) == 0 {
		return []string{"no cached room users"}
	}

	indices := make([]int, 0, len(roomEntities))
	for index := range roomEntities {
		indices = append(indices, index)
	}
	sort.Ints(indices)

	lines := make([]string, 0, len(indices))
	for _, index := range indices {
		entity := roomEntities[index]
		_, cleanName, hasToken := splitTokenAndName(entity.Name)
		displayName := entity.Name
		if hasToken {
			displayName = cleanName
			users28Mu.Lock()
			users28Mu.Unlock()
		}
		libraryPayload := string(ext.NewPacket(out.TRADE_OPEN, index).Data)
		lines = append(lines, fmt.Sprintf(
			"name=%q index=%d candidates{library_int=%q vl64=%q b64_2=%q b64_3=%q}",
			displayName,
			entity.Index,
			libraryPayload,
			encodeVL64(entity.Index),
			encodeB64(entity.Index, 2),
			encodeB64(entity.Index, 3),
		))
	}

	return lines
}

func splitTokenAndName(s string) (token string, name string, ok bool) {
	if len(s) < 6 {
		return "", "", false
	}
	token = s[:4]
	name = normalizeUsername(s[4:])
	if !isLikelyToken(token) || len(name) < 2 {
		return "", "", false
	}
	for i := 0; i < len(name); i++ {
		if !isLikelyNameChar(name[i]) && name[i] != ' ' {
			return "", "", false
		}
	}
	return token, name, true
}

func resolveTradeTokenToRoomIndex(token string) (index int, name string, ok bool) {
	roomMu.Lock()
	defer roomMu.Unlock()

	for _, entity := range roomEntities {
		entityToken, cleanName, hasToken := splitTokenAndName(entity.Name)
		if hasToken && entityToken == token {
			return entity.Index, cleanName, true
		}
	}

	return 0, "", false
}

func encodeVL64(value int) string {
	buf := make([]byte, gencoding.VL64EncodeLen(value))
	gencoding.VL64Encode(buf, value)
	return string(buf)
}

func encodeB64(value int, length int) string {
	buf := make([]byte, length)
	gencoding.B64Encode(buf, value)
	return string(buf)
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

func packetContainsVL64Value(data []byte, value int) bool {
	if value <= 0 || len(data) == 0 {
		return false
	}

	for i := 0; i < len(data); i++ {
		vlen := gencoding.VL64DecodeLen(data[i])
		if vlen <= 0 || vlen > 6 || i+vlen > len(data) {
			continue
		}
		if gencoding.VL64Decode(data[i:i+vlen]) == value {
			return true
		}
	}

	return false
}

func rememberOutgoingTradeOpenTarget(targetID int) {
	if targetID <= 0 {
		return
	}
	tradeOpenStateMu.Lock()
	lastOutgoingTradeOpenID = targetID
	lastOutgoingTradeOpenAt = time.Now()
	tradeOpenStateMu.Unlock()
}

// matchesRecentOutgoingFunc is a convenience wrapper used by the block-all guard.
// Returns true if the packet looks like a response to our own recent TRADE_OPEN.
func matchesRecentOutgoingFunc(data []byte) bool {
	id := 0
	if len(data) > 0 {
		if v, ok := decodeLeadingVL64(data); ok {
			id = v
		}
	}
	_, matched := matchesRecentOutgoingTradeOpen(data, id)
	return matched
}

func matchesRecentOutgoingTradeOpen(data []byte, leadingIncomingID int) (int, bool) {
	tradeOpenStateMu.Lock()
	targetID := lastOutgoingTradeOpenID
	at := lastOutgoingTradeOpenAt
	tradeOpenStateMu.Unlock()

	if targetID <= 0 {
		return 0, false
	}

	if time.Since(at) > 8*time.Second {
		return targetID, false
	}

	if leadingIncomingID > 0 && leadingIncomingID == targetID {
		return targetID, true
	}

	if packetContainsVL64Value(data, targetID) {
		return targetID, true
	}

	return targetID, false
}

func tryReadInt(pkt *g.Packet) (value int, pos int, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()

	value = pkt.ReadInt()
	pos = pkt.Pos
	return value, pos, true
}

func tryReadString(pkt *g.Packet) (value string, pos int, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()

	value = pkt.ReadString()
	pos = pkt.Pos
	return value, pos, true
}

func tryReadIntString(pkt *g.Packet) (id int, text string, pos int, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()

	id = pkt.ReadInt()
	text = pkt.ReadString()
	pos = pkt.Pos
	return id, text, pos, true
}

func tryReadIntInt(pkt *g.Packet) (a int, b int, pos int, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()

	a = pkt.ReadInt()
	b = pkt.ReadInt()
	pos = pkt.Pos
	return a, b, pos, true
}

func (a *App) onChatMessage(e *g.Intercept) {
	msg := e.Packet.ReadString()
	a.AddChatLog("[OUT] " + msg)
	commandMsg := extractInlineCommand(msg)

	// Process commands based on the message prefix and suffix
	if strings.HasPrefix(commandMsg, ":") {
		// Check if already rolling or closing
		if isPokerRolling || isTriRolling || isBJRolling || is13Rolling || isHitting || is13Hitting || isClosing {
			log.Println("Already rolling or closing...")
			e.Block()
			return
		}

		command := strings.TrimPrefix(commandMsg, ":")
		switch {
		case strings.HasSuffix(command, "reset"):
			e.Block()
			resetDiceState()
		case strings.HasSuffix(command, "roll"):
			e.Block()
			a.startPokerRoll()
		case strings.HasSuffix(command, "tri"):
			e.Block()
			isTriRolling = true
			logRollResult := fmt.Sprint("Tri Roll:\n")
			a.AddLogMsg(logRollResult)
			go a.rollTriDice()
		case strings.HasSuffix(command, "close"):
			e.Block()
			go a.closeAllDice()
		case strings.HasSuffix(command, "21"):
			e.Block()
			resetBlackjackSequence()
			blackjackRoundActive = true
			blackjackPlayerTurn = true
			blackjackPlayerName = strings.TrimSpace(lastTradePartnerName)
			if blackjackPlayerName == "" {
				blackjackPlayerName = "Player"
			}
			isBJRolling = true
			logRollResult := fmt.Sprintf("21 Roll:\n")
			a.AddLogMsg(logRollResult)
			go a.rollBjDice()
		case strings.HasSuffix(command, "13"):
			e.Block()
			is13Rolling = true
			logRollResult := fmt.Sprintf("13 Roll:\n")
			a.AddLogMsg(logRollResult)
			go a.roll13Dice()
		case strings.HasPrefix(command, "@"):
			e.Block()
			extra := strings.TrimSpace(strings.TrimPrefix(command, "@"))
			go a.evalAt(extra)
		case strings.HasSuffix(command, "verify"):
			e.Block()
			go verifyResult()
		case strings.HasSuffix(command, "commands"):
			e.Block()
			go a.ShowCommands()
		case strings.HasSuffix(command, "chaton"):
			e.Block()
			ChatIsDisabled = false
		case strings.HasSuffix(command, "chatoff"):
			e.Block()
			ChatIsDisabled = true
		case strings.HasSuffix(command, "tradesoff"):
			e.Block()
			blockAllTrades = true
			a.AddLogMsg("[TRADE_BLOCK] all incoming trades are now blocked")
		case strings.HasSuffix(command, "tradeson"):
			e.Block()
			blockAllTrades = false
			a.AddLogMsg("[TRADE_BLOCK] incoming trades re-enabled")
		}
	}
}

func extractInlineCommand(msg string) string {
	trimmed := strings.TrimSpace(msg)
	if strings.HasPrefix(trimmed, ":") {
		return trimmed
	}

	idx := strings.Index(trimmed, ":")
	if idx < 0 {
		return trimmed
	}

	return strings.TrimSpace(trimmed[idx:])
}

func (a *App) evalAt(msg string) {
	mutex.Lock()
	at := "@" + msg
	ext.Send(out.SHOUT, at)
	a.AddLogMsg(at)
	mutex.Unlock()
}

func (a *App) startPokerRoll() {
	isPokerRolling = true
	logRollResult := fmt.Sprintf("Poker Roll:\n")
	a.AddLogMsg(logRollResult)
	go a.rollPokerDice()
}

func (a *App) beginPokerSequence() {
	playerName := strings.TrimSpace(lastTradePartnerName)
	if playerName == "" {
		playerName = "Player"
	}

	resetPokerSequence()
	pokerSequenceStage = 1
	pokerSequencePlayerName = playerName

	first := "Lets Play!"
	second := fmt.Sprintf("%s Roll", playerName)

	a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] shouting: %q", first))
	ext.Send(out.SHOUT, first)

	go func(msg string) {
		time.Sleep(700 * time.Millisecond)
		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] shouting: %q", msg))
		ext.Send(out.SHOUT, msg)

		time.Sleep(700 * time.Millisecond)
		a.startPokerRoll()
	}(second)
}

func (a *App) beginBlackjackSequence() {
	playerName := strings.TrimSpace(lastTradePartnerName)
	if playerName == "" {
		playerName = "Player"
	}

	resetPokerSequence()
	resetBlackjackSequence()
	blackjackRoundActive = true
	blackjackPlayerTurn = true
	blackjackPlayerName = playerName

	first := "Lets Play!"
	second := fmt.Sprintf("%s Roll", playerName)

	a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] shouting: %q", first))
	ext.Send(out.SHOUT, first)

	go func(msg string) {
		time.Sleep(700 * time.Millisecond)
		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] shouting: %q", msg))
		ext.Send(out.SHOUT, msg)

		time.Sleep(700 * time.Millisecond)
		isBJRolling = true
		a.AddLogMsg("21 Roll:\n")
		go a.rollBjDice()
	}(second)
}

func (a *App) begin13Sequence() {
	playerName := strings.TrimSpace(lastTradePartnerName)
	if playerName == "" {
		playerName = "Player"
	}

	resetPokerSequence()
	resetBlackjackSequence()
	reset13Sequence()
	thirteenRoundActive = true
	thirteenPlayerTurn = true
	thirteenPlayerName = playerName

	first := "Lets Play!"
	second := fmt.Sprintf("%s Roll", playerName)

	a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] shouting: %q", first))
	ext.Send(out.SHOUT, first)

	go func(msg string) {
		time.Sleep(700 * time.Millisecond)
		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] shouting: %q", msg))
		ext.Send(out.SHOUT, msg)

		time.Sleep(700 * time.Millisecond)
		is13Rolling = true
		a.AddLogMsg("13 Roll:\n")
		go a.roll13Dice()
	}(second)
}

func (a *App) beginTriChoiceSequence() {
	playerName := strings.TrimSpace(lastTradePartnerName)
	if playerName == "" {
		playerName = "Player"
	}

	resetPokerSequence()
	resetBlackjackSequence()
	reset13Sequence()
	resetTriSequence()

	awaitingTriChoice = true
	awaitingTriChoicePartnerName = playerName

	if chatIdx, ok := lookupRoomEntityIndexByName(playerName); ok && chatIdx > 0 {
		awaitingTriChoicePartnerID = chatIdx
	} else if chatIdx, ok := lookupUsers28RoomIndexByName(playerName); ok && chatIdx > 0 {
		awaitingTriChoicePartnerID = chatIdx
	} else {
		awaitingTriChoicePartnerID = lastTradePartnerID
	}

	msg := "High or Low?"
	a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] shouting: %q", msg))
	ext.Send(out.SHOUT, msg)
}

func (a *App) beginTriRound(mode string) {
	playerName := strings.TrimSpace(lastTradePartnerName)
	if playerName == "" {
		playerName = "Player"
	}

	resetPokerSequence()
	resetBlackjackSequence()
	reset13Sequence()
	resetTriSequence()

	triRoundActive = true
	triPlayerTurn = true
	triMode = strings.ToLower(strings.TrimSpace(mode))
	triPlayerName = playerName

	gameLabel := "TriH"
	if triMode == "low" {
		gameLabel = "TriL"
	}
	a.setCurrentGameHistoryGame(gameLabel)

	first := "Lets Play!"
	second := fmt.Sprintf("%s Roll", playerName)

	a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] shouting: %q", first))
	ext.Send(out.SHOUT, first)

	go func(msg string) {
		time.Sleep(700 * time.Millisecond)
		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] shouting: %q", msg))
		ext.Send(out.SHOUT, msg)

		time.Sleep(700 * time.Millisecond)
		isTriRolling = true
		a.rollTriDice()
	}(second)
}

func (a *App) start13DealerTurn(reason string) {
	awaiting13Decision = false
	thirteenPlayerTurn = false
	a.AddLogMsg(fmt.Sprintf("[13_DEBUG] dealer turn starting reason=%s playerTotal=%d dealerTotal=%d", reason, thirteenPlayerTotal, thirteenDealerTotal))
	message := "Dealer Roll"
	a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] shouting: %q", message))
	ext.Send(out.SHOUT, message)
	go func() {
		time.Sleep(700 * time.Millisecond)
		is13Rolling = true
		a.roll13Dice()
	}()
}

func (a *App) finalize13Round(playerWins bool, reason string) {
	playerName := strings.TrimSpace(thirteenPlayerName)
	if playerName == "" {
		playerName = strings.TrimSpace(lastTradePartnerName)
	}
	if playerName == "" {
		playerName = "Player"
	}

	playerHand := strconv.Itoa(thirteenPlayerTotal)
	dealerHand := strconv.Itoa(thirteenDealerTotal)
	winnerName := "Dealer"
	if playerWins {
		winnerName = playerName
	}
	winnerMsg := fmt.Sprintf("%s Wins - %s: %s | Dealer: %s", winnerName, playerName, playerHand, dealerHand)

	a.AddLogMsg(fmt.Sprintf("[13_RULES] winner=%s reason=%s player=%d dealer=%d", winnerName, reason, thirteenPlayerTotal, thirteenDealerTotal))
	a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] shouting: %q", winnerMsg))
	if !ChatIsDisabled {
		waitForUnmute(90 * time.Second)
		time.Sleep(800 * time.Millisecond)
		sendMessageWithDelay(winnerMsg)
	}

	payoutTargetID := lastTradePartnerID
	payoutTargetName := playerName
	reset13Sequence()

	if playerWins && payoutTargetID > 0 {
		a.setCurrentGameHistoryResults(playerHand, dealerHand, playerName, "Payout Pending", false)
		a.noteCurrentGameHistory(winnerMsg)
		a.AddLogMsg(fmt.Sprintf("[PAYOUT] 13 player won, initiating payout trade to %s (%d)", payoutTargetName, payoutTargetID))
		resetPayoutRetryState()
		startPayout(a, payoutTargetID, payoutTargetName)
		return
	}

	a.setCurrentGameHistoryResults(playerHand, dealerHand, a.getCurrentDealerName(), "Completed", true)
	a.noteCurrentGameHistory(winnerMsg)
	go a.openDealerAfterRound()
}

func (a *App) finalizeTriRound() {
	playerName := strings.TrimSpace(triPlayerName)
	if playerName == "" {
		playerName = strings.TrimSpace(lastTradePartnerName)
	}
	if playerName == "" {
		playerName = "Player"
	}

	playerHand := strconv.Itoa(triPlayerTotal)
	dealerHand := strconv.Itoa(triDealerTotal)

	playerWins := false
	switch triMode {
	case "high":
		playerWins = triPlayerTotal > triDealerTotal
	case "low":
		playerWins = triPlayerTotal < triDealerTotal
	default:
		playerWins = false
	}

	// Respect computed `playerWins`; removed temporary forced-win test code.

	winnerName := "Dealer"
	if playerWins {
		winnerName = playerName
	}

	gameLabel := "TriH"
	if triMode == "low" {
		gameLabel = "TriL"
	}

	winnerMsg := fmt.Sprintf("%s Wins - %s: %s | Dealer: %s", winnerName, playerName, playerHand, dealerHand)

	a.AddLogMsg(fmt.Sprintf(
		"[TRI_RULES] mode=%s game=%s player=%d dealer=%d winner=%s",
		triMode,
		gameLabel,
		triPlayerTotal,
		triDealerTotal,
		winnerName,
	))
	log.Printf(
		"[TRI_RULES] mode=%s game=%s player=%d dealer=%d winner=%s",
		triMode,
		gameLabel,
		triPlayerTotal,
		triDealerTotal,
		winnerName,
	)

	a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] shouting: %q", winnerMsg))

	if !ChatIsDisabled {
		waitForUnmute(90 * time.Second)
		time.Sleep(800 * time.Millisecond)
		sendMessageWithDelay(winnerMsg)
	}

	payoutTargetID := lastTradePartnerID
	payoutTargetName := playerName
	resetTriSequence()

	if playerWins && payoutTargetID > 0 {
		a.setCurrentGameHistoryResults(playerHand, dealerHand, playerName, "Payout Pending", false)
		a.noteCurrentGameHistory(winnerMsg)
		a.AddLogMsg(fmt.Sprintf("[PAYOUT] tri player won, initiating payout trade to %s (%d)", payoutTargetName, payoutTargetID))
		resetPayoutRetryState()
		startPayout(a, payoutTargetID, payoutTargetName)
		return
	}

	a.setCurrentGameHistoryResults(playerHand, dealerHand, "Dealer", "Completed", true)
	a.noteCurrentGameHistory(winnerMsg)
	go a.openDealerAfterRound()
}

// Reset all saved dice states
func resetDiceState() {
	stopDealerOpenHeartbeat()
	stopTradeWindowTimeoutMonitor()
	stopGameChoiceTimeoutMonitor()
	stopShortageMonitor()
	stopUnderfundedTradeMonitor()
	stopTradeLimitMonitor()
	stopPayout()
	resetPayoutRetryState()

	mutex.Lock()
	defer mutex.Unlock()
	resultsWaitGroup.Wait() // Ensure all dice roll results are processed
	diceList = []*Dice{}
	casinoReady = false
	awaitingTradeOpen = false
	dealerAcceptingTrades = false
	dealerTradeWindowOpen = false
	tradeOpen = false
	dealerResyncInProgress = false
	fakeDiceTestingMode = false
	isPokerRolling, isTriRolling, isBJRolling, is13Rolling, isHitting, is13Hitting, isClosing = false, false, false, false, false, false, false

	// Ensure any pending game-choice timeout is stopped when resetting dice.
	resetTradeAutoFlow()
	resetPokerSequence()
	resetBlackjackSequence()
	reset13Sequence()
	resetTriSequence()
	lastTradePartnerID = 0
	lastTradePartnerName = ""
	lastTradePartnerToken = ""
	gameBetItems = nil
	lastTradeCoverageNotice = ""
	lastTradeBlockNotice = ""
	tradeLimitWasActive = false
	lastTradeLimitNotice = ""
	partnerTradeAccepted = false
	partnerAcceptedSnapshot = nil
}

// StartCasinoSetup enables dice setup recording. This must be called from the frontend
// when the user clicks the "Start Casino" button. It resets any existing dice and
// enables recording of incoming dice IDs. It also receives trade-limit configuration
// values which are stored in global state and emitted in live-dealer payloads.
func (a *App) StartCasinoSetup(dealerName string, roomName string, maxUniqueItems int, maxQuantityPerItem int) {
	// Reset state first (this will lock/unlock internally)
	resetDiceState()

	mutex.Lock()
	diceSetupActive = true
	casinoActive = true
	casinoReady = false
	mutex.Unlock()

	// Resolve dealer name: prefer provided value, then env, then username.
	name := strings.TrimSpace(dealerName)
	if name == "" {
		name = os.Getenv("LIVE_SYNC_DEALER_NAME")
		if name == "" {
			name = os.Getenv("USERNAME")
			if name == "" {
				name = "Dealer"
			}
		}
	}
	a.currentDealerName = name
	// Persist room name from frontend
	a.currentRoomName = strings.TrimSpace(roomName)

	// Sanitize and persist configured trade limits
	if maxUniqueItems < 1 {
		maxUniqueItems = 5
	}
	if maxQuantityPerItem < 1 {
		maxQuantityPerItem = 50
	}
	maxTradeUniqueItems = maxUniqueItems
	maxTradeQuantityPerItem = maxQuantityPerItem
	a.AddLogMsg(fmt.Sprintf("[CONFIG] max trade unique items = %d", maxTradeUniqueItems))
	a.AddLogMsg(fmt.Sprintf("[CONFIG] max trade quantity per item = %d", maxTradeQuantityPerItem))

	a.AddLogMsg("[DICE_SETUP] Dice setup mode enabled - roll all 5 dice now")
	a.emitDiceSetupUpdate()
	// Dealer is not open yet here. It only becomes open after setup completes
	// and a fresh hand snapshot has been captured.
	a.sendLiveDealerStatus(false, name)
}

// PauseCasinoSetup temporarily disables dice setup recording without clearing
// currently recorded dice.
func (a *App) PauseCasinoSetup() {
	mutex.Lock()
	diceSetupActive = false
	mutex.Unlock()

	a.AddLogMsg("[DICE_SETUP] Dice setup paused via UI")
	a.emitDiceSetupUpdate()
}

// ResumeCasinoSetup re-enables dice setup recording without resetting state.
func (a *App) ResumeCasinoSetup() {
	mutex.Lock()
	diceSetupActive = true
	mutex.Unlock()

	a.AddLogMsg("[DICE_SETUP] Dice setup resumed via UI")
	a.emitDiceSetupUpdate()
}

// StopCasinoSetup turns off dice setup and clears any recorded dice.
func (a *App) StopCasinoSetup() {
	// Reset full dice/game state to initial-like values
	resetDiceState()

	mutex.Lock()
	// Ensure setup flag is disabled, casino inactive and known dice cleared
	diceSetupActive = false
	casinoActive = false
	knownDiceIDs = map[int]struct{}{}

	// Clear any awaiting game choice and last partner info so app behaves like fresh start
	awaitingGameChoice = false
	gameChoiceUnreadableWarned = false
	awaitingGameChoicePartnerID = 0
	awaitingGameChoicePartnerName = ""
	lastTradePartnerID = 0
	lastTradePartnerName = ""
	lastTradePartnerToken = ""
	gameBetItems = nil
	lastAddItemWasOurs = false
	lastTradeCoverageNotice = ""
	lastTradeBlockNotice = ""

	stopTradeWindowTimeoutMonitor()
	stopUnderfundedTradeMonitor()
	stopDealerOpenHeartbeat()
	stopPayout()

	// release mutex before calling emitDiceSetupUpdate which locks the same mutex
	mutex.Unlock()

	a.AddLogMsg("[DICE_SETUP] Dice setup stopped and application state cleared via UI")
	a.emitDiceSetupUpdate()
	// Notify dashboard that dealer (casino) is closed using stored name.
	name := a.getCurrentDealerName()
	a.sendLiveDealerStatus(false, name)
	// Clear stored dealer name
	a.currentDealerName = ""
}

// emitDiceSetupUpdate sends the current dice setup state to the frontend.
func (a *App) emitDiceSetupUpdate() {
	mutex.Lock()
	// make a copy of dice values to avoid races
	diceCopy := make([]Dice, len(diceList))
	for i, d := range diceList {
		if d == nil {
			continue
		}
		diceCopy[i] = *d
	}
	complete := len(diceList) >= 5
	ready := casinoReady
	active := casinoActive
	setupActive := diceSetupActive
	mutex.Unlock()

	payload := struct {
		Dice            []Dice `json:"dice"`
		Complete        bool   `json:"complete"`
		Ready           bool   `json:"ready"`
		Active          bool   `json:"active"`
		DiceSetupActive bool   `json:"diceSetupActive"`
	}{
		Dice:            diceCopy,
		Complete:        complete,
		Ready:           ready,
		Active:          active,
		DiceSetupActive: setupActive,
	}
	b, _ := json.Marshal(payload)
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "diceSetupUpdate", string(b))
	}
}

func rememberDiceID(diceID int) {
	if diceID <= 0 {
		return
	}
	knownDiceIDs[diceID] = struct{}{}
}

func (a *App) openDealerAfterSetup(reason string) {
	stopDealerOpenHeartbeat()
	stopTradeWindowTimeoutMonitor()
	stopGameChoiceTimeoutMonitor()
	stopShortageMonitor()
	stopUnderfundedTradeMonitor()
	stopTradeLimitMonitor()

	resetTradeAutoFlow()
	tradeOpen = false
	tradeLimitWasActive = false
	lastTradeLimitNotice = ""
	partnerTradeAccepted = false
	partnerAcceptedSnapshot = nil
	gameBetItems = nil
	a.emitActiveGameBetItemsUpdate()
	a.ClearTradeItems()

	dealerResyncInProgress = true
	requestRoomUsers(a)
	ok := a.forceRefreshHandSnapshot("setup: " + reason)
	dealerResyncInProgress = false
	if shouldRefreshRoomUsers() {
		requestRoomUsers(a)
	}

	if !ok {
		awaitingTradeOpen = false
		dealerAcceptingTrades = false
		dealerTradeWindowOpen = false
		a.AddLogMsg(fmt.Sprintf("[DEALER_SETUP] refusing to announce dealer open because forced hand refresh failed (%s)", reason))
		a.sendLiveDealerStatus(false, a.getCurrentDealerName())
		return
	}

	awaitingTradeOpen = true
	dealerAcceptingTrades = true
	if shouldAnnounceDealerOpen() {
		dealerTradeWindowOpen = true
		openMsg := a.dealerOpenMessage()
		a.AddLogMsg(fmt.Sprintf("[DEALER_SETUP] shouting: %q (%s)", openMsg, reason))
		go sendMessageWithDelay(openMsg)
	} else {
		dealerTradeWindowOpen = false
		log.Printf("[DEALER_SETUP] dealer announcement skipped but trades remain active (%s)", reason)
	}

	startDealerOpenHeartbeat(a)
	a.sendLiveDealerStatus(dealerAcceptingTrades, a.getCurrentDealerName())
}

func (a *App) SkipDiceSetupForTesting() {
	mutex.Lock()
	diceList = make([]*Dice, 0, 5)
	for i := 1; i <= 5; i++ {
		diceList = append(diceList, &Dice{ID: 100000 + i, Value: rand.Intn(6) + 1, IsRolling: false, IsClosed: false})
	}
	fakeDiceTestingMode = true
	casinoReady = true
	mutex.Unlock()

	a.AddLogMsg("Dice setup bypass enabled for testing. Using 5 fake dice values.")
	go a.openDealerAfterSetup("skip dice setup")
}

func (a *App) handleThrowDice(e *g.Intercept) {
	packet := e.Packet
	rawData := string(packet.Data)
	logrus.WithFields(logrus.Fields{"raw_data": rawData}).Debug("Raw packet data")

	diceData := strings.Fields(rawData)
	if len(diceData) == 0 {
		a.AddLogMsg("[DICE_SETUP] ignored THROW_DICE with empty payload")
		return
	}
	diceIDStr := diceData[0]
	diceID, err := strconv.Atoi(diceIDStr)
	if err != nil {
		logrus.WithFields(logrus.Fields{"dice_id_str": diceIDStr, "error": err}).Warn("Failed to parse dice ID")
		return
	}
	rememberDiceID(diceID)
	// Search for a dice with the given ID in the list. Only record new dice
	// when the frontend has explicitly enabled dice setup mode.
	mutex.Lock()
	var existingDice *Dice
	for _, dice := range diceList {
		if dice != nil && dice.ID == diceID {
			existingDice = dice
			break
		}
	}

	needEmit := false
	setupCompletedNow := false

	// If not found and the list has fewer than 5 dice, create and add a new one
	if existingDice == nil && len(diceList) < 5 {
		if !diceSetupActive {
			// Not in setup mode - ignore new dice for setup purposes
			mutex.Unlock()
			return
		}

		newDice := &Dice{ID: diceID, IsRolling: true, IsClosed: false}
		diceList = append(diceList, newDice)
		log.Printf("Dice %d added\n", diceID)
		needEmit = true

		if len(diceList) == 5 {
			message := "Dice setup sucessful! Run :roll to confirm"
			a.AddLogMsg(message)
			// Turn off setup mode once complete and mark the casino ready.
			diceSetupActive = false
			casinoReady = true
			setupCompletedNow = true
		}
	}
	mutex.Unlock()

	if needEmit {
		a.emitDiceSetupUpdate()
	}

	if setupCompletedNow {
		go a.openDealerAfterSetup("dice setup complete")
	}
}

// handle the turning off of a dice
func (a *App) handleDiceOff(e *g.Intercept) {
	packet := e.Packet
	diceIDStr := string(packet.Data)

	diceID, err := strconv.Atoi(diceIDStr)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"dice_id_str": diceIDStr,
			"error":       err,
		}).Warn("Failed to parse dice ID")
		return
	}
	rememberDiceID(diceID)
	// Only record new dice IDs when in setup mode
	mutex.Lock()
	var existingDice *Dice
	for _, dice := range diceList {
		if dice != nil && dice.ID == diceID {
			existingDice = dice
			break
		}
	}

	needEmit := false
	setupCompletedNow := false
	if existingDice == nil && len(diceList) < 5 {
		if !diceSetupActive {
			mutex.Unlock()
			return
		}
		newDice := &Dice{ID: diceID, IsRolling: false, IsClosed: true}
		diceList = append(diceList, newDice)
		log.Printf("Dice %d added\n", diceID)
		needEmit = true
		if len(diceList) == 5 {
			message := "Dice setup sucessful! Run :roll to confirm"
			a.AddLogMsg(message)
			diceSetupActive = false
			casinoReady = true
			setupCompletedNow = true
		}
	}
	mutex.Unlock()

	if needEmit {
		a.emitDiceSetupUpdate()
	}

	if setupCompletedNow {
		go a.openDealerAfterSetup("dice setup complete (dice off)")
	}
}

// Handle the result of a dice roll
func (a *App) handleDiceResult(e *g.Intercept) {
	packet := e.Packet
	rawData := string(packet.Data)
	logrus.WithFields(logrus.Fields{"raw_data": rawData}).Debug("Raw packet data")

	diceData := strings.Fields(rawData)
	if len(diceData) < 2 {
		return
	}

	diceIDStr := diceData[0]
	diceID, err := strconv.Atoi(diceIDStr)
	if err != nil {
		logrus.WithFields(logrus.Fields{"dice_id_str": diceIDStr, "error": err}).Warn("Failed to parse dice ID")
		return
	}
	rememberDiceID(diceID)

	diceValueStr := diceData[1]
	diceValue, err := strconv.Atoi(diceValueStr)
	if err != nil {
		logrus.WithFields(logrus.Fields{"dice_value_str": diceValueStr, "error": err}).Warn("Failed to parse dice value")
		return
	}
	adjustedDiceValue := diceValue - (diceID * 38)

	needEmit := false
	mutex.Lock()
	for i, dice := range diceList {
		if dice.ID == diceID {
			if dice.IsRolling && (isPokerRolling || isTriRolling || isBJRolling || is13Rolling || is13Hitting || isHitting) {
				dice.IsRolling = false
				func() {
					defer func() {
						if r := recover(); r != nil {
							a.AddLogMsg(fmt.Sprintf("[DICE_SYNC_GUARD] recovered from resultsWaitGroup.Done panic for dice %d: %v", diceID, r))
						}
					}()
					resultsWaitGroup.Done()
				}()
			}
			diceList[i].Value = adjustedDiceValue
			diceList[i].IsClosed = diceList[i].Value == 0

			if isPokerRolling || isTriRolling || isBJRolling || is13Rolling || is13Hitting || isHitting {
				log.Printf("Dice %d rolled: %d\n", diceID, adjustedDiceValue)
				logRollResult := fmt.Sprintf("Dice %d rolled: %d\n", diceID, adjustedDiceValue)
				a.AddLogMsg(logRollResult)
			}
			needEmit = true
			break
		}
	}
	mutex.Unlock()

	if needEmit {
		a.emitDiceSetupUpdate()

		// Update readiness: when we have 5 recorded dice with non-zero values
		mutex.Lock()
		ready := len(diceList) >= 5
		if ready {
			for _, d := range diceList {
				if d == nil || d.Value <= 0 {
					ready = false
					break
				}
			}
		}
		casinoReady = ready
		mutex.Unlock()

		if casinoReady {
			a.AddLogMsg("[DICE_SETUP] dice setup complete — casinoReady=true")
		}
	}
}

// Close the dice and send the packets to the game server
func (a *App) closeAllDice() {
	if fakeDiceTestingMode {
		mutex.Lock()
		if len(diceList) < 5 {
			mutex.Unlock()
			log.Println("Not enough dice to roll")
			isClosing = false
			return
		}
		for _, dice := range diceList {
			dice.IsClosed = true
			dice.Value = 0
		}
		isClosing = false
		mutex.Unlock()
		return
	}

	mutex.Lock()
	isClosing = true
	mutex.Unlock()

	for _, dice := range diceList {
		dice.Close()

		// random delay between 550 and 600ms
		time.Sleep(rollDelay + time.Duration(rand.Intn(50))*time.Millisecond)
	}
	mutex.Lock()
	isClosing = false
	mutex.Unlock()
}

// Roll the poker dice by sending packets and waiting for results
func (a *App) rollPokerDice() {
	if fakeDiceTestingMode {
		mutex.Lock()
		if len(diceList) < 5 {
			mutex.Unlock()
			log.Println("Not enough dice to roll")
			isPokerRolling = false
			return
		}
		for i := range diceList {
			diceList[i].Value = rand.Intn(6) + 1
			diceList[i].IsClosed = false
			logRollResult := fmt.Sprintf("Dice %d rolled: %d\n", diceList[i].ID, diceList[i].Value)
			a.AddLogMsg(logRollResult)
		}
		mutex.Unlock()
		a.evaluatePokerHand()
		isPokerRolling = false
		return
	}

	mutex.Lock()

	if len(diceList) < 5 {
		mutex.Unlock()
		log.Println("Not enough dice to roll")
		isPokerRolling = false
		return
	}

	resultsWaitGroup.Add(len(diceList))
	mutex.Unlock()

	for _, dice := range diceList {
		dice.Roll()

		// random delay between 550 and 600ms
		time.Sleep(rollDelay + time.Duration(rand.Intn(100))*time.Millisecond)
	}

	time.Sleep(1000 * time.Millisecond)
	resultsWaitGroup.Wait()
	a.evaluatePokerHand()
	isPokerRolling = false
}

// Evaluate the poker hand and send the result to the chat
func (a *App) rollTriDice() {
	if fakeDiceTestingMode {
		mutex.Lock()
		if len(diceList) < 5 {
			mutex.Unlock()
			log.Println("Not enough dice to roll")
			isTriRolling = false
			return
		}
		currentSum = 0
		for _, index := range []int{0, 2, 4} {
			diceList[index].Value = rand.Intn(6) + 1
			diceList[index].IsClosed = false
			currentSum += diceList[index].Value
			logRollResult := fmt.Sprintf("Dice %d rolled: %d\n", diceList[index].ID, diceList[index].Value)
			a.AddLogMsg(logRollResult)
		}
		mutex.Unlock()
		a.evaluateTriRound()
		isTriRolling = false
		return
	}

	mutex.Lock()

	if len(diceList) < 5 {
		mutex.Unlock()
		log.Println("Not enough dice to roll")
		isTriRolling = false
		return
	}

	resultsWaitGroup.Add(3)
	mutex.Unlock()

	for _, index := range []int{0, 2, 4} {
		diceList[index].Roll()
		time.Sleep(rollDelay + time.Duration(rand.Intn(100))*time.Millisecond)
	}

	time.Sleep(1000 * time.Millisecond)
	resultsWaitGroup.Wait()

	a.evaluateTriRound()
	isTriRolling = false
}

// Roll dice for blackjack-style game
func (a *App) rollBjDice() {
	if fakeDiceTestingMode {
		mutex.Lock()
		if len(diceList) < 5 {
			mutex.Unlock()
			log.Println("Not enough dice to roll")
			isBJRolling = false
			return
		}
		blackjackNextHitIndex = 3
		currentSum = 0
		a.AddLogMsg(fmt.Sprintf("[BJ_DEBUG] turn start actor=%s using initial slots [0 1 2], next hit slot=%d", map[bool]string{true: "player", false: "dealer"}[blackjackPlayerTurn], blackjackNextHitIndex))
		for _, index := range []int{0, 1, 2} {
			diceList[index].Value = rand.Intn(6) + 1
			diceList[index].IsClosed = false
			currentSum += diceList[index].Value
			logRollResult := fmt.Sprintf("Dice %d rolled: %d\n", diceList[index].ID, diceList[index].Value)
			a.AddLogMsg(logRollResult)
			a.AddLogMsg(fmt.Sprintf("[BJ_DEBUG] initial slot=%d diceID=%d value=%d runningSum=%d", index, diceList[index].ID, diceList[index].Value, currentSum))
		}
		a.AddLogMsg(fmt.Sprintf("[BJ_DEBUG] initial 3 complete total=%d (fake mode)", currentSum))
		mutex.Unlock()

		a.evaluateBlackjackHand()
		isBJRolling = false
		return
	}

	mutex.Lock()

	if len(diceList) < 5 {
		mutex.Unlock()
		log.Println("Not enough dice to roll")
		isBJRolling = false
		return
	}

	blackjackNextHitIndex = 3
	currentSum = 0 // Reset sum before starting
	a.AddLogMsg(fmt.Sprintf("[BJ_DEBUG] turn start actor=%s using initial slots [0 1 2], next hit slot=%d", map[bool]string{true: "player", false: "dealer"}[blackjackPlayerTurn], blackjackNextHitIndex))
	resultsWaitGroup.Add(3)
	mutex.Unlock()

	// Roll the first three dice in order
	for _, index := range []int{0, 1, 2} {
		diceList[index].Roll()
		time.Sleep(rollDelay + time.Duration(rand.Intn(100))*time.Millisecond)
	}

	time.Sleep(1000 * time.Millisecond)
	a.waitForBlackjackDiceResults([]int{0, 1, 2}, 6*time.Second, "initial-roll")

	mutex.Lock()
	values := make([]int, 0, 3)
	for _, index := range []int{0, 1, 2} {
		currentSum += diceList[index].Value
		values = append(values, diceList[index].Value)
		a.AddLogMsg(fmt.Sprintf("[BJ_DEBUG] initial resolved slot=%d diceID=%d value=%d runningSum=%d", index, diceList[index].ID, diceList[index].Value, currentSum))
	}
	a.AddLogMsg(fmt.Sprintf("[BJ_DEBUG] initial 3 complete values=%v total=%d", values, currentSum))
	mutex.Unlock()

	a.evaluateBlackjackHand()
	isBJRolling = false
}

func (a *App) hitBjDice() {
	defer func() {
		blackjackHitInFlight = false
		if r := recover(); r != nil {
			a.AddLogMsg(fmt.Sprintf("[BJ_CRASH_GUARD] recovered panic in hitBjDice: %v", r))
			resetBlackjackSequence()
			isBJRolling = false
			isHitting = false
		}
	}()

	if fakeDiceTestingMode {
		mutex.Lock()
		if len(diceList) < 5 {
			mutex.Unlock()
			log.Println("Not enough dice to roll")
			isBJRolling = false
			isHitting = false
			return
		}

		slot := blackjackNextHitIndex
		if slot < 0 || slot >= len(diceList) {
			slot = 0
		}
		blackjackNextHitIndex = (slot + 1) % len(diceList)

		diceList[slot].Value = rand.Intn(6) + 1
		diceList[slot].IsClosed = false
		currentSum += diceList[slot].Value
		logRollResult := fmt.Sprintf("Dice %d rolled: %d\n", diceList[slot].ID, diceList[slot].Value)
		a.AddLogMsg(logRollResult)
		a.AddLogMsg(fmt.Sprintf("[BJ_DEBUG] hit actor=%s slot=%d diceID=%d value=%d newTotal=%d nextSlot=%d", map[bool]string{true: "player", false: "dealer"}[blackjackPlayerTurn], slot, diceList[slot].ID, diceList[slot].Value, currentSum, blackjackNextHitIndex))
		mutex.Unlock()

		blackjackHitInFlight = false
		a.evaluateBlackjackHand()
		isHitting = false
		isBJRolling = false
		return
	}

	mutex.Lock()

	if len(diceList) < 5 {
		mutex.Unlock()
		log.Println("Not enough dice to roll")
		isBJRolling = false
		isHitting = false
		return
	}

	slot := blackjackNextHitIndex
	if slot < 0 || slot >= len(diceList) {
		slot = 0
	}
	nextSlot := (slot + 1) % len(diceList)
	blackjackNextHitIndex = nextSlot
	diceID := diceList[slot].ID
	a.AddLogMsg(fmt.Sprintf("[BJ_DEBUG] hit queued actor=%s slot=%d diceID=%d nextSlot=%d", map[bool]string{true: "player", false: "dealer"}[blackjackPlayerTurn], slot, diceID, nextSlot))

	resultsWaitGroup.Add(1)
	mutex.Unlock()

	diceList[slot].Roll()

	time.Sleep(rollDelay + time.Duration(rand.Intn(100))*time.Millisecond)
	a.waitForBlackjackDiceResults([]int{slot}, 5*time.Second, "hit-roll")
	newValue := diceList[slot].Value

	mutex.Lock()
	currentSum = currentSum + newValue // Adjust current sum
	newTotal := currentSum
	mutex.Unlock()

	// Log the value of the dice rolled
	a.AddLogMsg(fmt.Sprintf("[BJ_DEBUG] hit resolved actor=%s slot=%d diceID=%d value=%d newTotal=%d", map[bool]string{true: "player", false: "dealer"}[blackjackPlayerTurn], slot, diceID, newValue, newTotal))
	// Re-evaluate the hand with the updated sum
	blackjackHitInFlight = false
	a.evaluateBlackjackHand()

	isHitting = false
	isBJRolling = false
}

func (a *App) waitForBlackjackDiceResults(slots []int, timeout time.Duration, reason string) {
	deadline := time.Now().Add(timeout)

	for {
		pending := make([]int, 0, len(slots))

		mutex.Lock()
		for _, slot := range slots {
			if slot < 0 || slot >= len(diceList) {
				continue
			}
			if diceList[slot].IsRolling {
				pending = append(pending, slot)
			}
		}
		mutex.Unlock()

		if len(pending) == 0 {
			a.AddLogMsg(fmt.Sprintf("[BJ_DEBUG] wait complete reason=%s slots=%v", reason, slots))
			return
		}

		if time.Now().After(deadline) {
			a.AddLogMsg(fmt.Sprintf("[BJ_DEBUG] wait timeout reason=%s pendingSlots=%v; forcing unstick", reason, pending))

			mutex.Lock()
			for _, slot := range pending {
				if slot < 0 || slot >= len(diceList) {
					continue
				}
				if !diceList[slot].IsRolling {
					continue
				}
				diceList[slot].IsRolling = false
				diceID := diceList[slot].ID
				diceValue := diceList[slot].Value
				func() {
					defer func() {
						if r := recover(); r != nil {
							a.AddLogMsg(fmt.Sprintf("[BJ_DEBUG] forced unstick Done panic diceID=%d slot=%d: %v", diceID, slot, r))
						}
					}()
					resultsWaitGroup.Done()
				}()
				a.AddLogMsg(fmt.Sprintf("[BJ_DEBUG] forced unstick slot=%d diceID=%d retainedValue=%d", slot, diceID, diceValue))
			}
			mutex.Unlock()
			return
		}

		time.Sleep(75 * time.Millisecond)
	}
}

// Roll dice for blackjack-style game
func (a *App) roll13Dice() {
	if fakeDiceTestingMode {
		mutex.Lock()
		if len(diceList) < 5 {
			mutex.Unlock()
			log.Println("Not enough dice to roll")
			is13Rolling = false
			return
		}
		currentSum = 0
		// start the rotating hit index at slot 2 (next available after initial two)
		thirteenNextHitIndex = 2
		for _, index := range []int{0, 1} {
			diceList[index].Value = rand.Intn(6) + 1
			diceList[index].IsClosed = false
			currentSum += diceList[index].Value
			logRollResult := fmt.Sprintf("Dice %d rolled: %d\n", diceList[index].ID, diceList[index].Value)
			a.AddLogMsg(logRollResult)
		}
		mutex.Unlock()
		a.evaluate13Hand()
		is13Rolling = false
		return
	}

	// Do not close other dice before starting a 13 roll —
	// closing can interfere with subsequent hit rolls. Keep slots available.
	mutex.Lock()

	if len(diceList) < 5 {
		mutex.Unlock()
		log.Println("Not enough dice to roll")
		is13Rolling = false
		return
	}

	currentSum = 0 // Reset sum before starting
	resultsWaitGroup.Add(2)
	// start the rotating hit index at slot 2 (next available after initial two)
	thirteenNextHitIndex = 2
	mutex.Unlock()

	// Roll the first three dice in order
	for _, index := range []int{0, 1} {
		diceList[index].Roll()
		time.Sleep(rollDelay + time.Duration(rand.Intn(100))*time.Millisecond)
	}

	time.Sleep(1000 * time.Millisecond)
	resultsWaitGroup.Wait()

	mutex.Lock()
	for _, index := range []int{0, 1} {
		currentSum += diceList[index].Value
	}
	mutex.Unlock()

	// If this is the player's initial roll and the two-dice total is <= 6,
	// immediately trigger a hit (roll the next dice) so the result is seen
	// without waiting for the external decision prompt.
	// Fall through to evaluation and prompt the player; do not auto-hit.
	a.evaluate13Hand()
	is13Rolling = false
}

func (a *App) hit13Dice() {
	defer func() { thirteenHitInFlight = false }()
	if fakeDiceTestingMode {
		mutex.Lock()
		if len(diceList) < 5 {
			mutex.Unlock()
			log.Println("Not enough dice to roll")
			is13Rolling = false
			is13Hitting = false
			return
		}

		slot := thirteenNextHitIndex
		if slot < 0 || slot >= len(diceList) {
			slot = 0
		}
		thirteenNextHitIndex = (slot + 1) % len(diceList)

		diceList[slot].Value = rand.Intn(6) + 1
		diceList[slot].IsClosed = false
		currentSum += diceList[slot].Value
		logRollResult := fmt.Sprintf("Dice %d rolled: %d\n", diceList[slot].ID, diceList[slot].Value)
		a.AddLogMsg(logRollResult)
		a.AddLogMsg(fmt.Sprintf("[13_DEBUG] hit actor=%s slot=%d diceID=%d value=%d newTotal=%d nextSlot=%d", map[bool]string{true: "player", false: "dealer"}[thirteenPlayerTurn], slot, diceList[slot].ID, diceList[slot].Value, currentSum, thirteenNextHitIndex))
		mutex.Unlock()

		a.evaluate13Hand()
		is13Hitting = false
		is13Rolling = false
		return
	}
	mutex.Lock()

	if len(diceList) < 5 {
		mutex.Unlock()
		log.Println("Not enough dice to roll")
		is13Rolling = false
		is13Hitting = false
		return
	}

	slot := thirteenNextHitIndex
	if slot < 0 || slot >= len(diceList) {
		slot = 0
	}
	nextSlot := (slot + 1) % len(diceList)
	thirteenNextHitIndex = nextSlot
	diceID := diceList[slot].ID
	a.AddLogMsg(fmt.Sprintf("[13_DEBUG] hit queued actor=%s slot=%d diceID=%d nextSlot=%d", map[bool]string{true: "player", false: "dealer"}[thirteenPlayerTurn], slot, diceID, nextSlot))

	resultsWaitGroup.Add(1)
	mutex.Unlock()

	diceList[slot].Roll()

	time.Sleep(rollDelay + time.Duration(rand.Intn(100))*time.Millisecond)
	a.waitForBlackjackDiceResults([]int{slot}, 5*time.Second, "hit-roll")
	newValue := diceList[slot].Value

	mutex.Lock()
	currentSum = currentSum + newValue // Adjust current sum
	newTotal := currentSum
	mutex.Unlock()

	a.AddLogMsg(fmt.Sprintf("[13_DEBUG] hit resolved actor=%s slot=%d diceID=%d value=%d newTotal=%d", map[bool]string{true: "player", false: "dealer"}[thirteenPlayerTurn], slot, diceID, newValue, newTotal))
	// Re-evaluate the hand with the updated sum
	a.evaluate13Hand()

	is13Hitting = false
	is13Rolling = false
}

func verifyResult() {
	// Convert the currentSum to a string
	sumStr := strconv.Itoa(currentSum)
	mutex.Lock()
	ext.Send(out.SHOUT, sumStr)
	mutex.Unlock()
}

func (a *App) ShowCommands() {
	commandList :=
		"Thanks for using my plugin!\nBelow is it's list of commands. \n" +
			"------------------------------------\n" +
			":reset \n" +
			"Forgets dice list for when you\nchange booth.\n" +
			"------------------------------------\n" +
			":roll \n" +
			"Rolls 5 dice and if chat is enabled \nsays the results in chat. \n" +
			"------------------------------------\n" +
			":close\n" +
			"Closes any of your open dice. \n" +
			"------------------------------------\n" +
			":21 \n" +
			"Rolls 3 dice first, then asks the player hit or stay.\n" +
			"Dealer plays automatically. \n" +
			"------------------------------------\n" +
			":13 \n" +
			"Rolls 2 dice first, then asks the player hit or stay.\n" +
			"Dealer plays automatically. \n" +
			"------------------------------------\n" +
			"------------------------------------\n" +
			":tri (quick roll)\n" +
			"Auto rolls 3 dice in Tri Formation \nif chat is enabled says the \nresults in chat. \n" +
			"Use TriH or TriL after a trade to pick Tri High or Tri Low in one shout.\n" +
			"------------------------------------\n" +
			":verify \n" +
			"Will say the previous result in\nchat. Use if you were muted and\ndont know the results of 21/13.\n" +
			"------------------------------------\n" +
			":chaton \n" +
			"Enables chat announcement \nof game results. \n" +
			"------------------------------------\n" +
			":chatoff \n" +
			"Disables chat announcement \nof game results. \n" +
			"------------------------------------\n" +
			":@ <amount> \n" +
			"Stores @ amount in roll log \nwith the result and will announce \nit in chat. \n" +
			"------------------------------------\n" +
			":commands - This help screen :)"

	time.Sleep(time.Duration(rand.Intn(250)+250) * time.Millisecond)
	ext.Send(in.SYSTEM_BROADCAST, commandList)
}

// QDave's Logging function for frontend
func (a *App) AddLogMsg(msg string) {
	a.logMu.Lock()
	defer a.logMu.Unlock()
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	timestampedMsg := fmt.Sprintf("[%s] %s", timestamp, msg)

	a.debugLog = appendCappedLog(a.debugLog, timestampedMsg, 300)
	if shouldShowInUserLog(msg) {
		a.log = appendCappedLog(a.log, timestampedMsg, 150)
	}

	runtime.EventsEmit(a.ctx, "logUpdate", strings.Join(a.log, "\n"))
	runtime.EventsEmit(a.ctx, "debugLogUpdate", strings.Join(a.debugLog, "\n"))
}

func (a *App) AddErrorLog(msg string, err error) {
	if err == nil {
		a.AddLogMsg(msg)
		return
	}
	a.AddLogMsg(fmt.Sprintf("%s: %v", msg, err))
	log.Printf("[ERROR] %s: %v", msg, err)
}

func (a *App) AddDebugLog(format string, args ...interface{}) {
	a.AddLogMsg(fmt.Sprintf(format, args...))
}

func appendCappedLog(lines []string, msg string, limit int) []string {
	lines = append(lines, msg)
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines
}

func shouldShowInUserLog(msg string) bool {
	trimmed := strings.TrimSpace(msg)
	if trimmed == "" {
		return false
	}

	blockedPrefixes := []string{
		"[PAYOUT_DEBUG]",
		"[STRIP_DEBUG]",
		"[TRADE_OPEN_DECODE]",
		"[TRADE_ROOM]",
		"[USERS28]",
		"[USERS28_DEBUG]",
		"[USERS28_FIX]",
		"[ROOM_USERS]",
		"[HEADER_SNIFF]",
		"[TRADE_HEADERS]",
		"[TRADE_ADDITEM #72]",
		"[TRADE_ACCEPT #109]",
		"[TRADE_CONFIRM #111]",
		"[TRADE_LIMIT_DEBUG]",
		"[TRADE_ITEMS_DEBUG]",
		"[TRADE_COVERAGE_DEBUG]",
		"[TRADE_BLOCK_DEBUG]",
		"[TRI_DEBUG]",
		"[GAME_HISTORY]",
		"[PY_PARSE]",
	}
	for _, prefix := range blockedPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return false
		}
	}

	blockedContains := []string{
		"raw[",
		"outgoing payload",
		"derived outgoing[",
		"fallback candidate outgoing[",
		"unresolved reopen target",
		"accepted by partner-name match",
		"accepted by index fallback",
		"accepted \"",
		"ignoring \"",
		"backfilled trade partner name",
	}
	for _, fragment := range blockedContains {
		if strings.Contains(trimmed, fragment) {
			return false
		}
	}

	if strings.HasPrefix(trimmed, "[TRADE_ITEMS #108]") || strings.HasPrefix(trimmed, "[TRADE_OPEN #") || strings.HasPrefix(trimmed, "[TRADE_CLOSE #") {
		return false
	}

	return true
}

func (a *App) AddChatLog(msg string) {
	a.chatLogMu.Lock()
	defer a.chatLogMu.Unlock()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	timestampedMsg := fmt.Sprintf("[%s] %s", timestamp, msg)

	a.chatLog = append(a.chatLog, timestampedMsg)
	if len(a.chatLog) > 100 {
		a.chatLog = a.chatLog[1:]
	}
	runtime.EventsEmit(a.ctx, "chatLogUpdate", strings.Join(a.chatLog, "\n"))
}

// Thanks QDave <3
func (a *App) handleTalk(e *g.Intercept) {
	msg := e.Packet.ReadString()
	if msg == "#gsuite" {
		runtime.WindowShow(a.ctx)
		e.Block()
	}
}

func (a *App) handleIncomingChat(e *g.Intercept) {
	index := e.Packet.ReadInt()
	msg := e.Packet.ReadString()

	chatType := "CHAT"
	if e.Is(in.CHAT_2) {
		chatType = "WHISPER"
	} else if e.Is(in.CHAT_3) {
		chatType = "SHOUT"
	}

	senderName, senderOk := resolveChatSenderName(index)
	if !senderOk && shouldRefreshRoomUsers() {
		go requestRoomUsers(a)
	}
	if !senderOk {
		if waitedName, waitedOk := waitForUsers28IndexName(index, 300*time.Millisecond); waitedOk {
			senderName = waitedName
			senderOk = true
		}
	}

	if senderOk {
		log.Printf("[INCOMING %s] %s(%d) -> %s", chatType, senderName, index, msg)
		a.AddChatLog(fmt.Sprintf("[IN %s] %s(%d) -> %s", chatType, senderName, index, msg))
	} else {
		log.Printf("[INCOMING %s] %d -> %s", chatType, index, msg)
		a.AddChatLog(fmt.Sprintf("[IN %s] %d -> %s", chatType, index, msg))
	}

	if awaitingBlackjackDecision {
		decision, ok := normalizeBlackjackDecision(msg)
		if !ok {
			a.AddLogMsg(fmt.Sprintf("[BJ_DEBUG] awaiting decision from %q(index=%d), ignored non-decision message=%q", awaitingBlackjackDecisionPartnerName, awaitingBlackjackDecisionPartnerID, msg))
			return
		}

		indexMatch := awaitingBlackjackDecisionPartnerID > 0 && index == awaitingBlackjackDecisionPartnerID
		nameMatch := awaitingBlackjackDecisionPartnerName != "" && strings.EqualFold(senderName, awaitingBlackjackDecisionPartnerName)
		if !indexMatch && !nameMatch && awaitingBlackjackDecisionPartnerName != "" {
			if expectedIdx, ok := lookupRoomEntityIndexByName(awaitingBlackjackDecisionPartnerName); ok && expectedIdx > 0 && expectedIdx == index {
				indexMatch = true
			}
		}
		if !indexMatch && !nameMatch && awaitingBlackjackDecisionPartnerName != "" {
			if expectedIdx, ok := lookupUsers28RoomIndexByName(awaitingBlackjackDecisionPartnerName); ok && expectedIdx > 0 && expectedIdx == index {
				indexMatch = true
			}
		}

		if !indexMatch && !nameMatch {
			a.AddLogMsg(fmt.Sprintf("[BJ] ignoring decision %q from %q (index %d); waiting for %q (index %d)", decision, senderName, index, awaitingBlackjackDecisionPartnerName, awaitingBlackjackDecisionPartnerID))
			return
		}

		e.Block()
		awaitingBlackjackDecision = false
		a.AddLogMsg(fmt.Sprintf("[BJ_DEBUG] accepted decision=%q from sender=%q index=%d (expectedName=%q expectedIndex=%d)", decision, senderName, index, awaitingBlackjackDecisionPartnerName, awaitingBlackjackDecisionPartnerID))

		if decision == "hit" {
			a.AddLogMsg("[BJ] player chose hit")
			isHitting = true
			isBJRolling = true
			go a.hitBjDice()
		} else {
			a.AddLogMsg("[BJ] player chose stay")
			a.startBlackjackDealerTurn("player stayed")
		}
		return
	}

	if awaiting13Decision {
		decision, ok := normalizeBlackjackDecision(msg)
		if !ok {
			a.AddLogMsg(fmt.Sprintf("[13_DEBUG] awaiting decision from %q(index=%d), ignored non-decision message=%q", awaiting13DecisionPartnerName, awaiting13DecisionPartnerID, msg))
			return
		}

		indexMatch := awaiting13DecisionPartnerID > 0 && index == awaiting13DecisionPartnerID
		nameMatch := awaiting13DecisionPartnerName != "" && strings.EqualFold(senderName, awaiting13DecisionPartnerName)
		if !indexMatch && !nameMatch && awaiting13DecisionPartnerName != "" {
			if expectedIdx, ok := lookupRoomEntityIndexByName(awaiting13DecisionPartnerName); ok && expectedIdx > 0 && expectedIdx == index {
				indexMatch = true
			}
		}
		if !indexMatch && !nameMatch && awaiting13DecisionPartnerName != "" {
			if expectedIdx, ok := lookupUsers28RoomIndexByName(awaiting13DecisionPartnerName); ok && expectedIdx > 0 && expectedIdx == index {
				indexMatch = true
			}
		}

		if !indexMatch && !nameMatch {
			a.AddLogMsg(fmt.Sprintf("[13] ignoring decision %q from %q (index %d); waiting for %q (index %d)", decision, senderName, index, awaiting13DecisionPartnerName, awaiting13DecisionPartnerID))
			return
		}

		e.Block()
		awaiting13Decision = false
		a.AddLogMsg(fmt.Sprintf("[13_DEBUG] accepted decision=%q from sender=%q index=%d (expectedName=%q expectedIndex=%d)", decision, senderName, index, awaiting13DecisionPartnerName, awaiting13DecisionPartnerID))

		if decision == "hit" {
			a.AddLogMsg("[13] player chose hit")
			is13Hitting = true
			is13Rolling = true
			thirteenHitInFlight = true
			go a.hit13Dice()
		} else {
			a.AddLogMsg("[13] player chose stay")
			a.start13DealerTurn("player stayed")
		}
		return
	}

	if awaitingTriChoice {
		cleaned := strings.ToLower(strings.TrimSpace(msg))
		cleaned = gameChoiceCleanupRe.ReplaceAllString(cleaned, "")
		if cleaned != "high" && cleaned != "low" {
			a.AddLogMsg(fmt.Sprintf("[TRI_DEBUG] awaiting tri choice from %q(index=%d), ignored non-choice message=%q", awaitingTriChoicePartnerName, awaitingTriChoicePartnerID, msg))
			return
		}

		indexMatch := awaitingTriChoicePartnerID > 0 && index == awaitingTriChoicePartnerID
		nameMatch := awaitingTriChoicePartnerName != "" && strings.EqualFold(senderName, awaitingTriChoicePartnerName)
		if !indexMatch && !nameMatch && awaitingTriChoicePartnerName != "" {
			if expectedIdx, ok := lookupRoomEntityIndexByName(awaitingTriChoicePartnerName); ok && expectedIdx > 0 && expectedIdx == index {
				indexMatch = true
			}
		}
		if !indexMatch && !nameMatch && awaitingTriChoicePartnerName != "" {
			if expectedIdx, ok := lookupUsers28RoomIndexByName(awaitingTriChoicePartnerName); ok && expectedIdx > 0 && expectedIdx == index {
				indexMatch = true
			}
		}

		if !indexMatch && !nameMatch {
			a.AddLogMsg(fmt.Sprintf("[TRI] ignoring choice %q from %q (index %d); waiting for %q (index %d)", cleaned, senderName, index, awaitingTriChoicePartnerName, awaitingTriChoicePartnerID))
			return
		}

		e.Block()
		awaitingTriChoice = false
		a.AddLogMsg(fmt.Sprintf("[TRI_DEBUG] accepted choice=%q from sender=%q index=%d (expectedName=%q expectedIndex=%d)", cleaned, senderName, index, awaitingTriChoicePartnerName, awaitingTriChoicePartnerID))

		if cleaned == "high" {
			a.beginTriRound("high")
		} else {
			a.beginTriRound("low")
		}
		return
	}

	if !awaitingGameChoice {
		return
	}

	choice, ok := normalizeIncomingGameChoice(msg)
	if !ok {
		// If the message is still readable despite punctuation/spaces, accept it.
		if looseChoice, looseOK := normalizeLooseGameChoice(msg); looseOK {
			choice = looseChoice
			ok = true
		} else if looksLikeUnreadableGameChoiceAttempt(msg) {
			playerName := strings.TrimSpace(awaitingGameChoicePartnerName)
			if playerName == "" {
				playerName = strings.TrimSpace(lastTradePartnerName)
			}
			if playerName == "" {
				playerName = "Player"
			}

			if !gameChoiceUnreadableWarned {
				gameChoiceUnreadableWarned = true
				warn := fmt.Sprintf("%q Please shout, I can not hear you.", playerName)
				a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] unreadable game choice from %s: %q", playerName, msg))
				ext.Send(out.SHOUT, warn)
			}
		}
		return
	}

	// senderName already resolved above.

	// Accept only if sender matches the locked trade starter.
	indexMatch := awaitingGameChoicePartnerID > 0 && index == awaitingGameChoicePartnerID
	nameMatch := awaitingGameChoicePartnerName != "" && strings.EqualFold(senderName, awaitingGameChoicePartnerName)

	if !indexMatch && !nameMatch && awaitingGameChoicePartnerName != "" {
		if expectedIdx, ok := lookupRoomEntityIndexByName(awaitingGameChoicePartnerName); ok && expectedIdx > 0 && expectedIdx == index {
			indexMatch = true
		}
	}
	if !indexMatch && !nameMatch && awaitingGameChoicePartnerName != "" {
		if expectedIdx, ok := lookupUsers28RoomIndexByName(awaitingGameChoicePartnerName); ok && expectedIdx > 0 && expectedIdx == index {
			indexMatch = true
		}
	}
	if !indexMatch && !nameMatch {
		if mappedName, ok := lookupRoomIdentityByChatIndex(index); ok && awaitingGameChoicePartnerName != "" && strings.EqualFold(strings.TrimSpace(mappedName), awaitingGameChoicePartnerName) {
			nameMatch = true
			senderName = mappedName
		}
	}
	if !indexMatch && !nameMatch {
		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] ignoring %q from %q (index %d); waiting for locked starter %q (index %d trade_id=%d)", choice, senderName, index, awaitingGameChoicePartnerName, awaitingGameChoicePartnerID, tradeStarterTradeID))
		return
	}

	if strings.TrimSpace(senderName) != "" && (strings.TrimSpace(lastTradePartnerName) == "" || strings.EqualFold(strings.TrimSpace(lastTradePartnerName), "Unknown")) {
		lastTradePartnerName = strings.TrimSpace(senderName)
		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] backfilled trade partner name from chat sender: %q", lastTradePartnerName))
	}

	e.Block()
	if isPokerRolling || isTriRolling || isBJRolling || is13Rolling || isHitting || is13Hitting || isClosing {
		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] %s selected but dice are busy", choice))
		return
	}

	// Stop the game choice timeout monitor — player has responded.
	stopGameChoiceTimeoutMonitor()
	awaitingGameChoice = false
	gameChoiceUnreadableWarned = false
	awaitingGameChoicePartnerID = 0
	awaitingGameChoicePartnerName = ""

	// For Tri (two-step selection) we must first ask High or Low
	if choice != "tri" {
		ack := fmt.Sprintf("%s! Lets Play!", gameChoiceDisplay(choice))
		a.setCurrentGameHistoryGame(gameChoiceDisplay(choice))
		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] shouting: %q", ack))
		ext.Send(out.SHOUT, ack)
	} else {
		a.AddLogMsg("[GAME_SELECT] Tri selected; prompting for High/Low instead of immediate Lets Play")
	}

	switch choice {
	case "pkr":
		resetBlackjackSequence()
		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] %d selected Pkr; starting player/dealer poker sequence", index))
		a.beginPokerSequence()
	case "21":
		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] %d selected 21; starting player/dealer 21 sequence", index))
		a.beginBlackjackSequence()
	case "13":
		// Start the 13-game sequence (similar flow to 21)
		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] %d selected 13; starting 13 sequence", index))
		a.begin13Sequence()
	case "tri":
		// Two-step Tri selection: prompt player for High or Low
		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] %d selected Tri; prompting for High/Low", index))
		a.beginTriChoiceSequence()
	case "trihigh":
		// Direct Tri High selection
		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] %d selected TriH; starting round", index))
		a.beginTriRound("high")
	case "trilow":
		// Direct Tri Low selection
		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] %d selected TriL; starting round", index))
		a.beginTriRound("low")
	}
}

func normalizeIncomingGameChoice(msg string) (string, bool) {
	cleaned := strings.ToLower(strings.TrimSpace(msg))
	cleaned = gameChoiceCleanupRe.ReplaceAllString(cleaned, "")
	switch cleaned {
	case "pkr", "poker":
		return "pkr", true
	case "21":
		return "21", true
	case "13":
		return "13", true
	case "tri":
		return "tri", true
	case "trih":
		return "trihigh", true
	case "trihigh":
		return "trihigh", true
	case "tril":
		return "trilow", true
	case "trilow":
		return "trilow", true
	default:
		return "", false
	}
}

func normalizeLooseGameChoice(msg string) (string, bool) {
	cleaned := strings.ToLower(strings.TrimSpace(msg))
	if cleaned == "" {
		return "", false
	}

	// keep only letters and digits
	var compact strings.Builder
	for _, r := range cleaned {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			compact.WriteRune(r)
		}
	}
	value := compact.String()

	switch value {
	case "pkr", "poker":
		return "pkr", true
	case "21":
		return "21", true
	case "13":
		return "13", true
	case "tri":
		return "tri", true
	case "trih":
		return "trihigh", true
	case "trihigh":
		return "trihigh", true
	case "tril":
		return "trilow", true
	case "trilow":
		return "trilow", true
	default:
		return "", false
	}
}

func looksLikeUnreadableGameChoiceAttempt(msg string) bool {
	cleaned := strings.ToLower(strings.TrimSpace(msg))
	if cleaned == "" {
		return false
	}

	// If the normal parser already understands it, do not warn.
	if _, ok := normalizeIncomingGameChoice(cleaned); ok {
		return false
	}

	// If the loose parser understands it, it means the text is still readable enough.
	// Example: "p.o.k.e.r" or "t r i" should still be accepted, not warned.
	if _, ok := normalizeLooseGameChoice(cleaned); ok {
		return false
	}

	// Detect broken whisper-like fragments such as p..k..r or t..i
	hasDots := strings.Contains(cleaned, ".")
	hasGameHints :=
		strings.Contains(cleaned, "p") ||
			strings.Contains(cleaned, "k") ||
			strings.Contains(cleaned, "r") ||
			strings.Contains(cleaned, "t") ||
			strings.Contains(cleaned, "i") ||
			strings.Contains(cleaned, "2") ||
			strings.Contains(cleaned, "1")

	if hasDots && hasGameHints {
		return true
	}

	// Broken short fragments that are clearly attempts but unreadable
	compact := gameChoiceCleanupRe.ReplaceAllString(cleaned, "")
	if compact == "" {
		return false
	}
	if len(compact) <= 4 {
		if strings.ContainsAny(compact, "pkrti") || compact == "2" || compact == "1" {
			return true
		}
	}

	return false
}

func normalizeBlackjackDecision(msg string) (string, bool) {
	cleaned := strings.ToLower(strings.TrimSpace(msg))
	cleaned = gameChoiceCleanupRe.ReplaceAllString(cleaned, "")
	switch cleaned {
	case "hit", "h":
		return "hit", true
	case "stay", "stand", "s":
		return "stay", true
	default:
		return "", false
	}
}

func gameChoiceDisplay(choice string) string {
	switch choice {
	case "pkr":
		return "Pkr"
	case "21":
		return "21"
	case "13":
		return "13"
	case "tri":
		return "Tri"
	case "trih":
		return "TriH"
	case "trihigh":
		return "TriH"
	case "tril":
		return "TriL"
	case "trilow":
		return "TriL"
	default:
		return choice
	}
}

// extractTradeTokenFromPacket returns the 4-byte user token in a TRADE_OPEN
// (header 104) packet. It sits immediately after the leading VL64 room index.
func extractTradeTokenFromPacket(data []byte) string {
	if len(data) < 1 {
		return ""
	}

	// Primary fast-path: token immediately follows leading VL64 room index.
	if vlen := gencoding.VL64DecodeLen(data[0]); vlen > 0 && vlen+4 <= len(data) {
		tok := string(data[vlen : vlen+4])
		if isLikelyToken(tok) {
			return tok
		}
	}

	// Fallback: scan for any printable 4-byte token which is preceded by
	// a VL64 value that ends exactly at the token start. Prefer candidates
	// that are followed by a plausible name marker (letter or brace).
	for tokenStart := 0; tokenStart+4 <= len(data); tokenStart++ {
		cand := string(data[tokenStart : tokenStart+4])
		if !isLikelyToken(cand) {
			continue
		}

		// Look back up to 6 bytes for a VL64 that finishes at tokenStart.
		scanStart := tokenStart - 6
		if scanStart < 0 {
			scanStart = 0
		}
		for startOff := scanStart; startOff < tokenStart; startOff++ {
			vlen := gencoding.VL64DecodeLen(data[startOff])
			if vlen <= 0 || startOff+vlen != tokenStart {
				continue
			}
			v := gencoding.VL64Decode(data[startOff:tokenStart])
			if v <= 0 {
				continue
			}

			// Heuristic: require the byte after the token to look like a name
			// start (letter, brace, bracket or space) when available.
			nameStart := tokenStart + 4
			if nameStart < len(data) {
				b := data[nameStart]
				if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || b == '{' || b == '[' || b == ' ' {
					return cand
				}
				// If next byte is not a strong name marker, still accept the
				// token as a weaker fallback.
				return cand
			}

			return cand
		}
	}

	return ""
}

// lookupTokenByName reverses parsed USERS28 token_hex values to find the token for a username.
func lookupTokenByName(name string) (string, bool) {
	needle := strings.ToLower(strings.TrimSpace(name))
	if needle == "" {
		return "", false
	}
	users28Mu.Lock()
	defer users28Mu.Unlock()
	for token, u := range users28ByToken {
		if strings.ToLower(strings.TrimSpace(u.Username)) == needle {
			return token, true
		}
	}
	return "", false
}
