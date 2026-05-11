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

	if !ChatIsDisabled {
		if !isMuted {
			sendMessageWithDelay(winnerMsg)
		} else {
			messageQueue = append(messageQueue, winnerMsg)
		}
	} else {
		// Chat is disabled; use a public shout so results are still visible.
		sendShout(winnerMsg)
	}

	payoutTargetID := lastTradePartnerID
	payoutTargetName := playerName

	if playerWins && payoutTargetID > 0 {
		a.setCurrentGameHistoryResults(resultText, "", playerName, "Payout Pending", false)
		a.setCurrentGameHistoryPayoutMultiplier(2.0)
		a.noteCurrentGameHistory(winnerMsg)
		resetPayoutRetryState()
		if isRiskEnabled {
			if riskSessionActive {
				// Post the round outcome immediately so external integrations see it.
				a.sendDiscordRoundResult(playerName, resultText, "", winnerMsg)
				go a.applyRiskOutcome(true)
				return
			}
			params := map[string]interface{}{}
			go a.handlePlayerWinRisk(cloneTradeItems(gameBetItems), payoutTargetName, payoutTargetID, "DT", params)
			// Post round outcome so external integrations show the win while risk prompt is pending.
			a.sendDiscordRoundResult(playerName, resultText, "", winnerMsg)
			return
		}
		a.AddLogMsg(fmt.Sprintf("[PAYOUT] dt player won, initiating payout trade to %s (%d)", payoutTargetName, payoutTargetID))
		startPayout(a, payoutTargetID, payoutTargetName)
		return
	}
	a.setCurrentGameHistoryResults(resultText, "", a.getCurrentDealerName(), "Completed", true)
	a.noteCurrentGameHistory(winnerMsg)
	if isRiskEnabled && riskSessionActive {
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
	if !ChatIsDisabled {
		if !isMuted {
			sendMessageWithDelay(msg)
		} else {
			messageQueue = append(messageQueue, msg)
		}
	} else {
		sendShout(msg)
	}

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

	go a.openDealerAfterRound()
}
