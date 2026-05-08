package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"mime/multipart"
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

	"github.com/jackc/pgx/v5/pgxpool"
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
	// Under/Over (Over/Under 7) choice state
	awaitingUOChoice            bool
	awaitingUOChoicePartnerID   int
	awaitingUOChoicePartnerName string
	triRoundActive              bool
	triPlayerTurn               bool
	triMode                     string // "high" or "low"
	triPlayerTotal              int
	triDealerTotal              int
	triPlayerName               string
	// Under/Over-7 state
	isUORolling    bool
	uoRoundActive  bool
	uoPlayerChoice string // "over", "under" or "7"
	// When true the dealer has selected the special UnderOver7 game-mode
	// which allows a player to shout "7" for a potential 3x payout.
	underOver7GameModeEnabled bool
	// Payout multiplier selected for the current payout round (2 or 3).
	payoutMultiplierForRound int = 2
	// Variant marker for the active Under/Over round: "uo" or "uo7".
	uoVariantForRound string
	// Pending variant selection when prompting for Over/Under (set by beginUO7ChoiceSequence)
	pendingUoVariant          string
	onlyUnderOver7Mode        bool // when true, dealer prompts only Under/Over-7
	enabledGamePkr            bool = true
	enabledGame21             bool = true
	enabledGame13             bool = true
	enabledGameTri            bool = true
	enabledGameUO7            bool = false
	pokerSequencePlayerName   string
	pokerSequencePlayerResult PokerHandResult
	pokerSequencePlayerHand   string
	payoutActive              bool
	payoutTradeActive         bool
	payoutTargetID            int
	payoutTargetName          string
	payoutAttempts            int
	payoutSessionID           int
	payoutTradeSent           bool
	payoutExpectedAddCount    int
	payoutActualAddCount      int
	lastPayoutCancelNoticeAt  time.Time

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
	lastDealerOpenShoutAt    time.Time
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
	// Risk decision monitor (initial "Keep or Risk" prompt)
	riskDecisionTimeoutMonitorID int
	riskDecisionTimeoutActive    bool
	gameChoiceUnreadableWarned   bool
	dealerResyncInProgress       bool
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
	// Dealer open announcement config
	dealerOpenMu          sync.Mutex
	tradeWindowSeconds    int = 45
	dealerAnnounceSeconds int = 45

	// Block recommended-rooms incoming packet configuration
	blockRecommendedRooms bool = true
	blockRecommendedMu    sync.Mutex

	// Block slide-object-bundle incoming packet configuration
	blockSlideObjectBundle bool = true
	blockSlideObjectMu     sync.Mutex

	// Additional incoming packet block flags
	blockStatusEffects      bool = true
	blockStatusEffectsMu    sync.Mutex
	blockRemoveBuddy        bool = true
	blockRemoveBuddyMu      sync.Mutex
	blockFriendListUpdate   bool = true
	blockFriendListUpdateMu sync.Mutex

	// Favourite room results (incoming header 61) - default disabled
	blockFavouriteRoomResults   bool = false
	blockFavouriteRoomResultsMu sync.Mutex

	// Poll event eligibility outgoing block flag
	blockPollEventEligibilityOutgoing   bool = true
	blockPollEventEligibilityOutgoingMu sync.Mutex

	// Block recommended-rooms outgoing packet configuration
	blockOutgoingRecommendedRooms bool = true
	blockOutgoingRecommendedMu    sync.Mutex

	// Block slide-object-bundle outgoing packet configuration
	blockSlideObjectOutgoing   bool = true
	blockSlideObjectOutgoingMu sync.Mutex

	// New: Incoming ARTICLES_PAGE (681) and CALENDAR_EVENTS (683) block flags
	blockArticlesPage     bool = true
	blockArticlesPageMu   sync.Mutex
	blockCalendarEvents   bool = true
	blockCalendarEventsMu sync.Mutex

	// Block USER_BANNED (incoming header 35)
	blockUserBanned   bool = true
	blockUserBannedMu sync.Mutex

	// Block incoming raw packets with header 4095 (e.g., 0x7f7f 'RB')
	blockIncoming4095   bool = true
	blockIncoming4095Mu sync.Mutex

	// New: Outgoing GET_PAGE_ARTICLES (680), GET_CALENDAR_EVENTS (682), and FRIENDLIST_UPDATE (15)
	blockGetPageArticlesOutgoing     bool = true
	blockGetPageArticlesOutgoingMu   sync.Mutex
	blockGetCalendarEventsOutgoing   bool = true
	blockGetCalendarEventsOutgoingMu sync.Mutex
	blockFriendListUpdateOutgoing    bool = true
	blockFriendListUpdateOutgoingMu  sync.Mutex

	// Incoming trade limits (configured at startup)
	maxTradeUniqueItems     int = 5
	maxTradeQuantityPerItem int = 50

	// Risk system (snapshot + internal tracking)
	isRiskEnabled     bool
	riskInitialized   bool
	riskSnapshotTaken bool
	dealerSnapshotQty int
	dealerRisk        int
	playerRisk        int
	// Dedicated risk snapshot (immutable until explicit refresh)
	riskHandSnapshot      []TradeItem
	riskHandSnapshotReady bool

	// Risk session state
	riskSessionActive bool
	riskSessionGame   string
	riskSessionParams map[string]interface{}
	riskPendingBet    int
	riskPartnerID     int
	riskPartnerName   string
	// Per-risk session payout multiplier (2 or 3). Set when a risk session starts.
	riskSessionPayoutMultiplier int = 2

	// One-shot override for payout auto-add to convert internal bank -> items
	riskPayoutRequired map[string]int
	riskPayoutActive   bool

	// When true, record every intercepted packet as a raw event for auditing
	// and debugging. This writes a minimal per-packet JSON record including
	// header, direction and hex payload. WARNING: this can generate a lot of
	// files; enable only when needed.
	captureAllPackets bool = true

	// Centralized shout worker/queue to avoid flood-control mutes
	shoutQueue         chan string
	shoutWorkerOnce    sync.Once
	shoutSpacing       = 2500 * time.Millisecond
	shoutReplaySpacing = 2500 * time.Millisecond

	autoShoutStopChan chan struct{}
	autoShoutMu       sync.Mutex

	// Auto shout #2 (duplicate slot)
	autoShout2Enabled bool
	autoShout2Phrase  string
	autoShout2Seconds int = 30

	autoShout2StopChan chan struct{}
	autoShout2Mu       sync.Mutex
)

type TradeItem struct {
	Name     string
	Quantity int
	RawData  string // Store raw field for debugging
}

// LiveGameSummary is an anonymized, frontend-friendly summary of a completed
// game. It intentionally does not expose player names — `Winner` is mapped
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
	RiskEnabled        bool              `json:"riskEnabled"`
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
			sendShout(m)
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
	ID          string `json:"id"`
	PlayerName  string `json:"playerName"`
	StartedAt   string `json:"startedAt"`
	UpdatedAt   string `json:"updatedAt"`
	CompletedAt string `json:"completedAt,omitempty"`
	Game        string `json:"game"`
	Winner      string `json:"winner"`
	Status      string `json:"status"`
	Issue       bool   `json:"issue"`
	IssueReason string `json:"issueReason"`
	// Issue metadata for easier triage
	IssueType      string      `json:"issueType,omitempty"`
	IssueOwed      int         `json:"issueOwed,omitempty"`
	IssueOwedItems string      `json:"issueOwedItems,omitempty"`
	PlayerResult   string      `json:"playerResult"`
	DealerResult   string      `json:"dealerResult"`
	BetItems       []TradeItem `json:"betItems"`
	PayoutItems    []TradeItem `json:"payoutItems"`
	Notes          []string    `json:"notes"`
	// New fields to capture player choice and raw shout, plus payout multiplier
	Choice           string `json:"choice,omitempty"`
	ChoiceShout      string `json:"choiceShout,omitempty"`
	PayoutMultiplier int    `json:"payoutMultiplier,omitempty"`
	// Risk session fields
	RiskSession bool `json:"riskSession,omitempty"`
	RiskPending int  `json:"riskPending,omitempty"` // amount currently risked for a re-roll
	RiskBank    int  `json:"riskBank,omitempty"`    // player's internal bank at that moment
	// Explicit decision marker for risk rounds (e.g. "Keep" or "Risk 3")
	RiskDecision string `json:"riskDecision,omitempty"`
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
	ext                   *g.Ext
	assets                embed.FS
	log                   []string
	debugLog              []string
	logMu                 sync.Mutex
	chatLog               []string
	chatLogMu             sync.Mutex
	gameHistory           []GameHistoryEntry
	gameHistoryMu         sync.Mutex
	currentGameHistoryID  string
	historyDB             *pgxpool.Pool
	historyOwnerKey       string
	historyDBMu           sync.Mutex
	historyInitMu         sync.Mutex
	historyPersistMu      sync.Mutex
	historyPersistStateMu sync.Mutex
	historyPersistSignal  chan struct{}
	historyPersistStop    chan struct{}
	historyPersistPending []GameHistoryEntry
	historyPersistRunning bool
	ctx                   context.Context
	currentDealerName     string
	currentRoomName       string
	users28PythonExec     string
	users28ParserScript   string
}

type DBConfig struct {
	DatabaseURL string `json:"databaseUrl"`
	OwnerKey    string `json:"ownerKey"`
}

const fallbackHistoryDBURL = "postgresql://neondb_owner:npg_Jx8ERGzK6eog@ep-small-thunder-a7ceewoj-pooler.ap-southeast-2.aws.neon.tech/neondb?sslmode=require&channel_binding=require"
const fallbackHistoryOwnerKey = "roll-origins"
const historyPersistDebounce = 1200 * time.Millisecond

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

// DealerOpenConfig holds frontend-friendly dealer-open settings.
type DealerOpenConfig struct {
	Enabled         bool `json:"enabled"`
	TradeSeconds    int  `json:"tradeSeconds"`
	AnnounceSeconds int  `json:"announceSeconds"`
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
		// Resolve relative to the executable so the script is always found
		// regardless of working directory (e.g. when launched by app-launcher).
		exePath, _ := os.Executable()
		exeDir := filepath.Dir(exePath)
		scriptCandidates := []string{
			// Primary: exe is at build/bin/ → two levels up = workspace root
			filepath.Join(exeDir, "..", "..", "scripts", "parse_users28.py"),
			// Fallback for go run from workspace root
			filepath.Join("..", "scripts", "parse_users28.py"),
			filepath.Join("scripts", "parse_users28.py"),
		}
		for _, c := range scriptCandidates {
			abs, _ := filepath.Abs(c)
			if _, err := os.Stat(abs); err == nil {
				a.users28ParserScript = abs
				break
			}
		}
		if a.users28ParserScript == "" {
			// Last resort: keep a relative path and let the runner report the error
			a.users28ParserScript = filepath.Join("scripts", "parse_users28.py")
		}
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
	a.startHistoryPersistWorker()
	go func() {
		a.initHistoryDatabase()
		a.loadGameHistory()
	}()
	rand.Seed(time.Now().UnixNano())
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

	// Periodic hand-snapshot sender: keep remote site up-to-date for open dealers.
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			// Skip when no dealer/casino/trade is open.
			if !casinoActive && !dealerAcceptingTrades && !tradeOpen {
				continue
			}

			// Prefer frozen snapshot when available, otherwise use live hand.
			handItemsMu.Lock()
			var items []TradeItem
			if tradeHandSnapshotReady && len(tradeHandSnapshot) > 0 {
				items = make([]TradeItem, len(tradeHandSnapshot))
				copy(items, tradeHandSnapshot)
			} else if len(currentHandItems) > 0 {
				items = make([]TradeItem, len(currentHandItems))
				copy(items, currentHandItems)
			}
			handItemsMu.Unlock()

			if len(items) == 0 {
				continue
			}
			// sendLiveDealerSnapshot is already asynchronous.
			a.sendLiveDealerSnapshot(items)
		}
	}()

}

func (a *App) shutdown(context.Context) {
	a.stopHistoryPersistWorker()
	a.historyDBMu.Lock()
	db := a.historyDB
	a.historyDB = nil
	a.historyDBMu.Unlock()
	if db != nil {
		db.Close()
	}
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

// GetDealerOpenConfig returns the current dealer-open configuration.
func (a *App) GetDealerOpenConfig() DealerOpenConfig {
	dealerOpenMu.Lock()
	defer dealerOpenMu.Unlock()

	return DealerOpenConfig{
		Enabled:         dealerAnnouncementsEnabled,
		TradeSeconds:    tradeWindowSeconds,
		AnnounceSeconds: dealerAnnounceSeconds,
	}
}

// SaveDealerOpenConfig updates dealer-open announcement settings and emits an event.
// SaveDealerOpenConfig updates dealer-open announcement settings and emits an event.
func (a *App) SaveDealerOpenConfig(enabled bool, tradeSeconds int, announceSeconds int) DealerOpenConfig {
	if tradeSeconds < 1 {
		tradeSeconds = 1
	}
	if announceSeconds < 1 {
		announceSeconds = 1
	}

	dealerOpenMu.Lock()
	dealerAnnouncementsEnabled = enabled
	tradeWindowSeconds = tradeSeconds
	dealerAnnounceSeconds = announceSeconds
	// If a trade-window monitor is active, update its deadline.
	if tradeWindowTimeoutActive {
		tradeWindowDeadline = tradeWindowOpenedAt.Add(time.Duration(tradeWindowSeconds) * time.Second)
	}
	cfg := DealerOpenConfig{
		Enabled:         dealerAnnouncementsEnabled,
		TradeSeconds:    tradeWindowSeconds,
		AnnounceSeconds: dealerAnnounceSeconds,
	}
	dealerOpenMu.Unlock()

	if a.ctx != nil {
		b, _ := json.Marshal(cfg)
		runtime.EventsEmit(a.ctx, "dealerOpenUpdate", string(b))
	}

	// Restart heartbeat to apply announce-interval changes immediately.
	if dealerOpenHeartbeatActive {
		stopDealerOpenHeartbeat()
	}
	if dealerAnnouncementsEnabled && awaitingTradeOpen && dealerTradeWindowOpen {
		startDealerOpenHeartbeat(a)
	}

	return cfg

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

// GetBlockArticlesPageConfig returns current block setting for incoming ARTICLES_PAGE (681).
func (a *App) GetBlockArticlesPageConfig() BlockSlideObjectConfig {
	blockArticlesPageMu.Lock()
	defer blockArticlesPageMu.Unlock()
	return BlockSlideObjectConfig{Enabled: blockArticlesPage}
}

// ToggleBlockArticlesPage toggles blocking of incoming ARTICLES_PAGE packets.
func (a *App) ToggleBlockArticlesPage(enabled bool) BlockSlideObjectConfig {
	blockArticlesPageMu.Lock()
	blockArticlesPage = enabled
	blockArticlesPageMu.Unlock()

	cfg := BlockSlideObjectConfig{Enabled: blockArticlesPage}
	if a.ctx != nil {
		b, _ := json.Marshal(cfg)
		runtime.EventsEmit(a.ctx, "blockArticlesPageUpdate", string(b))
	}

	return cfg
}

// GetBlockCalendarEventsConfig returns current block setting for incoming CALENDAR_EVENTS (683).
func (a *App) GetBlockCalendarEventsConfig() BlockSlideObjectConfig {
	blockCalendarEventsMu.Lock()
	defer blockCalendarEventsMu.Unlock()
	return BlockSlideObjectConfig{Enabled: blockCalendarEvents}
}

// ToggleBlockCalendarEvents toggles blocking of incoming CALENDAR_EVENTS packets.
func (a *App) ToggleBlockCalendarEvents(enabled bool) BlockSlideObjectConfig {
	blockCalendarEventsMu.Lock()
	blockCalendarEvents = enabled
	blockCalendarEventsMu.Unlock()

	cfg := BlockSlideObjectConfig{Enabled: blockCalendarEvents}
	if a.ctx != nil {
		b, _ := json.Marshal(cfg)
		runtime.EventsEmit(a.ctx, "blockCalendarEventsUpdate", string(b))
	}

	return cfg
}

// GetBlockFavouriteRoomResultsConfig returns current block setting for incoming FAVOURITEROOMRESULTS (61).
func (a *App) GetBlockFavouriteRoomResultsConfig() BlockSlideObjectConfig {
	blockFavouriteRoomResultsMu.Lock()
	defer blockFavouriteRoomResultsMu.Unlock()
	return BlockSlideObjectConfig{Enabled: blockFavouriteRoomResults}
}

// ToggleBlockFavouriteRoomResults toggles blocking of incoming FAVOURITEROOMRESULTS packets.
func (a *App) ToggleBlockFavouriteRoomResults(enabled bool) BlockSlideObjectConfig {
	blockFavouriteRoomResultsMu.Lock()
	blockFavouriteRoomResults = enabled
	blockFavouriteRoomResultsMu.Unlock()

	cfg := BlockSlideObjectConfig{Enabled: blockFavouriteRoomResults}
	if a.ctx != nil {
		b, _ := json.Marshal(cfg)
		runtime.EventsEmit(a.ctx, "blockFavouriteRoomResultsUpdate", string(b))
	}

	return cfg
}

// GetBlockUserBannedConfig returns current block setting for incoming USER_BANNED (35).
func (a *App) GetBlockUserBannedConfig() BlockSlideObjectConfig {
	blockUserBannedMu.Lock()
	defer blockUserBannedMu.Unlock()
	return BlockSlideObjectConfig{Enabled: blockUserBanned}
}

// ToggleBlockUserBanned toggles blocking of incoming USER_BANNED packets.
func (a *App) ToggleBlockUserBanned(enabled bool) BlockSlideObjectConfig {
	blockUserBannedMu.Lock()
	blockUserBanned = enabled
	blockUserBannedMu.Unlock()

	cfg := BlockSlideObjectConfig{Enabled: blockUserBanned}
	if a.ctx != nil {
		b, _ := json.Marshal(cfg)
		runtime.EventsEmit(a.ctx, "blockUserBannedUpdate", string(b))
	}

	return cfg
}

// GetBlockIncoming4095Config returns current block setting for incoming header 4095.
func (a *App) GetBlockIncoming4095Config() BlockSlideObjectConfig {
	blockIncoming4095Mu.Lock()
	defer blockIncoming4095Mu.Unlock()
	return BlockSlideObjectConfig{Enabled: blockIncoming4095}
}

// ToggleBlockIncoming4095 toggles blocking of incoming header 4095 packets.
func (a *App) ToggleBlockIncoming4095(enabled bool) BlockSlideObjectConfig {
	blockIncoming4095Mu.Lock()
	blockIncoming4095 = enabled
	blockIncoming4095Mu.Unlock()

	cfg := BlockSlideObjectConfig{Enabled: blockIncoming4095}
	if a.ctx != nil {
		b, _ := json.Marshal(cfg)
		runtime.EventsEmit(a.ctx, "blockIncoming4095Update", string(b))
	}

	return cfg
}

// GetBlockGetPageArticlesOutgoingConfig returns current setting for outgoing GET_PAGE_ARTICLES (680).
func (a *App) GetBlockGetPageArticlesOutgoingConfig() BlockSlideObjectConfig {
	blockGetPageArticlesOutgoingMu.Lock()
	defer blockGetPageArticlesOutgoingMu.Unlock()
	return BlockSlideObjectConfig{Enabled: blockGetPageArticlesOutgoing}
}

// ToggleBlockGetPageArticlesOutgoing toggles blocking of outgoing GET_PAGE_ARTICLES requests.
func (a *App) ToggleBlockGetPageArticlesOutgoing(enabled bool) BlockSlideObjectConfig {
	blockGetPageArticlesOutgoingMu.Lock()
	blockGetPageArticlesOutgoing = enabled
	blockGetPageArticlesOutgoingMu.Unlock()

	cfg := BlockSlideObjectConfig{Enabled: blockGetPageArticlesOutgoing}
	if a.ctx != nil {
		b, _ := json.Marshal(cfg)
		runtime.EventsEmit(a.ctx, "blockGetPageArticlesOutgoingUpdate", string(b))
	}

	return cfg
}

// GetBlockGetCalendarEventsOutgoingConfig returns current setting for outgoing GET_CALENDAR_EVENTS (682).
func (a *App) GetBlockGetCalendarEventsOutgoingConfig() BlockSlideObjectConfig {
	blockGetCalendarEventsOutgoingMu.Lock()
	defer blockGetCalendarEventsOutgoingMu.Unlock()
	return BlockSlideObjectConfig{Enabled: blockGetCalendarEventsOutgoing}
}

// ToggleBlockGetCalendarEventsOutgoing toggles blocking of outgoing GET_CALENDAR_EVENTS requests.
func (a *App) ToggleBlockGetCalendarEventsOutgoing(enabled bool) BlockSlideObjectConfig {
	blockGetCalendarEventsOutgoingMu.Lock()
	blockGetCalendarEventsOutgoing = enabled
	blockGetCalendarEventsOutgoingMu.Unlock()

	cfg := BlockSlideObjectConfig{Enabled: blockGetCalendarEventsOutgoing}
	if a.ctx != nil {
		b, _ := json.Marshal(cfg)
		runtime.EventsEmit(a.ctx, "blockGetCalendarEventsOutgoingUpdate", string(b))
	}

	return cfg
}

// GetBlockFriendListUpdateOutgoingConfig returns current setting for outgoing FRIENDLIST_UPDATE (15).
func (a *App) GetBlockFriendListUpdateOutgoingConfig() BlockSlideObjectConfig {
	blockFriendListUpdateOutgoingMu.Lock()
	defer blockFriendListUpdateOutgoingMu.Unlock()
	return BlockSlideObjectConfig{Enabled: blockFriendListUpdateOutgoing}
}

// ToggleBlockFriendListUpdateOutgoing toggles blocking of outgoing FRIENDLIST_UPDATE packets.
func (a *App) ToggleBlockFriendListUpdateOutgoing(enabled bool) BlockSlideObjectConfig {
	blockFriendListUpdateOutgoingMu.Lock()
	blockFriendListUpdateOutgoing = enabled
	blockFriendListUpdateOutgoingMu.Unlock()

	cfg := BlockSlideObjectConfig{Enabled: blockFriendListUpdateOutgoing}
	if a.ctx != nil {
		b, _ := json.Marshal(cfg)
		runtime.EventsEmit(a.ctx, "blockFriendListUpdateOutgoingUpdate", string(b))
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

// SetOnlyUnderOver enables/disables "Only Under/Over-7" dealer mode.
func (a *App) SetOnlyUnderOver(enabled bool) {
	onlyUnderOver7Mode = enabled
	a.AddLogMsg(fmt.Sprintf("[CONFIG] Under/Over-7 only mode = %t", enabled))
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "dealerModeChanged", enabled)
	}
}

// SetUnderOver7Mode enables/disables the special UnderOver7 game-mode
// (allows shouting "7" for a potential 3x payout). This is a dealer-level
// configuration that affects trade coverage checks and predicted payouts.
func (a *App) SetUnderOver7Mode(enabled bool) {
	mutex.Lock()
	underOver7GameModeEnabled = enabled
	mutex.Unlock()

	a.AddLogMsg(fmt.Sprintf("[CONFIG] UnderOver7 game-mode = %t", enabled))

	// When enabling the UO7 dealer mode, always force Risk off and clean up.
	if enabled {
		if a.GetRiskEnabled() {
			a.AddLogMsg("[CONFIG] UnderOver7 enabled - forcing Risk OFF (UnderOver7 forbids Risk)")
			a.SetRiskEnabled(false)
		}
	}

	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "underOver7ModeChanged", enabled)
	}
}

// SetRiskEnabled toggles Risk mode at runtime and resets session state when enabling.
func (a *App) SetRiskEnabled(enabled bool) {
	mutex.Lock()
	prev := isRiskEnabled
	blocked := false

	// Block enabling Risk if UnderOver7 mode is currently active.
	if enabled && underOver7GameModeEnabled {
		enabled = false
		blocked = true
	}

	isRiskEnabled = enabled
	if enabled {
		// fresh session state when enabling
		riskInitialized = false
		riskSnapshotTaken = false
		// clear any dedicated risk snapshot
		riskHandSnapshot = nil
		riskHandSnapshotReady = false
		dealerSnapshotQty = 0
		dealerRisk = 0
		playerRisk = 0
		riskSessionActive = false
		riskSessionGame = ""
		riskSessionParams = nil
		riskPendingBet = 0
		riskPartnerID = 0
		riskPartnerName = ""
		riskPayoutRequired = nil
		riskPayoutActive = false
	}
	mutex.Unlock()

	if blocked {
		a.AddLogMsg("[RISK] enable attempt blocked: UnderOver7 mode active; refusing to enable Risk")
	}

	a.AddLogMsg(fmt.Sprintf("[RISK] enabled=%t", isRiskEnabled))
	if !isRiskEnabled && prev && riskInitialized && riskSessionActive {
		// force finalize if disabling mid-session
		go a.finalizeRiskKeep()
	}

	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "riskEnabledUpdate", isRiskEnabled)
	}
}

func (a *App) GetRiskEnabled() bool {
	mutex.Lock()
	v := isRiskEnabled
	mutex.Unlock()
	return v
}

// runAutoShoutLoop shouts the configured phrase on a jittered interval.
// Each cycle waits the configured duration ±20% to avoid a perfectly
// mechanical cadence that looks bot-like.
func (a *App) runAutoShoutLoop(stopChan chan struct{}, phrase string, seconds int) {
	base := time.Duration(seconds) * time.Second
	a.AddLogMsg(fmt.Sprintf("[AUTO_SHOUT] started: ~every %ds -> %q", seconds, phrase))

	for {
		// ±20% jitter around the base interval
		jitter := time.Duration(rand.Int63n(int64(base/5)*2) - int64(base/5))
		wait := base + jitter
		nextAt := time.Now().Add(wait).Format("15:04:05")
		a.AddLogMsg(fmt.Sprintf("[AUTO_SHOUT] next shout in %ds (at %s)", int(wait.Seconds()), nextAt))
		if a.ctx != nil {
			b, _ := json.Marshal(map[string]interface{}{"slot": 1, "secsAway": int(wait.Seconds()), "nextAt": nextAt})
			runtime.EventsEmit(a.ctx, "autoShoutNextUpdate", string(b))
		}
		select {
		case <-stopChan:
			a.AddLogMsg("[AUTO_SHOUT] stopped")
			if a.ctx != nil {
				b, _ := json.Marshal(map[string]interface{}{"slot": 1, "secsAway": 0, "nextAt": ""})
				runtime.EventsEmit(a.ctx, "autoShoutNextUpdate", string(b))
			}
			return
		case <-time.After(wait):
		}

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

// GetAutoShoutConfig2 returns the current auto-shout configuration for slot 2.
func (a *App) GetAutoShoutConfig2() AutoShoutConfig {
	autoShout2Mu.Lock()
	defer autoShout2Mu.Unlock()

	return AutoShoutConfig{
		Enabled: autoShout2Enabled,
		Phrase:  autoShout2Phrase,
		Seconds: autoShout2Seconds,
	}
}

// SaveAutoShoutConfig2 updates phrase and seconds for slot 2. If auto-shout is
// currently enabled, the loop is restarted to apply interval changes.
func (a *App) SaveAutoShoutConfig2(phrase string, seconds int) AutoShoutConfig {
	autoShout2Mu.Lock()
	autoShout2Phrase = strings.TrimSpace(phrase)
	if seconds < 1 {
		seconds = 1
	}
	autoShout2Seconds = seconds
	wasEnabled := autoShout2Enabled
	autoShout2Mu.Unlock()

	// If enabled, restart loop so interval changes apply immediately.
	if wasEnabled {
		// Toggle off then on to restart
		a.ToggleAutoShout2(false)
		return a.ToggleAutoShout2(true)
	}

	cfg := AutoShoutConfig{
		Enabled: wasEnabled,
		Phrase:  autoShout2Phrase,
		Seconds: autoShout2Seconds,
	}

	if a.ctx != nil {
		b, _ := json.Marshal(cfg)
		runtime.EventsEmit(a.ctx, "autoShoutUpdate2", string(b))
	}

	return cfg
}

// ToggleAutoShout2 enables or disables the auto shout loop for slot 2.
func (a *App) ToggleAutoShout2(enabled bool) AutoShoutConfig {
	autoShout2Mu.Lock()

	autoShout2Enabled = enabled

	if autoShout2StopChan != nil {
		close(autoShout2StopChan)
		autoShout2StopChan = nil
	}

	if enabled {
		autoShout2StopChan = make(chan struct{})
		stopChan := autoShout2StopChan
		phrase := autoShout2Phrase
		seconds := autoShout2Seconds
		if seconds < 1 {
			seconds = 1
			autoShout2Seconds = 1
		}

		go a.runAutoShoutLoop2(stopChan, phrase, seconds)
	}

	cfg := AutoShoutConfig{
		Enabled: autoShout2Enabled,
		Phrase:  autoShout2Phrase,
		Seconds: autoShout2Seconds,
	}
	autoShout2Mu.Unlock()

	if a.ctx != nil {
		b, _ := json.Marshal(cfg)
		runtime.EventsEmit(a.ctx, "autoShoutUpdate2", string(b))
	}

	return cfg
}

// runAutoShoutLoop2 shouts the configured phrase (slot 2) on a jittered interval.
// Each cycle waits the configured duration ±20% to avoid a perfectly
// mechanical cadence that looks bot-like.
func (a *App) runAutoShoutLoop2(stopChan chan struct{}, phrase string, seconds int) {
	base := time.Duration(seconds) * time.Second
	a.AddLogMsg(fmt.Sprintf("[AUTO_SHOUT 2] started: ~every %ds -> %q", seconds, phrase))

	for {
		// ±20% jitter around the base interval
		jitter := time.Duration(rand.Int63n(int64(base/5)*2) - int64(base/5))
		wait := base + jitter
		nextAt := time.Now().Add(wait).Format("15:04:05")
		a.AddLogMsg(fmt.Sprintf("[AUTO_SHOUT 2] next shout in %ds (at %s)", int(wait.Seconds()), nextAt))
		if a.ctx != nil {
			b, _ := json.Marshal(map[string]interface{}{"slot": 2, "secsAway": int(wait.Seconds()), "nextAt": nextAt})
			runtime.EventsEmit(a.ctx, "autoShoutNextUpdate", string(b))
		}
		select {
		case <-stopChan:
			a.AddLogMsg("[AUTO_SHOUT 2] stopped")
			if a.ctx != nil {
				b, _ := json.Marshal(map[string]interface{}{"slot": 2, "secsAway": 0, "nextAt": ""})
				runtime.EventsEmit(a.ctx, "autoShoutNextUpdate", string(b))
			}
			return
		case <-time.After(wait):
		}

		autoShout2Mu.Lock()
		enabled := autoShout2Enabled
		currentPhrase := strings.TrimSpace(autoShout2Phrase)
		autoShout2Mu.Unlock()

		if !enabled || currentPhrase == "" {
			continue
		}

		if ChatIsDisabled {
			a.AddLogMsg("[AUTO_SHOUT 2] skipped because chat is disabled")
			continue
		}

		if isMuted {
			a.AddLogMsg("[AUTO_SHOUT 2] skipped because muted")
			continue
		}

		sendMessageWithDelay(currentPhrase)
	}
}

func (a *App) dealerOpenMessage() string {
	u := maxTradeUniqueItems
	q := maxTradeQuantityPerItem
	if u <= 1 {
		return fmt.Sprintf("You can bet 1 item, max %d per item - see my live hand - rollorigins.club", q)
	}
	return fmt.Sprintf("You can bet up to %d unique items, max %d per item - see my live hand - rollorigins.club", u, q)
}

func dealerGameActive() bool {
	return awaitingGameChoice ||
		awaitingBlackjackDecision ||
		awaiting13Decision ||
		awaitingTriChoice ||
		payoutActive ||
		payoutTradeActive ||
		payoutTradeSent ||
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
	return fakeDiceTestingMode || len(diceList) >= getExpectedDiceCount()
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
	configPath := filepath.Join(configDir, "roll-origins")
	os.MkdirAll(configPath, 0700)
	return filepath.Join(configPath, "poker_display_config.json")
}

func getGameHistoryFilePath() string {
	configDir, _ := os.UserConfigDir()
	configPath := filepath.Join(configDir, "roll-origins")
	os.MkdirAll(configPath, 0700)
	return filepath.Join(configPath, "game_history.json")
}

func getGameHistoryDBSpoolPath() string {
	return filepath.Join(filepath.Dir(getGameHistoryFilePath()), "game_history_db_spool.json")
}

func writeGameHistoryDBSpool(entries []GameHistoryEntry) error {
	if err := os.MkdirAll(filepath.Dir(getGameHistoryDBSpoolPath()), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(getGameHistoryDBSpoolPath(), b, 0600)
}

func readGameHistoryDBSpool() ([]GameHistoryEntry, error) {
	b, err := os.ReadFile(getGameHistoryDBSpoolPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return nil, nil
	}
	var entries []GameHistoryEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func clearGameHistoryDBSpool() error {
	err := os.Remove(getGameHistoryDBSpoolPath())
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (a *App) startHistoryPersistWorker() {
	if a == nil {
		return
	}

	a.historyPersistStateMu.Lock()
	if a.historyPersistRunning {
		a.historyPersistStateMu.Unlock()
		return
	}
	if a.historyPersistSignal == nil {
		a.historyPersistSignal = make(chan struct{}, 1)
	}
	if a.historyPersistStop == nil {
		a.historyPersistStop = make(chan struct{})
	}
	signal := a.historyPersistSignal
	stop := a.historyPersistStop
	a.historyPersistRunning = true
	a.historyPersistStateMu.Unlock()

	go func() {
		// Recover any previous failed persist from local spool on startup.
		a.flushQueuedGameHistoryPersist("startup_recovery")

		var timer *time.Timer
		var timerC <-chan time.Time

		for {
			select {
			case <-signal:
				if timer == nil {
					timer = time.NewTimer(historyPersistDebounce)
				} else {
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					timer.Reset(historyPersistDebounce)
				}
				timerC = timer.C
			case <-timerC:
				a.flushQueuedGameHistoryPersist("debounce")
				timerC = nil
			case <-stop:
				if timer != nil {
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
				}
				a.flushQueuedGameHistoryPersist("shutdown")
				return
			}
		}
	}()
}

func (a *App) stopHistoryPersistWorker() {
	if a == nil {
		return
	}

	a.historyPersistStateMu.Lock()
	stop := a.historyPersistStop
	a.historyPersistRunning = false
	a.historyPersistSignal = nil
	a.historyPersistStop = nil
	a.historyPersistStateMu.Unlock()

	if stop != nil {
		close(stop)
	}
}

func (a *App) queueGameHistoryPersist(entries []GameHistoryEntry, reason string) {
	if a == nil {
		return
	}
	a.startHistoryPersistWorker()

	a.historyPersistStateMu.Lock()
	a.historyPersistPending = cloneGameHistoryEntries(entries)
	signal := a.historyPersistSignal
	a.historyPersistStateMu.Unlock()

	if signal != nil {
		select {
		case signal <- struct{}{}:
		default:
		}
	}

	if strings.TrimSpace(reason) != "" {
		a.AddLogMsg(fmt.Sprintf("[GAME_HISTORY][DB] queued persist (%s) entries=%d", reason, len(entries)))
	}
}

func (a *App) flushQueuedGameHistoryPersist(reason string) {
	if a == nil {
		return
	}

	a.historyPersistStateMu.Lock()
	pending := cloneGameHistoryEntries(a.historyPersistPending)
	if len(pending) > 0 {
		a.historyPersistPending = nil
	}
	a.historyPersistStateMu.Unlock()

	if len(pending) == 0 {
		spoolEntries, err := readGameHistoryDBSpool()
		if err != nil {
			a.AddLogMsg(fmt.Sprintf("[GAME_HISTORY][DB] failed reading spool: %v", err))
			return
		}
		pending = spoolEntries
	}

	if len(pending) == 0 {
		return
	}

	if err := writeGameHistoryDBSpool(pending); err != nil {
		a.AddLogMsg(fmt.Sprintf("[GAME_HISTORY][DB] failed writing spool: %v", err))
		return
	}

	if err := a.persistGameHistoryToDB(pending); err != nil {
		a.AddLogMsg(fmt.Sprintf("[GAME_HISTORY][DB] flush failed (%s): %v", reason, err))
		return
	}

	if err := clearGameHistoryDBSpool(); err != nil {
		a.AddLogMsg(fmt.Sprintf("[GAME_HISTORY][DB] flush succeeded but spool cleanup failed: %v", err))
		return
	}

	a.AddLogMsg(fmt.Sprintf("[GAME_HISTORY][DB] flush ok (%s) entries=%d", reason, len(pending)))
}

func cloneTradeItems(items []TradeItem) []TradeItem {
	copyItems := make([]TradeItem, len(items))
	copy(copyItems, items)
	return copyItems
}

func cloneGameHistoryEntries(entries []GameHistoryEntry) []GameHistoryEntry {
	out := make([]GameHistoryEntry, len(entries))
	for i := range entries {
		out[i] = entries[i]
		out[i].BetItems = cloneTradeItems(entries[i].BetItems)
		out[i].PayoutItems = cloneTradeItems(entries[i].PayoutItems)
		out[i].Notes = append([]string(nil), entries[i].Notes...)
	}
	return out
}

func normalizeGameHistoryWinners(entries []GameHistoryEntry) int {
	modified := 0
	for i := range entries {
		w := strings.TrimSpace(entries[i].Winner)
		if w == "" {
			continue
		}
		if strings.EqualFold(w, "Dealer") {
			continue
		}
		if entries[i].PlayerName != "" && strings.EqualFold(w, entries[i].PlayerName) {
			entries[i].Winner = entries[i].PlayerName
			continue
		}
		entries[i].Winner = "Dealer"
		modified++
	}
	return modified
}

func loadDBConfig() (*DBConfig, error) {
	// Allow full config via environment variable — useful on machines where
	// db.local.json is not present (e.g. a VM or CI that cloned from git).
	if envURL := strings.TrimSpace(os.Getenv("ROLL_ORIGINS_DB_URL")); envURL != "" {
		ownerKey := strings.TrimSpace(os.Getenv("ROLL_ORIGINS_OWNER_KEY"))
		if ownerKey == "" {
			ownerKey = strings.TrimSpace(os.Getenv("TRADE_TRACKER_OWNER_KEY"))
		}
		return &DBConfig{DatabaseURL: envURL, OwnerKey: ownerKey}, nil
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

	if strings.TrimSpace(fallbackHistoryDBURL) != "" {
		return &DBConfig{DatabaseURL: fallbackHistoryDBURL, OwnerKey: fallbackHistoryOwnerKey}, nil
	}

	return nil, fmt.Errorf("db.local.json not found in cwd/exe parent paths")
}

func dbDiagLog(msg string) {
	path := filepath.Join(os.TempDir(), "roll-origins-db.log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "[%s] %s\n", time.Now().Format("15:04:05"), msg)
}

func (a *App) initHistoryDatabase() {
	cwd, _ := os.Getwd()
	exe, _ := os.Executable()
	dbDiagLog(fmt.Sprintf("initHistoryDatabase called cwd=%s exe=%s", cwd, exe))

	cfg, err := loadDBConfig()
	if err != nil {
		msg := fmt.Sprintf("[GAME_HISTORY][DB] config not loaded: %v", err)
		a.AddLogMsg(msg)
		dbDiagLog(msg)
		return
	}
	dbDiagLog(fmt.Sprintf("config loaded, url length=%d owner=%q", len(cfg.DatabaseURL), cfg.OwnerKey))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		msg := fmt.Sprintf("[GAME_HISTORY][DB] pool creation failed: %v", err)
		a.AddLogMsg(msg)
		dbDiagLog(msg)
		return
	}
	if err := db.Ping(ctx); err != nil {
		msg := fmt.Sprintf("[GAME_HISTORY][DB] ping failed: %v", err)
		a.AddLogMsg(msg)
		dbDiagLog(msg)
		db.Close()
		return
	}
	dbDiagLog("ping OK")

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

	a.historyDBMu.Lock()
	a.historyDB = db
	a.historyOwnerKey = owner
	a.historyDBMu.Unlock()

	if err := a.ensureGameHistoryTables(); err != nil {
		msg := fmt.Sprintf("[GAME_HISTORY][DB] migration failed: %v", err)
		a.AddLogMsg(msg)
		dbDiagLog(msg)
		return
	}

	a.AddLogMsg(fmt.Sprintf("[GAME_HISTORY][DB] connected (owner=%s)", owner))
	dbDiagLog(fmt.Sprintf("READY owner=%s", owner))
}

func (a *App) getHistoryDB() (*pgxpool.Pool, string) {
	a.historyDBMu.Lock()
	defer a.historyDBMu.Unlock()
	return a.historyDB, a.historyOwnerKey
}

func (a *App) resetHistoryDB() {
	a.historyDBMu.Lock()
	db := a.historyDB
	a.historyDB = nil
	a.historyOwnerKey = ""
	a.historyDBMu.Unlock()
	if db != nil {
		db.Close()
	}
}

func (a *App) ensureHistoryDatabaseConnected() error {
	if db, _ := a.getHistoryDB(); db != nil {
		return nil
	}

	a.historyInitMu.Lock()
	defer a.historyInitMu.Unlock()

	if db, _ := a.getHistoryDB(); db != nil {
		return nil
	}

	a.initHistoryDatabase()
	if db, _ := a.getHistoryDB(); db != nil {
		return nil
	}

	return fmt.Errorf("history database not connected")
}

func (a *App) ensureGameHistoryTables() error {
	db, _ := a.getHistoryDB()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	queries := []string{
		`CREATE TABLE IF NOT EXISTS game_history_entries (
			id TEXT NOT NULL,
			owner_key TEXT NOT NULL DEFAULT '',
			player_name TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT '',
			completed_at TEXT NOT NULL DEFAULT '',
			game TEXT NOT NULL DEFAULT '',
			winner TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			issue BOOLEAN NOT NULL DEFAULT FALSE,
			issue_reason TEXT NOT NULL DEFAULT '',
			player_result TEXT NOT NULL DEFAULT '',
			dealer_result TEXT NOT NULL DEFAULT '',
			notes JSONB NOT NULL DEFAULT '[]'::jsonb,
			choice TEXT NOT NULL DEFAULT '',
			choice_shout TEXT NOT NULL DEFAULT '',
			payout_multiplier INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_db_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (id, owner_key)
		)`,
		`CREATE TABLE IF NOT EXISTS game_history_items (
			entry_id TEXT NOT NULL,
			owner_key TEXT NOT NULL DEFAULT '',
			item_type TEXT NOT NULL,
			item_index INTEGER NOT NULL,
			item_name TEXT NOT NULL DEFAULT '',
			quantity INTEGER NOT NULL DEFAULT 1,
			raw_data TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (entry_id, owner_key, item_type, item_index),
			FOREIGN KEY (entry_id, owner_key)
				REFERENCES game_history_entries(id, owner_key)
				ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_game_history_owner_started ON game_history_entries(owner_key, started_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_game_history_items_owner_entry ON game_history_items(owner_key, entry_id, item_type, item_index)`,
	}

	for _, q := range queries {
		if _, err := db.Exec(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) loadGameHistoryFromDB() ([]GameHistoryEntry, error) {
	db, owner := a.getHistoryDB()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	rows, err := db.Query(ctx, `
		SELECT
			id,
			player_name,
			started_at,
			updated_at,
			completed_at,
			game,
			winner,
			status,
			issue,
			issue_reason,
			player_result,
			dealer_result,
			notes,
			choice,
			choice_shout,
			payout_multiplier
		FROM game_history_entries
		WHERE owner_key = $1
		ORDER BY started_at DESC, id DESC
	`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]GameHistoryEntry, 0)
	for rows.Next() {
		var e GameHistoryEntry
		var notesRaw []byte
		if err := rows.Scan(
			&e.ID,
			&e.PlayerName,
			&e.StartedAt,
			&e.UpdatedAt,
			&e.CompletedAt,
			&e.Game,
			&e.Winner,
			&e.Status,
			&e.Issue,
			&e.IssueReason,
			&e.PlayerResult,
			&e.DealerResult,
			&notesRaw,
			&e.Choice,
			&e.ChoiceShout,
			&e.PayoutMultiplier,
		); err != nil {
			return nil, err
		}
		if len(notesRaw) > 0 {
			_ = json.Unmarshal(notesRaw, &e.Notes)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	itemRows, err := db.Query(ctx, `
		SELECT entry_id, item_type, item_name, quantity, raw_data
		FROM game_history_items
		WHERE owner_key = $1
		ORDER BY entry_id, item_type, item_index
	`, owner)
	if err != nil {
		return nil, err
	}
	defer itemRows.Close()

	entryByID := make(map[string]*GameHistoryEntry, len(entries))
	for i := range entries {
		entryByID[entries[i].ID] = &entries[i]
	}

	for itemRows.Next() {
		var entryID string
		var itemType string
		var item TradeItem
		if err := itemRows.Scan(&entryID, &itemType, &item.Name, &item.Quantity, &item.RawData); err != nil {
			return nil, err
		}
		e := entryByID[entryID]
		if e == nil {
			continue
		}
		if strings.EqualFold(itemType, "bet") {
			e.BetItems = append(e.BetItems, item)
		} else {
			e.PayoutItems = append(e.PayoutItems, item)
		}
	}
	if err := itemRows.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

func (a *App) persistGameHistoryToDB(entries []GameHistoryEntry) error {
	a.historyPersistMu.Lock()
	defer a.historyPersistMu.Unlock()

	for attempt := 1; attempt <= 2; attempt++ {
		if err := a.ensureHistoryDatabaseConnected(); err != nil {
			dbDiagLog(fmt.Sprintf("persistGameHistoryToDB: db connect failed attempt=%d err=%v", attempt, err))
			if attempt == 2 {
				return err
			}
			continue
		}

		db, owner := a.getHistoryDB()
		if db == nil {
			if attempt == 2 {
				return fmt.Errorf("database not initialized")
			}
			continue
		}

		dbDiagLog(fmt.Sprintf("persistGameHistoryToDB: owner=%s entries=%d attempt=%d", owner, len(entries), attempt))

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		err := func() error {
			defer cancel()

			tx, err := db.Begin(ctx)
			if err != nil {
				return err
			}
			defer func() {
				_ = tx.Rollback(ctx)
			}()

			for _, e := range entries {
				notesJSON, _ := json.Marshal(e.Notes)
				if _, err := tx.Exec(ctx, `
					INSERT INTO game_history_entries (
						id, owner_key, player_name, started_at, updated_at, completed_at,
						game, winner, status, issue, issue_reason, player_result, dealer_result,
						notes, choice, choice_shout, payout_multiplier, updated_db_at
					) VALUES (
						$1,$2,$3,$4,$5,$6,
						$7,$8,$9,$10,$11,$12,$13,
						$14,$15,$16,$17,NOW()
					)
					ON CONFLICT (id, owner_key) DO UPDATE SET
						player_name = EXCLUDED.player_name,
						started_at = EXCLUDED.started_at,
						updated_at = EXCLUDED.updated_at,
						completed_at = EXCLUDED.completed_at,
						game = EXCLUDED.game,
						winner = EXCLUDED.winner,
						status = EXCLUDED.status,
						issue = EXCLUDED.issue,
						issue_reason = EXCLUDED.issue_reason,
						player_result = EXCLUDED.player_result,
						dealer_result = EXCLUDED.dealer_result,
						notes = EXCLUDED.notes,
						choice = EXCLUDED.choice,
						choice_shout = EXCLUDED.choice_shout,
						payout_multiplier = EXCLUDED.payout_multiplier,
						updated_db_at = NOW()
				`,
					e.ID,
					owner,
					e.PlayerName,
					e.StartedAt,
					e.UpdatedAt,
					e.CompletedAt,
					e.Game,
					e.Winner,
					e.Status,
					e.Issue,
					e.IssueReason,
					e.PlayerResult,
					e.DealerResult,
					notesJSON,
					e.Choice,
					e.ChoiceShout,
					e.PayoutMultiplier,
				); err != nil {
					return err
				}

				if _, err := tx.Exec(ctx, `DELETE FROM game_history_items WHERE entry_id = $1 AND owner_key = $2`, e.ID, owner); err != nil {
					return err
				}

				for i, item := range e.BetItems {
					qty := item.Quantity
					if qty <= 0 {
						qty = 1
					}
					if _, err := tx.Exec(ctx, `
						INSERT INTO game_history_items (entry_id, owner_key, item_type, item_index, item_name, quantity, raw_data)
						VALUES ($1,$2,'bet',$3,$4,$5,$6)
					`, e.ID, owner, i, item.Name, qty, item.RawData); err != nil {
						return err
					}
				}

				for i, item := range e.PayoutItems {
					qty := item.Quantity
					if qty <= 0 {
						qty = 1
					}
					if _, err := tx.Exec(ctx, `
						INSERT INTO game_history_items (entry_id, owner_key, item_type, item_index, item_name, quantity, raw_data)
						VALUES ($1,$2,'payout',$3,$4,$5,$6)
					`, e.ID, owner, i, item.Name, qty, item.RawData); err != nil {
						return err
					}
				}
			}

			if err := tx.Commit(ctx); err != nil {
				return err
			}
			return nil
		}()

		if err == nil {
			return nil
		}

		a.AddLogMsg(fmt.Sprintf("[GAME_HISTORY][DB] persist attempt %d failed: %v", attempt, err))
		if strings.Contains(strings.ToLower(err.Error()), "closed pool") {
			a.resetHistoryDB()
		}
	}

	return fmt.Errorf("game history persist failed after retry")
}

func (a *App) persistSingleGameEntryToDB(entry GameHistoryEntry) error {
	a.historyPersistMu.Lock()
	defer a.historyPersistMu.Unlock()

	for attempt := 1; attempt <= 2; attempt++ {
		if err := a.ensureHistoryDatabaseConnected(); err != nil {
			dbDiagLog(fmt.Sprintf("persistSingleGameEntryToDB: db connect failed attempt=%d err=%v", attempt, err))
			if attempt == 2 {
				return err
			}
			continue
		}

		db, owner := a.getHistoryDB()
		if db == nil {
			if attempt == 2 {
				return fmt.Errorf("database not initialized")
			}
			continue
		}

		dbDiagLog(fmt.Sprintf("persistSingleGameEntryToDB: owner=%s entry_id=%s attempt=%d", owner, entry.ID, attempt))

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		err := func() error {
			defer cancel()

			tx, err := db.Begin(ctx)
			if err != nil {
				return err
			}
			defer func() {
				_ = tx.Rollback(ctx)
			}()

			// Insert/update single entry only, no cleanup DELETE
			notesJSON, _ := json.Marshal(entry.Notes)
			if _, err := tx.Exec(ctx, `
				INSERT INTO game_history_entries (
					id, owner_key, player_name, started_at, updated_at, completed_at,
					game, winner, status, issue, issue_reason, player_result, dealer_result,
					notes, choice, choice_shout, payout_multiplier, updated_db_at
				) VALUES (
					$1,$2,$3,$4,$5,$6,
					$7,$8,$9,$10,$11,$12,$13,
					$14,$15,$16,$17,NOW()
				)
				ON CONFLICT (id, owner_key) DO UPDATE SET
					player_name = EXCLUDED.player_name,
					started_at = EXCLUDED.started_at,
					updated_at = EXCLUDED.updated_at,
					completed_at = EXCLUDED.completed_at,
					game = EXCLUDED.game,
					winner = EXCLUDED.winner,
					status = EXCLUDED.status,
					issue = EXCLUDED.issue,
					issue_reason = EXCLUDED.issue_reason,
					player_result = EXCLUDED.player_result,
					dealer_result = EXCLUDED.dealer_result,
					notes = EXCLUDED.notes,
					choice = EXCLUDED.choice,
					choice_shout = EXCLUDED.choice_shout,
					payout_multiplier = EXCLUDED.payout_multiplier,
					updated_db_at = NOW()
			`,
				entry.ID,
				owner,
				entry.PlayerName,
				entry.StartedAt,
				entry.UpdatedAt,
				entry.CompletedAt,
				entry.Game,
				entry.Winner,
				entry.Status,
				entry.Issue,
				entry.IssueReason,
				entry.PlayerResult,
				entry.DealerResult,
				notesJSON,
				entry.Choice,
				entry.ChoiceShout,
				entry.PayoutMultiplier,
			); err != nil {
				return err
			}

			// Update items for this entry (delete old, insert new)
			if _, err := tx.Exec(ctx, `DELETE FROM game_history_items WHERE entry_id = $1 AND owner_key = $2`, entry.ID, owner); err != nil {
				return err
			}

			for i, item := range entry.BetItems {
				qty := item.Quantity
				if qty <= 0 {
					qty = 1
				}
				if _, err := tx.Exec(ctx, `
					INSERT INTO game_history_items (entry_id, owner_key, item_type, item_index, item_name, quantity, raw_data)
					VALUES ($1,$2,'bet',$3,$4,$5,$6)
				`, entry.ID, owner, i, item.Name, qty, item.RawData); err != nil {
					return err
				}
			}

			for i, item := range entry.PayoutItems {
				qty := item.Quantity
				if qty <= 0 {
					qty = 1
				}
				if _, err := tx.Exec(ctx, `
					INSERT INTO game_history_items (entry_id, owner_key, item_type, item_index, item_name, quantity, raw_data)
					VALUES ($1,$2,'payout',$3,$4,$5,$6)
				`, entry.ID, owner, i, item.Name, qty, item.RawData); err != nil {
					return err
				}
			}

			if err := tx.Commit(ctx); err != nil {
				return err
			}
			return nil
		}()

		if err == nil {
			return nil
		}

		a.AddLogMsg(fmt.Sprintf("[GAME_HISTORY][DB] persist single entry attempt %d failed: %v", attempt, err))
		if strings.Contains(strings.ToLower(err.Error()), "closed pool") {
			a.resetHistoryDB()
		}
	}

	return fmt.Errorf("single entry persist failed after retry")
}

func (a *App) persistCurrentGameHistoryNow(reason string) {
	a.gameHistoryMu.Lock()
	current, ok := a.currentGameHistoryEntryForPersistLocked()
	if !ok {
		a.gameHistoryMu.Unlock()
		return
	}
	a.gameHistoryMu.Unlock()

	// Persist just the current entry directly (like Discord webhook gets just the one entry)
	go func() {
		if err := a.persistSingleGameEntryToDB(current); err != nil {
			a.AddLogMsg(fmt.Sprintf("[GAME_HISTORY][DB] persist now (%s) failed: %v", reason, err))
		} else {
			a.AddLogMsg(fmt.Sprintf("[GAME_HISTORY][DB] persist now (%s) ok", reason))
		}
	}()
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
	if entries, err := a.loadGameHistoryFromDB(); err == nil && len(entries) > 0 {
		modified := normalizeGameHistoryWinners(entries)
		a.gameHistoryMu.Lock()
		a.gameHistory = entries
		if modified > 0 {
			a.AddLogMsg(fmt.Sprintf("[GAME_HISTORY] normalized %d winner fields to 'Dealer'", modified))
			a.saveGameHistoryLocked()
		}
		a.gameHistoryMu.Unlock()
		a.emitGameHistoryUpdate()
		a.AddLogMsg("[GAME_HISTORY][DB] loaded game history from database")
		return
	} else if err != nil {
		a.AddLogMsg(fmt.Sprintf("[GAME_HISTORY][DB] load failed, using file fallback: %v", err))
	}

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
	modified := normalizeGameHistoryWinners(entries)

	a.gameHistoryMu.Lock()
	a.gameHistory = entries
	if modified > 0 {
		a.AddLogMsg(fmt.Sprintf("[GAME_HISTORY] normalized %d winner fields to 'Dealer'", modified))
		// Persist migrated history back to disk while we hold the lock.
		a.saveGameHistoryLocked()
	}
	a.gameHistoryMu.Unlock()
	if err := a.persistGameHistoryToDB(cloneGameHistoryEntries(entries)); err == nil && len(entries) > 0 {
		a.AddLogMsg("[GAME_HISTORY][DB] migrated file history into database")
	}
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
	go func() {
		if err := a.persistGameHistoryToDB(nil); err != nil {
			a.AddLogMsg(fmt.Sprintf("[GAME_HISTORY][DB] clear failed: %v", err))
		}
	}()
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

	// Take a snapshot without holding the mutex during heavy work.
	a.gameHistoryMu.Lock()
	snapshot := cloneGameHistoryEntries(a.gameHistory)
	jsonData, err := json.MarshalIndent(snapshot, "", "  ")
	a.gameHistoryMu.Unlock()

	// Write to disk synchronously — fast, local operation.
	if err == nil {
		_ = os.WriteFile(getGameHistoryFilePath(), jsonData, 0600)
	}

	// Queue DB persistence through a single debounced worker to avoid
	// spawning many heavy full-history upserts during a busy round.
	a.queueGameHistoryPersist(snapshot, "sync")

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

func (a *App) syncCurrentGameEntry() {
	a.AddLogMsg("[GAME_HISTORY] syncCurrentGameEntry start")

	// Only persist the current entry being modified, not the entire history.
	// This prevents expensive full-history upserts during busy gameplay.
	a.gameHistoryMu.Lock()
	current, ok := a.currentGameHistoryEntryForPersistLocked()
	if !ok {
		a.gameHistoryMu.Unlock()
		return
	}
	a.gameHistoryMu.Unlock()

	// Persist directly without the cleanup DELETE that would wipe other entries
	go func() {
		if err := a.persistSingleGameEntryToDB(current); err != nil {
			a.AddLogMsg(fmt.Sprintf("[GAME_HISTORY][DB] sync single entry failed: %v", err))
		} else {
			a.AddLogMsg("[GAME_HISTORY][DB] sync single entry ok")
		}
	}()
}

// currentGameHistoryEntryForPersistLocked returns the active game-history entry
// for persistence. Caller must hold a.gameHistoryMu.
func (a *App) currentGameHistoryEntryForPersistLocked() (GameHistoryEntry, bool) {
	if len(a.gameHistory) == 0 {
		return GameHistoryEntry{}, false
	}

	if idx := a.findCurrentGameHistoryIndexLocked(); idx >= 0 && idx < len(a.gameHistory) {
		return a.gameHistory[idx], true
	}

	// Fallback: newest entry is stored at the front.
	return a.gameHistory[0], true
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
	var replacedEntry *GameHistoryEntry
	// Snapshot current risk state so replaced/Issue entries include risk metadata.
	mutex.Lock()
	rp := riskPendingBet
	rb := playerRisk
	rs := riskSessionActive
	mutex.Unlock()

	if a.updateCurrentGameHistoryLocked(func(entry *GameHistoryEntry) {
		if entry.CompletedAt == "" {
			// Ensure any active risk data is recorded on the archived entry.
			entry.RiskSession = rs
			entry.RiskBank = rb
			entry.RiskPending = rp
			entry.Status = "Issue"
			entry.Issue = true
			entry.IssueReason = "Round was replaced before it fully finished"
			entry.CompletedAt = gameHistoryTimestamp()
			entry.Notes = append(entry.Notes, "New round started before previous round was fully resolved")
			e := *entry
			replacedEntry = &e
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
	if replacedEntry != nil {
		go a.sendDiscordWebhookForGame(*replacedEntry)
		a.persistCurrentGameHistoryNow("game_issue_replaced")
		a.sendLiveDealerGames(5)
		go LogEvent("game_issue", *replacedEntry, "Game marked issue: round replaced before full resolution", map[string]string{"player": replacedEntry.PlayerName})
	}
	// Persist game-begin record
	go LogEvent("game_begin", entry, "Game started", map[string]string{"player": entry.PlayerName})
	a.syncCurrentGameEntry()
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
	a.syncCurrentGameEntry()
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
	a.syncCurrentGameEntry()
}

// setCurrentGameHistoryChoice records the normalized choice and the raw shout
func (a *App) setCurrentGameHistoryChoice(choice string, shout string) {
	a.AddLogMsg("[GAME_HISTORY] setCurrentGameHistoryChoice start")
	a.gameHistoryMu.Lock()
	if !a.updateCurrentGameHistoryLocked(func(entry *GameHistoryEntry) {
		if strings.TrimSpace(choice) != "" {
			entry.Choice = choice
		}
		if strings.TrimSpace(shout) != "" {
			entry.ChoiceShout = shout
			entry.Notes = append(entry.Notes, fmt.Sprintf("Player shouted: %q", shout))
		}
	}) {
		a.gameHistoryMu.Unlock()
		return
	}
	a.gameHistoryMu.Unlock()
	a.AddLogMsg("[GAME_HISTORY] setCurrentGameHistoryChoice unlocked, syncing")
	a.syncCurrentGameEntry()
}

// setCurrentGameHistoryPayoutMultiplier stores the payout multiplier for the current entry
func (a *App) setCurrentGameHistoryPayoutMultiplier(mult int) {
	a.AddLogMsg("[GAME_HISTORY] setCurrentGameHistoryPayoutMultiplier start")
	a.gameHistoryMu.Lock()
	if !a.updateCurrentGameHistoryLocked(func(entry *GameHistoryEntry) {
		if mult > 0 {
			entry.PayoutMultiplier = mult
			entry.Notes = append(entry.Notes, fmt.Sprintf("Payout multiplier: %dx", mult))
		}
	}) {
		a.gameHistoryMu.Unlock()
		return
	}
	a.gameHistoryMu.Unlock()
	a.AddLogMsg("[GAME_HISTORY] setCurrentGameHistoryPayoutMultiplier unlocked, syncing")
	a.syncCurrentGameEntry()
}

func (a *App) setCurrentGameHistoryResults(playerResult string, dealerResult string, winner string, status string, complete bool) {
	a.AddLogMsg("[GAME_HISTORY] setCurrentGameHistoryResults start")
	a.gameHistoryMu.Lock()
	var completedEntry GameHistoryEntry
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
			// copy out the completed entry for persistent logging
			completedEntry = *entry
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
	a.syncCurrentGameEntry()
	if complete {
		// Notify Discord webhook (if configured)
		go a.sendDiscordWebhookForGame(completedEntry)
		// Fan out non-critical tasks after Discord enqueue so DB latency never
		// delays posting completed rounds to Discord.
		a.persistCurrentGameHistoryNow("game_complete")
		a.sendLiveDealerGames(5)
		// Persist completed game record for later review
		go LogEvent("game_complete", completedEntry, "Game completed", map[string]string{"player": completedEntry.PlayerName})
	}
}

func (a *App) markCurrentGameHistoryIssue(reason string, complete bool) {
	a.AddLogMsg("[GAME_HISTORY] markCurrentGameHistoryIssue start")
	a.gameHistoryMu.Lock()
	var completedEntry *GameHistoryEntry
	// Snapshot current risk state so Issue entries include risk metadata when closed.
	mutex.Lock()
	rp := riskPendingBet
	rb := playerRisk
	rs := riskSessionActive
	mutex.Unlock()

	if !a.updateCurrentGameHistoryLocked(func(entry *GameHistoryEntry) {
		entry.Issue = true
		entry.IssueReason = reason
		entry.Status = "Issue"
		entry.Notes = append(entry.Notes, reason)
		entry.RiskSession = rs
		entry.RiskBank = rb
		entry.RiskPending = rp
		if complete {
			entry.CompletedAt = gameHistoryTimestamp()
			e := *entry
			completedEntry = &e
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
	a.syncCurrentGameEntry()
	if complete && completedEntry != nil {
		go a.sendDiscordWebhookForGame(*completedEntry)
		a.persistCurrentGameHistoryNow("game_issue")
		a.sendLiveDealerGames(5)
		go LogEvent("game_issue", *completedEntry, "Game completed with issue", map[string]string{"player": completedEntry.PlayerName})
	}
}

func (a *App) captureCurrentGameHistoryPayoutItems(items []TradeItem, note string, complete bool, isPayout bool) {
	a.AddLogMsg("[GAME_HISTORY] captureCurrentGameHistoryPayoutItems start")
	a.gameHistoryMu.Lock()
	var completedEntry *GameHistoryEntry
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
				inferred = append(inferred, TradeItem{Name: b.Name, Quantity: b.Quantity * payoutMultiplierForRound, RawData: b.RawData})
			}
			entry.PayoutItems = inferred
			if len(inferred) > 0 {
				entry.Notes = append(entry.Notes, fmt.Sprintf("Predicted payout (%dx bet)", payoutMultiplierForRound))
			}
		} else {
			entry.PayoutItems = cloneTradeItems(items)
		}

		if strings.TrimSpace(note) != "" {
			entry.Notes = append(entry.Notes, note)
		}
		if complete {
			// For normal game completion, mark the entry Completed so the
			// game webhook reflects the game result. For payout-only
			// completions, do NOT flip the entry Status/CompletedAt here so
			// the game remains a separate historical record; instead send
			// a dedicated payout webhook below.
			if !isPayout {
				entry.Status = "Completed"
				entry.CompletedAt = gameHistoryTimestamp()
			}
			// copy out the completed snapshot for async webhook/send
			e := *entry
			completedEntry = &e
		}
	}) {
		a.gameHistoryMu.Unlock()
		return
	}
	if complete && !isPayout {
		a.currentGameHistoryID = ""
	}
	a.AddLogMsg("[GAME_HISTORY] captureCurrentGameHistoryPayoutItems mutation complete")
	a.gameHistoryMu.Unlock()
	a.AddLogMsg("[GAME_HISTORY] captureCurrentGameHistoryPayoutItems unlocked, syncing")
	a.syncCurrentGameEntry()
	// If this call marked the entry complete, send the appropriate webhook.
	if complete && completedEntry != nil {
		if isPayout {
			go a.sendDiscordWebhookForPayout(*completedEntry)
		} else {
			go a.sendDiscordWebhookForGame(*completedEntry)
		}
	}
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
		if captureAllPackets {
			dir := "in"
			if e.Packet.Header.Dir == g.Out {
				dir = "out"
			}
			go LogEvent("packet_raw", map[string]interface{}{
				"header":      e.Packet.Header.Value,
				"dir":         dir,
				"len":         len(e.Packet.Data),
				"payload_hex": fmt.Sprintf("% X", e.Packet.Data),
			}, fmt.Sprintf("packet header=%d dir=%s len=%d", e.Packet.Header.Value, dir, len(e.Packet.Data)), nil)
		}
		handleMutePacket(e)
		handleRoomResetPacket(a, e)
		handleTradePacket(a, e)
		handleUsers28Packet(a, e)
		handleIncomingHeaderSniff(a, e)
		handleOutgoingHeaderSniff(a, e)
		handleStripPacket(a, e)
	})

	// Ensure shout worker is started so sendShout can be used safely.
	startShoutWorker()
}

// startShoutWorker initializes the background worker that drains shoutQueue
// and sends messages with spacing to avoid triggering flood-control mutes.
func startShoutWorker() {
	shoutWorkerOnce.Do(func() {
		shoutQueue = make(chan string, 128)
		go func() {
			for m := range shoutQueue {
				s := strings.TrimSpace(m)
				if s == "" {
					continue
				}
				if isMuted {
					// If muted, keep it in the muted queue for later replay.
					messageQueue = append(messageQueue, s)
					log.Printf("[SHOUT_WORKER] muted while dequeued at %s: %q", time.Now().Format(time.RFC3339Nano), s)
					continue
				}
				// Ensure a controlled spacing before each actual send so the
				// worker enforces the flood-control delay regardless of how
				// quickly callers enqueue messages.
				sleepDur := shoutSpacing + time.Duration(rand.Intn(600))*time.Millisecond
				log.Printf("[SHOUT_WORKER] dequeued at %s, sleeping %s before send: %q", time.Now().Format(time.RFC3339Nano), sleepDur, s)
				time.Sleep(sleepDur)
				ext.Send(out.SHOUT, s)
				log.Printf("[SHOUT_WORKER] sent at %s: %q", time.Now().Format(time.RFC3339Nano), s)
			}
		}()
	})
}

// sendShout is a mute-aware helper for sending public shouts. It enqueues
// into the shout worker if possible, or falls back to a synchronous send.
func sendShout(msg string) {
	trimmed := strings.TrimSpace(msg)
	if trimmed == "" {
		return
	}
	if isMuted {
		messageQueue = append(messageQueue, trimmed)
		log.Printf("[SHOUT_QUEUE] muted enqueue at %s: %q", time.Now().Format(time.RFC3339Nano), trimmed)
		return
	}
	startShoutWorker()
	log.Printf("[SHOUT_QUEUE] enqueue at %s: %q", time.Now().Format(time.RFC3339Nano), trimmed)
	// Block until there is room in the queue so every shout goes through the
	// centralized shout worker and is rate-limited. This ensures consistent
	// 2.5s spacing between actual sends.
	shoutQueue <- trimmed
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

		// Enqueue queued messages and let the shout worker apply spacing.
		for _, message := range messageQueue {
			sendShout(message)
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
			go LogEvent("trade_open", map[string]interface{}{"mode": "outgoing", "target_id": targetID, "payload": string(e.Packet.Data)}, fmt.Sprintf("Outgoing TRADE_OPEN target=%d", targetID), nil)
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

			// Persist incoming TRADE_ITEMS snapshot for audit
			go LogEvent("trade_items", map[string]interface{}{"all_items": allItems, "partner_map": partnerMap, "own_map": ownMap}, fmt.Sprintf("TRADE_ITEMS all=%d", len(allItems)), map[string]string{"mode": "non-payout"})

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
				sendShout("Trade is back within limits, accept again if needed")
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
			// Determine partner name robustly: prefer explicit payout target
			// (set by startPayout), then fall back to the lastTradePartnerName
			// and finally try short lookups by trade id / room entity.
			partnerName := strings.TrimSpace(payoutTargetName)
			if partnerName == "" {
				partnerName = normalizeUsername(strings.TrimSpace(lastTradePartnerName))
			} else {
				partnerName = normalizeUsername(partnerName)
			}
			if partnerName == "" || partnerName == "Unknown" {
				if lastTradePartnerID > 0 {
					if name, ok := waitForUsers28TradeIDName(lastTradePartnerID, 700*time.Millisecond); ok {
						partnerName = normalizeUsername(strings.TrimSpace(name))
					} else if user, ok := lookupUsers28UserByTradeID(lastTradePartnerID); ok {
						partnerName = normalizeUsername(strings.TrimSpace(user.Username))
					} else if name, ok := lookupRoomEntityNameByIndex(lastTradePartnerID); ok {
						partnerName = normalizeUsername(strings.TrimSpace(name))
					}
				}
			}
			if partnerName == "" {
				partnerName = "Unknown"
			}

			a.AddLogMsg("[TRADE_COMPLETED #112] payout trade completed")
			a.captureCurrentGameHistoryPayoutItems(payoutItems, "Payout trade completed successfully", true, true)
			appendPayoutTimeline(payoutSessionID, "TRADE_COMPLETED partner=%q items=%v", partnerName, payoutItems)

			// Persist completed payout trade for audit
			go LogEvent("trade_completed", map[string]interface{}{"mode": "payout", "partner": partnerName, "payout_items": payoutItems}, fmt.Sprintf("Payout trade completed to %s", partnerName), nil)

			completeMsg := fmt.Sprintf("Trade Completed: \"%s\"", partnerName)
			a.AddLogMsg(fmt.Sprintf("[TRADE_COMPLETED] shouting: %q", completeMsg))
			sendShout(completeMsg)
		} else {
			a.AddLogMsg("[TRADE_COMPLETED #112] trade completed, sending trade summary")

			// Persist completed bet trade summary for audit
			go LogEvent("trade_completed", map[string]interface{}{"mode": "bet", "partner": normalizeUsername(strings.TrimSpace(lastTradePartnerName)), "bet_items": gameBetItems}, "Trade completed (bet)", nil)

			// Send the trade items summary to chat
			a.sendTradeCompletionMessage()

			// Record predicted payout items for history as 2x.
			// Predict 2x at bet completion; the actual multiplier is set later
			// when the game choice is evaluated so predictions don't inflate when
			// UO7 mode is active but the player hasn't picked "7".
			mult := 2
			payoutPred := make([]TradeItem, 0, len(gameBetItems))
			for _, it := range gameBetItems {
				if it.Quantity <= 0 {
					continue
				}
				payoutPred = append(payoutPred, TradeItem{Name: it.Name, Quantity: it.Quantity * mult, RawData: it.RawData})
			}
			if len(payoutPred) > 0 {
				// Keep the round open after the bet trade completes. At this point the
				// player still has to choose a game and the app still needs to record the
				// actual game, winner and results. We only persist a predicted payout so
				// the history modal can show the expected return while the round is live.
				a.captureCurrentGameHistoryPayoutItems(payoutPred, "Predicted payout (2x bet)", false, false)
			} else {
				// Do not complete the round here. A missing prediction should not clear
				// currentGameHistoryID before the game result is recorded.
				a.captureCurrentGameHistoryPayoutItems([]TradeItem{}, "No payout items recorded yet", false, false)
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
		if !payoutTradeSent && !matchesRecentOutgoingFunc(e.Packet.Data) {
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
				// Record trade-open token discovery
				go LogEvent("trade_open", map[string]interface{}{"token": tradeToken, "raw": fmt.Sprintf("% X", e.Packet.Data)}, "Trade open token extracted", nil)
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
					// Persist trade-open resolution
					go LogEvent("trade_open", map[string]interface{}{"token": tradeStarterToken, "name": tradeStarterName, "chat_id": tradeStarterChatID, "trade_id": tradeStarterTradeID}, "Trade open resolved via users28 token", map[string]string{"resolved": "true"})
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

		// Determine partner name robustly: prefer explicit payout target
		// then fall back to lastTradePartnerName and finally try short
		// lookups by trade id / room entity.
		partnerName := strings.TrimSpace(payoutTargetName)
		if partnerName == "" {
			partnerName = normalizeUsername(strings.TrimSpace(lastTradePartnerName))
		} else {
			partnerName = normalizeUsername(partnerName)
		}
		if partnerName == "" || partnerName == "Unknown" {
			if lastTradePartnerID > 0 {
				if name, ok := waitForUsers28TradeIDName(lastTradePartnerID, 700*time.Millisecond); ok {
					partnerName = normalizeUsername(strings.TrimSpace(name))
				} else if user, ok := lookupUsers28UserByTradeID(lastTradePartnerID); ok {
					partnerName = normalizeUsername(strings.TrimSpace(user.Username))
				} else if name, ok := lookupRoomEntityNameByIndex(lastTradePartnerID); ok {
					partnerName = normalizeUsername(strings.TrimSpace(name))
				}
			}
		}
		if partnerName == "" {
			partnerName = "Unknown"
		}
		if !isPayoutTradeOpen && !matchedRecentOutgoing {
			if partnerName == "" || strings.EqualFold(partnerName, "Unknown") {
				notify := "Sorry can't identify you from the current room-user state, please rejoin room and try again"
				a.AddLogMsg(fmt.Sprintf("[TRADE_OPEN] unresolved partner from strict parsed USERS28 state, cancelling trade: %s", notify))
				e.Block()
				ext.Send(out.TRADE_CLOSE)
				sendShout(notify)
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
			sendShout(openMsg)
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
			sendShout(closeMsg)
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
				appendPayoutTimeline(payoutSessionID, "TRADE_CLOSE by %s cancel_count=%d", retryTargetName, payoutCancelCount)

				playerName := strings.TrimSpace(retryTargetName)
				if playerName == "" {
					playerName = "Player"
				}

				// Public notice at most once every 45 seconds — be reassuring for early retries
				if canAnnouncePayoutCancelNotice() {
					var msg string
					if payoutCancelCount < 5 {
						msg = fmt.Sprintf("%q closed trade; retrying payout — please reopen trade and remain while items are added.", playerName)
					} else {
						msg = fmt.Sprintf("%q closed trade", playerName)
					}
					sendShout(msg)
					markPayoutCancelNoticeSent()
				}

				if payoutCancelCount >= 5 {
					stopPayoutResponseTimeoutMonitor()
					stopPayout()
					resetPayoutRetryState()
					resetTradeAutoFlow()

					flagMsg := "User have cancelled trade too many times, flagged issue please go to rollorigins.club."
					sendShout(flagMsg)

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

// appendPayoutTimeline writes a compact timestamped timeline entry for the
// current payout session. Files are named payout_timeline_<session>.log.
func appendPayoutTimeline(session int, format string, args ...interface{}) {
	if session <= 0 {
		session = payoutSessionID
	}
	fname := fmt.Sprintf("payout_timeline_%d.log", session)
	f, err := os.OpenFile(fname, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		log.Printf("[PAYOUT_LOG] failed to open timeline file %s: %v", fname, err)
		return
	}
	defer f.Close()
	ts := time.Now().Format(time.RFC3339Nano)
	entry := fmt.Sprintf(format, args...)
	fmt.Fprintf(f, "%s %s\n", ts, entry)
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
		sendShout(timeoutMsg)

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
			sendShout(flagMsg)

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
	// Record timeline and notify player for large payouts
	appendPayoutTimeline(sessionID, "Payout started target=%q id=%d session=%d", targetName, targetID, sessionID)
	if strings.TrimSpace(targetName) != "" {
		go sendMessageWithDelay(fmt.Sprintf("Payout started for %s — offering items now, please remain in trade until 'Trade Completed'.", targetName))
	}

	go func() {
		// Small delay so the winner shout clears Habbo's rate limiter first
		time.Sleep(1200 * time.Millisecond)

		// Outgoing TRADE_OPEN must use a room/chat index domain. Keep payout
		// target IDs in that same domain to avoid retry oscillation.
		if targetID > 512 && strings.TrimSpace(targetName) != "" {
			if idx, ok := waitForRoomEntityIndexByName(targetName, 900*time.Millisecond); ok && idx > 0 {
				a.AddLogMsg(fmt.Sprintf("[PAYOUT] normalized large target id %d -> room index %d for %s", targetID, idx, targetName))
				targetID = idx
				payoutTargetID = idx
			} else if idx, ok := waitForUsers28RoomIndexByName(targetName, 700*time.Millisecond); ok && idx > 0 {
				a.AddLogMsg(fmt.Sprintf("[PAYOUT] normalized large target id %d -> USERS28 room index %d for %s", targetID, idx, targetName))
				targetID = idx
				payoutTargetID = idx
			}
		}

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
				} else if resolvedID, ok := waitForUsers28RoomIndexByName(targetName, 700*time.Millisecond); ok && resolvedID > 0 && resolvedID != targetID {
					a.AddLogMsg(fmt.Sprintf("[PAYOUT] refreshed %s target from USERS28 room index %d -> %d", targetName, targetID, resolvedID))
					targetID = resolvedID
					payoutTargetID = resolvedID
				} else if resolvedID, ok := waitForUsers28NameIndex(targetName, 700*time.Millisecond); ok && resolvedID > 0 && resolvedID != targetID {
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
			appendPayoutTimeline(sessionID, "Outgoing TRADE_OPEN attempt %d payload=%q", attempt, outPreview)
			ext.Send(out.TRADE_OPEN, targetID)

			// Fallback: send the raw VL64 payload form as well. Some sessions are picky about payload composition.
			rawPayload := encodeVL64(targetID)
			a.AddLogMsg(fmt.Sprintf("[PAYOUT] outgoing[71] raw payload fallback=%q", rawPayload))
			appendPayoutTimeline(sessionID, "Outgoing TRADE_OPEN raw payload fallback=%q", rawPayload)
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
	time.Sleep(400 * time.Millisecond) // settle time after trade opens (shorter)

	// Always begin payout selection from a fresh completed strip scan so we do
	// not choose ids from a stale or partial hand snapshot from the previous
	// round. This is the critical guard for cases where the dealer just lost or
	// re-added the same item type and the live hand cache has not caught up yet.
	if ok := a.forceRefreshHandSnapshot("payout auto-add start"); !ok {
		a.AddLogMsg("[PAYOUT] aborting auto-add because forced hand refresh failed at payout start")
		return
	}

	// Allow a one-shot override from Risk finalize to specify exact required map.
	mutex.Lock()
	override := riskPayoutActive
	overrideReq := riskPayoutRequired
	if override {
		// consume override once
		riskPayoutActive = false
		riskPayoutRequired = nil
	}
	mutex.Unlock()

	var required map[string]int
	var betItems []TradeItem
	if override && len(overrideReq) > 0 {
		required = overrideReq
		// Build a pseudo betItems list for logging/ordering
		for name, qty := range required {
			betItems = append(betItems, TradeItem{Name: name, Quantity: qty})
		}
		sort.Slice(betItems, func(i, j int) bool { return betItems[i].Name < betItems[j].Name })
	} else {
		betItems = gameBetItems
		if len(betItems) == 0 {
			a.AddLogMsg("[PAYOUT] no bet items recorded, skipping auto-add")
			return
		}

		a.AddLogMsg(fmt.Sprintf("[PAYOUT_DEBUG] auto-add start: bet item types=%d", len(betItems)))
		for i, betItem := range betItems {
			a.AddLogMsg(fmt.Sprintf("[PAYOUT_DEBUG] bet[%d] name=%q qty=%d payoutTarget=%d", i, betItem.Name, betItem.Quantity, betItem.Quantity*2))
		}

		required = payoutRequirementsFromBetItemsMult(betItems, payoutMultiplierForRound)
	}
	// Compute total required items up-front so outgoing intercept can
	// track progress against this expected count while we send adds.
	requiredTotal := 0
	for _, need := range required {
		requiredTotal += need
	}
	payoutExpectedAddCount = requiredTotal
	payoutActualAddCount = 0
	// Announce and timeline for large payouts
	if requiredTotal >= payoutLargePayoutThreshold {
		estSecs := int((payoutAddInterval * time.Duration(requiredTotal)).Seconds())
		go sendMessageWithDelay(fmt.Sprintf("Large payout in progress for %s: offering %d items (est %d s). Please remain in trade until 'Trade Completed'.", payoutTargetName, requiredTotal, estSecs))
		appendPayoutTimeline(payoutSessionID, "Large payout started items=%d est_s=%d target=%q", requiredTotal, estSecs, payoutTargetName)
	}
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
			time.Sleep(payoutAddInterval)
			ext.Send(out.TRADE_ADDITEM, -itemID)
			plannedIDs = append(plannedIDs, itemID)
			total++
			payload := string(ext.NewPacket(out.TRADE_ADDITEM, -itemID).Data)
			a.AddLogMsg(fmt.Sprintf("[PAYOUT] added item %d (%s) %d/%d payload=%q", itemID, betItem.Name, total, needed, payload))
			a.AddLogMsg(fmt.Sprintf("[PAYOUT_DEBUG] added id %d (planned so far %d/%d)", itemID, len(plannedIDs), payoutExpectedAddCount))

			appendPayoutTimeline(payoutSessionID, "Sent TRADE_ADDITEM -%d item=%q progress=%d/%d payload=%q", itemID, betItem.Name, total, requiredTotal, payload)

			if requiredTotal >= payoutLargePayoutThreshold && payoutProgressAnnounceEvery > 0 && (total%payoutProgressAnnounceEvery) == 0 {
				go sendMessageWithDelay(fmt.Sprintf("Payout progress for %s: %d/%d items queued — please remain in trade.", payoutTargetName, total, requiredTotal))
				appendPayoutTimeline(payoutSessionID, "Progress %d/%d", total, requiredTotal)
			}
		}
	}
	a.AddLogMsg(fmt.Sprintf("[PAYOUT] auto-add complete: %d item(s) offered", total))
	appendPayoutTimeline(payoutSessionID, "Auto-add complete offered=%d planned=%d", total, payoutExpectedAddCount)

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
			appendPayoutTimeline(payoutSessionID, "Auto-accept sent after full placement (%d/%d sent=%d)", total, requiredTotal, payoutActualAddCount)

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

// handlePlayerWinRisk records an initial win into the internal Risk state
// instead of immediately opening a payout trade. It initializes dealer bank
// from the one-time snapshot and adjusts internal counters.
func (a *App) handlePlayerWinRisk(betItems []TradeItem, playerName string, playerID int, game string, params map[string]interface{}) {
	if !isRiskEnabled {
		startPayout(a, playerID, playerName)
		return
	}

	if len(betItems) == 0 {
		startPayout(a, playerID, playerName)
		return
	}

	betQty := 0
	for _, it := range betItems {
		betQty += it.Quantity
	}
	if betQty <= 0 {
		startPayout(a, playerID, playerName)
		return
	}

	// Ensure dealer snapshot is fresh for this trade — always force so a stale
	// snapshot from a previous session never inflates dealerRisk.
	a.captureRiskSnapshot(true)

	mutex.Lock()
	if !riskInitialized {
		// Limit dealer risk to stock relevant to this bet so unrelated hand
		// items cannot inflate the player's allowed risk amount.
		initialQty := 0
		if riskHandSnapshotReady && len(riskHandSnapshot) > 0 {
			initialQty = riskRelevantHandQuantity(riskHandSnapshot, betItems)
			dealerSnapshotQty = initialQty
			riskSnapshotTaken = true
		} else {
			initialQty = dealerSnapshotQty
		}
		dealerRisk = initialQty + betQty
		playerRisk = 0
		riskInitialized = true
		riskPartnerID = playerID
		riskPartnerName = playerName
	}

	// Compute payout multiplier: support UO7 configured multiplier when applicable.
	mult := 2
	if strings.EqualFold(game, "UO7") {
		if v, ok := params["uoChoice"].(string); ok {
			if strings.TrimSpace(strings.ToLower(v)) == "7" {
				mutex.Lock()
				mult = underOver7PayoutMultiplier
				mutex.Unlock()
			}
		}
	}
	// Record the per-risk-session multiplier so re-rolls and finalization honor it.
	riskSessionPayoutMultiplier = mult
	payout := betQty * mult
	if payout > dealerRisk {
		payout = dealerRisk
	}
	dealerRisk -= payout
	playerRisk += payout

	// Snapshot current risk state into the active game history entry.
	rp := riskPendingBet
	rb := playerRisk
	rs := riskSessionActive
	// release the main mutex briefly to avoid lock-order inversion
	mutex.Unlock()
	a.gameHistoryMu.Lock()
	_a := a // local alias for closure capture
	_a.updateCurrentGameHistoryLocked(func(entry *GameHistoryEntry) {
		entry.RiskSession = rs
		entry.RiskBank = rb
		entry.RiskPending = rp
		entry.Notes = append(entry.Notes, fmt.Sprintf("Risk session started: bank=%d", rb))
	})
	a.gameHistoryMu.Unlock()
	mutex.Lock()

	// compute usable max under lock (per-item cap, player-limited and dealer-limited)
	displayMax := maxTradeQuantityPerItem
	if playerRisk < displayMax {
		displayMax = playerRisk
	}
	if dealerRisk < displayMax {
		displayMax = dealerRisk
	}

	// If there's nothing the player can risk, clear session state and finalize or reopen.
	if displayMax <= 0 {
		finalPlayerRisk := playerRisk
		// clear session flags (keep partner info if we need it for finalize)
		stopRiskDecisionTimeoutMonitor()
		riskSessionActive = false
		riskSessionGame = ""
		riskSessionParams = nil
		riskPendingBet = 0
		// don't clear riskPartnerID/riskPartnerName here; finalizeRiskKeep relies on them
		mutex.Unlock()

		if finalPlayerRisk > 0 {
			// Dealer cannot cover further risk; convert bank into physical payout.
			go a.finalizeRiskKeep()
		} else {
			a.AddLogMsg(fmt.Sprintf("[RISK] no bank available to risk (playerRisk=%d dealerRisk=%d); reopening dealer", finalPlayerRisk, dealerRisk))
			go a.openDealerAfterRound()
		}
		return
	}

	// Normal path: start a risk session and prompt player
	riskSessionActive = true
	riskSessionGame = game
	riskSessionParams = params
	mutex.Unlock()

	a.AddLogMsg(fmt.Sprintf("[RISK] win recorded bet=%d payout=%d dealerRisk=%d playerRisk=%d", betQty, payout, dealerRisk, playerRisk))

	// Send initial prompt (mute-aware) and start the risk-decision reminder monitor
	go func(player string) {
		waitForUnmute(90 * time.Second)
		mutex.Lock()
		canPrompt := riskSessionActive && playerRisk > 0 && dealerRisk > 0
		mutex.Unlock()
		if !canPrompt {
			return
		}
		time.Sleep(800 * time.Millisecond)
		mutex.Lock()
		canPrompt = riskSessionActive && playerRisk > 0 && dealerRisk > 0
		msg, ok := buildRiskPromptLocked()
		mutex.Unlock()
		if !canPrompt || !ok {
			return
		}
		sendMessageWithDelay(msg)
		a.startRiskDecisionTimeoutMonitor(player)
	}(playerName)
}

// handleRiskBet validates and applies a player's risk bet (internal state move)
// then prompts the player to choose a game for the re-roll (do not auto-roll).
func (a *App) handleRiskBet(n int, sender string) {
	mutex.Lock()
	// Only the partner who won may place risk bets while a session is active.
	if !riskSessionActive || (riskPartnerName != "" && !strings.EqualFold(sender, riskPartnerName)) {
		mutex.Unlock()
		return
	}
	// Player engaged with risk decision; cancel the initial Keep-or-Risk reminder monitor.
	stopRiskDecisionTimeoutMonitor()

	// Defensive guards: reject if player's internal bank is empty or dealer reopened.
	if playerRisk <= 0 {
		mutex.Unlock()
		sendShout("No bank available to risk.")
		a.AddLogMsg(fmt.Sprintf("[RISK] rejected r%d from %s: no player bank", n, sender))
		// If there's no bank left, ensure dealer reopens cleanly.
		go a.openDealerAfterRound()
		return
	}
	if dealerAcceptingTrades || awaitingTradeOpen {
		mutex.Unlock()
		sendShout("Risk unavailable while dealer is open.")
		a.AddLogMsg(fmt.Sprintf("[RISK] rejected r%d from %s: dealer open", n, sender))
		return
	}

	max := maxTradeQuantityPerItem
	if playerRisk < max {
		max = playerRisk
	}
	if dealerRisk < max {
		max = dealerRisk
	}
	if n <= 0 || n > max {
		mutex.Unlock()
		sendShout(fmt.Sprintf("Invalid risk amount. Max: %d", max))
		return
	}

	playerRisk -= n
	dealerRisk += n
	riskPendingBet = n
	// capture snapshot values under lock to avoid races
	rp := riskPendingBet
	rb := playerRisk
	rs := riskSessionActive
	mutex.Unlock()

	// update history with the placed risk
	a.gameHistoryMu.Lock()
	a.updateCurrentGameHistoryLocked(func(entry *GameHistoryEntry) {
		entry.RiskPending = rp
		entry.RiskBank = rb
		if rs {
			entry.RiskSession = true
		}
		entry.Notes = append(entry.Notes, fmt.Sprintf("Risk bet placed: %d", rp))
		entry.RiskDecision = fmt.Sprintf("Risk %d", rp)
	})
	a.gameHistoryMu.Unlock()

	a.AddLogMsg(fmt.Sprintf("[RISK] %s risked %d (playerRisk=%d dealerRisk=%d)", sender, n, playerRisk, dealerRisk))

	// Prompt the player to choose a game for the risk re-roll (locked to the partner).
	awaitingGameChoice = true
	gameChoiceUnreadableWarned = false

	// Use the established risk partner name if available.
	if strings.TrimSpace(riskPartnerName) != "" {
		awaitingGameChoicePartnerName = normalizeUsername(strings.TrimSpace(riskPartnerName))
	} else {
		awaitingGameChoicePartnerName = normalizeUsername(strings.TrimSpace(sender))
	}

	// Attempt to resolve a chat index to lock the prompt.
	awaitingGameChoicePartnerID = 0
	if riskPartnerID > 0 {
		awaitingGameChoicePartnerID = riskPartnerID
	} else if tradeStarterChatID > 0 {
		awaitingGameChoicePartnerID = tradeStarterChatID
	} else if awaitingGameChoicePartnerName != "" {
		if chatIdx, ok := lookupRoomEntityIndexByName(awaitingGameChoicePartnerName); ok && chatIdx > 0 {
			awaitingGameChoicePartnerID = chatIdx
		} else if chatIdx, ok := waitForUsers28RoomIndexByName(awaitingGameChoicePartnerName, 900*time.Millisecond); ok && chatIdx > 0 {
			awaitingGameChoicePartnerID = chatIdx
		} else if chatIdx, ok := lookupUsers28RoomIndexByName(awaitingGameChoicePartnerName); ok && chatIdx > 0 {
			awaitingGameChoicePartnerID = chatIdx
		}
	}

	mutex.Lock()
	msg := buildGameChoicePromptLocked()
	mutex.Unlock()

	a.AddLogMsg(fmt.Sprintf("[RISK] prompting for game choice: %q (partner=%q id=%d)", msg, awaitingGameChoicePartnerName, awaitingGameChoicePartnerID))
	sendShout(msg)

	// Start the game-choice reminder monitor after the initial prompt is sent.
	a.startGameChoiceTimeoutMonitor()
}

func buildRiskBetItems(base []TradeItem, riskQty int) []TradeItem {
	if riskQty <= 0 {
		return cloneTradeItems(base)
	}

	if len(base) == 0 {
		return []TradeItem{{Name: "risk_bet", Quantity: riskQty}}
	}

	baseQty := map[string]int{}
	rawByName := map[string]string{}
	names := make([]string, 0)
	for _, it := range base {
		name := strings.TrimSpace(it.Name)
		if name == "" || it.Quantity <= 0 {
			continue
		}
		if _, ok := baseQty[name]; !ok {
			names = append(names, name)
		}
		baseQty[name] += it.Quantity
		if rawByName[name] == "" {
			rawByName[name] = it.RawData
		}
	}
	if len(baseQty) == 0 {
		return []TradeItem{{Name: "risk_bet", Quantity: riskQty}}
	}

	sort.Strings(names)
	baseTotal := 0
	for _, n := range names {
		baseTotal += baseQty[n]
	}
	if baseTotal <= 0 {
		return []TradeItem{{Name: "risk_bet", Quantity: riskQty}}
	}

	mult := riskQty / baseTotal
	rem := riskQty % baseTotal
	outQty := map[string]int{}
	for _, n := range names {
		outQty[n] = baseQty[n] * mult
	}
	for i := 0; rem > 0; i++ {
		n := names[i%len(names)]
		outQty[n]++
		rem--
	}

	out := make([]TradeItem, 0, len(names))
	for _, n := range names {
		if outQty[n] <= 0 {
			continue
		}
		out = append(out, TradeItem{Name: n, Quantity: outQty[n], RawData: rawByName[n]})
	}
	if len(out) == 0 {
		return []TradeItem{{Name: "risk_bet", Quantity: riskQty}}
	}

	return out
}

func (a *App) beginRiskRoundHistory(choice string, rawShout string, gameLabel string) {
	// Close any open entry in this chain before starting a new risk-game row.
	a.gameHistoryMu.Lock()
	if a.updateCurrentGameHistoryLocked(func(entry *GameHistoryEntry) {
		if strings.TrimSpace(entry.CompletedAt) == "" {
			entry.Status = "Risk Continued"
			entry.CompletedAt = gameHistoryTimestamp()
			entry.Notes = append(entry.Notes, "Risk chain continued into a new game")
		}
	}) {
		a.currentGameHistoryID = ""
	}
	a.gameHistoryMu.Unlock()

	partnerName := normalizeUsername(strings.TrimSpace(riskPartnerName))
	if partnerName == "" {
		partnerName = normalizeUsername(strings.TrimSpace(lastTradePartnerName))
	}
	if partnerName == "" {
		partnerName = "Player"
	}

	mutex.Lock()
	pending := riskPendingBet
	mutex.Unlock()

	riskBetItems := buildRiskBetItems(cloneTradeItems(gameBetItems), pending)
	a.beginGameHistory(partnerName, riskBetItems)
	a.setCurrentGameHistoryChoice(choice, rawShout)
	if strings.TrimSpace(gameLabel) != "" {
		a.setCurrentGameHistoryGame(gameLabel)
	}
	if pending > 0 {
		a.noteCurrentGameHistory(fmt.Sprintf("Risk stake: %d", pending))
	}
}

// executeRiskRound performs the same game roll for the active risk session.
// It sets minimal game state then invokes the normal roll path so evaluation
// still runs through the existing finalize/evaluate functions which will
// route results via applyRiskOutcome when riskSessionActive is true.
func (a *App) executeRiskRound() {
	mutex.Lock()
	game := riskSessionGame
	params := riskSessionParams
	mutex.Unlock()

	switch game {
	case "UO7", "UO":
		if v, ok := params["uoChoice"].(string); ok {
			mutex.Lock()
			uoPlayerChoice = v
			uoRoundActive = true
			isUORolling = true
			mutex.Unlock()
		} else {
			mutex.Lock()
			uoRoundActive = true
			isUORolling = true
			mutex.Unlock()
		}
		a.rollUnderOverDice()
	case "13":
		mutex.Lock()
		thirteenPlayerTurn = true
		is13Rolling = true
		mutex.Unlock()
		a.roll13Dice()
	case "Tri":
		if v, ok := params["mode"].(string); ok {
			mutex.Lock()
			triMode = v
			isTriRolling = true
			mutex.Unlock()
		} else {
			mutex.Lock()
			isTriRolling = true
			mutex.Unlock()
		}
		a.rollTriDice()
	case "21":
		mutex.Lock()
		blackjackPlayerTurn = true
		isBJRolling = true
		mutex.Unlock()
		a.rollBjDice()
	default:
		// Unknown game: simple coin flip fallback
		win := rand.Intn(2) == 0
		a.applyRiskOutcome(win)
	}
}

// applyRiskOutcome applies the result of a risk re-roll to internal bank
// accounting, shouts status, and finalizes if necessary.
func (a *App) applyRiskOutcome(playerWins bool) {
	mutex.Lock()
	partner := riskPartnerName
	pending := riskPendingBet
	if !riskSessionActive {
		mutex.Unlock()
		return
	}
	if !playerWins {
		// Player loses the pending bet only; keep any remaining bank so player
		// may choose to risk again. Clear pending bet and decide whether to
		// re-prompt or end the session if nothing remains usable.
		riskPendingBet = 0

		// Snapshot current risk state into the active game history entry.
		rp := riskPendingBet
		rb := playerRisk
		rs := riskSessionActive
		// release main mutex briefly to avoid lock-order inversion
		mutex.Unlock()
		a.gameHistoryMu.Lock()
		a.updateCurrentGameHistoryLocked(func(entry *GameHistoryEntry) {
			entry.RiskPending = rp
			entry.RiskBank = rb
			entry.RiskSession = rs
			entry.Notes = append(entry.Notes, fmt.Sprintf("Risk round resolved: lost %d, bank=%d", rp, rb))
		})
		a.gameHistoryMu.Unlock()
		mutex.Lock()

		// compute usable max including per-item cap
		displayMax := maxTradeQuantityPerItem
		if playerRisk < displayMax {
			displayMax = playerRisk
		}
		if dealerRisk < displayMax {
			displayMax = dealerRisk
		}

		// If nothing left to risk, end session and reopen/finalize as appropriate.
		if playerRisk <= 0 || displayMax <= 0 {
			stopRiskDecisionTimeoutMonitor()
			riskSessionActive = false
			riskSessionGame = ""
			riskSessionParams = nil
			mutex.Unlock()

			a.AddLogMsg(fmt.Sprintf("[RISK] %s lost risk session; dealerRisk=%d playerRisk=%d", partner, dealerRisk, playerRisk))
			sendShout(fmt.Sprintf("%s lost the risk streak.", partner))

			if playerRisk > 0 && dealerRisk <= 0 {
				go a.finalizeRiskKeep()
				return
			}
			go a.openDealerAfterRound()
			return
		}

		// Player still has bank left: keep session and re-prompt, unless dealer
		// can no longer cover additional risk.
		shouldFinalize := dealerRisk <= 0
		mutex.Unlock()
		if shouldFinalize {
			go a.finalizeRiskKeep()
			return
		}

		a.AddLogMsg(fmt.Sprintf("[RISK] %s lost risk round; bank remains playerRisk=%d dealerRisk=%d", partner, playerRisk, dealerRisk))
		sendShout(fmt.Sprintf("%s lost the risk round. Bank: %d", partner, playerRisk))

		go func(max int) {
			waitForUnmute(90 * time.Second)
			mutex.Lock()
			canPrompt := riskSessionActive && playerRisk > 0 && dealerRisk > 0
			mutex.Unlock()
			if !canPrompt {
				return
			}
			time.Sleep(800 * time.Millisecond)
			mutex.Lock()
			canPrompt = riskSessionActive && playerRisk > 0 && dealerRisk > 0
			mutex.Unlock()
			if !canPrompt {
				return
			}
			// Belt-and-suspenders: validate live snapshot covers dealerRisk before
			// prompting. If the snapshot is short (e.g. stale from a prior trade),
			// cap dealerRisk so we never over-promise.
			mutex.Lock()
			handItemsMu.Lock()
			snap := make([]TradeItem, len(riskHandSnapshot))
			copy(snap, riskHandSnapshot)
			handItemsMu.Unlock()
			liveCover := riskRelevantHandQuantity(snap, gameBetItems)
			totalCommitted := playerRisk + dealerRisk
			if liveCover < totalCommitted {
				diff := totalCommitted - liveCover
				if diff > dealerRisk {
					diff = dealerRisk
				}
				a.AddLogMsg(fmt.Sprintf("[RISK] live cover %d < committed %d; capping dealerRisk by %d", liveCover, totalCommitted, diff))
				dealerRisk -= diff
			}
			if dealerRisk <= 0 {
				mutex.Unlock()
				go a.finalizeRiskKeep()
				return
			}
			curMax := maxTradeQuantityPerItem
			if playerRisk < curMax {
				curMax = playerRisk
			}
			if dealerRisk < curMax {
				curMax = dealerRisk
			}
			msg := fmt.Sprintf("Keep or Risk (rN)? Current bank: %d. Your max risk: %d", playerRisk, curMax)
			mutex.Unlock()
			sendMessageWithDelay(msg)
		}(displayMax)
		return
	}

	// Player won the risk round: dealer pays according to session multiplier.
	pay := pending * riskSessionPayoutMultiplier
	if pay > dealerRisk {
		pay = dealerRisk
	}
	dealerRisk -= pay
	playerRisk += pay
	riskPendingBet = 0

	// Snapshot post-win risk state for history before releasing main mutex.
	rp2 := riskPendingBet
	rb2 := playerRisk
	rs2 := riskSessionActive
	mutex.Unlock()
	a.gameHistoryMu.Lock()
	a.updateCurrentGameHistoryLocked(func(entry *GameHistoryEntry) {
		entry.RiskPending = rp2
		entry.RiskBank = rb2
		entry.RiskSession = rs2
		entry.Notes = append(entry.Notes, fmt.Sprintf("Risk round won: paid=%d bank=%d", pay, rb2))
	})
	a.gameHistoryMu.Unlock()

	a.AddLogMsg(fmt.Sprintf("[RISK] %s won risk round: paid %d (playerRisk=%d dealerRisk=%d)", partner, pay, playerRisk, dealerRisk))
	// Duplicate winner shout removed: the winner is already announced by the game result.

	// Recompute usable max (include per-item cap) and re-prompt the player, or finalize/reopen if nothing to risk.
	mutex.Lock()
	displayMax := maxTradeQuantityPerItem
	if playerRisk < displayMax {
		displayMax = playerRisk
	}
	if dealerRisk < displayMax {
		displayMax = dealerRisk
	}
	if displayMax <= 0 {
		finalPlayerRisk := playerRisk
		// clear session flags (keep partner info for finalize)
		stopRiskDecisionTimeoutMonitor()
		riskSessionActive = false
		riskPendingBet = 0
		riskSessionGame = ""
		riskSessionParams = nil
		mutex.Unlock()

		if finalPlayerRisk > 0 {
			// Dealer cannot cover further risk; convert bank into physical payout.
			go a.finalizeRiskKeep()
		} else {
			go a.openDealerAfterRound()
		}
		return
	}
	if dealerRisk <= 0 {
		mutex.Unlock()
		go a.finalizeRiskKeep()
		return
	}
	mutex.Unlock()

	go func(max int) {
		waitForUnmute(90 * time.Second)
		mutex.Lock()
		canPrompt := riskSessionActive && playerRisk > 0 && dealerRisk > 0
		mutex.Unlock()
		if !canPrompt {
			return
		}
		time.Sleep(800 * time.Millisecond)
		mutex.Lock()
		canPrompt = riskSessionActive && playerRisk > 0 && dealerRisk > 0
		mutex.Unlock()
		if !canPrompt {
			return
		}
		// Belt-and-suspenders: validate live snapshot covers dealerRisk before
		// prompting. If the snapshot is short (e.g. stale from a prior trade),
		// cap dealerRisk so we never over-promise.
		mutex.Lock()
		handItemsMu.Lock()
		snap := make([]TradeItem, len(riskHandSnapshot))
		copy(snap, riskHandSnapshot)
		handItemsMu.Unlock()
		liveCover := riskRelevantHandQuantity(snap, gameBetItems)
		totalCommitted := playerRisk + dealerRisk
		if liveCover < totalCommitted {
			diff := totalCommitted - liveCover
			if diff > dealerRisk {
				diff = dealerRisk
			}
			a.AddLogMsg(fmt.Sprintf("[RISK] live cover %d < committed %d; capping dealerRisk by %d", liveCover, totalCommitted, diff))
			dealerRisk -= diff
		}
		if dealerRisk <= 0 {
			mutex.Unlock()
			go a.finalizeRiskKeep()
			return
		}
		curMax := maxTradeQuantityPerItem
		if playerRisk < curMax {
			curMax = playerRisk
		}
		if dealerRisk < curMax {
			curMax = dealerRisk
		}
		msg := fmt.Sprintf("Keep or Risk (rN)? Current bank: %d. Your max risk: %d", playerRisk, curMax)
		mutex.Unlock()
		sendMessageWithDelay(msg)
	}(displayMax)
}

func buildRiskPromptLocked() (string, bool) {
	if !riskSessionActive || playerRisk <= 0 || dealerRisk <= 0 {
		return "", false
	}

	curMax := maxTradeQuantityPerItem
	if playerRisk < curMax {
		curMax = playerRisk
	}
	if dealerRisk < curMax {
		curMax = dealerRisk
	}
	if curMax <= 0 {
		return "", false
	}

	return fmt.Sprintf("Keep or Risk (rN)? Current bank: %d. Your max risk: %d", playerRisk, curMax), true
}

// finalizeRiskKeep converts the current `playerRisk` internal bank into a
// physical payout trade by setting a one-shot required map and calling
// startPayout (autoAddPayoutItems will consume the override).
func (a *App) finalizeRiskKeep() {
	mutex.Lock()
	if playerRisk <= 0 || riskPartnerID <= 0 {
		mutex.Unlock()
		return
	}
	total := playerRisk
	targetID := riskPartnerID
	targetName := riskPartnerName
	sessionMult := riskSessionPayoutMultiplier
	// clear risk state early
	riskSessionActive = false
	riskSessionGame = ""
	riskSessionParams = nil
	riskPendingBet = 0
	playerRisk = 0
	riskInitialized = false
	// reset multiplier to default
	riskSessionPayoutMultiplier = 2
	mutex.Unlock()

	a.AddLogMsg(fmt.Sprintf("[RISK] finalizing Keep -> payout %d to %s(%d) (mult=%d)", total, targetName, targetID, sessionMult))
	a.noteCurrentGameHistory(fmt.Sprintf("Risk keep selected by %s; converting bank of %d to payout", targetName, total))

	// Record explicit decision in the active history entry so webhooks show "Keep".
	a.gameHistoryMu.Lock()
	a.updateCurrentGameHistoryLocked(func(entry *GameHistoryEntry) {
		entry.RiskDecision = "Keep"
	})
	a.gameHistoryMu.Unlock()

	// Build required map proportionally from recorded bet types if available
	baseMult := sessionMult
	base := payoutRequirementsFromBetItemsMult(gameBetItems, baseMult)
	baseTotal := 0
	for _, v := range base {
		baseTotal += v
	}

	required := map[string]int{}
	if baseTotal == 0 {
		// fallback: use first snapshot item name
		handItemsMu.Lock()
		if len(tradeHandSnapshot) > 0 {
			required[tradeHandSnapshot[0].Name] = total
		}
		handItemsMu.Unlock()
		if len(required) == 0 {
			a.AddLogMsg("[RISK] cannot build payout requirement: no base bet and no snapshot")
			return
		}
	} else {
		mult := total / baseTotal
		rem := total % baseTotal
		names := make([]string, 0, len(base))
		for n := range base {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			required[n] = base[n] * mult
		}
		i := 0
		for rem > 0 {
			required[names[i%len(names)]]++
			rem--
			i++
		}
	}

	mutex.Lock()
	riskPayoutRequired = required
	riskPayoutActive = true
	mutex.Unlock()

	// Use normal payout flow which will be auto-added using the override
	startPayout(a, targetID, targetName)
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

// payoutRequirementsFromBetItemsMult returns a map of required payout quantities
// per item given the bet items and a multiplier (2 for normal wins, 3 for UO7 "7" wins).
func payoutRequirementsFromBetItemsMult(betItems []TradeItem, mult int) map[string]int {
	required := map[string]int{}
	if mult <= 0 {
		mult = 2
	}
	for _, item := range betItems {
		if item.Quantity <= 0 {
			continue
		}
		required[item.Name] += item.Quantity * mult
	}
	return required
}

func riskRelevantHandQuantity(snapshot []TradeItem, betItems []TradeItem) int {
	if len(snapshot) == 0 || len(betItems) == 0 {
		return 0
	}

	relevant := make(map[string]struct{}, len(betItems))
	for _, item := range betItems {
		key := strings.ToLower(strings.TrimSpace(item.Name))
		if normalized, ok := normalizeClassKeyWithVariant(item.Name); ok {
			key = normalized
		}
		if key == "" {
			continue
		}
		relevant[key] = struct{}{}
	}
	if len(relevant) == 0 {
		return 0
	}

	total := 0
	for _, item := range snapshot {
		key := strings.ToLower(strings.TrimSpace(item.Name))
		if normalized, ok := normalizeClassKeyWithVariant(item.Name); ok {
			key = normalized
		}
		if _, ok := relevant[key]; !ok {
			continue
		}
		if item.Quantity > 0 {
			total += item.Quantity
		}
	}

	return total
}

// Backwards-compatible wrapper: default multiplier 2
func payoutRequirementsFromBetItems(betItems []TradeItem) map[string]int {
	return payoutRequirementsFromBetItemsMult(betItems, 2)
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

	appendPayoutTimeline(payoutSessionID, "verifyAndRetryPayoutAdds start planned=%d", len(plannedIDs))

	required := payoutRequirementsFromBetItemsMult(gameBetItems, payoutMultiplierForRound)
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
	appendPayoutTimeline(payoutSessionID, "post-add ownTotal=%d planned=%d", ownTotal, len(plannedIDs))
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
		appendPayoutTimeline(payoutSessionID, "Fallback retry add +%d payload=%q", itemID, payload)
	}

	// Wait again for trade echo, then only accept if exact payout offer is present.
	time.Sleep(2200 * time.Millisecond)
	if a.tryAcceptPayoutTrade(required, "after-positive-retry") {
		return
	}

	if payoutTradeActive && !tradeAutoAccepted && len(plannedIDs) >= requiredTotal && payoutActualAddCount >= requiredTotal {
		a.AddLogMsg(fmt.Sprintf("[PAYOUT_DEBUG] queued payout matched counts but trade echo is still short; refusing fallback accept (%d/%d sent=%d)", len(plannedIDs), requiredTotal, payoutActualAddCount))
	}

	a.AddLogMsg("[PAYOUT_DEBUG] payout items still not fully reflected in own trade offer; waiting for manual intervention")
	if tradeAutoAccepted {
		// Trade was already accepted — items were given to the player. The echo just didn't
		// confirm in time. Leave the game open so TRADE_COMPLETED (header 112) can finalize
		// it normally. Only add a note for audit purposes; do NOT mark as issue.
		a.noteCurrentGameHistory(fmt.Sprintf("Trade echo verification timed out but trade was already accepted — items sent=%d/%d; awaiting TRADE_COMPLETED confirmation", payoutActualAddCount, payoutExpectedAddCount))
		a.AddLogMsg(fmt.Sprintf("[PAYOUT_DEBUG] echo timed out but trade accepted; leaving game open for TRADE_COMPLETED (sent=%d/%d)", payoutActualAddCount, payoutExpectedAddCount))
	} else {
		// Trade was never accepted yet — items echo didn't confirm. Mark as a note/warning
		// but don't close the game. If the trade later completes successfully, it will
		// be updated to "Completed" status. Only mark as Issue if trade fails permanently.
		a.noteCurrentGameHistory(fmt.Sprintf("Payout items did not fully reflect in trade offer (sent=%d/%d); manual review needed", payoutActualAddCount, payoutExpectedAddCount))
		// Compute missing items using current own offer counts and attach structured Issue metadata to history
		have := ownTradeOfferCounts()
		missing := map[string]int{}
		owedTotal := 0
		for name, need := range required {
			haveQty := have[name]
			if haveQty < need {
				missing[name] = need - haveQty
				owedTotal += need - haveQty
			}
		}
		owedStr := formatMissingCounts(missing)

		a.gameHistoryMu.Lock()
		a.updateCurrentGameHistoryLocked(func(entry *GameHistoryEntry) {
			entry.IssueType = "Payout Failure"
			entry.IssueOwed = owedTotal
			entry.IssueOwedItems = owedStr
		})
		a.gameHistoryMu.Unlock()

		a.markCurrentGameHistoryIssue(fmt.Sprintf("Payout items did not fully reflect in trade offer after automated attempts (sent=%d/%d)", payoutActualAddCount, payoutExpectedAddCount), false)
	}
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
	dealerOpenMu.Lock()
	seconds := tradeWindowSeconds
	dealerOpenMu.Unlock()
	if seconds < 1 {
		seconds = 1
	}
	tradeWindowDeadline = tradeWindowOpenedAt.Add(time.Duration(seconds) * time.Second)
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
			sendShout(msg)
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
	dealerOpenMu.Lock()
	base := tradeWindowSeconds
	dealerOpenMu.Unlock()
	if base < 1 {
		base = 1
	}
	maxDeadline := tradeWindowOpenedAt.Add(time.Duration(base*2) * time.Second)
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

// stopRiskDecisionTimeoutMonitor cancels the initial Keep-or-Risk reminder monitor.
func stopRiskDecisionTimeoutMonitor() {
	riskDecisionTimeoutMonitorID++
	riskDecisionTimeoutActive = false
}

// startRiskDecisionTimeoutMonitor will repeat the initial Keep-or-Risk prompt
// up to 4 more times (5 total) before auto-finalizing the Keep path.
func (a *App) startRiskDecisionTimeoutMonitor(player string) {
	stopRiskDecisionTimeoutMonitor()

	riskDecisionTimeoutMonitorID++
	monitorID := riskDecisionTimeoutMonitorID
	riskDecisionTimeoutActive = true

	go func(id int, p string) {
		// Repeat 4 reminders (so initial + 4 = 5 total)
		for attempt := 1; attempt <= 4; attempt++ {
			time.Sleep(30 * time.Second)

			mutex.Lock()
			if id != riskDecisionTimeoutMonitorID || !riskDecisionTimeoutActive || !riskSessionActive {
				mutex.Unlock()
				return
			}
			reminder, ok := buildRiskPromptLocked()
			mutex.Unlock()
			if !ok {
				return
			}

			a.AddLogMsg(fmt.Sprintf("[RISK_DECISION_TIMEOUT] repeating prompt %d/4 for %s", attempt, p))
			sendShout(reminder)
		}

		mutex.Lock()
		shouldFinalize := id == riskDecisionTimeoutMonitorID && riskDecisionTimeoutActive && riskSessionActive
		mutex.Unlock()
		if !shouldFinalize {
			return
		}

		a.AddLogMsg(fmt.Sprintf("[RISK_DECISION_TIMEOUT] final timeout for %s; auto-finalizing Keep", p))
		sendShout(fmt.Sprintf("No response from %q — finalizing Keep and attempting payout.", p))
		time.Sleep(1200 * time.Millisecond)

		// Convert bank -> payout and start normal payout flow.
		go a.finalizeRiskKeep()
	}(monitorID, player)
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
		// Repeat the prompt 4 more times (initial prompt already sent by caller)
		reminderCount := 4
		interval := 30 * time.Second

		for attempt := 1; attempt <= reminderCount; attempt++ {
			time.Sleep(interval)

			if id != gameChoiceTimeoutMonitorID || !gameChoiceTimeoutActive || !awaitingGameChoice {
				return
			}

			mutex.Lock()
			reminder := buildGameChoicePromptLocked()
			mutex.Unlock()

			a.AddLogMsg(fmt.Sprintf("[GAME_CHOICE_TIMEOUT] repeating prompt %d/%d for %s", attempt, reminderCount, player))
			sendShout(reminder)
		}

		if id != gameChoiceTimeoutMonitorID || !gameChoiceTimeoutActive || !awaitingGameChoice {
			return
		}

		// Final timeout hit: auto-finalize Keep -> payout
		awaitingGameChoice = false
		gameChoiceUnreadableWarned = false
		awaitingGameChoicePartnerID = 0
		awaitingGameChoicePartnerName = ""
		gameChoiceTimeoutActive = false

		a.AddLogMsg(fmt.Sprintf("[GAME_CHOICE_TIMEOUT] final timeout for %s after %d reminders; auto-finalizing Keep", player, reminderCount))
		sendShout(fmt.Sprintf("No response from %q — finalizing Keep and attempting payout.", player))
		time.Sleep(1200 * time.Millisecond)

		go a.finalizeRiskKeep()
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
				sendShout("Shortage unresolved; closing trade")
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
				sendShout("Trade still over limit; closing now.")
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
	dealerOpenMsg := (&App{}).dealerOpenMessage()
	if a != nil {
		dealerOpenMsg = a.dealerOpenMessage()
	}
	addLog := func(msg string) {
		if a != nil {
			a.AddLogMsg(msg)
		}
	}

	// Capture configured announce interval at start.
	dealerOpenMu.Lock()
	announceSecs := dealerAnnounceSeconds
	dealerOpenMu.Unlock()
	emitNext := func(secsAway int, nextAt string) {
		if a != nil && a.ctx != nil {
			b, _ := json.Marshal(map[string]interface{}{"secsAway": secsAway, "nextAt": nextAt})
			runtime.EventsEmit(a.ctx, "dealerOpenNextUpdate", string(b))
		}
	}

	go func(id int, openMsg string, secs int) {
		defer emitNext(0, "")

		base := time.Duration(secs) * time.Second
		if base < time.Second {
			base = time.Second
		}

		for {
			if id != dealerOpenHeartbeatID {
				dealerOpenHeartbeatActive = false
				return
			}
			if !awaitingTradeOpen || !dealerTradeWindowOpen {
				dealerOpenHeartbeatActive = false
				return
			}

			// Add jitter to dealer-open shout cadence (not trade window timeout).
			jitterWindow := base / 5
			jitter := time.Duration(rand.Int63n(int64(jitterWindow)*2+1)) - jitterWindow
			wait := base + jitter
			if wait < time.Second {
				wait = time.Second
			}
			nextAt := time.Now().Add(wait).Format("15:04:05")
			emitNext(int(wait.Seconds()), nextAt)

			timer := time.NewTimer(wait)
			<-timer.C

			if id != dealerOpenHeartbeatID {
				dealerOpenHeartbeatActive = false
				return
			}
			if !awaitingTradeOpen || !dealerTradeWindowOpen {
				dealerOpenHeartbeatActive = false
				return
			}
			if !dealerDiceReady() {
				addLog(fmt.Sprintf("[TRADE_REOPEN] dice not ready; stopping jittered announcer (~%ds)", secs))
				log.Printf("[TRADE_REOPEN] dice not ready; stopping jittered announcer (~%ds)", secs)
				dealerTradeWindowOpen = false
				dealerOpenHeartbeatActive = false
				return
			}

			if isMuted {
				addLog(fmt.Sprintf("[TRADE_REOPEN] jittered dealer-open announcer skipped due to mute (wait=%ds)", int(wait.Seconds())))
				log.Printf("[TRADE_REOPEN] jittered dealer-open announcer skipped due to mute (wait=%ds)", int(wait.Seconds()))
			} else {
				addLog(fmt.Sprintf("[TRADE_REOPEN] jittered dealer-open announcer firing (base=%ds wait=%ds)", secs, int(wait.Seconds())))
				log.Printf("[TRADE_REOPEN] jittered dealer-open announcer firing (base=%ds wait=%ds)", secs, int(wait.Seconds()))
				sendMessageWithDelay(openMsg)
			}
		}
	}(id, dealerOpenMsg, announceSecs)
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
	// Determine partner name robustly: prefer explicit payout target,
	// then fall back to lastTradePartnerName and finally try short
	// lookups by trade id / room entity.
	partnerName := strings.TrimSpace(payoutTargetName)
	if partnerName == "" {
		partnerName = normalizeUsername(strings.TrimSpace(lastTradePartnerName))
	} else {
		partnerName = normalizeUsername(partnerName)
	}
	if partnerName == "" || partnerName == "Unknown" {
		if lastTradePartnerID > 0 {
			if name, ok := waitForUsers28TradeIDName(lastTradePartnerID, 700*time.Millisecond); ok {
				partnerName = normalizeUsername(strings.TrimSpace(name))
			} else if user, ok := lookupUsers28UserByTradeID(lastTradePartnerID); ok {
				partnerName = normalizeUsername(strings.TrimSpace(user.Username))
			} else if name, ok := lookupRoomEntityNameByIndex(lastTradePartnerID); ok {
				partnerName = normalizeUsername(strings.TrimSpace(name))
			}
		}
	}
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
		// Enforce a short cooldown to avoid rapid repeated dealer-open shouts
		// when reopen cycles happen in quick succession.
		now := time.Now()
		allowed := lastDealerOpenShoutAt.IsZero() || now.Sub(lastDealerOpenShoutAt) > tradeShoutCooldown
		if allowed {
			lastDealerOpenShoutAt = now
			a.AddLogMsg(fmt.Sprintf("[TRADE_REOPEN] shouting: %q (%s)", openMsg, reason))
			go sendMessageWithDelay(openMsg)
		} else {
			a.AddLogMsg(fmt.Sprintf("[TRADE_REOPEN] skipping dealer-open shout due to cooldown (%s)", reason))
		}
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

	// Defensive: clear lingering risk session state so reopened dealer does
	// not accept stale `rN` commands from a previous round.
	mutex.Lock()
	riskSessionActive = false
	riskInitialized = false
	riskSnapshotTaken = false
	riskHandSnapshot = nil
	riskHandSnapshotReady = false
	dealerSnapshotQty = 0
	dealerRisk = 0
	playerRisk = 0
	riskPendingBet = 0
	riskSessionGame = ""
	riskSessionParams = nil
	riskPayoutRequired = nil
	riskPayoutActive = false
	riskSessionPayoutMultiplier = 2
	mutex.Unlock()

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

	// Move hand refresh to background so dealer opens immediately without waiting
	go func() {
		a.forceRefreshHandSnapshot("openDealerAfterRound")
		if shouldRefreshRoomUsers() {
			requestRoomUsers(a)
		}
		// Refresh dedicated risk snapshot to reflect post-payout inventory.
		if isRiskEnabled {
			time.Sleep(250 * time.Millisecond)
			a.captureRiskSnapshot(true)
		}
	}()

	dealerResyncInProgress = false
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

	// Persist completed strip scan for later inspection
	go LogEvent("hand_scan", map[string]interface{}{"pages_scanned": pagesScanned, "reason": reason, "items": items}, fmt.Sprintf("Strip scan complete (pages=%d reason=%s)", pagesScanned, reason), nil)
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
	// Do not send live-dealer webhook until the casino/dice setup is fully ready.
	mutex.Lock()
	ready := casinoReady
	mutex.Unlock()
	if !ready {
		a.AddLogMsg("[LIVE_DEALER_STATUS] skipped send: casino not ready")
		return
	}

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
			RiskEnabled:        isRiskEnabled,
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

// PostDealerOpenAnnouncement posts a manual dealer-open announcement to one
// of two configured Discord webhook channels. Supports optional room image and
// optional raffle block with item name + item image.
func (a *App) PostDealerOpenAnnouncement(
	channel string,
	roomGame string,
	imageDataURL string,
	imageFileName string,
	raffleEnabled bool,
	raffleItemName string,
	raffleImageDataURL string,
	raffleImageFileName string,
) string {
	var webhookURL string
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case "general":
		webhookURL = "https://discordapp.com/api/webhooks/1500410995716395038/-F9mhhM4YA9rDIDYkmhAqzChZrHiLTCP0BWg1QtYRRCQdej0uedKHcH-QImUZUnzKwBA"
	case "dice-gamesd":
		webhookURL = "https://discordapp.com/api/webhooks/1500411179821170738/qbb-w4LcFR3WmOVYLigTmk0DtCOiQcEjf3CUxRM-wIAoPO_CbUnLJyT2OXQQYFNUYBHq"
	default:
		return "invalid channel"
	}

	room := strings.TrimSpace(roomGame)
	if room == "" {
		room = strings.TrimSpace(a.getCurrentRoomName())
	}
	if room == "" {
		room = "Unknown room"
	}

	dealer := strings.TrimSpace(a.getCurrentDealerName())
	if dealer == "" {
		dealer = "Dealer"
	}

	now := time.Now().UTC()

	headline := fmt.Sprintf("🎉 DEALER OPEN! 🎉 %s is LIVE! 🎲", room)
	content := headline

	baseEmbed := map[string]interface{}{
		"title":       "🎁 WHAT'S LIVE RIGHT NOW 🎁",
		"description": fmt.Sprintf("🔥 **%s**\n🎲 **Dealer:** %s\n🚀 Come and bet now before the room gets packed!", room, dealer),
		"color":       16763955,
		"fields": []map[string]interface{}{
			{"name": "🎯 Status", "value": "🟢 Open and accepting bets", "inline": true},
			{"name": "🎮 Room/Game", "value": room, "inline": true},
			{"name": "👤 Dealer", "value": dealer, "inline": true},
		},
		"footer": map[string]interface{}{
			"text": "✨ Live dealer update • Jump in now",
		},
		"timestamp": now.Format(time.RFC3339),
	}

	embeds := []interface{}{baseEmbed}

	if raffleEnabled {
		item := strings.TrimSpace(raffleItemName)
		if item == "" {
			item = "Mystery prize"
		}

		raffleEmbed := map[string]interface{}{
			"title":       "🎁 FREE DRAW TO WIN! 🎁",
			"description": fmt.Sprintf("🔥 If you come and bet, you go into a free draw to WIN: **%s**\n\n📢 Please check the raffle channel for updates, end dates, and who is in the draw.", item),
			"color":       16744448,
			"fields": []map[string]interface{}{
				{"name": "🏆 Prize Item", "value": item, "inline": false},
				{"name": "🎟️ How To Enter", "value": "Come and place a bet to be entered into the draw.", "inline": false},
				{"name": "📌 Important", "value": "Check the raffle channel for updates, end dates, and entries.", "inline": false},
			},
			"footer": map[string]interface{}{
				"text": "Raffle entries and status are tracked in the raffle channel",
			},
			"timestamp": now.Format(time.RFC3339),
		}
		embeds = append(embeds, raffleEmbed)
		content = headline + fmt.Sprintf(" 🎁 FREE DRAW: %s", item)
	}

	payload := map[string]interface{}{
		"username": "roll-origins",
		"content":  content,
		"embeds":   embeds,
		"allowed_mentions": map[string]interface{}{
			"parse": []string{},
		},
	}

	client := &http.Client{Timeout: 8 * time.Second}
	hasRoomImage := strings.TrimSpace(imageDataURL) != ""
	hasRaffleImage := raffleEnabled && strings.TrimSpace(raffleImageDataURL) != ""
	hasAnyImage := hasRoomImage || hasRaffleImage

	if !hasAnyImage {
		jb, err := json.Marshal(payload)
		if err != nil {
			return "marshal error: " + err.Error()
		}

		req, err := http.NewRequest("POST", webhookURL, bytes.NewReader(jb))
		if err != nil {
			return "request error: " + err.Error()
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return "post error: " + err.Error()
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			b, _ := io.ReadAll(resp.Body)
			return fmt.Sprintf("discord status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
		}

		a.AddLogMsg("[DEALER_POST] sent dealer-open announcement to " + channel)
		return "ok"
	}

	type uploadFile struct {
		Field string
		Name  string
		Bytes []byte
	}
	uploads := make([]uploadFile, 0, 2)

	if hasRoomImage {
		raw, mimeType, err := decodeImageDataURL(imageDataURL)
		if err != nil {
			return "image decode error: " + err.Error()
		}
		fileName := strings.TrimSpace(imageFileName)
		if fileName == "" {
			switch mimeType {
			case "image/jpeg":
				fileName = "dealer-open.jpg"
			case "image/gif":
				fileName = "dealer-open.gif"
			case "image/webp":
				fileName = "dealer-open.webp"
			default:
				fileName = "dealer-open.png"
			}
		}
		baseEmbed["image"] = map[string]interface{}{"url": "attachment://" + fileName}
		uploads = append(uploads, uploadFile{Field: "files[0]", Name: fileName, Bytes: raw})
	}

	if hasRaffleImage {
		raw, mimeType, err := decodeImageDataURL(raffleImageDataURL)
		if err != nil {
			return "raffle image decode error: " + err.Error()
		}
		fileName := strings.TrimSpace(raffleImageFileName)
		if fileName == "" {
			switch mimeType {
			case "image/jpeg":
				fileName = "raffle-item.jpg"
			case "image/gif":
				fileName = "raffle-item.gif"
			case "image/webp":
				fileName = "raffle-item.webp"
			default:
				fileName = "raffle-item.png"
			}
		}
		if raffleEnabled && len(embeds) > 1 {
			if raffleEmbed, ok := embeds[1].(map[string]interface{}); ok {
				raffleEmbed["image"] = map[string]interface{}{"url": "attachment://" + fileName}
			}
		}
		field := "files[1]"
		if len(uploads) == 0 {
			field = "files[0]"
		}
		uploads = append(uploads, uploadFile{Field: field, Name: fileName, Bytes: raw})
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "marshal error: " + err.Error()
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writer.WriteField("payload_json", string(payloadJSON)); err != nil {
		return "multipart payload error: " + err.Error()
	}

	for _, f := range uploads {
		part, err := writer.CreateFormFile(f.Field, f.Name)
		if err != nil {
			return "multipart file error: " + err.Error()
		}
		if _, err := part.Write(f.Bytes); err != nil {
			return "multipart write error: " + err.Error()
		}
	}

	if err := writer.Close(); err != nil {
		return "multipart close error: " + err.Error()
	}

	req, err := http.NewRequest("POST", webhookURL, &body)
	if err != nil {
		return "request error: " + err.Error()
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := client.Do(req)
	if err != nil {
		return "post error: " + err.Error()
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Sprintf("discord status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	a.AddLogMsg("[DEALER_POST] sent dealer-open announcement with image content to " + channel)
	return "ok"
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
	// Persist frozen hand snapshot for later inspection
	go LogEvent("hand_snapshot", snapshot, fmt.Sprintf("[TRADE_HAND_SNAPSHOT] captured (items=%d)", len(snapshot)), nil)
	// Send snapshot to configured live-dealer webhook (non-blocking)
	a.sendLiveDealerSnapshot(snapshot)

	// One-time risk snapshot capture if Risk mode enabled
	a.initRiskSnapshotIfNeeded()

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

// initRiskSnapshotIfNeeded captures the dealer's one-time hand quantity
// snapshot when Risk mode is enabled. It is intentionally idempotent.
func (a *App) initRiskSnapshotIfNeeded() {
	// Delegate to the explicit risk snapshot capturer. Non-forced.
	_ = a.captureRiskSnapshot(false)
}

// captureRiskSnapshot copies a one-shot risk snapshot from the frozen trade
// snapshot. If force==true it will replace any existing risk snapshot (used
// after payouts or explicit refreshes). Returns true on success.
func (a *App) captureRiskSnapshot(force bool) bool {
	if !isRiskEnabled {
		return false
	}

	mutex.Lock()
	already := riskHandSnapshotReady
	mutex.Unlock()
	if already && !force {
		return true
	}

	// Wait briefly for frozen snapshot to be available.
	deadline := time.Now().Add(2 * time.Second)
	for {
		handItemsMu.Lock()
		ready := tradeHandSnapshotReady && len(tradeHandSnapshot) > 0
		if ready {
			snap := make([]TradeItem, len(tradeHandSnapshot))
			copy(snap, tradeHandSnapshot)
			handItemsMu.Unlock()

			mutex.Lock()
			if !riskHandSnapshotReady || force {
				riskHandSnapshot = snap
				riskHandSnapshotReady = true
				total := 0
				for _, it := range snap {
					total += it.Quantity
				}
				dealerSnapshotQty = total
				riskSnapshotTaken = true
			}
			mutex.Unlock()

			a.AddLogMsg(fmt.Sprintf("[RISK] captured risk snapshot total=%d types=%d force=%t", dealerSnapshotQty, len(snap), force))
			return true
		}
		handItemsMu.Unlock()

		if time.Now().After(deadline) {
			if force {
				// Fall back to live hand if forced.
				handItemsMu.Lock()
				snap := make([]TradeItem, len(currentHandItems))
				copy(snap, currentHandItems)
				handItemsMu.Unlock()

				mutex.Lock()
				riskHandSnapshot = snap
				riskHandSnapshotReady = true
				total := 0
				for _, it := range snap {
					total += it.Quantity
				}
				dealerSnapshotQty = total
				riskSnapshotTaken = true
				mutex.Unlock()

				a.AddLogMsg(fmt.Sprintf("[RISK] forced capture from live hand total=%d types=%d", dealerSnapshotQty, len(snap)))
				return true
			}
			a.AddLogMsg("[RISK] captureRiskSnapshot timed out waiting for frozen snapshot")
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// absoluteMaxRisk returns the maximum single-risk amount allowed by
// configured per-item limits and current internal bank quantities.
func absoluteMaxRisk() int {
	mutex.Lock()
	defer mutex.Unlock()
	m := maxTradeQuantityPerItem
	if playerRisk < m {
		m = playerRisk
	}
	if dealerRisk < m {
		m = dealerRisk
	}
	return m
}

// playerBankMaxRisk returns the maximum single-risk amount limited by the
// current internal banks (player and dealer). This excludes per-item limits
// which are shown separately in dealer messages.
func playerBankMaxRisk() int {
	mutex.Lock()
	defer mutex.Unlock()
	m := playerRisk
	if dealerRisk < m {
		m = dealerRisk
	}
	if m < 0 {
		return 0
	}
	return m
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

		// If a pending forced variant was set while shortages existed, clear it
		// now that coverage has been restored so UO7 choices may be offered again.
		mutex.Lock()
		pendingUoVariant = ""
		mutex.Unlock()
		a.AddLogMsg("[TRADE_COVERAGE] cleared pendingUoVariant due to restored coverage")

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

	// If UnderOver7 mode is enabled and the current shortages are only for
	// the UO7 multiplier (but a normal 2x payout is still possible), do NOT
	// close the trade. Instead, send a per-item public warning, set the
	// pending variant to force Over/Under, and keep the trade open.
	if underOver7GameModeEnabled {
		handItemsMu.Lock()
		ready := tradeHandSnapshotReady
		var handSnapshot []TradeItem
		if ready {
			handSnapshot = make([]TradeItem, len(tradeHandSnapshot))
			copy(handSnapshot, tradeHandSnapshot)
		}
		handItemsMu.Unlock()

		tradeItemsMu.Lock()
		partnerItems := make([]TradeItem, len(currentTradeItems))
		copy(partnerItems, currentTradeItems)
		tradeItemsMu.Unlock()

		if ready && len(partnerItems) > 0 {
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

			mutex.Lock()
			multUO7 := underOver7PayoutMultiplier
			mutex.Unlock()

			requiredUO7 := map[string]int{}
			requiredX2 := map[string]int{}
			for _, it := range partnerItems {
				key := strings.ToLower(strings.TrimSpace(it.Name))
				if k, ok := normalizeClassKeyWithVariant(it.Name); ok {
					key = k
				}
				if it.Quantity <= 0 {
					continue
				}
				requiredUO7[key] += it.Quantity * multUO7
				requiredX2[key] += it.Quantity * 2
			}

			shortageUO7 := false
			shortageX2 := false
			for name, req := range requiredUO7 {
				have := handMap[name] + incomingMap[name]
				if have < req {
					shortageUO7 = true
					break
				}
			}
			for name, req := range requiredX2 {
				have := handMap[name] + incomingMap[name]
				if have < req {
					shortageX2 = true
					break
				}
			}

			// UO7-only shortage: warn and force Over/Under (do not close trade)
			if shortageUO7 && !shortageX2 {
				parts := []string{}
				seen := map[string]bool{}
				for _, it := range partnerItems {
					key := strings.ToLower(strings.TrimSpace(it.Name))
					if k, ok := normalizeClassKeyWithVariant(it.Name); ok {
						key = k
					}
					if seen[key] {
						continue
					}
					seen[key] = true
					avail := handMap[key] + incomingMap[key]
					coverable := avail / multUO7
					parts = append(parts, fmt.Sprintf("%s %d/%d", formatTradeItemName(key), coverable, incomingMap[key]))
				}
				msg := fmt.Sprintf("I can only cover Over or Under. I can cover: %s", strings.Join(parts, "; "))

				changed := msg != lastTradeCoverageNotice
				lastTradeCoverageNotice = msg
				lastTradeBlockNotice = msg

				now := time.Now()
				shouldShout := changed && (lastTradeCoverageShoutAt.IsZero() || now.Sub(lastTradeCoverageShoutAt) > tradeShoutCooldown)
				if shouldShout {
					lastTradeCoverageShoutAt = now
					go func(m string) {
						time.Sleep(350 * time.Millisecond)
						sendShout(m)
					}(msg)
				} else {
					a.AddLogMsg("[TRADE_COVERAGE] UO7-only shout suppressed by cooldown")
				}

				// Prevent 7 selection and force Over/Under when the round starts
				pendingUoVariant = "uo"
				a.noteCurrentGameHistory("UO7 coverage insufficient - warned partner and forced Over/Under")
				a.AddLogMsg("[TRADE_COVERAGE] UO7-only shortage: keeping trade open and forcing Over/Under")
				return
			}
		}
	}

	// No grace timer now — close immediately with a contextual message.
	stopShortageMonitor()

	// Build a human-friendly shortage message: distinguish "none available"
	// from "insufficient quantity" and show hand/incoming counts.
	var msg string
	if len(shortages) == 1 {
		s := shortages[0]
		if s.HaveHand == 0 && s.Incoming == 0 {
			msg = fmt.Sprintf("No %s available", formatTradeItemName(s.Name))
		} else {
			msg = fmt.Sprintf("Short %s %d/%d", formatTradeItemName(s.Name), s.Have, s.Required)
		}
	} else {
		parts := make([]string, 0, len(shortages))
		for _, s := range shortages {
			if s.HaveHand == 0 && s.Incoming == 0 {
				parts = append(parts, fmt.Sprintf("no %s", formatTradeItemName(s.Name)))
			} else {
				parts = append(parts, fmt.Sprintf("%s %d/%d", formatTradeItemName(s.Name), s.Have, s.Required))
			}
		}
		msg = fmt.Sprintf("Short: %s", strings.Join(parts, "; "))
	}

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
			sendShout(m)
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

	// required payout quantities: use configured UO7 multiplier when dealer-level
	// UnderOver7 game-mode is enabled (worst-case coverage for a shouted "7"),
	// otherwise 2x. If we've already forced Over/Under for this trade
	// (pendingUoVariant == "uo"), compute requirements as 2x so coverage
	// checks accept Over/Under offers.
	mult := 2
	if underOver7GameModeEnabled {
		mutex.Lock()
		mult = underOver7PayoutMultiplier
		// treat forced Over/Under as 2x
		if pendingUoVariant == "uo" {
			mult = 2
		}
		mutex.Unlock()
	}
	required := payoutRequirementsFromBetItemsMult(partnerItems, mult)
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
		requiredCanon[key] += it.Quantity * mult
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

	mutex.Lock()
	msg := buildGameChoicePromptLocked()
	mutex.Unlock()
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

	a.AddLogMsg(fmt.Sprintf("[TRADE_MESSAGE] shouting: %q", msg))
	sendShout(msg)
}

func setEnabledGamesFromSelection(codes []string) {
	// Defaults preserve historical behavior when no selection is provided.
	enabledGamePkr = true
	enabledGame21 = true
	enabledGame13 = true
	enabledGameTri = true
	enabledGameUO7 = false

	if len(codes) == 0 {
		return
	}

	enabledGamePkr = false
	enabledGame21 = false
	enabledGame13 = false
	enabledGameTri = false
	enabledGameUO7 = false

	for _, raw := range codes {
		s := strings.ToLower(strings.TrimSpace(raw))
		s = gameChoiceCleanupRe.ReplaceAllString(s, "")
		switch s {
		case "pkr", "poker":
			enabledGamePkr = true
		case "21":
			enabledGame21 = true
		case "13":
			enabledGame13 = true
		case "tri", "trih", "tril", "trihigh", "trilow":
			enabledGameTri = true
		case "uo", "uo7", "underover", "underover7":
			enabledGameUO7 = true
		}
	}

	if !enabledGamePkr && !enabledGame21 && !enabledGame13 && !enabledGameTri && !enabledGameUO7 {
		enabledGamePkr = true
		enabledGame21 = true
		enabledGame13 = true
		enabledGameTri = true
	}
}

func enabledGameChoicePartsLocked() []string {
	parts := make([]string, 0, 5)
	if enabledGamePkr {
		parts = append(parts, "pkr")
	}
	if enabledGame21 {
		parts = append(parts, "21")
	}
	if enabledGame13 {
		parts = append(parts, "13")
	}
	if enabledGameTri {
		parts = append(parts, "tri")
	}
	if enabledGameUO7 {
		parts = append(parts, "uo7")
	}
	if len(parts) == 0 {
		parts = append(parts, "pkr", "21", "13", "tri")
	}
	return parts
}

func buildGameChoicePromptLocked() string {
	if underOver7GameModeEnabled {
		m := underOver7PayoutMultiplier
		if pendingUoVariant == "uo" {
			return "Shout U (2-6), O (8-12) to DOUBLE!"
		}
		return fmt.Sprintf("Shout U (2-6), O (8-12) to DOUBLE! or 7 to WIN x%d!", m)
	}
	if onlyUnderOver7Mode {
		return "Shout U (2-6) or O (8-12) to DOUBLE!"
	}
	return "Shout " + strings.Join(enabledGameChoicePartsLocked(), ", ")
}

func isGameChoiceEnabledLocked(choice string) bool {
	switch choice {
	case "pkr":
		return enabledGamePkr
	case "21":
		return enabledGame21
	case "13":
		return enabledGame13
	case "tri", "trihigh", "trilow":
		return enabledGameTri
	case "uo", "uo7", "uo_over", "uo_under":
		return enabledGameUO7
	default:
		return false
	}
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

	// If configured, block incoming FAVOURITEROOMRESULTS (header 61).
	blockFavouriteRoomResultsMu.Lock()
	blockFav := blockFavouriteRoomResults
	blockFavouriteRoomResultsMu.Unlock()
	if blockFav {
		name := ext.Headers().Name(e.Packet.Header)
		if strings.Contains(strings.ToLower(name), "favourit") || e.Packet.Header.Value == 61 {
			a.AddLogMsg(fmt.Sprintf("[BLOCK] blocking incoming favourite-room-results packet [%d:%s]", e.Packet.Header.Value, name))
			e.Block()
			return
		}
	}

	// If configured, block incoming ARTICLES_PAGE (header 681).
	blockArticlesPageMu.Lock()
	blockArticles := blockArticlesPage
	blockArticlesPageMu.Unlock()
	if blockArticles {
		name := ext.Headers().Name(e.Packet.Header)
		if strings.Contains(strings.ToLower(name), "articles_page") || e.Packet.Header.Value == 681 {
			a.AddLogMsg(fmt.Sprintf("[BLOCK] blocking incoming articles-page packet [%d:%s]", e.Packet.Header.Value, name))
			e.Block()
			return
		}
	}

	// If configured, block incoming CALENDAR_EVENTS (header 683).
	blockCalendarEventsMu.Lock()
	blockCal := blockCalendarEvents
	blockCalendarEventsMu.Unlock()
	if blockCal {
		name := ext.Headers().Name(e.Packet.Header)
		if strings.Contains(strings.ToLower(name), "calendar_events") || e.Packet.Header.Value == 683 {
			a.AddLogMsg(fmt.Sprintf("[BLOCK] blocking incoming calendar-events packet [%d:%s]", e.Packet.Header.Value, name))
			e.Block()
			return
		}
	}

	// If configured, block incoming USER_BANNED (header 35).
	blockUserBannedMu.Lock()
	blockUserB := blockUserBanned
	blockUserBannedMu.Unlock()
	if blockUserB {
		name := ext.Headers().Name(e.Packet.Header)
		if strings.Contains(strings.ToLower(name), "user_banned") || e.Packet.Header.Value == 35 {
			a.AddLogMsg(fmt.Sprintf("[BLOCK] blocking incoming user-banned packet [%d:%s]", e.Packet.Header.Value, name))
			e.Block()
			return
		}
	}

	// If configured, block incoming raw 4095 packet (header 4095).
	blockIncoming4095Mu.Lock()
	block4095 := blockIncoming4095
	blockIncoming4095Mu.Unlock()
	if block4095 {
		name := ext.Headers().Name(e.Packet.Header)
		if strings.Contains(strings.ToLower(name), "rb") || e.Packet.Header.Value == 4095 {
			a.AddLogMsg(fmt.Sprintf("[BLOCK] blocking incoming 4095 packet [%d:%s]", e.Packet.Header.Value, name))
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

	// If configured, block outgoing GET_PAGE_ARTICLES (header 680).
	blockGetPageArticlesOutgoingMu.Lock()
	outGetArticles := blockGetPageArticlesOutgoing
	blockGetPageArticlesOutgoingMu.Unlock()
	if outGetArticles {
		name := ext.Headers().Name(e.Packet.Header)
		if strings.Contains(strings.ToLower(name), "get_page_articles") || e.Packet.Header.Value == 680 {
			a.AddLogMsg(fmt.Sprintf("[BLOCK] blocking outgoing get-page-articles packet [%d:%s]", e.Packet.Header.Value, name))
			e.Block()
			return
		}
	}

	// If configured, block outgoing GET_CALENDAR_EVENTS (header 682).
	blockGetCalendarEventsOutgoingMu.Lock()
	outGetCalendar := blockGetCalendarEventsOutgoing
	blockGetCalendarEventsOutgoingMu.Unlock()
	if outGetCalendar {
		name := ext.Headers().Name(e.Packet.Header)
		if strings.Contains(strings.ToLower(name), "get_calendar_events") || e.Packet.Header.Value == 682 {
			a.AddLogMsg(fmt.Sprintf("[BLOCK] blocking outgoing get-calendar-events packet [%d:%s]", e.Packet.Header.Value, name))
			e.Block()
			return
		}
	}

	// If configured, block outgoing FRIENDLIST_UPDATE (header 15).
	blockFriendListUpdateOutgoingMu.Lock()
	outFriendOutgoing := blockFriendListUpdateOutgoing
	blockFriendListUpdateOutgoingMu.Unlock()
	if outFriendOutgoing {
		name := ext.Headers().Name(e.Packet.Header)
		if strings.Contains(strings.ToLower(name), "friendlist") || e.Packet.Header.Value == 15 {
			a.AddLogMsg(fmt.Sprintf("[BLOCK] blocking outgoing friendlist-update packet [%d:%s]", e.Packet.Header.Value, name))
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
	// Persist outgoing chat for audit/analysis
	go LogEvent("chat_outgoing", map[string]interface{}{"text": msg}, "Outgoing chat", nil)
	commandMsg := extractInlineCommand(msg)

	// Process commands based on the message prefix and suffix
	if strings.HasPrefix(commandMsg, ":") {
		// Check if already rolling or closing
		if isPokerRolling || isTriRolling || isBJRolling || is13Rolling || isUORolling || isHitting || is13Hitting || isClosing {
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
	sendShout(at)
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

	go func() {
		// Wait the same total delay previously used (700 + 700ms) before
		// starting the player's roll; the public shout was already sent above.
		time.Sleep(1400 * time.Millisecond)
		a.AddLogMsg("[GAME_SELECT] starting player roll")
		a.startPokerRoll()
	}()
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

	go func() {
		// Combined ack already announced; delay then start player's BJ roll
		time.Sleep(1400 * time.Millisecond)
		isBJRolling = true
		a.AddLogMsg("21 Roll:\n")
		go a.rollBjDice()
	}()
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

	go func() {
		// Combined ack already announced; delay then start player's 13 roll
		time.Sleep(1400 * time.Millisecond)
		is13Rolling = true
		a.AddLogMsg("13 Roll:\n")
		go a.roll13Dice()
	}()
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
	sendShout(msg)
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

	go func() {
		// Combined ack already announced; delay then start player's Tri roll
		time.Sleep(1400 * time.Millisecond)
		isTriRolling = true
		a.rollTriDice()
	}()
}

// beginUOChoiceSequence prompts the player to choose Over or Under for the Under/Over-7 game.
func (a *App) beginUOChoiceSequence() {
	playerName := strings.TrimSpace(lastTradePartnerName)
	if playerName == "" {
		playerName = "Player"
	}

	resetPokerSequence()
	resetBlackjackSequence()
	reset13Sequence()
	resetTriSequence()

	awaitingUOChoice = true
	awaitingUOChoicePartnerName = playerName

	if chatIdx, ok := lookupRoomEntityIndexByName(playerName); ok && chatIdx > 0 {
		awaitingUOChoicePartnerID = chatIdx
	} else if chatIdx, ok := waitForUsers28RoomIndexByName(playerName, 900*time.Millisecond); ok && chatIdx > 0 {
		awaitingUOChoicePartnerID = chatIdx
	} else if chatIdx, ok := lookupUsers28RoomIndexByName(playerName); ok && chatIdx > 0 {
		awaitingUOChoicePartnerID = chatIdx
	} else {
		awaitingUOChoicePartnerID = lastTradePartnerID
	}

	msg := "Shout U (1-6) or O (8-12)? - Just shout U or O!"
	a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] shouting: %q", msg))
	sendShout(msg)
}

// beginUO7ChoiceSequence prompts the player to choose Over, Under or 7
// for the UnderOver7 variant (allows a 3x payout if player picks 7 and wins).
func (a *App) beginUO7ChoiceSequence() {
	playerName := strings.TrimSpace(lastTradePartnerName)
	if playerName == "" {
		playerName = "Player"
	}
	// If a forced Over/Under variant is already in effect (pendingUoVariant=="uo"),
	// do not offer the 7 option — fall back to the 2x Over/Under prompt.
	if pendingUoVariant == "uo" {
		a.AddLogMsg("[GAME_SELECT] beginUO7ChoiceSequence suppressed; pendingUoVariant==\"uo\" — forcing Over/Under prompt")
		a.beginUOChoiceSequence()
		return
	}

	resetPokerSequence()
	resetBlackjackSequence()
	reset13Sequence()
	resetTriSequence()

	awaitingUOChoice = true
	awaitingUOChoicePartnerName = playerName
	// Mark pending variant so beginUnderOverRound knows to treat the round as UO7
	pendingUoVariant = "uo7"

	if chatIdx, ok := lookupRoomEntityIndexByName(playerName); ok && chatIdx > 0 {
		awaitingUOChoicePartnerID = chatIdx
	} else if chatIdx, ok := waitForUsers28RoomIndexByName(playerName, 900*time.Millisecond); ok && chatIdx > 0 {
		awaitingUOChoicePartnerID = chatIdx
	} else if chatIdx, ok := lookupUsers28RoomIndexByName(playerName); ok && chatIdx > 0 {
		awaitingUOChoicePartnerID = chatIdx
	} else {
		awaitingUOChoicePartnerID = lastTradePartnerID
	}

	msg := "UO7 selected. Shout U (1-6), O (8-12), or 7."
	a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] shouting: %q", msg))
	sendShout(msg)
}

// beginUnderOverRound starts the Under/Over-7 round with the given player choice: "over" or "under".
func (a *App) beginUnderOverRound(mode string) {
	playerName := strings.TrimSpace(lastTradePartnerName)
	if playerName == "" {
		playerName = "Player"
	}

	resetPokerSequence()
	resetBlackjackSequence()
	reset13Sequence()
	resetTriSequence()

	uoRoundActive = true
	// Normalize incoming mode (accept "7" as valid choice)
	choice := strings.ToLower(strings.TrimSpace(mode))
	if choice == "seven" {
		choice = "7"
	}

	// Defensive: prevent starting a "7" round when UO7 coverage is insufficient.
	// If UO7 is short but normal x2 is coverable, force Over/Under and re-prompt.
	if choice == "7" {
		// If we already forced Over/Under, re-prompt Over/Under.
		if pendingUoVariant == "uo" {
			a.AddLogMsg("[GAME_SELECT] partner attempted '7' but pendingUoVariant==\"uo\"; re-prompting Over/Under")
			a.beginUOChoiceSequence()
			return
		}

		handItemsMu.Lock()
		ready := tradeHandSnapshotReady
		var handSnapshot []TradeItem
		if ready {
			handSnapshot = make([]TradeItem, len(tradeHandSnapshot))
			copy(handSnapshot, tradeHandSnapshot)
		}
		handItemsMu.Unlock()

		tradeItemsMu.Lock()
		partnerItems := make([]TradeItem, len(currentTradeItems))
		copy(partnerItems, currentTradeItems)
		tradeItemsMu.Unlock()

		if ready && len(partnerItems) > 0 {
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

			mutex.Lock()
			multUO7 := underOver7PayoutMultiplier
			mutex.Unlock()

			shortageUO7 := false
			shortageX2 := false
			for _, it := range partnerItems {
				key := strings.ToLower(strings.TrimSpace(it.Name))
				if k, ok := normalizeClassKeyWithVariant(it.Name); ok {
					key = k
				}
				if it.Quantity <= 0 {
					continue
				}
				if handMap[key]+incomingMap[key] < it.Quantity*multUO7 {
					shortageUO7 = true
				}
				if handMap[key]+incomingMap[key] < it.Quantity*2 {
					shortageX2 = true
				}
				if shortageUO7 && shortageX2 {
					break
				}
			}

			// If only UO7 is short, force UO (2x), shout concise cover counts and re-prompt.
			if shortageUO7 && !shortageX2 {
				parts := []string{}
				seen := map[string]bool{}
				for _, it := range partnerItems {
					key := strings.ToLower(strings.TrimSpace(it.Name))
					if k, ok := normalizeClassKeyWithVariant(it.Name); ok {
						key = k
					}
					if seen[key] {
						continue
					}
					seen[key] = true
					avail := handMap[key] + incomingMap[key]
					coverable := avail / multUO7
					parts = append(parts, fmt.Sprintf("%s %d/%d", formatTradeItemName(key), coverable, incomingMap[key]))
				}
				msg := fmt.Sprintf("I can only cover Over or Under. I can cover: %s", strings.Join(parts, "; "))

				changed := msg != lastTradeCoverageNotice
				lastTradeCoverageNotice = msg
				lastTradeBlockNotice = msg
				now := time.Now()
				shouldShout := changed && (lastTradeCoverageShoutAt.IsZero() || now.Sub(lastTradeCoverageShoutAt) > tradeShoutCooldown)
				if shouldShout {
					lastTradeCoverageShoutAt = now
					go func(m string) {
						time.Sleep(350 * time.Millisecond)
						sendShout(m)
					}(msg)
				} else {
					a.AddLogMsg("[GAME_SELECT] UO7-only shout suppressed by cooldown")
				}

				pendingUoVariant = "uo"
				a.noteCurrentGameHistory("UO7 coverage prevented starting 7 - forced Over/Under")
				a.AddLogMsg("[GAME_SELECT] forced Over/Under; re-prompting partner")
				a.beginUOChoiceSequence()
				return
			}
		}
	}

	uoPlayerChoice = choice
	// Establish variant for this round: prefer any pending variant set when prompting,
	// otherwise fall back to dealer-level configuration.
	if pendingUoVariant != "" {
		uoVariantForRound = pendingUoVariant
		pendingUoVariant = ""
	} else if underOver7GameModeEnabled {
		uoVariantForRound = "uo7"
	} else {
		uoVariantForRound = "uo"
	}
	gameLabel := "UO"
	if uoVariantForRound == "uo7" {
		gameLabel = "UO7"
	}
	a.setCurrentGameHistoryGame(gameLabel)

	go func() {
		time.Sleep(1400 * time.Millisecond)
		isUORolling = true
		a.rollUnderOverDice()
	}()
}

// rollUnderOverDice rolls the two configured dice (slots 1 and 5 -> indices 0 and 4).
func (a *App) rollUnderOverDice() {
	// choose indices: prefer [0,4] when a full 5-dice setup exists,
	// otherwise use the first two slots [0,1] when only 2 dice are configured.
	if fakeDiceTestingMode {
		mutex.Lock()
		// determine indices based on current diceList length
		if len(diceList) < 2 {
			mutex.Unlock()
			a.AddLogMsg("[UO] Not enough dice to roll")
			isUORolling = false
			return
		}
		currentSum = 0
		indices := []int{0, 1}
		if len(diceList) >= 5 {
			indices = []int{0, 4}
		}
		for _, index := range indices {
			diceList[index].Value = rand.Intn(6) + 1
			diceList[index].IsClosed = false
			currentSum += diceList[index].Value
			a.AddLogMsg(fmt.Sprintf("Dice %d rolled: %d", diceList[index].ID, diceList[index].Value))
		}
		mutex.Unlock()
		a.evaluateUnderOverRound()
		isUORolling = false
		return
	}

	mutex.Lock()
	// pick indices depending on how many dice are available
	var indices []int
	if len(diceList) >= 5 {
		indices = []int{0, 4}
	} else if len(diceList) >= 2 {
		indices = []int{0, 1}
	}
	if len(indices) < 2 {
		mutex.Unlock()
		a.AddLogMsg("[UO] Not enough dice to roll")
		isUORolling = false
		return
	}
	resultsWaitGroup.Add(len(indices))
	mutex.Unlock()

	for _, index := range indices {
		diceList[index].Roll()
		time.Sleep(rollDelay + time.Duration(rand.Intn(100))*time.Millisecond)
	}

	time.Sleep(1000 * time.Millisecond)
	resultsWaitGroup.Wait()

	a.evaluateUnderOverRound()
	isUORolling = false
}

// evaluateUnderOverRound computes the result of the Under/Over-7 roll and resolves payout.
func (a *App) evaluateUnderOverRound() {
	mutex.Lock()
	mutex.Unlock()
	if !uoRoundActive {
		isUORolling = false
		return
	}

	total := 0
	if len(diceList) >= 5 {
		total = diceList[0].Value + diceList[4].Value
	} else if len(diceList) >= 2 {
		total = diceList[0].Value + diceList[1].Value
	}
	a.AddLogMsg(fmt.Sprintf("[UO] evaluating total=%d playerChoice=%s", total, uoPlayerChoice))

	// Default payout multiplier is 2; may be 3 for UO7 when player chose "7" and won.
	mult := 2
	playerWins := false
	// Consider variant selected for this round. If the round variant is "uo7"
	// then the player may choose "7" for a triple payout.
	if uoVariantForRound == "uo7" {
		if uoPlayerChoice == "7" {
			playerWins = (total == 7)
			if playerWins {
				mutex.Lock()
				mult = underOver7PayoutMultiplier
				mutex.Unlock()
			}
		} else {
			// Standard over/under behaviour; 7 is a dealer win unless player picked 7.
			if total == 7 {
				playerWins = false
			} else if total < 7 {
				playerWins = (uoPlayerChoice == "under")
			} else {
				playerWins = (uoPlayerChoice == "over")
			}
		}
	} else {
		// Legacy behaviour: dealer always wins on a 7
		if total == 7 {
			playerWins = false
		} else if total < 7 {
			playerWins = (uoPlayerChoice == "under")
		} else {
			playerWins = (uoPlayerChoice == "over")
		}
	}

	// Persist multiplier for payout routines that will auto-add items.
	payoutMultiplierForRound = mult
	// Record multiplier in game history so UI/webhooks reflect the correct payout
	a.setCurrentGameHistoryPayoutMultiplier(mult)

	playerName := strings.TrimSpace(lastTradePartnerName)
	if playerName == "" {
		playerName = "Player"
	}
	winnerName := "Dealer"
	if playerWins {
		winnerName = playerName
	}
	winnerMsg := fmt.Sprintf("%s Wins - %s: %d", winnerName, playerName, total)

	a.AddLogMsg(fmt.Sprintf("[UO_RULES] winner=%s total=%d choice=%s", winnerName, total, uoPlayerChoice))
	if !ChatIsDisabled {
		waitForUnmute(90 * time.Second)
		time.Sleep(800 * time.Millisecond)
		sendMessageWithDelay(winnerMsg)
	}

	payoutTargetID := lastTradePartnerID
	payoutTargetName := playerName
	uoRoundActive = false

	// clear any awaiting choice state and mark the UO round finished
	awaitingUOChoice = false
	awaitingUOChoicePartnerID = 0
	awaitingUOChoicePartnerName = ""

	if playerWins && payoutTargetID > 0 {
		a.setCurrentGameHistoryResults(strconv.Itoa(total), "", playerName, "Payout Pending", false)
		a.noteCurrentGameHistory(winnerMsg)
		resetPayoutRetryState()
		// Never route Under/Over-7 rounds into Risk (UO7 is auto-payout only).
		if isRiskEnabled && uoVariantForRound != "uo7" {
			if riskSessionActive {
				// Post the round outcome immediately so Discord shows who won this roll.
				a.sendDiscordRoundResult(playerName, strconv.Itoa(total), "", winnerMsg)
				go a.applyRiskOutcome(true)
				return
			}
			params := map[string]interface{}{"uoChoice": uoPlayerChoice}
			riskGame := "UO"
			if uoVariantForRound == "uo7" {
				riskGame = "UO7"
			}
			go a.handlePlayerWinRisk(cloneTradeItems(gameBetItems), payoutTargetName, payoutTargetID, riskGame, params)
			// Post round outcome so Discord shows the win even while risk prompt is pending.
			a.sendDiscordRoundResult(playerName, strconv.Itoa(total), "", winnerMsg)
			return
		}
		startPayout(a, payoutTargetID, payoutTargetName)
		return
	}

	a.setCurrentGameHistoryResults(strconv.Itoa(total), "", a.getCurrentDealerName(), "Completed", true)
	a.noteCurrentGameHistory(winnerMsg)

	// If a risk session is active, route this loss through the risk logic
	// so only the pending bet is lost and the player can be re-prompted.
	// Do NOT route UO7 rounds into Risk.
	if isRiskEnabled && riskSessionActive && uoVariantForRound != "uo7" {
		go a.applyRiskOutcome(false)
		return
	}

	// Ensure the winner message is delivered before reopening the dealer
	go func() {
		time.Sleep(1200 * time.Millisecond)
		a.openDealerAfterRound()
	}()
}

func (a *App) start13DealerTurn(reason string) {
	awaiting13Decision = false
	thirteenPlayerTurn = false
	a.AddLogMsg(fmt.Sprintf("[13_DEBUG] dealer turn starting reason=%s playerTotal=%d dealerTotal=%d", reason, thirteenPlayerTotal, thirteenDealerTotal))
	a.AddLogMsg("[GAME_SELECT] starting dealer roll")
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
		if isRiskEnabled {
			if riskSessionActive {
				// Post the round outcome immediately so Discord shows who won this roll.
				a.sendDiscordRoundResult(playerName, playerHand, dealerHand, winnerMsg)
				go a.applyRiskOutcome(true)
				return
			}
			go a.handlePlayerWinRisk(cloneTradeItems(gameBetItems), payoutTargetName, payoutTargetID, "13", nil)
			// Post round outcome so Discord shows the win even while risk prompt is pending.
			a.sendDiscordRoundResult(playerName, playerHand, dealerHand, winnerMsg)
			return
		}
		startPayout(a, payoutTargetID, payoutTargetName)
		return
	}

	a.setCurrentGameHistoryResults(playerHand, dealerHand, a.getCurrentDealerName(), "Completed", true)
	a.noteCurrentGameHistory(winnerMsg)
	if isRiskEnabled && riskSessionActive {
		go a.applyRiskOutcome(false)
		return
	}
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
		if isRiskEnabled {
			if riskSessionActive {
				// Post the round outcome immediately so Discord shows who won this roll.
				a.sendDiscordRoundResult(playerName, playerHand, dealerHand, winnerMsg)
				go a.applyRiskOutcome(true)
				return
			}
			params := map[string]interface{}{"mode": triMode}
			go a.handlePlayerWinRisk(cloneTradeItems(gameBetItems), payoutTargetName, payoutTargetID, "Tri", params)
			// Post round outcome so Discord shows the win even while risk prompt is pending.
			a.sendDiscordRoundResult(playerName, playerHand, dealerHand, winnerMsg)
			return
		}
		startPayout(a, payoutTargetID, payoutTargetName)
		return
	}

	a.setCurrentGameHistoryResults(playerHand, dealerHand, "Dealer", "Completed", true)
	a.noteCurrentGameHistory(winnerMsg)
	if isRiskEnabled && riskSessionActive {
		go a.applyRiskOutcome(false)
		return
	}
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

// getExpectedDiceCount returns the number of dice required for setup based on
// the current dealer mode. Under/Over-7 mode requires only 2 dice; otherwise
// the default is 5.
func getExpectedDiceCount() int {
	// Use 2 dice when either 'only under/over' dealer mode is enabled
	// or when the separate UnderOver7 game-mode (7-for-3x) is enabled.
	if onlyUnderOver7Mode || underOver7GameModeEnabled {
		return 2
	}
	return 5
}

// StartCasinoSetup enables dice setup recording. This must be called from the frontend
// when the user clicks the "Start Casino" button. It resets any existing dice and
// enables recording of incoming dice IDs. It also receives trade-limit configuration
// values which are stored in global state and emitted in live-dealer payloads.
func (a *App) StartCasinoSetup(dealerName string, roomName string, maxUniqueItems int, maxQuantityPerItem int, riskEnabled bool, enabledGames []string) {
	// Reset state first (this will lock/unlock internally)
	resetDiceState()

	mutex.Lock()
	setEnabledGamesFromSelection(enabledGames)
	selectedPrompt := strings.Join(enabledGameChoicePartsLocked(), ", ")
	mutex.Unlock()
	a.AddLogMsg(fmt.Sprintf("[CONFIG] enabled games = %s", selectedPrompt))

	// Apply risk mode flag from frontend
	mutex.Lock()
	isRiskEnabled = riskEnabled
	if riskEnabled {
		// clear any previous risk session metadata so the new session is clean
		riskInitialized = false
		riskSnapshotTaken = false
		// clear any dedicated risk snapshot
		riskHandSnapshot = nil
		riskHandSnapshotReady = false
		dealerSnapshotQty = 0
		dealerRisk = 0
		playerRisk = 0
		riskSessionActive = false
		riskSessionGame = ""
		riskSessionParams = nil
		riskPendingBet = 0
		riskPartnerID = 0
		riskPartnerName = ""
		riskPayoutRequired = nil
		riskPayoutActive = false
	}
	mutex.Unlock()

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

	a.AddLogMsg(fmt.Sprintf("[DICE_SETUP] Dice setup mode enabled - roll all %d dice now", getExpectedDiceCount()))
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
	complete := len(diceList) >= getExpectedDiceCount()
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
	expected := getExpectedDiceCount()
	diceList = make([]*Dice, 0, expected)
	for i := 1; i <= expected; i++ {
		diceList = append(diceList, &Dice{ID: 100000 + i, Value: rand.Intn(6) + 1, IsRolling: false, IsClosed: false})
	}
	fakeDiceTestingMode = true
	casinoReady = true
	mutex.Unlock()

	a.AddLogMsg(fmt.Sprintf("Dice setup bypass enabled for testing. Using %d fake dice values.", expected))
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

	// If not found and the list has fewer than the expected dice, create and add a new one
	expected := getExpectedDiceCount()
	if existingDice == nil && len(diceList) < expected {
		if !diceSetupActive {
			// Not in setup mode - ignore new dice for setup purposes
			mutex.Unlock()
			return
		}

		newDice := &Dice{ID: diceID, IsRolling: true, IsClosed: false}
		diceList = append(diceList, newDice)
		log.Printf("Dice %d added\n", diceID)
		needEmit = true

		if len(diceList) == expected {
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
	expected := getExpectedDiceCount()
	if existingDice == nil && len(diceList) < expected {
		if !diceSetupActive {
			mutex.Unlock()
			return
		}
		newDice := &Dice{ID: diceID, IsRolling: false, IsClosed: true}
		diceList = append(diceList, newDice)
		log.Printf("Dice %d added\n", diceID)
		needEmit = true
		if len(diceList) == expected {
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
	if len(diceData)%2 != 0 {
		a.AddLogMsg(fmt.Sprintf("[DICE_PARSE] odd DICE_VALUE field count=%d payload=%q", len(diceData), rawData))
	}

	type diceResultPair struct {
		diceID   int
		rawValue int
		adjValue int
	}

	pairs := make([]diceResultPair, 0, len(diceData)/2)
	for i := 0; i+1 < len(diceData); i += 2 {
		diceIDStr := diceData[i]
		diceID, err := strconv.Atoi(diceIDStr)
		if err != nil {
			logrus.WithFields(logrus.Fields{"dice_id_str": diceIDStr, "error": err}).Warn("Failed to parse dice ID")
			continue
		}
		rememberDiceID(diceID)

		diceValueStr := diceData[i+1]
		diceValue, err := strconv.Atoi(diceValueStr)
		if err != nil {
			logrus.WithFields(logrus.Fields{"dice_value_str": diceValueStr, "error": err}).Warn("Failed to parse dice value")
			continue
		}

		pairs = append(pairs, diceResultPair{
			diceID:   diceID,
			rawValue: diceValue,
			adjValue: diceValue - (diceID * 38),
		})
	}

	if len(pairs) == 0 {
		return
	}

	needEmit := false
	mutex.Lock()
	for _, pair := range pairs {
		for i, dice := range diceList {
			if dice.ID == pair.diceID {
				if dice.IsRolling && (isPokerRolling || isTriRolling || isBJRolling || is13Rolling || is13Hitting || isHitting || isUORolling) {
					dice.IsRolling = false
					func() {
						defer func() {
							if r := recover(); r != nil {
								a.AddLogMsg(fmt.Sprintf("[DICE_SYNC_GUARD] recovered from resultsWaitGroup.Done panic for dice %d: %v", pair.diceID, r))
							}
						}()
						resultsWaitGroup.Done()
					}()
				}
				diceList[i].Value = pair.adjValue
				diceList[i].IsClosed = diceList[i].Value == 0

				if isPokerRolling || isTriRolling || isBJRolling || is13Rolling || is13Hitting || isHitting || isUORolling {
					log.Printf("Dice %d rolled: %d\n", pair.diceID, pair.adjValue)
					logRollResult := fmt.Sprintf("Dice %d rolled: %d\n", pair.diceID, pair.adjValue)
					a.AddLogMsg(logRollResult)
					// Persist dice result for later inspection
					go LogEvent("dice_result", map[string]interface{}{"dice_id": pair.diceID, "value": pair.adjValue}, logRollResult, map[string]string{"source": "handleDiceResult"})
				}
				needEmit = true
				break
			}
		}
	}
	mutex.Unlock()

	if needEmit {
		a.emitDiceSetupUpdate()

		// Update readiness: when we have the expected recorded dice with non-zero values
		mutex.Lock()
		expected := getExpectedDiceCount()
		ready := len(diceList) >= expected
		if ready {
			for i := 0; i < expected; i++ {
				if i >= len(diceList) {
					ready = false
					break
				}
				d := diceList[i]
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
		expected := getExpectedDiceCount()
		if len(diceList) < expected {
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
	sendShout(sumStr)
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
	full := fmt.Sprintf("%s: %v", msg, err)
	a.AddLogMsg(full)
	log.Printf("[ERROR] %s: %v", msg, err)
	// Record to persistent event log for later review
	go LogEvent("error", map[string]interface{}{"message": msg, "error": err.Error()}, full, nil)
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
		go LogEvent("chat_incoming", map[string]interface{}{"type": chatType, "sender": senderName, "index": index, "text": msg}, fmt.Sprintf("Incoming %s from %s", chatType, senderName), nil)
	} else {
		log.Printf("[INCOMING %s] %d -> %s", chatType, index, msg)
		a.AddChatLog(fmt.Sprintf("[IN %s] %d -> %s", chatType, index, msg))
		go LogEvent("chat_incoming", map[string]interface{}{"type": chatType, "sender_index": index, "text": msg}, fmt.Sprintf("Incoming %s (index=%d)", chatType, index), nil)
	}

	// Risk command parsing: case-insensitive, supports "r2", "r 2", "risk 2", and "keep"
	if riskSessionActive {
		// Ensure sender is the current risk partner
		isPartner := false
		if riskPartnerID > 0 && index == riskPartnerID {
			isPartner = true
		}
		if riskPartnerName != "" && strings.EqualFold(senderName, riskPartnerName) {
			isPartner = true
		}
		if isPartner {
			cleaned := strings.TrimSpace(msg)
			lower := strings.ToLower(cleaned)
			if strings.EqualFold(lower, "keep") {
				e.Block()
				go a.finalizeRiskKeep()
				return
			}
			re := regexp.MustCompile(`(?i)^\s*(?:r|risk)\s*?(\d+)\s*$`)
			if m := re.FindStringSubmatch(msg); len(m) == 2 {
				amt, _ := strconv.Atoi(m[1])
				e.Block()
				go a.handleRiskBet(amt, senderName)
				return
			}
		}
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

	if awaitingUOChoice {
		cleaned := strings.ToLower(strings.TrimSpace(msg))
		cleaned = gameChoiceCleanupRe.ReplaceAllString(cleaned, "")
		// Accept over, under, short forms o/u/o7/u7/over7/under7, or 7 (allow "seven" too)
		if cleaned != "over" && cleaned != "under" && cleaned != "o" && cleaned != "u" && cleaned != "o7" && cleaned != "u7" && cleaned != "over7" && cleaned != "under7" && cleaned != "7" && cleaned != "seven" {
			a.AddLogMsg(fmt.Sprintf("[UO_DEBUG] awaiting UO choice from %q(index=%d), ignored non-choice message=%q", awaitingUOChoicePartnerName, awaitingUOChoicePartnerID, msg))
			return
		}

		indexMatch := awaitingUOChoicePartnerID > 0 && index == awaitingUOChoicePartnerID
		nameMatch := awaitingUOChoicePartnerName != "" && strings.EqualFold(senderName, awaitingUOChoicePartnerName)
		if !indexMatch && !nameMatch && awaitingUOChoicePartnerName != "" {
			if expectedIdx, ok := lookupRoomEntityIndexByName(awaitingUOChoicePartnerName); ok && expectedIdx > 0 && expectedIdx == index {
				indexMatch = true
			}
		}
		if !indexMatch && !nameMatch && awaitingUOChoicePartnerName != "" {
			if expectedIdx, ok := lookupUsers28RoomIndexByName(awaitingUOChoicePartnerName); ok && expectedIdx > 0 && expectedIdx == index {
				indexMatch = true
			}
		}

		if !indexMatch && !nameMatch {
			a.AddLogMsg(fmt.Sprintf("[UO] ignoring choice %q from %q (index %d); waiting for %q (index %d)", cleaned, senderName, index, awaitingUOChoicePartnerName, awaitingUOChoicePartnerID))
			return
		}

		e.Block()
		// Normalize "seven" -> "7" early for checks
		if cleaned == "seven" {
			cleaned = "7"
		}

		// Accept shorthand letters: 'o'/'o7'/'over7' -> over, 'u'/'u7'/'under7' -> under
		if cleaned == "o" || cleaned == "o7" || cleaned == "over7" {
			cleaned = "over"
		} else if cleaned == "u" || cleaned == "u7" || cleaned == "under7" {
			cleaned = "under"
		}

		// If player selected "7", verify UO7 coverage; if UO7 is short but
		// normal x2 is still possible, do NOT allow a 7 selection. Warn
		// publicly with per-item coverable counts and keep the UO choice
		// locked so the player may reply with Over/Under.
		if cleaned == "7" {
			handItemsMu.Lock()
			ready := tradeHandSnapshotReady
			var handSnapshot []TradeItem
			if ready {
				handSnapshot = make([]TradeItem, len(tradeHandSnapshot))
				copy(handSnapshot, tradeHandSnapshot)
			}
			handItemsMu.Unlock()

			tradeItemsMu.Lock()
			partnerItems := make([]TradeItem, len(currentTradeItems))
			copy(partnerItems, currentTradeItems)
			tradeItemsMu.Unlock()

			if ready && len(partnerItems) > 0 {
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

				multUO7 := underOver7PayoutMultiplier
				requiredUO7 := map[string]int{}
				requiredX2 := map[string]int{}
				for _, it := range partnerItems {
					key := strings.ToLower(strings.TrimSpace(it.Name))
					if k, ok := normalizeClassKeyWithVariant(it.Name); ok {
						key = k
					}
					if it.Quantity <= 0 {
						continue
					}
					requiredUO7[key] += it.Quantity * multUO7
					requiredX2[key] += it.Quantity * 2
				}

				shortageUO7 := false
				shortageX2 := false
				for name, req := range requiredUO7 {
					have := handMap[name] + incomingMap[name]
					if have < req {
						shortageUO7 = true
						break
					}
				}
				for name, req := range requiredX2 {
					have := handMap[name] + incomingMap[name]
					if have < req {
						shortageX2 = true
						break
					}
				}

				if shortageUO7 && !shortageX2 {
					parts := []string{}
					seen := map[string]bool{}
					for _, it := range partnerItems {
						key := strings.ToLower(strings.TrimSpace(it.Name))
						if k, ok := normalizeClassKeyWithVariant(it.Name); ok {
							key = k
						}
						if seen[key] {
							continue
						}
						seen[key] = true
						avail := handMap[key] + incomingMap[key]
						coverable := avail / multUO7
						parts = append(parts, fmt.Sprintf("%s %d/%d", formatTradeItemName(key), coverable, incomingMap[key]))
					}
					msg := fmt.Sprintf("I can only cover Over or Under. I can cover: %s", strings.Join(parts, "; "))
					changed := msg != lastTradeCoverageNotice
					lastTradeCoverageNotice = msg
					lastTradeBlockNotice = msg
					now := time.Now()
					shouldShout := changed && (lastTradeCoverageShoutAt.IsZero() || now.Sub(lastTradeCoverageShoutAt) > tradeShoutCooldown)
					if shouldShout {
						lastTradeCoverageShoutAt = now
						time.Sleep(350 * time.Millisecond)
						sendShout(msg)
					} else {
						a.AddLogMsg("[TRADE_COVERAGE] UO7-only shout suppressed by cooldown")
					}

					pendingUoVariant = "uo"
					a.noteCurrentGameHistory("UO7 coverage prevented 7 selection; partner asked to choose Over/Under")
					a.AddLogMsg("[UO_DEBUG] prevented 7 choice due to UO7 coverage")
					// Keep awaitingUOChoice true so the partner can now reply with Over/Under.
					return
				}
			}
		}

		// Persist acceptance and continue as normal
		// If the partner attempted to choose '7' but we previously forced
		// Over/Under (pendingUoVariant=="uo"), reject the '7' and re-prompt
		// so the partner can reply with Over/Under only.
		if cleaned == "7" && pendingUoVariant == "uo" {
			// Ignore '7' selections when Over/Under has been forced; do not
			// acknowledge or accept the shout so the player must choose Over/Under.
			return
		}
		awaitingUOChoice = false
		a.AddLogMsg(fmt.Sprintf("[UO_DEBUG] accepted choice=%q from sender=%q index=%d (expectedName=%q expectedIndex=%d)", cleaned, senderName, index, awaitingUOChoicePartnerName, awaitingUOChoicePartnerID))
		// If this UO choice is being made as part of an active risk session,
		// record the selected choice and multiplier on the risk session and
		// execute the risk roll path instead of starting a normal round.
		if riskSessionActive {
			variant := "uo"
			if pendingUoVariant != "" {
				variant = pendingUoVariant
				pendingUoVariant = ""
			} else if underOver7GameModeEnabled {
				variant = "uo7"
			}
			gameLabel := "UO"
			if variant == "uo7" {
				gameLabel = "UO7"
			}
			mutex.Lock()
			riskSessionGame = gameLabel
			riskSessionParams = map[string]interface{}{"uoChoice": cleaned}
			if cleaned == "7" && variant == "uo7" {
				riskSessionPayoutMultiplier = underOver7PayoutMultiplier
			} else {
				riskSessionPayoutMultiplier = 2
			}
			mutex.Unlock()

			a.beginRiskRoundHistory(cleaned, msg, gameLabel)
			ack := fmt.Sprintf("%s! Starting, Player Roll", gameChoiceDisplay("uo7"))
			a.AddLogMsg(fmt.Sprintf("[UO_DEBUG] risk re-roll choice=%q variant=%s mult=%d; executing risk roll", cleaned, variant, riskSessionPayoutMultiplier))
			sendShout(ack)
			go func() {
				time.Sleep(1400 * time.Millisecond)
				a.executeRiskRound()
			}()
			return
		}

		// Record normalized choice and raw shout into game history
		a.setCurrentGameHistoryChoice(cleaned, msg)

		if cleaned == "over" {
			a.beginUnderOverRound("over")
		} else if cleaned == "under" {
			a.beginUnderOverRound("under")
		} else {
			a.beginUnderOverRound("7")
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
		if riskSessionActive {
			gameLabel := "TriL"
			if cleaned == "high" {
				gameLabel = "TriH"
			}
			a.beginRiskRoundHistory(cleaned, msg, gameLabel)
		} else {
			// Record normalized choice and raw shout into game history
			a.setCurrentGameHistoryChoice(cleaned, msg)
		}

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
		// Quick path: if UO7 mode active and player directly shouted "7"/"seven",
		// accept it as an immediate Under/Over-7 selection and start the round.
		cleanedChoice := strings.ToLower(strings.TrimSpace(msg))
		cleanedChoice = gameChoiceCleanupRe.ReplaceAllString(cleanedChoice, "")
		if underOver7GameModeEnabled && (cleanedChoice == "7" || cleanedChoice == "seven") {
			// verify sender matches expected trade starter
			indexMatch := awaitingGameChoicePartnerID > 0 && index == awaitingGameChoicePartnerID
			nameMatch := awaitingUOChoicePartnerName != "" && strings.EqualFold(senderName, awaitingGameChoicePartnerName)
			nameMatch = awaitingGameChoicePartnerName != "" && strings.EqualFold(senderName, awaitingGameChoicePartnerName)
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
				a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] ignoring %q from %q (index %d); waiting for locked starter %q (index %d trade_id=%d)", cleanedChoice, senderName, index, awaitingGameChoicePartnerName, awaitingGameChoicePartnerID, tradeStarterTradeID))
				return
			}

			e.Block()

			if isPokerRolling || isTriRolling || isBJRolling || is13Rolling || isHitting || is13Hitting || isClosing {
				a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] %q selected but dice are busy", cleanedChoice))
				return
			}

			// Check UO7 coverage vs normal x2. If only UO7 is short, warn and force Over/Under.
			handItemsMu.Lock()
			ready := tradeHandSnapshotReady
			var handSnapshot []TradeItem
			if ready {
				handSnapshot = make([]TradeItem, len(tradeHandSnapshot))
				copy(handSnapshot, tradeHandSnapshot)
			}
			handItemsMu.Unlock()

			tradeItemsMu.Lock()
			partnerItems := make([]TradeItem, len(currentTradeItems))
			copy(partnerItems, currentTradeItems)
			tradeItemsMu.Unlock()

			if ready && len(partnerItems) > 0 {
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

				multUO7 := underOver7PayoutMultiplier
				requiredUO7 := map[string]int{}
				requiredX2 := map[string]int{}
				for _, it := range partnerItems {
					key := strings.ToLower(strings.TrimSpace(it.Name))
					if k, ok := normalizeClassKeyWithVariant(it.Name); ok {
						key = k
					}
					if it.Quantity <= 0 {
						continue
					}
					requiredUO7[key] += it.Quantity * multUO7
					requiredX2[key] += it.Quantity * 2
				}

				shortageUO7 := false
				shortageX2 := false
				for name, req := range requiredUO7 {
					have := handMap[name] + incomingMap[name]
					if have < req {
						shortageUO7 = true
						break
					}
				}
				for name, req := range requiredX2 {
					have := handMap[name] + incomingMap[name]
					if have < req {
						shortageX2 = true
						break
					}
				}

				if shortageUO7 && !shortageX2 {
					parts := []string{}
					seen := map[string]bool{}
					for _, it := range partnerItems {
						key := strings.ToLower(strings.TrimSpace(it.Name))
						if k, ok := normalizeClassKeyWithVariant(it.Name); ok {
							key = k
						}
						if seen[key] {
							continue
						}
						seen[key] = true
						avail := handMap[key] + incomingMap[key]
						coverable := avail / multUO7
						parts = append(parts, fmt.Sprintf("%s %d/%d", formatTradeItemName(key), coverable, incomingMap[key]))
					}

					msg := fmt.Sprintf("I can only cover Over or Under. I can cover: %s", strings.Join(parts, "; "))
					changed := msg != lastTradeCoverageNotice
					lastTradeCoverageNotice = msg
					lastTradeBlockNotice = msg
					now := time.Now()
					shouldShout := changed && (lastTradeCoverageShoutAt.IsZero() || now.Sub(lastTradeCoverageShoutAt) > tradeShoutCooldown)
					if shouldShout {
						lastTradeCoverageShoutAt = now
						time.Sleep(350 * time.Millisecond)
						sendShout(msg)
					} else {
						a.AddLogMsg("[TRADE_COVERAGE] UO7-only shout suppressed by cooldown")
					}

					pendingUoVariant = "uo"
					a.noteCurrentGameHistory("UO7 coverage insufficient - switched to Over/Under")
					a.AddLogMsg("[GAME_SELECT] falling back to Over/Under choice due to UO7 coverage")
					a.beginUOChoiceSequence()
					return
				}
			}

			stopGameChoiceTimeoutMonitor()
			awaitingGameChoice = false
			gameChoiceUnreadableWarned = false
			awaitingGameChoicePartnerID = 0
			awaitingGameChoicePartnerName = ""

			// If this selection is part of an active risk session, record the
			// chosen variant and multiplier on the risk session and execute
			// the risk roll path instead of starting a normal round.
			if riskSessionActive {
				variant := "uo"
				if pendingUoVariant != "" {
					variant = pendingUoVariant
					pendingUoVariant = ""
				} else if underOver7GameModeEnabled {
					variant = "uo7"
				}
				gameLabel := "UO"
				if variant == "uo7" {
					gameLabel = "UO7"
				}
				mutex.Lock()
				riskSessionGame = gameLabel
				riskSessionParams = map[string]interface{}{"uoChoice": "7"}
				if variant == "uo7" {
					riskSessionPayoutMultiplier = underOver7PayoutMultiplier
				} else {
					riskSessionPayoutMultiplier = 2
				}
				mutex.Unlock()

				a.beginRiskRoundHistory("7", msg, gameLabel)
				ack := fmt.Sprintf("%s! Starting, Player Roll", gameChoiceDisplay("uo7"))
				a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] risk re-roll selected -> 7 (variant=%s mult=%d); executing risk roll", variant, riskSessionPayoutMultiplier))
				sendShout(ack)
				go func() {
					time.Sleep(1400 * time.Millisecond)
					a.executeRiskRound()
				}()
				return
			}

			// Record the immediate selection and raw shout into game history
			a.setCurrentGameHistoryChoice("7", msg)

			// If we have previously forced Over/Under for this trade, do not
			// acknowledge a shouted '7' with a "Starting" ack. Instead,
			// re-prompt Over/Under so the player must choose U/O explicitly.
			mutex.Lock()
			pv := pendingUoVariant
			mutex.Unlock()
			if pv == "uo" {
				a.AddLogMsg("[GAME_SELECT] received '7' but pendingUoVariant==\"uo\"; re-prompting Over/Under without acknowledging '7'")
				a.beginUOChoiceSequence()
				return
			}

			ack := fmt.Sprintf("%s! Starting, Player Roll", gameChoiceDisplay("uo7"))
			a.setCurrentGameHistoryGame(gameChoiceDisplay("uo7"))
			a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] shouting: %q", ack))
			sendShout(ack)

			a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] %d selected Under/Over7 -> 7; starting round", index))
			a.beginUnderOverRound("7")
			return
		}

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
				sendShout(warn)
			}
		}
		return
	}

	// Standalone UO modes: while waiting for initial game choice, only allow
	// U/O/7-related choices. Ignore other game selections (e.g. 13, 21, pkr, tri).
	if choice == "uo_over" || choice == "uo_under" {
		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] coerced shorthand %q into UO7 game selection; prompting for final U/O/7 choice", choice))
		choice = "uo7"
	}

	if underOver7GameModeEnabled || onlyUnderOver7Mode {
		switch choice {
		case "uo", "uo7", "uo_over", "uo_under":
			// allowed as-is
		default:
			a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] ignoring non-UO choice %q because standalone UO mode is enabled", choice))
			return
		}
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

	if !underOver7GameModeEnabled && !onlyUnderOver7Mode {
		mutex.Lock()
		enabled := isGameChoiceEnabledLocked(choice)
		prompt := buildGameChoicePromptLocked()
		mutex.Unlock()
		if !enabled {
			a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] ignoring disabled choice %q; allowed prompt: %q", choice, prompt))
			sendShout("That game is disabled. " + prompt)
			return
		}
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

	// For Tri and Under/Over variants (two-step selection) we must first ask
	// High/Low or Over/Under before starting the round.
	if choice != "tri" && choice != "uo" && choice != "uo7" {
		// Combine the standard "Starting" ack with the player-roll prompt
		ack := fmt.Sprintf("%s! Starting, Player Roll", gameChoiceDisplay(choice))
		if riskSessionActive {
			a.beginRiskRoundHistory(choice, msg, gameChoiceDisplay(choice))
		} else {
			// Record normalized choice and raw shout into game history
			a.setCurrentGameHistoryChoice(choice, msg)
			a.setCurrentGameHistoryGame(gameChoiceDisplay(choice))
		}
		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] shouting: %q", ack))
		sendShout(ack)
	} else {
		if choice == "tri" {
			a.AddLogMsg("[GAME_SELECT] Tri selected; prompting for High/Low instead of immediate Lets Play")
		} else {
			a.AddLogMsg("[GAME_SELECT] Under/Over selected; prompting for Over/Under instead of immediate Lets Play")
		}
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
	case "uo7":
		// Two-step Under/Over-7 selection:
		// - standalone UO7 mode allows Over/Under/7
		// - mixed mode forces standard Over/Under only (no "7")
		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] %d selected Under/Over7; checking mode and coverage", index))

		if !underOver7GameModeEnabled {
			pendingUoVariant = "uo"
			a.noteCurrentGameHistory("Mixed-mode UO7 selected - forcing Over/Under (no 7)")
			a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] %d selected Under/Over7 in mixed mode; prompting Over/Under only", index))
			a.beginUOChoiceSequence()
			break
		}

		handItemsMu.Lock()
		ready := tradeHandSnapshotReady
		var handSnapshot []TradeItem
		if ready {
			handSnapshot = make([]TradeItem, len(tradeHandSnapshot))
			copy(handSnapshot, tradeHandSnapshot)
		}
		handItemsMu.Unlock()

		tradeItemsMu.Lock()
		partnerItems := make([]TradeItem, len(currentTradeItems))
		copy(partnerItems, currentTradeItems)
		tradeItemsMu.Unlock()

		if ready && len(partnerItems) > 0 {
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

			multUO7 := underOver7PayoutMultiplier
			requiredUO7 := map[string]int{}
			requiredX2 := map[string]int{}
			for _, it := range partnerItems {
				key := strings.ToLower(strings.TrimSpace(it.Name))
				if k, ok := normalizeClassKeyWithVariant(it.Name); ok {
					key = k
				}
				if it.Quantity <= 0 {
					continue
				}
				requiredUO7[key] += it.Quantity * multUO7
				requiredX2[key] += it.Quantity * 2
			}

			shortageUO7 := false
			shortageX2 := false
			for name, req := range requiredUO7 {
				have := handMap[name] + incomingMap[name]
				if have < req {
					shortageUO7 = true
					break
				}
			}
			for name, req := range requiredX2 {
				have := handMap[name] + incomingMap[name]
				if have < req {
					shortageX2 = true
					break
				}
			}

			if shortageUO7 && !shortageX2 {
				parts := []string{}
				seen := map[string]bool{}
				for _, it := range partnerItems {
					key := strings.ToLower(strings.TrimSpace(it.Name))
					if k, ok := normalizeClassKeyWithVariant(it.Name); ok {
						key = k
					}
					if seen[key] {
						continue
					}
					seen[key] = true
					avail := handMap[key] + incomingMap[key]
					coverable := avail / multUO7
					parts = append(parts, fmt.Sprintf("%s %d/%d", formatTradeItemName(key), coverable, incomingMap[key]))
				}

				msg := fmt.Sprintf("I can only cover Over or Under. I can cover: %s", strings.Join(parts, "; "))
				changed := msg != lastTradeCoverageNotice
				lastTradeCoverageNotice = msg
				lastTradeBlockNotice = msg
				now := time.Now()
				shouldShout := changed && (lastTradeCoverageShoutAt.IsZero() || now.Sub(lastTradeCoverageShoutAt) > tradeShoutCooldown)
				if shouldShout {
					lastTradeCoverageShoutAt = now
					time.Sleep(350 * time.Millisecond)
					sendShout(msg)
				} else {
					a.AddLogMsg("[TRADE_COVERAGE] UO7-only shout suppressed by cooldown")
				}

				pendingUoVariant = "uo"
				a.noteCurrentGameHistory("UO7 coverage insufficient - switched to Over/Under")
				a.AddLogMsg("[GAME_SELECT] falling back to Over/Under choice due to UO7 coverage")
				a.beginUOChoiceSequence()
				break
			}
		}

		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] %d selected Under/Over7; prompting for Over/Under/7", index))
		a.beginUO7ChoiceSequence()
	case "uo":
		// Two-step Under/Over selection: prompt player for Over or Under
		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] %d selected Under/Over; prompting for Over/Under", index))
		a.beginUOChoiceSequence()
	case "uo_over":
		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] %d selected Under/Over -> Over; starting round", index))
		// If this was chosen as part of a risk session, record the risk
		// parameters and execute the risk roll instead of starting a normal
		// round.
		if riskSessionActive {
			variant := "uo"
			if underOver7GameModeEnabled {
				variant = "uo7"
			}
			gameLabel := "UO"
			if variant == "uo7" {
				gameLabel = "UO7"
			}
			mutex.Lock()
			riskSessionGame = gameLabel
			riskSessionParams = map[string]interface{}{"uoChoice": "over"}
			riskSessionPayoutMultiplier = 2
			mutex.Unlock()
			a.setCurrentGameHistoryGame(gameLabel)
			ack := fmt.Sprintf("%s! Starting, Player Roll", gameChoiceDisplay("uo7"))
			sendShout(ack)
			go func() {
				time.Sleep(1400 * time.Millisecond)
				a.executeRiskRound()
			}()
		} else {
			a.setCurrentGameHistoryGame("UO7")
			a.beginUnderOverRound("over")
		}
	case "uo_under":
		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] %d selected Under/Over -> Under; starting round", index))
		if riskSessionActive {
			variant := "uo"
			if underOver7GameModeEnabled {
				variant = "uo7"
			}
			gameLabel := "UO"
			if variant == "uo7" {
				gameLabel = "UO7"
			}
			mutex.Lock()
			riskSessionGame = gameLabel
			riskSessionParams = map[string]interface{}{"uoChoice": "under"}
			riskSessionPayoutMultiplier = 2
			mutex.Unlock()
			a.setCurrentGameHistoryGame(gameLabel)
			ack := fmt.Sprintf("%s! Starting, Player Roll", gameChoiceDisplay("uo7"))
			sendShout(ack)
			go func() {
				time.Sleep(1400 * time.Millisecond)
				a.executeRiskRound()
			}()
		} else {
			a.setCurrentGameHistoryGame("UO7")
			a.beginUnderOverRound("under")
		}
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
	case "uo", "underover":
		return "uo", true
	case "uo7", "underover7", "u7", "o7":
		return "uo7", true
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
	case "uo", "underover":
		return "uo", true
	case "uo7", "underover7", "u7", "o7":
		return "uo7", true
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
	case "uo7":
		return "UO7"
	case "uo", "uo_over", "uo_under":
		return "UO7"
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
