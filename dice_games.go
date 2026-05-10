package main

import (
	"fmt"
	"strings"
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

	// Assuming first two dice are used for this game.
	dice1 := diceList[0].Value
	dice2 := diceList[1].Value

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
