package main

import (
	"fmt"
	"log"
	"math/rand"
	"strconv"
	"strings"
	"time"
)

// Send message with a delay to simulate user typing/waiting
func sendMessageWithDelay(message string) {
	// Immediately enqueue the message; the shout worker will apply spacing.
	// Ensure we have an up-to-date frozen hand snapshot before posting the
	// canonical "dealer open" announcement. This avoids announcing that we're
	// open before inventory is ready for coverage checks.
	if app != nil && strings.TrimSpace(message) == app.dealerOpenMessage() {
		// Only enforce when strict snapshot lifecycle is active and a snapshot
		// is not already ready. Avoids re-syncing on every periodic repost.
		if strictTradeSnapshotLifecycle && !dealerSnapshotReady() {
			app.AddLogMsg("[TRADE_HAND_SNAPSHOT] pre-dealer-open sync requested (sendMessageWithDelay)")
			ok := app.forceRefreshHandSnapshot("sendMessageWithDelay: dealer open")
			if !ok {
				app.AddLogMsg("[TRADE_HAND_SNAPSHOT] forced refresh failed; skipping dealer open shout")
				return
			}
		}
	}

	// Use mute-aware sendShout so messages respect mute/queueing and rate limits.
	sendShout(message)
	log.Printf("Queued/shouted message: %s", message)
}

// Wait for all dice results and evaluate the poker hand
func (a *App) evaluatePokerHand() {
	if pokerSequenceStage == 0 {
		a.AddLogMsg("[POKER_GUARD] evaluatePokerHand called but pokerSequenceStage is 0; skipping logic")
		return
	}

	result := evaluatePokerRules(diceList)
	hand := a.toPokerString(diceList)
	logRollResult := fmt.Sprintf("Poker Result: %s\n", hand)
	time.Sleep(time.Duration(rand.Intn(250)+250) * time.Millisecond)
	a.AddLogMsg(logRollResult)

	if !ChatIsDisabled {
		if pokerSequenceStage == 1 {
			// Send the player's result and immediately indicate dealer will roll
			sendMessageWithDelay(fmt.Sprintf("%s | Rolling...", hand))
		} else if pokerSequenceStage == 2 {
			// Suppress separate dealer-hand announcement; final winner message will include dealer result
		} else {
			// Fallback: send the raw hand text
			sendMessageWithDelay(hand)
		}
	}

	if pokerSequenceStage == 1 {
		pokerSequencePlayerResult = result
		pokerSequencePlayerHand = hand
		a.AddLogMsg(fmt.Sprintf("[POKER_DEBUG] stage 1 complete: player_cat=%d player_hand=%q", result.Category, hand))
		a.setCurrentGameHistoryResults(hand, "", "", "In Progress", false)
		a.noteCurrentGameHistory("Player poker hand recorded")
		pokerSequenceStage = 2
		go func() {
			time.Sleep(700 * time.Millisecond)
			// Start dealer roll without an extra shout (we already indicated it)
			a.AddLogMsg("[GAME_SELECT] starting dealer roll")
			time.Sleep(700 * time.Millisecond)
			a.startPokerRoll()
		}()
	} else if pokerSequenceStage == 2 {
		a.AddLogMsg(fmt.Sprintf("[POKER_DEBUG] stage 2 start: player_cat=%d player_hand=%q dealer_cat=%d dealer_hand=%q", 
			pokerSequencePlayerResult.Category, pokerSequencePlayerHand, result.Category, hand))
		
		winner := comparePokerHands(pokerSequencePlayerResult, result)
		playerName := strings.TrimSpace(pokerSequencePlayerName)
		if playerName == "" {
			playerName = "Player"
		}
		winnerName := "Dealer"
		if winner == PokerWinnerPlayer {
			winnerName = playerName
		}
		
		a.AddLogMsg(fmt.Sprintf("[POKER_DEBUG] winner determined: %d (%s)", winner, winnerName))

		winnerMsg := fmt.Sprintf("%s Wins - %s: %s | Dealer: %s", winnerName, playerName, pokerSequencePlayerHand, hand)

		a.AddLogMsg(fmt.Sprintf("[POKER_RULES] player_cat=%d player_tie=%v dealer_cat=%d dealer_tie=%v winner=%s", 
			pokerSequencePlayerResult.Category, pokerSequencePlayerResult.Tiebreaks, 
			result.Category, result.Tiebreaks, 
			winnerMsg))
		log.Printf("[POKER_RULES] player_cat=%d player_tie=%v dealer_cat=%d dealer_tie=%v winner=%s", 
			pokerSequencePlayerResult.Category, pokerSequencePlayerResult.Tiebreaks, 
			result.Category, result.Tiebreaks, 
			winnerMsg)

		a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] shouting: %q", winnerMsg))
		log.Printf("[GAME_SELECT] shouting: %q", winnerMsg)
		if !ChatIsDisabled {
			waitForUnmute(90 * time.Second)
			time.Sleep(800 * time.Millisecond)
			sendMessageWithDelay(winnerMsg)
		}

		payoutTargetID := lastTradePartnerID
		payoutTargetName := playerName
		playerHand := pokerSequencePlayerHand
		resetPokerSequence()
		isPokerRolling = false

		if winner == PokerWinnerPlayer && payoutTargetID > 0 {
			a.setCurrentGameHistoryResults(playerHand, hand, playerName, "Payout Pending", false)
			a.noteCurrentGameHistory(winnerMsg)
			a.AddLogMsg(fmt.Sprintf("[PAYOUT] player won, initiating payout trade to %s (%d)", payoutTargetName, payoutTargetID))
			log.Printf("[PAYOUT] player won, initiating payout trade to %s (%d)", payoutTargetName, payoutTargetID)
			resetPayoutRetryState()

			// Post the round outcome immediately so Discord shows who won this roll.
			a.sendDiscordRoundResult(playerName, playerHand, hand, winnerMsg)

			if isRiskEnabled {
				if riskSessionActive {
					go a.applyRiskOutcome(true)
					return
				}
				go a.handlePlayerWinRisk(cloneTradeItems(gameBetItems), payoutTargetName, payoutTargetID, "Pkr", nil)
				return
			}
			startPayout(a, payoutTargetID, payoutTargetName)
			return
		} else {
			a.setCurrentGameHistoryResults(playerHand, hand, a.getCurrentDealerName(), "Completed", true)
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
	}

	isPokerRolling = false
}

func (a *App) evaluateBlackjackHand() {
	defer func() {
		if r := recover(); r != nil {
			a.AddLogMsg(fmt.Sprintf("[BJ_CRASH_GUARD] recovered panic in evaluateBlackjackHand: %v", r))
			log.Printf("[BJ_CRASH_GUARD] recovered panic in evaluateBlackjackHand: %v", r)
			resetBlackjackSequence()
			isBJRolling = false
			isHitting = false
		}
	}()

	mutex.Lock()
	mutex.Unlock()
	if !blackjackRoundActive {
		isBJRolling = false
		isHitting = false
		return
	}

	log.Printf("[BJ] evaluating sum=%d playerTurn=%t", currentSum, blackjackPlayerTurn)
	a.AddLogMsg(fmt.Sprintf("[BJ_DEBUG] evaluate start roundActive=%t playerTurn=%t sum=%d hitInFlight=%t awaitingDecision=%t", blackjackRoundActive, blackjackPlayerTurn, currentSum, blackjackHitInFlight, awaitingBlackjackDecision))
	log.Printf("[BJ_DEBUG] evaluate start roundActive=%t playerTurn=%t sum=%d hitInFlight=%t awaitingDecision=%t", blackjackRoundActive, blackjackPlayerTurn, currentSum, blackjackHitInFlight, awaitingBlackjackDecision)

	if blackjackPlayerTurn {
		playerLabel := strings.TrimSpace(blackjackPlayerName)
		if playerLabel == "" {
			playerLabel = strings.TrimSpace(lastTradePartnerName)
		}
		if playerLabel == "" {
			playerLabel = "Player"
		}

		blackjackPlayerTotal = currentSum
		a.AddLogMsg(fmt.Sprintf("[BJ] player total now %d", blackjackPlayerTotal))
		log.Printf("[BJ] player total now %d", blackjackPlayerTotal)

		if blackjackPlayerTotal > 21 {
			a.finalizeBlackjackRound(false, "player bust")
			isBJRolling = false
			isHitting = false
			return
		}

		// auto-announce and proceed when player reaches 21
		if blackjackPlayerTotal == 21 {
			playerLabel := strings.TrimSpace(blackjackPlayerName)
			if playerLabel == "" {
				playerLabel = strings.TrimSpace(lastTradePartnerName)
			}
			if playerLabel == "" {
				playerLabel = "Player"
			}

			// Announce player result and indicate dealer roll in one message
			msg := fmt.Sprintf("%s got 21 | Rolling...", playerLabel)
			a.AddLogMsg(fmt.Sprintf("[BJ] announcing: %q", msg))
			log.Printf("[BJ] announcing: %q", msg)
			if !ChatIsDisabled {
				waitForUnmute(90 * time.Second)
				time.Sleep(800 * time.Millisecond)
				sendMessageWithDelay(msg)
			}

			a.startBlackjackDealerTurn("player reached 21")
			isBJRolling = false
			isHitting = false
			return
		}
		if blackjackPlayerTotal <= 15 {
			a.AddLogMsg(fmt.Sprintf("[BJ] player total %d <= 15; auto-hit", blackjackPlayerTotal))
			log.Printf("[BJ] player total %d <= 15; auto-hit", blackjackPlayerTotal)
			if blackjackHitInFlight {
				a.AddLogMsg("[BJ] player auto-hit deferred: hit already in flight")
				log.Printf("[BJ] player auto-hit deferred: hit already in flight")
				go func() {
					time.Sleep(150 * time.Millisecond)
					a.evaluateBlackjackHand()
				}()
				return
			}
			blackjackHitInFlight = true
			isHitting = true
			go a.hitBjDice()
			return
		}

		// only prompt once - do not spam chat
		if awaitingBlackjackDecision {
			isBJRolling = false
			isHitting = false
			return
		}

		awaitingBlackjackDecision = true
		awaitingBlackjackDecisionPartnerName = strings.TrimSpace(blackjackPlayerName)
		if awaitingBlackjackDecisionPartnerName == "" {
			awaitingBlackjackDecisionPartnerName = strings.TrimSpace(lastTradePartnerName)
		}
		if awaitingBlackjackDecisionPartnerName == "" {
			awaitingBlackjackDecisionPartnerName = "Player"
		}
		if chatIdx, ok := lookupRoomEntityIndexByName(awaitingBlackjackDecisionPartnerName); ok && chatIdx > 0 {
			awaitingBlackjackDecisionPartnerID = chatIdx
			a.AddLogMsg(fmt.Sprintf("[BJ_DEBUG] prompt target resolved via room index=%d name=%q", awaitingBlackjackDecisionPartnerID, awaitingBlackjackDecisionPartnerName))
			log.Printf("[BJ_DEBUG] prompt target resolved via room index=%d name=%q", awaitingBlackjackDecisionPartnerID, awaitingBlackjackDecisionPartnerName)
		} else if chatIdx, ok := lookupUsers28RoomIndexByName(awaitingBlackjackDecisionPartnerName); ok && chatIdx > 0 {
			awaitingBlackjackDecisionPartnerID = chatIdx
			a.AddLogMsg(fmt.Sprintf("[BJ_DEBUG] prompt target resolved via users28 roomIndex=%d name=%q", awaitingBlackjackDecisionPartnerID, awaitingBlackjackDecisionPartnerName))
			log.Printf("[BJ_DEBUG] prompt target resolved via users28 roomIndex=%d name=%q", awaitingBlackjackDecisionPartnerID, awaitingBlackjackDecisionPartnerName)
		} else {
			awaitingBlackjackDecisionPartnerID = lastTradePartnerID
			a.AddLogMsg(fmt.Sprintf("[BJ_DEBUG] prompt target fallback index=%d name=%q", awaitingBlackjackDecisionPartnerID, awaitingBlackjackDecisionPartnerName))
			log.Printf("[BJ_DEBUG] prompt target fallback index=%d name=%q", awaitingBlackjackDecisionPartnerID, awaitingBlackjackDecisionPartnerName)
		}

		prompt := fmt.Sprintf("%s total %d. Hit or stay?", playerLabel, blackjackPlayerTotal)
		a.AddLogMsg(fmt.Sprintf("[BJ] prompting decision: %q", prompt))
		log.Printf("[BJ] prompting decision: %q", prompt)
		if !ChatIsDisabled && !isMuted {
			a.AddLogMsg("[BJ_DEBUG] sending hit/stay prompt to chat")
			log.Printf("[BJ_DEBUG] sending hit/stay prompt to chat")
			sendMessageWithDelay(prompt)
			a.startBlackjackDecisionTimeoutMonitor(awaitingBlackjackDecisionPartnerName)
		} else {
			a.AddLogMsg("[BJ] prompt not sent (chat disabled or muted); auto-staying")
			log.Printf("[BJ] prompt not sent (chat disabled or muted); auto-staying")
			awaitingBlackjackDecision = false
			a.startBlackjackDealerTurn("prompt unavailable auto-stay")
			isBJRolling = false
			isHitting = false
			return
		}

		isBJRolling = false
		isHitting = false
		return
	}

	blackjackDealerTotal = currentSum
	a.AddLogMsg(fmt.Sprintf("[BJ] dealer total now %d (player=%d)", blackjackDealerTotal, blackjackPlayerTotal))
	log.Printf("[BJ] dealer total now %d (player=%d)", blackjackDealerTotal, blackjackPlayerTotal)

	if blackjackDealerTotal > 21 {
		a.finalizeBlackjackRound(true, "dealer bust")
		isBJRolling = false
		isHitting = false
		return
	}

	if blackjackDealerTotal < blackjackPlayerTotal {
		a.AddLogMsg(fmt.Sprintf("[BJ] dealer total %d < player %d; dealer hits", blackjackDealerTotal, blackjackPlayerTotal))
		log.Printf("[BJ] dealer total %d < player %d; dealer hits", blackjackDealerTotal, blackjackPlayerTotal)
		if blackjackHitInFlight {
			a.AddLogMsg("[BJ] dealer hit deferred: hit already in flight")
			log.Printf("[BJ] dealer hit deferred: hit already in flight")
			go func() {
				time.Sleep(150 * time.Millisecond)
				a.evaluateBlackjackHand()
			}()
			return
		}
		blackjackHitInFlight = true
		isHitting = true
		go a.hitBjDice()
		return
	}

	if blackjackDealerTotal == blackjackPlayerTotal {
		a.AddLogMsg("[BJ_RULES] tie detected; replaying 21 round")
		if !ChatIsDisabled {
			waitForUnmute(90 * time.Second)
			time.Sleep(800 * time.Millisecond)
			sendMessageWithDelay("Tie — replaying 21 round")
		}
		go func() {
			time.Sleep(700 * time.Millisecond)
			a.beginBlackjackSequence()
		}()
		isBJRolling = false
		isHitting = false
		return
	}
	a.finalizeBlackjackRound(false, "dealer beat-or-tie")
	isBJRolling = false
	isHitting = false
}

func (a *App) startBlackjackDealerTurn(reason string) {
	awaitingBlackjackDecision = false
	blackjackPlayerTurn = false
	a.AddLogMsg(fmt.Sprintf("[BJ_DEBUG] dealer turn starting reason=%s playerTotal=%d dealerTotal=%d", reason, blackjackPlayerTotal, blackjackDealerTotal))
	log.Printf("[BJ_DEBUG] dealer turn starting reason=%s playerTotal=%d dealerTotal=%d", reason, blackjackPlayerTotal, blackjackDealerTotal)
	a.AddLogMsg("[GAME_SELECT] starting dealer roll")
	go func() {
		time.Sleep(700 * time.Millisecond)
		isBJRolling = true
		a.rollBjDice()
	}()
}

func (a *App) finalizeBlackjackRound(playerWins bool, reason string) {
	playerName := strings.TrimSpace(blackjackPlayerName)
	if playerName == "" {
		playerName = strings.TrimSpace(lastTradePartnerName)
	}
	if playerName == "" {
		playerName = "Player"
	}

	playerHand := strconv.Itoa(blackjackPlayerTotal)
	dealerHand := strconv.Itoa(blackjackDealerTotal)
	winnerName := "Dealer"
	if playerWins {
		winnerName = playerName
	}
	winnerMsg := fmt.Sprintf("%s Wins - %s: %s | Dealer: %s", winnerName, playerName, playerHand, dealerHand)

	a.AddLogMsg(fmt.Sprintf("[BJ_RULES] winner=%s reason=%s player=%d dealer=%d", winnerName, reason, blackjackPlayerTotal, blackjackDealerTotal))
	log.Printf("[BJ_RULES] winner=%s reason=%s player=%d dealer=%d", winnerName, reason, blackjackPlayerTotal, blackjackDealerTotal)
	a.AddLogMsg(fmt.Sprintf("[GAME_SELECT] shouting: %q", winnerMsg))
	log.Printf("[GAME_SELECT] shouting: %q", winnerMsg)
	if !ChatIsDisabled {
		waitForUnmute(90 * time.Second)
		time.Sleep(800 * time.Millisecond)
		sendMessageWithDelay(winnerMsg)
	}

	payoutTargetID := lastTradePartnerID
	payoutTargetName := playerName
	isBJRolling = false
	isHitting = false
	resetBlackjackSequence()

	if playerWins && payoutTargetID > 0 {
		a.setCurrentGameHistoryResults(playerHand, dealerHand, playerName, "Payout Pending", false)
		a.noteCurrentGameHistory(winnerMsg)
		a.AddLogMsg(fmt.Sprintf("[PAYOUT] 21 player won, initiating payout trade to %s (%d)", payoutTargetName, payoutTargetID))
		log.Printf("[PAYOUT] 21 player won, initiating payout trade to %s (%d)", payoutTargetName, payoutTargetID)
		resetPayoutRetryState()

		// Post the round outcome immediately so Discord shows who won this roll.
		a.sendDiscordRoundResult(playerName, playerHand, dealerHand, winnerMsg)

		if isRiskEnabled {
			if riskSessionActive {
				go a.applyRiskOutcome(true)
				return
			}
			go a.handlePlayerWinRisk(cloneTradeItems(gameBetItems), payoutTargetName, payoutTargetID, "21", nil)
			return
		}
		startPayout(a, payoutTargetID, payoutTargetName)
		return
	}

	a.setCurrentGameHistoryResults(playerHand, dealerHand, a.getCurrentDealerName(), "Completed", true)
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

func (a *App) evaluate13Hand() {
	defer func() {
		if r := recover(); r != nil {
			a.AddLogMsg(fmt.Sprintf("[13_CRASH_GUARD] recovered panic in evaluate13Hand: %v", r))
			log.Printf("[13_CRASH_GUARD] recovered panic in evaluate13Hand: %v", r)
			reset13Sequence()
			is13Rolling = false
			is13Hitting = false
		}
	}()

	mutex.Lock()
	mutex.Unlock()
	if !thirteenRoundActive {
		is13Rolling = false
		is13Hitting = false
		return
	}

	a.AddLogMsg(fmt.Sprintf("[13] evaluating sum=%d playerTurn=%t", currentSum, thirteenPlayerTurn))
	log.Printf("[13] evaluating sum=%d playerTurn=%t", currentSum, thirteenPlayerTurn)
	a.AddLogMsg(fmt.Sprintf("[13_DEBUG] evaluate start roundActive=%t playerTurn=%t sum=%d hitInFlight=%t awaitingDecision=%t", thirteenRoundActive, thirteenPlayerTurn, currentSum, thirteenHitInFlight, awaiting13Decision))
	log.Printf("[13_DEBUG] evaluate start roundActive=%t playerTurn=%t sum=%d hitInFlight=%t awaitingDecision=%t", thirteenRoundActive, thirteenPlayerTurn, currentSum, thirteenHitInFlight, awaiting13Decision)

	if thirteenPlayerTurn {
		playerLabel := strings.TrimSpace(thirteenPlayerName)
		if playerLabel == "" {
			playerLabel = strings.TrimSpace(lastTradePartnerName)
		}
		if playerLabel == "" {
			playerLabel = "Player"
		}

		thirteenPlayerTotal = currentSum
		a.AddLogMsg(fmt.Sprintf("[13] player total now %d", thirteenPlayerTotal))
		log.Printf("[13] player total now %d", thirteenPlayerTotal)

		if thirteenPlayerTotal > 13 {
			a.finalize13Round(false, "player bust")
			is13Rolling = false
			is13Hitting = false
			return
		}

		if thirteenPlayerTotal == 13 {
			a.start13DealerTurn("player reached 13")
			is13Rolling = false
			is13Hitting = false
			return
		}

		if thirteenPlayerTotal <= 7 {
			a.AddLogMsg(fmt.Sprintf("[13] player total %d <= 7; auto-hit", thirteenPlayerTotal))
			log.Printf("[13] player total %d <= 7; auto-hit", thirteenPlayerTotal)
			if thirteenHitInFlight {
				a.AddLogMsg("[13] player auto-hit deferred: hit already in flight")
				log.Printf("[13] player auto-hit deferred: hit already in flight")
				go func() {
					time.Sleep(150 * time.Millisecond)
					a.evaluate13Hand()
				}()
				return
			}
			thirteenHitInFlight = true
			is13Hitting = true
			go a.hit13Dice()
			return
		}

		// only prompt once - do not spam chat
		if awaiting13Decision {
			is13Rolling = false
			is13Hitting = false
			return
		}

		awaiting13Decision = true
		awaiting13DecisionPartnerName = strings.TrimSpace(thirteenPlayerName)
		if awaiting13DecisionPartnerName == "" {
			awaiting13DecisionPartnerName = strings.TrimSpace(lastTradePartnerName)
		}
		if awaiting13DecisionPartnerName == "" {
			awaiting13DecisionPartnerName = "Player"
		}
		if chatIdx, ok := lookupRoomEntityIndexByName(awaiting13DecisionPartnerName); ok && chatIdx > 0 {
			awaiting13DecisionPartnerID = chatIdx
			a.AddLogMsg(fmt.Sprintf("[13_DEBUG] prompt target resolved via room index=%d name=%q", awaiting13DecisionPartnerID, awaiting13DecisionPartnerName))
			log.Printf("[13_DEBUG] prompt target resolved via room index=%d name=%q", awaiting13DecisionPartnerID, awaiting13DecisionPartnerName)
		} else if chatIdx, ok := lookupUsers28RoomIndexByName(awaiting13DecisionPartnerName); ok && chatIdx > 0 {
			awaiting13DecisionPartnerID = chatIdx
			a.AddLogMsg(fmt.Sprintf("[13_DEBUG] prompt target resolved via users28 roomIndex=%d name=%q", awaiting13DecisionPartnerID, awaiting13DecisionPartnerName))
			log.Printf("[13_DEBUG] prompt target resolved via users28 roomIndex=%d name=%q", awaiting13DecisionPartnerID, awaiting13DecisionPartnerName)
		} else {
			awaiting13DecisionPartnerID = lastTradePartnerID
			a.AddLogMsg(fmt.Sprintf("[13_DEBUG] prompt target fallback index=%d name=%q", awaiting13DecisionPartnerID, awaiting13DecisionPartnerName))
			log.Printf("[13_DEBUG] prompt target fallback index=%d name=%q", awaiting13DecisionPartnerID, awaiting13DecisionPartnerName)
		}

		prompt := fmt.Sprintf("%s total %d. Hit or stay?", playerLabel, thirteenPlayerTotal)
		a.AddLogMsg(fmt.Sprintf("[13] prompting decision: %q", prompt))
		log.Printf("[13] prompting decision: %q", prompt)
		if !ChatIsDisabled && !isMuted {
			a.AddLogMsg("[13_DEBUG] sending hit/stay prompt to chat")
			log.Printf("[13_DEBUG] sending hit/stay prompt to chat")
			sendMessageWithDelay(prompt)
			a.startThirteenDecisionTimeoutMonitor(awaiting13DecisionPartnerName)
		} else {
			a.AddLogMsg("[13] prompt not sent (chat disabled or muted); auto-staying")
			log.Printf("[13] prompt not sent (chat disabled or muted); auto-staying")
			awaiting13Decision = false
			a.start13DealerTurn("prompt unavailable auto-stay")
			is13Rolling = false
			is13Hitting = false
			return
		}

		is13Rolling = false
		is13Hitting = false
		return
	}

	thirteenDealerTotal = currentSum
	a.AddLogMsg(fmt.Sprintf("[13] dealer total now %d (player=%d)", thirteenDealerTotal, thirteenPlayerTotal))
	log.Printf("[13] dealer total now %d (player=%d)", thirteenDealerTotal, thirteenPlayerTotal)

	if thirteenDealerTotal > 13 {
		a.finalize13Round(true, "dealer bust")
		is13Rolling = false
		is13Hitting = false
		return
	}

	if thirteenDealerTotal < thirteenPlayerTotal {
		a.AddLogMsg(fmt.Sprintf("[13] dealer total %d < player %d; dealer hits", thirteenDealerTotal, thirteenPlayerTotal))
		log.Printf("[13] dealer total %d < player %d; dealer hits", thirteenDealerTotal, thirteenPlayerTotal)
		if thirteenHitInFlight {
			a.AddLogMsg("[13] dealer hit deferred: hit already in flight")
			log.Printf("[13] dealer hit deferred: hit already in flight")
			go func() {
				time.Sleep(150 * time.Millisecond)
				a.evaluate13Hand()
			}()
			return
		}
		thirteenHitInFlight = true
		is13Hitting = true
		go a.hit13Dice()
		return
	}

	if thirteenDealerTotal == thirteenPlayerTotal {
		a.AddLogMsg("[13_RULES] tie detected; replaying 13 round")
		if !ChatIsDisabled {
			waitForUnmute(90 * time.Second)
			time.Sleep(800 * time.Millisecond)
			sendMessageWithDelay("Tie — replaying 13 round")
		}
		go func() {
			time.Sleep(700 * time.Millisecond)
			a.begin13Sequence()
		}()
		is13Rolling = false
		is13Hitting = false
		return
	}
	a.finalize13Round(false, "dealer beat-or-tie")
	is13Rolling = false
	is13Hitting = false
}

func (a *App) evaluateSixHand() {
	defer func() {
		if r := recover(); r != nil {
			a.AddLogMsg(fmt.Sprintf("[6_CRASH_GUARD] recovered panic in evaluateSixHand: %v", r))
			log.Printf("[6_CRASH_GUARD] recovered panic in evaluateSixHand: %v", r)
			resetSixSequence()
			isSixRolling = false
			isSixHitting = false
		}
	}()

	mutex.Lock()
	mutex.Unlock()
	if !sixRoundActive {
		isSixRolling = false
		isSixHitting = false
		return
	}

	a.AddLogMsg(fmt.Sprintf("[6] evaluating sum=%d playerTurn=%t", currentSum, sixPlayerTurn))
	log.Printf("[6] evaluating sum=%d playerTurn=%t", currentSum, sixPlayerTurn)

	if sixPlayerTurn {
		playerLabel := strings.TrimSpace(sixPlayerName)
		if playerLabel == "" {
			playerLabel = strings.TrimSpace(lastTradePartnerName)
		}
		if playerLabel == "" {
			playerLabel = "Player"
		}

		sixPlayerTotal = currentSum
		a.AddLogMsg(fmt.Sprintf("[6] player total now %d", sixPlayerTotal))

		if sixPlayerTotal > 6 {
			a.finalizeSixRound(false, "player bust")
			isSixRolling = false
			isSixHitting = false
			return
		}

		if sixPlayerTotal == 6 {
			// Announce player result and indicate dealer roll in one message
			msg := fmt.Sprintf("%s got 6 | Rolling...", playerLabel)
			a.AddLogMsg(fmt.Sprintf("[6] announcing: %q", msg))
			if !ChatIsDisabled {
				waitForUnmute(90 * time.Second)
				time.Sleep(800 * time.Millisecond)
				sendMessageWithDelay(msg)
			}

			a.startSixDealerTurn("player reached 6")
			isSixRolling = false
			isSixHitting = false
			return
		}

		// Only prompt once
		if awaitingSixDecision {
			isSixRolling = false
			isSixHitting = false
			return
		}

		awaitingSixDecision = true
		awaitingSixDecisionPartnerName = strings.TrimSpace(sixPlayerName)
		if awaitingSixDecisionPartnerName == "" {
			awaitingSixDecisionPartnerName = strings.TrimSpace(lastTradePartnerName)
		}
		if awaitingSixDecisionPartnerName == "" {
			awaitingSixDecisionPartnerName = "Player"
		}
		if chatIdx, ok := lookupRoomEntityIndexByName(awaitingSixDecisionPartnerName); ok && chatIdx > 0 {
			awaitingSixDecisionPartnerID = chatIdx
		} else if chatIdx, ok := lookupUsers28RoomIndexByName(awaitingSixDecisionPartnerName); ok && chatIdx > 0 {
			awaitingSixDecisionPartnerID = chatIdx
		} else {
			awaitingSixDecisionPartnerID = lastTradePartnerID
		}

		prompt := fmt.Sprintf("%s total %d. Hit or stay?", playerLabel, sixPlayerTotal)
		a.AddLogMsg(fmt.Sprintf("[6] prompting decision: %q", prompt))
		if !ChatIsDisabled && !isMuted {
			sendMessageWithDelay(prompt)
			a.startSixDecisionTimeoutMonitor(awaitingSixDecisionPartnerName)
		} else {
			a.AddLogMsg("[6] prompt not sent (chat disabled or muted); auto-staying")
			awaitingSixDecision = false
			a.startSixDealerTurn("prompt unavailable auto-stay")
			isSixRolling = false
			isSixHitting = false
			return
		}

		isSixRolling = false
		isSixHitting = false
		return
	}

	sixDealerTotal = currentSum
	a.AddLogMsg(fmt.Sprintf("[6] dealer total now %d (player=%d)", sixDealerTotal, sixPlayerTotal))

	if sixDealerTotal > 6 {
		a.finalizeSixRound(true, "dealer bust")
		isSixRolling = false
		isSixHitting = false
		return
	}

	if sixDealerTotal < sixPlayerTotal {
		a.AddLogMsg(fmt.Sprintf("[6] dealer total %d < player %d; dealer hits", sixDealerTotal, sixPlayerTotal))
		if sixHitInFlight {
			go func() {
				time.Sleep(150 * time.Millisecond)
				a.evaluateSixHand()
			}()
			return
		}
		sixHitInFlight = true
		isSixHitting = true
		go a.hitSixDice()
		return
	}

	if sixDealerTotal == sixPlayerTotal {
		a.AddLogMsg("[6_RULES] tie detected; replaying 6 round")
		if !ChatIsDisabled {
			waitForUnmute(90 * time.Second)
			time.Sleep(800 * time.Millisecond)
			sendMessageWithDelay("Tie — replaying 6 round")
		}
		go func() {
			time.Sleep(700 * time.Millisecond)
			a.beginSixSequence()
		}()
		isSixRolling = false
		isSixHitting = false
		return
	}
	a.finalizeSixRound(false, "dealer beat-or-tie")
	isSixRolling = false
	isSixHitting = false
}

// Wait for all dice results and evaluate the tri hand
func (a *App) evaluateTriRound() {
	defer func() {
		if r := recover(); r != nil {
			a.AddLogMsg(fmt.Sprintf("[TRI_CRASH_GUARD] recovered panic in evaluateTriRound: %v", r))
			log.Printf("[TRI_CRASH_GUARD] recovered panic in evaluateTriRound: %v", r)
			resetTriSequence()
			isTriRolling = false
		}
	}()

	mutex.Lock()
	mutex.Unlock()
	if !triRoundActive {
		isTriRolling = false
		return
	}

	total := sumHandInt([]int{
		diceList[0].Value,
		diceList[2].Value,
		diceList[4].Value,
	})

	totalText := strconv.Itoa(total)
	a.AddLogMsg(fmt.Sprintf("[TRI] evaluating total=%d playerTurn=%t mode=%s", total, triPlayerTurn, triMode))
	log.Printf("[TRI] evaluating total=%d playerTurn=%t mode=%s", total, triPlayerTurn, triMode)

	if !ChatIsDisabled && triPlayerTurn {
		// Send player total and indicate dealer will roll
		sendMessageWithDelay(fmt.Sprintf("%s | Rolling...", totalText))
	}

	if triPlayerTurn {
		triPlayerTotal = total
		a.setCurrentGameHistoryResults(totalText, "", "", "In Progress", false)
		a.noteCurrentGameHistory(fmt.Sprintf("Tri player total recorded (%s)", triMode))

		triPlayerTurn = false

		go func() {
			time.Sleep(700 * time.Millisecond)
			// Start dealer roll without an extra shout (we already indicated it)
			a.AddLogMsg("[GAME_SELECT] starting dealer roll")
			time.Sleep(700 * time.Millisecond)
			isTriRolling = true
			a.rollTriDice()
		}()

		return
	}

	triDealerTotal = total
	isTriRolling = false
	a.finalizeTriRound()
}

func (a *App) evaluatePairUpRound() {
	defer func() {
		if r := recover(); r != nil {
			a.AddLogMsg(fmt.Sprintf("[PU_CRASH_GUARD] recovered panic in evaluatePairUpRound: %v", r))
			log.Printf("[PU_CRASH_GUARD] recovered panic in evaluatePairUpRound: %v", r)
			isPairUpRolling = false
		}
	}()

	mutex.Lock()
	if len(diceList) < 5 {
		mutex.Unlock()
		isPairUpRolling = false
		return
	}
	v1 := diceList[0].Value
	v2 := diceList[2].Value
	v3 := diceList[4].Value
	mutex.Unlock()

	a.AddLogMsg(fmt.Sprintf("[PU] evaluating: %d %d %d", v1, v2, v3))

	playerWins := (v1 == v2 || v1 == v3 || v2 == v3)

	resultText := fmt.Sprintf("%d-%d-%d", v1, v2, v3)

	playerName := strings.TrimSpace(lastTradePartnerName)
	if playerName == "" {
		playerName = "Player"
	}

	winnerName := "Dealer"
	if playerWins {
		winnerName = playerName
	}

	winnerMsg := fmt.Sprintf("%s Wins - %s: %d-%d-%d", winnerName, playerName, v1, v2, v3)

	if !ChatIsDisabled {
		sendMessageWithDelay(winnerMsg)
	}

	payoutTargetID := lastTradePartnerID
	payoutTargetName := playerName
	isPairUpRolling = false

	if playerWins && payoutTargetID > 0 {
		a.setCurrentGameHistoryResults(resultText, "", playerName, "Payout Pending", false)
		a.noteCurrentGameHistory(winnerMsg)
		a.AddLogMsg(fmt.Sprintf("[PAYOUT] pu player won, initiating payout trade to %s (%d)", payoutTargetName, payoutTargetID))
		resetPayoutRetryState()

		// Post the round outcome immediately so Discord shows the win.
		a.sendDiscordRoundResult(playerName, resultText, "", winnerMsg)

		if isRiskEnabled {
			if riskSessionActive {
				go a.applyRiskOutcome(true)
				return
			}
			params := map[string]interface{}{}
			go a.handlePlayerWinRisk(cloneTradeItems(gameBetItems), payoutTargetName, payoutTargetID, "PU", params)
			return
		}
		startPayout(a, payoutTargetID, payoutTargetName)
		return
	}

	a.setCurrentGameHistoryResults(resultText, "", "Dealer", "Completed", true)
	a.noteCurrentGameHistory(winnerMsg)
	if isRiskEnabled && riskSessionActive {
		go a.applyRiskOutcome(false)
		return
	}
	go a.openDealerAfterRound()
	isPairUpRolling = false
}

// Sum the values of the dice and return a string representation
func sumHand(values []int) string {
	sum := 0
	for _, val := range values {
		sum += val
	}
	return strconv.Itoa(sum)
}

// Sum the values of the dice and return the integer sum
func sumHandInt(values []int) int {
	sum := 0
	for _, val := range values {
		sum += val
	}
	return sum
}

// Evaluate the hand of dice and return a string representation
// thank you b7 <3 (and me, eduard, selfplug lol)
func (a *App) toPokerString(dices []*Dice) string {
	config := a.LoadConfig()
	if config == nil {
		fmt.Println("Using default configuration")
		config = defaultPokerDisplayConfig()
	}

	return formatPokerHandResult(config, evaluatePokerRules(dices))
}
