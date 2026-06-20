package main

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
)

// Double Trouble (2 Dice) Rules:
// Win: Any Even total (2, 4, 6, 8, 10, 12).
// Lose: Any Odd total (3, 5, 9, 11).
// House Win: If they roll exactly 7.

type GameResult int

const (
	PlayerWin GameResult = iota
	PlayerLose
	HouseWin
)

func evaluateDoubleTrouble(dice1, dice2 int) (int, GameResult) {
	total := dice1 + dice2
	if total == 7 {
		return total, HouseWin
	}
	if total%2 == 0 {
		return total, PlayerWin
	}
	return total, PlayerLose
}

// Triple Trouble removed

func (a *App) evaluateDoubleTroubleRound() {
	if len(diceList) < 2 {
		a.AddLogMsg("[DT] Error: not enough dice found for Double Trouble")
		return
	}

	// Prefer using slots 1 and 5 (indices 0 and 4) when available, otherwise
	// fall back to the first two configured dice (indices 0 and 1).
	var dice1, dice2 int
	if len(diceList) >= 5 {
		dice1 = diceList[0].Value
		dice2 = diceList[4].Value
	} else if len(diceList) >= 2 {
		dice1 = diceList[0].Value
		dice2 = diceList[1].Value
	} else {
		a.AddLogMsg("[DT] Error: not enough dice found for Double Trouble")
		return
	}

	total, result := evaluateDoubleTrouble(dice1, dice2)

	// Log the resolved dice and result for debugging and visibility
	a.AddLogMsg(fmt.Sprintf("[DT] evaluating dice: %d + %d = %d -> result=%v", dice1, dice2, total, result))

	playerName := strings.TrimSpace(lastTradePartnerName)
	if playerName == "" {
		playerName = "Player"
	}

	var winnerName string
	var playerWins bool
	switch result {
	case PlayerWin:
		winnerName = playerName
		playerWins = true
	case PlayerLose:
		winnerName = "Dealer"
		playerWins = false
	case HouseWin:
		winnerName = "Dealer" // House win is a dealer win
		playerWins = false
	}

	resultText := fmt.Sprintf("%d", total)
	winnerMsg := fmt.Sprintf("%s Wins - Total: %s", winnerName, resultText)

	sendMessageWithDelay(winnerMsg)

	payoutTargetID := lastTradePartnerID
	payoutTargetName := playerName
	isDTRolling = false

	if playerWins && payoutTargetID > 0 {
		a.setCurrentGameHistoryResults(resultText, "", playerName, "Payout Pending", false)
		a.setCurrentGameHistoryPayoutMultiplier(2.0)
		a.noteCurrentGameHistory(winnerMsg)
		resetPayoutRetryState()

		if isRiskEnabled {
			if riskSessionActive {
				a.applyRiskOutcome(true)
				a.sendDiscordRoundResult(playerName, resultText, "", winnerMsg)
				return
			}
			params := map[string]interface{}{}
			a.handlePlayerWinRisk(cloneTradeItems(gameBetItems), payoutTargetName, payoutTargetID, "DT", params)
			a.sendDiscordRoundResult(playerName, resultText, "", winnerMsg)
			return
		}
		a.sendDiscordRoundResult(playerName, resultText, "", winnerMsg)
		a.AddLogMsg(fmt.Sprintf("[PAYOUT] dt player won, initiating payout trade to %s (%d)", payoutTargetName, payoutTargetID))
		startPayout(a, payoutTargetID, payoutTargetName)
		return
	}
	mutex.Lock()
	riskActive := riskSessionActive
	splitEnabled := isSplitDealerMode
	mutex.Unlock()

	completeRound := !riskActive
	a.setCurrentGameHistoryResults(resultText, "", a.getCurrentDealerName(), "Completed", completeRound)
	a.noteCurrentGameHistory(winnerMsg)

	if splitEnabled {
		a.finalizeBankerTrade()
	}

	if isRiskEnabled && riskActive {
		go a.applyRiskOutcome(false)
		return
	}
	go a.openDealerAfterRound()
}

func (a *App) beginBanditRound() {
	mutex.Lock()
	banditRoundActive = true
	isBanditRolling = true
	mutex.Unlock()

	a.AddLogMsg("[BANDIT] starting One Arm Bandit round")
	a.setCurrentGameHistoryGame("Bandit")

	playerName := strings.TrimSpace(lastTradePartnerName)
	if playerName == "" {
		playerName = "Player"
	}

	msg := fmt.Sprintf("%s! Player Roll — One Arm Bandit: GOOD LUCK!", playerName)
	sendMessageWithDelay(msg)

	go a.rollBanditDice()
}

func (a *App) rollBanditDice() {
	if fakeDiceTestingMode {
		time.Sleep(1 * time.Second)
		mutex.Lock()
		for i := 0; i < 3 && i < len(diceList); i++ {
			diceList[i].Value = rand.Intn(6) + 1
			diceList[i].IsRolling = false
		}
		mutex.Unlock()
		a.evaluateBanditRound()
		return
	}

	mutex.Lock()
	numDice := len(diceList)
	if numDice < 3 {
		mutex.Unlock()
		a.AddLogMsg("[BANDIT] Error: not enough dice for Bandit (need 3)")
		sendShout("Error: Setup requires 3 dice for Bandit.")
		return
	}

	// Reset wait group
	resultsWaitGroup.Add(3)
	for i := 0; i < 3; i++ {
		diceList[i].Roll()
		time.Sleep(rollDelay + time.Duration(rand.Intn(100))*time.Millisecond)
	}
	mutex.Unlock()

	resultsWaitGroup.Wait()
	a.evaluateBanditRound()
}

func (a *App) evaluateBanditRound() {
	mutex.Lock()
	if len(diceList) < 3 {
		mutex.Unlock()
		return
	}
	d1 := diceList[0].Value
	d2 := diceList[1].Value
	d3 := diceList[2].Value
	jackpotMult := banditJackpotPayout
	triplesMult := banditTriplesPayout
	mutex.Unlock()

	a.AddLogMsg(fmt.Sprintf("[BANDIT] evaluation: %d-%d-%d", d1, d2, d3))

	resultText := fmt.Sprintf("%d-%d-%d", d1, d2, d3)
	playerName := strings.TrimSpace(lastTradePartnerName)
	if playerName == "" {
		playerName = "Player"
	}

	// Triple Logic
	if d1 == d2 && d2 == d3 {
		mult := triplesMult
		winnerMsg := fmt.Sprintf("%s: TRIPLES! %d-%d-%d", playerName, d1, d2, d3)
		if d1 == 6 {
			mult = jackpotMult
			winnerMsg = fmt.Sprintf("%s: JACKPOT! %d-%d-%d", playerName, d1, d2, d3)
		}

		sendShout(winnerMsg)

		payoutTargetID := lastTradePartnerID
		payoutTargetName := playerName

		if payoutTargetID > 0 {
			a.setCurrentGameHistoryResults(resultText, "", playerName, "Payout Pending", false)
			a.setCurrentGameHistoryPayoutMultiplier(mult)
			a.noteCurrentGameHistory(winnerMsg)
			resetPayoutRetryState()

			// Post the round outcome immediately so Discord shows the win.
			a.sendDiscordRoundResult(playerName, resultText, "", winnerMsg)

			mutex.Lock()
			payoutMultiplierForRound = mult
			banditRoundActive = false
			isBanditRolling = false
			mutex.Unlock()

			a.AddLogMsg(fmt.Sprintf("[PAYOUT] bandit player won x%.2f, initiating payout", mult))
			startPayout(a, payoutTargetID, payoutTargetName)
			return
		}
	}

	// Consecutive Pair Logic (XXY or YXX) -> Re-roll
	if (d1 == d2) || (d2 == d3) {
		msg := fmt.Sprintf("%s: Pair! %d-%d-%d - FREE RE-ROLL!", playerName, d1, d2, d3)
		sendShout(msg)
		a.AddLogMsg("[BANDIT] re-roll triggered")
		time.Sleep(2 * time.Second)
		go a.rollBanditDice()
		return
	}

	// Loss (Split Pair XYX or No Match XYZ)
	winnerMsg := fmt.Sprintf("Dealer Wins: %d-%d-%d", d1, d2, d3)
	sendShout(winnerMsg)

	a.setCurrentGameHistoryResults(resultText, "", a.getCurrentDealerName(), "Completed", true)
	a.noteCurrentGameHistory(winnerMsg)

	mutex.Lock()
	banditRoundActive = false
	isBanditRolling = false
	mutex.Unlock()

	mutex.Lock()
	splitEnabled := isSplitDealerMode
	mutex.Unlock()
	if splitEnabled {
		a.finalizeBankerTrade()
	}

	go a.openDealerAfterRound()
}

// evaluateMidHouseRound evaluates the 3-dice Mid-House 10/11 game.
func (a *App) evaluateMidHouseRound() {
	mutex.Lock()
	total := 0
	indices := []int{0, 1, 2}
	if len(diceList) >= 5 {
		indices = []int{0, 2, 4}
	}
	if len(diceList) < len(indices) {
		mutex.Unlock()
		a.AddLogMsg("[MIDHOUSE_ERROR] Not enough dice to evaluate")
		go a.openDealerAfterRound()
		return
	}
	for _, idx := range indices {
		total += diceList[idx].Value
	}
	midHouseRoundActive = false
	choice := midHouseChoice
	mutex.Unlock()

	playerName := strings.TrimSpace(lastTradePartnerName)
	if playerName == "" {
		playerName = "Player"
	}

	playerWins := false
	if total == 10 || total == 11 {
		// Mid-House/Dead Zone: Dealer always wins
		playerWins = false
	} else if choice == "u10" {
		playerWins = (total <= 9)
	} else if choice == "o11" {
		playerWins = (total >= 12)
	}

	winnerName := "Dealer"
	if playerWins {
		winnerName = playerName
	}

	resultMsg := fmt.Sprintf("%s rolled %d.", playerName, total)
	status := "LOSE"
	if playerWins {
		status = "WIN"
	} else if total == 10 || total == 11 {
		status = "Mid-House Dealer WIN"
	} else {
		status = "Dealer WIN"
	}

	msg := fmt.Sprintf("%s - %s!", resultMsg, status)
	a.AddLogMsg(fmt.Sprintf("[MIDHOUSE_RULES] player=%s total=%d choice=%s winner=%s", playerName, total, choice, winnerName))
	sendShout(msg)

	payoutTargetID := lastTradePartnerID
	payoutTargetName := playerName
	isMidHouseRolling = false

	if playerWins && payoutTargetID > 0 {
		payoutMultiplierForRound = 2.0
		a.setCurrentGameHistoryPayoutMultiplier(2.0)
		a.setCurrentGameHistoryResults(fmt.Sprintf("%d", total), "", playerName, "Payout Pending", false)
		a.noteCurrentGameHistory(msg)
		resetPayoutRetryState()

		if isRiskEnabled {
			if riskSessionActive {
				a.applyRiskOutcome(true)
				a.sendDiscordRoundResult(playerName, fmt.Sprintf("%d", total), "", msg)
				return
			}
			params := map[string]interface{}{"mhChoice": choice}
			a.handlePlayerWinRisk(cloneTradeItems(gameBetItems), payoutTargetName, payoutTargetID, "MidHouse", params)
			a.sendDiscordRoundResult(playerName, fmt.Sprintf("%d", total), "", msg)
			return
		}
		a.sendDiscordRoundResult(playerName, fmt.Sprintf("%d", total), "", msg)
		go startPayout(a, payoutTargetID, payoutTargetName)
	} else {
		mutex.Lock()
		riskActive := riskSessionActive
		splitEnabled := isSplitDealerMode
		mutex.Unlock()

		completeRound := !riskActive
		a.setCurrentGameHistoryResults(fmt.Sprintf("%d", total), "", "Dealer", "Completed", completeRound)
		a.noteCurrentGameHistory(msg)

		if splitEnabled {
			a.finalizeBankerTrade()
		}

		if isRiskEnabled && riskActive {
			go a.applyRiskOutcome(false)
			return
		}
		go a.openDealerAfterRound()
	}
}
