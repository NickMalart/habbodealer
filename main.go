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
	"math"
	"math/rand"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	stdruntime "runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
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
	lastTradePartnerChatID               int
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
	// 6-game state
	awaitingSixDecision            bool
	awaitingSixDecisionPartnerID   int
	awaitingSixDecisionPartnerName string
	sixRoundActive                 bool
	sixPlayerTurn                  bool
	sixPlayerTotal                 int
	sixDealerTotal                 int
	sixPlayerName                  string
	sixHitInFlight                 bool
	sixNextHitIndex                int
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
	// Double Trouble rolling state
	isDTRolling bool
	// Under/Over-7 state
	isUORolling    bool
	uoRoundActive  bool
	h18RoundActive bool
	uoPlayerChoice string // "over", "under" or "7"
	// When true the dealer has selected the special UnderOver7 game-mode
	// which allows a player to shout "7" for a potential 3x payout.
	underOver7GameModeEnabled bool
	// Payout multiplier selected for the current payout round.
	payoutMultiplierForRound float64 = 2.0
	// Variant marker for the active Under/Over round: "uo" or "uo7".
	uoVariantForRound string
	// Pending variant selection when prompting for Over/Under (set by beginUO7ChoiceSequence)
	pendingUoVariant            string
	onlyUnderOver7Mode          bool // when true, dealer prompts only Under/Over-7
	isSplitDealerMode           bool
	bankerName                  string
	enabledGamePkr              bool    = true
	enabledGame21               bool    = true
	enabledGame13               bool    = true
	enabledGame6                bool    = true
	enabledGameTri              bool    = true
	enabledGameDT               bool    = true
	enabledGameUO7              bool    = false
	enabledGamePairUp           bool    = true
	enabledGameH18              bool    = true
	enabledGameBandit           bool    = false
	enabledGameMH               bool    = false
	banditJackpotPayout         float64 = 20.0
	banditTriplesPayout         float64 = 5.0
	isBanditRolling             bool    = false
	banditRoundActive           bool    = false
	isMidHouseRolling           bool    = false
	midHouseRoundActive         bool    = false
	midHouseChoice              string  // "u10" or "o11"
	awaitingMHChoice            bool
	awaitingMHChoicePartnerID   int
	awaitingMHChoicePartnerName string
	pokerSequencePlayerName     string
	pokerSequencePlayerResult   PokerHandResult
	pokerSequencePlayerHand     string
	payoutActive                bool
	payoutTradeActive           bool
	payoutTargetID              int
	payoutTargetName            string
	payoutAttempts              int
	payoutSessionID             int
	payoutTradeSent             bool
	payoutExpectedAddCount      int
	payoutActualAddCount        int
	lastPayoutCancelNoticeAt    time.Time
	payoutLargePayoutThreshold  = 20
	payoutAddInterval           = 120 * time.Millisecond
	payoutProgressAnnounceEvery = 10

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
	tradeLimitWasActive bool
	lastTradeOpenData   string
	lastTradeOpen       string
	tradeOpen           bool
	messageQueue        []string
	isPokerRolling      bool
	isTriRolling        bool
	isBJRolling         bool
	is13Rolling         bool
	is13Hitting         bool
	isSixRolling        bool
	isSixHitting        bool
	isPairUpRolling     bool
	isH18Rolling        bool
	isHitting           bool
	isClosing           bool
	ChatIsDisabled      bool
	ChatMinimalMode     bool = true
	mutex               sync.Mutex

	resultsWaitGroup       sync.WaitGroup
	rollDelay              = 550 * time.Millisecond
	stripNextDelay         = 750 * time.Millisecond
	stripGetNewPayload     = "new"
	stripGetNextPayload    = "next"
	tradeUserPattern       = regexp.MustCompile(`\[(\d+)\]`)
	stripItemNameRe        = regexp.MustCompile(`(?:CF_\d+_[a-z][a-z0-9_.-]*|[a-z][a-z0-9_.-]*_[a-z0-9_.-]+)(?:\*\d+)?`)
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

	blackjackDecisionTimeoutMonitorID int
	blackjackDecisionTimeoutActive    bool

	sixDecisionTimeoutMonitorID int
	sixDecisionTimeoutActive    bool

	thirteenDecisionTimeoutMonitorID int
	thirteenDecisionTimeoutActive    bool

	triChoiceTimeoutMonitorID int
	triChoiceTimeoutActive    bool

	uoChoiceTimeoutMonitorID int
	uoChoiceTimeoutActive    bool

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
	minTradeQuantityPerItem int = 1

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
	shoutSpacingMu     sync.Mutex
	shoutReplaySpacing = 3000 * time.Millisecond

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
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
	RawName  string `json:"raw_name,omitempty"`
	Qty      int    `json:"qty,omitempty"`
	RawData  string `json:"rawData"` // Store raw field for debugging
}

type StockedItem struct {
	ID            int    `json:"id"`
	RawName       string `json:"rawName"`
	CanonicalName string `json:"canonicalName"`
	DisplayName   string `json:"displayName"`
	IsActive      bool   `json:"isActive"`
}

var (
	stockedItems         []StockedItem
	stockedItemsCache    = make(map[string]string) // raw_name -> canonical_name
	stockedCanonicalSet  = make(map[string]struct{})
	stockedItemsRegistry = make(map[string]StockedItem) // raw_name -> StockedItem
	stockedItemsMu       sync.RWMutex
)

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
	MinQuantityPerItem int               `json:"minQuantityPerItem"`
	RiskEnabled        bool              `json:"riskEnabled"`
	Snapshot           []TradeItem       `json:"snapshot,omitempty"`
	RecentGames        []LiveGameSummary `json:"recentGames,omitempty"`
}

type tradeLimitViolation struct {
	TooManyUniqueItems bool
	TooMuchQuantity    bool
	TooLittleQuantity   bool
	HasUnknownItems    bool
	UniqueCount        int
	MaxUnique          int
	MaxPerItem         int
	MinPerItem         int
	OverLimitItems     []TradeItem
	UnderLimitItems    []TradeItem
	UnknownItems       []TradeItem
}

// getTradeLimitViolation inspects the parsed list of trade items and returns
// a violation struct if limits are exceeded, or nil otherwise.
func (a *App) getTradeLimitViolation(items []TradeItem) *tradeLimitViolation {
	if len(items) == 0 {
		return nil
	}

	// Use canonical names to count unique item types correctly.
	// This ensures that goldbar and goldbar*1 are treated as the same type.
	uniqueTypes := make(map[string]struct{})
	for _, it := range items {
		uniqueTypes[a.getCanonicalName(it.Name)] = struct{}{}
	}

	v := &tradeLimitViolation{
		UniqueCount: len(uniqueTypes),
		MaxUnique:   maxTradeUniqueItems,
		MaxPerItem:  maxTradeQuantityPerItem,
		MinPerItem:  minTradeQuantityPerItem,
	}
	if v.UniqueCount > v.MaxUnique {
		v.TooManyUniqueItems = true
	}
	for _, it := range items {
		// Detect unknown items:
		// 1. Parser explicitly failed (unrecognized: prefix)
		// 2. Item name does not exist in our hand snapshot, stocked whitelist, or catalog
		isValid := isKnownTradeClassName(a, it.Name)

		if strings.HasPrefix(it.Name, "unknown_item_") || strings.HasPrefix(it.Name, "unrecognized:") || !isValid {
			v.HasUnknownItems = true
			v.UnknownItems = append(v.UnknownItems, it)
			log.Printf("[TRADE_LIMIT_DEBUG] unknown item detected: %q (not in hand snapshot or catalog)", it.Name)
		}

		over := it.Quantity > v.MaxPerItem
		under := it.Quantity < v.MinPerItem
		log.Printf("[TRADE_LIMIT_DEBUG] check item=%q qty=%d max=%d min=%d over=%t under=%t", it.Name, it.Quantity, v.MaxPerItem, v.MinPerItem, over, under)
		if over {
			v.OverLimitItems = append(v.OverLimitItems, it)
		}
		if under {
			v.UnderLimitItems = append(v.UnderLimitItems, it)
		}
	}
	if len(v.OverLimitItems) > 0 {
		v.TooMuchQuantity = true
	}
	if len(v.UnderLimitItems) > 0 {
		v.TooLittleQuantity = true
	}
	log.Printf("[TRADE_LIMIT_DEBUG] unique=%d maxUnique=%d tooManyUnique=%t tooMuchQuantity=%t tooLittleQuantity=%t hasUnknown=%t", v.UniqueCount, v.MaxUnique, v.TooManyUniqueItems, v.TooMuchQuantity, v.TooLittleQuantity, v.HasUnknownItems)
	if !v.TooManyUniqueItems && !v.TooMuchQuantity && !v.TooLittleQuantity && !v.HasUnknownItems {
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
	if v.TooLittleQuantity {
		parts = append(parts, fmt.Sprintf("min %d each", v.MinPerItem))
	}
	if v.HasUnknownItems {
		parts = append(parts, "items we don't have")
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

// rejectTradeForLimitViolation announces the reason and closes the trade immediately.
func (a *App) rejectTradeForLimitViolation(v *tradeLimitViolation) {
	if v == nil {
		return
	}
	msg := formatTradeLimitViolationMessage(v)

	now := time.Now()
	mutex.Lock()
	cooldown := now.Sub(lastTradeLimitShoutAt) < 10*time.Second
	isDuplicate := msg == lastTradeLimitNotice
	if !isDuplicate {
		lastTradeLimitNotice = msg
	}
	if !isDuplicate || !cooldown {
		lastTradeLimitShoutAt = now
		mutex.Unlock()

		a.AddLogMsg("[TRADE_LIMIT] " + msg)

		// Shout to the partner about the limit violation.
		if lastTradePartnerID > 0 {
			sendShoutTargeted(lastTradePartnerID, msg)
		} else {
			sendPublicShout(msg)
		}
	} else {
		mutex.Unlock()
	}

	// Force close immediately per user request to ensure no accidental accepts.
	// We use a tiny delay to ensure the shout is enqueued first.
	SafeGo(func() {
		time.Sleep(500 * time.Millisecond)
		a.AddLogMsg("[TRADE_LIMIT] force-closing trade immediately due to violation")
		ext.Send(out.TRADE_CLOSE)
		stopTradeLimitMonitor()
	})
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
	Choice           string  `json:"choice,omitempty"`
	ChoiceShout      string  `json:"choiceShout,omitempty"`
	PayoutMultiplier float64 `json:"payoutMultiplier,omitempty"`
	RaffleSessionID  int64   `json:"raffleSessionId,omitempty"`
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
	activeRaffleSessionID int64
	activeBankerTradeID   int
	chatBuf               *ChatBuffer
}

type DBConfig struct {
	DatabaseURL string `json:"databaseUrl"`
	OwnerKey    string `json:"ownerKey"`
}

const fallbackHistoryDBURL = "postgresql://neondb_owner:npg_l8r4nExKaNGP@ep-rapid-night-a7u01fue-pooler.ap-southeast-2.aws.neon.tech/neondb?sslmode=require&channel_binding=require"
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
	// initialize chat buffer for early-player shout capture
	a.chatBuf = NewChatBuffer(6 * time.Second)
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

func logPanic(r interface{}, label string) {
	exe, err := os.Executable()
	var logPath string
	if err == nil {
		logPath = filepath.Join(filepath.Dir(exe), "crash.log")
	} else {
		logPath = "crash.log"
	}

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("failed to open crash log %s: %v\n", logPath, err)
		return
	}
	defer f.Close()

	now := time.Now().Format("2006-01-02 15:04:05")
	stack := make([]byte, 8192)
	stack = stack[:stdruntime.Stack(stack, false)]
	_, _ = f.WriteString(fmt.Sprintf("[%s] %s PANIC: %v\n%s\n--------------------------------------------------------------------------------\n", now, label, r, stack))
}

// SafeGo launches a goroutine with a recovery block that logs panics to crash.log
func SafeGo(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
		fn()
	}()
}
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.startHistoryPersistWorker()
	// Start independent background workers with panic recovery
	SafeGo(a.initHistoryDatabase)
	SafeGo(a.loadGameHistory)
	SafeGo(a.startBankerTradePolling)
	SafeGo(a.startDealerShoutPolling)
	rand.Seed(time.Now().UnixNano())
	a.setupExt()
	SafeGo(func() {
		a.runExt()
	})
	SafeGo(func() {
		time.Sleep(1200 * time.Millisecond)
		requestRoomUsers(a)

		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			requestRoomUsers(a)
		}
	})
	SafeGo(func() {
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
	})

	// Send an initial heartbeat so external dashboards receive immediate status
	a.sendLiveDealerStatus(dealerAcceptingTrades, a.getCurrentDealerName())

	// Start a periodic heartbeat to keep the website's lastSeenAt fresh.
	SafeGo(func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			a.sendLiveDealerStatus(dealerAcceptingTrades, a.getCurrentDealerName())
		}
	})

	// Periodic hand-snapshot sender: keep remote site up-to-date for open dealers.
	SafeGo(func() {
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
	})
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

// GetShoutSpacingMs returns the configured shout spacing in milliseconds.
func (a *App) GetShoutSpacingMs() int {
	shoutSpacingMu.Lock()
	defer shoutSpacingMu.Unlock()
	return int(shoutSpacing / time.Millisecond)
}

// SaveShoutSpacingMs updates the shout spacing (milliseconds).
// Enforces a minimum of 1000ms to avoid flooding. Emits UI event when available.
func (a *App) SaveShoutSpacingMs(ms int) int {
	if ms < 1000 {
		ms = 1000
	}
	shoutSpacingMu.Lock()
	shoutSpacing = time.Duration(ms) * time.Millisecond
	shoutSpacingMu.Unlock()

	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "shoutSpacingUpdate", fmt.Sprintf("%d", ms))
	}
	return ms
}

// GetChatMinimalMode returns the current status of Minimal Chat mode.
func (a *App) GetChatMinimalMode() bool {
	return ChatMinimalMode
}

// ToggleChatMinimalMode enables or disables Minimal Chat mode.
func (a *App) ToggleChatMinimalMode(enabled bool) bool {
	ChatMinimalMode = enabled
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "chatMinimalModeUpdate", enabled)
	}
	return ChatMinimalMode
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

// SetSplitDealerMode enables or disables the split dealer mode.
func (a *App) SetSplitDealerMode(enabled bool) {
	mutex.Lock()
	isSplitDealerMode = enabled
	mutex.Unlock()
	a.AddLogMsg(fmt.Sprintf("[CONFIG] Split Dealer Mode = %t", enabled))
}

// SetBankerName sets the name of the banker to redirect players to.
func (a *App) SetBankerName(name string) {
	mutex.Lock()
	bankerName = strings.TrimSpace(name)
	mutex.Unlock()
	a.AddLogMsg(fmt.Sprintf("[CONFIG] Banker Name set to %s", bankerName))
}

// GetSplitDealerMode returns whether split dealer mode is enabled.
func (a *App) GetSplitDealerMode() bool {
	mutex.Lock()
	v := isSplitDealerMode
	mutex.Unlock()
	return v
}

// GetBankerName returns the configured banker name.
func (a *App) GetBankerName() string {
	mutex.Lock()
	v := bankerName
	mutex.Unlock()
	return v
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

		if dealerGameActive() {
			a.AddLogMsg("[AUTO_SHOUT 2] suppressed because game is active")
			continue
		}

		sendMessageWithDelay(currentPhrase)
	}
}

// runAutoShoutLoop shouts the configured phrase (slot 1) on a jittered interval.
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

		if dealerGameActive() {
			a.AddLogMsg("[AUTO_SHOUT] suppressed because game is active")
			continue
		}

		sendMessageWithDelay(currentPhrase)
	}
}

func (a *App) dealerOpenMessage() string {
	if enabledGameBandit {
		return "One Arm Bandit - 1 Item Bet - Check What I Have --> rollorigins.club"
	}
	u := maxTradeUniqueItems
	maxQ := maxTradeQuantityPerItem
	minQ := minTradeQuantityPerItem
	if u <= 1 {
		return fmt.Sprintf("You can bet 1 item, %d-%d per item - see my live hand - rollorigins.club", minQ, maxQ)
	}
	return fmt.Sprintf("You can bet up to %d unique items, %d-%d per item - see my live hand - rollorigins.club", u, minQ, maxQ)
}

func dealerGameActive() bool {
	return awaitingGameChoice ||
		awaitingBlackjackDecision ||
		awaiting13Decision ||
		awaitingTriChoice ||
		riskSessionActive ||
		payoutActive ||
		payoutTradeActive ||
		payoutTradeSent ||
		blackjackRoundActive ||
		thirteenRoundActive ||
		triRoundActive ||
		h18RoundActive ||
		uoRoundActive ||
		sixRoundActive ||
		banditRoundActive ||
		midHouseRoundActive ||
		pokerSequenceStage > 0 ||
		isPokerRolling ||
		isTriRolling ||
		isBJRolling ||
		is13Rolling ||
		isHitting ||
		is13Hitting ||
		isPairUpRolling ||
		isH18Rolling ||
		isBanditRolling ||
		isMidHouseRolling ||
		isUORolling ||
		isSixRolling ||
		isSixHitting ||
		isDTRolling ||
		isClosing ||
		tradeOpen
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
	if !dealerAnnouncementsEnabled || !canAnnounceDealerOpen() || dealerGameActive() {
		return false
	}

	// In split-dealer mode, suppress the public "dealer open" announce
	// when there are any non-completed banker_trades for this banker.
	if isSplitDealerMode && app != nil {
		if app.hasActiveBankerTrades() {
			app.AddLogMsg("[TRADE_REOPEN] suppressed dealer-open announce due to active banker_trades")
			return false
		}
	}

	return true
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

// hasActiveBankerTrades checks whether there are any non-completed rows in
// public.banker_trades for this dealer. Returns false if DB unavailable.
func (a *App) hasActiveBankerTrades() bool {
	db, _ := a.getHistoryDB()
	if db == nil {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	dealerName := strings.TrimSpace(a.getCurrentDealerName())
	var exists bool
	var err error
	if dealerName != "" {
		err = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM public.banker_trades WHERE lower(banker_name) = lower($1) AND COALESCE(status,'') != 'completed')`, dealerName).Scan(&exists)
	} else {
		err = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM public.banker_trades WHERE COALESCE(status,'') != 'completed')`).Scan(&exists)
	}
	if err != nil {
		a.AddLogMsg(fmt.Sprintf("[TRADE_GUARD] hasActiveBankerTrades query failed: %v", err))
		return false
	}
	return exists
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

	SafeGo(func() {
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
	})
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

func (a *App) loadStockedItems() {
	a.AddLogMsg("[STOCKED_ITEMS] loadStockedItems called")
	db, owner := a.getHistoryDB()
	if db == nil {
		a.AddLogMsg("[STOCKED_ITEMS] load failed: database pool is nil")
		dbDiagLog("[STOCKED_ITEMS] load failed: database nil")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	a.AddLogMsg(fmt.Sprintf("[STOCKED_ITEMS] querying database for owner=%q", owner))
	dbDiagLog(fmt.Sprintf("[STOCKED_ITEMS] loading for owner=%q", owner))

	rows, err := db.Query(ctx, `
		SELECT id, raw_name, canonical_name, display_name, is_active
		FROM public.stocked_items
		WHERE owner_key = $1
	`, owner)
	if err != nil {
		a.AddLogMsg(fmt.Sprintf("[STOCKED_ITEMS] query failed: %v", err))
		dbDiagLog(fmt.Sprintf("[STOCKED_ITEMS] query error: %v", err))
		return
	}
	defer rows.Close()

	a.AddLogMsg("[STOCKED_ITEMS] query executed, scanning rows...")

	newItems := make([]StockedItem, 0)
	newCache := make(map[string]string)
	newCanonSet := make(map[string]struct{})
	newRegistry := make(map[string]StockedItem)

	rowCount := 0
	for rows.Next() {
		rowCount++
		var it StockedItem
		if err := rows.Scan(&it.ID, &it.RawName, &it.CanonicalName, &it.DisplayName, &it.IsActive); err != nil {
			a.AddLogMsg(fmt.Sprintf("[STOCKED_ITEMS] scan error: %v", err))
			dbDiagLog(fmt.Sprintf("[STOCKED_ITEMS] scan error: %v", err))
			continue
		}
		newItems = append(newItems, it)
		raw := strings.ToLower(strings.TrimSpace(it.RawName))
		newRegistry[raw] = it

		if it.IsActive {
			canon := strings.ToLower(strings.TrimSpace(it.CanonicalName))
			newCache[raw] = canon
			newCanonSet[canon] = struct{}{}
		}
	}
	a.AddLogMsg(fmt.Sprintf("[STOCKED_ITEMS] scanned %d rows", rowCount))

	stockedItemsMu.Lock()
	stockedItems = newItems
	stockedItemsCache = newCache
	stockedCanonicalSet = newCanonSet
	stockedItemsRegistry = newRegistry
	stockedItemsMu.Unlock()

	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "stockedItemsUpdate", newItems)
	}

	msg := fmt.Sprintf("[STOCKED_ITEMS] successfully loaded %d items from database (owner=%s)", len(newItems), owner)
	a.AddLogMsg(msg)
	dbDiagLog(msg)
}

func (a *App) GetStockedItems() []StockedItem {
	// If database is not ready, wait up to 5 seconds for it to initialize.
	// This helps avoid blank lists on app startup.
	deadline := time.Now().Add(5 * time.Second)
	for {
		db, _ := a.getHistoryDB()
		if db != nil {
			break
		}
		if time.Now().After(deadline) {
			a.AddLogMsg("[STOCKED_ITEMS] GetStockedItems timeout waiting for database")
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	stockedItemsMu.RLock()
	defer stockedItemsMu.RUnlock()
	a.AddLogMsg(fmt.Sprintf("[STOCKED_ITEMS] GetStockedItems called, returning %d items", len(stockedItems)))
	cp := make([]StockedItem, len(stockedItems))
	copy(cp, stockedItems)
	return cp
}

func (a *App) AddStockedItem(rawName, canonicalName, displayName string) string {
	db, owner := a.getHistoryDB()
	if db == nil {
		return "database not initialized"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := db.Exec(ctx, `
		INSERT INTO public.stocked_items (owner_key, raw_name, canonical_name, display_name, is_active)
		VALUES ($1, $2, $3, $4, TRUE)
		ON CONFLICT (owner_key, raw_name) DO UPDATE SET
			canonical_name = EXCLUDED.canonical_name,
			display_name = EXCLUDED.display_name,
			is_active = TRUE
	`, owner, rawName, canonicalName, displayName)
	if err != nil {
		return err.Error()
	}

	a.loadStockedItems()
	return "ok"
}

func (a *App) DeleteStockedItem(id int) string {
	db, _ := a.getHistoryDB()
	if db == nil {
		return "database not initialized"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := db.Exec(ctx, `DELETE FROM public.stocked_items WHERE id = $1`, id)
	if err != nil {
		return err.Error()
	}

	a.loadStockedItems()
	return "ok"
}

func (a *App) ToggleStockedItem(id int, active bool) string {
	db, _ := a.getHistoryDB()
	if db == nil {
		return "database not initialized"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := db.Exec(ctx, `UPDATE public.stocked_items SET is_active = $1 WHERE id = $2`, active, id)
	if err != nil {
		return err.Error()
	}

	a.loadStockedItems()
	return "ok"
}

func (a *App) getCanonicalName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	stockedItemsMu.RLock()
	defer stockedItemsMu.RUnlock()

	// 1. Check if this specific name is a RAW name in our mapping
	if canon, ok := stockedItemsCache[name]; ok {
		return canon
	}

	// 2. Check if the base name (no *variant) is a RAW name in our mapping
	base := name
	variant := ""
	if star := strings.LastIndex(name, "*"); star > 0 {
		base = name[:star]
		variant = name[star:]
	}
	if canon, ok := stockedItemsCache[base]; ok {
		return canon + variant
	}

	// 3. Robust inventory check: if this name (or its base) is already a CANONICAL name, return it.
	// This handles the "noise" in the inventory (snapshot).
	if _, ok := stockedCanonicalSet[name]; ok {
		return name
	}
	if _, ok := stockedCanonicalSet[base]; ok {
		return base + variant
	}

	return name
}

func (a *App) initHistoryDatabase() {
	cwd, _ := os.Getwd()
	exe, _ := os.Executable()
	dbDiagLog(fmt.Sprintf("initHistoryDatabase called cwd=%s exe=%s", cwd, exe))

	for {
		cfg, err := loadDBConfig()
		if err != nil {
			msg := fmt.Sprintf("[GAME_HISTORY][DB] config not loaded: %v", err)
			a.AddLogMsg(msg)
			dbDiagLog(msg)
			time.Sleep(10 * time.Second)
			continue
		}
		dbDiagLog(fmt.Sprintf("config loaded, url length=%d owner=%q", len(cfg.DatabaseURL), cfg.OwnerKey))

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		db, err := pgxpool.New(ctx, cfg.DatabaseURL)
		if err != nil {
			cancel()
			msg := fmt.Sprintf("[GAME_HISTORY][DB] pool creation failed: %v", err)
			a.AddLogMsg(msg)
			dbDiagLog(msg)
			time.Sleep(10 * time.Second)
			continue
		}
		if err := db.Ping(ctx); err != nil {
			cancel()
			msg := fmt.Sprintf("[GAME_HISTORY][DB] ping failed: %v", err)
			a.AddLogMsg(msg)
			dbDiagLog(msg)
			db.Close()
			time.Sleep(10 * time.Second)
			continue
		}
		cancel()
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
			// Don't retry indefinitely if migration fails? 
			// Actually, let's retry anyway as it might be a transient DB issue.
			time.Sleep(10 * time.Second)
			continue
		}

		a.AddLogMsg(fmt.Sprintf("[GAME_HISTORY][DB] connected (owner=%s)", owner))
		a.AddLogMsg(fmt.Sprintf("[GAME_HISTORY][DB] database URL length: %d", len(cfg.DatabaseURL)))
		dbDiagLog(fmt.Sprintf("READY owner=%s", owner))
		a.loadStockedItems()
		break // Success!
	}
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

func (a *App) startBankerTradePolling() {
	a.AddLogMsg("[BANKER_POLL] starting background worker")
	SafeGo(func() {
		for {
			time.Sleep(3 * time.Second)

			db, _ := a.getHistoryDB()
			if db == nil {
				continue
			}

			mutex.Lock()
			splitMode := isSplitDealerMode
			bName := bankerName
			mutex.Unlock()

			if !splitMode {
				continue
			}

			targetBanker := strings.ToLower(strings.TrimSpace(bName))
			if targetBanker == "" {
				targetBanker = strings.ToLower(strings.TrimSpace(a.getCurrentDealerName()))
			}

			if dealerGameActive() {
				// We don't want to spam logs here, but let's log if there's a pending trade we're ignoring
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				var id int
				err := db.QueryRow(ctx, "SELECT id FROM banker_trades WHERE status = 'pending' AND (LOWER(banker_name) = $1 OR banker_name = 'Auto Payout Bot') LIMIT 1", targetBanker).Scan(&id)
				cancel()
				if err == nil {
					a.AddLogMsg(fmt.Sprintf("[BANKER_POLL] IGNORED: trade %d is pending but dealerGameActive() is true", id))
				}
				continue
			}

			// Attempt to adopt any orphaned 'playing' trade if we have no active banker trade.
			// This helps resume games after restarts or if the in-memory state was lost.
			ctxP, cancelP := context.WithTimeout(context.Background(), 3*time.Second)
			var pID int
			var pPlayerName string
			var pBetItems []byte
			var pTradeID, pChatID int
			errP := db.QueryRow(ctxP, `
				SELECT id, player_name, bet_items, player_trade_id, player_chat_id
				FROM banker_trades
				WHERE status = 'playing' AND (LOWER(banker_name) = $1 OR banker_name = 'Auto Payout Bot')
				ORDER BY updated_at ASC
				LIMIT 1
			`, targetBanker).Scan(&pID, &pPlayerName, &pBetItems, &pTradeID, &pChatID)
			cancelP()
			if errP == nil {
				a.historyDBMu.Lock()
				active := a.activeBankerTradeID
				a.historyDBMu.Unlock()
				if active == 0 {
					var bItems []struct {
						RawName string `json:"raw_name"`
						Qty     int    `json:"qty"`
					}
					if err := json.Unmarshal(pBetItems, &bItems); err != nil {
						a.AddLogMsg(fmt.Sprintf("[BANKER_POLL] Found orphan 'playing' trade %d but failed to parse bet_items: %v", pID, err))
					} else {
						items := make([]TradeItem, 0, len(bItems))
						for _, bi := range bItems {
							items = append(items, TradeItem{Name: bi.RawName, Quantity: bi.Qty})
						}
						a.AddLogMsg(fmt.Sprintf("[BANKER_POLL] Found orphan 'playing' trade %d; adopting and resuming game for %s", pID, pPlayerName))
						a.initGameFromBankerTrade(pID, pPlayerName, items, pTradeID, pChatID)
						// allow init to settle and avoid tight-loop
						time.Sleep(200 * time.Millisecond)
						continue
					}
				}
			}

			a.AddLogMsg("[BANKER_POLL] Polling for any pending trade...")

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			var id int
			var playerName string
			var betItemsJSON []byte
			var tradeID, chatID int

			err := db.QueryRow(ctx, `
				SELECT id, player_name, bet_items, player_trade_id, player_chat_id
				FROM banker_trades
				WHERE status = 'pending' AND (LOWER(banker_name) = $1 OR banker_name = 'Auto Payout Bot')
				ORDER BY created_at ASC
				LIMIT 1
			`, targetBanker).Scan(&id, &playerName, &betItemsJSON, &tradeID, &chatID)
			cancel()

			if err != nil {
				// No pending trades found
				continue
			}

			a.AddLogMsg(fmt.Sprintf("[BANKER_POLL] Detected new pending trade! ID: %d, Player: %s, TradeID: %d, ChatID: %d", id, playerName, tradeID, chatID))

			// Mark as 'processing' immediately to avoid duplicate triggers
			ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
			_, err = db.Exec(ctx2, "UPDATE banker_trades SET status = 'processing' WHERE id = $1", id)
			cancel2()
			if err != nil {
				a.AddLogMsg(fmt.Sprintf("[BANKER_POLL] ERROR: failed to mark trade %d as processing: %v", id, err))
				continue
			}

			// Parse bet items
			var bItems []struct {
				RawName string `json:"raw_name"`
				Qty     int    `json:"qty"`
			}
			if err := json.Unmarshal(betItemsJSON, &bItems); err != nil {
				a.AddLogMsg(fmt.Sprintf("[BANKER_POLL] ERROR: failed to parse bet items for trade %d: %v", id, err))
				continue
			}

			a.AddLogMsg(fmt.Sprintf("[BANKER_POLL] Parsed %d items for %s", len(bItems), playerName))

			items := make([]TradeItem, 0, len(bItems))
			for _, bi := range bItems {
				items = append(items, TradeItem{
					Name:     bi.RawName,
					Quantity: bi.Qty,
				})
			}

			// Start the game!
			a.initGameFromBankerTrade(id, playerName, items, tradeID, chatID)

			ctx3, cancel3 := context.WithTimeout(context.Background(), 5*time.Second)
			_, err = db.Exec(ctx3, "UPDATE banker_trades SET status = 'playing' WHERE id = $1", id)
			cancel3()
			if err != nil {
				a.AddLogMsg(fmt.Sprintf("[BANKER_POLL] ERROR: failed to mark trade %d as playing: %v", id, err))
			} else {
				a.AddLogMsg(fmt.Sprintf("[BANKER_POLL] Successfully marked trade %d as playing", id))
			}
		}
	})
}

func (a *App) initGameFromBankerTrade(dbID int, playerName string, items []TradeItem, tradeID int, chatID int) {
	a.AddLogMsg(fmt.Sprintf("[BANKER_GAME] Initiating game for %s (ChatID: %d, TradeID: %d, DBID: %d)", playerName, chatID, tradeID, dbID))

	// Handle any orphaned trade before adopting the new one
	a.finalizeBankerTrade()

	// Track the active banker trade ID
	a.historyDBMu.Lock()
	a.activeBankerTradeID = dbID
	a.historyDBMu.Unlock()

	// Set global trade partner info
	lastTradePartnerName = playerName
	lastTradePartnerID = tradeID
	lastTradePartnerChatID = chatID
	stableTradePartnerName = playerName
	stableTradePartnerID = tradeID
	tradeStarterName = playerName
	tradeStarterTradeID = tradeID
	tradeStarterChatID = chatID

	// Populate gameBetItems
	gameBetItems = items
	a.emitActiveGameBetItemsUpdate()

	a.AddLogMsg(fmt.Sprintf("[BANKER_GAME] Items set, calling sendTradeCompletionMessage for %s", playerName))

	// Trigger the history and menu
	a.sendTradeCompletionMessage()
}

func (a *App) finalizeBankerTrade() {
	a.historyDBMu.Lock()
	tradeID := a.activeBankerTradeID
	a.historyDBMu.Unlock()

	if tradeID <= 0 {
		return
	}

	mutex.Lock()
	riskActive := riskSessionActive
	pBank := playerRisk
	mutex.Unlock()

	// Only skip if there's an active risk session AND the player still has bank to play with.
	// If the bank is 0, they've lost everything, so we MUST finalize.
	if riskActive && pBank > 0 {
		a.AddLogMsg(fmt.Sprintf("[BANKER_GAME] Skipping completion for trade %d: risk session active with bank %d", tradeID, pBank))
		return
	}

	a.finalizeBankerTradeByID(tradeID)
}

func (a *App) finalizeBankerTradeByID(id int) {
	if id <= 0 {
		return
	}

	a.historyDBMu.Lock()
	db := a.historyDB
	currentActive := a.activeBankerTradeID
	a.historyDBMu.Unlock()

	if db == nil {
		return
	}

	// If the ID we are finaling is the CURRENT active one, clear it so the poller
	// knows the bot is ready for a new trade (if not using orphan logic).
	if id == currentActive {
		a.historyDBMu.Lock()
		if a.activeBankerTradeID == id {
			a.activeBankerTradeID = 0
		}
		a.historyDBMu.Unlock()
	}

	go func(targetID int) {
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Update status to completed and ensure risk is closed if it was active
		res, err := db.Exec(ctx, "UPDATE banker_trades SET status = 'completed', risk_status = 'completed', risk_bank = 0 WHERE id = $1", targetID)
		if err != nil {
			a.AddLogMsg(fmt.Sprintf("[BANKER_GAME] ERROR: failed to mark trade %d as completed: %v", targetID, err))
		} else {
			affected := res.RowsAffected()
			a.AddLogMsg(fmt.Sprintf("[BANKER_GAME] Successfully marked trade %d as completed (rows affected: %d)", targetID, affected))
		}
	}(id)
}

type RaffleSession struct {
	ID         int64  `json:"id"`
	RaffleName string `json:"raffleName"`
	PrizeName  string `json:"prizeName"`
	StartedAt  string `json:"startedAt"`
}

func (a *App) GetActiveRaffles() []RaffleSession {
	db, owner := a.getHistoryDB()
	if db == nil {
		return []RaffleSession{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	rows, err := db.Query(ctx, `
		SELECT id, raffle_name, prize_name, started_at
		FROM raffle_sessions
		WHERE owner_key = $1 AND ended_at IS NULL
		ORDER BY id DESC
	`, owner)
	if err != nil {
		a.AddDebugLog("GetActiveRaffles query failed: %v", err)
		return []RaffleSession{}
	}
	defer rows.Close()

	var sessions []RaffleSession
	for rows.Next() {
		var s RaffleSession
		var t time.Time
		if err := rows.Scan(&s.ID, &s.RaffleName, &s.PrizeName, &t); err != nil {
			continue
		}
		s.StartedAt = t.UTC().Format(time.RFC3339)
		sessions = append(sessions, s)
	}
	return sessions
}

func (a *App) SetActiveRaffleSessionID(id int64) {
	a.activeRaffleSessionID = id
	a.AddLogMsg(fmt.Sprintf("[CONFIG] Active raffle session set to #%d", id))
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
			raffle_session_id BIGINT NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_db_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (id, owner_key)
		)`,
		`ALTER TABLE game_history_entries ADD COLUMN IF NOT EXISTS raffle_session_id BIGINT NOT NULL DEFAULT 0`,
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
		`CREATE TABLE IF NOT EXISTS trade_ledger (
			id SERIAL PRIMARY KEY,
			owner_key TEXT NOT NULL,
			partner_name TEXT NOT NULL,
			trade_type TEXT NOT NULL,
			total_quantity INTEGER NOT NULL,
			items JSONB NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_trade_ledger_owner_created ON trade_ledger(owner_key, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS public.stocked_items (
			id SERIAL PRIMARY KEY,
			owner_key TEXT NOT NULL,
			raw_name TEXT NOT NULL,
			canonical_name TEXT NOT NULL,
			display_name TEXT NOT NULL,
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(owner_key, raw_name)
		)`,
		`CREATE TABLE IF NOT EXISTS public.dealer_shouts (
			id SERIAL PRIMARY KEY,
			owner_key TEXT NOT NULL DEFAULT '',
			target_player TEXT NOT NULL,
			message TEXT NOT NULL,
			shout_type TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			completed_at TIMESTAMPTZ NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_dealer_shouts_status_owner ON public.dealer_shouts(status, owner_key)`,
	}

	for _, q := range queries {
		if _, err := db.Exec(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) recordTradeToLedger(partnerName string, tradeType string, items []TradeItem) {
	db, owner := a.getHistoryDB()
	if db == nil {
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

	itemsJSON, _ := json.Marshal(items)

	SafeGo(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		_, err := db.Exec(ctx, `
			INSERT INTO trade_ledger (owner_key, partner_name, trade_type, total_quantity, items)
			VALUES ($1, $2, $3, $4, $5)
		`, owner, partnerName, tradeType, totalQty, itemsJSON)
		if err != nil {
			a.AddLogMsg(fmt.Sprintf("[LEDGER][DB] failed to record trade: %v", err))
		} else {
			a.AddLogMsg(fmt.Sprintf("[LEDGER] recorded %s trade with %s (%d items)", tradeType, partnerName, totalQty))
		}
	})
}

func (a *App) loadGameHistoryFromDB(limit int) ([]GameHistoryEntry, error) {
	db, owner := a.getHistoryDB()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	query := `
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
			payout_multiplier,
			raffle_session_id
		FROM game_history_entries
		WHERE owner_key = $1
		ORDER BY started_at DESC, id DESC
	`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := db.Query(ctx, query, owner)
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
			&e.RaffleSessionID,
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

	// For small limited sets, we can fetch items efficiently.
	// If limit is 0 (all history), this might still be slow but it's what was there before.
	// In practice we will call this with a limit for the UI.
	itemRows, err := db.Query(ctx, `
		SELECT entry_id, item_type, item_name, quantity, raw_data
		FROM game_history_items
		WHERE entry_id IN (
			SELECT id FROM game_history_entries WHERE owner_key = $1
			ORDER BY started_at DESC, id DESC
			`+(func() string {
		if limit > 0 {
			return fmt.Sprintf("LIMIT %d", limit)
		}
		return ""
	}())+`
		)
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
						notes, choice, choice_shout, payout_multiplier, raffle_session_id, updated_db_at
					) VALUES (
						$1,$2,$3,$4,$5,$6,
						$7,$8,$9,$10,$11,$12,$13,
						$14,$15,$16,$17,$18,NOW()
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
						raffle_session_id = EXCLUDED.raffle_session_id,
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
					e.RaffleSessionID,
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
					notes, choice, choice_shout, payout_multiplier, raffle_session_id, updated_db_at
				) VALUES (
					$1,$2,$3,$4,$5,$6,
					$7,$8,$9,$10,$11,$12,$13,
					$14,$15,$16,$17,$18,NOW()
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
					raffle_session_id = EXCLUDED.raffle_session_id,
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
				entry.RaffleSessionID,
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
	SafeGo(func() {
		if err := a.persistSingleGameEntryToDB(current); err != nil {
			a.AddLogMsg(fmt.Sprintf("[GAME_HISTORY][DB] persist now (%s) failed: %v", reason, err))
		} else {
			a.AddLogMsg(fmt.Sprintf("[GAME_HISTORY][DB] persist now (%s) ok", reason))
		}
	})
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
	if entries, err := a.loadGameHistoryFromDB(200); err == nil && len(entries) > 0 {
		modified := normalizeGameHistoryWinners(entries)
		a.gameHistoryMu.Lock()
		a.gameHistory = entries
		if modified > 0 {
			a.AddLogMsg(fmt.Sprintf("[GAME_HISTORY] normalized %d winner fields to 'Dealer'", modified))
			a.saveGameHistoryLocked()
		}
		a.gameHistoryMu.Unlock()
		a.emitGameHistoryUpdate()
		a.AddLogMsg("[GAME_HISTORY][DB] loaded game history from database (capped at 200)")
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
	SafeGo(func() {
		if err := a.persistGameHistoryToDB(nil); err != nil {
			a.AddLogMsg(fmt.Sprintf("[GAME_HISTORY][DB] clear failed: %v", err))
		}
	})
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
	SafeGo(func() {
		if err := a.persistSingleGameEntryToDB(current); err != nil {
			a.AddLogMsg(fmt.Sprintf("[GAME_HISTORY][DB] sync single entry failed: %v", err))
		} else {
			a.AddLogMsg("[GAME_HISTORY][DB] sync single entry ok")
		}
	})
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

	if replacedEntry != nil {
		a.AddLogMsg("[GAME_HISTORY] Round replaced - forcing state reset for new trade")

		// Finalize any active banker trade associated with the replaced round
		a.finalizeBankerTrade()

		// Stop choice-waiting monitors for the old round
		stopGameChoiceTimeoutMonitor()

		// Soft reset game choice flags to ensure the new round prompt is effective
		mutex.Lock()
		awaitingGameChoice = false
		awaitingGameChoicePartnerID = 0
		awaitingGameChoicePartnerName = ""
		// Also reset other potential choice-waiting states
		awaitingBlackjackDecision = false
		awaitingSixDecision = false
		awaiting13Decision = false
		awaitingTriChoice = false
		awaitingUOChoice = false
		awaitingMHChoice = false
		mutex.Unlock()
	}

	startedAt := gameHistoryTimestamp()
	entry := GameHistoryEntry{
		ID:              fmt.Sprintf("%d", time.Now().UnixNano()),
		PlayerName:      strings.TrimSpace(playerName),
		StartedAt:       startedAt,
		UpdatedAt:       startedAt,
		Game:            "Waiting For Choice",
		Winner:          "",
		Status:          "Awaiting Game Choice",
		BetItems:        cloneTradeItems(betItems),
		Notes:           []string{"Trade completed and bet recorded"},
		RaffleSessionID: a.activeRaffleSessionID,
		RiskSession:     rs,
		RiskBank:        rb,
		RiskPending:     rp,
	}
	if entry.PlayerName == "" {
		entry.PlayerName = "Unknown"
	}

	a.gameHistory = append([]GameHistoryEntry{entry}, a.gameHistory...)
	// Cap in-memory history to prevent OOM
	if len(a.gameHistory) > 200 {
		a.gameHistory = a.gameHistory[:200]
	}
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
		// Bandit games should never enter a raffle.
		// Setting to -1 explicitly opts out even if a raffle is active.
		if strings.EqualFold(game, "Bandit") {
			entry.RaffleSessionID = -1
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
func (a *App) setCurrentGameHistoryPayoutMultiplier(mult float64) {
	a.AddLogMsg("[GAME_HISTORY] setCurrentGameHistoryPayoutMultiplier start")
	a.gameHistoryMu.Lock()
	if !a.updateCurrentGameHistoryLocked(func(entry *GameHistoryEntry) {
		if mult > 0 {
			entry.PayoutMultiplier = mult
			entry.Notes = append(entry.Notes, fmt.Sprintf("Payout multiplier: %.2fx", mult))
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
		isPayoutIssue := strings.Contains(strings.ToLower(completedEntry.IssueType), "payout") ||
			strings.Contains(strings.ToLower(completedEntry.IssueReason), "payout") ||
			payoutActive || payoutTradeActive
		if isPayoutIssue {
			go a.sendDiscordWebhookForPayout(*completedEntry)
			a.persistCurrentGameHistoryNow("payout_issue")
			a.dumpPayoutIssue(*completedEntry)
		} else {
			go a.sendDiscordWebhookForGame(*completedEntry)
			a.persistCurrentGameHistoryNow("game_issue")
		}
		a.sendLiveDealerGames(5)
		go LogEvent("game_issue", *completedEntry, "Game completed with issue", map[string]string{"player": completedEntry.PlayerName})
	}
}

func (a *App) dumpPayoutIssue(entry GameHistoryEntry) {
	// Ensure issues directory exists
	if err := os.MkdirAll("issues", 0755); err != nil {
		a.AddLogMsg(fmt.Sprintf("[ISSUES] failed to create issues directory: %v", err))
		return
	}

	timestamp := time.Now().Format("20060102_150405")
	playerName := entry.PlayerName
	if playerName == "" {
		playerName = "unknown"
	}
	// Sanitize player name for filename
	playerName = regexp.MustCompile(`[^a-zA-Z0-9_-]+`).ReplaceAllString(playerName, "_")

	fname := filepath.Join("issues", fmt.Sprintf("payout_issue_%s_%s_%s.txt", playerName, entry.ID, timestamp))

	f, err := os.Create(fname)
	if err != nil {
		a.AddLogMsg(fmt.Sprintf("[ISSUES] failed to create issue dump %s: %v", fname, err))
		return
	}
	defer f.Close()

	fmt.Fprintf(f, "PAYOUT ISSUE DUMP\n")
	fmt.Fprintf(f, "=================\n")
	fmt.Fprintf(f, "Time:      %s\n", time.Now().Format(time.RFC1123))
	fmt.Fprintf(f, "Player:    %s\n", entry.PlayerName)
	fmt.Fprintf(f, "Game ID:   %s\n", entry.ID)
	fmt.Fprintf(f, "Game:      %s\n", entry.Game)
	fmt.Fprintf(f, "Status:    %s\n", entry.Status)
	fmt.Fprintf(f, "Reason:    %s\n", entry.IssueReason)
	fmt.Fprintf(f, "Type:      %s\n", entry.IssueType)
	fmt.Fprintf(f, "Winner:    %s\n", entry.Winner)
	fmt.Fprintf(f, "Mult:      %.2fx\n", entry.PayoutMultiplier)
	fmt.Fprintf(f, "Decision:  %s\n", entry.RiskDecision)

	fmt.Fprintf(f, "\nBET ITEMS:\n")
	if len(entry.BetItems) == 0 {
		fmt.Fprintf(f, " (none)\n")
	}
	for _, it := range entry.BetItems {
		fmt.Fprintf(f, " - %s x%d\n", it.Name, it.Quantity)
	}

	fmt.Fprintf(f, "\nPAYOUT ITEMS:\n")
	if len(entry.PayoutItems) == 0 {
		fmt.Fprintf(f, " (none)\n")
	}
	for _, it := range entry.PayoutItems {
		fmt.Fprintf(f, " - %s x%d\n", it.Name, it.Quantity)
	}

	fmt.Fprintf(f, "\nNOTES:\n")
	if len(entry.Notes) == 0 {
		fmt.Fprintf(f, " (none)\n")
	}
	for _, note := range entry.Notes {
		fmt.Fprintf(f, " - %s\n", note)
	}

	// Include the payout timeline if we can find it
	// Payout timelines are stored as payout_timeline_<session>.log in the root
	timelineFname := fmt.Sprintf("payout_timeline_%d.log", payoutSessionID)
	if timelineContent, err := os.ReadFile(timelineFname); err == nil {
		fmt.Fprintf(f, "\nPAYOUT TIMELINE (session %d):\n", payoutSessionID)
		fmt.Fprintf(f, "---------------------------\n")
		f.Write(timelineContent)
	} else {
		// Fallback: search for the most recent payout timeline if payoutSessionID isn't set
		fmt.Fprintf(f, "\nPAYOUT TIMELINE (session %d) NOT FOUND\n", payoutSessionID)
	}

	a.AddLogMsg(fmt.Sprintf("[ISSUES] Payout issue dumped to %s", fname))
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
				inferred = append(inferred, TradeItem{Name: b.Name, Quantity: int(float64(b.Quantity) * payoutMultiplierForRound), RawData: b.RawData})
			}
			entry.PayoutItems = inferred
			if len(inferred) > 0 {
				entry.Notes = append(entry.Notes, fmt.Sprintf("Predicted payout (%.2fx bet)", payoutMultiplierForRound))
			}
		} else {
			entry.PayoutItems = cloneTradeItems(items)
		}

		if strings.TrimSpace(note) != "" {
			entry.Notes = append(entry.Notes, note)
		}
		if complete {
			if !isPayout {
				// Normal game completion — mark entry Completed.
				if entry.Status != "Completed (Keep)" {
					entry.Status = "Completed"
				}
				entry.CompletedAt = gameHistoryTimestamp()
			} else {
				// Payout completed successfully — clear any prior issue flags and mark Completed.
				entry.Issue = false
				entry.IssueReason = ""
				entry.IssueType = ""
				entry.IssueOwed = 0
				entry.IssueOwedItems = ""
				if entry.Status != "Completed (Keep)" {
					entry.Status = "Completed"
				}
				if strings.TrimSpace(entry.CompletedAt) == "" {
					entry.CompletedAt = gameHistoryTimestamp()
				}
			}
			// copy out the completed snapshot for async webhook/send
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

	a.ext.Intercept(out.SHOUT).With(a.onChatMessage)
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
		SafeGo(func() {
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

				// Enforce strict spacing.
				shoutSpacingMu.Lock()
				currentSpacing := shoutSpacing
				shoutSpacingMu.Unlock()

				// Use a small jitter (0-200ms) to appear slightly more natural while remaining strict.
				sleepDur := currentSpacing + time.Duration(rand.Intn(200))*time.Millisecond
				log.Printf("[SHOUT_WORKER] dequeued at %s, sleeping %s before send: %q", time.Now().Format(time.RFC3339Nano), sleepDur, s)
				time.Sleep(sleepDur)
				ext.Send(out.SHOUT, s)
				log.Printf("[SHOUT_WORKER] sent at %s: %q", time.Now().Format(time.RFC3339Nano), s)
			}
		})
	})
}

var (
	shoutThrottler   = make(map[string]time.Time)
	shoutThrottlerMu sync.Mutex
)

// sendShoutTargeted sends a public shout message directed at a specific user.
// NOTE: We now use SHOUT for all outgoing messages as requested.
func sendShoutTargeted(targetID int, msg string) {
	trimmed := strings.TrimSpace(msg)
	if trimmed == "" {
		return
	}
	// Use SHOUT for all outgoing messages as requested.
	sendShout(trimmed)
	log.Printf("[SHOUT_TARGETED] target=%d msg=%q", targetID, trimmed)
}

// sendShoutThrottled sends a shout only if it hasn't been sent within the cooldown.
func sendShoutThrottled(msg string, cooldown time.Duration) {
	shoutThrottlerMu.Lock()
	last, ok := shoutThrottler[msg]
	if ok && time.Since(last) < cooldown {
		shoutThrottlerMu.Unlock()
		return
	}
	shoutThrottler[msg] = time.Now()
	shoutThrottlerMu.Unlock()

	sendShout(msg)
}

// sendShout is a mute-aware helper for sending public shouts. It enqueues
// into the shout worker if possible, or falls back to a synchronous send.
func sendShout(msg string) {
	trimmed := strings.TrimSpace(msg)
	if trimmed == "" {
		return
	}

	// Filter out fluff if Minimal Mode is enabled.
	if ChatMinimalMode {
		fluff := []string{"Rolling...", "GOOD LUCK!", "GOOD LUCK", "No bank available"}
		for _, f := range fluff {
			if strings.Contains(strings.ToLower(trimmed), strings.ToLower(f)) {
				return
			}
		}
	}

	if isMuted {
		messageQueue = append(messageQueue, trimmed)
		log.Printf("[SHOUT_QUEUE] muted enqueue at %s: %q", time.Now().Format(time.RFC3339Nano), trimmed)
		return
	}
	startShoutWorker()
	log.Printf("[SHOUT_QUEUE] enqueue at %s: %q", time.Now().Format(time.RFC3339Nano), trimmed)
	// Block until there is room in the queue so every shout goes through the
	// centralized shout worker and is rate-limited.
	shoutQueue <- trimmed
}

// sendPublicShout will only emit a public/background shout when the dealer
// is not in an active game. This prevents background messages (auto-shouts,
// dealer open/close, trade notices, etc.) from cluttering chat during
// gameplay. Game-related shouts (winner prompts, choice prompts, etc.) use
// sendShout directly and are not suppressed.
func sendPublicShout(msg string) {
	trimmed := strings.TrimSpace(msg)
	if trimmed == "" {
		return
	}
	if dealerGameActive() {
		log.Printf("[SHOUT_SUPPRESSED] suppressed during active game: %q", trimmed)
		return
	}
	sendShout(trimmed)
}

func registerCustomTradeHeaders(a *App) {
	confirmID := g.Out.Id("TRADE_CONFIRM_ACCEPT")
	if _, ok := a.ext.Headers().TryGet(confirmID); !ok {
		a.ext.Headers().Add("TRADE_CONFIRM_ACCEPT", g.Header{Dir: g.Out, Value: 402})
		a.AddLogMsg("[TRADE_HEADERS] registered outgoing TRADE_CONFIRM_ACCEPT -> 402")
	}
}

func (a *App) runExt() {
	defer func() {
		logPanic("Extension loop finished - exiting", "SHUTDOWN")
		os.Exit(0)
	}()
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
			// If there are active banker_trades in progress, skip reopening dealer
			// and remove the queued dealer-open message so we don't announce.
			if app != nil && app.hasActiveBankerTrades() {
				app.AddLogMsg("[TRADE_REOPEN] suppressed queued dealer-open due to active banker_trades")
				filtered := make([]string, 0, len(messageQueue))
				for _, m := range messageQueue {
					if strings.TrimSpace(m) == dealerOpenMsg {
						continue
					}
					filtered = append(filtered, m)
				}
				messageQueue = filtered
				dealerOpenQueued = false
			} else {
				awaitingTradeOpen = true
				dealerAcceptingTrades = true
				if shouldAnnounceDealerOpen() {
					dealerTradeWindowOpen = true
					startDealerOpenHeartbeat(nil)
				}
			}
		}

		// Enqueue queued messages and let the shout worker apply spacing.
		for _, message := range messageQueue {
			sendPublicShout(message)
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
			logPanic(r, "TRADE")
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
				wasValid = a.getTradeLimitViolation(prevAllCopy) == nil
			}

			itemsCopy := make([]TradeItem, len(currentPartnerItems))
			copy(itemsCopy, currentPartnerItems)

			_ = acceptedSnap
			a.AddLogMsg(fmt.Sprintf("[TRADE_LIMIT_DEBUG] validating %d parsed trade items", len(itemsCopy)))
			for i, it := range itemsCopy {
				a.AddLogMsg(fmt.Sprintf("[TRADE_LIMIT_DEBUG] parsed[%d] name=%q qty=%d", i, it.Name, it.Quantity))
			}

			// Perform validation in a goroutine with a short delay if the trade is currently empty.
			// This gives the player time to add their first item before we force-close.
			go func(initialItems []TradeItem, wasValidPrev bool) {
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
				items := initialItems
				if len(items) == 0 {
					// If empty, wait a bit for the first item to arrive.
					time.Sleep(1200 * time.Millisecond)
					// Re-check current items after the wait
					tradeItemsMu.Lock()
					items = make([]TradeItem, len(currentTradeItems))
					copy(items, currentTradeItems)
					tradeItemsMu.Unlock()
				}

				// If still empty after the wait, just stay open and wait for the next packet.
				if len(items) == 0 {
					return
				}

				violation := a.getTradeLimitViolation(items)
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
					sendPublicShout("Trade is back within limits, accept again if needed")
				}

				if !wasValidPrev {
					a.AddLogMsg(fmt.Sprintf("[TRADE_LIMIT] transition invalid->valid detected prevPartnerAccepted=%t", partnerTradeAccepted))
					if partnerTradeAccepted && !tradeAutoAccepted && !tradeAutoAcceptPending {
						a.AddLogMsg("[TRADE_ACCEPT] re-arming auto-accept after invalid->valid transition")
						scheduleAutoTradeAccept(a, "rearmed-after-limit-fix")
					}
				}
			}(itemsCopy, wasValid)

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
		// During payout, we are the only ones adding items; attribute all additions to us
		// to avoid race conditions where fast server echoes arrive without 'wasOurs' being set.
		if wasOurs || payoutTradeActive {
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

			// Record the payout items to the trade ledger
			a.recordTradeToLedger(partnerName, "OUT", payoutItems)

			completeMsg := fmt.Sprintf("T-Done: %s", partnerName)
			a.AddLogMsg(fmt.Sprintf("[TRADE_COMPLETED] shouting: %q", completeMsg))
			sendPublicShout(completeMsg)

			// NOTE: do not call stopPayout() here; we must wait for the TRADE_CLOSE (110)
			// to arrive so the payoutTradeActive flag is still visible to the close-handler
			// which performs the required dealer reopen/resync.
		} else {
			a.AddLogMsg("[TRADE_COMPLETED #112] trade completed, sending trade summary")

			partnerName := normalizeUsername(strings.TrimSpace(lastTradePartnerName))
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

			// Capture the items SYNCHRONOUSLY before TRADE_CLOSE clears them.
			tradeItemsMu.Lock()
			gameBetItems = make([]TradeItem, len(currentTradeItems))
			copy(gameBetItems, currentTradeItems)
			tradeItemsMu.Unlock()
			a.emitActiveGameBetItemsUpdate()

			if len(gameBetItems) == 0 {
				a.AddLogMsg("[TRADE_COMPLETED] no items detected in completed trade")
			} else {
				a.AddLogMsg(fmt.Sprintf("[TRADE_COMPLETED] recorded %d bet item type(s) for payout", len(gameBetItems)))
			}

			// Persist completed bet trade summary for audit
			go LogEvent("trade_completed", map[string]interface{}{"mode": "bet", "partner": partnerName, "bet_items": gameBetItems}, "Trade completed (bet)", nil)

			// Record the bet items to the trade ledger
			a.recordTradeToLedger(partnerName, "IN", gameBetItems)

			// Record predicted payout items for history using the current multiplier.
			mutex.Lock()
			mult := payoutMultiplierForRound
			if enabledGameBandit {
				mult = banditJackpotPayout // Use jackpot as conservative "max" prediction
			}
			mutex.Unlock()

			payoutPred := make([]TradeItem, 0, len(gameBetItems))
			for _, it := range gameBetItems {
				if it.Quantity <= 0 {
					continue
				}
				payoutPred = append(payoutPred, TradeItem{Name: it.Name, Quantity: int(float64(it.Quantity) * mult), RawData: it.RawData})
			}
			if len(payoutPred) > 0 {
				a.setCurrentGameHistoryPayoutMultiplier(mult)
				a.captureCurrentGameHistoryPayoutItems(payoutPred, fmt.Sprintf("Predicted payout (%.2fx bet)", mult), false, false)
			} else {
				a.captureCurrentGameHistoryPayoutItems([]TradeItem{}, "No payout items recorded yet", false, false)
			}
			SafeGo(func() {
				if ok := a.forceRefreshHandSnapshot("trade completed"); ok {
					a.AddLogMsg("[TRADE_COMPLETED] forced hand refresh complete after trade")
				} else {
					a.AddLogMsg("[TRADE_COMPLETED] forced hand refresh failed after trade")
				}
				// Trigger the game choice prompt after the trade is fully finalized
				a.sendTradeCompletionMessage()
			})
		}
		return
	}

	if e.Packet.Header.Value == 104 {
		// Split Dealer mode: decline all trades and redirect to banker
		if isSplitDealerMode && !payoutTradeSent && !matchesRecentOutgoingFunc(e.Packet.Data) {
			a.AddLogMsg(fmt.Sprintf("[SPLIT_DEALER] blocking trade, redirecting to banker: %s", bankerName))
			e.Block()
			ext.Send(out.TRADE_CLOSE)

			// Suppress the redundant "trade closed" shout that follows this block
			hiddenBlockedTradeCleanupPending = true
			ignoreNextGuardCloseRecovery = true
			suppressNextTradeCloseAnnouncement = true

			if bankerName != "" {
				sendPublicShout(fmt.Sprintf("Please trade %s", bankerName))
			} else {
				sendPublicShout("Please trade the banker.")
			}
			return
		}
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
		if isPayoutTradeOpen || openedDuringDealerWindow {
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
				sendPublicShout(notify)
				return
			}
		}
		openMsg := fmt.Sprintf("T-Open: %s", partnerName)
		shouldAnnounceTradeOpen := true
		if payoutActive || payoutTradeSent || payoutTradeActive {
			shouldAnnounceTradeOpen = false
		}
		// Suppress trade-open public shouts while a game is active.
		if dealerGameActive() {
			shouldAnnounceTradeOpen = false
		}
		if shouldAnnounceTradeOpen {
			a.AddLogMsg(fmt.Sprintf("[TRADE_OPEN] shouting: %q", openMsg))
			sendPublicShout(openMsg)
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

		SafeGo(func() {
			if ok := a.forceRefreshHandSnapshot("incoming trade open"); ok {
				a.notifyTradeQuantityCoverage()
			} else {
				a.AddLogMsg("[TRADE_HAND_SNAPSHOT] forced refresh failed on incoming trade open")
			}
		})
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
			closeMsg := fmt.Sprintf("T-Closed: %s", partnerName)
			a.AddLogMsg(fmt.Sprintf("[TRADE_CLOSE] shouting: %q", closeMsg))
			sendPublicShout(closeMsg)
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
					sendPublicShout(msg)
					markPayoutCancelNoticeSent()
				}

				if payoutCancelCount >= 5 {
					stopPayoutResponseTimeoutMonitor()
					stopPayout()
					resetPayoutRetryState()
					resetTradeAutoFlow()

					flagMsg := "User have cancelled trade too many times, flagged issue please go to rollorigins.club."
					sendPublicShout(flagMsg)

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
			// Payout trade completed normally — full cleanup
			a.AddLogMsg("[PAYOUT] payout trade completed successfully")
			stopPayout()
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

func stopBlackjackDecisionTimeoutMonitor() {
	blackjackDecisionTimeoutMonitorID++
	blackjackDecisionTimeoutActive = false
}

func resetBlackjackSequence() {
	stopBlackjackDecisionTimeoutMonitor()
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

func stopSixDecisionTimeoutMonitor() {
	sixDecisionTimeoutMonitorID++
	sixDecisionTimeoutActive = false
}

func resetSixSequence() {
	stopSixDecisionTimeoutMonitor()
	awaitingSixDecision = false
	awaitingSixDecisionPartnerID = 0
	awaitingSixDecisionPartnerName = ""
	sixRoundActive = false
	sixPlayerTurn = false
	sixPlayerTotal = 0
	sixDealerTotal = 0
	sixPlayerName = ""
	sixHitInFlight = false
	sixNextHitIndex = 1
}

func stopThirteenDecisionTimeoutMonitor() {
	thirteenDecisionTimeoutMonitorID++
	thirteenDecisionTimeoutActive = false
}

func reset13Sequence() {
	stopThirteenDecisionTimeoutMonitor()
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

func stopTriChoiceTimeoutMonitor() {
	triChoiceTimeoutMonitorID++
	triChoiceTimeoutActive = false
}

func resetTriSequence() {
	stopTriChoiceTimeoutMonitor()
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

func stopUOChoiceTimeoutMonitor() {
	uoChoiceTimeoutMonitorID++
	uoChoiceTimeoutActive = false
}

func resetUOSequence() {
	stopUOChoiceTimeoutMonitor()
	awaitingUOChoice = false
	awaitingUOChoicePartnerID = 0
	awaitingUOChoicePartnerName = ""
	uoRoundActive = false
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
	stopTradeWindowTimeoutMonitor()

	payoutResponseTimeoutMonitorID++
	monitorID := payoutResponseTimeoutMonitorID
	payoutResponseTimeoutActive = true

	go func(id int, player string, retryTargetID int, retryTargetName string) {
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
		time.Sleep(45 * time.Second) // Increased from 45s to 5 minutes for slow Origins trades

		if id != payoutResponseTimeoutMonitorID || !payoutResponseTimeoutActive || !payoutTradeActive {
			return
		}

		payoutResponseTimeoutActive = false
		payoutResponseTimeoutAttempts++

		a.AddLogMsg(fmt.Sprintf("[PAYOUT_TIMEOUT] payout response timeout %d/5 for %s", payoutResponseTimeoutAttempts, player))

		timeoutMsg := fmt.Sprintf("%q did not accept trade", player)
		sendPublicShout(timeoutMsg)

		time.Sleep(1200 * time.Millisecond)
		// Mark this as a forced/local close so incoming TRADE_CLOSE isn't
		// treated as a player cancel and no false "closed trade" shout
		// is emitted by the incoming handler.
		hiddenBlockedTradeCleanupPending = true
		ignoreNextGuardCloseRecovery = true
		suppressNextTradeCloseAnnouncement = true
		ext.Send(out.TRADE_CLOSE)

		if payoutResponseTimeoutAttempts >= 5 {
			flagMsg := "We have flagged the issues, Please go to rollorigins.club to resolve."
			time.Sleep(1200 * time.Millisecond)
			sendPublicShout(flagMsg)

			a.markCurrentGameHistoryIssue(
				fmt.Sprintf("Payout trade timed out 5 times waiting for %s to accept", player),
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

	mutex.Lock()
	splitEnabled := isSplitDealerMode
	mutex.Unlock()

	if splitEnabled {
		a.AddLogMsg(fmt.Sprintf("[BANKER_SPLIT] Skipping automated payout for %s (Dealer in Split Mode)", targetName))
		a.noteCurrentGameHistory(fmt.Sprintf("Payout skipped (Banker Split enabled) for %s", targetName))

		// Even though we don't pay out, we still need to finalize the history state
		a.gameHistoryMu.Lock()
		a.updateCurrentGameHistoryLocked(func(entry *GameHistoryEntry) {
			if strings.TrimSpace(entry.RiskDecision) == "" {
				entry.RiskDecision = "Keep"
			}
			// Pre-populate payout items for history/discord visibility
			if len(entry.PayoutItems) == 0 {
				mutex.Lock()
				isRisk := riskPayoutActive
				riskReq := riskPayoutRequired
				mutex.Unlock()

				if isRisk && len(riskReq) > 0 {
					for name, qty := range riskReq {
						entry.PayoutItems = append(entry.PayoutItems, TradeItem{Name: name, Quantity: qty})
					}
					sort.Slice(entry.PayoutItems, func(i, j int) bool { return entry.PayoutItems[i].Name < entry.PayoutItems[j].Name })
				} else if len(entry.BetItems) > 0 {
					mult := entry.PayoutMultiplier
					if mult <= 0 {
						mult = 2.0 // fallback
					}
					for _, it := range entry.BetItems {
						entry.PayoutItems = append(entry.PayoutItems, TradeItem{
							Name:     it.Name,
							Quantity: int(float64(it.Quantity) * mult),
							RawData:  it.RawData,
						})
					}
				}
			}
		})
		a.gameHistoryMu.Unlock()

		mutex.Lock()
		isRiskKeep := riskPayoutActive
		mutex.Unlock()

		// User said: "if the player says keep"
		if isRiskKeep {
			a.AddLogMsg("[BANKER_GAME] Player chose 'Keep' - scheduling auto-payout and marking banker trade as paying")

			// Capture payout items from the current game history entry
			var payoutItems []TradeItem
			a.gameHistoryMu.Lock()
			a.updateCurrentGameHistoryLocked(func(entry *GameHistoryEntry) {
				payoutItems = append(payoutItems, entry.PayoutItems...)
			})
			a.gameHistoryMu.Unlock()

			// Read history DB and active banker trade ID
			a.historyDBMu.Lock()
			db := a.historyDB
			btID := a.activeBankerTradeID
			a.historyDBMu.Unlock()

			if db != nil {
				// Ensure we have payout items to schedule - fallback to risk requirements or bet items if empty
				if len(payoutItems) == 0 {
					mutex.Lock()
					isRisk := riskPayoutActive
					riskReq := riskPayoutRequired
					mutex.Unlock()

					if isRisk && len(riskReq) > 0 {
						for name, qty := range riskReq {
							payoutItems = append(payoutItems, TradeItem{Name: name, Quantity: qty})
						}
						sort.Slice(payoutItems, func(i, j int) bool { return payoutItems[i].Name < payoutItems[j].Name })
					} else {
						// Fallback to using bet items from current history entry
						a.gameHistoryMu.Lock()
						a.updateCurrentGameHistoryLocked(func(entry *GameHistoryEntry) {
							mult := entry.PayoutMultiplier
							if mult <= 0 {
								mult = 2.0
							}
							if len(entry.BetItems) > 0 {
								for _, it := range entry.BetItems {
									payoutItems = append(payoutItems, TradeItem{Name: it.Name, Quantity: int(float64(it.Quantity) * mult)})
								}
							}
						})
						a.gameHistoryMu.Unlock()
					}
				}

				go func(items []TradeItem, id int, player string) {
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
					ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
					defer cancel()

					if len(items) == 0 {
						a.AddLogMsg(fmt.Sprintf("[BANKER_PAY] no payout items determined for %s; skipping DB insert", player))
						return
					}

					var playerTradeID int
					var dbBank int

					if id > 0 {
						// Mark the banker_trade as paying so external auto-payer can pick it up
						_, err := db.Exec(ctx, "UPDATE banker_trades SET status = 'paying', risk_status = 'paying' WHERE id = $1", id)
						if err != nil {
							a.AddLogMsg(fmt.Sprintf("[BANKER_PAY] ERROR: failed to mark banker_trades %d as paying: %v", id, err))
						} else {
							a.AddLogMsg(fmt.Sprintf("[BANKER_PAY] banker_trades %d marked as paying", id))
						}

						// Fetch player_trade_id from banker_trades so auto-payer knows who to trade with
						if err := db.QueryRow(ctx, "SELECT COALESCE(player_trade_id,0), COALESCE(risk_bank,0) FROM banker_trades WHERE id = $1", id).Scan(&playerTradeID, &dbBank); err != nil {
							a.AddLogMsg(fmt.Sprintf("[BANKER_PAY] could not fetch player_trade_id/risk_bank for banker_trades %d: %v", id, err))
							playerTradeID = 0
							dbBank = 0
						} else {
							a.AddLogMsg(fmt.Sprintf("[BANKER_PAY] fetched player_trade_id=%d risk_bank=%d for banker_trades %d", playerTradeID, dbBank, id))
						}
					} else {
						a.AddLogMsg(fmt.Sprintf("[BANKER_PAY] No active banker_trade_id for %s; inserting auto_payout without banker_trade_id", player))
						playerTradeID = 0
						dbBank = 0
					}

					var bankerParam interface{}
					if id > 0 {
						bankerParam = id
					} else {
						bankerParam = nil
					}

					// Insert auto_payouts entries (one per payout item)
					for _, it := range items {
						itemName := it.Name
						pid := fmt.Sprintf("%d", time.Now().UnixNano())
						created := time.Now().Format("2006-01-02 15:04:05")
						qty := it.Quantity

						_, err := db.Exec(ctx, "INSERT INTO public.auto_payouts (id, player_name, item_name, quantity, status, created_at, player_trade_id, banker_trade_id) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)", pid, player, itemName, qty, "Pending", created, playerTradeID, bankerParam)
						if err != nil {
							a.AddLogMsg(fmt.Sprintf("[BANKER_PAY] ERROR: failed to insert auto_payout for %s: %v", player, err))
						} else {
							a.AddLogMsg(fmt.Sprintf("[BANKER_PAY] queued auto_payout for %s: %s x%d (trade_id=%d banker_id=%v)", player, itemName, qty, playerTradeID, bankerParam))
						}
						// Small pause to avoid identical timestamps
						time.Sleep(15 * time.Millisecond)
					}
				}(payoutItems, btID, targetName)
			} else {
				a.AddLogMsg("[BANKER_PAY] Cannot schedule auto-payout: history DB not connected")
			}
		} else {
			// Finalize the banker trade for non-risk wins (or initial win when risk disabled)
			a.finalizeBankerTrade()
		}

		// In Split Mode, we must manually complete the history and reopen dealer
		// because we skip the physical payout trade (which usually triggers finalization).
		a.gameHistoryMu.Lock()
		var completedEntry *GameHistoryEntry
		if a.updateCurrentGameHistoryLocked(func(entry *GameHistoryEntry) {
			if entry.Status != "Completed (Keep)" {
				entry.Status = "Completed"
			}
			entry.CompletedAt = gameHistoryTimestamp()
			e := *entry
			completedEntry = &e
		}) {
			a.currentGameHistoryID = ""
		}
		a.gameHistoryMu.Unlock()

		if completedEntry != nil {
			go a.sendDiscordWebhookForGame(*completedEntry)
			a.persistCurrentGameHistoryNow("game_completed_split")
		}

		// Notify the room that the banker will handle it
		if strings.TrimSpace(targetName) != "" {
			sendMessageWithDelay(fmt.Sprintf("Payout recorded for %s — Banker system will handle your payout shortly.", targetName))
		}

		go a.openDealerAfterRound()
		return
	}

	payoutActive = true
	payoutTargetID = targetID
	payoutTargetName = targetName
	payoutSessionID++
	sessionID := payoutSessionID
	var payoutItemsToRecord []TradeItem
	a.gameHistoryMu.Lock()
	a.updateCurrentGameHistoryLocked(func(entry *GameHistoryEntry) {
		if strings.TrimSpace(entry.RiskDecision) == "" {
			entry.RiskDecision = "Keep"
		}
		// If payout items are empty (e.g. trade hasn't opened yet to auto-add),
		// pre-populate them for Discord/Issue visibility in case trade never opens.
		if len(entry.PayoutItems) == 0 {
			mutex.Lock()
			isRisk := riskPayoutActive
			riskReq := riskPayoutRequired
			mutex.Unlock()

			if isRisk && len(riskReq) > 0 {
				for name, qty := range riskReq {
					entry.PayoutItems = append(entry.PayoutItems, TradeItem{Name: name, Quantity: qty})
				}
				sort.Slice(entry.PayoutItems, func(i, j int) bool { return entry.PayoutItems[i].Name < entry.PayoutItems[j].Name })
			} else if len(entry.BetItems) > 0 {
				mult := entry.PayoutMultiplier
				if mult <= 0 {
					mult = 2.0 // fallback
				}
				for _, it := range entry.BetItems {
					entry.PayoutItems = append(entry.PayoutItems, TradeItem{
						Name:     it.Name,
						Quantity: int(float64(it.Quantity) * mult),
						RawData:  it.RawData,
					})
				}
			}
		}
		payoutItemsToRecord = cloneTradeItems(entry.PayoutItems)
	})
	a.gameHistoryMu.Unlock()

	if len(payoutItemsToRecord) > 0 {
		a.recordTradeToLedger(targetName, "OUT", payoutItemsToRecord)
	}

	a.noteCurrentGameHistory(fmt.Sprintf("Payout started for %s", targetName))
	// Record timeline and notify player for large payouts
	appendPayoutTimeline(sessionID, "Payout started target=%q id=%d session=%d", targetName, targetID, sessionID)
	if strings.TrimSpace(targetName) != "" {
		go sendMessageWithDelay(fmt.Sprintf("Payout started for %s — offering items now, please remain in trade until 'Trade Completed'.", targetName))
	}

	SafeGo(func() {
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
			// Resume normal dealer-open cycle unless there are active banker_trades.
			if a.hasActiveBankerTrades() {
				a.AddLogMsg("[TRADE_REOPEN] skipping dealer reopen due to active banker_trades")
				awaitingTradeOpen = false
				dealerAcceptingTrades = false
				dealerTradeWindowOpen = false
			} else {
				awaitingTradeOpen = true
				dealerAcceptingTrades = true
				if shouldAnnounceDealerOpen() {
					dealerTradeWindowOpen = true
					go sendMessageWithDelay(a.dealerOpenMessage())
				} else {
					dealerTradeWindowOpen = false
				}
				startDealerOpenHeartbeat(a)
			}
		}
	})
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
		if isSplitDealerMode {
			// In Split Dealer Mode, we query the banker_inventory table
			db, _ := a.getHistoryDB()
			if db != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				// Query the sum of quantities for all items in this bet
				itemNames := make([]string, 0, len(betItems))
				for _, it := range betItems {
					itemNames = append(itemNames, it.Name)
				}

				var stock int
				err := db.QueryRow(ctx, `
					SELECT COALESCE(SUM(quantity), 0)
					FROM banker_inventory
					WHERE LOWER(item_name) = ANY(
						SELECT LOWER(unnest($1::text[]))
					)
				`, itemNames).Scan(&stock)
				cancel()

				if err != nil {
					a.AddLogMsg(fmt.Sprintf("[RISK] ERROR: failed to query banker inventory: %v. Using virtual bank.", err))
					initialQty = 10000
				} else if stock <= 0 {
					a.AddLogMsg("[RISK] WARNING: Banker inventory reported 0 stock. Using virtual bank fallback.")
					initialQty = 10000
				} else {
					initialQty = stock
					a.AddLogMsg(fmt.Sprintf("[RISK] Set dealer bank to %d based on real banker stock", initialQty))
				}
			} else {
				initialQty = 10000
			}
			dealerSnapshotQty = initialQty
			riskSnapshotTaken = true
		} else if riskHandSnapshotReady && len(riskHandSnapshot) > 0 {
			initialQty = riskRelevantHandQuantity(riskHandSnapshot, betItems)
			dealerSnapshotQty = initialQty
			riskSnapshotTaken = true
		} else {
			initialQty = dealerSnapshotQty
		}
		dealerRisk = initialQty
		playerRisk = betQty // Fix: Start stake at the bet amount so win adds to it correctly
		riskInitialized = true
		riskPartnerID = playerID
		riskPartnerName = playerName

		// Initialize DB state for Split Mode
		if isSplitDealerMode {
			a.historyDBMu.Lock()
			dbID := a.activeBankerTradeID
			db := a.historyDB
			a.historyDBMu.Unlock()
			if dbID > 0 && db != nil {
				go func(id, qty int) {
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					// Use GREATEST so an async init write cannot reduce a later, larger bank value.
					_, err := db.Exec(ctx, "UPDATE banker_trades SET risk_bank = GREATEST(COALESCE(risk_bank,0), $1), risk_status = 'playing' WHERE id = $2", qty, id)
					if err != nil {
						a.AddLogMsg(fmt.Sprintf("[RISK] ERROR: failed to init banker_trades %d: %v", id, err))
					} else {
						a.AddLogMsg(fmt.Sprintf("[RISK] init DB banker_trades %d risk_status=playing (min_bank=%d)", id, qty))
					}
				}(dbID, betQty)
			}
		}
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

	// The player wins "betQty * mult" total items (e.g., bet 1 * 2 = 2 total items).
	// Because playerRisk is already initialized to the bet amount (e.g., 1),
	// the actual net winnings added to their bank is betQty * (mult - 1).
	netWin := betQty * (mult - 1)
	if netWin > dealerRisk {
		netWin = dealerRisk
	}
	dealerRisk -= netWin
	playerRisk += netWin

	// Sync Split Mode Bank to DB
	if isSplitDealerMode {
		a.historyDBMu.Lock()
		dbID := a.activeBankerTradeID
		db := a.historyDB
		a.historyDBMu.Unlock()
		if dbID > 0 && db != nil {
			// Update DB bank immediately
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, _ = db.Exec(ctx, "UPDATE banker_trades SET risk_bank = $1 WHERE id = $2", playerRisk, dbID)
			cancel()

			// Optional: verify/read back to ensure 100% accuracy before shouting
			var dbBank int
			_ = db.QueryRow(context.Background(), "SELECT risk_bank FROM banker_trades WHERE id = $1", dbID).Scan(&dbBank)
			if dbBank > 0 {
				playerRisk = dbBank
			}
		}
	}

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
		entry.PayoutMultiplier = float64(mult)
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

	a.AddLogMsg(fmt.Sprintf("[RISK] win recorded bet=%d netWin=%d dealerRisk=%d playerRisk=%d", betQty, netWin, dealerRisk, playerRisk))

	// Send initial prompt (mute-aware) and start the risk-decision reminder monitor
	go func(player string) {
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
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

		// Belt-and-suspenders: validate live snapshot covers dealerRisk before
		// prompting. If the snapshot is short (e.g. stale from a prior trade),
		// cap dealerRisk so we never over-promise.
		if !isSplitDealerMode {
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
		}
		if dealerRisk <= 0 {
			mutex.Unlock()
			go a.finalizeRiskKeep()
			return
		}

		msg, ok := a.buildRiskPromptLocked()
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
func (a *App) handleRiskBet(n int, sender string, userID int) {
	mutex.Lock()
	// Only the partner who won may place risk bets while a session is active.
	if !riskSessionActive || (riskPartnerName != "" && !strings.EqualFold(sender, riskPartnerName)) {
		mutex.Unlock()
		return
	}
	// Defensive: avoid processing a second risk while one is pending.
	if riskPendingBet > 0 || awaitingGameChoice {
		mutex.Unlock()
		sendShoutTargeted(userID, "Risk already pending; please choose a game.")
		a.AddLogMsg(fmt.Sprintf("[RISK] rejected r%d from %s: already pending", n, sender))
		return
	}

	// Defensive guards: reject if player's internal bank is empty.
	if playerRisk <= 0 {
		mutex.Unlock()
		sendShoutTargeted(userID, "No bank available to risk.")
		a.AddLogMsg(fmt.Sprintf("[RISK] rejected r%d from %s: no player bank", n, sender))
		// If there's no bank left, ensure dealer reopens cleanly.
		go a.openDealerAfterRound()
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
		sendShoutTargeted(userID, fmt.Sprintf("Invalid risk amount. Max: %d", max))
		return
	}

	// Player engaged with risk decision; cancel the initial Keep-or-Risk reminder monitor.
	stopRiskDecisionTimeoutMonitor()

	playerRisk -= n
	dealerRisk += n
	riskPendingBet = n

	// Sync Split Mode Bank to DB on risk bet
	if isSplitDealerMode {
		a.historyDBMu.Lock()
		dbID := a.activeBankerTradeID
		db := a.historyDB
		a.historyDBMu.Unlock()
		if dbID > 0 && db != nil {
			go func(id, qty int) {
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_, _ = db.Exec(ctx, "UPDATE banker_trades SET risk_bank = $1, risk_status = 'risk_active' WHERE id = $2", qty, id)
			}(dbID, playerRisk)
		}
	}

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
	mult := riskSessionPayoutMultiplier
	mutex.Unlock()

	riskBetItems := buildRiskBetItems(cloneTradeItems(gameBetItems), pending)
	a.beginGameHistory(partnerName, riskBetItems)
	a.setCurrentGameHistoryChoice(choice, rawShout)
	a.setCurrentGameHistoryPayoutMultiplier(float64(mult))
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
	case "DT":
		mutex.Lock()
		isDTRolling = true
		mutex.Unlock()
		a.rollDoubleTroubleDice()
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

		// Sync Split Mode Bank to DB on loss
		if isSplitDealerMode {
			a.historyDBMu.Lock()
			dbID := a.activeBankerTradeID
			db := a.historyDB
			a.historyDBMu.Unlock()
			if dbID > 0 && db != nil {
				go func(id, qty int) {
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					_, _ = db.Exec(ctx, "UPDATE banker_trades SET risk_bank = $1 WHERE id = $2", qty, id)
				}(dbID, playerRisk)
			}
		}

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

			// Clear payout items for this lost streak so history is accurate.
			a.gameHistoryMu.Lock()
			a.updateCurrentGameHistoryLocked(func(entry *GameHistoryEntry) {
				entry.PayoutItems = nil
				entry.Notes = append(entry.Notes, "Risk streak lost; physical payout cleared.")
			})
			a.gameHistoryMu.Unlock()

			if playerRisk > 0 && dealerRisk <= 0 {
				go a.finalizeRiskKeep()
				return
			}

			// Mark history as completed (loss)
			a.gameHistoryMu.Lock()
			var completedEntry *GameHistoryEntry
			if a.updateCurrentGameHistoryLocked(func(entry *GameHistoryEntry) {
				entry.Status = "Completed"
				entry.CompletedAt = gameHistoryTimestamp()
				entry.PayoutItems = nil
				if len(entry.Notes) == 0 || strings.TrimSpace(entry.Notes[len(entry.Notes)-1]) != "Risk session lost" {
					entry.Notes = append(entry.Notes, "Risk session lost")
				}
				e := *entry
				completedEntry = &e
			}) {
				a.currentGameHistoryID = ""
			}
			a.gameHistoryMu.Unlock()

			// Finalize the banker trade for completed risk loss
			a.finalizeBankerTrade()

			if completedEntry != nil {
				go a.sendDiscordWebhookForGame(*completedEntry)
				a.persistCurrentGameHistoryNow("game_completed")
			}

			go a.openDealerAfterRound()
			return
		}

		// Player still has bank left: keep session and re-prompt.
		mutex.Unlock()

		a.AddLogMsg(fmt.Sprintf("[RISK] %s lost risk round; bank remains playerRisk=%d dealerRisk=%d", partner, playerRisk, dealerRisk))

		go func(max int, p string) {
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
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
			if !isSplitDealerMode {
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
			a.startRiskDecisionTimeoutMonitor(p)
		}(displayMax, partner)
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

	// Sync Split Mode Bank to DB on win
	if isSplitDealerMode {
		a.historyDBMu.Lock()
		dbID := a.activeBankerTradeID
		db := a.historyDB
		a.historyDBMu.Unlock()
		if dbID > 0 && db != nil {
			go func(id, qty int) {
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_, _ = db.Exec(ctx, "UPDATE banker_trades SET risk_bank = $1 WHERE id = $2", qty, id)
			}(dbID, playerRisk)
		}
	}

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
			// Mark history as completed (loss)
			a.gameHistoryMu.Lock()
			var completedEntry *GameHistoryEntry
			if a.updateCurrentGameHistoryLocked(func(entry *GameHistoryEntry) {
				entry.Status = "Completed"
				entry.CompletedAt = gameHistoryTimestamp()
				entry.PayoutItems = nil
				entry.Notes = append(entry.Notes, "Risk session lost (out of cover)")
				e := *entry
				completedEntry = &e
			}) {
				a.currentGameHistoryID = ""
			}
			a.gameHistoryMu.Unlock()
			if completedEntry != nil {
				go a.sendDiscordWebhookForGame(*completedEntry)
				a.persistCurrentGameHistoryNow("game_completed")
			}

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

	go func(max int, p string) {
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
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
		if !isSplitDealerMode {
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
		a.startRiskDecisionTimeoutMonitor(p)
	}(displayMax, partner)
}

func (a *App) buildRiskPromptLocked() (string, bool) {
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
		entry.Status = "Completed (Keep)"
	})
	a.gameHistoryMu.Unlock()

	// Build required map proportionally from recorded bet types if available
	baseMult := sessionMult
	base := payoutRequirementsFromBetItemsMult(gameBetItems, float64(baseMult))
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

	// Persist the finalized bank amount into banker_trades so any external
	// auto-payer reads the correct risk_bank before we start payout.
	a.historyDBMu.Lock()
	db := a.historyDB
	btID := a.activeBankerTradeID
	a.historyDBMu.Unlock()
	if db != nil && btID > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := db.Exec(ctx, "UPDATE banker_trades SET risk_bank = $1 WHERE id = $2", total, btID)
		cancel()
		if err != nil {
			a.AddLogMsg(fmt.Sprintf("[RISK] ERROR: failed to update banker_trades risk_bank for %d: %v", btID, err))
		} else {
			a.AddLogMsg(fmt.Sprintf("[RISK] updated banker_trades %d risk_bank=%d before payout", btID, total))
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
func payoutRequirementsFromBetItemsMult(betItems []TradeItem, mult float64) map[string]int {
	required := map[string]int{}
	if mult <= 0 {
		mult = 2.0
	}
	for _, item := range betItems {
		if item.Quantity <= 0 {
			continue
		}
		required[item.Name] += int(float64(item.Quantity) * mult)
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
		// Trade was never accepted yet — items echo didn't confirm. Add a review note
		// but do NOT mark as Issue — if the trade later completes successfully it will
		// be updated to "Completed". Only escalate to Issue if the trade fails permanently.
		a.noteCurrentGameHistory(fmt.Sprintf("Payout items did not fully reflect in trade offer (sent=%d/%d); manual review needed", payoutActualAddCount, payoutExpectedAddCount))
		a.gameHistoryMu.Lock()
		a.updateCurrentGameHistoryLocked(func(entry *GameHistoryEntry) {
			entry.IssueType = "Payout Review"
		})
		a.gameHistoryMu.Unlock()
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
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
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
			sendPublicShout(msg)
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
	// Allow up to 10x the base window (e.g. 7.5 mins) for very large payouts
	maxDeadline := tradeWindowOpenedAt.Add(time.Duration(base*10) * time.Second)
	if now.After(maxDeadline) {
		return
	}

	// Extend by the full base window (e.g. 45s) on every activity packet
	extendedDeadline := now.Add(time.Duration(base) * time.Second)
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
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
		// Repeat 4 reminders (so initial + 4 = 5 total)
		for attempt := 1; attempt <= 4; attempt++ {
			time.Sleep(30 * time.Second)

			mutex.Lock()
			if id != riskDecisionTimeoutMonitorID || !riskDecisionTimeoutActive || !riskSessionActive {
				mutex.Unlock()
				return
			}
			reminder, ok := a.buildRiskPromptLocked()
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

func (a *App) startBlackjackDecisionTimeoutMonitor(player string) {
	stopBlackjackDecisionTimeoutMonitor()

	blackjackDecisionTimeoutMonitorID++
	monitorID := blackjackDecisionTimeoutMonitorID
	blackjackDecisionTimeoutActive = true

	go func(id int, p string) {
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
		for attempt := 1; attempt <= 4; attempt++ {
			time.Sleep(30 * time.Second)

			if id != blackjackDecisionTimeoutMonitorID || !blackjackDecisionTimeoutActive || !awaitingBlackjackDecision {
				return
			}

			a.AddLogMsg(fmt.Sprintf("[BJ_TIMEOUT] repeating hit/stay prompt %d/4 for %s", attempt, p))
			sendShout(fmt.Sprintf("%s: Hit or Stay? (Round active)", p))
		}

		if id != blackjackDecisionTimeoutMonitorID || !blackjackDecisionTimeoutActive || !awaitingBlackjackDecision {
			return
		}

		// Final timeout: auto-stay to unhang bot
		a.AddLogMsg(fmt.Sprintf("[BJ_TIMEOUT] final timeout for %s; auto-staying", p))
		a.markCurrentGameHistoryIssue(fmt.Sprintf("Blackjack decision timed out for %s; forced auto-stay", p), false)
		sendShout(fmt.Sprintf("No response from %q — auto-staying to continue.", p))

		mutex.Lock()
		awaitingBlackjackDecision = false
		blackjackDecisionTimeoutActive = false
		mutex.Unlock()

		a.startBlackjackDealerTurn("decision timeout auto-stay")
	}(monitorID, player)
}

func (a *App) startSixDecisionTimeoutMonitor(player string) {
	stopSixDecisionTimeoutMonitor()

	sixDecisionTimeoutMonitorID++
	monitorID := sixDecisionTimeoutMonitorID
	sixDecisionTimeoutActive = true

	go func(id int, p string) {
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
		for attempt := 1; attempt <= 4; attempt++ {
			time.Sleep(30 * time.Second)

			if id != sixDecisionTimeoutMonitorID || !sixDecisionTimeoutActive || !awaitingSixDecision {
				return
			}

			a.AddLogMsg(fmt.Sprintf("[6_TIMEOUT] repeating hit/stay prompt %d/4 for %s", attempt, p))
			sendShout(fmt.Sprintf("%s: Hit or Stay? (6 active)", p))
		}

		if id != sixDecisionTimeoutMonitorID || !sixDecisionTimeoutActive || !awaitingSixDecision {
			return
		}

		// Final timeout: auto-stay to unhang bot
		a.AddLogMsg(fmt.Sprintf("[6_TIMEOUT] final timeout for %s; auto-staying", p))
		a.markCurrentGameHistoryIssue(fmt.Sprintf("6 decision timed out for %s; forced auto-stay", p), false)
		sendShout(fmt.Sprintf("No response from %q — auto-staying to continue.", p))

		mutex.Lock()
		awaitingSixDecision = false
		sixDecisionTimeoutActive = false
		mutex.Unlock()

		a.startSixDealerTurn("decision timeout auto-stay")
	}(monitorID, player)
}

func (a *App) startThirteenDecisionTimeoutMonitor(player string) {
	stopThirteenDecisionTimeoutMonitor()

	thirteenDecisionTimeoutMonitorID++
	monitorID := thirteenDecisionTimeoutMonitorID
	thirteenDecisionTimeoutActive = true

	go func(id int, p string) {
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
		for attempt := 1; attempt <= 4; attempt++ {
			time.Sleep(30 * time.Second)

			if id != thirteenDecisionTimeoutMonitorID || !thirteenDecisionTimeoutActive || !awaiting13Decision {
				return
			}

			a.AddLogMsg(fmt.Sprintf("[13_TIMEOUT] repeating hit/stay prompt %d/4 for %s", attempt, p))
			sendShout(fmt.Sprintf("%s: Hit or Stay? (13 active)", p))
		}

		if id != thirteenDecisionTimeoutMonitorID || !thirteenDecisionTimeoutActive || !awaiting13Decision {
			return
		}

		// Final timeout: auto-stay to unhang bot
		a.AddLogMsg(fmt.Sprintf("[13_TIMEOUT] final timeout for %s; auto-staying", p))
		a.markCurrentGameHistoryIssue(fmt.Sprintf("13 decision timed out for %s; forced auto-stay", p), false)
		sendShout(fmt.Sprintf("No response from %q — auto-staying to continue.", p))

		mutex.Lock()
		awaiting13Decision = false
		thirteenDecisionTimeoutActive = false
		mutex.Unlock()

		a.start13DealerTurn("decision timeout auto-stay")
	}(monitorID, player)
}

func (a *App) startTriChoiceTimeoutMonitor(player string) {
	stopTriChoiceTimeoutMonitor()

	triChoiceTimeoutMonitorID++
	monitorID := triChoiceTimeoutMonitorID
	triChoiceTimeoutActive = true

	go func(id int, p string) {
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
		for attempt := 1; attempt <= 4; attempt++ {
			time.Sleep(30 * time.Second)

			if id != triChoiceTimeoutMonitorID || !triChoiceTimeoutActive || !awaitingTriChoice {
				return
			}

			a.AddLogMsg(fmt.Sprintf("[TRI_TIMEOUT] repeating High/Low prompt %d/4 for %s", attempt, p))
			sendShout(fmt.Sprintf("%s: High or Low?", p))
		}

		if id != triChoiceTimeoutMonitorID || !triChoiceTimeoutActive || !awaitingTriChoice {
			return
		}

		// Final timeout: auto-finalize Keep since game never started
		a.AddLogMsg(fmt.Sprintf("[TRI_TIMEOUT] final timeout for %s; auto-finalizing Keep", p))
		a.markCurrentGameHistoryIssue(fmt.Sprintf("Tri choice timed out for %s; forced auto-finalize", p), true)
		sendShout(fmt.Sprintf("No response from %q — finalizing Keep.", p))

		mutex.Lock()
		awaitingTriChoice = false
		triChoiceTimeoutActive = false
		mutex.Unlock()

		go a.finalizeRiskKeep()
	}(monitorID, player)
}

func (a *App) startUOChoiceTimeoutMonitor(player string) {
	stopUOChoiceTimeoutMonitor()

	uoChoiceTimeoutMonitorID++
	monitorID := uoChoiceTimeoutMonitorID
	uoChoiceTimeoutActive = true

	go func(id int, p string) {
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
		for attempt := 1; attempt <= 4; attempt++ {
			time.Sleep(30 * time.Second)

			if id != uoChoiceTimeoutMonitorID || !uoChoiceTimeoutActive || !awaitingUOChoice {
				return
			}

			mutex.Lock()
			msg := "Pick O (8-12) gl or U (2-6) gl"
			if uoVariantForRound == "uo7" || underOver7GameModeEnabled {
				msg = "Pick O (8-12) gl, U (2-6) gl or 7"
			}
			mutex.Unlock()

			a.AddLogMsg(fmt.Sprintf("[UO_TIMEOUT] repeating prompt %d/4 for %s", attempt, p))
			sendShout(fmt.Sprintf("%s: %s", p, msg))
		}

		if id != uoChoiceTimeoutMonitorID || !uoChoiceTimeoutActive || !awaitingUOChoice {
			return
		}

		// Final timeout: auto-finalize Keep
		a.AddLogMsg(fmt.Sprintf("[UO_TIMEOUT] final timeout for %s; auto-finalizing Keep", p))
		a.markCurrentGameHistoryIssue(fmt.Sprintf("UO choice timed out for %s; forced auto-finalize", p), true)
		sendShout(fmt.Sprintf("No response from %q — finalizing Keep.", p))

		mutex.Lock()
		awaitingUOChoice = false
		uoChoiceTimeoutActive = false
		mutex.Unlock()

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
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
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

		// Final timeout hit: determine whether this is a risk session or a normal banker game.
		awaitingGameChoice = false
		gameChoiceUnreadableWarned = false
		awaitingGameChoicePartnerID = 0
		awaitingGameChoicePartnerName = ""
		gameChoiceTimeoutActive = false

		a.AddLogMsg(fmt.Sprintf("[GAME_CHOICE_TIMEOUT] final timeout for %s after %d reminders; auto-finalizing", player, reminderCount))
		sendShout(fmt.Sprintf("No response from %q — finalizing and flagging issue.", player))
		time.Sleep(1200 * time.Millisecond)

		mutex.Lock()
		riskActive := riskSessionActive
		mutex.Unlock()

		if riskActive {
			// Preserve existing behavior for risk sessions (convert bank -> payout)
			go a.finalizeRiskKeep()
		} else {
			// Mark the game as an issue, persist, send webhook, and finalize the banker trade
			a.AddLogMsg(fmt.Sprintf("[GAME_CHOICE_TIMEOUT] marking game as issue and finalizing banker trade for %s", player))
			a.markCurrentGameHistoryIssue("Player did not choose a game option; auto-finalized", true)
			go a.finalizeBankerTrade()
		}
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
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
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
				if lastTradePartnerID > 0 {
					sendShoutTargeted(lastTradePartnerID, "Shortage unresolved; closing trade")
				} else {
					sendShout("Shortage unresolved; closing trade")
				}
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
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
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
			if a.getTradeLimitViolation(itemsCopy) == nil {
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
				if lastTradePartnerID > 0 {
					sendShoutTargeted(lastTradePartnerID, "Trade still over limit; closing now.")
				} else {
					sendShout("Trade still over limit; closing now.")
				}
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
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
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
			} else if dealerGameActive() {
				addLog("[TRADE_REOPEN] jittered dealer-open announcer suppressed because game is active")
				log.Println("[TRADE_REOPEN] jittered dealer-open announcer suppressed because game is active")
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
		if v := a.getTradeLimitViolation(itemsCopy); v != nil {
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
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
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
				if v := a.getTradeLimitViolation(itemsCopy); v != nil {
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
				logPanic(r, "GOROUTINE")
			}
		}()
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
	if strings.TrimSpace(a.currentGameHistoryID) != "" {
		// If the current history entry is already Completed (for example
		// payout finished and capture cleared issue flags), do not overwrite
		// it with an Issue due to a session reset.
		a.gameHistoryMu.Lock()
		var shouldMarkIssue = true
		for _, e := range a.gameHistory {
			if e.ID == a.currentGameHistoryID {
				if strings.EqualFold(e.Status, "Completed") {
					shouldMarkIssue = false
				}
				break
			}
		}
		a.gameHistoryMu.Unlock()

		if shouldMarkIssue {
			a.markCurrentGameHistoryIssue(fmt.Sprintf("Dealer session reset before round fully resolved (%s)", reason), true)
		}
	}

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
	// gameBetItems preservation: do not clear here, as we need it for payout/risk after game results.
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
		if r := recover(); r != nil {
			logPanic(r, "RECOVERED")
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
		if fieldStr == "" {
			continue
		}
		a.AddLogMsg(fmt.Sprintf("[TRADE_PARSE_DEBUG] field=%q", fieldStr))

		// Quick check: if any active stocked_items.raw_name is a substring
		// of the incoming field, treat that as the canonical item name.
		lowField := strings.ToLower(fieldStr)
		stockedItemsMu.RLock()
		bestStocked := ""
		for raw := range stockedItemsCache {
			if raw == "" {
				continue
			}
			if strings.Contains(lowField, raw) {
				if len(raw) > len(bestStocked) {
					bestStocked = raw
				}
			}
		}
		stockedItemsMu.RUnlock()
		if bestStocked != "" {
			// Attempt to extract quantity (if present as *N)
			qty := 1
			if idx := strings.LastIndex(fieldStr, "*"); idx >= 0 && idx < len(fieldStr)-1 {
				if q, err := strconv.Atoi(fieldStr[idx+1:]); err == nil {
					qty = q
				}
			}
			counts[bestStocked] += qty
			if _, exists := rawByName[bestStocked]; !exists {
				rawByName[bestStocked] = fieldStr
			}
			continue
		}

		itemName, qty, ok := a.extractTradeItemAndQuantity(fieldStr)
		if !ok {
			// Skip fields that are definitely not items (usernames, numeric IDs)
			// to avoid false-positive unknown item violations on metadata.
			lowField := strings.ToLower(fieldStr)
			partner := strings.ToLower(strings.TrimSpace(lastTradePartnerName))
			dealer := strings.ToLower(strings.TrimSpace(a.getCurrentDealerName()))
			if lowField == partner || lowField == dealer {
				a.AddLogMsg(fmt.Sprintf("[TRADE_PARSE_DEBUG] skipping field matching trader name=%q", fieldStr))
				continue
			}

			// Skip fields that match any known room user to avoid picking up names as items.
			// This is critical for usernames with underscores that look like furni classes.
			isUser := false
			users28Mu.Lock()
			for _, u := range users28Canonical {
				if lowField == strings.ToLower(strings.TrimSpace(u.Username)) {
					isUser = true
					break
				}
			}
			users28Mu.Unlock()
			if isUser {
				a.AddLogMsg(fmt.Sprintf("[TRADE_PARSE_DEBUG] skipping field matching room user name=%q", fieldStr))
				continue
			}

			if _, err := strconv.Atoi(fieldStr); err == nil {
				a.AddLogMsg(fmt.Sprintf("[TRADE_PARSE_DEBUG] skipping numeric field (likely user id)=%q", fieldStr))
				continue
			}

			// Check whitelist early to allow managed items to bypass typical structure checks
			stockedItemsMu.RLock()
			_, whitelisted := stockedItemsCache[lowField]
			if !whitelisted {
				if star := strings.LastIndex(lowField, "*"); star > 0 {
					_, whitelisted = stockedItemsCache[lowField[:star]]
				}
			}
			stockedItemsMu.RUnlock()

			// Use normalizeTradeItemName as a heuristic to see if this string
			// even COULD be an item name. This filters out the binary junk/metadata
			// (like "cizMIf|I~s") that is present in initial trade packets.
			cleanName, looksLikeItem := normalizeTradeItemName(fieldStr)
			if !looksLikeItem && !whitelisted {
				// If it doesn't look like an item name and isn't whitelisted, it's almost certainly metadata.
				a.AddLogMsg(fmt.Sprintf("[TRADE_PARSE_DEBUG] skipping field that doesn't look like an item (likely metadata)=%q", fieldStr))
				continue
			}

			// For fields that didn't verify against the catalog or hand, only
			// treat them as unrecognized items if they have typical Habbo class
			// formatting (underscore, star suffix, or cf_ prefix) OR are whitelisted.
			if !(strings.Contains(lowField, "_") || strings.Contains(lowField, "*") || strings.HasPrefix(lowField, "cf_") || stripItemNameRe.MatchString(lowField) || whitelisted) {
				a.AddLogMsg(fmt.Sprintf("[TRADE_PARSE_DEBUG] skipping field that lacks item-like structure=%q", fieldStr))
				continue
			}

			// It's not a name or an ID, and it LOOKS like an item name, but we
			// failed to identify it as a known item.

			// Try to extract a name even if unverified.
			name := cleanName
			if match := stripItemNameRe.FindString(fieldStr); match != "" {
				name = match
			}
			a.AddLogMsg(fmt.Sprintf("[TRADE_PARSE_DEBUG] unrecognized item candidate, marking as unknown: %q", name))
			a.AddLogMsg(fmt.Sprintf("[TRADE_UNKNOWN_FIELD] raw=%q", fieldStr))

			// Generate a unique unknown item key so each unrecognized field counts
			// towards the unique item limit in getTradeLimitViolation.
			unknownKey := fmt.Sprintf("unrecognized:%s", name)
			counts[unknownKey] = 1
			if _, exists := rawByName[unknownKey]; !exists {
				rawByName[unknownKey] = fieldStr
			}
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
	isWhitelisted := func(cand string) bool {
		low := strings.ToLower(cand)
		stockedItemsMu.RLock()
		defer stockedItemsMu.RUnlock()
		_, managed := stockedItemsRegistry[low]
		if !managed {
			if star := strings.LastIndex(low, "*"); star > 0 {
				_, managed = stockedItemsRegistry[low[:star]]
			}
		}
		return managed
	}

	// Try legacy format first (e.g. "itkoHP|club_sofa")
	if strings.Contains(field, "|") {
		parts := strings.Split(field, "|")
		if len(parts) >= 2 {
			cand := strings.TrimSpace(parts[len(parts)-1])
			if strings.Contains(cand, "_") || strings.Contains(cand, "*") || stripItemNameRe.MatchString(cand) || isWhitelisted(cand) {
				if name, qty, ok := a.normalizeTradeFieldClassWithQty(cand); ok {
					return name, qty, true
				}
			}
		}
	}

	// Try current format (e.g. "irbUAXb{chair_plasty*2")
	if strings.Contains(field, "{") {
		parts := strings.SplitN(field, "{", 2)
		if len(parts) == 2 {
			cand := strings.TrimSpace(parts[1])
			if strings.Contains(cand, "_") || strings.Contains(cand, "*") || strings.HasPrefix(strings.ToLower(cand), "cf_") || stripItemNameRe.MatchString(cand) || isWhitelisted(cand) {
				if name, qty, ok := a.normalizeTradeFieldClassWithQty(cand); ok {
					return name, qty, true
				}
			}
		}
	}

	// Fallback: try strict strip regex first, then try looser token candidates.
	if match := stripItemNameRe.FindString(field); match != "" {
		if _, ok := normalizeClassKeyWithVariant(match); ok {
			if name, qty, ok2 := a.normalizeTradeFieldClassWithQty(match); ok2 {
				return name, qty, true
			}
		}
	}

	// Looser candidate scanning: find word-like tokens and try each one.
	candidateRe := regexp.MustCompile(`[A-Za-z][A-Za-z0-9_]*(?:\*\d+)?`)
	matches := candidateRe.FindAllString(field, -1)
	if len(matches) > 0 {
		for _, cand := range matches {
			if name, qty, ok := a.normalizeTradeFieldClassWithQty(cand); ok {
				return name, qty, true
			}
		}
	}

	// Relaxed fallback: some server payloads include recognizable single-word
	// class names that don't match the strict furni regex (no underscore)
	// but are still meaningful (examples: "giftflowers", "hologram").
	low := strings.ToLower(field)
	handItemsMu.Lock()
	best := ""
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

	// 1. Authoritative check: Stocked Items list.
	// If the item is in our managed list, the 'isActive' toggle is the final word.
	stockedItemsMu.RLock()
	registryItem, exists := stockedItemsRegistry[name]
	if !exists {
		if star := strings.LastIndex(name, "*"); star > 0 {
			registryItem, exists = stockedItemsRegistry[name[:star]]
		}
	}
	stockedItemsMu.RUnlock()

	if exists {
		// Found in our table: respect the user's active/inactive toggle.
		return registryItem.IsActive
	}

	// Determine the base name (without *n variant suffix) for legacy fallback checks.
	baseName := name
	if star := strings.LastIndex(name, "*"); star > 0 {
		baseName = name[:star]
	}

	// 2. Legacy fallback: catalog membership (checks even if registry has entries,
	// unless the specific item was found and marked inactive above).
	catalogSet := a.GetCatalogNameSet()
	if _, ok := catalogSet[name]; ok {
		return true
	}
	if baseName != name {
		if _, ok := catalogSet[baseName]; ok {
			return true
		}
	}

	// 3. Last resort: items currently observed in the dealer's scanned hand (ONLY used if Stocked Items is empty).
	handItemsMu.Lock()
	itemsToCheck := currentHandItems
	if tradeHandSnapshotReady && len(tradeHandSnapshot) > 0 {
		itemsToCheck = tradeHandSnapshot
	}
	for _, item := range itemsToCheck {
		itName := strings.ToLower(strings.TrimSpace(item.Name))
		if itName == name || itName == baseName {
			handItemsMu.Unlock()
			return true
		}
	}
	handItemsMu.Unlock()

	return false
}

func normalizeTradeItemName(raw string) (string, bool) {
	name := strings.TrimSpace(strings.ToLower(raw))
	// Aggressively strip null bytes and other common non-printable characters
	// that may be present in Habbo Shockwave/Origins server packets.
	name = strings.Trim(name, "\x00\r\n\t")

	if name == "" || isCoordinatePattern(name) {
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

	// If banker_trades are in progress, do not reopen the dealer now.
	if a.hasActiveBankerTrades() {
		a.AddLogMsg("[TRADE_REOPEN] refusing to reopen dealer because active banker_trades exist")
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
	// gameBetItems preservation: do not clear here, as we need it for payout/risk after game results.
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

	// If banker_trades are in progress, do not reopen dealer now.
	if a.hasActiveBankerTrades() {
		a.AddLogMsg(fmt.Sprintf("[TRADE_REOPEN] refusing to reopen idle dealer because active banker_trades exist (%s)", reason))
		awaitingTradeOpen = false
		dealerAcceptingTrades = false
		dealerTradeWindowOpen = false
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
	SafeGo(func() {
		a.forceRefreshHandSnapshot("openDealerAfterRound")
		if shouldRefreshRoomUsers() {
			requestRoomUsers(a)
		}
		// Refresh dedicated risk snapshot to reflect post-payout inventory.
		if isRiskEnabled {
			time.Sleep(250 * time.Millisecond)
			a.captureRiskSnapshot(true)
		}
	})

	dealerResyncInProgress = false
	// Do not open dealer if banker_trades are currently active.
	if a.hasActiveBankerTrades() {
		a.AddLogMsg("[DEALER_REOPEN] skipping dealer reopen due to active banker_trades")
		awaitingTradeOpen = false
		dealerAcceptingTrades = false
		dealerTradeWindowOpen = false
	} else {
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

	SafeGo(func() {
		time.Sleep(stripNextDelay)
		sendGetStripRaw(a, stripGetNextPayload)
	})

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
			SafeGo(func() {
				a.captureTradeHandSnapshot()
				// small delay to ensure snapshot processed before coverage check
				time.Sleep(50 * time.Millisecond)
				a.notifyTradeQuantityCoverage()
			})
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
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
		// If Split Dealer Mode is enabled, and the history DB is available,
		// prefer a DB-backed snapshot built from banker_inventory joined to
		// public.stocked_items where is_active = TRUE. Pull items for all
		// bankers (ignore configured banker name) per request.
		if a.GetSplitDealerMode() {
			db, owner := a.getHistoryDB()
			if db != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				var rows pgx.Rows
				var err error
				if owner != "" {
					rows, err = db.Query(ctx, `
						SELECT bi.item_name, bi.quantity
						FROM banker_inventory bi
						JOIN public.stocked_items si
						  ON LOWER(si.raw_name) = LOWER(bi.item_name)
						WHERE si.owner_key = $1 AND si.is_active = TRUE
					`, owner)
				} else {
					rows, err = db.Query(ctx, `
						SELECT bi.item_name, bi.quantity
						FROM banker_inventory bi
						JOIN public.stocked_items si
						  ON LOWER(si.raw_name) = LOWER(bi.item_name)
						WHERE si.is_active = TRUE
					`)
				}

				if err != nil {
					a.AddLogMsg(fmt.Sprintf("[TRADE_HAND_SNAPSHOT] DB query failed: %v", err))
				} else {
					defer rows.Close()
					dbItems := make([]TradeItem, 0)
					for rows.Next() {
						var name string
						var qty int
						if err := rows.Scan(&name, &qty); err != nil {
							a.AddLogMsg(fmt.Sprintf("[TRADE_HAND_SNAPSHOT] DB scan error: %v", err))
							continue
						}
						dbItems = append(dbItems, TradeItem{Name: name, Quantity: qty})
					}
					if len(dbItems) > 0 {
						snapshot = dbItems
						a.AddLogMsg(fmt.Sprintf("[TRADE_HAND_SNAPSHOT] replaced snapshot with %d DB items (all bankers)", len(dbItems)))
					} else {
						a.AddLogMsg("[TRADE_HAND_SNAPSHOT] DB returned 0 active items; keeping in-memory snapshot")
					}
				}
			}
		}

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
			MinQuantityPerItem: minTradeQuantityPerItem,
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
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
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
			MinQuantityPerItem: minTradeQuantityPerItem,
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
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
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
	if ok := waitForStripScanCompletion(scanID, 60*time.Second); !ok {
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
					parts = append(parts, fmt.Sprintf("%s %d/%d", a.formatTradeItemName(key), coverable, incomingMap[key]))
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
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
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
	mutex.Lock()
	isBandit := enabledGameBandit
	mutex.Unlock()

	if isBandit {
		msg = "We don't have enough for the multiplier"
	} else if len(shortages) == 1 {
		s := shortages[0]
		if s.HaveHand == 0 && s.Incoming == 0 {
			msg = fmt.Sprintf("We don't have %s", a.formatTradeItemName(s.Name))
		} else {
			msg = fmt.Sprintf("We need %s (%d/%d)", a.formatTradeItemName(s.Name), s.Have, s.Required)
		}
	} else {
		parts := make([]string, 0, len(shortages))
		for _, s := range shortages {
			if s.HaveHand == 0 && s.Incoming == 0 {
				parts = append(parts, fmt.Sprintf("we don't have %s", a.formatTradeItemName(s.Name)))
			} else {
				parts = append(parts, fmt.Sprintf("%s (%d/%d)", a.formatTradeItemName(s.Name), s.Have, s.Required))
			}
		}
		msg = fmt.Sprintf("Shortages: %s", strings.Join(parts, "; "))
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
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
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
	// or the Jackpot payout for Bandit mode, otherwise 2x.
	// If we've already forced Over/Under for this trade
	// (pendingUoVariant == "uo"), compute requirements as 2x so coverage
	// checks accept Over/Under offers.
	mult := 2.0
	mutex.Lock()
	if enabledGameBandit {
		mult = banditJackpotPayout
	} else if underOver7GameModeEnabled {
		mult = float64(underOver7PayoutMultiplier)
		// treat forced Over/Under as 2x
		if pendingUoVariant == "uo" {
			mult = 2.0
		}
	}
	mutex.Unlock()
	required := payoutRequirementsFromBetItemsMult(partnerItems, mult)
	if len(required) == 0 {
		return nil
	}

	// Build canonical maps aggregated by base name.
	// This ensures that variant mismatches (e.g. gold_bar*1 vs gold_bar)
	// do not cause false coverage shortages.
	handMap := map[string]int{}
	for _, it := range handSnapshot {
		name := a.getCanonicalName(it.Name)
		base := name
		if star := strings.LastIndex(name, "*"); star > 0 {
			base = name[:star]
		}
		handMap[base] += it.Quantity
	}

	incomingMap := map[string]int{}
	for _, it := range partnerItems {
		name := a.getCanonicalName(it.Name)
		base := name
		if star := strings.LastIndex(name, "*"); star > 0 {
			base = name[:star]
		}
		incomingMap[base] += it.Quantity
	}

	// Recompute required payouts using base names.
	requiredCanon := map[string]int{}
	for _, it := range partnerItems {
		name := a.getCanonicalName(it.Name)
		base := name
		if star := strings.LastIndex(name, "*"); star > 0 {
			base = name[:star]
		}
		if it.Quantity <= 0 {
			continue
		}
		requiredCanon[base] += int(math.Round(float64(it.Quantity) * mult))
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
	if v := a.getTradeLimitViolation(itemsCopy); v != nil {
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

func (a *App) formatTradeShortages(shortages []tradeShortage) string {
	parts := make([]string, 0, len(shortages))
	for _, shortage := range shortages {
		parts = append(parts, fmt.Sprintf(
			"%s hand %d traded %d",
			a.formatTradeItemName(shortage.Name),
			shortage.HaveHand,
			shortage.Required,
		))
	}
	return strings.Join(parts, ", ")
}

// sendTradeCompletionMessage sends the post-trade game prompt sequence.
func (a *App) sendTradeCompletionMessage() {
	// Redundant capture removed to prevent race conditions with TRADE_CLOSE wiping currentTradeItems.
	// gameBetItems is now captured synchronously in the TRADE_COMPLETED handler.

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
	isBandit := enabledGameBandit
	menu := buildGameChoicePromptLocked()
	mutex.Unlock()

	if isBandit {
		a.beginBanditRound()
		return
	}

	msg := fmt.Sprintf("%s: %s", partnerName, menu)

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

	// If we have a recent buffered shout from the partner, try to apply it immediately
	if a.chatBuf != nil && awaitingGameChoicePartnerID > 0 {
		if bufMsg, ok := a.chatBuf.PopMostRecent(awaitingGameChoicePartnerID, 6*time.Second); ok {
			a.AddLogMsg(fmt.Sprintf("[CHAT_BUFFER] replaying buffered shout from %d: %q", awaitingGameChoicePartnerID, bufMsg))
			if a.applyBufferedGameChoice(awaitingGameChoicePartnerID, bufMsg) {
				a.AddLogMsg("[CHAT_BUFFER] applied early buffered choice; skipping prompt")
				return
			}
		}
	}

	a.startGameChoiceTimeoutMonitor()

	a.AddLogMsg(fmt.Sprintf("[TRADE_MESSAGE] shouting: %q", msg))
	sendShout(msg)
}

// applyBufferedGameChoice attempts to parse and apply a buffered shout as an immediate
// game choice. Returns true if the buffered message was accepted and processed.
func (a *App) applyBufferedGameChoice(index int, msg string) bool {
	// Normalize choice using existing helpers
	choice, ok := normalizeIncomingGameChoice(msg)
	if !ok {
		if c, ok2 := normalizeLooseGameChoice(msg); ok2 {
			choice = c
			ok = true
		} else {
			cleaned := strings.ToLower(strings.TrimSpace(msg))
			cleaned = gameChoiceCleanupRe.ReplaceAllString(cleaned, "")
			if underOver7GameModeEnabled && (cleaned == "7" || cleaned == "seven") {
				choice = "uo7"
				ok = true
			}
		}
	}
	if !ok {
		return false
	}

	// Ensure the buffered shout is from the locked starter (if set)
	if awaitingGameChoicePartnerID > 0 && index != awaitingGameChoicePartnerID {
		return false
	}

	// Check enabled games (best-effort)
	if !underOver7GameModeEnabled && !onlyUnderOver7Mode {
		mutex.Lock()
		enabled := isGameChoiceEnabledLocked(choice)
		prompt := buildGameChoicePromptLocked()
		mutex.Unlock()
		if !enabled {
			a.AddLogMsg(fmt.Sprintf("[CHAT_BUFFER] buffered choice %q is disabled; prompt: %q", choice, prompt))
			return false
		}
	}

	// Accept and execute the choice similar to live chat handler
	stopGameChoiceTimeoutMonitor()
	awaitingGameChoice = false
	gameChoiceUnreadableWarned = false
	awaitingGameChoicePartnerID = 0
	awaitingGameChoicePartnerName = ""

	// Record history and start the appropriate round
	if choice != "tri" && choice != "uo" && choice != "uo7" {
		var ack string
		switch choice {
		case "pairup":
			ack = "PU! If you roll a double or triple you Win! Player Roll"
		case "h18":
			ack = "H18! 19+ Win / 17- Lose / 18 House! Player Roll"
		case "6":
			ack = "6! Starting, Player Roll"
		case "dt":
			ack = fmt.Sprintf("%s! Player Roll — Double Trouble: land on even to win", gameChoiceDisplay(choice))
		default:
			ack = fmt.Sprintf("%s! Starting, Player Roll", gameChoiceDisplay(choice))
		}

		if riskSessionActive {
			a.beginRiskRoundHistory(choice, msg, gameChoiceDisplay(choice))
		} else {
			a.setCurrentGameHistoryChoice(choice, msg)
			a.setCurrentGameHistoryGame(gameChoiceDisplay(choice))
		}

		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] shouting: %q", ack))
		sendShout(ack)
	} else {
		if choice == "tri" {
			a.AddLogMsg("[GAME_SELECT] Tri selected via buffer; prompting for High/Low")
		} else {
			a.AddLogMsg("[GAME_SELECT] Under/Over selected via buffer; prompting for Over/Under/7")
		}
	}

	switch choice {
	case "pkr":
		resetBlackjackSequence()
		a.AddLogMsg("[GAME_SELECT] buffered Pkr start")
		a.beginPokerSequence()
	case "21":
		a.AddLogMsg("[GAME_SELECT] buffered 21 start")
		a.beginBlackjackSequence()
	case "13":
		a.AddLogMsg("[GAME_SELECT] buffered 13 start")
		a.begin13Sequence()
	case "6":
		a.AddLogMsg("[GAME_SELECT] buffered 6 start")
		a.beginSixSequence()
	case "pairup":
		a.AddLogMsg("[GAME_SELECT] buffered PairUp start")
		a.beginPairUpRound()
	case "h18":
		a.AddLogMsg("[GAME_SELECT] buffered H18 start")
		a.beginH18Round()
	case "dt":
		a.AddLogMsg("[GAME_SELECT] buffered DT start")
		a.beginDoubleTroubleRound()
	case "tri":
		a.AddLogMsg("[GAME_SELECT] buffered Tri -> prompting High/Low")
		a.beginTriChoiceSequence()
	case "uo7":
		if !underOver7GameModeEnabled {
			pendingUoVariant = "uo"
			a.noteCurrentGameHistory("Mixed-mode UO7 selected via buffer - forcing Over/Under (no 7)")
			a.beginUOChoiceSequence()
			return true
		}
		// try to start UO7 immediately; if coverage later blocks it will fallback in game logic
		a.AddLogMsg("[GAME_SELECT] buffered UO7 start -> attempting Under/Over7")
		a.beginUO7ChoiceSequence()
	case "uo":
		a.AddLogMsg("[GAME_SELECT] buffered UO -> prompting Over/Under")
		a.beginUOChoiceSequence()
	case "uo_over":
		if riskSessionActive {
			SafeGo(func() {
				time.Sleep(1400 * time.Millisecond)
				a.executeRiskRound()
			})
		} else {
			a.setCurrentGameHistoryGame("UO7")
			a.beginUnderOverRound("over")
		}
	case "uo_under":
		if riskSessionActive {
			SafeGo(func() {
				time.Sleep(1400 * time.Millisecond)
				a.executeRiskRound()
			})
		} else {
			a.setCurrentGameHistoryGame("UO7")
			a.beginUnderOverRound("under")
		}
	case "trihigh":
		a.AddLogMsg("[GAME_SELECT] buffered TriH start")
		a.beginTriRound("high")
	case "trilow":
		a.AddLogMsg("[GAME_SELECT] buffered TriL start")
		a.beginTriRound("low")
	case "mh":
		a.AddLogMsg("[GAME_SELECT] buffered MidHouse -> prompting u10/o11")
		a.beginMidHouseChoiceSequence()
	case "mh_u10":
		a.AddLogMsg("[GAME_SELECT] buffered u10 start")
		a.beginMidHouseRound("u10")
	case "mh_o11":
		a.AddLogMsg("[GAME_SELECT] buffered o11 start")
		a.beginMidHouseRound("o11")
	}

	return true
}

func setEnabledGamesFromSelection(codes []string) {
	// Defaults preserve historical behavior when no selection is provided.
	enabledGamePkr = true
	enabledGame21 = true
	enabledGame13 = true
	enabledGameTri = true
	enabledGameDT = true
	enabledGameUO7 = false
	enabledGamePairUp = true
	enabledGameH18 = true
	enabledGameBandit = false
	enabledGameMH = false

	if len(codes) == 0 {
		return
	}

	enabledGamePkr = false
	enabledGame21 = false
	enabledGame13 = false
	enabledGame6 = false
	enabledGameTri = false
	enabledGameDT = false
	enabledGameUO7 = false
	enabledGamePairUp = false
	enabledGameH18 = false
	enabledGameBandit = false
	enabledGameMH = false

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
		case "6":
			enabledGame6 = true
		case "tri", "trih", "tril", "trihigh", "trilow":
			enabledGameTri = true
		case "dt", "double", "doubletrouble":
			enabledGameDT = true
		case "uo", "uo7", "underover", "underover7":
			enabledGameUO7 = true
		case "pu", "pu3", "pairup":
			enabledGamePairUp = true
		case "h18":
			enabledGameH18 = true
		case "bandit", "oab", "onearmbandit":
			enabledGameBandit = true
		case "mh", "midhouse", "1011":
			enabledGameMH = true
		}
	}

	if !enabledGamePkr && !enabledGame21 && !enabledGame13 && !enabledGame6 && !enabledGameTri && !enabledGameUO7 && !enabledGamePairUp && !enabledGameH18 && !enabledGameBandit && !enabledGameMH {
		enabledGamePkr = true
		enabledGame21 = true
		enabledGame13 = true
		enabledGame6 = true
		enabledGameTri = true
		enabledGamePairUp = true
		enabledGameH18 = true
	}
}

func enabledGameChoicePartsLocked() []string {
	parts := make([]string, 0, 7)
	if enabledGamePkr {
		parts = append(parts, "pkr")
	}
	if enabledGame21 {
		parts = append(parts, "21")
	}
	if enabledGame13 {
		parts = append(parts, "13")
	}
	if enabledGame6 {
		parts = append(parts, "6")
	}
	if enabledGameTri {
		parts = append(parts, "tri")
	}
	if enabledGameDT {
		parts = append(parts, "dt")
	}
	if enabledGameUO7 {
		parts = append(parts, "u7", "o7")
	}
	if enabledGamePairUp {
		parts = append(parts, "pu")
	}
	if enabledGameH18 {
		parts = append(parts, "h18")
	}
	if enabledGameBandit {
		parts = append(parts, "bandit")
	}
	if enabledGameMH {
		parts = append(parts, "u10", "o11")
	}
	if len(parts) == 0 {
		parts = append(parts, "pkr", "21", "13", "tri", "pu", "h18")
	}
	return parts
}

func buildGameChoicePromptLocked() string {
	if enabledGameBandit {
		return "Bandit Mode"
	}
	if underOver7GameModeEnabled {
		m := underOver7PayoutMultiplier
		if pendingUoVariant == "uo" {
			return "U/O (x2)"
		}
		return fmt.Sprintf("U/O (x2) or 7 (x%d)", m)
	}
	if onlyUnderOver7Mode {
		return "U/O (x2)"
	}
	return strings.Join(enabledGameChoicePartsLocked(), "/")
}

func isGameChoiceEnabledLocked(choice string) bool {
	switch choice {
	case "pkr":
		return enabledGamePkr
	case "21":
		return enabledGame21
	case "13":
		return enabledGame13
	case "6":
		return enabledGame6
	case "tri", "trihigh", "trilow":
		return enabledGameTri
	case "dt":
		return enabledGameDT
	case "uo", "uo7", "uo_over", "uo_under":
		return enabledGameUO7
	case "pairup":
		return enabledGamePairUp
	case "h18":
		return enabledGameH18
	case "bandit":
		return enabledGameBandit
	case "mh", "mh_u10", "mh_o11":
		return enabledGameMH
	default:
		return false
	}
}

// SetBanditJackpotPayout sets the payout multiplier for Triple 6s in One Arm Bandit.
func (a *App) SetBanditJackpotPayout(payout float64) {
	mutex.Lock()
	banditJackpotPayout = payout
	mutex.Unlock()
	a.AddLogMsg(fmt.Sprintf("[CONFIG] Bandit Jackpot Payout set to x%.2f", payout))
}

// SetBanditTriplesPayout sets the payout multiplier for other triples in One Arm Bandit.
func (a *App) SetBanditTriplesPayout(payout float64) {
	mutex.Lock()
	banditTriplesPayout = payout
	mutex.Unlock()
	a.AddLogMsg(fmt.Sprintf("[CONFIG] Bandit Triples Payout set to x%.2f", payout))
}

func (a *App) formatTradeItemName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))

	// Check stocked items first for a friendly name
	stockedItemsMu.RLock()
	for _, it := range stockedItems {
		if strings.ToLower(strings.TrimSpace(it.RawName)) == name || strings.ToLower(strings.TrimSpace(it.CanonicalName)) == name {
			stockedItemsMu.RUnlock()
			return it.DisplayName
		}
	}
	stockedItemsMu.RUnlock()

	if strings.HasPrefix(name, "unrecognized:") {
		raw := name[13:]
		// If it has a star variant suffix (*109), strip it for display.
		if star := strings.LastIndex(raw, "*"); star > 0 {
			raw = raw[:star]
		}
		return a.formatTradeItemName(raw)
	}
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
		if r := recover(); r != nil {
			logPanic(r, "RECOVERED")
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
		if r := recover(); r != nil {
			logPanic(r, "RECOVERED")
			ok = false
		}
	}()

	value = pkt.ReadInt()
	pos = pkt.Pos
	return value, pos, true
}

func tryReadString(pkt *g.Packet) (value string, pos int, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			logPanic(r, "RECOVERED")
			ok = false
		}
	}()

	value = pkt.ReadString()
	pos = pkt.Pos
	return value, pos, true
}

func tryReadIntString(pkt *g.Packet) (id int, text string, pos int, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			logPanic(r, "RECOVERED")
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
		if r := recover(); r != nil {
			logPanic(r, "RECOVERED")
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
			reset13Sequence()
			thirteenRoundActive = true
			thirteenPlayerTurn = true
			thirteenPlayerName = strings.TrimSpace(lastTradePartnerName)
			if thirteenPlayerName == "" {
				thirteenPlayerName = "Player"
			}
			is13Rolling = true
			logRollResult := fmt.Sprintf("13 Roll:\n")
			a.AddLogMsg(logRollResult)
			go a.roll13Dice()
		case strings.HasSuffix(command, "6"):
			e.Block()
			resetSixSequence()
			sixRoundActive = true
			sixPlayerTurn = true
			sixPlayerName = strings.TrimSpace(lastTradePartnerName)
			if sixPlayerName == "" {
				sixPlayerName = "Player"
			}
			isSixRolling = true
			logRollResult := fmt.Sprintf("6 Roll:\n")
			a.AddLogMsg(logRollResult)
			go a.rollSixDice()
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

	SafeGo(func() {
		// Wait the same total delay previously used (700 + 700ms) before
		// starting the player's roll; the public shout was already sent above.
		time.Sleep(1400 * time.Millisecond)
		a.AddLogMsg("[GAME_SELECT] starting player roll")
		a.startPokerRoll()
	})
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
	a.setCurrentGameHistoryGame("21")

	SafeGo(func() {
		// Combined ack already announced; delay then start player's BJ roll
		time.Sleep(1400 * time.Millisecond)
		isBJRolling = true
		a.AddLogMsg("21 Roll:\n")
		go a.rollBjDice()
	})
}

func (a *App) beginSixSequence() {
	playerName := strings.TrimSpace(lastTradePartnerName)
	if playerName == "" {
		playerName = "Player"
	}

	resetPokerSequence()
	resetSixSequence()
	sixRoundActive = true
	sixPlayerTurn = true
	sixPlayerName = playerName
	a.setCurrentGameHistoryGame("6")

	SafeGo(func() {
		time.Sleep(1400 * time.Millisecond)
		isSixRolling = true
		a.AddLogMsg("6 Roll:\n")
		go a.rollSixDice()
	})
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
	a.setCurrentGameHistoryGame("13")

	SafeGo(func() {
		// Combined ack already announced; delay then start player's 13 roll
		time.Sleep(1400 * time.Millisecond)
		is13Rolling = true
		a.AddLogMsg("13 Roll:\n")
		go a.roll13Dice()
	})
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
	a.startTriChoiceTimeoutMonitor(playerName)
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

	SafeGo(func() {
		// Combined ack already announced; delay then start player's Tri roll
		time.Sleep(1400 * time.Millisecond)
		isTriRolling = true
		a.rollTriDice()
	})
}

// beginDoubleTroubleRound starts the Double Trouble (2-dice) round.
func (a *App) beginDoubleTroubleRound() {
	playerName := strings.TrimSpace(lastTradePartnerName)
	if playerName == "" {
		playerName = "Player"
	}

	resetPokerSequence()
	resetBlackjackSequence()
	reset13Sequence()
	resetTriSequence()

	isDTRolling = true
	a.setCurrentGameHistoryGame("DT")

	SafeGo(func() {
		time.Sleep(1400 * time.Millisecond)
		a.rollDoubleTroubleDice()
	})
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

	msg := "Pick O (8-12) gl or U (2-6) gl"
	a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] shouting: %q", msg))
	sendShout(msg)
	a.startUOChoiceTimeoutMonitor(playerName)
}

// beginUO7ChoiceSequence prompts the player to choose Over, Under or 7
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

	msg := "UO7 selected. Pick O (8-12) gl, U (2-6) gl or 7"
	a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] shouting: %q", msg))
	sendShout(msg)
	a.startUOChoiceTimeoutMonitor(playerName)
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
					parts = append(parts, fmt.Sprintf("%s %d/%d", a.formatTradeItemName(key), coverable, incomingMap[key]))
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
		defer func() {
			if r := recover(); r != nil {
				logPanic(r, "GOROUTINE")
			}
		}()
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

	SafeGo(func() {
		time.Sleep(1400 * time.Millisecond)
		isUORolling = true
		a.rollUnderOverDice()
	})
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
			// For the special Under/Over-7 variant, restrict fake dice to 2-6
			if uoVariantForRound == "uo7" {
				diceList[index].Value = rand.Intn(5) + 2 // 2..6
			} else {
				diceList[index].Value = rand.Intn(6) + 1 // 1..6
			}
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

// rollDoubleTroubleDice rolls two dice for Double Trouble and evaluates the result.
func (a *App) rollDoubleTroubleDice() {
	if fakeDiceTestingMode {
		mutex.Lock()
		if len(diceList) < 2 {
			mutex.Unlock()
			a.AddLogMsg("[DT] Not enough dice to roll")
			isDTRolling = false
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
		a.evaluateDoubleTroubleRound()
		isDTRolling = false
		return
	}

	mutex.Lock()
	var indices []int
	if len(diceList) >= 5 {
		indices = []int{0, 4}
	} else if len(diceList) >= 2 {
		indices = []int{0, 1}
	}
	if len(indices) < 2 {
		mutex.Unlock()
		a.AddLogMsg("[DT] Not enough dice to roll")
		isDTRolling = false
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

	a.evaluateDoubleTroubleRound()
	isDTRolling = false
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
	payoutMultiplierForRound = float64(mult)
	// Record multiplier in game history so UI/webhooks reflect the correct payout
	a.setCurrentGameHistoryPayoutMultiplier(float64(mult))

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
	isUORolling = false

	// clear any awaiting choice state and mark the UO round finished
	awaitingUOChoice = false
	awaitingUOChoicePartnerID = 0
	awaitingUOChoicePartnerName = ""

	if playerWins && payoutTargetID > 0 {
		a.setCurrentGameHistoryResults(strconv.Itoa(total), "", playerName, "Payout Pending", false)
		a.noteCurrentGameHistory(winnerMsg)
		resetPayoutRetryState()

		// Post the round outcome immediately so Discord shows the win.
		a.sendDiscordRoundResult(playerName, strconv.Itoa(total), "", winnerMsg)

		// Never route Under/Over-7 rounds into Risk (UO7 is auto-payout only).
		if isRiskEnabled && uoVariantForRound != "uo7" {
			if riskSessionActive {
				go a.applyRiskOutcome(true)
				return
			}
			params := map[string]interface{}{"uoChoice": uoPlayerChoice}
			riskGame := "UO"
			if uoVariantForRound == "uo7" {
				riskGame = "UO7"
			}
			go a.handlePlayerWinRisk(cloneTradeItems(gameBetItems), payoutTargetName, payoutTargetID, riskGame, params)
			return
		}
		startPayout(a, payoutTargetID, payoutTargetName)
		return
	}

	a.setCurrentGameHistoryResults(strconv.Itoa(total), "", a.getCurrentDealerName(), "Completed", true)
	a.noteCurrentGameHistory(winnerMsg)

	mutex.Lock()
	splitEnabled := isSplitDealerMode
	mutex.Unlock()
	if splitEnabled {
		a.finalizeBankerTrade()
	}

	// If a risk session is active, route this loss through the risk logic
	// so only the pending bet is lost and the player can be re-prompted.
	// Do NOT route UO7 rounds into Risk.
	if isRiskEnabled && riskSessionActive && uoVariantForRound != "uo7" {
		go a.applyRiskOutcome(false)
		return
	}

	// Ensure the winner message is delivered before reopening the dealer
	SafeGo(func() {
		time.Sleep(1200 * time.Millisecond)
		a.openDealerAfterRound()
	})
}

func (a *App) start13DealerTurn(reason string) {
	awaiting13Decision = false
	thirteenPlayerTurn = false
	a.AddLogMsg(fmt.Sprintf("[13_DEBUG] dealer turn starting reason=%s playerTotal=%d dealerTotal=%d", reason, thirteenPlayerTotal, thirteenDealerTotal))
	a.AddLogMsg("[GAME_SELECT] starting dealer roll")
	SafeGo(func() {
		time.Sleep(700 * time.Millisecond)
		is13Rolling = true
		a.roll13Dice()
	})
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
	is13Rolling = false
	is13Hitting = false
	reset13Sequence()

	if playerWins && payoutTargetID > 0 {
		a.setCurrentGameHistoryResults(playerHand, dealerHand, playerName, "Payout Pending", false)
		a.noteCurrentGameHistory(winnerMsg)
		a.AddLogMsg(fmt.Sprintf("[PAYOUT] 13 player won, initiating payout trade to %s (%d)", payoutTargetName, payoutTargetID))
		resetPayoutRetryState()

		// Post the round outcome immediately so Discord shows who won this roll.
		a.sendDiscordRoundResult(playerName, playerHand, dealerHand, winnerMsg)

		if isRiskEnabled {
			if riskSessionActive {
				go a.applyRiskOutcome(true)
				return
			}
			go a.handlePlayerWinRisk(cloneTradeItems(gameBetItems), payoutTargetName, payoutTargetID, "13", nil)
			return
		}
		startPayout(a, payoutTargetID, payoutTargetName)
		return
	}

	a.setCurrentGameHistoryResults(playerHand, dealerHand, a.getCurrentDealerName(), "Completed", true)
	a.noteCurrentGameHistory(winnerMsg)

	// Finalize banker trade on dealer win
	a.finalizeBankerTrade()

	if isRiskEnabled && riskSessionActive {
		go a.applyRiskOutcome(false)
		return
	}
	go a.openDealerAfterRound()
}

func (a *App) startSixDealerTurn(reason string) {
	awaitingSixDecision = false
	sixPlayerTurn = false
	a.AddLogMsg(fmt.Sprintf("[6_DEBUG] dealer turn starting reason=%s playerTotal=%d dealerTotal=%d", reason, sixPlayerTotal, sixDealerTotal))
	a.AddLogMsg("[GAME_SELECT] starting dealer roll")
	SafeGo(func() {
		time.Sleep(700 * time.Millisecond)
		isSixRolling = true
		a.rollSixDice()
	})
}

func (a *App) finalizeSixRound(playerWins bool, reason string) {
	playerName := strings.TrimSpace(sixPlayerName)
	if playerName == "" {
		playerName = strings.TrimSpace(lastTradePartnerName)
	}
	if playerName == "" {
		playerName = "Player"
	}

	playerHand := strconv.Itoa(sixPlayerTotal)
	dealerHand := strconv.Itoa(sixDealerTotal)
	winnerName := "Dealer"
	if playerWins {
		winnerName = playerName
	}
	winnerMsg := fmt.Sprintf("%s Wins - %s: %s | Dealer: %s", winnerName, playerName, playerHand, dealerHand)

	a.AddLogMsg(fmt.Sprintf("[6_RULES] winner=%s reason=%s player=%d dealer=%d", winnerName, reason, sixPlayerTotal, sixDealerTotal))
	a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] shouting: %q", winnerMsg))
	if !ChatIsDisabled {
		waitForUnmute(90 * time.Second)
		time.Sleep(800 * time.Millisecond)
		sendMessageWithDelay(winnerMsg)
	}

	payoutTargetID := lastTradePartnerID
	payoutTargetName := playerName
	isSixRolling = false
	isSixHitting = false
	resetSixSequence()

	if playerWins && payoutTargetID > 0 {
		a.setCurrentGameHistoryResults(playerHand, dealerHand, playerName, "Payout Pending", false)
		a.noteCurrentGameHistory(winnerMsg)
		a.AddLogMsg(fmt.Sprintf("[PAYOUT] 6 player won, initiating payout trade to %s (%d)", payoutTargetName, payoutTargetID))
		resetPayoutRetryState()

		// Post the round outcome immediately so Discord shows who won this roll.
		a.sendDiscordRoundResult(playerName, playerHand, dealerHand, winnerMsg)

		if isRiskEnabled {
			if riskSessionActive {
				go a.applyRiskOutcome(true)
				return
			}
			go a.handlePlayerWinRisk(cloneTradeItems(gameBetItems), payoutTargetName, payoutTargetID, "6", nil)
			return
		}
		startPayout(a, payoutTargetID, payoutTargetName)
		return
	}

	a.setCurrentGameHistoryResults(playerHand, dealerHand, a.getCurrentDealerName(), "Completed", true)
	a.noteCurrentGameHistory(winnerMsg)

	// Finalize banker trade on dealer win
	a.finalizeBankerTrade()

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
	isTriRolling = false
	resetTriSequence()

	if playerWins && payoutTargetID > 0 {
		a.setCurrentGameHistoryResults(playerHand, dealerHand, playerName, "Payout Pending", false)
		a.noteCurrentGameHistory(winnerMsg)
		a.AddLogMsg(fmt.Sprintf("[PAYOUT] tri player won, initiating payout trade to %s (%d)", payoutTargetName, payoutTargetID))
		resetPayoutRetryState()

		// Post the round outcome immediately so Discord shows who won this roll.
		a.sendDiscordRoundResult(playerName, playerHand, dealerHand, winnerMsg)

		if isRiskEnabled {
			if riskSessionActive {
				go a.applyRiskOutcome(true)
				return
			}
			params := map[string]interface{}{"mode": triMode}
			go a.handlePlayerWinRisk(cloneTradeItems(gameBetItems), payoutTargetName, payoutTargetID, "Tri", params)
			return
		}
		startPayout(a, payoutTargetID, payoutTargetName)
		return
	}

	a.setCurrentGameHistoryResults(playerHand, dealerHand, "Dealer", "Completed", true)
	a.noteCurrentGameHistory(winnerMsg)

	mutex.Lock()
	splitEnabled := isSplitDealerMode
	mutex.Unlock()
	if splitEnabled {
		a.finalizeBankerTrade()
	}

	if isRiskEnabled && riskSessionActive {
		go a.applyRiskOutcome(false)
		return
	}
	go a.openDealerAfterRound()
}

func (a *App) beginH18Round() {
	playerName := strings.TrimSpace(lastTradePartnerName)
	if playerName == "" {
		playerName = "Player"
	}

	resetPokerSequence()
	resetBlackjackSequence()
	reset13Sequence()
	resetTriSequence()
	resetH18Sequence()

	h18RoundActive = true
	a.setCurrentGameHistoryGame("H18")

	SafeGo(func() {
		// Combined ack already announced; delay then start player's H18 roll
		time.Sleep(1400 * time.Millisecond)
		isH18Rolling = true
		a.rollH18Dice()
	})
}

func (a *App) rollH18Dice() {
	if fakeDiceTestingMode {
		mutex.Lock()
		if len(diceList) < 5 {
			mutex.Unlock()
			log.Println("Not enough dice to roll")
			isH18Rolling = false
			return
		}
		for i := range diceList {
			diceList[i].Value = rand.Intn(6) + 1
			diceList[i].IsClosed = false
			logRollResult := fmt.Sprintf("Dice %d rolled: %d\n", diceList[i].ID, diceList[i].Value)
			a.AddLogMsg(logRollResult)
		}
		mutex.Unlock()
		a.evaluateH18Round()
		isH18Rolling = false
		return
	}

	mutex.Lock()
	if len(diceList) < 5 {
		mutex.Unlock()
		log.Println("Not enough dice to roll")
		isH18Rolling = false
		return
	}
	resultsWaitGroup.Add(5)
	mutex.Unlock()

	for i := 0; i < 5; i++ {
		diceList[i].Roll()
		time.Sleep(rollDelay + time.Duration(rand.Intn(100))*time.Millisecond)
	}

	time.Sleep(1000 * time.Millisecond)
	resultsWaitGroup.Wait()

	a.evaluateH18Round()
	isH18Rolling = false
}

func (a *App) evaluateH18Round() {
	mutex.Lock()
	total := 0
	for i := 0; i < 5; i++ {
		total += diceList[i].Value
	}
	h18RoundActive = false
	mutex.Unlock()

	playerName := strings.TrimSpace(lastTradePartnerName)
	if playerName == "" {
		playerName = "Player"
	}

	resultMsg := fmt.Sprintf("%s rolled %d.", playerName, total)
	var status string
	var multiplier int

	if total >= 19 {
		status = "WIN"
		multiplier = 2
	} else if total <= 17 {
		status = "LOSE"
		multiplier = 0
	} else {
		// House edge: Total is exactly 18
		status = "Dealer WIN"
		multiplier = 0
	}

	msg := fmt.Sprintf("%s - %s!", resultMsg, status)
	a.AddLogMsg(fmt.Sprintf("[H18_RULES] player=%s total=%d status=%s", playerName, total, status))
	sendShout(msg)

	payoutTargetID := lastTradePartnerID
	payoutTargetName := playerName
	isH18Rolling = false

	if multiplier > 0 && payoutTargetID > 0 {
		payoutMultiplierForRound = float64(multiplier)
		a.setCurrentGameHistoryPayoutMultiplier(float64(multiplier))
		a.setCurrentGameHistoryResults(strconv.Itoa(total), "", playerName, "Payout Pending", false)
		a.noteCurrentGameHistory(msg)
		resetPayoutRetryState()

		// Post round result to Discord for visibility
		a.sendDiscordRoundResult(playerName, strconv.Itoa(total), "", msg)

		if isRiskEnabled {
			if riskSessionActive {
				go a.applyRiskOutcome(true)
				return
			}
			go a.handlePlayerWinRisk(cloneTradeItems(gameBetItems), payoutTargetName, payoutTargetID, "H18", nil)
			return
		}

		go startPayout(a, payoutTargetID, payoutTargetName)
	} else {
		a.setCurrentGameHistoryResults(strconv.Itoa(total), "", "Dealer", "Completed", true)
		a.noteCurrentGameHistory(msg)

		mutex.Lock()
		splitEnabled := isSplitDealerMode
		mutex.Unlock()
		if splitEnabled {
			a.finalizeBankerTrade()
		}

		if isRiskEnabled && riskSessionActive {
			go a.applyRiskOutcome(false)
			return
		}
		go a.openDealerAfterRound()
	}
}

func resetH18Sequence() {
	isH18Rolling = false
	h18RoundActive = false
}

func resetBanditSequence() {
	isBanditRolling = false
	banditRoundActive = false
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
	isPokerRolling, isTriRolling, isBJRolling, is13Rolling, isSixRolling, isHitting, is13Hitting, isSixHitting, isPairUpRolling, isH18Rolling, isBanditRolling, isUORolling, isClosing = false, false, false, false, false, false, false, false, false, false, false, false, false

	// Ensure any pending game-choice timeout is stopped when resetting dice.
	resetTradeAutoFlow()
	resetPokerSequence()
	resetBlackjackSequence()
	reset13Sequence()
	resetTriSequence()
	resetH18Sequence()
	resetBanditSequence()
	resetMidHouseSequence()
	lastTradePartnerID = 0
	lastTradePartnerName = ""
	lastTradePartnerToken = ""
	gameBetItems = nil
	lastTradeCoverageNotice = ""
	lastTradeBlockNotice = ""
	tradeLimitWasActive = false
	lastTradeLimitNotice = ""
	lastTradeLimitShoutAt = time.Time{}
	partnerTradeAccepted = false
	partnerAcceptedSnapshot = nil
}

// getExpectedDiceCount returns the number of dice required for setup based on
// the current dealer mode. Under/Over-7 mode requires only 2 dice; otherwise
// the default is 5.
func getExpectedDiceCount() int {
	// Use 5 dice for Mid-House to support dice 1 and 5.
	if enabledGameMH {
		return 5
	}
	// Use 3 dice for One Arm Bandit mode.
	if enabledGameBandit {
		return 3
	}
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
func (a *App) StartCasinoSetup(dealerName string, roomName string, maxUniqueItems int, maxQuantityPerItem int, minQuantityPerItem int, riskEnabled bool, enabledGames []string) {
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
	if minQuantityPerItem < 1 {
		minQuantityPerItem = 1
	}
	maxTradeUniqueItems = maxUniqueItems
	maxTradeQuantityPerItem = maxQuantityPerItem
	minTradeQuantityPerItem = minQuantityPerItem
	a.AddLogMsg(fmt.Sprintf("[CONFIG] max trade unique items = %d", maxTradeUniqueItems))
	a.AddLogMsg(fmt.Sprintf("[CONFIG] max trade quantity per item = %d", maxTradeQuantityPerItem))
	a.AddLogMsg(fmt.Sprintf("[CONFIG] min trade quantity per item = %d", minTradeQuantityPerItem))

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
	// gameBetItems preservation: do not clear here, as we need it for payout/risk after game results.
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

	// Prevent opening when banker_trades are in progress.
	if a.hasActiveBankerTrades() {
		a.AddLogMsg(fmt.Sprintf("[DEALER_SETUP] refusing to open dealer because active banker_trades exist (%s)", reason))
		awaitingTradeOpen = false
		dealerAcceptingTrades = false
		dealerTradeWindowOpen = false
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
				diceList[i].Value = pair.adjValue
				diceList[i].IsClosed = diceList[i].Value == 0

				if dice.IsRolling && (isPokerRolling || isTriRolling || isBJRolling || is13Rolling || isSixRolling || is13Hitting || isSixHitting || isHitting || isUORolling || isDTRolling || isPairUpRolling || isH18Rolling || isBanditRolling || isMidHouseRolling) {
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

				if isPokerRolling || isTriRolling || isBJRolling || is13Rolling || isSixRolling || is13Hitting || isSixHitting || isHitting || isUORolling || isDTRolling || isPairUpRolling || isH18Rolling || isBanditRolling || isMidHouseRolling {
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

// Roll dice for 6-style game (target sum is 6)
func (a *App) rollSixDice() {
	defer func() {
		isSixRolling = false
		if r := recover(); r != nil {
			a.AddLogMsg(fmt.Sprintf("[6_CRASH_GUARD] recovered panic in rollSixDice: %v", r))
		}
	}()

	if fakeDiceTestingMode {
		mutex.Lock()
		if len(diceList) < 1 {
			mutex.Unlock()
			a.AddLogMsg("[6] Not enough dice to roll")
			return
		}
		sixNextHitIndex = 1
		currentSum = 0
		for _, index := range []int{0} {
			diceList[index].Value = rand.Intn(6) + 1
			diceList[index].IsClosed = false
			currentSum += diceList[index].Value
			logRollResult := fmt.Sprintf("Dice %d rolled: %d\n", diceList[index].ID, diceList[index].Value)
			a.AddLogMsg(logRollResult)
		}
		mutex.Unlock()

		a.evaluateSixHand()
		return
	}

	mutex.Lock()

	if len(diceList) < 1 {
		mutex.Unlock()
		a.AddLogMsg("[6] Not enough dice to roll")
		return
	}

	sixNextHitIndex = 1
	currentSum = 0 // Reset sum before starting
	resultsWaitGroup.Add(1)
	mutex.Unlock()

	// Roll the first dice
	for _, index := range []int{0} {
		diceList[index].Roll()
		time.Sleep(rollDelay + time.Duration(rand.Intn(100))*time.Millisecond)
	}

	time.Sleep(1000 * time.Millisecond)
	a.waitForSixDiceResults([]int{0}, 5*time.Second, "initial-roll")

	mutex.Lock()
	for _, index := range []int{0} {
		currentSum += diceList[index].Value
	}
	mutex.Unlock()

	a.evaluateSixHand()
}

func (a *App) hitSixDice() {
	defer func() {
		sixHitInFlight = false
		isSixHitting = false
		isSixRolling = false
		if r := recover(); r != nil {
			a.AddLogMsg(fmt.Sprintf("[6_CRASH_GUARD] recovered panic in hitSixDice: %v", r))
		}
	}()

	if fakeDiceTestingMode {
		mutex.Lock()
		if len(diceList) < 1 {
			mutex.Unlock()
			a.AddLogMsg("[6] Not enough dice to roll")
			return
		}

		slot := sixNextHitIndex
		if slot < 0 || slot >= len(diceList) {
			slot = 0
		}
		sixNextHitIndex = (slot + 1) % len(diceList)

		diceList[slot].Value = rand.Intn(6) + 1
		diceList[slot].IsClosed = false
		currentSum += diceList[slot].Value
		logRollResult := fmt.Sprintf("Dice %d rolled: %d\n", diceList[slot].ID, diceList[slot].Value)
		a.AddLogMsg(logRollResult)
		mutex.Unlock()

		a.evaluateSixHand()
		return
	}
	mutex.Lock()

	if len(diceList) < 1 {
		mutex.Unlock()
		a.AddLogMsg("[6] Not enough dice to roll")
		return
	}

	slot := sixNextHitIndex
	if slot < 0 || slot >= len(diceList) {
		slot = 0
	}
	nextSlot := (slot + 1) % len(diceList)
	sixNextHitIndex = nextSlot
	mutex.Unlock()

	resultsWaitGroup.Add(1)
	diceList[slot].Roll()

	time.Sleep(rollDelay + time.Duration(rand.Intn(100))*time.Millisecond)
	a.waitForSixDiceResults([]int{slot}, 5*time.Second, "hit-roll")
	newValue := diceList[slot].Value

	mutex.Lock()
	currentSum = currentSum + newValue // Adjust current sum
	mutex.Unlock()

	// Re-evaluate the hand with the updated sum
	a.evaluateSixHand()
}

func (a *App) waitForSixDiceResults(slots []int, timeout time.Duration, reason string) {
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
			return
		}

		if time.Now().After(deadline) {
			mutex.Lock()
			for _, slot := range pending {
				if slot < 0 || slot >= len(diceList) {
					continue
				}
				if !diceList[slot].IsRolling {
					continue
				}
				diceList[slot].IsRolling = false
				func() {
					defer func() {
						if r := recover(); r != nil { logPanic(r, "RECOVERED") }
					}()
					resultsWaitGroup.Done()
				}()
			}
			mutex.Unlock()
			return
		}

		time.Sleep(75 * time.Millisecond)
	}
}

func (a *App) beginPairUpRound() {
	playerName := strings.TrimSpace(lastTradePartnerName)
	if playerName == "" {
		playerName = "Player"
	}

	resetPokerSequence()
	resetBlackjackSequence()
	reset13Sequence()
	resetTriSequence()

	a.setCurrentGameHistoryGame("Pair Up")

	SafeGo(func() {
		time.Sleep(1400 * time.Millisecond)
		isPairUpRolling = true
		a.rollPairUpDice()
	})
}

func (a *App) rollPairUpDice() {
	if fakeDiceTestingMode {
		mutex.Lock()
		if len(diceList) < 5 {
			mutex.Unlock()
			log.Println("Not enough dice to roll")
			isPairUpRolling = false
			return
		}
		for _, index := range []int{0, 2, 4} {
			diceList[index].Value = rand.Intn(6) + 1
			diceList[index].IsClosed = false
			logRollResult := fmt.Sprintf("Dice %d rolled: %d\n", diceList[index].ID, diceList[index].Value)
			a.AddLogMsg(logRollResult)
		}
		mutex.Unlock()
		a.evaluatePairUpRound()
		isPairUpRolling = false
		return
	}

	mutex.Lock()

	if len(diceList) < 5 {
		mutex.Unlock()
		log.Println("Not enough dice to roll")
		isPairUpRolling = false
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

	a.evaluatePairUpRound()
	isPairUpRolling = false
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
			"Rolls 5 dice and if chat is enabled \nshouts the results in chat. \n" +
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
			"Auto rolls 3 dice in Tri Formation \nif chat is enabled shouts the \nresults in chat. \n" +
			"Use TriH or TriL after a trade to pick Tri High or Tri Low in one shout.\n" +
			"------------------------------------\n" +
			":verify \n" +
			"Will shout the previous result in\nchat. Use if you were muted and\ndont know the results of 21/13.\n" +
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
		return
	}

	// Block all manual shouts/chat and route them through the strict queue.
	// This ensures that even manual typing respects the flood-control delay.
	e.Block()
	sendShout(msg)
}

func (a *App) handleIncomingChat(e *g.Intercept) {
	index := e.Packet.ReadInt()
	msg := e.Packet.ReadString()

	chatType := "TALK"
	if e.Is(in.CHAT_2) {
		chatType = "PRIVATE"
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

	// Buffer potential early game-choice shouts if we're not currently awaiting a choice
	if a.chatBuf != nil && !awaitingGameChoice && !awaitingBlackjackDecision && !awaiting13Decision && !awaitingUOChoice && !awaitingTriChoice && !awaitingMHChoice {
		if _, ok := normalizeIncomingGameChoice(msg); ok {
			a.chatBuf.Add(index, msg)
		} else if _, ok := normalizeLooseGameChoice(msg); ok {
			a.chatBuf.Add(index, msg)
		} else if looksLikeUnreadableGameChoiceAttempt(msg) {
			a.chatBuf.Add(index, msg)
		}
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
				stopRiskDecisionTimeoutMonitor()
				go a.finalizeRiskKeep()
				return
			}
			re := regexp.MustCompile(`(?i)^\s*(?:r|risk)\s*?(\d+)\s*$`)
			if m := re.FindStringSubmatch(msg); len(m) == 2 {
				amt, _ := strconv.Atoi(m[1])

				// Prevent duplicate/parallel risk attempts — prefer the first.
				mutex.Lock()
				prompted := riskDecisionTimeoutActive
				pending := awaitingGameChoice || riskPendingBet > 0
				mutex.Unlock()

				if !prompted {
					a.AddLogMsg(fmt.Sprintf("[RISK] ignored r%d from %s: prompt not yet shown", amt, senderName))
					e.Block()
					return
				}

				if pending {
					sendShoutTargeted(index, "Risk already placed; choose a game (or wait for the prompt).")
					a.AddLogMsg(fmt.Sprintf("[RISK] ignored duplicate r%d from %s: already pending", amt, senderName))
					e.Block()
					return
				}

				e.Block()
				go a.handleRiskBet(amt, senderName, index)
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
		stopBlackjackDecisionTimeoutMonitor()
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
		stopThirteenDecisionTimeoutMonitor()
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

	if awaitingSixDecision {
		decision, ok := normalizeBlackjackDecision(msg)
		if !ok {
			a.AddLogMsg(fmt.Sprintf("[6_DEBUG] awaiting decision from %q(index=%d), ignored non-decision message=%q", awaitingSixDecisionPartnerName, awaitingSixDecisionPartnerID, msg))
			return
		}

		indexMatch := awaitingSixDecisionPartnerID > 0 && index == awaitingSixDecisionPartnerID
		nameMatch := awaitingSixDecisionPartnerName != "" && strings.EqualFold(senderName, awaitingSixDecisionPartnerName)
		if !indexMatch && !nameMatch && awaitingSixDecisionPartnerName != "" {
			if expectedIdx, ok := lookupRoomEntityIndexByName(awaitingSixDecisionPartnerName); ok && expectedIdx > 0 && expectedIdx == index {
				indexMatch = true
			}
		}
		if !indexMatch && !nameMatch && awaitingSixDecisionPartnerName != "" {
			if expectedIdx, ok := lookupUsers28RoomIndexByName(awaitingSixDecisionPartnerName); ok && expectedIdx > 0 && expectedIdx == index {
				indexMatch = true
			}
		}

		if !indexMatch && !nameMatch {
			a.AddLogMsg(fmt.Sprintf("[6] ignoring decision %q from %q (index %d); waiting for %q (index %d)", decision, senderName, index, awaitingSixDecisionPartnerName, awaitingSixDecisionPartnerID))
			return
		}

		e.Block()
		awaitingSixDecision = false
		stopSixDecisionTimeoutMonitor()
		a.AddLogMsg(fmt.Sprintf("[6_DEBUG] accepted decision=%q from sender=%q index=%d (expectedName=%q expectedIndex=%d)", decision, senderName, index, awaitingSixDecisionPartnerName, awaitingSixDecisionPartnerID))

		if decision == "hit" {
			a.AddLogMsg("[6] player chose hit")
			isSixHitting = true
			isSixRolling = true
			sixHitInFlight = true
			go a.hitSixDice()
		} else {
			a.AddLogMsg("[6] player chose stay")
			a.startSixDealerTurn("player stayed")
		}
		return
	}

	if awaitingMHChoice {
		cleaned := strings.ToLower(strings.TrimSpace(msg))
		cleaned = gameChoiceCleanupRe.ReplaceAllString(cleaned, "")
		if cleaned != "u10" && cleaned != "o11" && cleaned != "u" && cleaned != "o" {
			a.AddLogMsg(fmt.Sprintf("[MH_DEBUG] awaiting MH choice from %q(index=%d), ignored non-choice message=%q", awaitingMHChoicePartnerName, awaitingMHChoicePartnerID, msg))
		} else {
			indexMatch := awaitingMHChoicePartnerID > 0 && index == awaitingMHChoicePartnerID
			nameMatch := awaitingMHChoicePartnerName != "" && strings.EqualFold(senderName, awaitingMHChoicePartnerName)
			if !indexMatch && !nameMatch && awaitingMHChoicePartnerName != "" {
				if expectedIdx, ok := lookupRoomEntityIndexByName(awaitingMHChoicePartnerName); ok && expectedIdx > 0 && expectedIdx == index {
					indexMatch = true
				}
			}
			if !indexMatch && !nameMatch && awaitingMHChoicePartnerName != "" {
				if expectedIdx, ok := lookupUsers28RoomIndexByName(awaitingMHChoicePartnerName); ok && expectedIdx > 0 && expectedIdx == index {
					indexMatch = true
				}
			}

			if !indexMatch && !nameMatch {
				a.AddLogMsg(fmt.Sprintf("[MH] ignoring choice %q from %q (index %d); waiting for %q (index %d)", cleaned, senderName, index, awaitingMHChoicePartnerName, awaitingMHChoicePartnerID))
			} else {
				e.Block()
				if cleaned == "u" {
					cleaned = "u10"
				} else if cleaned == "o" {
					cleaned = "o11"
				}
				awaitingMHChoice = false
				a.AddLogMsg(fmt.Sprintf("[MH_DEBUG] accepted choice=%q from sender=%q index=%d", cleaned, senderName, index))

				if riskSessionActive {
					a.beginRiskRoundHistory(cleaned, msg, "MidHouse")
				} else {
					a.setCurrentGameHistoryChoice(cleaned, msg)
					a.setCurrentGameHistoryGame("MidHouse")
				}

				ack := fmt.Sprintf("%s! Starting, Player Roll", gameChoiceDisplay("mh_"+cleaned))
				a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] shouting: %q", ack))
				sendShout(ack)

				a.beginMidHouseRound(cleaned)
				return
			}
		}
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
						parts = append(parts, fmt.Sprintf("%s %d/%d", a.formatTradeItemName(key), coverable, incomingMap[key]))
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
			SafeGo(func() {
				time.Sleep(1400 * time.Millisecond)
				a.executeRiskRound()
			})
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
		// allow single-letter shortcuts 'h' and 'l' for high/low
		if cleaned == "h" {
			cleaned = "high"
		} else if cleaned == "l" {
			cleaned = "low"
		}
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
	a.AddLogMsg(fmt.Sprintf("[GAME_SELECT_DEBUG] processing message from %q: %q (normalized: %q ok: %t)", senderName, msg, choice, ok))
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
						parts = append(parts, fmt.Sprintf("%s %d/%d", a.formatTradeItemName(key), coverable, incomingMap[key]))
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
				SafeGo(func() {
					time.Sleep(1400 * time.Millisecond)
					a.executeRiskRound()
				})
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
		}
		return
	}

	// Standalone UO modes: while waiting for initial game choice, only allow
	// U/O/7-related choices. Ignore other game selections (e.g. 13, 21, pkr, tri).
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
			sendShoutTargeted(index, "That game is disabled. "+prompt)
			return
		}
	}

	e.Block()
	if (dealerGameActive() && !awaitingGameChoice) || isClosing {
		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] %s selected but dice are busy (gameActive:true closing:%t)", choice, isClosing))
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
		// Build a context-aware ack: special-case DT for clearer prompts
		var ack string
		switch choice {
		case "pairup":
			ack = "PU! If you roll a double or triple you Win! Player Roll"
		case "h18":
			ack = "H18! 19+ Win / 17- Lose / 18 House! Player Roll"
		case "6":
			ack = "6! Starting, Player Roll"
		case "dt":
			ack = fmt.Sprintf("%s! Player Roll — Double Trouble: land on even to win", gameChoiceDisplay(choice))
		default:
			ack = fmt.Sprintf("%s! Starting, Player Roll", gameChoiceDisplay(choice))
		}

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
	case "6":
		// Start the 6-game sequence
		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] %d selected 6; starting 6 sequence", index))
		a.beginSixSequence()
	case "pairup":
		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] %d selected PU; starting round", index))
		a.beginPairUpRound()
	case "h18":
		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] %d selected H18; starting round", index))
		a.beginH18Round()
	case "dt":
		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] %d selected DT; starting round", index))
		a.beginDoubleTroubleRound()
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
					parts = append(parts, fmt.Sprintf("%s %d/%d", a.formatTradeItemName(key), coverable, incomingMap[key]))
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
			SafeGo(func() {
				time.Sleep(1400 * time.Millisecond)
				a.executeRiskRound()
			})
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
			SafeGo(func() {
				time.Sleep(1400 * time.Millisecond)
				a.executeRiskRound()
			})
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
	case "mh":
		// Two-step MidHouse selection: prompt player for u10 or o11
		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] %d selected MidHouse; prompting for u10/o11", index))
		a.beginMidHouseChoiceSequence()
	case "mh_u10":
		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] %d selected u10; starting MidHouse round", index))
		a.beginMidHouseRound("u10")
	case "mh_o11":
		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] %d selected o11; starting MidHouse round", index))
		a.beginMidHouseRound("o11")
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
	case "6", "six":
		return "6", true
	case "tri":
		return "tri", true
	case "uo", "underover":
		return "uo", true
	case "uo7", "underover7":
		return "uo7", true
	case "u7":
		return "uo_under", true
	case "o7":
		return "uo_over", true
	case "pu", "pu3", "pairup", "pair":
		return "pairup", true
	case "trih":
		return "trihigh", true
	case "trihigh":
		return "trihigh", true
	case "tril":
		return "trilow", true
	case "trilow":
		return "trilow", true
	case "dt", "double", "doubletrouble":
		return "dt", true
	case "h18", "highfive18", "hf18":
		return "h18", true
	case "mh", "midhouse", "1011":
		return "mh", true
	case "u10":
		return "mh_u10", true
	case "o11":
		return "mh_o11", true
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
	case "6", "six":
		return "6", true
	case "tri":
		return "tri", true
	case "uo", "underover":
		return "uo", true
	case "uo7", "underover7":
		return "uo7", true
	case "u7":
		return "uo_under", true
	case "o7":
		return "uo_over", true
	case "pu", "pu3", "pairup", "pair":
		return "pairup", true
	case "trih":
		return "trihigh", true
	case "trihigh":
		return "trihigh", true
	case "tril":
		return "trilow", true
	case "trilow":
		return "trilow", true
	case "dt", "double", "doubletrouble":
		return "dt", true
	case "h18", "highfive18", "hf18":
		return "h18", true
	case "mh", "midhouse", "1011":
		return "mh", true
	case "u10":
		return "mh_u10", true
	case "o11":
		return "mh_o11", true
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

	// Detect broken private-like fragments such as p..k..r or t..i
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
	case "6":
		return "6"
	case "tri":
		return "Tri"
	case "dt":
		return "DT"
	case "uo7":
		return "UO7"
	case "uo_over":
		return "o7 (8-12)"
	case "uo_under":
		return "u7 (2-6)"
	case "uo":
		return "UO"
	case "pairup":
		return "PU"
	case "trih":
		return "TriH"
	case "trihigh":
		return "TriH"
	case "tril":
		return "TriL"
	case "trilow":
		return "TriL"
	case "h18":
		return "H18"
	case "mh":
		return "MidHouse"
	case "mh_u10":
		return "u10 roll between (3-9)"
	case "mh_o11":
		return "o11 roll between (12-18)"
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

func (a *App) beginMidHouseChoiceSequence() {
	playerName := strings.TrimSpace(lastTradePartnerName)
	if playerName == "" {
		playerName = "Player"
	}

	resetPokerSequence()
	resetBlackjackSequence()
	reset13Sequence()
	resetTriSequence()
	resetMidHouseSequence()

	awaitingMHChoice = true
	awaitingMHChoicePartnerName = playerName

	if chatIdx, ok := lookupRoomEntityIndexByName(playerName); ok && chatIdx > 0 {
		awaitingMHChoicePartnerID = chatIdx
	} else if chatIdx, ok := waitForUsers28RoomIndexByName(playerName, 900*time.Millisecond); ok && chatIdx > 0 {
		awaitingMHChoicePartnerID = chatIdx
	} else if chatIdx, ok := lookupUsers28RoomIndexByName(playerName); ok && chatIdx > 0 {
		awaitingMHChoicePartnerID = chatIdx
	} else {
		awaitingMHChoicePartnerID = lastTradePartnerID
	}

	msg := "u10 (3-9) or o11 (12-18)? gl"
	a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] shouting: %q", msg))
	sendShout(msg)
}

func (a *App) beginMidHouseRound(choice string) {
	playerName := strings.TrimSpace(lastTradePartnerName)
	if playerName == "" {
		playerName = "Player"
	}

	resetPokerSequence()
	resetBlackjackSequence()
	reset13Sequence()
	resetTriSequence()
	resetMidHouseSequence()

	midHouseRoundActive = true
	midHouseChoice = choice
	a.setCurrentGameHistoryGame("MidHouse")

	SafeGo(func() {
		time.Sleep(1400 * time.Millisecond)
		isMidHouseRolling = true
		a.rollMidHouseDice()
	})
}

func (a *App) rollMidHouseDice() {
	if fakeDiceTestingMode {
		mutex.Lock()
		indices := []int{0, 1, 2}
		if len(diceList) >= 5 {
			indices = []int{0, 2, 4}
		}
		if len(diceList) < len(indices) {
			mutex.Unlock()
			log.Println("Not enough dice to roll")
			isMidHouseRolling = false
			return
		}
		for _, idx := range indices {
			diceList[idx].Value = rand.Intn(6) + 1
			diceList[idx].IsClosed = false
			logRollResult := fmt.Sprintf("Dice %d rolled: %d\n", diceList[idx].ID, diceList[idx].Value)
			a.AddLogMsg(logRollResult)
		}
		mutex.Unlock()
		a.evaluateMidHouseRound()
		isMidHouseRolling = false
		return
	}

	mutex.Lock()
	indices := []int{0, 1, 2}
	if len(diceList) >= 5 {
		indices = []int{0, 2, 4}
	}
	if len(diceList) < len(indices) {
		mutex.Unlock()
		log.Println("Not enough dice to roll")
		isMidHouseRolling = false
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

	a.evaluateMidHouseRound()
	isMidHouseRolling = false
}

func (a *App) startDealerShoutPolling() {
	a.AddLogMsg("[SHOUT_POLL] worker started and entering loop")
	SafeGo(func() {
		// Wait a bit for DB to stabilize on startup
		time.Sleep(5 * time.Second)
		
		lastHeartbeat := time.Now()
		a.AddLogMsg("[SHOUT_POLL] worker loop running")
		for {
			time.Sleep(1 * time.Second) // Poll every 1 second

			if time.Since(lastHeartbeat) > 30*time.Second {
				a.AddLogMsg("[SHOUT_POLL] heartbeat: poller is active and waiting for shouts")
				lastHeartbeat = time.Now()
			}

			db, owner := a.getHistoryDB()
			if db == nil {
				if time.Since(lastHeartbeat) > 25*time.Second {
					a.AddLogMsg("[SHOUT_POLL] heartbeat: still waiting for database connection...")
					// don't reset lastHeartbeat here so it triggers frequently if disconnected
				}
				continue
			}

			// Query for the oldest pending shout for this dealer
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			var id int
			var targetPlayer, message, dbOwner string
			
			// Simplified query: Ignore owner_key entirely and pick up any pending shout
			query := `
				SELECT id, target_player, message, owner_key
				FROM public.dealer_shouts 
				WHERE status = 'pending'
				ORDER BY created_at ASC 
				LIMIT 1
			`
			err := db.QueryRow(ctx, query).Scan(&id, &targetPlayer, &message, &dbOwner)
			cancel()

			if err != nil {
				// No pending shouts found for our filters
				if err != pgx.ErrNoRows && !strings.Contains(err.Error(), "no rows") {
					a.AddLogMsg(fmt.Sprintf("[SHOUT_POLL] query error: %v", err))
				}
				continue
			}

			a.AddLogMsg(fmt.Sprintf("[SHOUT_POLL] Picking up shout ID %d for %s (DB Owner: %q, Local Owner: %q): %s", id, targetPlayer, dbOwner, owner, message))

			// Mark as 'processing' to avoid duplicate shouts from same poller
			ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
			_, err = db.Exec(ctx2, "UPDATE public.dealer_shouts SET status = 'processing' WHERE id = $1", id)
			cancel2()
			if err != nil {
				a.AddLogMsg(fmt.Sprintf("[SHOUT_POLL] ERROR: failed to mark shout %d as processing: %v", id, err))
				continue
			}

			// Execute the shout!
			sendShout(message)

			// Mark as completed
			ctx3, cancel3 := context.WithTimeout(context.Background(), 2*time.Second)
			_, err = db.Exec(ctx3, "UPDATE public.dealer_shouts SET status = 'completed', completed_at = NOW() WHERE id = $1", id)
			cancel3()
			if err != nil {
				a.AddLogMsg(fmt.Sprintf("[SHOUT_POLL] ERROR: failed to mark shout %d as completed: %v", id, err))
			} else {
				a.AddLogMsg(fmt.Sprintf("[SHOUT_POLL] Successfully shouted and completed ID %d", id))
			}
		}
	})
}

func resetMidHouseSequence() {
	isMidHouseRolling = false
	midHouseRoundActive = false
	awaitingMHChoice = false
	awaitingMHChoicePartnerID = 0
	awaitingMHChoicePartnerName = ""
}
